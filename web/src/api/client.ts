import request from '@/utils/request'

export interface Client {
  id: number
  client_id: string
  client_secret?: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface CreateClientRequest {
  client_id: string
}

export interface CreateClientResponse {
  client: Client
  client_secret: string
}

// 获取Client列表
export function getClients() {
  return request<{ clients: Client[] }>({
    url: '/clients',
    method: 'get'
  })
}

// 创建Client
export function createClient(data: CreateClientRequest) {
  return request<CreateClientResponse>({
    url: '/clients',
    method: 'post',
    data
  })
}

// 启用Client
export function enableClient(id: number) {
  return request({
    url: `/clients/${id}/enable`,
    method: 'put'
  })
}

// 禁用Client
export function disableClient(id: number) {
  return request({
    url: `/clients/${id}/disable`,
    method: 'put'
  })
}

// 删除Client
export function deleteClient(id: number) {
  return request({
    url: `/clients/${id}`,
    method: 'delete'
  })
}

// 重新生成Secret
export function regenerateSecret(id: number) {
  return request<{ client_secret: string }>({
    url: `/clients/${id}/regenerate-secret`,
    method: 'post'
  })
}
