import request from '@/utils/request'

// 端口映射服务接口
export interface ProxyService {
  id: number
  name: string
  agent_id: number
  listen_port: number
  target_addr: string
  status: string
  connections: number
  bytes_in: number
  bytes_out: number
  remark: string
  // 权限控制字段
  access_type: string
  owner_id: number
  group_id: number | null
  created_at: string
  updated_at: string
  // 关联字段
  agent_name?: string
  agent_status?: string
  agent_ts_ip?: string
  agent_ts_connected?: boolean
}

// 创建服务请求
export interface CreateServiceRequest {
  name: string
  agent_id: number
  listen_port: number
  target_addr: string
  remark?: string
}

// 更新服务请求
export interface UpdateServiceRequest {
  name?: string
  listen_port?: number
  target_addr?: string
  remark?: string
}

// 获取服务列表
export function getServices() {
  return request({
    url: '/api/v1/admin/services',
    method: 'get'
  })
}

// 创建服务
export function createService(data: CreateServiceRequest) {
  return request({
    url: '/api/v1/admin/services',
    method: 'post',
    data
  })
}

// 更新服务
export function updateService(id: number, data: UpdateServiceRequest) {
  return request({
    url: `/api/v1/admin/services/${id}`,
    method: 'put',
    data
  })
}

// 删除服务
export function deleteService(id: number) {
  return request({
    url: `/api/v1/admin/services/${id}`,
    method: 'delete'
  })
}

// 启动服务
export function startService(id: number) {
  return request({
    url: `/api/v1/admin/services/${id}/start`,
    method: 'put'
  })
}

// 停止服务
export function stopService(id: number) {
  return request({
    url: `/api/v1/admin/services/${id}/stop`,
    method: 'put'
  })
}

// 获取服务统计
export function getServiceStats(id: number) {
  return request({
    url: `/api/v1/admin/services/${id}/stats`,
    method: 'get'
  })
}

// 获取指定 Agent 的服务列表
export function getServicesByAgent(agentId: number) {
  return request({
    url: `/api/v1/admin/agents/${agentId}/services`,
    method: 'get'
  })
}
