import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

export type PlatformMembershipScopeType = 'provider' | 'tenant'
export type PlatformMembershipState = 'active' | 'disabled' | 'scheduled' | 'expired'
export type PlatformOrganizationStatus = 'active' | 'suspended' | 'retired'
export type ProviderDomainScope = 'root' | 'named'

export interface PlatformOrganization {
  id: string
  scope_type: PlatformMembershipScopeType
  key: string
  name: string
  domain_label?: string
  domain_scope?: ProviderDomainScope
  status: PlatformOrganizationStatus
  management_membership_count: number
  business_member_count: number
  technical_resource_count: number
  resource_count: number
  scope_count: number
  revision: number
  row_version: number
  created_at: string
  updated_at: string
}

export interface PlatformOrganizationParams {
  scope_type?: PlatformMembershipScopeType
  status?: PlatformOrganizationStatus
  search?: string
  page: number
  size: number
}

export interface PlatformOrganizationMutationRequest {
  key?: string
  name?: string
  domain_label?: string
  domain_scope?: ProviderDomainScope
  domain_change_confirmation?: string
  reason: string
}

export interface PlatformAuditLog {
  id: number
  actor_admin_id: number
  actor_username: string
  actor_user_id: number
  actor_user_name: string
  effective_user_id: number
  effective_user_name: string
  simulation_session_id: string
  scope_type: 'platform' | PlatformMembershipScopeType
  scope_id: string
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

export interface PlatformAuditParams {
  scope_type?: 'platform' | PlatformMembershipScopeType
  simulation?: 'true' | 'false'
  action_type?: string
  search?: string
  page: number
  size: number
}

export interface PlatformManagementMembership {
  id: string
  scope_type: PlatformMembershipScopeType
  scope_id: string
  scope_key: string
  scope_name: string
  scope_status: string
  user_id: number
  username: string
  display_name: string
  user_enabled: boolean
  role: string
  enabled: boolean
  valid_from: string
  expires_at?: string
  permission_revision: number
  reason: string
  row_version: number
  created_at: string
  updated_at: string
}

export interface PlatformManagementMembershipParams {
  scope_type?: PlatformMembershipScopeType
  role?: string
  state?: PlatformMembershipState
  search?: string
  page: number
  size: number
}

export interface PlatformMembershipMutationRequest {
  user_id?: number
  role?: string
  enabled?: boolean
  valid_from?: string
  expires_at?: string
  reason: string
}

const platformHeaders = { 'X-Management-Scope-Type': 'platform' }

export const getPlatformOrganizations = (params: PlatformOrganizationParams) =>
  request.get<any, PagedResponse<PlatformOrganization[]>>('/api/v1/management/platform/organizations', { params, headers: platformHeaders })

export const createPlatformOrganization = (scopeType: PlatformMembershipScopeType, data: PlatformOrganizationMutationRequest, idempotencyKey: string) =>
  request.post<any, ApiResponse<PlatformOrganization>>(`/api/v1/management/platform/${scopeType}s`, data, { headers: { ...platformHeaders, 'Idempotency-Key': idempotencyKey } })

export const updatePlatformOrganization = (organization: PlatformOrganization, data: PlatformOrganizationMutationRequest) =>
  request.patch<any, ApiResponse<PlatformOrganization>>(`/api/v1/management/platform/${organization.scope_type}s/${organization.id}`, data, { headers: { ...platformHeaders, 'If-Match': String(organization.row_version) } })

export const transitionPlatformOrganization = (organization: PlatformOrganization, action: 'suspend' | 'resume', reason: string, idempotencyKey: string) =>
  request.post<any, ApiResponse<PlatformOrganization>>(`/api/v1/management/platform/${organization.scope_type}s/${organization.id}/${action}`, { reason }, { headers: { ...platformHeaders, 'Idempotency-Key': idempotencyKey, 'If-Match': String(organization.row_version) } })

export const getPlatformAuditLogs = (params: PlatformAuditParams) =>
  request.get<any, PagedResponse<PlatformAuditLog[]>>('/api/v1/management/platform/audit-logs', { params, headers: platformHeaders })

export const getPlatformManagementMemberships = (params: PlatformManagementMembershipParams) =>
  request.get<any, PagedResponse<PlatformManagementMembership[]>>('/api/v1/management/platform/memberships', { params, headers: platformHeaders })

export const createPlatformManagementMembership = (scopeType: PlatformMembershipScopeType, scopeId: string, data: PlatformMembershipMutationRequest, idempotencyKey: string) =>
  request.post<any, ApiResponse<PlatformManagementMembership>>(`/api/v1/management/platform/management-memberships/${scopeType}s/${scopeId}`, data, {
    headers: { ...platformHeaders, 'Idempotency-Key': idempotencyKey }
  })

export const updatePlatformManagementMembership = (membership: PlatformManagementMembership, data: PlatformMembershipMutationRequest) =>
  request.patch<any, ApiResponse<PlatformManagementMembership>>(`/api/v1/management/platform/management-memberships/${membership.scope_type}s/${membership.scope_id}/${membership.id}`, data, {
    headers: { ...platformHeaders, 'If-Match': String(membership.row_version) }
  })
