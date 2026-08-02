import request from '@/utils/request'
import type { PagedResponse } from '@/types/models'

export type TechnicalResourceType = 'agent' | 'endpoint'
export type TechnicalResourceState = 'pending' | 'registered' | 'disabled' | 'retired'
export type ResourceHealthState = 'unknown' | 'online' | 'degraded' | 'offline'
export type SupplyResourceType = 'host' | 'kubernetes'
export type SupplyCandidateState = 'observed' | 'pending_review' | 'accepted' | 'linked' | 'conflict' | 'rejected'
export type IdentityQuality = 'strong' | 'insufficient' | 'collision'
export type PlatformResourceState = 'draft' | 'active' | 'suspended' | 'retired'
export type ResourceScopeType = 'cluster' | 'namespace'
export type ResourceScopeState = 'draft' | 'active' | 'allocatable' | 'suspended' | 'retired'
export type ResourceScopeIsolationMode = '' | 'namespace_isolated' | 'reviewed_shared'

export interface TechnicalResource {
  id: string
  provider_id: string
  type: TechnicalResourceType
  stable_key: string
  parent_id?: string
  lifecycle_state: TechnicalResourceState
  health_state: ResourceHealthState
  credential_revision: number
  source_epoch?: string
  last_sequence: number
  last_received_at?: string
  lease_expires_at?: string
  config_revision: number
  observed_revision: number
  row_version: number
  created_at: string
  updated_at: string
}

export interface SupplyCandidate {
  id: string
  provider_id: string
  technical_resource_id: string
  resource_type: SupplyResourceType
  stable_key: string
  identity_quality: IdentityQuality
  first_observed_at: string
  last_observed_at: string
  lease_expires_at: string
  review_state: SupplyCandidateState
  conflict_code?: string
  opaque_conflict_id?: string
  reviewed_by_user_id?: number
  reviewed_at?: string
  row_version: number
  created_at: string
  updated_at: string
}

export interface PlatformResource {
  id: string
  provider_id: string
  type: SupplyResourceType
  stable_key: string
  display_name: string
  lifecycle_state: PlatformResourceState
  health_state: ResourceHealthState
  capability_revision: number
  allocatable_scope_count: number
  row_version: number
  created_at: string
  updated_at: string
}

export interface ResourceScope {
  id: string
  provider_id: string
  platform_resource_id: string
  type: ResourceScopeType
  stable_key: string
  parent_id?: string
  namespace_observation_id?: string
  lifecycle_state: ResourceScopeState
  isolation_mode: ResourceScopeIsolationMode
  config_revision: number
  evidence_revision: number
  row_version: number
  created_at: string
  updated_at: string
}

export interface ProviderSupplyListParams {
  search?: string
  type?: string
  state?: string
  page: number
  size: number
}

export type ProviderManagementRole = 'provider_admin' | 'provider_operator' | 'provider_viewer'

export interface ProviderMembership {
  id: string
  user_id: number
  username: string
  display_name: string
  user_enabled: boolean
  role: ProviderManagementRole
  enabled: boolean
  valid_from: string
  expires_at?: string
  permission_revision: number
  reason: string
  row_version: number
  created_at: string
  updated_at: string
}

export interface ProviderAuditLog {
  id: number
  actor_admin_id: number
  actor_username: string
  actor_user_id: number
  effective_user_id: number
  simulation_session_id: string
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

export interface ProviderGovernanceListParams {
  search?: string
  role?: string
  state?: string
  action_type?: string
  page: number
  size: number
}

const providerHeaders = (providerId: string) => ({
  'X-Management-Scope-Type': 'provider',
  'X-Management-Scope-ID': providerId,
})

export const getProviderTechnicalResources = (providerId: string, params: ProviderSupplyListParams) =>
  request.get<any, PagedResponse<TechnicalResource[]>>('/api/v1/management/provider/technical-resources', {
    params,
    headers: providerHeaders(providerId),
  })

export const getProviderSupplyCandidates = (providerId: string, params: ProviderSupplyListParams) =>
  request.get<any, PagedResponse<SupplyCandidate[]>>('/api/v1/management/provider/supply-candidates', {
    params,
    headers: providerHeaders(providerId),
  })

export const getProviderPlatformResources = (providerId: string, params: ProviderSupplyListParams) =>
  request.get<any, PagedResponse<PlatformResource[]>>('/api/v1/management/provider/resources', {
    params,
    headers: providerHeaders(providerId),
  })

export const getProviderResourceScopes = (providerId: string, params: ProviderSupplyListParams) =>
  request.get<any, PagedResponse<ResourceScope[]>>('/api/v1/management/provider/scopes', {
    params,
    headers: providerHeaders(providerId),
  })

export const getProviderMemberships = (providerId: string, params: ProviderGovernanceListParams) =>
  request.get<any, PagedResponse<ProviderMembership[]>>('/api/v1/management/provider/memberships', {
    params,
    headers: providerHeaders(providerId),
  })

export const getProviderAuditLogs = (providerId: string, params: ProviderGovernanceListParams) =>
  request.get<any, PagedResponse<ProviderAuditLog[]>>('/api/v1/management/provider/audit-logs', {
    params,
    headers: providerHeaders(providerId),
  })
