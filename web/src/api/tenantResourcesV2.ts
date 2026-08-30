import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

export type TenantResourceType = 'host_ssh' | 'container_ssh' | 'container_service'
export type TenantResourceVisibility = 'pending' | 'visible' | 'hidden' | 'retired'
export type TenantResourceAvailability = 'unknown' | 'available' | 'degraded' | 'unavailable'

export interface TenantResourceV2 {
  resource_id: string
  type: TenantResourceType
  display_name: string
  description?: string
  visibility_state: TenantResourceVisibility
  availability_state: TenantResourceAvailability
  revision: number
  row_version: number
  namespace_scope_id?: string
  namespace_name?: string
  target_revision?: number
  observation_revision?: number
  ready: boolean
  service_uid?: string
  service_name?: string
  port_name?: string
  port_number?: number
  protocol?: string
  workload_uid?: string
  workload_kind?: string
  workload_name?: string
  pod_uid?: string
  pod_name?: string
  container_name?: string
  identity_quality?: string
  agent_node_id?: number
  ssh_domain?: string
  target_ip?: string
  target_port?: number
  ssh_users?: string[]
  created_at: string
  updated_at: string
}

export interface TenantResourceListResult {
  items: TenantResourceV2[]
  next_cursor?: string
}

export interface TenantResourceListParams {
  type?: string
  visibility?: string
  availability?: string
  namespace?: string
  query?: string
  cursor?: string
}

export interface TenantGrantCreateInput {
  resource_id: string
  subject: {
    type: 'user' | 'group'
    user_id?: number
    group_id?: number
  }
  actions: Array<'shell' | 'connect'>
  valid_from?: string
  expires_at?: string
  max_session_seconds?: number
}

export type TenantGrantStatus = 'enabled' | 'suspended' | 'revoked' | 'expired'
export interface TenantGrantV2 {
  id: string
  resource_id: string
  resource_name?: string
  subject_type: 'user' | 'group'
  subject_user_id?: number
  subject_group_id?: number
  subject_name?: string
  actions: string[]
  valid_from: string
  expires_at?: string
  max_session_seconds: number
  status: TenantGrantStatus
  revision: number
  row_version: number
  revoked_at?: string
  revoke_reason?: string
  created_at: string
  updated_at: string
}

export type ResourceSessionStatus = 'authorizing' | 'active' | 'ending' | 'ended' | 'terminated' | 'rejected'
export interface ResourceSessionV2 {
  id: string
  resource_id: string
  grant_id: string
  grant_revision: number
  user_id: number
  device_id: number
  session_type: TenantResourceType
  action: string
  authorization_revision: number
  valid_until: string
  status: ResourceSessionStatus
  request_id: string
  started_at: string
  connected_at?: string
  ended_at?: string
  result?: string
  close_reason?: string
  row_version: number
  created_at: string
  updated_at: string
}

const tenantHeaders = (tenantId: string) => ({
  'X-Management-Scope-Type': 'tenant',
  'X-Management-Scope-ID': tenantId,
})

export const getTenantResourcesV2 = (tenantId: string, params: TenantResourceListParams) =>
  request.get<any, ApiResponse<TenantResourceListResult>>(`/api/v1/management/tenants/${tenantId}/resources`, { params, headers: tenantHeaders(tenantId) })

export const getTenantResourceCandidatesV2 = (tenantId: string, params: Omit<TenantResourceListParams, 'visibility'>) =>
  request.get<any, ApiResponse<TenantResourceListResult>>(`/api/v1/management/tenants/${tenantId}/resource-candidates`, { params, headers: tenantHeaders(tenantId) })

export const publishTenantResourceCandidateV2 = (tenantId: string, resource: TenantResourceV2, reason: string) =>
  request.post<any, ApiResponse<{ result: TenantResourceV2; row_version: number }>>(
    `/api/v1/management/tenants/${tenantId}/resource-candidates/${resource.resource_id}/publish`,
    { observation_revision: resource.observation_revision, reason },
    { headers: { ...tenantHeaders(tenantId), 'If-Match': String(resource.row_version), 'Idempotency-Key': crypto.randomUUID() } },
  )

export const getTenantResourceV2 = (tenantId: string, resourceId: string) =>
  request.get<any, ApiResponse<TenantResourceV2>>(`/api/v1/management/tenants/${tenantId}/resources/${resourceId}`, { headers: tenantHeaders(tenantId) })

export const getTenantGrantsV2 = (tenantId: string, params: { resource_id?: string; subject_type?: string; status?: string; page: number; size: number }) =>
  request.get<any, PagedResponse<TenantGrantV2[]>>(`/api/v1/management/tenants/${tenantId}/grants`, { params, headers: tenantHeaders(tenantId) })

export const createTenantGrantV2 = (tenantId: string, data: TenantGrantCreateInput) =>
  request.post<any, ApiResponse<{ result: TenantGrantV2; row_version: number }>>(
    `/api/v1/management/tenants/${tenantId}/grants`,
    data,
    { headers: { ...tenantHeaders(tenantId), 'Idempotency-Key': crypto.randomUUID() } },
  )

export const getTenantSessionsV2 = (tenantId: string, params: { resource_id?: string; user_id?: number; status?: string; page: number; size: number }) =>
  request.get<any, PagedResponse<ResourceSessionV2[]>>(`/api/v1/management/tenants/${tenantId}/sessions`, { params, headers: tenantHeaders(tenantId) })
