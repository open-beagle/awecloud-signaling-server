import request from '@/utils/request'
import type { PagedResponse, ApiResponse } from '@/types/models'

// 操作审计日志项
export interface OperationAuditItem {
  id: number
  agent_name: string
  client_name: string
  endpoint_name: string
  operation_type: string
  target: string
  detail: string
  started_at: string
  ended_at: string
  duration_ms: number
  created_at: string
}

// 操作类型
export interface OperationType {
  value: string
  label: string
}

// 查询参数
export interface OperationAuditParams {
  operation_type?: string
  agent_user_id?: number
  endpoint_name?: string
  start_date?: string
  end_date?: string
  page?: number
  size?: number
}

// 查询操作审计日志
export const getOperationAuditList = (params: OperationAuditParams) => {
  return request.get<any, PagedResponse<OperationAuditItem[]>>('/api/v1/admin/audit/operations', { params })
}

// 获取操作类型列表
export const getOperationTypes = () => {
  return request.get<any, ApiResponse<OperationType[]>>('/api/v1/admin/audit/operation-types')
}
