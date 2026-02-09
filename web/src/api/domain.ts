import request from '@/utils/request'
import type { PagedResponse, ApiResponse } from '@/types/models'

// 域名类型
export type DomainType = 'agent_ssh' | 'agent_k8sapi' | 'agent_k8s_service' | 'agent_service' | 'endpoint_ssh' | 'endpoint_k8sapi' | 'endpoint_k8s_service'

// 域名状态
export type DomainStatus = 'online' | 'offline'

// 域名列表项
export interface DomainItem {
  id: number
  domain: string
  type: DomainType
  agent_user_id: number
  agent_name: string
  endpoint_id?: string
  target_port?: number
  namespace?: string
  service_name?: string
  status: DomainStatus
  created_at: string
}

// 获取域名列表
export const getDomains = (params?: {
  page?: number
  size?: number
  search?: string
  type?: string
  agent_id?: string
  status?: string
}) => {
  return request.get<any, PagedResponse<DomainItem[]>>('/api/v1/admin/domains', { params })
}

// DNS 域名解析
export const resolveDomain = (domain: string) => {
  return request.get<any, ApiResponse>('/api/v1/client/dns/resolve', { params: { domain } })
}
