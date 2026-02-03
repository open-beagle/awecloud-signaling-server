import request from '@/utils/request'

// Client Token 接口
export interface ClientToken {
  id: number
  token: string
  user_id: number
  name: string
  status: 'pending' | 'bound'
  device_fingerprint: string
  device_name: string
  created_at: string
  bound_at: string | null
  last_used_at: string | null
  node_id: number | null
  created_by: number
  created_by_name: string
  user_name: string
}

// 创建 Client Token 请求
export interface CreateClientTokenRequest {
  user_id: number
  name: string
  device_name: string
}

// 创建 Client Token 响应
export interface CreateClientTokenResponse {
  token: string
  name: string
  device_name: string
  env_config: string
}

// 创建 Client Token
export function createClientToken(data: CreateClientTokenRequest) {
  return request<CreateClientTokenResponse>({
    url: '/api/v1/admin/client/token',
    method: 'post',
    data
  })
}

// 获取 Client Token 列表
export function getClientTokens(params?: { user_id?: number; page?: number; page_size?: number }) {
  return request<ClientToken[]>({
    url: '/api/v1/admin/client/tokens',
    method: 'get',
    params
  })
}

// 获取单个 Client Token
export function getClientToken(id: number) {
  return request<ClientToken>({
    url: `/api/v1/admin/client/token/${id}`,
    method: 'get'
  })
}

// 删除 Client Token
export function deleteClientToken(id: number) {
  return request({
    url: `/api/v1/admin/client/token/${id}`,
    method: 'delete'
  })
}
