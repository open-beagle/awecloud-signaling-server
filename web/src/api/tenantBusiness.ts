import request from '@/utils/request'
import type { PagedResponse } from '@/types/models'

export interface TenantMemberDevice {
  node_id: number
  user_id: number
  user_name: string
  user_alias?: string
  device_name: string
  hostname?: string
  ip?: string
  version?: string
  last_heartbeat?: string
  online: boolean
}

export interface TenantAuditLog {
  id: number
  actor_admin_id: number
  actor_username: string
  platform_role: string
  tenant_role: string
  required_permission: string
  permission_revision: number
  action_type: string
  target_type: string
  target_id: string
  target_name: string
  request_id: string
  source_ip: string
  detail?: string
  created_at: string
}

export type TenantManagementRole = 'tenant_admin' | 'security_auditor' | 'tenant_viewer'

export interface TenantManagementMembership {
  id: string
  user_id: number
  username: string
  display_name: string
  user_enabled: boolean
  role: TenantManagementRole
  enabled: boolean
  valid_from: string
  expires_at?: string
  permission_revision: number
  reason: string
  row_version: number
  created_at: string
  updated_at: string
}

const tenantManagementHeaders = (tenantId: string) => ({
  'X-Management-Scope-Type': 'tenant',
  'X-Management-Scope-ID': tenantId,
})

export const getTenantManagementMemberships = (tenantId: string, params: { search?: string; role?: string; state?: string; page: number; size: number }) =>
  request.get<any, PagedResponse<TenantManagementMembership[]>>(`/api/v1/management/tenants/${tenantId}/management-memberships`, {
    params,
    headers: tenantManagementHeaders(tenantId),
  })

export const getTenantMemberDevices = (tenantId: string, params: { search?: string; page: number; size: number }) =>
  request.get<any, PagedResponse<TenantMemberDevice[]>>(`/api/v1/admin/tenants/${tenantId}/member-devices`, {
    params,
    headers: { 'X-Tenant-ID': tenantId },
  })

export const getTenantAuditLogs = (tenantId: string, params: { search?: string; action_type?: string; page: number; size: number }) =>
  request.get<any, PagedResponse<TenantAuditLog[]>>(`/api/v1/admin/tenants/${tenantId}/audit-logs`, {
    params,
    headers: { 'X-Tenant-ID': tenantId },
  })
