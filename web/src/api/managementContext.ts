import request from '@/utils/request'
import type { ApiResponse } from '@/types/models'

export type ManagementWorkspace = 'tenant' | 'provider' | 'platform'

export interface ManagementContext {
  scope_type: ManagementWorkspace
  scope_id?: string
  scope_key?: string
  scope_name?: string
  scope_status?: string
  role: string
  permissions: string[]
  permission_revision: number
  expires_at?: string
}

export const getManagementContexts = () =>
  request.get<any, ApiResponse<ManagementContext[]>>('/api/v1/management/contexts')

export const getCurrentManagementContext = (scopeType: ManagementWorkspace, scopeId = '') => {
  const headers: Record<string, string> = { 'X-Management-Scope-Type': scopeType }
  if (scopeType !== 'platform') headers['X-Management-Scope-ID'] = scopeId
  return request.get<any, ApiResponse<ManagementContext>>('/api/v1/management/contexts/current', { headers })
}
