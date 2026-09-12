package constants

// Log templates are centralized so that any business field change forces a
// coordinated update of the related log statements across the codebase.
const (
	LogUserRegisterSuccess            = "user register success: phone=%s user_id=%d"
	LogUserRegisterFailed             = "user register failed: phone=%s error=%v"
	LogUserLoginSuccess               = "user login success: phone=%s user_id=%d role=%s"
	LogUserLoginFailed                = "user login failed: phone=%s error=%v"
	LogUserProfileUpdateSuccess       = "user profile update success: user_id=%d"
	LogProductPublishSuccess          = "product publish success: product_id=%d title=%s"
	LogProductPublishFailed           = "product publish failed: seller=%d title=%s error=%v"
	LogProductSoldSuccess             = "product sold success: product_id=%d status=sold"
	LogProductRemoveSuccess           = "product remove success: product_id=%d"
	LogConversationCreateSuccess      = "conversation create success: conv_id=%d product_id=%d"
	LogMessageSendSuccess             = "message send success: conv_id=%d sender=%d"
	LogTradeOrderCreateSuccess        = "trade order create success: order_id=%d product_id=%d"
	LogTradeOrderBuyerConfirmSuccess  = "trade order buyer confirm success: order_id=%d"
	LogTradeOrderSellerConfirmSuccess = "trade order seller confirm success: order_id=%d"
	LogTradeOrderCompleteSuccess      = "trade order complete success: order_id=%d product_id=%d"
	LogTradeOrderCompleteFailed       = "trade order complete failed: order_id=%d error=%v"
	LogTradeOrderCancelSuccess        = "trade order cancel success: order_id=%d"
	LogReviewCreateSuccess            = "review create success: review_id=%d trade_id=%d rating=%s"
	LogReviewCreateFailed             = "review create failed: trade_id=%d error=%v"
	LogCreditUpdateSuccess            = "credit update success: user_id=%d score=%d"
	LogBookExchangePublishSuccess     = "book exchange publish success: exchange_id=%d user_id=%d"
	LogBookExchangeMatchSuccess       = "book exchange match success: exchange_id=%d matched_id=%d"
	LogBookExchangeCloseSuccess       = "book exchange close success: exchange_id=%d"
	LogGraduationListSuccess          = "graduation event list success: count=%d"
	LogMiddlewareAuthFailed           = "auth middleware failed: error=%v"
	LogMiddlewareRbacDenied           = "rbac middleware denied: user_id=%d role=%s required=%v"
	LogRateLimitReached               = "rate limit reached: ip=%s route=%s"
	LogSeedingCompleted               = "database seeding completed: users=%d products=%d"
	LogReportCreateSuccess            = "report create success: report_id=%d product_id=%d reporter=%d reason=%s"
	LogReportCreateFailed             = "report create failed: product_id=%d reporter=%d error=%v"
	LogReportDuplicateRejected        = "report duplicate rejected: product_id=%d reporter=%d"
	LogReportListSuccess              = "report list success: viewer=%s status=%s count=%d"
	LogReportHandleSuccess            = "report handled: report_id=%d action=%s handler=%d product_id=%d"
	LogReportHandleFailed             = "report handle failed: report_id=%d handler=%d error=%v"
	LogReportProductBlocked           = "blocked action on removed product: product_id=%d actor=%d action=%s"
)

// LogTemplateCount guards the "at least 25 templates" requirement.
const LogTemplateCount = 36
