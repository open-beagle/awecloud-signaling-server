import request from '@/utils/request'
import type { PagedResponse } from '@/types/models'

export type TechnicalResourceType = 'agent' | 'endpoint'
export type TechnicalResourceState = 'pending' | 'registered' | 'disabled' | 'retired' | 'deleted'
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
  display_name: string
  domain_label: string
  domain_namespace: string
  hostname: string
  host_domain_label: string
  hostname_source?: 'reported' | 'legacy_name'
  parent_hostname?: string
  version?: string
  updater_protocol?: string
  ssh_enabled: boolean
  container_ssh_enabled: boolean
  k8s_enabled: boolean
  svc_enabled: boolean
  endpoint_access_enabled: boolean
  endpoint_count: number
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

export interface TechnicalResourceBinding {
  id: string
  technical_resource_id: string
  source_type: 'legacy_node' | 'legacy_endpoint'
  source_id: string
  credential_revision: number
  enabled: boolean
  reason: string
  row_version: number
  created_at: string
  updated_at: string
}

export interface TechnicalResourceDetail {
  resource: TechnicalResource
  bindings: TechnicalResourceBinding[]
  endpoints: TechnicalResource[]
}

export interface TechnicalResourceCapabilities {
  ssh_enabled: boolean
  ssh_users?: string[]
  k8s_enabled: boolean
  k8s_api_address?: string
  svc_enabled: boolean
  svc_label_selector?: string
  svc_namespaces?: string[]
  endpoint_access_enabled: boolean
  k8s_listen_port?: number
  svc_listen_port_base?: number
  endpoint_listen_port?: number
}

export interface ProviderRelease {
  id: string
  component: 'agent' | 'endpoint'
  version: string
  channel: string
  release_notes?: string
  published_at?: string
}

export interface ProviderUpdateTask {
  id: string
  desired_version: string
  status: string
  last_error_message?: string
  created_at: string
  updated_at: string
}

export interface TechnicalResourceDeleteCheck {
  allowed: boolean
  blockers: Array<{ code: string; message: string; count: number }>
}

export interface DeploymentCredentialResult {
  credential: { id: string; technical_resource_id: string; token: string; expires_at: string }
  install_command: string
}

export interface ProviderMutationResult<T> {
  result: T
  row_version: number
}

export type ProviderMutationResponse<T> = { success: boolean; data: ProviderMutationResult<T> | T }

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
  access_domain?: string
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
  platform_resource_display_name?: string
  platform_resource_stable_key?: string
  platform_resource_access_domain?: string
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

export const createProviderAgent = (providerId: string, runtimeName: string, domainLabel: string, reason: string) =>
  request.post<any, ProviderMutationResponse<TechnicalResource>>('/api/v1/management/provider/technical-resources', {
    type: 'agent', runtime_name: runtimeName, domain_label: domainLabel, credential_revision: 1, reason,
  }, { headers: { ...providerHeaders(providerId), 'Idempotency-Key': crypto.randomUUID() } })

export const createProviderDeploymentCredential = (providerId: string, resourceId: string, name: string, ttlMinutes: number) =>
  request.post<any, { success: boolean; data: DeploymentCredentialResult }>(`/api/v1/management/provider/technical-resources/${resourceId}/deployment-credentials`, {
    name, ttl_minutes: ttlMinutes,
  }, { headers: providerHeaders(providerId) })

export const getProviderTechnicalResource = (providerId: string, resourceId: string) =>
  request.get<any, { success: boolean; data: TechnicalResourceDetail }>(`/api/v1/management/provider/technical-resources/${resourceId}`, {
    headers: providerHeaders(providerId),
  })

export const updateProviderAgentDomainLabel = (providerId: string, resource: TechnicalResource, domainLabel: string, reason: string) =>
  request.patch<any, { success: boolean; data: TechnicalResource }>(`/api/v1/management/provider/technical-resources/${resource.id}/domain-label`, {
    domain_label: domainLabel, reason,
  }, { headers: { ...providerHeaders(providerId), 'If-Match': String(resource.row_version) } })

export const updateProviderAgentDisplayName = (providerId: string, resource: TechnicalResource, displayName: string) =>
  request.patch<any, { success: boolean; data: TechnicalResource }>(`/api/v1/management/provider/technical-resources/${resource.id}/display-name`, {
    display_name: displayName,
  }, { headers: { ...providerHeaders(providerId), 'If-Match': String(resource.row_version) } })

export const updateProviderAgentHostDomainLabel = (providerId: string, resource: TechnicalResource, hostDomainLabel: string) =>
  request.patch<any, { success: boolean; data: TechnicalResource }>(`/api/v1/management/provider/technical-resources/${resource.id}/host-domain-label`, {
    host_domain_label: hostDomainLabel,
  }, { headers: { ...providerHeaders(providerId), 'If-Match': String(resource.row_version) } })

export const getProviderTechnicalResourceCapabilities = (providerId: string, resourceId: string) =>
  request.get<any, { success: boolean; data: TechnicalResourceCapabilities }>(`/api/v1/management/provider/technical-resources/${resourceId}/capabilities`, {
    headers: providerHeaders(providerId),
  })

export const updateProviderTechnicalResourceCapabilities = (providerId: string, resource: TechnicalResource, data: TechnicalResourceCapabilities) =>
  request.patch<any, { success: boolean; data: TechnicalResource }>(`/api/v1/management/provider/technical-resources/${resource.id}/config`, data, {
    headers: { ...providerHeaders(providerId), 'If-Match': String(resource.row_version) },
  })

export const setProviderTechnicalResourceLifecycle = (providerId: string, resource: TechnicalResource, action: 'maintenance' | 'resume' | 'retire', reason: string) =>
  request.post<any, { success: boolean; data: TechnicalResource }>(`/api/v1/management/provider/technical-resources/${resource.id}/${action}`, { reason }, {
    headers: { ...providerHeaders(providerId), 'If-Match': String(resource.row_version) },
  })

export const getProviderTechnicalResourceReleases = (providerId: string, resourceId: string) =>
  request.get<any, { success: boolean; data: ProviderRelease[] }>(`/api/v1/management/provider/technical-resources/${resourceId}/releases`, {
    headers: providerHeaders(providerId),
  })

export const getProviderTechnicalResourceUpdateTasks = (providerId: string, resourceId: string) =>
  request.get<any, { success: boolean; data: ProviderUpdateTask[] }>(`/api/v1/management/provider/technical-resources/${resourceId}/update-tasks`, {
    headers: providerHeaders(providerId),
  })

export const createProviderTechnicalResourceUpdateTask = (providerId: string, resourceId: string, releaseId: string, force: boolean, reason: string) =>
  request.post<any, { success: boolean; data: ProviderUpdateTask }>(`/api/v1/management/provider/technical-resources/${resourceId}/update-tasks`, {
    release_id: releaseId, force, reason,
  }, { headers: { ...providerHeaders(providerId), 'Idempotency-Key': crypto.randomUUID() } })

export const checkProviderTechnicalResourceDelete = (providerId: string, resourceId: string) =>
  request.get<any, { success: boolean; data: TechnicalResourceDeleteCheck }>(`/api/v1/management/provider/technical-resources/${resourceId}/delete-check`, {
    headers: providerHeaders(providerId),
  })

export const deleteProviderTechnicalResource = (providerId: string, resource: TechnicalResource, reason: string) =>
  request.delete<any, { success: boolean; data: { result: TechnicalResource; row_version: number } }>(`/api/v1/management/provider/technical-resources/${resource.id}`, {
    data: { reason },
    headers: { ...providerHeaders(providerId), 'If-Match': String(resource.row_version), 'Idempotency-Key': crypto.randomUUID() },
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
