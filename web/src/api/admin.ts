import request from '@/utils/request'
import type { LoginRequest, LoginResponse } from '@/types/models'

export const login = (data: LoginRequest) => {
  return request.post<any, LoginResponse>('/api/v1/admin/auth/login', data)
}

export const logout = () => {
  return request.post('/api/v1/admin/auth/logout')
}
