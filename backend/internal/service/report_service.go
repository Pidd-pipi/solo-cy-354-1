package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lp/campus-market/internal/constants"
	"github.com/lp/campus-market/internal/dto"
	"github.com/lp/campus-market/internal/model"
	"github.com/lp/campus-market/internal/util"
	"gorm.io/gorm"
)

// reportStore is the data access contract for report rows.
type reportStore interface {
	Create(ctx context.Context, rp *model.Report) error
	FindPendingByProduct(ctx context.Context, productID uint) (*model.Report, error)
	FindByID(ctx context.Context, id uint) (*model.Report, error)
	ListByReporter(ctx context.Context, reporterID uint) ([]dto.ReportView, error)
	ListByStatus(ctx context.Context, status string) ([]dto.ReportView, error)
	Handle(ctx context.Context, id, handlerID uint, status, result, note string, handledAt interface{}) error
	Transaction(ctx context.Context, fn func(txCtx context.Context) error) error
}

// reportProductStore is the product access contract used by report handling.
type reportProductStore interface {
	FindByID(ctx context.Context, id uint) (*model.Product, error)
	UpdateStatus(ctx context.Context, id uint, status string) error
}

// ReportService manages student product reports and administrator handling.
type ReportService struct {
	reports  reportStore
	products reportProductStore
	logger   *slog.Logger
}

// NewReportService wires the report service dependencies.
func NewReportService(reports reportStore, products reportProductStore, logger *slog.Logger) *ReportService {
	return &ReportService{reports: reports, products: products, logger: logger}
}

// Create lets a logged-in student file a report against a product. Only one
// pending report is kept per product; the seller and removed products cannot
// be reported.
func (s *ReportService) Create(ctx context.Context, reporterID uint, req *dto.CreateReportRequest) (*model.Report, error) {
	if !constants.IsReportReason(req.Reason) {
		return nil, util.NewAppError(400, constants.CodeValidation, "举报原因不合法", nil)
	}
	product, err := s.products.FindByID(ctx, req.ProductID)
	if err != nil {
		return nil, util.WrapAppError(fmt.Errorf("report[product=%d] product lookup: %w", req.ProductID, err), 404, constants.CodeNotFound, constants.MsgReportTarget)
	}
	if product.SellerID == reporterID {
		return nil, util.NewAppError(400, constants.CodeBadRequest, constants.MsgReportOwnProduct, nil)
	}
	if product.Status == constants.ProductStatusRemoved {
		return nil, util.NewAppError(409, constants.CodeConflict, constants.MsgReportProductRemoved, nil)
	}
	if existing, err := s.reports.FindPendingByProduct(ctx, req.ProductID); err == nil && existing != nil {
		s.logger.Info(fmt.Sprintf(constants.LogReportDuplicateRejected, req.ProductID, reporterID))
		return nil, util.NewAppError(409, constants.CodeConflict, constants.MsgReportPendingExists, nil)
	} else if err != nil && !errors.Is(err, util.ErrNotFound) {
		return nil, util.WrapAppError(fmt.Errorf("report[product=%d] pending lookup: %w", req.ProductID, err), 500, constants.CodeInternalError, constants.MsgInternalError)
	}
	rp := &model.Report{
		ProductID: req.ProductID, ReporterID: reporterID,
		Reason: req.Reason, Detail: strings.TrimSpace(req.Detail),
		Status: constants.ReportStatusPending, PendingProductID: &req.ProductID,
	}
	if err := s.reports.Create(ctx, rp); err != nil {
		if isDuplicateKey(err) {
			s.logger.Info(fmt.Sprintf(constants.LogReportDuplicateRejected, req.ProductID, reporterID))
			return nil, util.NewAppError(409, constants.CodeConflict, constants.MsgReportPendingExists, nil)
		}
		s.logger.Error(fmt.Sprintf(constants.LogReportCreateFailed, req.ProductID, reporterID, err))
		return nil, util.WrapAppError(fmt.Errorf("report[product=%d reporter=%d] create: %w", req.ProductID, reporterID, err), 500, constants.CodeInternalError, constants.MsgInternalError)
	}
	s.logger.Info(fmt.Sprintf(constants.LogReportCreateSuccess, rp.ID, req.ProductID, reporterID, req.Reason))
	return rp, nil
}

// ListMine returns the reporter's own reports with handling status.
func (s *ReportService) ListMine(ctx context.Context, reporterID uint) ([]dto.ReportView, error) {
	items, err := s.reports.ListByReporter(ctx, reporterID)
	if err != nil {
		return nil, util.WrapAppError(fmt.Errorf("report[reporter=%d] list: %w", reporterID, err), 500, constants.CodeInternalError, constants.MsgInternalError)
	}
	s.logger.Info(fmt.Sprintf(constants.LogReportListSuccess, "reporter", "mine", len(items)))
	return items, nil
}

// AdminList returns reports filtered by status for the administrator console.
func (s *ReportService) AdminList(ctx context.Context, adminID uint, status string) ([]dto.ReportView, error) {
	if status != "" && !constants.IsReportStatus(status) {
		return nil, util.NewAppError(400, constants.CodeValidation, "举报状态不合法", nil)
	}
	items, err := s.reports.ListByStatus(ctx, status)
	if err != nil {
		return nil, util.WrapAppError(fmt.Errorf("report[admin=%d] list: %w", adminID, err), 500, constants.CodeInternalError, constants.MsgInternalError)
	}
	s.logger.Info(fmt.Sprintf(constants.LogReportListSuccess, "admin", status, len(items)))
	return items, nil
}

// Handle processes a pending report: "remove" takes the product down and
// upholds the report; "reject" dismisses it. The handler, time and result are
// recorded atomically with the product status change.
func (s *ReportService) Handle(ctx context.Context, adminID uint, reportID uint, req *dto.HandleReportRequest) (*dto.ReportView, error) {
	if !constants.IsReportAction(req.Action) {
		return nil, util.NewAppError(400, constants.CodeValidation, "处理方式不合法", nil)
	}
	// The note is appended to HandleResult ("下架商品：<note>"); guard the
	// length at the service layer too so an oversized note can never reach
	// the database and fail mid-transaction.
	if len([]rune(strings.TrimSpace(req.Note))) > 200 {
		return nil, util.NewAppError(400, constants.CodeValidation, "处理备注最多200字", nil)
	}
	rp, err := s.reports.FindByID(ctx, reportID)
	if err != nil {
		return nil, util.WrapAppError(fmt.Errorf("report[id=%d] handle find: %w", reportID, err), 404, constants.CodeNotFound, constants.MsgReportNotFound)
	}
	if rp.Status != constants.ReportStatusPending {
		return nil, util.NewAppError(409, constants.CodeConflict, constants.MsgReportAlreadyHandled, nil)
	}
	reportStatus := constants.ReportStatusRejected
	result := constants.ReportActionText(req.Action)
	if req.Note != "" {
		result = fmt.Sprintf("%s：%s", result, strings.TrimSpace(req.Note))
	}
	now := time.Now()
	err = s.reports.Transaction(ctx, func(txCtx context.Context) error {
		if req.Action == constants.ReportActionRemove {
			reportStatus = constants.ReportStatusRemoved
			if err := s.products.UpdateStatus(txCtx, rp.ProductID, constants.ProductStatusRemoved); err != nil {
				return fmt.Errorf("report[id=%d] take product %d down: %w", reportID, rp.ProductID, err)
			}
		}
		if err := s.reports.Handle(txCtx, reportID, adminID, reportStatus, result, strings.TrimSpace(req.Note), now); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, util.ErrConcurrentUpdate) {
			return nil, util.NewAppError(409, constants.CodeConflict, constants.MsgReportAlreadyHandled, nil)
		}
		s.logger.Error(fmt.Sprintf(constants.LogReportHandleFailed, reportID, adminID, err))
		return nil, util.WrapAppError(fmt.Errorf("report[id=%d] handle: %w", reportID, err), 500, constants.CodeInternalError, constants.MsgInternalError)
	}
	s.logger.Info(fmt.Sprintf(constants.LogReportHandleSuccess, reportID, req.Action, adminID, rp.ProductID))
	views, err := s.reports.ListByStatus(ctx, reportStatus)
	if err != nil {
		// The write already succeeded; surface the updated report id without masking it.
		return &dto.ReportView{ID: reportID, ProductID: rp.ProductID, Status: reportStatus}, nil
	}
	for i := range views {
		if views[i].ID == reportID {
			return &views[i], nil
		}
	}
	return &dto.ReportView{ID: reportID, ProductID: rp.ProductID, Status: reportStatus}, nil
}

// isDuplicateKey reports whether err is a MySQL unique-constraint violation
// (GORM translates it to ErrDuplicatedKey; the driver string is kept as fallback).
func isDuplicateKey(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	return err != nil && strings.Contains(err.Error(), "Error 1062")
}
