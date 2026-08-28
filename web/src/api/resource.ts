import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

export type ResourceType = 'host_ssh' | 'container_ssh' | 'kubernetes_api' | 'database_service' | 'tcp_service'
export type ResourceState = 'pending' | 'available' | 'degraded' | 'draining' | 'stopped' | 'revoked'

export interface Tenant {
  id: string
  key: string
  name: string
  status: 'active' | 'suspended'
  member_count?: number
  resource_count?: number
  created_at: string
  updated_at: string
}

export interface TenantMember {
  user_id: number
  name: string
  alias?: string
  role: string
  enabled: boolean
  expires_at?: string
}

export interface TenantMemberCandidate {
  user_id: number
  name: string
  alias?: string
}

export interface Resource {
  id: string
  tenant_id: string
  tenant_name?: string
  type: ResourceType
  display_name: string
  provider_id?: string
  external_workspace_id?: string
  owner_user_id?: number
  owner_name?: string
  agent_node_id?: number
  cluster_id?: string
  namespace?: string
  pod_name?: string
  pod_uid?: string
  container_name?: string
  shell_profile_id?: string
  target_revision: number
  state: ResourceState
  expires_at?: string
  grant_count?: number
  session_count?: number
  created_at: string
  updated_at: string
}

export interface ResourceTarget {
  id: number
  resource_id: string
  revision: number
  agent_node_id?: number
  cluster_id?: string
  namespace?: string
  pod_name?: string
  pod_uid: string
  container_name: string
  ready: boolean
  observed_at: string
  created_at: string
}

export interface AccessGrant {
  id: string
  tenant_id: string
  resource_id: string
  subject_type: 'user' | 'group'
  subject_user_id?: number
  subject_group_id?: number
  actions: string
  shell_profile_id?: string
  valid_from: string
  expires_at: string
  max_session_seconds: number
  revision: number
  status: string
  created_at: string
  updated_at: string
  resource_name?: string
  resource_type?: ResourceType
  tenant_name?: string
  subject_name?: string
}

export type ContainerSessionStatus = 'active' | 'ended' | 'revoked' | 'rejected'

export interface ContainerSession {
  id: string
  tenant_id: string
  user_id: number
  device_id?: number
  resource_id: string
  resource_name?: string
  user_name?: string
  workspace_id?: string
  grant_revision: number
  target_revision: number
  agent_node_id?: number
  status: ContainerSessionStatus
  started_at: string
  ended_at?: string
  result?: string
  close_reason?: string
}

export interface ResourceDetail {
  resource: Resource
  tenant: Tenant
  target?: ResourceTarget
  grants: AccessGrant[]
}

export interface ResourceSummary {
  total: number
  available: number
  degraded: number
  active_sessions: number
  by_type: Partial<Record<ResourceType, number>>
}

export interface ResourceEvent {
  id: number
  action_type: string
  target_type: string
  target_id: string
  target_name: string
  detail?: string
  created_at: string
}

export interface ProviderTenantBinding {
  id: string
  provider_id: string
  external_tenant_id: string
  tenant_id: string
  tenant_name?: string
  status: 'active' | 'revoked'
  created_at: string
  updated_at: string
}

export interface WorkspaceBinding {
  id: string
  provider_id: string
  external_tenant_id: string
  external_workspace_id: string
  tenant_id: string
  tenant_name?: string
  owner_user_id?: number
  resource_id: string
  generation: number
  status: 'active' | 'stopped' | 'revoked'
  expires_at?: string
  created_at: string
  updated_at: string
}

export interface LegacyResourceClaim {
  id: string
  source_type: 'agent_node' | 'endpoint'
  source_id: string
  source_name: string
  source_state: string
  tenant_id: string
  tenant_name: string
  status: 'active' | 'revoked'
  claimed_by: number
  claim_reason?: string
  created_at: string
  updated_at: string
}

export type DiscoveryCandidateStatus = 'observed' | 'pending_claim' | 'published' | 'conflict' | 'stale' | 'rejected'

export interface DiscoveryCandidate {
  id: string
  agent_node_id: number
  agent_name?: string
  provider_hint?: string
  cluster_id?: string
  namespace: string
  pod_name?: string
  pod_uid: string
  container_name: string
  workspace_hint?: string
  generation_hint?: number
  ready: boolean
  status: DiscoveryCandidateStatus
  conflict_reason?: string
  label_snapshot?: string
  observed_at: string
  lease_expires_at?: string
  resource_id?: string
  created_at: string
  updated_at: string
}

export const getTenants = (params?: { search?: string; status?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<Tenant[]>>('/api/v1/admin/tenants', { params })
}

export const createTenant = (data: { key: string; name: string }) => {
  return request.post<any, ApiResponse<Tenant>>('/api/v1/admin/tenants', data)
}

export const addTenantMember = (tenantId: string, data: { user_id: number; role: 'member' | 'viewer' }) => {
  return request.post<any, ApiResponse<TenantMember>>(`/api/v1/admin/tenants/${tenantId}/members`, data, { headers: { 'X-Tenant-ID': tenantId } })
}

export const getTenantMemberCandidates = (tenantId: string, params: { search?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<TenantMemberCandidate[]>>(`/api/v1/admin/tenants/${tenantId}/member-candidates`, {
    params,
    headers: { 'X-Tenant-ID': tenantId }
  })
}

export const getTenantMembers = (tenantId: string) => {
  return request.get<any, ApiResponse<TenantMember[]>>(`/api/v1/admin/tenants/${tenantId}/members`, { headers: { 'X-Tenant-ID': tenantId } })
}

export const disableTenantMember = (tenantId: string, userId: number) => {
  return request.post<any, ApiResponse<TenantMember>>(`/api/v1/admin/tenants/${tenantId}/members/${userId}/disable`, undefined, { headers: { 'X-Tenant-ID': tenantId } })
}

export const deleteTenantMember = (tenantId: string, userId: number) => {
  return request.delete<any, ApiResponse<null>>(`/api/v1/admin/tenants/${tenantId}/members/${userId}`, { headers: { 'X-Tenant-ID': tenantId } })
}

export const getProviderTenantBindings = (params?: { provider_id?: string; status?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<ProviderTenantBinding[]>>('/api/v1/admin/provider-tenant-bindings', { params })
}

export const createProviderTenantBinding = (data: { provider_id: string; external_tenant_id: string; tenant_id: string }) => {
  return request.post<any, ApiResponse<ProviderTenantBinding>>('/api/v1/admin/provider-tenant-bindings', data)
}

export const getWorkspaceBindings = (params?: { tenant_id?: string; provider_id?: string; status?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<WorkspaceBinding[]>>('/api/v1/admin/workspace-bindings', { params })
}

export const createWorkspaceBinding = (data: {
  provider_id: string
  external_tenant_id: string
  external_workspace_id: string
  display_name?: string
  owner_user_id?: number
  generation?: number
  status?: 'active' | 'stopped' | 'revoked'
  expires_at?: string
}) => {
  return request.post<any, ApiResponse<WorkspaceBinding>>('/api/v1/admin/workspace-bindings', data)
}

export const getLegacyResourceClaims = (params?: { source_type?: string; tenant_id?: string; status?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<LegacyResourceClaim[]>>('/api/v1/admin/legacy-resource-claims', { params })
}

export const claimLegacyResource = (data: { source_type: 'agent_node' | 'endpoint'; source_id: string; tenant_id: string; reason: string }) => {
  return request.post<any, ApiResponse<LegacyResourceClaim>>('/api/v1/admin/legacy-resource-claims', data)
}

export const revokeLegacyResourceClaim = (id: string) => {
  return request.post<any, ApiResponse<LegacyResourceClaim>>(`/api/v1/admin/legacy-resource-claims/${id}/revoke`, {})
}

export const getManagedResources = (params?: {
  tenant_id?: string
  type?: ResourceType
  state?: ResourceState
  search?: string
  page?: number
  size?: number
}) => {
  return request.get<any, PagedResponse<Resource[]>>('/api/v1/admin/resources', { params })
}

export const getResourceSummary = (params?: { tenant_id?: string; state?: ResourceState; search?: string }) => {
  return request.get<any, ApiResponse<ResourceSummary>>('/api/v1/admin/resources/summary', { params })
}

export const getPlatformResources = (params?: { type?: ResourceType; state?: ResourceState; search?: string; page?: number; size?: number }) =>
  request.get<any, PagedResponse<Resource[]>>('/api/v1/admin/platform/resources', { params })

export const getPlatformResourceSummary = (params?: { state?: ResourceState; search?: string }) =>
  request.get<any, ApiResponse<ResourceSummary>>('/api/v1/admin/platform/resources/summary', { params })

export const getManagedResource = (id: string) => {
  return request.get<any, ApiResponse<ResourceDetail>>(`/api/v1/admin/resources/${id}`)
}

export const getResourceEvents = (id: string, params?: { page?: number; size?: number }) => {
  return request.get<any, PagedResponse<ResourceEvent[]>>(`/api/v1/admin/resources/${id}/events`, { params })
}

export const observeResourceTarget = (id: string, data: {
  agent_node_id: number
  cluster_id?: string
  namespace: string
  pod_name?: string
  pod_uid: string
  container_name: string
  ready?: boolean
}) => {
  return request.post<any, ApiResponse<ResourceTarget>>(`/api/v1/admin/resources/${id}/targets`, data)
}

export const createManagedResource = (data: Partial<Resource> & { tenant_id: string; type: ResourceType; display_name: string }) => {
  return request.post<any, ApiResponse<Resource>>('/api/v1/admin/resources', data)
}

export const getResourceGrants = (id: string) => {
  return request.get<any, ApiResponse<AccessGrant[]>>(`/api/v1/admin/resources/${id}/grants`)
}

export const createResourceGrant = (id: string, data: {
  subject_type?: 'user' | 'group'
  subject_user_id?: number
  subject_group_id?: number
  actions?: string[]
  shell_profile_id?: string
  valid_from?: string
  expires_at?: string
  max_session_seconds?: number
}, tenantId?: string) => {
  return request.post<any, ApiResponse<AccessGrant>>(`/api/v1/admin/resources/${id}/grants`, data, tenantId ? { headers: { 'X-Tenant-ID': tenantId } } : undefined)
}

export const revokeResourceGrant = (id: string) => {
  return request.post<any, ApiResponse<AccessGrant>>(`/api/v1/admin/grants/${id}/revoke`)
}

export const getAccessGrants = (params?: { tenant_id?: string; resource_id?: string; subject_type?: string; status?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<AccessGrant[]>>('/api/v1/admin/grants', { params })
}

export const getSessions = (params?: { tenant_id?: string; resource_id?: string; user_id?: number; status?: ContainerSessionStatus; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<ContainerSession[]>>('/api/v1/admin/sessions', { params })
}

export const revokeSession = (id: string, reason: string) => {
  return request.post<any, ApiResponse<ContainerSession>>(`/api/v1/admin/sessions/${id}/revoke`, { reason })
}

export const forceDisconnectSession = (id: string, reason: string) => {
  return request.post<any, ApiResponse<ContainerSession>>(`/api/v1/admin/sessions/${id}/force-disconnect`, { reason })
}

export const getResourceCandidates = (params?: { status?: DiscoveryCandidateStatus; search?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<DiscoveryCandidate[]>>('/api/v1/admin/resource-candidates', { params })
}

export const rejectResourceCandidate = (id: string, reason: string) => {
  return request.post<any, ApiResponse<DiscoveryCandidate>>(`/api/v1/admin/resource-candidates/${id}/reject`, { reason })
}

export const reconcileResourceCandidate = (id: string) => {
  return request.post<any, ApiResponse<DiscoveryCandidate>>(`/api/v1/admin/resource-candidates/${id}/reconcile`)
}

// Legacy discovery endpoint retained for the old diagnostic page.
export interface DiscoveredK8SService {
  agent_id: number
  agent_name: string
  namespace: string
  service_name: string
  cluster_ip: string
  ports: { name: string; port: number; protocol: string }[]
  labels: Record<string, string>
  endpoint_name: string
}

export const getDiscoveredK8SServices = (params?: { search?: string; agent_id?: number }) => {
  return request.get<any, ApiResponse<DiscoveredK8SService[]>>('/api/v1/admin/resources/k8s-services', { params })
}

export const syncK8SServiceDiscovery = () => {
  return request.post<any, ApiResponse<null>>('/api/v1/admin/resources/sync')
}
