import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

// 用户角色
export type UserRole = 'agent' | 'client'

// 用户来源
export type UserSource = 'manual' | 'logto'

// 用户模型
export interface User {
  id: number
  name: string
  alias?: string
  role: UserRole
  ssh_enabled?: boolean
  enabled?: boolean
  source?: UserSource
  created_at: string
  updated_at: string
  // 关联数据
  node_count?: number
  service_count?: number
  group_count?: number
}

// 用户详情中的设备
export interface UserNode {
  id: number
  name: string
  type: string
  ip: string
  version: string
  status: string
  last_heartbeat?: string
}

// 用户详情
export interface UserDetail extends User {
  nodes?: UserNode[]
}

// 创建用户请求
export interface CreateUserRequest {
  name: string
  alias?: string
  role: UserRole
  ssh_enabled?: boolean
}

// 创建用户响应
export interface CreateUserResponse {
  id: number
  name: string
  secret: string
}

// 更新用户请求
export interface UpdateUserRequest {
  alias?: string
  ssh_enabled?: boolean
}

// 获取用户列表
export const getUsers = (params?: { role?: UserRole; search?: string; enabled?: string; source?: UserSource; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<User[]>>('/api/v1/admin/users', { params })
}

// 获取用户详情（支持 ID 或用户名）
export const getUser = (identifier: number | string) => {
  return request.get<any, ApiResponse<UserDetail>>(`/api/v1/admin/users/${identifier}`)
}

// 创建用户
export const createUser = (data: CreateUserRequest) => {
  return request.post<any, ApiResponse<CreateUserResponse>>('/api/v1/admin/users', data)
}

// 更新用户（支持 ID 或用户名）
export const updateUser = (identifier: number | string, data: UpdateUserRequest) => {
  return request.put<any, ApiResponse<User>>(`/api/v1/admin/users/${identifier}`, data)
}

// 删除用户（支持 ID 或用户名）
export const deleteUser = (identifier: number | string) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/users/${identifier}`)
}

// 重新生成密钥（支持 ID 或用户名）
export const regenerateUserSecret = (identifier: number | string) => {
  return request.post<any, ApiResponse<{ secret: string }>>(`/api/v1/admin/users/${identifier}/regenerate-secret`)
}

// 获取用户实时信息（仅 Agent，支持 ID 或用户名）
export interface UserRealtimeInfo {
  hostname: string
  runtime: string
  tunnel_ip: string
  tunnel_connected: boolean
  tunnel_connected_time: number
  networks: Array<{
    name: string
    ip: string
    gateway: string
  }>
}

export const getUserRealtime = (identifier: number | string) => {
  return request.get<any, ApiResponse<UserRealtimeInfo>>(`/api/v1/admin/users/${identifier}/realtime`)
}

// 启用用户（支持 ID 或用户名）
export const enableUser = (identifier: number | string) => {
  return request.put<any, ApiResponse>(`/api/v1/admin/users/${identifier}/enable`)
}

// 禁用用户（支持 ID 或用户名）
export const disableUser = (identifier: number | string) => {
  return request.put<any, ApiResponse>(`/api/v1/admin/users/${identifier}/disable`)
}
