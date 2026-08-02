import request from '@/utils/request'
import type { ApiResponse } from '@/types/models'

export type TenantManagementRole = 'tenant_admin' | 'security_auditor' | 'tenant_viewer' | 'member'

export interface TenantContext {
  tenant_id: string
  tenant_key: string
  tenant_name: string
  tenant_status: 'active' | 'suspended'
  management_role: TenantManagementRole
  permissions: string[]
  permission_revision: number
  expires_at?: string
}

export const getTenantContexts = () =>
  request.get<any, ApiResponse<TenantContext[]>>('/api/v1/admin/tenant-contexts')

export const getTenantContext = (tenantId: string) =>
  request.get<any, ApiResponse<TenantContext>>(`/api/v1/admin/tenant-contexts/${tenantId}`)
