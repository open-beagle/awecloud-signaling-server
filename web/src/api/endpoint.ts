import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

// ========== Endpoint 管理 ==========

// SSH Endpoint
export interface EndpointSSH {
  id: string
  user_id: number
  user_name?: string
  name: string
  alias?: string
  host: string
  port: number
  ssh_users: string[]
  enabled: boolean
  created_at: string
  updated_at: string
}

// K8SAPI Endpoint
export interface EndpointK8SAPI {
  id: string
  user_id: number
  user_name?: string
  name: string
  alias?: string
  api_server: string
  kubeconfig_ref: string
  enabled: boolean
  created_at: string
  updated_at: string
}

// K8SService Endpoint
export interface EndpointK8SService {
  id: string
  user_id: number
  user_name?: string
  name: string
  alias?: string
  namespace: string
  service_name: string
  target_port: number
  enabled: boolean
  created_at: string
  updated_at: string
}

// 列表
export const getEndpoints = (type: string, params?: { search?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<any[]>>(`/api/v1/admin/endpoints/${type}`, { params })
}

// 详情
export const getEndpoint = (type: string, id: string) => {
  return request.get<any, ApiResponse<any>>(`/api/v1/admin/endpoints/${type}/${id}`)
}

// 创建
export const createEndpoint = (type: string, data: any) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/endpoints/${type}`, data)
}

// 更新
export const updateEndpoint = (type: string, id: string, data: any) => {
  return request.put<any, ApiResponse>(`/api/v1/admin/endpoints/${type}/${id}`, data)
}

// 删除
export const deleteEndpoint = (type: string, id: string) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/endpoints/${type}/${id}`)
}
