import request from '@/utils/request'

// Agent 服务权限接口
export interface AgentServicePermission {
  id: number
  agent_id: number
  agent_name: string
  agent_ip: string
  service_id: number
  service_name: string
  service_addr: string
  granted_by: number
  granted_at: string
}

// 添加 Agent 服务权限请求
export interface AddAgentPermissionRequest {
  agent_id: number
  service_ids: number[]
}

// 获取 Agent 服务权限列表
export function getAgentServicePermissions() {
  return request({
    url: '/api/v1/admin/agent-permissions',
    method: 'get'
  })
}

// 添加 Agent 服务权限
export function addAgentServicePermission(data: AddAgentPermissionRequest) {
  return request({
    url: '/api/v1/admin/agent-permissions',
    method: 'post',
    data
  })
}

// 删除 Agent 服务权限
export function removeAgentServicePermission(id: number) {
  return request({
    url: `/api/v1/admin/agent-permissions/${id}`,
    method: 'delete'
  })
}
