import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

// 设备类型
export type NodeType = 'agent' | 'desktop'

// 设备系统信息
export interface NodeSystemInfo {
  os: string
  os_version: string
  arch: string
  hostname: string
  cpu: string
  cpu_cores: number
  memory_gb: number
}

// 设备模型
export interface Node {
  id: number
  user_id: number
  name: string
  type: NodeType
  ip?: string
  version?: string
  hostname?: string
  system_info?: string
  last_heartbeat?: string
  created_at: string
  updated_at: string
  // 关联数据
  user?: {
    id: number
    name: string
    alias?: string
    role: string
  }
  // 计算字段
  online?: boolean
}

// 设备详情
export interface NodeDetail extends Node {
  system_info_parsed?: NodeSystemInfo
}

// 获取设备列表
export const getNodes = (params?: { type?: NodeType; user_id?: number; search?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<Node[]>>('/api/v1/admin/nodes', { params })
}

// 获取设备详情
export const getNode = (id: number) => {
  return request.get<any, ApiResponse<NodeDetail>>(`/api/v1/admin/nodes/${id}`)
}

// 删除设备
export const deleteNode = (id: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/nodes/${id}`)
}

// 获取用户的设备列表
export const getNodesByUser = (userId: number) => {
  return request.get<any, ApiResponse<Node[]>>(`/api/v1/admin/users/${userId}/nodes`)
}
