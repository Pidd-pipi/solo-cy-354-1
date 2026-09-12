package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lp/campus-market/internal/constants"
	"github.com/lp/campus-market/internal/dto"
	"github.com/lp/campus-market/internal/middleware"
	"github.com/lp/campus-market/internal/service"
	"github.com/lp/campus-market/internal/util"
)

// ReportHandler exposes product-report endpoints for students and admins.
type ReportHandler struct {
	svc    *service.ReportService
	logger *slog.Logger
}

// NewReportHandler wires the report handler dependencies.
func NewReportHandler(svc *service.ReportService, logger *slog.Logger) *ReportHandler {
	return &ReportHandler{svc: svc, logger: logger}
}

// Create handles POST /reports: a student reports a product.
func (h *ReportHandler) Create(c *gin.Context) {
	userID, err := middleware.CurrentUserID(c)
	if err != nil {
		util.Fail(c, http.StatusUnauthorized, constants.CodeUnauthorized, constants.MsgUnauthorized)
		return
	}
	var req dto.CreateReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, http.StatusBadRequest, constants.CodeValidation, constants.MsgValidationFailed)
		return
	}
	rp, err := h.svc.Create(c.Request.Context(), userID, &req)
	if err != nil {
		// Handler layer rewraps the service error again, including entity and
		// field names, per the project's layered error-propagation convention.
		c.Error(fmt.Errorf("report[product=%d][role=student] create handler: %w", req.ProductID, err))
		return
	}
	util.OK(c, rp)
}

// ListMine handles GET /reports/me: the reporter's own report records.
func (h *ReportHandler) ListMine(c *gin.Context) {
	userID, err := middleware.CurrentUserID(c)
	if err != nil {
		util.Fail(c, http.StatusUnauthorized, constants.CodeUnauthorized, constants.MsgUnauthorized)
		return
	}
	items, err := h.svc.ListMine(c.Request.Context(), userID)
	if err != nil {
		c.Error(err)
		return
	}
	util.OK(c, items)
}

// AdminList handles GET /admin/reports: pending/handled records for admins.
func (h *ReportHandler) AdminList(c *gin.Context) {
	adminID, err := middleware.CurrentUserID(c)
	if err != nil {
		util.Fail(c, http.StatusUnauthorized, constants.CodeUnauthorized, constants.MsgUnauthorized)
		return
	}
	status := c.Query("status")
	items, err := h.svc.AdminList(c.Request.Context(), adminID, status)
	if err != nil {
		c.Error(err)
		return
	}
	util.OK(c, items)
}

// Handle handles POST /admin/reports/:id/handle: remove the product or reject.
func (h *ReportHandler) Handle(c *gin.Context) {
	adminID, err := middleware.CurrentUserID(c)
	if err != nil {
		util.Fail(c, http.StatusUnauthorized, constants.CodeUnauthorized, constants.MsgUnauthorized)
		return
	}
	reportID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		util.Fail(c, http.StatusBadRequest, constants.CodeBadRequest, "举报ID不合法")
		return
	}
	var req dto.HandleReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, http.StatusBadRequest, constants.CodeValidation, constants.MsgValidationFailed)
		return
	}
	view, err := h.svc.Handle(c.Request.Context(), adminID, uint(reportID), &req)
	if err != nil {
		c.Error(fmt.Errorf("report[id=%d][role=admin] handle handler: %w", reportID, err))
		return
	}
	util.OK(c, view)
}
