import request from '@/utils/request'

export interface SystemConfig {
  id: number
  client_download_url: string
  desktop_min_version: string
  headscale_public_url?: string
  stun_port?: number
  ip_prefix?: string
  auth_key_expiry_hours?: number
  domain_suffix?: string
  created_at: string
  updated_at: string
}

export interface UpdaterCatalogSyncResult {
  scanned: number
  created: number
  existing: number
  revoked: number
  failed: number
}

export type UpdaterComponent = 'agent' | 'endpoint' | 'desktop'

export interface UpdaterRelease {
  id: string
  component: UpdaterComponent
  version: string
  commit_id: string
  channel: string
  status: 'draft' | 'published' | 'revoked'
  release_notes: string
  min_supported_version: string
  published_at?: string
  created_at: string
  updated_at: string
  artifact_count: number
}

export interface UpdaterArtifact {
  id: string
  release_id: string
  os: string
  arch: string
  role: string
  package_type: string
  filename: string
  download_url: string
  size: number
  sha256: string
  status: 'available' | 'revoked'
}

export interface UpdaterReleaseDetail {
  release: UpdaterRelease
  artifacts: UpdaterArtifact[]
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
  headscale_public_url?: string
  stun_port?: number
  ip_prefix?: string
  auth_key_expiry_hours?: number
  domain_suffix?: string
}) {
  return request<{ success: boolean; message: string; data: SystemConfig }>({
    url: '/api/v1/admin/system/config',
    method: 'put',
    data
  })
}

export function syncUpdaterCatalog() {
  return request<{ success: boolean; message: string; data: UpdaterCatalogSyncResult }>({
    url: '/api/v1/admin/updater/sync',
    method: 'post',
    timeout: 120000
  })
}

export function getUpdaterReleases(component?: UpdaterComponent) {
  return request<{ success: boolean; data: UpdaterRelease[] }>({
    url: '/api/v1/admin/updater/releases',
    method: 'get',
    params: component ? { component } : undefined
  })
}

export function getUpdaterRelease(id: string) {
  return request<{ success: boolean; data: UpdaterReleaseDetail }>({
    url: `/api/v1/admin/updater/releases/${id}`,
    method: 'get'
  })
}

// 获取公开的系统配置（不需要认证）
export function getPublicSystemConfig() {
  return request<{ success: boolean; data: { client_download_url: string } }>({
    url: '/api/v1/public/system/config',
    method: 'get'
  })
}
