import request from '@/utils/request'
import type { ApiResponse } from '@/types/models'

// 版本信息
export interface VersionInfo {
  agent: string
  desktop: string
  endpoint: string
}

// 获取最新版本信息
export const getLatestVersions = () => {
  return request.get<any, ApiResponse<VersionInfo>>('/api/v1/admin/version/latest')
}
