import request from '@/utils/request'
import type { STCPInstance, ApiResponse } from '@/types/models'

export const getSTCPInstances = () => {
  return request.get<any, ApiResponse<STCPInstance[]>>('/api/v1/admin/stcp-instances')
}

export const createSTCPInstance = (data: {
  agent_id: number
  instance_name: string
  local_ip: string
  local_port: number
  description?: string
}) => {
  return request.post<any, ApiResponse<STCPInstance>>('/api/v1/admin/stcp-instances', data)
}

export const deleteSTCPInstance = (id: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/stcp-instances/${id}`)
}

export const getSTCPAccesses = (instanceId: number) => {
  return request.get<any, ApiResponse<any[]>>(`/api/v1/admin/stcp-instances/${instanceId}/accesses`)
}

export const grantSTCPAccess = (instanceId: number, clientId: number) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/stcp-instances/${instanceId}/grant`, {
    client_id: clientId
  })
}

export const revokeSTCPAccess = (instanceId: number, clientId: number) => {
  return request.post<any, ApiResponse>(`/api/v1/admin/stcp-instances/${instanceId}/revoke`, {
    client_id: clientId
  })
}

export const setAccessType = (instanceId: number, accessType: string, groupId?: number) => {
  return request.put<any, ApiResponse>(`/api/v1/admin/stcp-instances/${instanceId}/access-type`, {
    access_type: accessType,
    group_id: groupId
  })
}
