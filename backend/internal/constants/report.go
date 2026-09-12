package constants

// Report reason enum shared with the frontend (students pick one when reporting a product).
const (
	ReportReasonSpam       = "spam"       // 垃圾广告
	ReportReasonFraud      = "fraud"      // 欺诈/诈骗
	ReportReasonProhibited = "prohibited" // 违禁物品
	ReportReasonFakeInfo   = "fake_info"  // 虚假信息
	ReportReasonHarassment = "harassment" // 骚扰/不文明交易
	ReportReasonOther      = "other"      // 其他
)

// ReportReasons lists all selectable report reasons.
var ReportReasons = []string{
	ReportReasonSpam, ReportReasonFraud, ReportReasonProhibited,
	ReportReasonFakeInfo, ReportReasonHarassment, ReportReasonOther,
}

// IsReportReason reports whether the given reason is valid.
func IsReportReason(r string) bool {
	for _, v := range ReportReasons {
		if v == r {
			return true
		}
	}
	return false
}

// ReportStatus enum. Only one "pending" report may exist per product;
// handled reports end up "removed" (report upheld, product taken down)
// or "rejected" (report dismissed).
const (
	ReportStatusPending  = "pending"
	ReportStatusRemoved  = "removed"
	ReportStatusRejected = "rejected"
)

// ReportStatuses lists all report statuses.
var ReportStatuses = []string{
	ReportStatusPending, ReportStatusRemoved, ReportStatusRejected,
}

// IsReportStatus reports whether the given status is valid.
func IsReportStatus(s string) bool {
	for _, v := range ReportStatuses {
		if v == s {
			return true
		}
	}
	return false
}

// ReportAction enum chosen by the administrator when handling a report.
const (
	ReportActionRemove = "remove" // 下架商品，举报成立
	ReportActionReject = "reject" // 驳回举报
)

// ReportActions lists all administrator handling actions.
var ReportActions = []string{
	ReportActionRemove, ReportActionReject,
}

// IsReportAction reports whether the given action is valid.
func IsReportAction(a string) bool {
	for _, v := range ReportActions {
		if v == a {
			return true
		}
	}
	return false
}

// ReportReasonText returns the Chinese label of a report reason.
func ReportReasonText(r string) string {
	switch r {
	case ReportReasonSpam:
		return "垃圾广告"
	case ReportReasonFraud:
		return "欺诈/诈骗"
	case ReportReasonProhibited:
		return "违禁物品"
	case ReportReasonFakeInfo:
		return "虚假信息"
	case ReportReasonHarassment:
		return "骚扰/不文明交易"
	case ReportReasonOther:
		return "其他"
	default:
		return "未知"
	}
}

// ReportStatusText returns the Chinese label of a report status.
func ReportStatusText(s string) string {
	switch s {
	case ReportStatusPending:
		return "待处理"
	case ReportStatusRemoved:
		return "已下架（举报成立）"
	case ReportStatusRejected:
		return "已驳回"
	default:
		return "未知"
	}
}

// ReportActionText returns the Chinese label of an administrator action.
func ReportActionText(a string) string {
	switch a {
	case ReportActionRemove:
		return "下架商品"
	case ReportActionReject:
		return "驳回举报"
	default:
		return "未知"
	}
}
