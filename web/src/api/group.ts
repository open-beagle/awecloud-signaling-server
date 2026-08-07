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

const tenantHeaders = (tenantId?: string) => tenantId
  ? { headers: { 'X-Tenant-ID': tenantId } }
  : {}

// 获取分组列表
export const getGroups = (params?: { tenant_id?: string; search?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<Group[]>>('/api/v1/admin/groups', { params, ...tenantHeaders(params?.tenant_id) })
}

// 获取分组详情
export const getGroup = (id: number, tenantId?: string) => {
  return request.get<any, ApiResponse<Group>>(`/api/v1/admin/groups/${id}`, tenantHeaders(tenantId))
}

// 创建分组
export const createGroup = (data: { tenant_id?: string; name: string; alias?: string; description?: string }) => {
  return request.post<any, ApiResponse<Group>>('/api/v1/admin/groups', data, tenantHeaders(data.tenant_id))
}

// 更新分组
export const updateGroup = (id: number, data: { name?: string; alias?: string; description?: string }, tenantId?: string) => {
  return request.put<any, ApiResponse<Group>>(`/api/v1/admin/groups/${id}`, data, tenantHeaders(tenantId))
}

// 删除分组
export const deleteGroup = (id: number, tenantId?: string) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/groups/${id}`, tenantHeaders(tenantId))
}

// 获取分组成员
export const getGroupMembers = (groupId: number, tenantId?: string) => {
  return request.get<any, ApiResponse<GroupMember[]>>(`/api/v1/admin/groups/${groupId}/members`, tenantHeaders(tenantId))
}

// 添加分组成员
export const addGroupMember = (groupId: number, userId: number, tenantId?: string) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/groups/${groupId}/members`, { user_ids: [userId] }, tenantHeaders(tenantId))
}

// 移除分组成员
export const removeGroupMember = (groupId: number, userId: number, tenantId?: string) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/groups/${groupId}/members/${userId}`, tenantHeaders(tenantId))
}
