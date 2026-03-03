import request from '@/utils/request'
import type { PagedResponse } from '@/types/models'

// ========== K8S Service 聚合查询（P9 新增） ==========
// 按集群（Agent User）维度聚合

// K8S Service 授权集群列表项
export interface K8SServiceUnifiedACLItem {
  id: number           // 集群 ID（Agent User ID）
  name: string         // 集群名
  alias: string        // 集群别名
  provider_type: 'agent' | 'endpoint'  // 提供者类型
  provider_id: string  // 提供者 ID
  provider_name: string // 提供者名称（Endpoint name，agent 时为空）
  user_count: number
  group_count: number
  created_at: string
}

// 获取 K8S Service 授权合并列表
export const getK8SServiceUnifiedACLList = (params?: {
  type?: string
  search?: string
  page?: number
  size?: number
}) => {
  return request.get<any, PagedResponse<K8SServiceUnifiedACLItem[]>>(
    '/api/v1/admin/acl/k8s-service-unified',
    { params }
  )
}
