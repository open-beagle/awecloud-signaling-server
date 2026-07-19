import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useAppStore = defineStore('app', () => {
  const sidebarCollapsed = ref(false)
  const loading = ref(false)
  const mobileSidebarOpen = ref(false)

  const toggleSidebar = () => {
    sidebarCollapsed.value = !sidebarCollapsed.value
  }

  const setLoading = (value: boolean) => {
    loading.value = value
  }

  const toggleMobileSidebar = () => { mobileSidebarOpen.value = !mobileSidebarOpen.value }
  const closeMobileSidebar = () => { mobileSidebarOpen.value = false }

  return {
    sidebarCollapsed,
    loading,
    mobileSidebarOpen,
    toggleSidebar,
    toggleMobileSidebar,
    closeMobileSidebar,
    setLoading
  }
})
