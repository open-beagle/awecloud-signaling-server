import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

// ========== K8S API 授权 ==========

// K8S API 授权列表项
export interface K8SACLItem {
  id: number
  name: string
  alias?: string
  role: string
  k8s_enabled: boolean
  user_count: number
  group_count: number
  created_at: string
}

// K8S API 授权项
export interface K8SACLPermissionItem {
  id: number
  name: string
  alias?: string
  k8s_groups: string[]
  namespaces: string[]
  enabled: boolean
  granted_at: string
}

// K8S API 授权详情
export interface K8SACLDetail {
  id: number
  name: string
  alias?: string
  role: string
  k8s_enabled: boolean
  users: K8SACLPermissionItem[]
  groups: K8SACLPermissionItem[]
}

// 获取 K8S API 授权列表
export const getK8SACLList = (params?: { search?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<K8SACLItem[]>>('/api/v1/admin/acl/k8s', { params })
}

// 获取 K8S API 授权详情
export const getK8SACL = (id: number) => {
  return request.get<any, ApiResponse<K8SACLDetail>>(`/api/v1/admin/acl/k8s/${id}`)
}

// 添加 K8S 用户授权
export const addK8SACLUsers = (id: number, data: { user_ids: number[]; k8s_groups: string[]; namespaces: string[] }) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/acl/k8s/${id}/users`, data)
}

// 添加 K8S 分组授权
export const addK8SACLGroups = (id: number, data: { group_ids: number[]; k8s_groups: string[]; namespaces: string[] }) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/acl/k8s/${id}/groups`, data)
}

// 撤销 K8S 用户授权
export const removeK8SACLUser = (id: number, userId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/acl/k8s/${id}/users/${userId}`)
}

// 撤销 K8S 分组授权
export const removeK8SACLGroup = (id: number, groupId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/acl/k8s/${id}/groups/${groupId}`)
}
