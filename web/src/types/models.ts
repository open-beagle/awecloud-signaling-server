// Agent模型
export interface Agent {
  id: number
  agent_name: string
  description: string
  agent_token: string
  created_at: string
  updated_at: string
  status?: 'online' | 'offline'
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
