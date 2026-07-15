import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useTenantStore = defineStore('tenant', () => {
  const tenantId = ref(localStorage.getItem('tenant_context') || '')

  const setTenant = (value: string) => {
    tenantId.value = value
    if (value) localStorage.setItem('tenant_context', value)
    else localStorage.removeItem('tenant_context')
  }

  return { tenantId, setTenant }
})
