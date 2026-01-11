import request from '@/utils/request'
import type { Agent, AgentDetail, ApiResponse } from '@/types/models'

export const getAgents = () => {
  return request.get<any, ApiResponse<Agent[]>>('/api/v1/admin/agents')
}

export const getAgent = (id: number) => {
  return request.get<any, ApiResponse<AgentDetail>>(`/api/v1/admin/agents/${id}`)
}

export const createAgent = (data: { agent_name: string; description?: string; group_name?: string }) => {
  return request.post<any, ApiResponse<Agent>>('/api/v1/admin/agents', data)
}

export const updateAgent = (id: number, data: { agent_name?: string; group_name?: string; description?: string }) => {
  return request.put<any, ApiResponse<Agent>>(`/api/v1/admin/agents/${id}`, data)
}

export const deleteAgent = (id: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/agents/${id}`)
}

export const getAgentToken = (id: number) => {
  return request.get<any, ApiResponse<{ agent_token: string }>>(`/api/v1/admin/agents/${id}/token`)
}

export const regenerateToken = (id: number) => {
  return request.post<any, ApiResponse<{ agent_token: string }>>(`/api/v1/admin/agents/${id}/regenerate-token`)
}
