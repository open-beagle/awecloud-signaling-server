import request from '@/utils/request'
import type { ApiResponse } from '@/types/models'

export interface OverviewAttentionItem {
  kind: 'tenant' | 'candidate' | 'resource'
  title: string
  detail: string
  status: string
  target_id?: string
  route?: string
  updated_at: string
}

export interface TenantOverview {
  tenant_id: string
  member_count: number
  group_count: number
  resource_count: number
  active_sessions: number
  risk_count: number
  attention: OverviewAttentionItem[]
}

export interface PlatformOverview {
  tenant_count: number
  admin_membership_count: number
  resource_count: number
  agent_count: number
  endpoint_count: number
  high_risk_count: number
  attention: OverviewAttentionItem[]
}

export const getTenantOverview = (tenantId: string) =>
  request.get<any, ApiResponse<TenantOverview>>(`/api/v1/admin/tenants/${tenantId}/overview`, { headers: { 'X-Tenant-ID': tenantId } })

export const getPlatformOverview = () =>
  request.get<any, ApiResponse<PlatformOverview>>('/api/v1/admin/overview/platform')
