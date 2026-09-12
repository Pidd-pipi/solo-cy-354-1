package service

import (
	"context"
	"testing"
	"time"

	"github.com/lp/campus-market/internal/constants"
	"github.com/lp/campus-market/internal/dto"
	"github.com/lp/campus-market/internal/model"
	"github.com/lp/campus-market/internal/util"
	"log/slog"
)

// fakeReportRepo is an in-memory implementation of the reportStore contract.
type fakeReportRepo struct {
	reports map[uint]*model.Report
	nextID  uint
}

func newFakeReportRepo() *fakeReportRepo {
	return &fakeReportRepo{reports: map[uint]*model.Report{}, nextID: 1}
}

func (f *fakeReportRepo) Create(_ context.Context, rp *model.Report) error {
	for _, r := range f.reports {
		if r.Status == constants.ReportStatusPending && r.ProductID == rp.ProductID {
			return util.ErrDuplicate
		}
	}
	rp.ID = f.nextID
	f.nextID++
	cp := *rp
	f.reports[rp.ID] = &cp
	return nil
}

func (f *fakeReportRepo) FindPendingByProduct(_ context.Context, productID uint) (*model.Report, error) {
	for _, r := range f.reports {
		if r.Status == constants.ReportStatusPending && r.ProductID == productID {
			cp := *r
			return &cp, nil
		}
	}
	return nil, util.ErrNotFound
}

func (f *fakeReportRepo) FindByID(_ context.Context, id uint) (*model.Report, error) {
	if r, ok := f.reports[id]; ok {
		cp := *r
		return &cp, nil
	}
	return nil, util.ErrNotFound
}

func (f *fakeReportRepo) ListByReporter(_ context.Context, reporterID uint) ([]dto.ReportView, error) {
	var out []dto.ReportView
	for _, r := range f.reports {
		if r.ReporterID == reporterID {
			out = append(out, dto.ReportView{ID: r.ID, ProductID: r.ProductID, ReporterID: r.ReporterID, Reason: r.Reason, Detail: r.Detail, Status: r.Status, CreatedAt: r.CreatedAt})
		}
	}
	return out, nil
}

func (f *fakeReportRepo) ListByStatus(_ context.Context, status string) ([]dto.ReportView, error) {
	var out []dto.ReportView
	for _, r := range f.reports {
		if status != "" && r.Status != status {
			continue
		}
		out = append(out, dto.ReportView{ID: r.ID, ProductID: r.ProductID, ReporterID: r.ReporterID, Status: r.Status, HandlerID: r.HandlerID, HandledAt: r.HandledAt, HandleResult: r.HandleResult})
	}
	return out, nil
}

func (f *fakeReportRepo) Handle(_ context.Context, id, handlerID uint, status, result, note string, handledAt interface{}) error {
	r, ok := f.reports[id]
	if !ok || r.Status != constants.ReportStatusPending {
		return util.ErrConcurrentUpdate
	}
	r.Status = status
	r.PendingProductID = nil
	r.HandlerID = &handlerID
	r.HandleResult = result
	r.HandleNote = note
	if t, ok := handledAt.(time.Time); ok {
		r.HandledAt = &t
	}
	return nil
}

func (f *fakeReportRepo) Transaction(ctx context.Context, fn func(txCtx context.Context) error) error {
	return fn(ctx)
}

// seedProductForReport creates an on-sale product and returns its id.
func seedProductForReport(t *testing.T, products *fakeProductRepo, sellerID uint) uint {
	t.Helper()
	p := &model.Product{SellerID: sellerID, Title: "测试商品", Price: 9.9, Category: constants.ProductCategoryBooks, Status: constants.ProductStatusOnSale}
	if err := products.Create(context.Background(), p); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	return p.ID
}

func TestReportServiceCreate(t *testing.T) {
	tests := []struct {
		name       string
		reporterID uint
		reason     string
		seedFirst  bool
		wantErr    bool
	}{
		{name: "valid report", reporterID: 2, reason: constants.ReportReasonFraud, wantErr: false},
		{name: "invalid reason", reporterID: 2, reason: "hacker", wantErr: true},
		{name: "seller reports own product", reporterID: 1, reason: constants.ReportReasonSpam, wantErr: true},
		{name: "duplicate pending report", reporterID: 3, reason: constants.ReportReasonSpam, seedFirst: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			products := newFakeProductRepo()
			reports := newFakeReportRepo()
			svc := NewReportService(reports, products, slog.Default())
			pid := seedProductForReport(t, products, 1)
			if tt.seedFirst {
				if _, err := svc.Create(context.Background(), 2, &dto.CreateReportRequest{ProductID: pid, Reason: constants.ReportReasonOther, Detail: "先举报一条"}); err != nil {
					t.Fatalf("seed report: %v", err)
				}
			}
			_, err := svc.Create(context.Background(), tt.reporterID, &dto.CreateReportRequest{ProductID: pid, Reason: tt.reason, Detail: "补充说明"})
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestReportServiceCannotReportRemovedProduct(t *testing.T) {
	products := newFakeProductRepo()
	reports := newFakeReportRepo()
	svc := NewReportService(reports, products, slog.Default())
	pid := seedProductForReport(t, products, 1)
	if err := products.UpdateStatus(context.Background(), pid, constants.ProductStatusRemoved); err != nil {
		t.Fatalf("remove product: %v", err)
	}
	if _, err := svc.Create(context.Background(), 2, &dto.CreateReportRequest{ProductID: pid, Reason: constants.ReportReasonFraud}); err == nil {
		t.Fatalf("expected error reporting removed product")
	}
}

func TestReportServiceHandle(t *testing.T) {
	tests := []struct {
		name             string
		action           string
		wantProductDown  bool
		wantReportStatus string
	}{
		{name: "uphold: remove product", action: constants.ReportActionRemove, wantProductDown: true, wantReportStatus: constants.ReportStatusRemoved},
		{name: "reject report", action: constants.ReportActionReject, wantProductDown: false, wantReportStatus: constants.ReportStatusRejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			products := newFakeProductRepo()
			reports := newFakeReportRepo()
			svc := NewReportService(reports, products, slog.Default())
			pid := seedProductForReport(t, products, 1)
			rp, err := svc.Create(context.Background(), 2, &dto.CreateReportRequest{ProductID: pid, Reason: constants.ReportReasonFraud})
			if err != nil {
				t.Fatalf("create report: %v", err)
			}
			view, err := svc.Handle(context.Background(), 99, rp.ID, &dto.HandleReportRequest{Action: tt.action, Note: "处理备注"})
			if err != nil {
				t.Fatalf("handle report: %v", err)
			}
			if view.Status != tt.wantReportStatus {
				t.Fatalf("report status = %s, want %s", view.Status, tt.wantReportStatus)
			}
			gotProduct, _ := products.FindByID(context.Background(), pid)
			isDown := gotProduct.Status == constants.ProductStatusRemoved
			if isDown != tt.wantProductDown {
				t.Fatalf("product removed = %v, want %v (status=%s)", isDown, tt.wantProductDown, gotProduct.Status)
			}
			// handler/time/result must be recorded
			stored := reports.reports[rp.ID]
			if stored.HandlerID == nil || *stored.HandlerID != 99 {
				t.Fatalf("handler id not recorded")
			}
			if stored.HandledAt == nil {
				t.Fatalf("handled at not recorded")
			}
			if stored.HandleResult == "" {
				t.Fatalf("handle result not recorded")
			}
			// handling the same report twice must fail
			if _, err := svc.Handle(context.Background(), 99, rp.ID, &dto.HandleReportRequest{Action: tt.action}); err == nil {
				t.Fatalf("expected error on duplicate handling")
			}
		})
	}
}

func TestReportServiceNewReportAllowedAfterHandled(t *testing.T) {
	products := newFakeProductRepo()
	reports := newFakeReportRepo()
	svc := NewReportService(reports, products, slog.Default())
	pid := seedProductForReport(t, products, 1)
	rp, _ := svc.Create(context.Background(), 2, &dto.CreateReportRequest{ProductID: pid, Reason: constants.ReportReasonFraud})
	if _, err := svc.Handle(context.Background(), 99, rp.ID, &dto.HandleReportRequest{Action: constants.ReportActionReject}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	// After rejection a new (different) pending report is allowed again.
	if _, err := svc.Create(context.Background(), 3, &dto.CreateReportRequest{ProductID: pid, Reason: constants.ReportReasonOther}); err != nil {
		t.Fatalf("expected new report allowed after rejection, got %v", err)
	}
}
