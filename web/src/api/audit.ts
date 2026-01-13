import request from '@/utils/request'

// 审计日志项
export interface AuditLog {
  id: number
  action_type: string
  actor_name: string
  target_name: string
  detail: string
  created_at: string
}

// 操作类型
export interface ActionType {
  value: string
  label: string
}

// 查询参数
export interface QueryAuditLogsParams {
  action_type?: string
  user_id?: number
  start_date?: string
  end_date?: string
  page?: number
  size?: number
}

// 查询审计日志
export function queryAuditLogs(params: QueryAuditLogsParams) {
  return request({
    url: '/api/v1/admin/audit/logs',
    method: 'get',
    params
  })
}

// 获取操作类型列表
export function getActionTypes() {
  return request({
    url: '/api/v1/admin/audit/action-types',
    method: 'get'
  })
}


// 管理员选项
export interface AdminOption {
  id: number
  username: string
}

// 获取管理员列表（用于筛选）
export function getAdminList() {
  return request({
    url: '/api/v1/admin/audit/users',
    method: 'get'
  })
}
