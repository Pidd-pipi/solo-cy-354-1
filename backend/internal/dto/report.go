package dto

import "time"

// CreateReportRequest is the payload for a student reporting a product.
type CreateReportRequest struct {
	ProductID uint   `json:"product_id" binding:"required"`
	Reason    string `json:"reason" binding:"required,oneof=spam fraud prohibited fake_info harassment other"`
	Detail    string `json:"detail" binding:"max=500"`
}

// HandleReportRequest is the administrator handling payload.
type HandleReportRequest struct {
	Action string `json:"action" binding:"required,oneof=remove reject"`
	Note   string `json:"note" binding:"max=200"`
}

// ListReportQuery filters the administrator report list by status.
type ListReportQuery struct {
	Status string `form:"status"`
	PageQuery
}

// ReportView is the joined report row returned to reporters and administrators.
type ReportView struct {
	ID            uint       `json:"id"`
	ProductID     uint       `json:"product_id"`
	ProductTitle  string     `json:"product_title"`
	ProductStatus string     `json:"product_status"`
	ReporterID    uint       `json:"reporter_id"`
	ReporterName  string     `json:"reporter_name"`
	Reason        string     `json:"reason"`
	Detail        string     `json:"detail"`
	Status        string     `json:"status"`
	HandlerID     *uint      `json:"handler_id"`
	HandlerName   string     `json:"handler_name"`
	HandledAt     *time.Time `json:"handled_at"`
	HandleResult  string     `json:"handle_result"`
	HandleNote    string     `json:"handle_note"`
	CreatedAt     time.Time  `json:"created_at"`
}
