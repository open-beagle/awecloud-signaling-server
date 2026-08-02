import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import {
  getCurrentManagementContext,
  getManagementContexts,
  type ManagementContext,
  type ManagementWorkspace
} from '@/api/managementContext'
import type { UserSimulationSession } from '@/api/userSimulation'
import { useTenantStore } from '@/stores/tenant'

const WORKSPACE_KEY = 'management_workspace'
const PROVIDER_CONTEXT_KEY = 'provider_context'
const USER_SIMULATION_STORAGE_KEY = 'management_user_simulation'

const isWorkspace = (value: string | null): value is ManagementWorkspace =>
  value === 'tenant' || value === 'provider' || value === 'platform'

const storedSimulation = (): UserSimulationSession | undefined => {
  const raw = localStorage.getItem(USER_SIMULATION_STORAGE_KEY)
  if (!raw) return undefined
  try {
    const session = JSON.parse(raw) as UserSimulationSession
    if (session.id && session.status === 'active' && new Date(session.expires_at).getTime() > Date.now()) return session
  } catch {
    // Invalid local state must never attach an unverified simulation header.
  }
  localStorage.removeItem(USER_SIMULATION_STORAGE_KEY)
  return undefined
}

export const workspaceHome = (workspace: ManagementWorkspace) => ({
  tenant: '/tenant-overview',
  provider: '/provider-overview',
  platform: '/platform-overview'
}[workspace])

export const workspaceLabel = (workspace: ManagementWorkspace) => ({
  tenant: '租户',
  provider: '资源',
  platform: '平台'
}[workspace])

export const useWorkspaceStore = defineStore('workspace', () => {
  const persistedWorkspace = localStorage.getItem(WORKSPACE_KEY)
  const currentWorkspace = ref<ManagementWorkspace>(isWorkspace(persistedWorkspace) ? persistedWorkspace : 'platform')
  const providerId = ref(localStorage.getItem(PROVIDER_CONTEXT_KEY) || '')
  const contexts = ref<ManagementContext[]>([])
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref('')
  const contextRevision = ref(0)
  const simulationSession = ref<UserSimulationSession | undefined>(storedSimulation())

  const tenantContexts = computed(() => contexts.value.filter(item => item.scope_type === 'tenant'))
  const providerContexts = computed(() => contexts.value.filter(item => item.scope_type === 'provider'))
  const platformContext = computed(() => contexts.value.find(item => item.scope_type === 'platform'))
  const tenantId = computed(() => useTenantStore().tenantId)
  const currentContext = computed(() => {
    if (currentWorkspace.value === 'platform') return platformContext.value
    const selectedId = currentWorkspace.value === 'tenant' ? tenantId.value : providerId.value
    return contexts.value.find(item => item.scope_type === currentWorkspace.value && item.scope_id === selectedId)
  })
  const isSimulationActive = computed(() => !!simulationSession.value && simulationSession.value.status === 'active' && new Date(simulationSession.value.expires_at).getTime() > Date.now())

  const persistWorkspace = (workspace: ManagementWorkspace) => {
    currentWorkspace.value = workspace
    localStorage.setItem(WORKSPACE_KEY, workspace)
  }

  const persistProvider = (value: string) => {
    providerId.value = value
    if (value) localStorage.setItem(PROVIDER_CONTEXT_KEY, value)
    else localStorage.removeItem(PROVIDER_CONTEXT_KEY)
  }

  const hasContext = (workspace: ManagementWorkspace) => workspace === 'platform'
    ? !!platformContext.value
    : contexts.value.some(item => item.scope_type === workspace)

  const selectedContextId = (workspace: ManagementWorkspace) => {
    if (workspace === 'tenant') return tenantId.value
    if (workspace === 'provider') return providerId.value
    return ''
  }

  const can = (permission: string) => currentContext.value?.permissions.includes(permission) || false

  const chooseInitialWorkspace = () => {
    const persisted = localStorage.getItem(WORKSPACE_KEY)
    if (isWorkspace(persisted) && hasContext(persisted)) return persistWorkspace(persisted)
    if (platformContext.value) return persistWorkspace('platform')
    if (tenantContexts.value.length > 0) return persistWorkspace('tenant')
    if (providerContexts.value.length > 0) return persistWorkspace('provider')
  }

  const loadContexts = async (force = false) => {
    if (loading.value || (loaded.value && !force)) return
    loading.value = true
    error.value = ''
    try {
      const response = await getManagementContexts()
      contexts.value = response.success && response.data ? response.data : []
      if (isSimulationActive.value && simulationSession.value) {
        const session = simulationSession.value
        try {
          const current = await getCurrentManagementContext(session.scope_type, session.scope_id)
          if (!current.success || !current.data) throw new Error('用户模拟上下文不可用')
          const index = contexts.value.findIndex(item => item.scope_type === session.scope_type && item.scope_id === session.scope_id)
          if (index >= 0) contexts.value[index] = current.data
          else contexts.value.push(current.data)
          persistWorkspace(session.scope_type)
          if (session.scope_type === 'tenant') useTenantStore().applyManagementContext(current.data)
          else persistProvider(session.scope_id)
        } catch {
          simulationSession.value = undefined
          localStorage.removeItem(USER_SIMULATION_STORAGE_KEY)
        }
      }
      const tenantStore = useTenantStore()
      const availableTenantIds = tenantContexts.value.map(item => item.scope_id || '').filter(Boolean)
      const nextTenantId = availableTenantIds.includes(tenantStore.tenantId) ? tenantStore.tenantId : availableTenantIds[0] || ''
      tenantStore.syncTenantContext(nextTenantId)

      const availableProviderIds = providerContexts.value.map(item => item.scope_id || '').filter(Boolean)
      if (!availableProviderIds.includes(providerId.value)) persistProvider(availableProviderIds[0] || '')

      loaded.value = true
      chooseInitialWorkspace()
      contextRevision.value++
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : '工作域上下文加载失败'
      throw cause
    } finally {
      loading.value = false
    }
  }

  const activateWorkspace = (workspace: ManagementWorkspace) => {
    if (isSimulationActive.value && workspace !== simulationSession.value?.scope_type) return false
    persistWorkspace(workspace)
    contextRevision.value++
    return true
  }

  const activateSimulation = async (session: UserSimulationSession, effectiveUserName = '') => {
    const active = { ...session, effective_user_name: effectiveUserName || session.effective_user_name }
    simulationSession.value = active
    localStorage.setItem(USER_SIMULATION_STORAGE_KEY, JSON.stringify(active))
    persistWorkspace(active.scope_type)
    if (active.scope_type === 'tenant') useTenantStore().syncTenantContext(active.scope_id)
    else persistProvider(active.scope_id)
    try {
      const response = await getCurrentManagementContext(active.scope_type, active.scope_id)
      if (!response.success || !response.data) throw new Error('用户模拟上下文不可用')
      const index = contexts.value.findIndex(item => item.scope_type === active.scope_type && item.scope_id === active.scope_id)
      if (index >= 0) contexts.value[index] = response.data
      else contexts.value.push(response.data)
      if (active.scope_type === 'tenant') useTenantStore().applyManagementContext(response.data)
      contextRevision.value++
    } catch (cause) {
      simulationSession.value = undefined
      localStorage.removeItem(USER_SIMULATION_STORAGE_KEY)
      throw cause
    }
  }

  const clearSimulation = () => {
    simulationSession.value = undefined
    localStorage.removeItem(USER_SIMULATION_STORAGE_KEY)
    persistWorkspace('platform')
    contextRevision.value++
  }

  const selectContext = async (workspace: 'tenant' | 'provider', scopeId: string) => {
    const candidate = contexts.value.find(item => item.scope_type === workspace && item.scope_id === scopeId)
    if (!candidate) return false
    try {
      const response = await getCurrentManagementContext(workspace, scopeId)
      if (!response.success || !response.data) return false
      const index = contexts.value.findIndex(item => item.scope_type === workspace && item.scope_id === scopeId)
      if (index >= 0) contexts.value[index] = response.data
      if (workspace === 'tenant') useTenantStore().syncTenantContext(scopeId)
      else persistProvider(scopeId)
      contextRevision.value++
      window.dispatchEvent(new CustomEvent('management-context-changed', {
        detail: { workspace, scopeId }
      }))
      return true
    } catch {
      return false
    }
  }

  const reset = () => {
    currentWorkspace.value = 'platform'
    providerId.value = ''
    contexts.value = []
    simulationSession.value = undefined
    loading.value = false
    loaded.value = false
    error.value = ''
    contextRevision.value++
    localStorage.removeItem(WORKSPACE_KEY)
		localStorage.removeItem(PROVIDER_CONTEXT_KEY)
		localStorage.removeItem(USER_SIMULATION_STORAGE_KEY)
  }

  window.addEventListener('admin-session-cleared', reset)
  window.addEventListener('user-simulation-invalid', () => {
    clearSimulation()
    loadContexts(true).catch(() => undefined)
    useTenantStore().loadContexts(true).catch(() => undefined)
  })

  return {
    currentWorkspace,
    providerId,
    contexts,
    tenantContexts,
    providerContexts,
    platformContext,
    currentContext,
    loading,
    loaded,
    error,
    contextRevision,
    simulationSession,
    isSimulationActive,
    hasContext,
    selectedContextId,
    can,
    loadContexts,
    activateWorkspace,
    selectContext,
    activateSimulation,
    clearSimulation,
    reset
  }
})
