import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

// ========== Endpoint SSH 授权 ==========

// Endpoint SSH 授权列表项
export interface EndpointSSHACLItem {
  id: string
  name: string
  alias: string
  agent_id: number
  agent_name: string
  host: string
  port: number
  status: string
  user_count: number
  group_count: number
  created_at: string
}

// Endpoint SSH 授权项
export interface EndpointSSHACLPermissionItem {
  id: number
  name: string
  alias: string
  ssh_users: string[]
  enabled: boolean
  granted_at: string
}

// Endpoint SSH 授权详情
export interface EndpointSSHACLDetail {
  id: string
  name: string
  alias: string
  agent_id: number
  agent_name: string
  host: string
  port: number
  status: string
  users: EndpointSSHACLPermissionItem[]
  groups: EndpointSSHACLPermissionItem[]
}

// 获取 Endpoint SSH 授权列表
export const getEndpointSSHACLList = (params?: { search?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<EndpointSSHACLItem[]>>('/api/v1/admin/acl/endpoint-ssh', { params })
}

// 获取 Endpoint SSH 授权详情
export const getEndpointSSHACL = (id: string) => {
  return request.get<any, ApiResponse<EndpointSSHACLDetail>>(`/api/v1/admin/acl/endpoint-ssh/${id}`)
}

// 添加 Endpoint SSH 用户授权
export const addEndpointSSHACLUsers = (id: string, userIds: number[], sshUsers: string[]) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/acl/endpoint-ssh/${id}/users`, { user_ids: userIds, ssh_users: sshUsers })
}

// 添加 Endpoint SSH 分组授权
export const addEndpointSSHACLGroups = (id: string, groupIds: number[], sshUsers: string[]) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/acl/endpoint-ssh/${id}/groups`, { group_ids: groupIds, ssh_users: sshUsers })
}

// 撤销 Endpoint SSH 用户授权
export const removeEndpointSSHACLUser = (id: string, userId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/acl/endpoint-ssh/${id}/users/${userId}`)
}

// 撤销 Endpoint SSH 分组授权
export const removeEndpointSSHACLGroup = (id: string, groupId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/acl/endpoint-ssh/${id}/groups/${groupId}`)
}
