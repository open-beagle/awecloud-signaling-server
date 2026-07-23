import axios from 'axios'
import type { AxiosInstance, AxiosRequestConfig, AxiosResponse, InternalAxiosRequestConfig } from 'axios'
import { ElMessage } from 'element-plus'
import router from '@/router'

// 创建自定义的 request 函数，返回类型为 T 而不是 AxiosResponse<T>
interface RequestInstance extends AxiosInstance {
  <T = any>(config: AxiosRequestConfig): Promise<T>
}

const service: AxiosInstance = axios.create({
  baseURL: '',
  timeout: 10000
})

// 请求拦截器
service.interceptors.request.use(
  (config: InternalAxiosRequestConfig) => {
    const token = localStorage.getItem('token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    const tenantId = localStorage.getItem('tenant_context')
		const contextFreePaths = ['/api/v1/admin/tenants', '/api/v1/admin/tenant-contexts', '/api/v1/admin/tenant-admin-memberships', '/api/v1/admin/overview', '/api/v1/admin/platform-admins', '/api/v1/admin/platform']
		const platformResourcePaths = ['/api/v1/admin/nodes', '/api/v1/admin/endpoints', '/api/v1/admin/legacy-resource-claims']
		const isPlatformResource = platformResourcePaths.some(path => config.url === path || config.url?.startsWith(`${path}/`))
		const isContextFree = isPlatformResource || (config.method?.toLowerCase() === 'get' && contextFreePaths.some(path => config.url === path || config.url?.startsWith(`${path}/`)))
		if (tenantId && !isContextFree) {
      config.headers['X-Tenant-ID'] = tenantId
    }
    return config
  },
  (error) => {
    return Promise.reject(error)
  }
)

// 响应拦截器
service.interceptors.response.use(
  (response: AxiosResponse) => {
    // 如果是blob类型，直接返回data
    if (response.config.responseType === 'blob') {
      return response.data
    }
    return response.data
  },
  (error) => {
    if (error.response) {
      const { status, data } = error.response
		const invalidTenantCodes = ['TENANT_CONTEXT_UNAVAILABLE', 'PERMISSION_REVISION_STALE', 'ADMIN_DISABLED']
		if (invalidTenantCodes.includes(data?.code)) {
			localStorage.removeItem('tenant_context')
			window.dispatchEvent(new CustomEvent('tenant-context-invalid', { detail: { code: data.code } }))
		}
      
      if (status === 401) {
        ElMessage.error('未授权，请重新登录')
        localStorage.removeItem('token')
        localStorage.removeItem('admin_role')
        localStorage.removeItem('tenant_context')
		window.dispatchEvent(new Event('admin-session-cleared'))
		if (router.currentRoute.value.name !== 'Login') router.push('/login')
      } else if (status === 403) {
			ElMessage.error(data?.message || '没有权限访问此资源')
      } else if (status === 404) {
        ElMessage.error('请求的资源不存在')
      } else if (status === 409) {
        // 冲突错误（如端口冲突）
        ElMessage.error(data?.message || '操作冲突，请检查输入')
      } else if (status === 500) {
        ElMessage.error(data?.message || '服务器内部错误，请稍后重试')
      } else if (status === 502) {
        ElMessage.error('网关错误，服务暂时不可用')
      } else if (status === 503) {
        ElMessage.error('服务暂时不可用，请稍后重试')
      } else if (status === 504) {
        ElMessage.error('请求超时，请检查网络连接')
      } else {
        ElMessage.error(data?.message || `请求失败 (${status})`)
      }
    } else if (error.request) {
      // 请求已发出但没有收到响应
      if (error.code === 'ECONNABORTED') {
        ElMessage.error('请求超时，请检查网络连接后重试')
      } else if (error.code === 'ERR_NETWORK') {
        ElMessage.error('网络连接失败，请检查网络设置')
      } else {
        ElMessage.error('网络错误，无法连接到服务器')
      }
    } else {
      // 请求配置出错
      ElMessage.error('请求配置错误')
    }
    
    return Promise.reject(error)
  }
)

const request = service as RequestInstance

export default request
