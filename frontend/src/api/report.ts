import request from '../utils/request'
import type { Report, ReportView } from '../types'

export interface CreateReportPayload {
  product_id: number
  reason: string
  detail?: string
}

// 学生提交商品举报
export function createReport(data: CreateReportPayload) {
  return request.post<never, { code: number; message: string; data: Report }>('/reports', data)
}

// 我的举报记录（举报者查看处理状态）
export function listMyReports() {
  return request.get<never, { code: number; message: string; data: ReportView[] }>('/reports/me')
}

// 管理员：查看举报记录（status 为空表示全部）
export function listReports(status?: string) {
  return request.get<never, { code: number; message: string; data: ReportView[] }>('/admin/reports', {
    params: status ? { status } : {},
  })
}

// 管理员：处理举报（下架商品 / 驳回举报）
export function handleReport(id: number, action: 'remove' | 'reject', note?: string) {
  return request.post<never, { code: number; message: string; data: ReportView }>(
    `/admin/reports/${id}/handle`,
    { action, note },
  )
}
