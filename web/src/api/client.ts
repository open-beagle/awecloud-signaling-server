import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

export interface Client {
  id: number
  name: string
  alias?: string
  desktop_count?: number
  status?: string
  last_online?: string
  created_at?: string
  updated_at?: string
}

export interface Desktop {
  id: number
  client_id: number
  device_name: string
  tunnel_ip?: string
  status?: string
  last_online?: string
  created_at: string
}

export interface ClientDetail {
  id: number
  name: string
  alias?: string
  created_at: string
}

export interface ClientGroupItem {
  id: number
  name: string
}

export interface ServicePermission {
  id: string
  name: string
  agent_name: string
  listen_addr: string
  auth_type: string
  granted_at: string
}

export interface CreateClientRequest {
  name: string
  alias?: string
}

export interface CreateClientResponse {
  id: number
  name: string
  secret: string
}

// 获取 Client 列表
export function getClients(page = 1, size = 20, search = '') {
  return request.get<any, PagedResponse<Client[]>>('/api/v1/admin/clients', {
    params: { page, size, search }
  })
}

// 获取 Client 详情
export function getClientDetail(id: number) {
  return request.get<any, ApiResponse<ClientDetail>>(`/api/v1/admin/clients/${id}`)
}

// 获取 Client 所属分组
export function getClientGroups(id: number) {
  return request.get<any, ApiResponse<ClientGroupItem[]>>(`/api/v1/admin/clients/${id}/groups`)
}

// 获取 Client 的 Desktop 列表
export function getClientDesktops(id: number) {
  return request.get<any, ApiResponse<Desktop[]>>(`/api/v1/admin/clients/${id}/desktops`)
}

// 获取 Client 已授权服务列表
export function getClientServices(id: number) {
  return request.get<any, ApiResponse<ServicePermission[]>>(`/api/v1/admin/clients/${id}/services`)
}

// 创建 Client
export function createClient(data: CreateClientRequest) {
  return request.post<any, ApiResponse<CreateClientResponse>>('/api/v1/admin/clients', data)
}

// 更新 Client
export function updateClient(id: number, data: { alias?: string }) {
  return request.put<any, ApiResponse>(`/api/v1/admin/clients/${id}`, data)
}

// 删除 Client
export function deleteClient(id: number) {
  return request.delete<any, ApiResponse>(`/api/v1/admin/clients/${id}`)
}

// 重新生成 Secret
export function regenerateSecret(id: number) {
  return request.post<any, ApiResponse<{ secret: string }>>(`/api/v1/admin/clients/${id}/regenerate-secret`)
}

// 注销 Desktop
export function logoutDesktop(clientId: number, desktopId: number) {
  return request.post<any, ApiResponse>(`/api/v1/admin/clients/${clientId}/desktops/${desktopId}/logout`)
}

// 删除 Desktop
export function deleteDesktop(clientId: number, desktopId: number) {
  return request.delete<any, ApiResponse>(`/api/v1/admin/clients/${clientId}/desktops/${desktopId}`)
}
