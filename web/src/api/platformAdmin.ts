import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

export type PlatformRole = 'platform_admin' | 'platform_viewer' | 'none'

export interface PlatformAdminAccount {
  id: number
  username: string
  platform_role: PlatformRole
  enabled: boolean
  created_at: string
  updated_at: string
}

export const getPlatformAdmins = (params: { search?: string; page: number; size: number }) =>
  request.get<any, PagedResponse<PlatformAdminAccount[]>>('/api/v1/admin/platform-admins', { params })

export const createPlatformAdmin = (data: { username: string; password: string; platform_role: PlatformRole; enabled: boolean }) =>
  request.post<any, ApiResponse<PlatformAdminAccount>>('/api/v1/admin/platform-admins', data)

export const updatePlatformAdmin = (id: number, data: { platform_role: PlatformRole; enabled: boolean }) =>
  request.put<any, ApiResponse<PlatformAdminAccount>>(`/api/v1/admin/platform-admins/${id}`, data)
