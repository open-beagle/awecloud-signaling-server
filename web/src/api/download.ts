import axios from 'axios'

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
  commit_id: string
  published_at: string
  downloads: DesktopLauncherDownload[]
}

export async function getDesktopLaunchers() {
  const response = await axios.get<DesktopLauncherDownloadsResponse>('/api/v1/public/download/desktop', {
    timeout: 10000
  })
  return response.data
}
