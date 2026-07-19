import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { getMe, login as loginApi, logout as logoutApi } from '@/api/admin'
import type { LoginRequest } from '@/types/models'

export const useAuthStore = defineStore('auth', () => {
  const token = ref(localStorage.getItem('token') || '')
  const username = ref(localStorage.getItem('username') || '')
  const role = ref(localStorage.getItem('admin_role') || '')

  const login = async (data: LoginRequest) => {
    const res = await loginApi(data)
    // 后端返回格式: { success: true, data: { token: "...", admin: {...} } }
    const tokenValue = res.data?.token || res.token
    if (res.success && tokenValue) {
      token.value = tokenValue
      username.value = res.data?.admin.username || data.username
      role.value = res.data?.admin.role || ''
      localStorage.setItem('token', tokenValue)
      localStorage.setItem('username', username.value)
      localStorage.setItem('admin_role', role.value)
      return true
    }
    return false
  }

  const logout = async () => {
    try {
      await logoutApi()
    } catch (error) {
      // ignore
    }
    token.value = ''
    username.value = ''
    role.value = ''
    localStorage.removeItem('token')
    localStorage.removeItem('username')
    localStorage.removeItem('admin_role')
    localStorage.removeItem('tenant_context')
  }


  const loadProfile = async () => {
    if (!token.value) return
    const res = await getMe()
    if (res.success && res.data) {
      username.value = res.data.username
      role.value = res.data.role
      localStorage.setItem('username', username.value)
      localStorage.setItem('admin_role', role.value)
    }
  }

  const isAuthenticated = computed(() => !!token.value)
  const canWrite = computed(() => role.value === 'admin' || role.value === 'tenant_admin')
  const isPlatformAdmin = computed(() => role.value === 'admin')

  return {
    token,
    username,
    role,
    login,
    logout,
    loadProfile,
    isAuthenticated,
    canWrite,
    isPlatformAdmin
  }
})
