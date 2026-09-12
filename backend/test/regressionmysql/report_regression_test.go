package regressionmysql

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/lp/campus-market/internal/constants"
	"github.com/lp/campus-market/internal/database"
	"github.com/lp/campus-market/internal/testsupport"
	"gorm.io/gorm"
)

// discardLogger swallows migration warnings in tests.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestOldColumnWidthIsReallyEnforced first proves the test harness can detect
// the old VARCHAR(128) defect: a 200-character admin note (the maximum allowed
// by the UI) MUST fail to write to a 128-wide column under strict SQL mode.
// If this test did not fail here, the column-width regression coverage would
// be vacuous.
func TestOldColumnWidthIsReallyEnforced(t *testing.T) {
	db := freshDB(t)
	testsupport.MigrateAndSeed(t, db)
	app := testsupport.New(t, db)
	pid := publishProduct(t, app)
	rid := fileReport(t, app, pid, "fraud")
	simulateOldSchema(t, db)

	note := strings.Repeat("超", 200)
	err := db.Exec(
		"UPDATE reports SET handle_result = ? WHERE id = ?",
		"下架商品："+note, rid,
	).Error
	if err == nil {
		t.Fatalf("200-char note unexpectedly fit into VARCHAR(128): column-width enforcement is not active, this regression suite cannot detect the bug")
	}
	if !isDataTooLong(err) {
		t.Fatalf("expected ER_DATA_TOO_LONG(1406) from VARCHAR(128), got: %v", err)
	}
	t.Logf("VARCHAR(128) rejected the 200-char note as expected: %v", err)
}

// TestStartupMigrationWidensOldSchema reproduces an upgraded deployment whose
// reports table was created with the old VARCHAR(128) column, then runs the
// EXACT production startup migration and verifies behaviorally that a 200-char
// note can now be written and read back complete.
func TestStartupMigrationWidensOldSchema(t *testing.T) {
	db := freshDB(t)
	testsupport.MigrateAndSeed(t, db)
	app := testsupport.New(t, db)
	pid := publishProduct(t, app)
	rid := fileReport(t, app, pid, "fraud")
	simulateOldSchema(t, db)

	// On the old schema the API handling must fail at the database exactly like
	// the reported production incident; assert that behavior first.
	rec := app.Do(http.MethodPost, "/api/v1/admin/reports/"+itoa(rid)+"/handle", app.TokenC,
		testsupport.JSON(map[string]any{"action": "remove", "note": strings.Repeat("超", 200)}))
	if rec.Code == http.StatusOK {
		t.Fatalf("200-char handling unexpectedly succeeded against VARCHAR(128) old schema")
	}
	t.Logf("old schema rejects long-note handling with status=%d body=%s", rec.Code, rec.Body.String())
	// failure must be atomic on real InnoDB: report still pending, product on sale
	var status, pstatus string
	if err := db.Table("reports").Where("id = ?", rid).Select("status").Scan(&status).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("products").Where("id = ?", pid).Select("status").Scan(&pstatus).Error; err != nil {
		t.Fatal(err)
	}
	if status != constants.ReportStatusPending || pstatus != constants.ProductStatusOnSale {
		t.Fatalf("old-schema failure was not atomic: report=%s product=%s", status, pstatus)
	}

	// Run the same migration entry point used by cmd/server main.
	if err := database.Migrate(db, discardLogger()); err != nil {
		t.Fatalf("startup migration: %v", err)
	}
	if got := columnType(t, db, "reports", "handle_result"); got != "varchar(512)" {
		t.Fatalf("handle_result type after migration = %s, want varchar(512)", got)
	}
	// Idempotent: running it a second time must succeed and keep the width.
	if err := database.EnsureReportHandleResultWidth(db); err != nil {
		t.Fatalf("second migration run: %v", err)
	}
	if got := columnType(t, db, "reports", "handle_result"); got != "varchar(512)" {
		t.Fatalf("handle_result type changed on rerun: %s", got)
	}

	// Behavioral proof: the formerly-rejected handling now succeeds end to end.
	rec = app.Do(http.MethodPost, "/api/v1/admin/reports/"+itoa(rid)+"/handle", app.TokenC,
		testsupport.JSON(map[string]any{"action": "remove", "note": strings.Repeat("超", 200)}))
	if rec.Code != http.StatusOK {
		t.Fatalf("handling after migration status = %d body=%s", rec.Code, rec.Body.String())
	}
	note := strings.Repeat("超", 200)
	full := "下架商品：" + note
	var got string
	if err := db.Raw("SELECT handle_result FROM reports WHERE id = ?", rid).Scan(&got).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != full {
		t.Fatalf("handle_result truncated: got %d runes, want %d", len([]rune(got)), len([]rune(full)))
	}
}

// TestLongNoteEndToEndOnRealMySQL runs the full HTTP handling flow on a real
// InnoDB server after the startup migration: 200-char notes for both "remove"
// and "reject" are saved and returned completely, and 201 chars is rejected
// with HTTP 400 while the report remains pending and the product untouched.
func TestLongNoteEndToEndOnRealMySQL(t *testing.T) {
	db := freshDB(t)
	testsupport.MigrateAndSeed(t, db)
	// Production startup path (fresh schema is already 512-wide, but exercise it).
	if err := database.Migrate(db, discardLogger()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	app := testsupport.New(t, db)

	tests := []struct {
		name        string
		action      string
		wantStatus  string
		wantProduct string
	}{
		{name: "remove with 200-char note", action: "remove", wantStatus: constants.ReportStatusRemoved, wantProduct: constants.ProductStatusRemoved},
		{name: "reject with 200-char note", action: "reject", wantStatus: constants.ReportStatusRejected, wantProduct: constants.ProductStatusOnSale},
	}
	note200 := strings.Repeat("长", 200)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pid := publishProduct(t, app)
			rid := fileReport(t, app, pid, "fraud")
			rec := app.Do(http.MethodPost, "/api/v1/admin/reports/"+itoa(rid)+"/handle", app.TokenC,
				testsupport.JSON(map[string]any{"action": tt.action, "note": note200}))
			if rec.Code != http.StatusOK {
				t.Fatalf("handle status = %d, want 200 body=%s", rec.Code, rec.Body.String())
			}
			var env struct {
				Status       string `json:"status"`
				HandleResult string `json:"handle_result"`
				HandleNote   string `json:"handle_note"`
			}
			var envelope testsupport.Envelope
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(envelope.Data, &env); err != nil {
				t.Fatal(err)
			}
			wantResult := map[string]string{
				"remove": "下架商品：" + note200,
				"reject": "驳回举报：" + note200,
			}[tt.action]
			if env.Status != tt.wantStatus || env.HandleResult != wantResult || env.HandleNote != note200 {
				t.Fatalf("unexpected handling result: %+v", env)
			}
			if len([]rune(env.HandleResult)) <= 128 {
				t.Fatalf("returned result did not exceed old 128 width; long note not really saved")
			}
			var pstatus string
			if err := db.Table("products").Where("id = ?", pid).Select("status").Scan(&pstatus).Error; err != nil {
				t.Fatal(err)
			}
			if pstatus != tt.wantProduct {
				t.Fatalf("product status = %s, want %s", pstatus, tt.wantProduct)
			}
		})
	}

	// 201 chars: validation rejects at the API and nothing is half-updated.
	t.Run("201-char note rejected and state intact", func(t *testing.T) {
		pid := publishProduct(t, app)
		rid := fileReport(t, app, pid, "other")
		rec := app.Do(http.MethodPost, "/api/v1/admin/reports/"+itoa(rid)+"/handle", app.TokenC,
			testsupport.JSON(map[string]any{"action": "remove", "note": strings.Repeat("长", 201)}))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("201-char note status = %d, want 400", rec.Code)
		}
		var status string
		var handler *uint
		if err := db.Table("reports").Where("id = ?", rid).Select("status, handler_id").Row().Scan(&status, &handler); err != nil {
			t.Fatal(err)
		}
		if status != constants.ReportStatusPending || handler != nil {
			t.Fatalf("report changed by rejected request: status=%s handler=%v", status, handler)
		}
		var pstatus string
		if err := db.Table("products").Where("id = ?", pid).Select("status").Scan(&pstatus).Error; err != nil {
			t.Fatal(err)
		}
		if pstatus != constants.ProductStatusOnSale {
			t.Fatalf("product changed by rejected request: %s", pstatus)
		}
	})
}

// TestTransactionRollbackAndRetryOnRealInnoDB injects a failure into the
// report UPDATE while the product has ALREADY been updated inside the same
// transaction, then asserts InnoDB rolled everything back, and that the same
// pending report handles successfully after the fault is removed (recovery).
func TestTransactionRollbackAndRetryOnRealInnoDB(t *testing.T) {
	db := freshDB(t)
	testsupport.MigrateAndSeed(t, db)
	if err := database.Migrate(db, discardLogger()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	app := testsupport.New(t, db)
	pid := publishProduct(t, app)
	rid := fileReport(t, app, pid, "prohibited")

	const cbName = "test_inject_report_update_failure_mysql"
	injected := errors.New("injected report update failure")
	if err := db.Callback().Update().Before("gorm:update").Register(cbName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "reports" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	rec := app.Do(http.MethodPost, "/api/v1/admin/reports/"+itoa(rid)+"/handle", app.TokenC,
		testsupport.JSON(map[string]any{"action": "remove", "note": "应整体回滚"}))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("failed handle status = %d, want 500 body=%s", rec.Code, rec.Body.String())
	}

	// Assert NO half-update at the storage level.
	var status string
	var pendingProduct *uint
	var handlerID *uint
	var result string
	row := db.Table("reports").Where("id = ?", rid).
		Select("status, pending_product_id, handler_id, handle_result").Row()
	if err := row.Scan(&status, &pendingProduct, &handlerID, &result); err != nil {
		t.Fatalf("scan report: %v", err)
	}
	if status != constants.ReportStatusPending {
		t.Fatalf("report half-updated to %s (rollback failed)", status)
	}
	if pendingProduct == nil || *pendingProduct != pid {
		t.Fatalf("pending_product_id not restored: %v", pendingProduct)
	}
	if handlerID != nil || result != "" {
		t.Fatalf("handler fields must be untouched, got handler=%v result=%q", handlerID, result)
	}
	var pstatus string
	if err := db.Table("products").Where("id = ?", pid).Select("status").Scan(&pstatus).Error; err != nil {
		t.Fatal(err)
	}
	if pstatus != constants.ProductStatusOnSale {
		t.Fatalf("product half-updated to %s (rollback failed)", pstatus)
	}

	// Recovery: remove the fault and retry the SAME still-pending report.
	if err := db.Callback().Update().Remove(cbName); err != nil {
		t.Fatalf("remove callback: %v", err)
	}
	rec = app.Do(http.MethodPost, "/api/v1/admin/reports/"+itoa(rid)+"/handle", app.TokenC,
		testsupport.JSON(map[string]any{"action": "remove", "note": "恢复后处理"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if err := db.Table("reports").Where("id = ?", rid).Select("status").Scan(&status).Error; err != nil {
		t.Fatal(err)
	}
	if status != constants.ReportStatusRemoved {
		t.Fatalf("after recovery status = %s, want removed", status)
	}
	if err := db.Table("products").Where("id = ?", pid).Select("status").Scan(&pstatus).Error; err != nil {
		t.Fatal(err)
	}
	if pstatus != constants.ProductStatusRemoved {
		t.Fatalf("after recovery product = %s, want removed", pstatus)
	}
}
