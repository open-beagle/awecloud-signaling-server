import request from '@/utils/request'
import type { LoginRequest, LoginResponse } from '@/types/models'

export const login = (data: LoginRequest) => {
  return request.post<any, LoginResponse>('/api/admin/login', data)
}

export const logout = () => {
  return request.post('/api/admin/logout')
}
