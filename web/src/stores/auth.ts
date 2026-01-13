import { defineStore } from 'pinia'
import { ref } from 'vue'
import { login as loginApi, logout as logoutApi } from '@/api/admin'
import type { LoginRequest } from '@/types/models'

export const useAuthStore = defineStore('auth', () => {
  const token = ref(localStorage.getItem('token') || '')
  const username = ref(localStorage.getItem('username') || '')

  const login = async (data: LoginRequest) => {
    const res = await loginApi(data)
    // 后端返回格式: { success: true, data: { token: "...", admin: {...} } }
    const tokenValue = res.data?.token || res.token
    if (res.success && tokenValue) {
      token.value = tokenValue
      username.value = data.username
      localStorage.setItem('token', tokenValue)
      localStorage.setItem('username', data.username)
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
    localStorage.removeItem('token')
    localStorage.removeItem('username')
  }

  const isAuthenticated = () => {
    return !!token.value
  }

  return {
    token,
    username,
    login,
    logout,
    isAuthenticated
  }
})
