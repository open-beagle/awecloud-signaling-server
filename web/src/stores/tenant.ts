import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { getTenantContext, getTenantContexts, type TenantContext } from '@/api/tenantContext'

export const useTenantStore = defineStore('tenant', () => {
  const tenantId = ref(localStorage.getItem('tenant_context') || '')
	const contexts = ref<TenantContext[]>([])
	const loading = ref(false)
	const loaded = ref(false)
	const invalid = ref(false)
	const error = ref('')
	const contextRevision = ref(0)
	let loadPromise: Promise<void> | null = null

	const current = computed(() => contexts.value.find(item => item.tenant_id === tenantId.value))
	const tenantRole = computed(() => current.value?.management_role || '')
	const permissions = computed(() => current.value?.permissions || [])

	const persistTenant = (value: string) => {
    tenantId.value = value
    if (value) localStorage.setItem('tenant_context', value)
    else localStorage.removeItem('tenant_context')
  }

	const clearTenantState = () => {
		persistTenant('')
		invalid.value = true
		contextRevision.value++
	}

	const syncTenantContext = (value: string) => {
		const previous = tenantId.value
		if (value === previous) return
		persistTenant(value)
		invalid.value = false
		contextRevision.value++
		window.dispatchEvent(new CustomEvent('tenant-context-changed', { detail: { previous, current: value } }))
	}

	const reset = () => {
		persistTenant('')
		contexts.value = []
		loaded.value = false
		invalid.value = false
		error.value = ''
		contextRevision.value++
	}

	const loadContexts = (force = false): Promise<void> => {
		if (loading.value && loadPromise) return loadPromise
		if (loaded.value && !force) return Promise.resolve()
		loading.value = true
		error.value = ''
		loadPromise = (async () => {
			try {
				const response = await getTenantContexts()
				contexts.value = response.success && response.data ? response.data : []
				loaded.value = true
				invalid.value = false
				if (tenantId.value && !contexts.value.some(item => item.tenant_id === tenantId.value)) {
					persistTenant('')
				}
				if (!tenantId.value && contexts.value.length > 0) persistTenant(contexts.value[0].tenant_id)
			} catch (cause) {
				error.value = cause instanceof Error ? cause.message : '租户上下文加载失败'
				throw cause
			} finally {
				loading.value = false
				loadPromise = null
			}
		})()
		return loadPromise
	}

	const selectTenant = async (value: string) => {
		const previous = tenantId.value
		if (!value || value === previous) return true
		try {
			const response = await getTenantContext(value)
			if (!response.success || !response.data) return false
			const index = contexts.value.findIndex(item => item.tenant_id === value)
			if (index >= 0) contexts.value[index] = response.data
			else contexts.value.push(response.data)
			persistTenant(value)
			invalid.value = false
			contextRevision.value++
			window.dispatchEvent(new CustomEvent('tenant-context-changed', { detail: { previous, current: value } }))
			return true
		} catch {
			persistTenant(previous)
			return false
		}
	}

	const canTenant = (permission: string) => permissions.value.includes(permission)

	return {
		tenantId,
		contexts,
		current,
		tenantRole,
		permissions,
		loading,
		loaded,
		invalid,
		error,
		contextRevision,
		loadContexts,
		selectTenant,
		syncTenantContext,
		clearTenantState,
		reset,
		canTenant
	}
})
