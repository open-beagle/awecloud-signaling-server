import request from '@/utils/request'
import type { ApiResponse } from '@/types/models'

export interface Group {
  id: number
  name: string
  description: string
  member_count?: number
  created_at: string
  updated_at: string
}

export interface GroupMember {
  id: number
  group_id: number
  client_id: number
  role: string
  created_at: string
  client?: {
    id: number
    client_id: string
  }
}

export const getGroups = () => {
  return request.get<any, ApiResponse<Group[]>>('/api/v1/admin/groups')
}

export const createGroup = (data: { name: string; description?: string }) => {
  return request.post<any, ApiResponse<Group>>('/api/v1/admin/groups', data)
}

export const updateGroup = (id: number, data: { name?: string; description?: string }) => {
  return request.put<any, ApiResponse>(`/api/v1/admin/groups/${id}`, data)
}

export const deleteGroup = (id: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/groups/${id}`)
}

export const getGroupMembers = (groupId: number) => {
  return request.get<any, ApiResponse<GroupMember[]>>(`/api/v1/admin/groups/${groupId}/members`)
}

export const addGroupMember = (groupId: number, clientId: number, role: string = 'member') => {
  return request.post<any, ApiResponse>(`/api/v1/admin/groups/${groupId}/members`, {
    client_id: clientId,
    role
  })
}

export const removeGroupMember = (groupId: number, clientId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/groups/${groupId}/members/${clientId}`)
}
