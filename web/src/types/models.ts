// ========== 用户模型 ==========

// 用户角色
export type UserRole = 'agent' | 'client'

// 用户模型
export interface User {
  id: number
  name: string
  alias?: string
  role: UserRole
  ssh_enabled?: boolean
  created_at: string
  updated_at: string
  // 关联数据
  node_count?: number
  service_count?: number
  group_count?: number
  versions?: string[]      // 设备版本列表（去重）
  latest_version?: string  // 最新版本号
}

// 用户详情
export interface UserDetail extends User {
  nodes?: Node[]
}

// ========== 设备模型 ==========

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

// 设备模型
export interface Node {
  id: number
  user_id: number
  name: string
  type: NodeType
  ip?: string
  version?: string
  hostname?: string
  system_info?: string
  last_heartbeat?: string
  created_at: string
  updated_at: string
  // 关联数据
  user?: User
  // 计算字段
  online?: boolean
}

// ========== 分组模型 ==========

// 分组模型
export interface Group {
  id: number
  name: string
  alias?: string
  description?: string
  member_count?: number
  created_at: string
  updated_at: string
}

// 分组成员
export interface GroupMember {
  id: number
  group_id: number
  user_id: number
  created_at: string
  user?: User
}


// ========== 端口映射服务模型 ==========

// 端口映射服务
export interface ProxyService {
  id: string
  name: string
  alias?: string
  user_id: number
  source_addr: string
  target_addr: string
  enabled: boolean
  display_status?: 'disabled' | 'offline' | 'running' | 'stopped' | 'error' | 'pending'
  error_msg?: string
  connections?: number
  bytes_in?: number
  bytes_out?: number
  created_at?: string
  updated_at?: string
  // 关联数据
  user?: User
}

// ========== 端口转发模型 ==========

// 端口转发
export interface PortForward {
  id: string
  user_id: number
  target_service_id?: string
  name: string
  alias?: string
  source_addr: string
  target_addr: string
  enabled: boolean
  display_status: 'disabled' | 'offline' | 'running' | 'stopped' | 'error' | 'pending'
  error_msg?: string
  target_user_name?: string
  target_service_name?: string
}

// ========== Visitor 模型 ==========

// Visitor（端口访问服务）
export interface Visitor {
  id: number
  name: string
  user_id: number
  listen_port: number
  target_service_id: number
  target_addr: string
  status: 'running' | 'stopped' | 'error'
  connections?: number
  bytes_in?: number
  bytes_out?: number
  created_at: string
  updated_at: string
  // 关联数据
  target_user_name?: string
  target_service_name?: string
}

// ========== 登录相关 ==========

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

// ========== API 响应 ==========

// API 响应
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

// ========== 兼容旧模型（逐步废弃） ==========

// Agent 模型（兼容旧代码）
export interface Agent {
  id: number
  name: string
  alias?: string
  version?: string
  last_online?: string
  created_at?: string
  updated_at?: string
  status?: 'online' | 'offline'
  ip?: string
  ts_connected?: boolean
  group_count?: number
  service_count?: number
  forward_count?: number
  ssh_enabled?: boolean
}

// Agent 详情
export interface AgentDetail extends Agent {
  services?: ProxyService[]
  forwards?: PortForward[]
}

// 网络信息
export interface NetworkInfo {
  lan_ip: string
  lan_gateway: string
  lan_interface: string
  runtime_env: 'native' | 'docker' | 'kubernetes'
  hostname: string
}
