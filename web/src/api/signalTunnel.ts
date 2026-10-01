import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

export interface TunnelPortMapping {
  resource_id: string
  service_name: string
  target_port: number
  protocol: string
  local_port: number
  allow_k8s_api?: boolean
  description?: string
  namespace?: string
}

export interface TunnelItem {
  id: number
  name: string
  target_agent: string
  status: 'online' | 'offline' | 'pending'
  online: boolean
  device_name?: string
  ip_address?: string
  exposed_ports: Array<{ port: number; name: string; type: 'service' | 'k8sapi' }>
  throughput: string
  latency: string
  created_at: string
}

export interface TunnelDetail extends TunnelItem {
  token_id?: number
  k8s_deploy_yaml?: string
  service_ports: TunnelPortMapping[]
  k8s_api_enabled: boolean
  k8s_api_port: number
}

// 获取 Tunnel 列表
export const getTunnelList = (params?: { search?: string; status?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<TunnelItem[]>>('/api/v1/admin/tunnels', { params })
}

// 创建 Tunnel 实例
export const createTunnelInstance = (data: { name: string; target_agent: string }) => {
  return request.post<any, ApiResponse<any>>('/api/v1/admin/tunnels', data)
}

// 获取可绑定的边缘 Agent 节点列表
export const getAvailableEdgeAgents = () => {
  return request.get<any, ApiResponse<Array<{ name: string; ip: string; online: boolean; status: string }>>>('/api/v1/admin/tunnels/available-agents').catch(() => {
    return request.get<any, any>('/api/v1/admin/nodes', { params: { type: 'agent' } })
  }).then((res: any) => {
    const list = Array.isArray(res.data) ? res.data : (res.data?.items || res.items || [])
    return list.map((item: any) => ({
      name: item.name || item.hostname,
      ip: item.ip || item.ip_address || '127.0.0.1',
      online: item.online !== false,
      status: item.status || 'running'
    }))
  })
}

// 获取 Tunnel 详情
export const getTunnelDetail = (id: number | string) => {
  return request.get<any, ApiResponse<TunnelDetail>>(`/api/v1/admin/tunnels/${id}`)
}

// 保存 Tunnel 端口与 K8s API 配置
export const updateTunnelPorts = (id: number | string, data: { k8s_api_enabled: boolean; k8s_api_port: number; ports: TunnelPortMapping[] }) => {
  return request.put<any, ApiResponse>(`/api/v1/admin/tunnels/${id}/ports`, data)
}

// 删除 Tunnel 实例
export const deleteTunnelInstance = (id: number | string) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/tunnels/${id}`)
}
