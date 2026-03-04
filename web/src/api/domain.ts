import request from '@/utils/request'
import type { PagedResponse, ApiResponse } from '@/types/models'

// 域名能力类型
export type DomainType = 'ssh' | 'k8sapi' | 'k8ssvc'

// 域名状态
export type DomainStatus = 'online' | 'offline'

// 域名列表项
export interface DomainItem {
  id: number
  domain: string
  type: DomainType
  user_id: number
  user_name: string
  node_id?: number
  device_name?: string    // Node 设备名（Hostname）
  endpoint_id?: string    // Endpoint ID（非空表示 Endpoint 域名）
  endpoint_name?: string  // Endpoint 名称
  region?: string         // Region（Agent User 名称）
  agent_name?: string     // Agent 名称（Node 名称）
  target_ip?: string
  target_port?: number
  namespace?: string
  service_name?: string
  ssh_users?: string[]    // SSH 用户列表（仅 ssh 类型）
  status: DomainStatus
  created_at: string
  updated_at: string
}

// 获取域名列表
export const getDomains = (params?: {
  page?: number
  size?: number
  search?: string
  type?: string
  user_id?: string
  status?: string
}) => {
  return request.get<any, PagedResponse<DomainItem[]>>('/api/v1/admin/domains', { params })
}

// DNS 域名解析
export const resolveDomain = (domain: string) => {
  return request.get<any, ApiResponse>('/api/v1/client/dns/resolve', { params: { domain } })
}

// 删除域名记录
export const deleteDomain = (id: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/domains/${id}`)
}

// 刷新域名记录（回填 target_ip）
export const refreshDomains = () => {
  return request.post<any, ApiResponse>('/api/v1/admin/domains/refresh')
}
