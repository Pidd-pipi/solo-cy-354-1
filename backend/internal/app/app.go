// Package app composes the application bootstrap: database connection,
// schema migration, seeding, HTTP server and its lifecycle. The ordering in
// Start is a hard contract: the HTTP server does not listen until every
// startup migration succeeds, so a failed reports.handle_result widening
// prevents the service from becoming available.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/lp/campus-market/internal/config"
	"github.com/lp/campus-market/internal/database"
	"github.com/lp/campus-market/internal/router"
	"gorm.io/gorm"
)

// OpenDBFunc opens a GORM handle for the given DSN.
type OpenDBFunc func(dsn string) (*gorm.DB, error)

// Seeder populates demo data after a successful migration.
type Seeder func(ctx context.Context, db *gorm.DB, logger *slog.Logger) error

// Application holds the running server and its dependencies.
type Application struct {
	DB     *gorm.DB
	Server *http.Server
	Logger *slog.Logger
}

// Start connects to the database, runs all startup migrations, seeds demo
// data and only THEN begins listening. It returns an error WITHOUT serving
// when a migration fails — in particular when reports.handle_result cannot
// be widened, because long administrator report notes would otherwise keep
// failing at runtime.
func Start(cfg *config.Config, logger *slog.Logger, openDB OpenDBFunc, seeder Seeder) (*Application, error) {
	db, err := openDB(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	closePool := func() {
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
	}

	// Migration is fatal: do not construct or listen on the HTTP server
	// until the schema (incl. the report-note column width) is correct.
	if err := database.Migrate(db); err != nil {
		closePool()
		return nil, err
	}

	if cfg.SeedingEnabled && seeder != nil {
		if err := seeder(context.Background(), db, logger); err != nil {
			// Seeding is non-fatal: schema invariants are already in place.
			logger.Error("seeding failed", slog.String("error", err.Error()))
		}
	}

	a := &Application{
		DB:     db,
		Logger: logger,
		Server: &http.Server{
			Addr:         ":" + cfg.Port,
			Handler:      router.New(cfg, db, logger),
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
		},
	}
	go func() {
		logger.Info("campus-market server listening", slog.String("addr", a.Server.Addr))
		if err := a.Server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", slog.String("error", err.Error()))
		}
	}()
	return a, nil
}

// Shutdown gracefully stops the HTTP server and closes the database pool.
func (a *Application) Shutdown(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := a.Server.Shutdown(ctx)
	if sqlDB, e := a.DB.DB(); e == nil {
		_ = sqlDB.Close()
	}
	return err
}
