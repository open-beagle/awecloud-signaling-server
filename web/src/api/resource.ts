import request from '@/utils/request'
import type { ApiResponse } from '@/types/models'

// ========== 资源发现 ==========

// 发现的 K8S Service
export interface DiscoveredK8SService {
  agent_id: number
  agent_name: string
  namespace: string
  service_name: string
  cluster_ip: string
  ports: { name: string; port: number; protocol: string }[]
  labels: Record<string, string>
  endpoint_name: string // 发现来源：为空表示 Agent 本身发现，不为空表示 Endpoint 发现
}

// 获取发现的 K8S Service 列表
export const getDiscoveredK8SServices = (params?: { search?: string; agent_id?: number }) => {
  return request.get<any, ApiResponse<DiscoveredK8SService[]>>('/api/v1/admin/resources/k8s-services', { params })
}

// 触发 Agent 立即上报 K8S Service 发现数据
export const syncK8SServiceDiscovery = () => {
  return request.post<any, ApiResponse<null>>('/api/v1/admin/resources/sync')
}
