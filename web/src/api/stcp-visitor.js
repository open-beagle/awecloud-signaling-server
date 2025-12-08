import request from '@/utils/request'

// 获取STCP访问列表
export function getSTCPVisitors(params) {
  return request({
    url: '/api/v1/admin/stcp-visitors',
    method: 'get',
    params
  })
}

// 创建STCP访问
export function createSTCPVisitor(data) {
  return request({
    url: '/api/v1/admin/stcp-visitors',
    method: 'post',
    data
  })
}

// 更新STCP访问
export function updateSTCPVisitor(id, data) {
  return request({
    url: `/api/v1/admin/stcp-visitors/${id}`,
    method: 'put',
    data
  })
}

// 删除STCP访问
export function deleteSTCPVisitor(id) {
  return request({
    url: `/api/v1/admin/stcp-visitors/${id}`,
    method: 'delete'
  })
}

// 启用STCP访问
export function enableSTCPVisitor(id) {
  return request({
    url: `/api/v1/admin/stcp-visitors/${id}/enable`,
    method: 'put'
  })
}

// 禁用STCP访问
export function disableSTCPVisitor(id) {
  return request({
    url: `/api/v1/admin/stcp-visitors/${id}/disable`,
    method: 'put'
  })
}
