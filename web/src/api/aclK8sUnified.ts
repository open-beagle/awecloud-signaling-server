import request from '@/utils/request'
import type { PagedResponse } from '@/types/models'

// ========== K8S API 聚合查询（P9 新增） ==========

// K8S API 授权合并列表项
export interface K8SUnifiedACLItem {
  id: string
  name: string
  alias: string
  type: 'agent' | 'endpoint'
  agent_name: string
  agent_id: number
  api_server: string
  status: string
  user_count: number
  group_count: number
  created_at: string
}

// 获取 K8S API 授权合并列表
export const getK8SUnifiedACLList = (params?: {
  type?: string
  search?: string
  page?: number
  size?: number
}) => {
  return request.get<any, PagedResponse<K8SUnifiedACLItem[]>>(
    '/api/v1/admin/acl/k8s-unified',
    { params }
  )
}
