package util

import (
	"fmt"
	"time"

	"github.com/lp/campus-market/internal/constants"
)

// FormatDateTime renders a time value using the shared display layout.
func FormatDateTime(t time.Time) string {
	return t.Format("2006-01-02 15:04")
}

// FormatDate renders a date-only display string.
func FormatDate(t time.Time) string {
	return t.Format("2006-01-02")
}

// FormatPrice renders a price with two decimals.
func FormatPrice(v float64) string {
	return fmt.Sprintf("¥%.2f", v)
}

// ProductStatusText maps a product status to its Chinese label.
func ProductStatusText(s string) string {
	return constants.ProductStatusText(s)
}

// TradeStatusText maps a trade status to its Chinese label.
func TradeStatusText(s string) string {
	return constants.TradeStatusText(s)
}

// RoleText maps a user role to its Chinese label.
func RoleText(role string) string {
	return constants.UserRoleText(role)
}

// CreditLevelText maps a credit score to a Chinese grade label.
func CreditLevelText(score int) string {
	return constants.CreditLevelText(score)
}

// CategoryText maps a product category to its Chinese label.
func CategoryText(c string) string {
	return constants.ProductCategoryText(c)
}

// ReportReasonText maps a report reason to its Chinese label.
func ReportReasonText(r string) string {
	return constants.ReportReasonText(r)
}

// ReportStatusText maps a report status to its Chinese label.
func ReportStatusText(s string) string {
	return constants.ReportStatusText(s)
}

// ReportActionText maps an administrator report action to its Chinese label.
func ReportActionText(a string) string {
	return constants.ReportActionText(a)
}
