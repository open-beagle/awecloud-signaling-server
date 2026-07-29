import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import {
  getCurrentManagementContext,
  getManagementContexts,
  type ManagementContext,
  type ManagementWorkspace
} from '@/api/managementContext'
import { useTenantStore } from '@/stores/tenant'

const WORKSPACE_KEY = 'management_workspace'
const PROVIDER_CONTEXT_KEY = 'provider_context'

const isWorkspace = (value: string | null): value is ManagementWorkspace =>
  value === 'tenant' || value === 'provider' || value === 'platform'

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

  const tenantContexts = computed(() => contexts.value.filter(item => item.scope_type === 'tenant'))
  const providerContexts = computed(() => contexts.value.filter(item => item.scope_type === 'provider'))
  const platformContext = computed(() => contexts.value.find(item => item.scope_type === 'platform'))
  const tenantId = computed(() => useTenantStore().tenantId)
  const currentContext = computed(() => {
    if (currentWorkspace.value === 'platform') return platformContext.value
    const selectedId = currentWorkspace.value === 'tenant' ? tenantId.value : providerId.value
    return contexts.value.find(item => item.scope_type === currentWorkspace.value && item.scope_id === selectedId)
  })
  const isSimulationActive = computed(() => false)

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
    if (isSimulationActive.value && workspace !== currentWorkspace.value) return false
    persistWorkspace(workspace)
    contextRevision.value++
    return true
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
    loading.value = false
    loaded.value = false
    error.value = ''
    contextRevision.value++
    localStorage.removeItem(WORKSPACE_KEY)
    localStorage.removeItem(PROVIDER_CONTEXT_KEY)
  }

  window.addEventListener('admin-session-cleared', reset)

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
    isSimulationActive,
    hasContext,
    selectedContextId,
    can,
    loadContexts,
    activateWorkspace,
    selectContext,
    reset
  }
})
