import request from '@/utils/request'

export interface SystemConfig {
  id: number
  client_download_url: string
  desktop_min_version: string
  derp_url?: string
  stun_port?: number
  ip_prefix?: string
  auth_key_expiry_hours?: number
  created_at: string
  updated_at: string
}

// 获取系统配置
export function getSystemConfig() {
  return request<{ success: boolean; data: SystemConfig }>({
    url: '/api/v1/admin/system/config',
    method: 'get'
  })
}

// 更新系统配置
export function updateSystemConfig(data: {
  client_download_url?: string
  desktop_min_version?: string
  derp_url?: string
  stun_port?: number
  ip_prefix?: string
  auth_key_expiry_hours?: number
}) {
  return request<{ success: boolean; message: string; data: SystemConfig }>({
    url: '/api/v1/admin/system/config',
    method: 'put',
    data
  })
}

// 获取公开的系统配置（不需要认证）
export function getPublicSystemConfig() {
  return request<{ success: boolean; data: { client_download_url: string } }>({
    url: '/api/v1/public/system/config',
    method: 'get'
  })
}
