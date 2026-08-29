import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

// 用户角色
export type UserRole = 'agent' | 'client'

// 用户来源
export type UserSource = 'manual' | 'logto'

export type UserDeletionStatus = 'accepting' | 'queued' | 'running' | 'failed' | 'succeeded'

export interface UserDeletionSummary {
  id: string
  user_id: number
  status: UserDeletionStatus
  current_step: string
  progress: number
  attempt: number
  error_code?: string
  error_message?: string
  request_id: string
  created_at: string
  updated_at: string
  completed_at?: string
  row_version: number
}

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
  versions?: string[]      // 设备版本列表（去重）
  latest_version?: string  // 最新版本号
  deletion?: UserDeletionSummary
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

export const createUserDeletionJob = (identifier: number | string, idempotencyKey: string) => {
  return request.post<any, ApiResponse<UserDeletionSummary>>(`/api/v1/admin/users/${identifier}/deletion-jobs`, undefined, {
    headers: { 'Idempotency-Key': idempotencyKey }
  })
}

export const getUserDeletionJob = (jobId: string) => {
  return request.get<any, ApiResponse<UserDeletionSummary>>(`/api/v1/admin/user-deletion-jobs/${jobId}`)
}

export const retryUserDeletionJob = (job: UserDeletionSummary, idempotencyKey: string) => {
  return request.post<any, ApiResponse<UserDeletionSummary>>(`/api/v1/admin/user-deletion-jobs/${job.id}/retry`, undefined, {
    headers: { 'Idempotency-Key': idempotencyKey, 'If-Match': String(job.row_version) }
  })
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
