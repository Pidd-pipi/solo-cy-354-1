// Package database owns schema creation and forward migrations executed at
// startup. Keeping the statements here (instead of inline in main) lets the
// regression test suite exercise the exact same migration path as production.
package database

import (
	"errors"
	"fmt"

	"github.com/lp/campus-market/internal/model"
	"gorm.io/gorm"
)

// ReportHandleResultDDL widens reports.handle_result to VARCHAR(512). It is
// exported so tests can assert the migration that guards long report notes.
const ReportHandleResultDDL = "ALTER TABLE reports MODIFY COLUMN handle_result VARCHAR(512) NOT NULL DEFAULT ''"

// ErrReportNoteColumnWidth marks a failed reports.handle_result widening. It
// is fatal at startup: without the wide column, handling a report with a long
// administrator note keeps failing at runtime, so the service must not become
// available.
var ErrReportNoteColumnWidth = errors.New("startup migration failed: report note column reports.handle_result could not be widened to VARCHAR(512)")

// AllModels lists every table managed by AutoMigrate, in dependency order.
func AllModels() []interface{} {
	return []interface{}{
		&model.User{}, &model.Product{}, &model.Conversation{}, &model.Message{},
		&model.TradeOrder{}, &model.Review{}, &model.BookExchange{}, &model.Report{},
	}
}

// AutoMigrate creates missing tables/columns/indexes. It does NOT widen
// columns that already exist, so the reports.handle_result widening is a
// separate, explicit migration step.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(AllModels()...)
}

// EnsureReportHandleResultWidth widens reports.handle_result to VARCHAR(512)
// on existing databases. The original reports table used VARCHAR(128), and
// the administrator handling result ("下架商品：<note>", note up to 200 chars)
// no longer fits; strict SQL mode then rejects every long-note handling
// request. The statement is idempotent when the column already has the width.
func EnsureReportHandleResultWidth(db *gorm.DB) error {
	if err := db.Exec(ReportHandleResultDDL).Error; err != nil {
		// Wrap both the feature sentinel and the underlying driver error so
		// callers can match ErrReportNoteColumnWidth AND inspect the real
		// MySQL error code (e.g. 1142 ALTER denied, 1146 missing table).
		return fmt.Errorf("%w: 举报处理备注字段 reports.handle_result 扩列失败（DDL: %s）: %w",
			ErrReportNoteColumnWidth, ReportHandleResultDDL, err)
	}
	return nil
}

// Migrate runs schema creation and forward migrations. Any failure — in
// particular the report-note column widening — is returned to the caller,
// which must abort startup rather than serve a half-migrated database.
func Migrate(db *gorm.DB) error {
	if err := AutoMigrate(db); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}
	if err := EnsureReportHandleResultWidth(db); err != nil {
		return err
	}
	return nil
}
