import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

// 部署 Token 状态
export type DeployTokenStatus = 'pending' | 'bound' | 'expired'

// 部署 Token 列表项
export interface DeployToken {
  id: number
  device_name: string
  status: DeployTokenStatus
  device_fingerprint?: string
  created_by: number
  created_by_name?: string
  created_at: string
  expires_at: string
  bound_at?: string
  last_used_at?: string
}

// 创建部署 Token 请求
export interface CreateDeployTokenRequest {
  device_name: string
}

// 创建部署 Token 响应
export interface CreateDeployTokenResponse {
  token: string
  expires_at: string
  install_command: string
}

// 获取部署命令响应
export interface GetDeployCommandResponse {
  install_command: string
}

// 生成部署 Token（支持 ID 或用户名）
export const createDeployToken = (userIdentifier: number | string, data: CreateDeployTokenRequest) => {
  return request.post<any, ApiResponse<CreateDeployTokenResponse>>(
    `/api/v1/admin/users/${userIdentifier}/deploy-token`,
    data
  )
}

// 获取部署 Token 列表（支持 ID 或用户名）
export const getDeployTokens = (userIdentifier: number | string, params?: { page?: number; size?: number }) => {
  return request.get<any, PagedResponse<DeployToken[]>>(
    `/api/v1/admin/users/${userIdentifier}/deploy-tokens`,
    { params }
  )
}

// 获取部署命令
export const getDeployCommand = (tokenId: number) => {
  return request.get<any, ApiResponse<GetDeployCommandResponse>>(
    `/api/v1/admin/deploy-tokens/${tokenId}/command`
  )
}

// 撤销部署 Token
export const revokeDeployToken = (tokenId: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/deploy-tokens/${tokenId}`)
}
