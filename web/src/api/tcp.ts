import request from '@/utils/request'
import type { TCPService, TCPServiceConfig } from '@/types/models'

// 获取TCP服务列表
export const getTCPServices = (params?: { agent_id?: number; enabled?: boolean }) => {
  return request.get<{ data: TCPService[] }>('/api/v1/admin/tcp-services', { params })
}

// 创建TCP服务
export const createTCPService = (data: {
  service_name: string
  agent_id: number
  local_ip: string
  local_port: number
  description?: string
  access_control?: string
}) => {
  return request.post<{ data: TCPService }>('/api/v1/admin/tcp-services', data)
}

// 更新TCP服务
export const updateTCPService = (id: number, data: {
  description?: string
  access_control?: string
  ip_whitelist?: string
}) => {
  return request.put(`/api/v1/admin/tcp-services/${id}`, data)
}

// 删除TCP服务
export const deleteTCPService = (id: number) => {
  return request.delete(`/api/v1/admin/tcp-services/${id}`)
}

// 启用TCP服务
export const enableTCPService = (id: number) => {
  return request.put(`/api/v1/admin/tcp-services/${id}/enable`)
}

// 禁用TCP服务
export const disableTCPService = (id: number) => {
  return request.put(`/api/v1/admin/tcp-services/${id}/disable`)
}

// 获取TCP服务配置
export const getTCPServiceConfig = () => {
  return request.get<{ data: TCPServiceConfig }>('/api/v1/admin/settings/tcp-service')
}

// 更新TCP服务配置
export const updateTCPServiceConfig = (data: {
  tcp_service_port_start?: number
  tcp_service_max_per_agent?: number
}) => {
  return request.put('/api/v1/admin/settings/tcp-service', data)
}
