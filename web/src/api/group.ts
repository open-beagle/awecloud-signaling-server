import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

// 分组模型
export interface Group {
  id: number
  tenant_id?: string
  name: string
  alias?: string
  description?: string
  member_count?: number
  created_at: string
  updated_at: string
}

// 分组成员
export interface GroupMember {
  id: number
  group_id: number
  user_id: number
  created_at: string
  user?: {
    id: number
    name: string
    alias?: string
    role: string
  }
}

// 获取分组列表
export const getGroups = (params?: { tenant_id?: string; search?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<Group[]>>('/api/v1/admin/groups', { params })
}

// 获取分组详情
export const getGroup = (id: number) => {
  return request.get<any, ApiResponse<Group>>(`/api/v1/admin/groups/${id}`)
}

// 创建分组
export const createGroup = (data: { tenant_id?: string; name: string; alias?: string; description?: string }) => {
  return request.post<any, ApiResponse<Group>>('/api/v1/admin/groups', data)
}

// 更新分组
export const updateGroup = (id: number, data: { name?: string; alias?: string; description?: string }) => {
  return request.put<any, ApiResponse<Group>>(`/api/v1/admin/groups/${id}`, data)
}

// 删除分组
export const deleteGroup = (id: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/groups/${id}`)
}

// 获取分组成员
export const getGroupMembers = (groupId: number) => {
  return request.get<any, ApiResponse<GroupMember[]>>(`/api/v1/admin/groups/${groupId}/members`)
}

// 添加分组成员
export const addGroupMember = (groupId: number, userId: number) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/groups/${groupId}/members`, { user_ids: [userId] })
}

// 移除分组成员
export const removeGroupMember = (groupId: number, userId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/groups/${groupId}/members/${userId}`)
}
