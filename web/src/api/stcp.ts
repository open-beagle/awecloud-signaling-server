import request from '@/utils/request'
import type { STCPInstance, ApiResponse } from '@/types/models'

export const getSTCPInstances = () => {
  return request.get<any, ApiResponse<STCPInstance[]>>('/stcp-instances')
}

export const createSTCPInstance = (data: {
  agent_id: number
  instance_name: string
  local_ip: string
  local_port: number
  description?: string
}) => {
  return request.post<any, ApiResponse<STCPInstance>>('/stcp-instances', data)
}

export const deleteSTCPInstance = (id: number) => {
  return request.delete<any, ApiResponse>(`/stcp-instances/${id}`)
}
