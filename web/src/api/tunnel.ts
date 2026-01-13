import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

// ========== User 管理 ==========

export interface TunnelUser {
  id: number
  name: string
  display_name: string
  type: 'agent' | 'client' | 'orphan'
  linked_entity: string
  linked_id: number
  node_count: number
  created_at: string
}

export interface TunnelUserDetail extends TunnelUser {
  email: string
}

// 获取 User 列表
export const getTunnelUsers = (params?: {
  type?: string
  search?: string
  page?: number
  size?: number
}) => {
  return request.get<any, PagedResponse<TunnelUser[]>>('/api/v1/admin/tunnel/users', { params })
}

// 获取 User 详情
export const getTunnelUser = (id: number) => {
  return request.get<any, ApiResponse<TunnelUserDetail>>(`/api/v1/admin/tunnel/users/${id}`)
}

// 更新 User
export const updateTunnelUser = (id: number, data: { display_name?: string }) => {
  return request.put<any, ApiResponse>(`/api/v1/admin/tunnel/users/${id}`, data)
}

// 删除 User
export const deleteTunnelUser = (id: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/tunnel/users/${id}`)
}

// 获取 User 的 Node 列表
export const getTunnelUserNodes = (id: number) => {
  return request.get<any, ApiResponse<TunnelNode[]>>(`/api/v1/admin/tunnel/users/${id}/nodes`)
}

// ========== Node 管理 ==========

export interface TunnelNode {
  id: number
  name: string
  user_id: number
  user_name: string
  ip_address: string
  online: boolean
  tags: string[]
  last_seen: string
  created_at: string
}

export interface TunnelNodeDetail extends TunnelNode {
  given_name: string
  ip_addresses: string[]
  forced_tags: string[]
  valid_tags: string[]
  expiry: string
  linked_type: 'agent' | 'desktop' | 'none'
  linked_id: number
}

// 获取 Node 列表
export const getTunnelNodes = (params?: {
  user_id?: number
  status?: string
  search?: string
  page?: number
  size?: number
}) => {
  return request.get<any, PagedResponse<TunnelNode[]>>('/api/v1/admin/tunnel/nodes', { params })
}

// 获取 Node 详情
export const getTunnelNode = (id: number) => {
  return request.get<any, ApiResponse<TunnelNodeDetail>>(`/api/v1/admin/tunnel/nodes/${id}`)
}

// 更新 Node
export const updateTunnelNode = (id: number, data: { given_name?: string }) => {
  return request.put<any, ApiResponse>(`/api/v1/admin/tunnel/nodes/${id}`, data)
}

// 更新 Node Tags
export const updateTunnelNodeTags = (id: number, tags: string[]) => {
  return request.put<any, ApiResponse>(`/api/v1/admin/tunnel/nodes/${id}/tags`, { tags })
}

// 删除 Node
export const deleteTunnelNode = (id: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/tunnel/nodes/${id}`)
}

// 获取常用 Tags 列表
export interface TagOption {
  tag: string
  type: 'client-group' | 'agent-group'
  count: number
}

export const getTunnelTags = () => {
  return request.get<any, ApiResponse<TagOption[]>>('/api/v1/admin/tunnel/tags')
}

// ========== ACL 管理 ==========

export interface ACLPolicy {
  policy: string
  last_synced_at: string
}

export interface ACLRule {
  index: number
  action: string
  src: string[]
  dst: string[]
  description: string
}

export interface TagOwner {
  tag: string
  owners: string[]
}

export interface ACLRules {
  rules: ACLRule[]
  tag_owners: TagOwner[]
}

// 获取 ACL Policy
export const getTunnelACL = () => {
  return request.get<any, ApiResponse<ACLPolicy>>('/api/v1/admin/tunnel/acl')
}

// 更新 ACL Policy
export const updateTunnelACL = (policy: string) => {
  return request.put<any, ApiResponse>('/api/v1/admin/tunnel/acl', { policy })
}

// 获取 ACL 规则列表（可视化）
export const getTunnelACLRules = () => {
  return request.get<any, ApiResponse<ACLRules>>('/api/v1/admin/tunnel/acl/rules')
}

// 强制同步 ACL
export const syncTunnelACL = () => {
  return request.post<any, ApiResponse>('/api/v1/admin/tunnel/acl/sync')
}
