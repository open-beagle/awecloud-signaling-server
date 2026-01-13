import request from '@/utils/request'
import type { Agent, AgentDetail, ApiResponse } from '@/types/models'

export const getAgents = () => {
  return request.get<any, ApiResponse<Agent[]>>('/api/v1/admin/agents')
}

export const getAgent = (id: number) => {
  return request.get<any, ApiResponse<AgentDetail>>(`/api/v1/admin/agents/${id}`)
}

// 获取 Agent 实时信息
export interface AgentRealtimeInfo {
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

export const getAgentRealtime = (id: number) => {
  return request.get<any, ApiResponse<AgentRealtimeInfo>>(`/api/v1/admin/agents/${id}/realtime`)
}

// 创建 Agent 响应
export interface CreateAgentResponse {
  id: number
  name: string
  secret: string
}

export const createAgent = (data: { name: string; alias?: string }) => {
  return request.post<any, ApiResponse<CreateAgentResponse>>('/api/v1/admin/agents', data)
}

export const updateAgent = (id: number, data: { alias?: string }) => {
  return request.put<any, ApiResponse<Agent>>(`/api/v1/admin/agents/${id}`, data)
}

export const deleteAgent = (id: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/agents/${id}`)
}

export const regenerateSecret = (id: number) => {
  return request.post<any, ApiResponse<{ secret: string }>>(`/api/v1/admin/agents/${id}/regenerate-secret`)
}
