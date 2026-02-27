import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

// ========== 统一 Endpoint 类型 ==========

// Endpoint 列表项
export interface EndpointItem {
  id: string
  user_id: number
  agent_name: string
  name: string
  alias: string
  version: string
  status: string
  ssh_enabled: boolean
  k8sapi_enabled: boolean
  k8sservice_enabled: boolean
  created_at: string
}

// Endpoint 详情
export interface EndpointDetail {
  id: string
  user_id: number
  agent_name: string
  name: string
  alias: string
  version: string
  status: string
  ssh_enabled: boolean
  ssh_run_as: string
  ssh_can_switch_user: boolean
  ssh_users: string[]
  ssh_port: number
  k8sapi_enabled: boolean
  k8sapi_api_server: string
  k8sapi_port: number
  k8sservice_enabled: boolean
  k8sservice_label_selector: string
  k8sservice_namespaces: string[]
  created_at: string
  updated_at: string
}

// 列表参数
export interface EndpointListParams {
  agent_id?: string
  status?: string
  search?: string
  page?: number
  size?: number
}

// 更新请求
export interface UpdateEndpointData {
  alias?: string
  ssh_enabled?: boolean
  ssh_run_as?: string
  ssh_can_switch_user?: boolean
  ssh_users?: string[]
  k8sapi_enabled?: boolean
  k8sapi_api_server?: string
  k8sservice_enabled?: boolean
  k8sservice_label_selector?: string
  k8sservice_namespaces?: string[]
}

// 列表
export const getEndpoints = (params?: EndpointListParams) => {
  return request.get<any, PagedResponse<EndpointItem[]>>('/api/v1/admin/endpoints', { params })
}

// 详情
export const getEndpointDetail = (id: string) => {
  return request.get<any, ApiResponse<EndpointDetail>>(`/api/v1/admin/endpoints/${id}`)
}

// 更新
export const updateEndpoint = (id: string, data: UpdateEndpointData) => {
  return request.put<any, ApiResponse>(`/api/v1/admin/endpoints/${id}`, data)
}

// 注销
export const revokeEndpoint = (id: string) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/endpoints/${id}`)
}
