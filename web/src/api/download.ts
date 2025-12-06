import request from '@/utils/request'

export interface DownloadItem {
  version: string
  download_url: string
  filename: string
  os: string
  arch: string
}

export interface DownloadsResponse {
  success: boolean
  version?: string
  downloads?: Record<string, DownloadItem>
  message?: string
}

// 获取所有平台的下载列表
export function getDownloads() {
  return request<DownloadsResponse>({
    url: '/api/v1/public/download/desktop/versions',
    method: 'get'
  })
}

// 获取当前系统推荐的下载信息
export function getRecommendedDownload() {
  return request<{
    success: boolean
    version?: string
    download_url?: string
    filename?: string
    os?: string
    arch?: string
    message?: string
  }>({
    url: '/api/v1/public/download/desktop',
    method: 'get'
  })
}
