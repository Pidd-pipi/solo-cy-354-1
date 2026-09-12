package repository

import (
	"context"

	"github.com/lp/campus-market/internal/dto"
	"github.com/lp/campus-market/internal/model"
	"github.com/lp/campus-market/internal/util"
	"gorm.io/gorm"
)

// ReportRepository persists product report rows.
type ReportRepository struct {
	db *gorm.DB
}

// NewReportRepository builds a ReportRepository.
func NewReportRepository(db *gorm.DB) *ReportRepository {
	return &ReportRepository{db: db}
}

// Transaction runs fn inside a database transaction for cross-repository writes
// (report handling updates both the report and the product atomically).
func (r *ReportRepository) Transaction(ctx context.Context, fn func(txCtx context.Context) error) error {
	return Transaction(ctx, r.db, fn)
}

// Create inserts a new report. PendingProductID mirrors ProductID so the
// unique index uniq_report_pending_product blocks a second pending report.
func (r *ReportRepository) Create(ctx context.Context, rp *model.Report) error {
	return db(ctx, r.db).Create(rp).Error
}

// FindPendingByProduct returns the pending report of a product, if any.
func (r *ReportRepository) FindPendingByProduct(ctx context.Context, productID uint) (*model.Report, error) {
	var rp model.Report
	err := db(ctx, r.db).
		Where("product_id = ? AND status = ?", productID, "pending").
		First(&rp).Error
	if err != nil {
		return nil, normalizeError(err)
	}
	return &rp, nil
}

// FindByID returns a report by id.
func (r *ReportRepository) FindByID(ctx context.Context, id uint) (*model.Report, error) {
	var rp model.Report
	err := db(ctx, r.db).First(&rp, id).Error
	if err != nil {
		return nil, normalizeError(err)
	}
	return &rp, nil
}

// ListByReporter returns reports submitted by a student, newest first.
func (r *ReportRepository) ListByReporter(ctx context.Context, reporterID uint) ([]dto.ReportView, error) {
	var views []dto.ReportView
	err := reportQuery(db(ctx, r.db)).
		Where("r.reporter_id = ?", reporterID).
		Order("r.created_at DESC").
		Find(&views).Error
	return views, err
}

// ListByStatus returns reports filtered by status (empty = all), newest first.
func (r *ReportRepository) ListByStatus(ctx context.Context, status string) ([]dto.ReportView, error) {
	var views []dto.ReportView
	q := reportQuery(db(ctx, r.db))
	if status != "" {
		q = q.Where("r.status = ?", status)
	}
	err := q.Order("r.status ASC, r.created_at DESC").Find(&views).Error
	return views, err
}

// Handle marks a report handled, clearing PendingProductID so a new report can
// be raised later, and records handler/time/result.
func (r *ReportRepository) Handle(ctx context.Context, id, handlerID uint, status, result, note string, handledAt interface{}) error {
	res := db(ctx, r.db).Model(&model.Report{}).Where("id = ? AND status = ?", id, "pending").
		Updates(map[string]interface{}{
			"status":             status,
			"pending_product_id": nil,
			"handler_id":         handlerID,
			"handled_at":         handledAt,
			"handle_result":      result,
			"handle_note":        note,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return util.ErrConcurrentUpdate
	}
	return nil
}

// reportQuery joins products/users to build the report list view.
func reportQuery(q *gorm.DB) *gorm.DB {
	return q.Table("reports AS r").
		Select(`r.id, r.product_id, p.title AS product_title, p.status AS product_status,
			r.reporter_id, ru.nickname AS reporter_name,
			r.reason, r.detail, r.status,
			r.handler_id, hu.nickname AS handler_name,
			r.handled_at, r.handle_result, r.handle_note, r.created_at`).
		Joins("LEFT JOIN products p ON p.id = r.product_id").
		Joins("LEFT JOIN users ru ON ru.id = r.reporter_id").
		Joins("LEFT JOIN users hu ON hu.id = r.handler_id")
}
