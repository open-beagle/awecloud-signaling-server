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
  node_name?: string
  device_name?: string // 设备名（Node.Hostname）
  endpoint_id?: string
  endpoint_name?: string
  target_ip?: string
  target_port?: number
  namespace?: string
  service_name?: string
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
