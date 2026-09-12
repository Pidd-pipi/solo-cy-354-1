package integration

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/lp/campus-market/internal/constants"
)

// TestReportFullFlow exercises the complete report lifecycle over real HTTP
// routes against an in-memory MySQL-compatible database:
//  1. student B reports student A's product (reason + detail)
//  2. the same product keeps only ONE pending report (different reporter too)
//  3. seller cannot report own product; anonymous cannot report
//  4. admin lists pending records; student cannot access admin console (RBAC)
//  5. admin rejects a second product's report; rejection records handler/time/result
//  6. after rejection a new report is accepted again
//  7. on the first product admin chooses "remove": product goes removed;
//     orders and conversations are then blocked
//  8. reporter sees the final handling status in /reports/me
func TestReportFullFlow(t *testing.T) {
	h := newHarness(t)

	// product #1 belongs to student A (user 1); create a second on-sale product
	body, _ := json.Marshal(map[string]any{
		"title": "二手台灯", "description": "暖光", "price": 20,
		"category": "daily", "condition": "全新", "campus": "东校区", "trade_location": "南门",
	})
	rec := h.do(http.MethodPost, "/api/v1/products", h.tokenA, body)
	var secondProduct struct {
		ID uint `json:"id"`
	}
	_ = json.Unmarshal(h.mustData(rec, "publish second product"), &secondProduct)
	if secondProduct.ID == 0 {
		t.Fatalf("second product id missing")
	}

	// 1. B reports product #1 with reason + detail
	body, _ = json.Marshal(map[string]any{"product_id": 1, "reason": "fraud", "detail": "要求先转账再交货"})
	rec = h.do(http.MethodPost, "/api/v1/reports", h.tokenB, body)
	var report struct {
		ID     uint   `json:"id"`
		Status string `json:"status"`
		Reason string `json:"reason"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(h.mustData(rec, "create report"), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.Status != constants.ReportStatusPending {
		t.Fatalf("new report status = %s, want pending", report.Status)
	}

	// 2a. same reporter reporting again -> conflict (one pending report per product)
	body, _ = json.Marshal(map[string]any{"product_id": 1, "reason": "spam"})
	h.assertBusinessError(h.do(http.MethodPost, "/api/v1/reports", h.tokenB, body), constants.CodeConflict, "duplicate same reporter")

	// 2b. a DIFFERENT reporter is also blocked while one is pending: there is no
	//     third seeded student, so register one on the spot.
	body, _ = json.Marshal(map[string]any{"phone": "13900000001", "password": "123456", "nickname": "学生丙同学", "campus": "南校区"})
	_ = h.mustData(h.do(http.MethodPost, "/api/v1/users/register", "", body), "register student C")
	cToken := h.login("13900000001", "123456")
	body, _ = json.Marshal(map[string]any{"product_id": 1, "reason": "other", "detail": "我也觉得有问题"})
	h.assertBusinessError(h.do(http.MethodPost, "/api/v1/reports", cToken, body), constants.CodeConflict, "duplicate other reporter")

	// 3a. seller cannot report own product
	body, _ = json.Marshal(map[string]any{"product_id": 1, "reason": "other"})
	h.assertBusinessError(h.do(http.MethodPost, "/api/v1/reports", h.tokenA, body), constants.CodeBadRequest, "seller self report")

	// 3b. anonymous report -> 401 unauthorized
	rec = h.do(http.MethodPost, "/api/v1/reports", "", body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous report status = %d, want 401", rec.Code)
	}

	// 3c. invalid reason rejected by validation
	body, _ = json.Marshal(map[string]any{"product_id": 1, "reason": "hacker"})
	rec = h.do(http.MethodPost, "/api/v1/reports", h.tokenB, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid reason status = %d, want 400", rec.Code)
	}

	// B reports the second product too (this one will be rejected later)
	body, _ = json.Marshal(map[string]any{"product_id": secondProduct.ID, "reason": "fake_info", "detail": "描述与实物不符"})
	rec = h.do(http.MethodPost, "/api/v1/reports", h.tokenB, body)
	var secondReport struct {
		ID uint `json:"id"`
	}
	_ = json.Unmarshal(h.mustData(rec, "report second product"), &secondReport)

	// 4a. student cannot open the admin console (RBAC)
	rec = h.do(http.MethodGet, "/api/v1/admin/reports?status=pending", h.tokenB, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("student admin list status = %d, want 403", rec.Code)
	}

	// 4b. admin lists pending records: two rows
	rec = h.do(http.MethodGet, "/api/v1/admin/reports?status=pending", h.adminTo, nil)
	var pending []map[string]any
	if err := json.Unmarshal(h.mustData(rec, "admin pending list"), &pending); err != nil {
		t.Fatalf("decode pending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending count = %d, want 2", len(pending))
	}

	// 5. admin rejects the second report; handler/time/result must be recorded
	body, _ = json.Marshal(map[string]any{"action": "reject", "note": "证据不足"})
	rec = h.do(http.MethodPost, "/api/v1/admin/reports/"+itoa(secondReport.ID)+"/handle", h.adminTo, body)
	var handled struct {
		Status       string `json:"status"`
		HandlerName  string `json:"handler_name"`
		HandleResult string `json:"handle_result"`
		HandledAt    string `json:"handled_at"`
	}
	if err := json.Unmarshal(h.mustData(rec, "admin reject"), &handled); err != nil {
		t.Fatalf("decode handled: %v", err)
	}
	if handled.Status != constants.ReportStatusRejected || handled.HandlerName != "管理员" || handled.HandledAt == "" || handled.HandleResult == "" {
		t.Fatalf("rejection not recorded correctly: %+v", handled)
	}
	// handling twice -> conflict
	h.assertBusinessError(h.do(http.MethodPost, "/api/v1/admin/reports/"+itoa(secondReport.ID)+"/handle", h.adminTo, body), constants.CodeConflict, "double handle")

	// 6. after rejection the second product accepts a new pending report again
	body, _ = json.Marshal(map[string]any{"product_id": secondProduct.ID, "reason": "spam"})
	rec = h.do(http.MethodPost, "/api/v1/reports", cToken, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("new report after rejection status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	// 7. admin upholds the first report: product taken down
	body, _ = json.Marshal(map[string]any{"action": "remove", "note": "确认欺诈"})
	rec = h.do(http.MethodPost, "/api/v1/admin/reports/"+itoa(report.ID)+"/handle", h.adminTo, body)
	var upheld struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(h.mustData(rec, "admin remove"), &upheld)
	if upheld.Status != constants.ReportStatusRemoved {
		t.Fatalf("handled status = %s, want removed", upheld.Status)
	}

	// 7a. product status is now removed
	rec = h.do(http.MethodGet, "/api/v1/products/1", "", nil)
	var product struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(h.mustData(rec, "get removed product"), &product)
	if product.Status != constants.ProductStatusRemoved {
		t.Fatalf("product status = %s, want removed", product.Status)
	}

	// 7b. ordering the removed product is blocked
	body, _ = json.Marshal(map[string]any{"product_id": 1})
	h.assertBusinessError(h.do(http.MethodPost, "/api/v1/trade-orders", h.tokenB, body), constants.CodeConflict, "order removed product")

	// 7c. starting a new private conversation about it is blocked
	body, _ = json.Marshal(map[string]any{"product_id": 1})
	h.assertBusinessError(h.do(http.MethodPost, "/api/v1/conversations", h.tokenB, body), constants.CodeConflict, "chat removed product")

	// 8. reporter's personal records show both reports with final statuses
	rec = h.do(http.MethodGet, "/api/v1/reports/me", h.tokenB, nil)
	var mine []map[string]any
	if err := json.Unmarshal(h.mustData(rec, "my reports"), &mine); err != nil {
		t.Fatalf("decode my reports: %v", err)
	}
	statusByProduct := map[float64]string{}
	for _, r := range mine {
		statusByProduct[r["product_id"].(float64)] = r["status"].(string)
		if title, ok := r["product_title"].(string); !ok || title == "" {
			t.Fatalf("report view missing product_title: %v", r)
		}
	}
	if statusByProduct[1] != constants.ReportStatusRemoved {
		t.Fatalf("product 1 report status = %q, want removed", statusByProduct[1])
	}
	if statusByProduct[float64(secondProduct.ID)] != constants.ReportStatusRejected {
		t.Fatalf("product 2 report status = %q, want rejected", statusByProduct[float64(secondProduct.ID)])
	}

	// 9. admin default console (no filter) shows pending + handled records
	rec = h.do(http.MethodGet, "/api/v1/admin/reports", h.adminTo, nil)
	_ = h.mustData(rec, "admin all list")
}

func itoa(n uint) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
