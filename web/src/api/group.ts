import request from '@/utils/request'
import type { ApiResponse } from '@/types/models'

export interface Group {
  id: number
  name: string
  alias?: string
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
    name?: string
    alias?: string
  }
}

export interface AgentGroupMember {
  id: number
  group_id: number
  agent_id: number
  created_at: string
  agent?: {
    id: number
    name: string
    alias?: string
    ip?: string
  }
}

// 用户分组 API
export const getGroups = () => {
  return request.get<any, ApiResponse<Group[]>>('/api/v1/admin/client-groups')
}

export const createGroup = (data: { name: string; alias?: string; description?: string }) => {
  return request.post<any, ApiResponse<Group>>('/api/v1/admin/client-groups', data)
}

export const updateGroup = (id: number, data: { name?: string; alias?: string; description?: string }) => {
  return request.put<any, ApiResponse>(`/api/v1/admin/client-groups/${id}`, data)
}

export const deleteGroup = (id: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/client-groups/${id}`)
}

export const getGroupMembers = (groupId: number) => {
  return request.get<any, ApiResponse<GroupMember[]>>(`/api/v1/admin/client-groups/${groupId}/members`)
}

export const addGroupMember = (groupId: number, clientId: number, role: string = 'member') => {
  return request.post<any, ApiResponse>(`/api/v1/admin/client-groups/${groupId}/members`, {
    client_id: clientId,
    role
  })
}

export const removeGroupMember = (groupId: number, clientId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/client-groups/${groupId}/members/${clientId}`)
}

// 代理分组 API
export const getAgentGroups = () => {
  return request.get<any, ApiResponse<Group[]>>('/api/v1/admin/agent-groups')
}

export const createAgentGroup = (data: { name: string; alias?: string; description?: string }) => {
  return request.post<any, ApiResponse<Group>>('/api/v1/admin/agent-groups', data)
}

export const updateAgentGroup = (id: number, data: { name?: string; alias?: string; description?: string }) => {
  return request.put<any, ApiResponse>(`/api/v1/admin/agent-groups/${id}`, data)
}

export const deleteAgentGroup = (id: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/agent-groups/${id}`)
}

export const getAgentGroupMembers = (groupId: number) => {
  return request.get<any, ApiResponse<AgentGroupMember[]>>(`/api/v1/admin/agent-groups/${groupId}/members`)
}

export const addAgentGroupMember = (groupId: number, agentId: number) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/agent-groups/${groupId}/members`, {
    agent_id: agentId
  })
}

export const removeAgentGroupMember = (groupId: number, agentId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/agent-groups/${groupId}/members/${agentId}`)
}
