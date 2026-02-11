import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

// ========== 服务授权 ==========

// 服务授权列表项
export interface ServiceACLItem {
  id: string
  name: string
  alias?: string
  user_id: number
  user_name: string
  source_addr: string
  user_count: number
  group_count: number
  created_at: string
}

// 授权项
export interface ACLPermissionItem {
  id: number
  name: string
  alias?: string
  granted_at: string
}

// 服务授权详情
export interface ServiceACLDetail {
  id: string
  name: string
  alias?: string
  user_id: number
  user_name: string
  source_addr: string
  target_addr: string
  users: ACLPermissionItem[]
  groups: ACLPermissionItem[]
}

// 获取服务授权列表
export const getServiceACLList = (params?: { search?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<ServiceACLItem[]>>('/api/v1/admin/acl/services', { params })
}

// 获取服务授权详情
export const getServiceACL = (id: string) => {
  return request.get<any, ApiResponse<ServiceACLDetail>>(`/api/v1/admin/acl/services/${id}`)
}

// 添加服务用户授权
export const addServiceACLUsers = (id: string, userIds: number[]) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/acl/services/${id}/users`, { user_ids: userIds })
}

// 添加服务分组授权
export const addServiceACLGroups = (id: string, groupIds: number[]) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/acl/services/${id}/groups`, { group_ids: groupIds })
}

// 撤销服务用户授权
export const removeServiceACLUser = (id: string, userId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/acl/services/${id}/users/${userId}`)
}

// 撤销服务分组授权
export const removeServiceACLGroup = (id: string, groupId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/acl/services/${id}/groups/${groupId}`)
}


// ========== 用户授权 ==========

// 用户授权列表项
export interface UserACLItem {
  id: number
  name: string
  alias?: string
  role: string
  user_count: number
  group_count: number
  created_at: string
}

// 用户授权详情
export interface UserACLDetail {
  id: number
  name: string
  alias?: string
  role: string
  users: ACLPermissionItem[]
  groups: ACLPermissionItem[]
}

// 获取用户授权列表
export const getUserACLList = (params?: { search?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<UserACLItem[]>>('/api/v1/admin/acl/users', { params })
}

// 获取用户授权详情
export const getUserACL = (id: number) => {
  return request.get<any, ApiResponse<UserACLDetail>>(`/api/v1/admin/acl/users/${id}`)
}

// 添加用户授权（用户级别）
export const addUserACLUsers = (id: number, userIds: number[]) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/acl/users/${id}/users`, { user_ids: userIds })
}

// 添加用户授权（分组级别）
export const addUserACLGroups = (id: number, groupIds: number[]) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/acl/users/${id}/groups`, { group_ids: groupIds })
}

// 撤销用户授权（用户级别）
export const removeUserACLUser = (id: number, userId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/acl/users/${id}/users/${userId}`)
}

// 撤销用户授权（分组级别）
export const removeUserACLGroup = (id: number, groupId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/acl/users/${id}/groups/${groupId}`)
}

// ========== 分组授权 ==========

// 分组授权列表项
export interface GroupACLItem {
  id: number
  name: string
  alias?: string
  member_count: number
  user_count: number
  group_count: number
  created_at: string
}

// 分组授权详情
export interface GroupACLDetail {
  id: number
  name: string
  alias?: string
  users: ACLPermissionItem[]
  groups: ACLPermissionItem[]
}

// 获取分组授权列表
export const getGroupACLList = (params?: { search?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<GroupACLItem[]>>('/api/v1/admin/acl/groups', { params })
}

// 获取分组授权详情
export const getGroupACL = (id: number) => {
  return request.get<any, ApiResponse<GroupACLDetail>>(`/api/v1/admin/acl/groups/${id}`)
}

// 添加分组授权（用户级别）
export const addGroupACLUsers = (id: number, userIds: number[]) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/acl/groups/${id}/users`, { user_ids: userIds })
}

// 添加分组授权（分组级别）
export const addGroupACLGroups = (id: number, groupIds: number[]) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/acl/groups/${id}/groups`, { group_ids: groupIds })
}

// 撤销分组授权（用户级别）
export const removeGroupACLUser = (id: number, userId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/acl/groups/${id}/users/${userId}`)
}

// 撤销分组授权（分组级别）
export const removeGroupACLGroup = (id: number, groupId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/acl/groups/${id}/groups/${groupId}`)
}


// ========== SSH 授权 ==========

// SSH 授权列表项
export interface SSHACLItem {
  id: number
  name: string
  alias?: string
  role: string
  ssh_enabled: boolean
  user_count: number
  group_count: number
  created_at: string
}

// SSH 授权项
export interface SSHACLPermissionItem {
  id: number
  name: string
  alias?: string
  ssh_users: string[]
  enabled: boolean
  granted_at: string
}

// SSH 授权详情
export interface SSHACLDetail {
  id: number
  name: string
  alias?: string
  role: string
  ssh_enabled: boolean
  users: SSHACLPermissionItem[]
  groups: SSHACLPermissionItem[]
}

// 获取 SSH 授权列表
export const getSSHACLList = (params?: { search?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<SSHACLItem[]>>('/api/v1/admin/acl/ssh', { params })
}

// 获取 SSH 授权详情
export const getSSHACL = (id: number) => {
  return request.get<any, ApiResponse<SSHACLDetail>>(`/api/v1/admin/acl/ssh/${id}`)
}

// 添加 SSH 用户授权
export const addSSHACLUsers = (id: number, userIds: number[], sshUsers: string[]) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/acl/ssh/${id}/users`, { user_ids: userIds, ssh_users: sshUsers })
}

// 添加 SSH 分组授权
export const addSSHACLGroups = (id: number, groupIds: number[], sshUsers: string[]) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/acl/ssh/${id}/groups`, { group_ids: groupIds, ssh_users: sshUsers })
}

// 撤销 SSH 用户授权
export const removeSSHACLUser = (id: number, userId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/acl/ssh/${id}/users/${userId}`)
}

// 撤销 SSH 分组授权
export const removeSSHACLGroup = (id: number, groupId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/acl/ssh/${id}/groups/${groupId}`)
}
