// Package database owns schema creation and forward migrations executed at
// startup. Keeping the statements here (instead of inline in main) lets the
// regression test suite exercise the exact same migration path as production.
package database

import (
	"log/slog"

	"github.com/lp/campus-market/internal/model"
	"gorm.io/gorm"
)

// AllModels lists every table managed by AutoMigrate, in dependency order.
func AllModels() []interface{} {
	return []interface{}{
		&model.User{}, &model.Product{}, &model.Conversation{}, &model.Message{},
		&model.TradeOrder{}, &model.Review{}, &model.BookExchange{}, &model.Report{},
	}
}

// AutoMigrate creates missing tables/columns/indexes.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(AllModels()...)
}

// EnsureReportHandleResultWidth widens reports.handle_result to VARCHAR(512)
// on existing databases. GORM AutoMigrate does not reliably widen a column
// that was created narrower (the original reports table used VARCHAR(128)),
// so deployments upgrading from that schema would reject long administrator
// notes ("Data too long for column" under strict SQL mode). The statement is
// idempotent and a no-op when the column already has the target width.
const reportHandleResultDDL = "ALTER TABLE reports MODIFY COLUMN handle_result VARCHAR(512) NOT NULL DEFAULT ''"

func EnsureReportHandleResultWidth(db *gorm.DB) error {
	return db.Exec(reportHandleResultDDL).Error
}

// Migrate runs schema creation and forward migrations. A failed widening is
// logged but does not abort startup on engines without ALTER support (tests);
// MySQL/MariaDB must succeed for the report feature to work correctly.
func Migrate(db *gorm.DB, logger *slog.Logger) error {
	if err := AutoMigrate(db); err != nil {
		return err
	}
	if err := EnsureReportHandleResultWidth(db); err != nil {
		logger.Warn("widen reports.handle_result skipped", slog.String("error", err.Error()))
	}
	return nil
}
