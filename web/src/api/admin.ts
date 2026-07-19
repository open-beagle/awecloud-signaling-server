import request from '@/utils/request'
import type { ApiResponse, LoginRequest, LoginResponse } from '@/types/models'

export interface AdminProfile {
  id: number
  username: string
  role: 'admin' | 'viewer' | 'tenant_admin'
  created_at: string
}

export const login = (data: LoginRequest) => {
  return request.post<any, LoginResponse>('/api/v1/admin/auth/login', data)
}

export const logout = () => {
  return request.post('/api/v1/admin/auth/logout')
}

export const getMe = () => {
  return request.get<any, ApiResponse<AdminProfile>>('/api/v1/admin/auth/me')
}
