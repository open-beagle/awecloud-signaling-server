import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

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
  rdp_mode?: string
  rdp_allow_share?: boolean
  rdp_admin_disabled?: boolean
  rdp_local_ready?: boolean
  rdp_local_port?: number
  rdp_tunnel_exposed?: boolean
  active_grant_count?: number
}

export interface TenantDeviceRDPGrant {
  id: number
  tenant_id: string
  target_node_id: number
  owner_user_id: number
  grantee_user_id: number
  grantee_user_name: string
  grantee_alias?: string
  created_by_name?: string
  reason?: string
  status: 'active' | 'expired' | 'revoked' | string
  expires_at: string
  revoked_at?: string
  revoke_reason?: string
  created_at: string
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

export const getTenantDeviceRDPGrants = (tenantId: string, nodeId: number) =>
  request.get<any, ApiResponse<TenantDeviceRDPGrant[]>>(`/api/v1/admin/tenants/${tenantId}/member-devices/${nodeId}/rdp-grants`, {
    headers: { 'X-Tenant-ID': tenantId },
  })

export const createTenantDeviceRDPGrant = (
  tenantId: string,
  nodeId: number,
  data: { grantee_user_id: number; duration_minutes?: number; expires_at?: string; reason?: string }
) =>
  request.post<any, ApiResponse<TenantDeviceRDPGrant>>(`/api/v1/admin/tenants/${tenantId}/member-devices/${nodeId}/rdp-grants`, data, {
    headers: { 'X-Tenant-ID': tenantId },
  })

export const revokeTenantDeviceRDPGrant = (tenantId: string, nodeId: number, grantId: number, reason?: string) =>
  request.delete<any, ApiResponse>(`/api/v1/admin/tenants/${tenantId}/member-devices/${nodeId}/rdp-grants/${grantId}`, {
    params: reason ? { reason } : undefined,
    headers: { 'X-Tenant-ID': tenantId },
  })

export const getTenantAuditLogs = (tenantId: string, params: { search?: string; action_type?: string; page: number; size: number }) =>
  request.get<any, PagedResponse<TenantAuditLog[]>>(`/api/v1/admin/tenants/${tenantId}/audit-logs`, {
    params,
    headers: { 'X-Tenant-ID': tenantId },
  })
