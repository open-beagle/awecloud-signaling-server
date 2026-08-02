import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

export type ResourceAllocationMode = 'assigned' | 'leased' | 'shared'
export type ResourceAllocationState = 'draft' | 'scheduled' | 'active' | 'suspended' | 'expired' | 'revoked'

export interface ResourceAllocationItem {
  id: string
  allocation_id: string
  scope_id: string
  scope_row_version_snapshot: number
  created_at: string
}

export interface ResourceAllocation {
  id: string
  tenant_id: string
  mode: ResourceAllocationMode
  valid_from: string
  expires_at?: string
  contract_ref?: string
  state: ResourceAllocationState
  row_version: number
  created_by_user_id: number
  activated_by_user_id?: number
  activated_at?: string
  terminated_by_user_id?: number
  terminated_at?: string
  termination_reason?: string
  renewed_from_id?: string
  created_at: string
  updated_at: string
  items: ResourceAllocationItem[]
}

export interface PlatformAllocationListParams {
  tenant_id?: string
  provider_id?: string
  resource_id?: string
  scope_id?: string
  mode?: string
  state?: string
  search?: string
  valid_at?: string
  page: number
  size: number
}

const platformHeaders = { 'X-Management-Scope-Type': 'platform' }

export const getPlatformAllocations = (params: PlatformAllocationListParams) =>
  request.get<any, PagedResponse<ResourceAllocation[]>>('/api/v1/management/platform/allocations', { params, headers: platformHeaders })

export const getPlatformAllocation = (allocationId: string) =>
  request.get<any, ApiResponse<ResourceAllocation>>(`/api/v1/management/platform/allocations/${allocationId}`, { headers: platformHeaders })
