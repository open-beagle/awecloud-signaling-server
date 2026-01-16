import request from '@/utils/request'

// SSH 授权类型定义
export interface SSHClientPermission {
  id: number
  client_id: number
  client_name: string
  client_ip: string
  agent_id: number
  agent_name: string
  ssh_users: string[]
  enabled: boolean
  created_at: string
}

export interface SSHClientGroupPermission {
  id: number
  group_id: number
  group_name: string
  member_count: number
  agent_id: number
  agent_name: string
  ssh_users: string[]
  enabled: boolean
  created_at: string
}

// Agent SSH 统计
export interface AgentSSHStats {
  id: number
  name: string
  alias: string
  tailscale_ip: string
  client_group_count: number
  client_count: number
}

// Agent SSH 详情
export interface AgentSSHDetail {
  agent: {
    id: number
    name: string
    alias: string
    tailscale_ip: string
  }
  client_permissions: SSHClientPermission[]
  group_permissions: SSHClientGroupPermission[]
}

// 获取 Agent SSH 统计列表
export function getAgentSSHStats() {
  return request.get<AgentSSHStats[]>('/api/v1/admin/agents/ssh-stats')
}

// 获取指定 Agent 的 SSH 授权详情
export function getAgentSSHPermissions(agentId: number) {
  return request.get<AgentSSHDetail>(`/api/v1/admin/agents/${agentId}/ssh-permissions`)
}

// Desktop -> Agent SSH 授权
export function listClientPermissions() {
  return request.get<SSHClientPermission[]>('/api/v1/admin/ssh/client-permissions')
}

export function createClientPermission(data: {
  client_id: number
  agent_id: number
  ssh_users: string[]
}) {
  return request.post('/api/v1/admin/ssh/client-permissions', data)
}

export function updateClientPermission(id: number, data: {
  ssh_users?: string[]
  enabled?: boolean
}) {
  return request.put(`/api/v1/admin/ssh/client-permissions/${id}`, data)
}

export function deleteClientPermission(id: number) {
  return request.delete(`/api/v1/admin/ssh/client-permissions/${id}`)
}

// Desktop 分组 -> Agent SSH 授权
export function listClientGroupPermissions() {
  return request.get<SSHClientGroupPermission[]>('/api/v1/admin/ssh/client-group-permissions')
}

export function createClientGroupPermission(data: {
  group_id: number
  agent_id: number
  ssh_users: string[]
}) {
  return request.post('/api/v1/admin/ssh/client-group-permissions', data)
}

export function updateClientGroupPermission(id: number, data: {
  ssh_users?: string[]
  enabled?: boolean
}) {
  return request.put(`/api/v1/admin/ssh/client-group-permissions/${id}`, data)
}

export function deleteClientGroupPermission(id: number) {
  return request.delete(`/api/v1/admin/ssh/client-group-permissions/${id}`)
}
