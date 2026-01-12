import request from '@/utils/request'

export interface Client {
  id: number
  client_id: string
  client_secret?: string
  enabled: boolean
  tailscale_ip?: string
  created_at: string
  updated_at: string
}

export interface Desktop {
  id: number
  client_id: number
  device_name: string
  tailscale_ip?: string
  online: boolean
  last_seen_at: string
  created_at: string
}

export interface ClientDetail extends Client {
  desktops?: Desktop[]
  groups?: string[]
  permissions?: ServicePermission[]
}

export interface ServicePermission {
  id: number
  service_id: number
  service_name: string
  agent_name: string
  access_address: string
  granted_at: string
  grant_type: 'direct' | 'group'
  group_name?: string
}

export interface CreateClientRequest {
  client_id: string
}

export interface CreateClientResponse {
  client: Client
  client_secret: string
}

export interface ClientsResponse {
  clients: Client[]
}

export interface RegenerateSecretResponse {
  client_secret: string
}

// 获取Client列表
export function getClients() {
  return request<ClientsResponse>({
    url: '/api/v1/admin/clients',
    method: 'get'
  })
}

// 获取Client详情
export function getClientDetail(id: number) {
  return request<ClientDetail>({
    url: `/api/v1/admin/clients/${id}`,
    method: 'get'
  })
}

// 创建Client
export function createClient(data: CreateClientRequest) {
  return request<CreateClientResponse>({
    url: '/api/v1/admin/clients',
    method: 'post',
    data
  })
}

// 启用Client
export function enableClient(id: number) {
  return request({
    url: `/api/v1/admin/clients/${id}/enable`,
    method: 'put'
  })
}

// 禁用Client
export function disableClient(id: number) {
  return request({
    url: `/api/v1/admin/clients/${id}/disable`,
    method: 'put'
  })
}

// 删除Client
export function deleteClient(id: number) {
  return request({
    url: `/api/v1/admin/clients/${id}`,
    method: 'delete'
  })
}

// 重新生成Secret
export function regenerateSecret(id: number) {
  return request<RegenerateSecretResponse>({
    url: `/api/v1/admin/clients/${id}/regenerate-secret`,
    method: 'post'
  })
}

// 注销设备
export function revokeDesktop(desktopId: number) {
  return request({
    url: `/api/v1/admin/desktops/${desktopId}`,
    method: 'delete'
  })
}
