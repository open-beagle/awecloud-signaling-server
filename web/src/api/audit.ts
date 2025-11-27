import request from '@/utils/request'

export interface DeviceInfo {
  os: string
  os_version: string
  arch: string
  cpu_model: string
  machine_id: string
  hostname: string
}

export interface AuditLog {
  id: number
  client_id: number
  client_name: string
  stcp_instance_id: number
  stcp_instance_name: string
  action: string
  local_port: number
  device_info: DeviceInfo
  device_fingerprint: string
  ip_address: string
  success: boolean
  error_message: string
  created_at: string
}

export interface QueryAuditLogsParams {
  client_id?: string
  stcp_instance_id?: string
  action?: string
  start_date?: string
  end_date?: string
  page?: number
  page_size?: number
}

export interface QueryAuditLogsResponse {
  success: boolean
  logs: AuditLog[]
  total: number
  page: number
  page_size: number
  total_pages: number
  message?: string
}

// 查询审计日志
export function queryAuditLogs(params: QueryAuditLogsParams) {
  return request<QueryAuditLogsResponse>({
    url: '/api/v1/admin/audit/connection',
    method: 'get',
    params
  })
}

// 导出审计日志
export function exportAuditLogs(params: QueryAuditLogsParams) {
  return request({
    url: '/api/v1/admin/audit/connection/export',
    method: 'get',
    params,
    responseType: 'blob'
  })
}
