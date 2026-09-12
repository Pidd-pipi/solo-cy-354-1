package model

import "time"

// Report is a student report against a product. At most one pending report
// may exist per product: PendingProductID mirrors ProductID while the report
// is pending (carrying a unique index) and is set to NULL after handling,
// so MySQL allows multiple handled rows but only one pending row per product.
type Report struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	ProductID        uint       `gorm:"index;not null" json:"product_id"`
	ReporterID       uint       `gorm:"index;not null" json:"reporter_id"`
	Reason           string     `gorm:"size:24;not null" json:"reason"`
	Detail           string     `gorm:"type:text" json:"detail"`
	Status           string     `gorm:"size:16;index;not null;default:pending" json:"status"`
	PendingProductID *uint      `gorm:"uniqueIndex:uniq_report_pending_product" json:"-"`
	HandlerID        *uint      `gorm:"index" json:"handler_id"`
	HandledAt        *time.Time `json:"handled_at"`
	// HandleResult holds the action label plus the administrator note
	// (e.g. "下架商品：<note>"). The note may be up to 200 characters, so the
	// column must be wider than a short enum-sized varchar.
	HandleResult string    `gorm:"size:512" json:"handle_result"`
	HandleNote   string    `gorm:"type:text" json:"handle_note"`
	CreatedAt    time.Time `json:"created_at"`
}
