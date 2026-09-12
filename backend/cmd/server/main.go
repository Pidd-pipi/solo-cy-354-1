// Command server is the campus-market backend entrypoint.
package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lp/campus-market/internal/app"
	"github.com/lp/campus-market/internal/config"
	"github.com/lp/campus-market/internal/util"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func main() {
	logger := util.NewLogger()
	cfg := config.Load()

	openDB := func(dsn string) (*gorm.DB, error) {
		return gorm.Open(mysql.Open(dsn), &gorm.Config{
			Logger: gormlogger.Default.LogMode(gormlogger.Silent),
		})
	}

	application, err := app.Start(cfg, logger, openDB, seed)
	if err != nil {
		// A failed startup migration (e.g. reports.handle_result could not be
		// widened) is fatal: the service must not become available on a
		// half-migrated schema where long report notes keep failing.
		logger.Error("application startup failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down server")
	_ = application.Shutdown(10 * time.Second)
}
