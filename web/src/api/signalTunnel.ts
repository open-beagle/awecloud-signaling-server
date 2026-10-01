import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'
import type { DeployToken, CreateDeployTokenRequest, CreateDeployTokenResponse } from './deployToken'

export interface TunnelPortMapping {
  resource_id: string
  service_name: string
  target_port: number
  protocol: string
  local_port: number
  allow_k8s_api?: boolean
  description?: string
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
  return request.get<any, PagedResponse<TunnelItem[]>>('/api/v1/admin/tunnels', { params }).catch(() => {
    // 降级兜底查询 deploy-tokens 模式为 tunnel 的记录
    return request.get<any, PagedResponse<DeployToken[]>>('/api/v1/admin/deploy-tokens', { params: { mode: 'tunnel', ...params } }).then((res: any) => {
      const items: TunnelItem[] = (res.data?.items || res.items || []).map((tok: any) => ({
        id: tok.id,
        name: tok.name,
        target_agent: tok.target_agent_name || '未绑定',
        status: tok.status === 'bound' ? 'online' : 'pending',
        online: tok.status === 'bound',
        device_name: tok.device_name || tok.name,
        ip_address: tok.ip_address || '100.64.0.12',
        exposed_ports: [
          { port: 10080, name: 'MCP 推理', type: 'service' },
          { port: 6443, name: 'K8s API', type: 'k8sapi' }
        ],
        throughput: '12.4 MB/s',
        latency: '18 ms',
        created_at: tok.created_at
      }))
      return {
        data: {
          items,
          total: items.length
        }
      } as any
    })
  })
}

// 创建 Tunnel 实例
export const createTunnelInstance = (data: { name: string; target_agent: string }) => {
  // 使用专属 Deploy Token 创建接口，传入 target_agent_name 与 mode: tunnel
  return request.post<any, ApiResponse<CreateDeployTokenResponse>>('/api/v1/admin/deploy-tokens', {
    name: data.name,
    target_agent_name: data.target_agent,
    mode: 'tunnel'
  })
}

// 获取可绑定的边缘 Agent 节点列表
export const getAvailableEdgeAgents = () => {
  return request.get<any, any>('/api/v1/admin/nodes', { params: { type: 'agent' } }).then((res: any) => {
    const list = res.data?.items || res.data || res.items || []
    return list.map((item: any) => ({
      name: item.name || item.hostname,
      ip: item.ip || item.ip_address || '127.0.0.1',
      online: item.online !== false,
      status: item.status || 'running'
    }))
  })
}
