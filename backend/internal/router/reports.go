package router

import (
	"github.com/gin-gonic/gin"
	"github.com/lp/campus-market/internal/handler"
)

// RegisterReportRoutes registers student report endpoints and the
// administrator-only report handling console.
func RegisterReportRoutes(
	g *gin.RouterGroup,
	h *handler.ReportHandler,
	auth, requireAdmin, apiLimiter gin.HandlerFunc,
) {
	// Student-facing: file a report and review my own report records.
	reports := g.Group("/reports", auth)
	{
		reports.POST("", apiLimiter, h.Create)
		reports.GET("/me", apiLimiter, h.ListMine)
	}
	// Admin-only: browse pending/handled reports and handle them.
	adminReports := g.Group("/admin/reports", auth, requireAdmin)
	{
		adminReports.GET("", apiLimiter, h.AdminList)
		adminReports.POST("/:id/handle", apiLimiter, h.Handle)
	}
}
