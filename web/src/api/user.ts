import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

// 用户角色
export type UserRole = 'agent' | 'client'

// 用户模型
export interface User {
  id: number
  name: string
  alias?: string
  role: UserRole
  ssh_enabled?: boolean
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
export const getUsers = (params?: { role?: UserRole; search?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<User[]>>('/api/v1/admin/users', { params })
}

// 获取用户详情
export const getUser = (id: number) => {
  return request.get<any, ApiResponse<UserDetail>>(`/api/v1/admin/users/${id}`)
}

// 创建用户
export const createUser = (data: CreateUserRequest) => {
  return request.post<any, ApiResponse<CreateUserResponse>>('/api/v1/admin/users', data)
}

// 更新用户
export const updateUser = (id: number, data: UpdateUserRequest) => {
  return request.put<any, ApiResponse<User>>(`/api/v1/admin/users/${id}`, data)
}

// 删除用户
export const deleteUser = (id: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/users/${id}`)
}

// 重新生成密钥
export const regenerateUserSecret = (id: number) => {
  return request.post<any, ApiResponse<{ secret: string }>>(`/api/v1/admin/users/${id}/regenerate-secret`)
}

// 获取用户实时信息（仅 Agent）
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

export const getUserRealtime = (id: number) => {
  return request.get<any, ApiResponse<UserRealtimeInfo>>(`/api/v1/admin/users/${id}/realtime`)
}
