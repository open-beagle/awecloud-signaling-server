import request from '@/utils/request'
import type { Agent, ApiResponse } from '@/types/models'

export const getAgents = () => {
  return request.get<any, ApiResponse<Agent[]>>('/agents')
}

export const createAgent = (data: { agent_name: string; description?: string }) => {
  return request.post<any, ApiResponse<Agent>>('/agents', data)
}

export const deleteAgent = (id: number) => {
  return request.delete<any, ApiResponse>(`/agents/${id}`)
}

export const regenerateToken = (id: number) => {
  return request.post<any, ApiResponse<{ agent_token: string }>>(`/agents/${id}/regenerate-token`)
}
