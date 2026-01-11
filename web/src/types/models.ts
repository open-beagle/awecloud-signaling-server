// Agent模型
export interface Agent {
  id: number
  agent_name: string
  description: string
  agent_token: string
  version: string
  last_heartbeat: string
  created_at: string
  updated_at: string
  status?: 'online' | 'offline'
  // Tailscale 相关字段
  tailscale_ip?: string
  ts_connected?: boolean
  ts_conn_type?: string
  ts_registered_at?: string
  // 分组管理
  group_name?: string
  // 服务数量（列表响应）
  service_count?: number
}

// Agent 详情响应（包含服务列表）
export interface AgentDetail extends Agent {
  services?: ProxyService[]
  ts_connected_at?: string // 隧道连接时间（内存缓存）
}

// 端口映射服务模型
export interface ProxyService {
  id: number
  name: string
  agent_id: number
  listen_port: number
  target_addr: string
  status: 'running' | 'stopped' | 'error'
  connections: number
  bytes_in: number
  bytes_out: number
  remark: string
  access_type: 'public' | 'private' | 'group'
  owner_id: number
  group_id?: number
  created_at: string
  updated_at: string
  // 关联数据
  agent?: Agent
}

// STCP实例模型
export interface STCPInstance {
  id: number
  agent_id: number
  agent_name?: string
  instance_name: string
  secret_key: string
  local_ip: string
  local_port: number
  description: string
  created_at: string
  updated_at: string
}

// 登录请求
export interface LoginRequest {
  username: string
  password: string
}

// 登录响应
export interface LoginResponse {
  success: boolean
  message: string
  token?: string
}

// API响应
export interface ApiResponse<T = any> {
  success: boolean
  message: string
  data?: T
}

// TCP实例模型
export interface TCPService {
  id: number
  service_name: string
  agent_id: number
  agent_name?: string
  local_ip: string
  local_port: number
  remote_port: number
  description: string
  enabled: boolean
  access_control: string
  ip_whitelist: string
  created_at: string
  updated_at: string
}

// TCP实例配置
export interface TCPServiceConfig {
  tcp_service_port_start: string
  tcp_service_max_per_agent: string
  next_available_port: number
  total_allocated_ports: number
}
