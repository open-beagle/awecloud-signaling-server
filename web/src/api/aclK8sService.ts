import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

// ========== K8S Service 授权 ==========

// K8S Service 授权列表项
export interface K8SServiceACLItem {
  id: number
  name: string
  alias?: string
  role: string
  svc_enabled: boolean
  user_count: number
  group_count: number
  created_at: string
}

// K8S Service 授权项
export interface K8SServiceACLPermissionItem {
  id: number
  name: string
  alias?: string
  namespaces: string[]
  service_names: string[]
  enabled: boolean
  granted_at: string
}

// K8S Service 授权详情
export interface K8SServiceACLDetail {
  id: number
  name: string
  alias?: string
  role: string
  svc_enabled: boolean
  users: K8SServiceACLPermissionItem[]
  groups: K8SServiceACLPermissionItem[]
}

// 获取 K8S Service 授权列表
export const getK8SServiceACLList = (params?: { search?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<K8SServiceACLItem[]>>('/api/v1/admin/acl/k8s-service', { params })
}

// 获取 K8S Service 授权详情
export const getK8SServiceACL = (id: number) => {
  return request.get<any, ApiResponse<K8SServiceACLDetail>>(`/api/v1/admin/acl/k8s-service/${id}`)
}

// 添加 K8SService 用户授权
export const addK8SServiceACLUsers = (id: number, data: { user_ids: number[]; namespaces: string[]; service_names: string[] }) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/acl/k8s-service/${id}/users`, data)
}

// 添加 K8SService 分组授权
export const addK8SServiceACLGroups = (id: number, data: { group_ids: number[]; namespaces: string[]; service_names: string[] }) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/acl/k8s-service/${id}/groups`, data)
}

// 撤销 K8SService 用户授权
export const removeK8SServiceACLUser = (id: number, userId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/acl/k8s-service/${id}/users/${userId}`)
}

// 撤销 K8SService 分组授权
export const removeK8SServiceACLGroup = (id: number, groupId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/acl/k8s-service/${id}/groups/${groupId}`)
}
