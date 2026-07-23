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

export const getTenantMemberDevices = (tenantId: string, params: { search?: string; page: number; size: number }) =>
  request.get<any, PagedResponse<TenantMemberDevice[]>>(`/api/v1/admin/tenants/${tenantId}/member-devices`, { params })

export const getTenantAuditLogs = (tenantId: string, params: { search?: string; action_type?: string; page: number; size: number }) =>
  request.get<any, PagedResponse<TenantAuditLog[]>>(`/api/v1/admin/tenants/${tenantId}/audit-logs`, { params })
