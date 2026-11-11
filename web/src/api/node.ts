import request from '@/utils/request'
import type { ApiResponse, PagedResponse } from '@/types/models'

// 设备类型
export type NodeType = 'agent' | 'desktop'

// 设备系统信息
export interface NodeSystemInfo {
  os: string
  os_version: string
  arch: string
  hostname: string
  cpu: string
  cpu_cores: number
  memory_gb: number
}

// Headscale 节点信息
export interface HeadscaleNodeInfo {
  id: number
  name: string
  given_name: string
  ip_addresses: string[]
  online: boolean
  last_seen?: string
  expiry?: string
  created_at?: string
  forced_tags?: string[]
  user_name?: string
}

// 设备模型
export interface Node {
  id: number
  user_id: number
  name: string
  type: NodeType
  headscale_node_id?: number
  ip?: string
  version?: string
  hostname?: string
  host_domain_label: string
  system_info?: string
  status?: string
  last_heartbeat?: string
  created_at: string
  updated_at: string
  // 关联数据
  user?: {
    id: number
    name: string
    alias?: string
    role: string
  }
  // 计算字段
  online?: boolean
}

// 设备详情
export interface NodeDetail extends Node {
  system_info_parsed?: NodeSystemInfo
  headscale?: HeadscaleNodeInfo
}

export interface DesktopNodeRDPGrantSummary {
  id: number
  tenant_id: string
  grantee_user_id: number
  grantee_user_name: string
  created_by_name?: string
  reason?: string
  status: string
  expires_at: string
  revoked_at?: string
  created_at: string
}

export interface DesktopRDPSessionAuditItem {
  id: number
  session_uuid: string
  target_node_id: number
  owner_user_id: number
  event_type: 'started' | 'ended' | 'rejected' | string
  source_headscale_node_id?: number
  source_node_id?: number
  source_user_name?: string
  source_device_name?: string
  source_ip?: string
  grant_id?: number
  close_reason?: string
  duration_ms?: number
  occurred_at: string
}

export interface DesktopNodeRDPDiagnostics {
  node_id: number
  rdp_mode: string
  rdp_allow_share: boolean
  rdp_admin_disabled: boolean
  rdp_local_ready: boolean
  rdp_local_port: number
  rdp_tunnel_exposed: boolean
  active_grants: DesktopNodeRDPGrantSummary[]
  recent_sessions: DesktopRDPSessionAuditItem[]
}

// 获取设备列表
export const getNodes = (params?: { type?: NodeType; user_id?: number; search?: string; page?: number; size?: number }) => {
  return request.get<any, PagedResponse<Node[]>>('/api/v1/admin/nodes', { params })
}

// 获取设备详情
export const getNode = (id: number) => {
  return request.get<any, ApiResponse<NodeDetail>>(`/api/v1/admin/nodes/${id}`)
}

// 获取 Desktop 设备远程桌面只读诊断与会话审计
export const getNodeRDPStatus = (id: number) => {
  return request.get<any, ApiResponse<DesktopNodeRDPDiagnostics>>(`/api/v1/admin/nodes/${id}/rdp`)
}

// 平台管理员紧急熔断关闭或解除熔断 Desktop 远程桌面（不具备反向代开能力）
export const updateNodeRDPAdminDisable = (id: number, data: { admin_disabled: boolean; reason?: string }) => {
  return request.put<any, ApiResponse<{ node_id: number; rdp_admin_disabled: boolean }>>(`/api/v1/admin/nodes/${id}/rdp/admin-disable`, data)
}

// 删除设备
export const deleteNode = (id: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/nodes/${id}`)
}

// 获取用户的设备列表
export const getNodesByUser = (userId: number) => {
  return request.get<any, ApiResponse<Node[]>>(`/api/v1/admin/users/${userId}/nodes`)
}

// Agent 能力配置
export interface CapabilityConfig {
  ssh_enabled: boolean
  k8s_enabled: boolean | null
  k8s_listen_port: number | null
  k8s_api_server: string
  svc_enabled: boolean | null
  svc_label_selector: string
  svc_namespaces: string
  svc_listen_port_base: number | null
  endpoint_enabled: boolean | null
  endpoint_listen_port: number | null
  endpoint_token: string
}

// 获取 Agent 能力配置
export const getNodeCapabilities = (id: number) => {
  return request.get<any, ApiResponse<CapabilityConfig>>(`/api/v1/admin/nodes/${id}/capabilities`)
}

// 更新 Agent 能力配置
export const updateNodeCapabilities = (id: number, data: Partial<CapabilityConfig>) => {
  return request.put<any, ApiResponse>(`/api/v1/admin/nodes/${id}/capabilities`, data)
}

export const updateNodeHostDomainLabel = (id: number, hostDomainLabel: string) =>
  request.put<any, ApiResponse>(`/api/v1/admin/nodes/${id}/domain-label`, { host_domain_label: hostDomainLabel })

// 重置 Agent 能力配置
export const resetNodeCapabilities = (id: number) => {
  return request.delete<any, ApiResponse>(`/api/v1/admin/nodes/${id}/capabilities`)
}

// 重新生成 Endpoint Token
export const regenerateEndpointToken = (id: number) => {
  return request.post<any, ApiResponse<{ endpoint_token: string }>>(`/api/v1/admin/nodes/${id}/capabilities/endpoint-token/regenerate`)
}
