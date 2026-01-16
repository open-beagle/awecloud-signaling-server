// Agent模型
export interface Agent {
  id: number
  name: string
  alias?: string
  version: string
  last_online?: string
  created_at?: string
  updated_at?: string
  status?: 'online' | 'offline'
  connections?: number
  // Tailscale 相关字段
  ip?: string
  ts_connected?: boolean
  ts_conn_type?: string
  ts_registered_at?: string
  // 分组和服务数量
  group_count?: number
  service_count?: number    // 本地服务数量
  forward_count?: number    // 远程服务数量
  // SSH 配置
  ssh_enabled?: boolean     // SSH 是否启用
}

// 网络信息
export interface NetworkInfo {
  lan_ip: string           // 局域网 IP
  lan_gateway: string      // 网关地址
  lan_interface: string    // 网卡名称
  runtime_env: 'native' | 'docker' | 'kubernetes'  // 运行环境
  hostname: string         // 主机名
}

// Visitor 模型（端口访问服务）
export interface Visitor {
  id: number
  name: string
  agent_id: number
  listen_port: number
  target_service_id: number
  target_addr: string
  status: 'running' | 'stopped' | 'error'
  connections: number
  bytes_in: number
  bytes_out: number
  created_at: string
  updated_at: string
  // 关联数据（展示用）
  target_agent_name?: string
  target_service_name?: string
}

// Agent 详情响应（包含服务列表）
export interface AgentDetail extends Agent {
  created_at?: string
  last_online?: string
  services?: ProxyService[]
  forwards?: PortForward[]   // 端口访问服务列表
}

// 端口转发模型
export interface PortForward {
  id: string
  agent_id: number
  target_service_id?: string
  name: string              // 从关联服务获取
  alias?: string            // 从关联服务获取
  source_addr: string       // 源地址（局域网 IP:端口）
  target_addr: string       // 目标地址（VPN 地址）
  enabled: boolean
  display_status: 'disabled' | 'offline' | 'running' | 'stopped' | 'error' | 'pending'  // 合并后的显示状态
  error_msg?: string
  target_agent_name?: string
  target_service_name?: string
}

// 端口映射服务模型
export interface ProxyService {
  id: string
  name: string
  alias?: string
  agent_id: number
  source_addr: string       // 源地址（VPN IP:端口）
  target_addr: string       // 目标地址（局域网地址）
  enabled: boolean
  display_status: 'disabled' | 'offline' | 'running' | 'stopped' | 'error' | 'pending'  // 合并后的显示状态
  error_msg?: string
  connections?: number
  bytes_in?: number
  bytes_out?: number
  remark?: string
  created_at?: string
  updated_at?: string
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
  data?: {
    token: string
    expires_at: string
    admin: {
      id: number
      username: string
      role: string
      created_at: string
    }
  }
}

// API响应
export interface ApiResponse<T = any> {
  success: boolean
  message?: string
  data?: T
}

// 分页响应
export interface PagedResponse<T = any> {
  success: boolean
  message?: string
  data?: T
  total: number
  page: number
  size: number
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
