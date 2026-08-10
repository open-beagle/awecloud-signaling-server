import request from '@/utils/request'

export interface DesktopLauncherDownload {
  os: string
  arch: string
  package_type: string
  filename: string
  download_url: string
  size: number
}

export interface DesktopLauncherDownloadsResponse {
  version: string
  published_at: string | null
  downloads: DesktopLauncherDownload[]
}

export function getDesktopLaunchers() {
  return request<DesktopLauncherDownloadsResponse>({
    url: '/api/v1/public/download/desktop',
    method: 'get'
  })
}
