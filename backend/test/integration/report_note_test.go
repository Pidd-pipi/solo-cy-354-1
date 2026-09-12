package integration

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/lp/campus-market/internal/constants"
	"gorm.io/gorm"
)

// publishAndReport creates a fresh on-sale product (by student A) and files a
// pending report against it (by student B), returning both ids.
func (h *harness) publishAndReport(t *testing.T, reason string) (uint, uint) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"title": "边界测试商品", "description": "d", "price": 6,
		"category": "books", "condition": "全新", "campus": "东校区", "trade_location": "东门",
	})
	rec := h.do(http.MethodPost, "/api/v1/products", h.tokenA, body)
	var p struct {
		ID uint `json:"id"`
	}
	if err := json.Unmarshal(h.mustData(rec, "publish"), &p); err != nil || p.ID == 0 {
		t.Fatalf("publish product failed: %s", rec.Body.String())
	}
	body, _ = json.Marshal(map[string]any{"product_id": p.ID, "reason": reason})
	rec = h.do(http.MethodPost, "/api/v1/reports", h.tokenB, body)
	var r struct {
		ID uint `json:"id"`
	}
	if err := json.Unmarshal(h.mustData(rec, "report"), &r); err != nil || r.ID == 0 {
		t.Fatalf("create report failed: %s", rec.Body.String())
	}
	return p.ID, r.ID
}

// TestHandleReportNoteBoundary verifies that the longest note allowed by the
// UI (200 characters, including multibyte Chinese) is fully saved in
// handle_result even when it far exceeds the old 128-char column limit, and
// that 201 characters is rejected (HTTP 400) without touching the report.
func TestHandleReportNoteBoundary(t *testing.T) {
	h := newHarness(t)

	// ---- 200 Chinese characters: exactly at the limit, action = remove ----
	productID, reportID := h.publishAndReport(t, "fraud")
	note200 := strings.Repeat("备", 200)
	body, _ := json.Marshal(map[string]any{"action": "remove", "note": note200})
	rec := h.do(http.MethodPost, "/api/v1/admin/reports/"+itoa(reportID)+"/handle", h.adminTo, body)
	var handled struct {
		Status       string `json:"status"`
		HandleResult string `json:"handle_result"`
		HandleNote   string `json:"handle_note"`
	}
	if err := json.Unmarshal(h.mustData(rec, "handle 200-char note"), &handled); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if handled.Status != constants.ReportStatusRemoved {
		t.Fatalf("status = %s, want removed", handled.Status)
	}
	wantResult := "下架商品：" + note200
	if handled.HandleResult != wantResult {
		t.Fatalf("handle_result length = %d runes, want %d (full note not saved)",
			len([]rune(handled.HandleResult)), len([]rune(wantResult)))
	}
	if len([]rune(handled.HandleResult)) < 128 {
		t.Fatalf("expected handle_result to exceed old 128-char column limit")
	}
	if handled.HandleNote != note200 {
		t.Fatalf("handle_note not saved verbatim")
	}
	// product is taken down
	rec = h.do(http.MethodGet, "/api/v1/products/"+itoa(productID), "", nil)
	var p struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(h.mustData(rec, "product"), &p)
	if p.Status != constants.ProductStatusRemoved {
		t.Fatalf("product status = %s, want removed", p.Status)
	}

	// ---- second product: reject with a 200-char note also survives ----
	productID2, reportID2 := h.publishAndReport(t, "spam")
	body, _ = json.Marshal(map[string]any{"action": "reject", "note": note200})
	rec = h.do(http.MethodPost, "/api/v1/admin/reports/"+itoa(reportID2)+"/handle", h.adminTo, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("reject with 200-char note status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = h.do(http.MethodGet, "/api/v1/products/"+itoa(productID2), "", nil)
	_ = json.Unmarshal(h.mustData(rec, "product2"), &p)
	if p.Status != constants.ProductStatusOnSale {
		t.Fatalf("rejected report must leave product on_sale, got %s", p.Status)
	}

	// ---- 201 characters: rejected with 400 and report stays pending ----
	_, reportID3 := h.publishAndReport(t, "other")
	note201 := strings.Repeat("说", 201)
	body, _ = json.Marshal(map[string]any{"action": "remove", "note": note201})
	rec = h.do(http.MethodPost, "/api/v1/admin/reports/"+itoa(reportID3)+"/handle", h.adminTo, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("201-char note status = %d, want 400", rec.Code)
	}
	rec = h.do(http.MethodGet, "/api/v1/admin/reports?status=pending", h.adminTo, nil)
	var pending []struct {
		ID uint `json:"id"`
	}
	_ = json.Unmarshal(h.mustData(rec, "pending list"), &pending)
	found := false
	for _, r := range pending {
		if r.ID == reportID3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("report with oversized note must remain pending, pending list: %+v", pending)
	}
}

// TestHandleReportRollbackOnFailure proves that if the report update fails
// after the product status was already changed inside the same transaction,
// the whole unit rolls back: the report stays pending (with
// pending_product_id intact, no handler fields) and the product stays on sale.
//
// This runs on an in-memory SQLite database because the test MySQL engine
// (go-mysql-server memory provider) applies writes immediately and does not
// honor ROLLBACK; real MySQL/InnoDB used in production does.
func TestHandleReportRollbackOnFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&_pragma=foreign_keys(1)"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	h := newHarnessWithDB(t, db)
	productID, reportID := h.publishAndReport(t, "prohibited")

	// Inject a failure into the report UPDATE for this DB. The service updates
	// the product status FIRST inside the transaction, so a failure here proves
	// that rollback also undoes the already-executed product update.
	const cbName = "test_inject_report_update_failure"
	err = h.db.Callback().Update().Before("gorm:update").Register(cbName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "reports" {
			tx.AddError(errors.New("injected report update failure"))
		}
	})
	if err != nil {
		t.Fatalf("register callback: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"action": "remove", "note": "应该整体回滚"})
	rec := h.do(http.MethodPost, "/api/v1/admin/reports/"+itoa(reportID)+"/handle", h.adminTo, body)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("failed handle status = %d, want 500 body=%s", rec.Code, rec.Body.String())
	}

	// No half-update: product is still on sale ...
	var productStatus string
	if err := h.db.Table("products").Where("id = ?", productID).Select("status").Scan(&productStatus).Error; err != nil {
		t.Fatalf("query product: %v", err)
	}
	if productStatus != constants.ProductStatusOnSale {
		t.Fatalf("product half-updated to %s, want on_sale (rollback failed)", productStatus)
	}
	// ... and the report is still pending with its unique-guard column intact.
	var row struct {
		Status           string
		PendingProductID *int64
		HandlerID        *int64
		HandleResult     string
	}
	if err := h.db.Table("reports").Where("id = ?", reportID).
		Select("status, pending_product_id, handler_id, handle_result").
		Scan(&row).Error; err != nil {
		t.Fatalf("query report: %v", err)
	}
	if row.Status != constants.ReportStatusPending {
		t.Fatalf("report half-updated to %s, want pending (rollback failed)", row.Status)
	}
	if row.PendingProductID == nil || *row.PendingProductID != int64(productID) {
		t.Fatalf("pending_product_id not restored: got %v", row.PendingProductID)
	}
	if row.HandlerID != nil || row.HandleResult != "" {
		t.Fatalf("handler fields must be untouched after rollback: %+v", row)
	}

	// Remove the injected fault; the still-pending report can then be handled.
	if err := h.db.Callback().Update().Remove(cbName); err != nil {
		t.Fatalf("remove callback: %v", err)
	}
	rec = h.do(http.MethodPost, "/api/v1/admin/reports/"+itoa(reportID)+"/handle", h.adminTo, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("retry handle after recovery status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := h.db.Table("products").Where("id = ?", productID).Select("status").Scan(&productStatus).Error; err != nil {
		t.Fatalf("requery product: %v", err)
	}
	if productStatus != constants.ProductStatusRemoved {
		t.Fatalf("after recovery product = %s, want removed", productStatus)
	}
}
