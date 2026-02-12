import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

// ========== 统一 Endpoint 类型 ==========

// Endpoint 列表项（统一）
export interface EndpointItem {
  id: string
  type: string // ssh / k8sapi / k8sservice
  user_id: number
  agent_name: string
  name: string
  alias: string
  host: string
  port: number
  api_server: string
  status: string
  enabled: boolean
  created_at: string
}

// Endpoint 详情
export interface EndpointDetail {
  id: string
  type: string
  user_id: number
  agent_name: string
  name: string
  alias: string
  host: string
  port: number
  ssh_users: string[]
  api_server: string
  domain: string
  status: string
  enabled: boolean
  created_at: string
  updated_at: string
}

// 列表参数
export interface EndpointListParams {
  type?: string
  agent_id?: string
  status?: string
  search?: string
  page?: number
  size?: number
}

// 统一列表
export const getEndpoints = (params?: EndpointListParams) => {
  return request.get<any, PagedResponse<EndpointItem[]>>('/api/v1/admin/endpoints', { params })
}

// 统一详情
export const getEndpointDetail = (type: string, id: string) => {
  return request.get<any, ApiResponse<EndpointDetail>>(`/api/v1/admin/endpoints/${type}/${id}`)
}

// 更新（仅允许修改别名和启用状态）
export const updateEndpoint = (type: string, id: string, data: { alias?: string; enabled?: boolean }) => {
  return request.put<any, ApiResponse>(`/api/v1/admin/endpoints/${type}/${id}`, data)
}

// 注销
export const revokeEndpoint = (type: string, id: string) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/endpoints/${type}/${id}`)
}
