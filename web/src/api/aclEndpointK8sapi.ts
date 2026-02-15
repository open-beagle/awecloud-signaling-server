import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

// ========== Endpoint K8SAPI 授权 ==========

// Endpoint K8SAPI 授权列表项
export interface EndpointK8SAPIACLItem {
  id: string
  name: string
  alias: string
  agent_id: number
  agent_name: string
  api_server: string
  status: string
  user_count: number
  group_count: number
  created_at: string
}

// Endpoint K8SAPI 授权项
export interface EndpointK8SAPIACLPermissionItem {
  id: number
  name: string
  alias: string
  k8s_groups: string[]
  namespaces: string[]
  enabled: boolean
  granted_at: string
}

// Endpoint K8SAPI 授权详情
export interface EndpointK8SAPIACLDetail {
  id: string
  name: string
  alias: string
  agent_id: number
  agent_name: string
  api_server: string
  status: string
  users: EndpointK8SAPIACLPermissionItem[]
  groups: EndpointK8SAPIACLPermissionItem[]
}

// 获取 Endpoint K8SAPI 授权列表
export const getEndpointK8SAPIACLList = (params?: { search?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<EndpointK8SAPIACLItem[]>>('/api/v1/admin/acl/endpoint-k8sapi', { params })
}

// 获取 Endpoint K8SAPI 授权详情
export const getEndpointK8SAPIACL = (id: string) => {
  return request.get<any, ApiResponse<EndpointK8SAPIACLDetail>>(`/api/v1/admin/acl/endpoint-k8sapi/${id}`)
}

// 添加 Endpoint K8SAPI 用户授权
export const addEndpointK8SAPIACLUsers = (id: string, data: { user_ids: number[]; k8s_groups: string[]; namespaces: string[] }) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/acl/endpoint-k8sapi/${id}/users`, data)
}

// 添加 Endpoint K8SAPI 分组授权
export const addEndpointK8SAPIACLGroups = (id: string, data: { group_ids: number[]; k8s_groups: string[]; namespaces: string[] }) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/acl/endpoint-k8sapi/${id}/groups`, data)
}

// 撤销 Endpoint K8SAPI 用户授权
export const removeEndpointK8SAPIACLUser = (id: string, userId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/acl/endpoint-k8sapi/${id}/users/${userId}`)
}

// 撤销 Endpoint K8SAPI 分组授权
export const removeEndpointK8SAPIACLGroup = (id: string, groupId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/acl/endpoint-k8sapi/${id}/groups/${groupId}`)
}
