import request from '@/utils/request'
import type { ApiResponse, Visitor } from '@/types/models'

// 获取 Visitor 列表
export function getVisitors(agentId?: number) {
  return request<ApiResponse<Visitor[]>>({
    url: '/api/v1/visitors',
    method: 'get',
    params: agentId ? { agent_id: agentId } : undefined
  })
}

// 获取 Visitor 详情
export function getVisitor(id: number) {
  return request<ApiResponse<Visitor>>({
    url: `/api/v1/visitors/${id}`,
    method: 'get'
  })
}

// 创建 Visitor
export function createVisitor(data: {
  name: string
  agent_id: number
  listen_port: number
  target_service_id: number
}) {
  return request<ApiResponse<Visitor>>({
    url: '/api/v1/visitors',
    method: 'post',
    data
  })
}

// 删除 Visitor
export function deleteVisitor(id: number) {
  return request<ApiResponse<void>>({
    url: `/api/v1/visitors/${id}`,
    method: 'delete'
  })
}

// 启动 Visitor
export function startVisitor(id: number) {
  return request<ApiResponse<void>>({
    url: `/api/v1/visitors/${id}/start`,
    method: 'post'
  })
}

// 停止 Visitor
export function stopVisitor(id: number) {
  return request<ApiResponse<void>>({
    url: `/api/v1/visitors/${id}/stop`,
    method: 'post'
  })
}
