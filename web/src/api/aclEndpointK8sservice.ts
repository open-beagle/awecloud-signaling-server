import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

// ========== Endpoint K8SService 授权 ==========

// Endpoint K8SService 授权列表项
export interface EndpointK8SServiceACLItem {
  id: string
  name: string
  alias: string
  agent_id: number
  agent_name: string
  status: string
  user_count: number
  group_count: number
  created_at: string
}

// Endpoint K8SService 授权项
export interface EndpointK8SServiceACLPermissionItem {
  id: number
  name: string
  alias: string
  namespaces: string[]
  service_names: string[]
  enabled: boolean
  granted_at: string
}

// Endpoint K8SService 授权详情
export interface EndpointK8SServiceACLDetail {
  id: string
  name: string
  alias: string
  agent_id: number
  agent_name: string
  status: string
  users: EndpointK8SServiceACLPermissionItem[]
  groups: EndpointK8SServiceACLPermissionItem[]
}

// 获取 Endpoint K8SService 授权列表
export const getEndpointK8SServiceACLList = (params?: { search?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<EndpointK8SServiceACLItem[]>>('/api/v1/admin/acl/endpoint-k8sservice', { params })
}

// 获取 Endpoint K8SService 授权详情
export const getEndpointK8SServiceACL = (id: string) => {
  return request.get<any, ApiResponse<EndpointK8SServiceACLDetail>>(`/api/v1/admin/acl/endpoint-k8sservice/${id}`)
}

// 添加 Endpoint K8SService 用户授权
export const addEndpointK8SServiceACLUsers = (id: string, data: { user_ids: number[]; namespaces: string[]; service_names: string[] }) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/acl/endpoint-k8sservice/${id}/users`, data)
}

// 添加 Endpoint K8SService 分组授权
export const addEndpointK8SServiceACLGroups = (id: string, data: { group_ids: number[]; namespaces: string[]; service_names: string[] }) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/acl/endpoint-k8sservice/${id}/groups`, data)
}

// 撤销 Endpoint K8SService 用户授权
export const removeEndpointK8SServiceACLUser = (id: string, userId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/acl/endpoint-k8sservice/${id}/users/${userId}`)
}

// 撤销 Endpoint K8SService 分组授权
export const removeEndpointK8SServiceACLGroup = (id: string, groupId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/acl/endpoint-k8sservice/${id}/groups/${groupId}`)
}
