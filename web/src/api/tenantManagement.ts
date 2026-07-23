import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

export interface TenantSettings {
  id: string
  key: string
  name: string
  status: 'active' | 'suspended'
  created_at: string
  updated_at: string
}

export interface TenantAdminMembership {
  id: number
  admin_id: number
  admin_username: string
  admin_enabled: boolean
  tenant_id: string
  tenant_key: string
  tenant_name: string
  tenant_status: 'active' | 'suspended'
  role: 'tenant_admin' | 'security_auditor' | 'tenant_viewer'
  enabled: boolean
  expires_at?: string
  permission_revision: number
  created_at: string
  updated_at: string
}

export interface TenantAdminOption {
  id: number
  username: string
  role: string
  enabled: boolean
}

export interface TenantAdminMembershipInput {
  admin_id?: number
  tenant_id?: string
  role: TenantAdminMembership['role']
  enabled: boolean
  expires_at?: string | null
}

export const getTenantSettings = (tenantId: string) =>
  request.get<any, ApiResponse<TenantSettings>>(`/api/v1/admin/tenants/${tenantId}/settings`, { headers: { 'X-Tenant-ID': tenantId } })

export const updateTenantSettings = (tenantId: string, data: { name: string }) =>
  request.put<any, ApiResponse<TenantSettings>>(`/api/v1/admin/tenants/${tenantId}/settings`, data, { headers: { 'X-Tenant-ID': tenantId } })

export const getTenantAdminMemberships = (params: { search?: string; tenant_id?: string; role?: string; page: number; size: number }) =>
  request.get<any, PagedResponse<TenantAdminMembership[]>>('/api/v1/admin/tenant-admin-memberships', { params })

export const getTenantAdminOptions = () =>
  request.get<any, ApiResponse<TenantAdminOption[]>>('/api/v1/admin/tenant-admin-memberships/options')

export const createTenantAdminMembership = (data: TenantAdminMembershipInput) =>
  request.post<any, ApiResponse<TenantAdminMembership>>('/api/v1/admin/tenant-admin-memberships', data)

export const updateTenantAdminMembership = (id: number, data: TenantAdminMembershipInput) =>
  request.put<any, ApiResponse<TenantAdminMembership>>(`/api/v1/admin/tenant-admin-memberships/${id}`, data)
