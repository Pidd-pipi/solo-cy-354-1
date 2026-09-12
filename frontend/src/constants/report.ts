// 举报原因（与后端 backend/internal/constants/report.go 保持一致）
export const REPORT_REASONS = [
  { value: 'spam', label: '垃圾广告' },
  { value: 'fraud', label: '欺诈/诈骗' },
  { value: 'prohibited', label: '违禁物品' },
  { value: 'fake_info', label: '虚假信息' },
  { value: 'harassment', label: '骚扰/不文明交易' },
  { value: 'other', label: '其他' },
] as const

// 举报处理状态
export const REPORT_STATUSES = [
  { value: 'pending', label: '待处理', type: 'warning' },
  { value: 'removed', label: '已下架（举报成立）', type: 'danger' },
  { value: 'rejected', label: '已驳回', type: 'info' },
] as const

// 管理员处理动作
export const REPORT_ACTIONS = [
  { value: 'remove', label: '下架商品', type: 'danger' },
  { value: 'reject', label: '驳回举报', type: 'info' },
] as const

export function reportReasonLabel(value: string): string {
  return REPORT_REASONS.find((r) => r.value === value)?.label ?? value
}

export function reportStatusLabel(value: string): string {
  return REPORT_STATUSES.find((s) => s.value === value)?.label ?? value
}

export function reportStatusType(value: string): string {
  return REPORT_STATUSES.find((s) => s.value === value)?.type ?? 'info'
}
