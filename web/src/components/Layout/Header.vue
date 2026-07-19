<template>
  <div class="header">
    <div class="header-left">
      <Logo :collapsed="false" class="header-logo" />
      <el-select
        v-model="tenantStore.tenantId"
        class="tenant-select"
        size="small"
        placeholder="全部客户（只读）"
        clearable
        @change="handleTenantChange"
      >
        <el-option label="全部客户（只读）" value="" />
        <el-option v-for="tenant in tenants" :key="tenant.id" :label="tenant.name" :value="tenant.id" />
      </el-select>
    </div>
    <div class="header-right">
      <span class="client-download" @click="handleDownloadClient">
        <el-icon><Download /></el-icon>
        客户端
      </span>
      <el-dropdown @command="handleLanguageChange">
        <span class="language-selector">
          <el-icon><Globe /></el-icon>
          {{ currentLanguage }}
        </span>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="zh-CN">中文</el-dropdown-item>
            <el-dropdown-item command="en-US">English</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
      <el-dropdown @command="handleCommand">
        <span class="user-info">
          <el-icon><User /></el-icon>
          {{ authStore.username }}
          <el-tag v-if="authStore.role" size="small" effect="plain">{{ roleLabel }}</el-tag>
        </span>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="logout">
              <el-icon><SwitchButton /></el-icon>
              {{ t('common.logout') }}
            </el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { useTenantStore } from '@/stores/tenant'
import { getTenants, type Tenant } from '@/api/resource'
import { ElMessage } from 'element-plus'
import Logo from '@/components/Common/Logo.vue'

const router = useRouter()
const { t, locale } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const tenantStore = useTenantStore()
const tenants = ref<Tenant[]>([])

const currentLanguage = computed(() => {
  return locale.value === 'zh-CN' ? '中文' : 'English'
})
const roleLabel = computed(() => ({ admin: 'Platform Admin', tenant_admin: 'Tenant Admin', viewer: 'Viewer' }[authStore.role] || authStore.role))

const toggleSidebar = () => {
  appStore.toggleSidebar()
}

const handleLanguageChange = (lang: string) => {
  locale.value = lang
  localStorage.setItem('locale', lang)
  ElMessage.success(t('common.success'))
}

const handleCommand = async (command: string) => {
  if (command === 'logout') {
    await authStore.logout()
    router.push('/login')
  }
}

const handleDownloadClient = () => {
  window.open('/download', '_blank')
}

const handleTenantChange = () => {
  tenantStore.setTenant(tenantStore.tenantId)
}

onMounted(async () => {
  try {
    if (!authStore.role) await authStore.loadProfile()
    const res = await getTenants({ page: 1, size: 100, status: 'active' })
    if (res.success && res.data) {
      tenants.value = res.data
      if (tenantStore.tenantId && !tenants.value.some(tenant => tenant.id === tenantStore.tenantId)) {
        tenantStore.setTenant('')
      }
    }
  } catch (error) {
    console.error('获取客户上下文失败:', error)
  }
})


</script>

<style scoped>
.header {
  width: 100%;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 20px;
}

.header-logo {
  padding: 0;
}

.tenant-select {
  width: 220px;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 20px;
}

@media (max-width: 900px) {
  .tenant-select {
    width: 180px;
  }
}

@media (max-width: 640px) {
  .tenant-select {
    width: 150px;
  }
}

.client-download,
.language-selector,
.user-info {
  display: flex;
  align-items: center;
  gap: 5px;
  cursor: pointer;
  color: var(--text-primary);
  font-size: 14px;
  padding: 8px 12px;
  border-radius: 4px;
  transition: all 0.3s;
}

.client-download:hover,
.language-selector:hover,
.user-info:hover {
  color: var(--primary-color);
  background-color: #ecf5ff;
}
</style>
