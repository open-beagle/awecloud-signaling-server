import request from '@/utils/request'

// Tailscale 状态
export interface TailscaleStatus {
  headscale_url: string
  headscale_online: boolean
  namespace: string
  total_nodes: number
  online_nodes: number
  agents_connected: number
  clients_connected: number
}

// Headscale 节点
export interface HeadscaleNode {
  id: string
  machine_key: string
  node_key: string
  name: string
  given_name: string
  ip_addresses: string[]
  online: boolean
  last_seen: string
  expiry: string
  created_at: string
  register_method: string
}

// 获取 Tailscale 状态
export function getTailscaleStatus() {
  return request({
    url: '/api/v1/admin/tailscale/status',
    method: 'get'
  })
}

// 同步 Tailscale 状态
export function syncTailscale() {
  return request({
    url: '/api/v1/admin/tailscale/sync',
    method: 'post'
  })
}

// 获取 Headscale 节点列表
export function getTailscaleNodes() {
  return request({
    url: '/api/v1/admin/tailscale/nodes',
    method: 'get'
  })
}
