import request from '@/utils/request'

// 服务权限接口
export interface ServicePermission {
  id: number
  service_id: number
  service_name?: string
  client_id: number
  client_name?: string
  client_ip?: string
  granted_by: number
  granted_at: string
  expires_at?: string
}

// 添加服务权限请求
export interface AddServicePermissionRequest {
  client_id: number
  expires_at?: string
}

// 更新访问类型请求
export interface UpdateAccessTypeRequest {
  access_type: string
  group_id?: number | null
}

// 获取服务的权限列表
export function getServicePermissions(serviceId: number) {
  return request({
    url: `/api/v1/admin/services/${serviceId}/permissions`,
    method: 'get'
  })
}

// 添加服务权限
export function addServicePermission(serviceId: number, data: AddServicePermissionRequest) {
  return request({
    url: `/api/v1/admin/services/${serviceId}/permissions`,
    method: 'post',
    data
  })
}

// 删除服务权限
export function removeServicePermission(serviceId: number, permissionId: number) {
  return request({
    url: `/api/v1/admin/services/${serviceId}/permissions/${permissionId}`,
    method: 'delete'
  })
}

// 更新服务访问类型
export function updateServiceAccessType(serviceId: number, data: UpdateAccessTypeRequest) {
  return request({
    url: `/api/v1/admin/services/${serviceId}/access-type`,
    method: 'put',
    data
  })
}

// 获取所有服务的权限列表
export function getAllServicePermissions() {
  return request({
    url: '/api/v1/admin/services/permissions',
    method: 'get'
  })
}
