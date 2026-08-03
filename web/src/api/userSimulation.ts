import request from '@/utils/request'
import { createIdempotencyKey } from '@/utils/idempotency'
import type { ApiResponse } from '@/types/models'

export const USER_SIMULATION_STORAGE_KEY = 'management_user_simulation'

export type UserSimulationScopeType = 'tenant' | 'provider'
export type UserSimulationStatus = 'active' | 'revoked' | 'expired'

export interface UserSimulationSession {
  id: string
  actor_user_id: number
  effective_user_id: number
  effective_user_name?: string
  scope_type: UserSimulationScopeType
  scope_id: string
  reason: string
  status: UserSimulationStatus
  started_at: string
  expires_at: string
  ended_at?: string
  end_reason?: string
  created_request_id: string
  permission_revision: number
  row_version: number
  created_at: string
  updated_at: string
}

const platformHeaders = { 'X-Management-Scope-Type': 'platform' }

export const getUserSimulations = () =>
  request.get<any, ApiResponse<UserSimulationSession[]>>('/api/v1/management/user-simulations', { headers: platformHeaders })

export const createUserSimulation = (data: { effective_user_id: number; scope_type: UserSimulationScopeType; scope_id: string; reason: string; expires_at: string }) =>
  request.post<any, ApiResponse<UserSimulationSession>>('/api/v1/management/user-simulations', data, {
    headers: { ...platformHeaders, 'Idempotency-Key': createIdempotencyKey() },
  })

export const revokeUserSimulation = (sessionId: string, rowVersion: number, reason: string) =>
  request.post<any, ApiResponse<UserSimulationSession>>(`/api/v1/management/user-simulations/${sessionId}/revoke`, { reason }, {
    headers: { ...platformHeaders, 'If-Match': String(rowVersion) },
  })
