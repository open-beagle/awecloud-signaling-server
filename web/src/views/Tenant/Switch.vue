<template>
  <div class="page-container tenant-switch-page">
    <div class="page-header">
      <div>
        <h1>租户切换</h1>
        <p>选择要进入的租户业务空间。切换后只改变租户侧菜单和租户级权限。</p>
      </div>
      <el-button :loading="tenantStore.loading" @click="reload">刷新</el-button>
    </div>

    <el-alert v-if="tenantStore.error" :title="tenantStore.error" type="error" show-icon :closable="false" />
    <el-skeleton v-if="tenantStore.loading && !tenantStore.loaded" :rows="4" animated />
    <el-empty v-else-if="tenantStore.contexts.length === 0" description="当前管理员没有可进入的租户业务空间" />
    <div v-else class="tenant-list" role="list" aria-label="可管理租户">
      <article v-for="tenant in tenantStore.contexts" :key="tenant.tenant_id" class="tenant-row" :class="{ current: tenant.tenant_id === tenantStore.tenantId }" role="listitem">
        <div class="tenant-main">
          <el-icon><OfficeBuilding /></el-icon>
          <div>
            <strong>{{ tenant.tenant_name }}</strong>
            <span>{{ tenant.tenant_key }}</span>
          </div>
        </div>
        <div class="tenant-meta">
          <el-tag size="small" effect="plain" :type="tenant.tenant_status === 'suspended' ? 'warning' : 'success'">{{ tenant.tenant_status === 'suspended' ? '已暂停' : '正常' }}</el-tag>
          <span>{{ roleLabel(tenant.management_role) }}</span>
          <small>{{ tenant.permissions.length }} 项权限</small>
        </div>
        <el-button v-if="tenant.tenant_id === tenantStore.tenantId" disabled>当前租户</el-button>
        <el-button v-else type="primary" :loading="switching === tenant.tenant_id" @click="enterTenant(tenant.tenant_id)">进入租户</el-button>
      </article>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { OfficeBuilding } from '@element-plus/icons-vue'
import { useTenantStore } from '@/stores/tenant'

const router = useRouter()
const tenantStore = useTenantStore()
const switching = ref('')
const roleLabel = (role: string) => ({ tenant_admin: '租户管理员', security_auditor: '安全审计员', tenant_viewer: '租户观察员' }[role] || role)

const reload = () => tenantStore.loadContexts(true).catch(() => undefined)
const enterTenant = async (tenantId: string) => {
  switching.value = tenantId
  const changed = await tenantStore.selectTenant(tenantId)
  switching.value = ''
  if (!changed) return ElMessage.error('租户切换失败，原租户上下文保持不变')
  ElMessage.success('已进入租户业务空间')
  const destination = tenantStore.canTenant('tenant.resources.read') ? '/resources' : '/tenant-members'
  router.push(destination)
}
</script>

<style scoped>
.tenant-switch-page { max-width: none; }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; margin-bottom: 18px; }
h1 { margin: 0; color: var(--text-primary); font-size: 24px; line-height: 32px; }
.page-header p { margin: 5px 0 0; color: var(--text-secondary); font-size: 13px; }
.tenant-list { margin-top: 14px; overflow: hidden; border: 1px solid var(--border-light); border-radius: 7px; background: #fff; }
.tenant-row { display: grid; grid-template-columns: minmax(280px, 1fr) minmax(320px, auto) 100px; align-items: center; gap: 24px; min-height: 72px; padding: 10px 14px; border-bottom: 1px solid var(--border-light); }
.tenant-row:last-child { border-bottom: 0; }
.tenant-row.current { background: #f6f9ff; }
.tenant-main, .tenant-meta { display: flex; align-items: center; gap: 12px; }
.tenant-main > .el-icon { color: var(--primary-color); font-size: 19px; }
.tenant-main div { min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.tenant-main strong { overflow: hidden; color: var(--text-primary); font-size: 14px; text-overflow: ellipsis; white-space: nowrap; }
.tenant-main span, .tenant-meta small { color: var(--text-secondary); font-size: 12px; }
.tenant-meta > span { min-width: 84px; color: var(--text-regular); font-size: 13px; }
@media (max-width: 900px) { .tenant-row { grid-template-columns: 1fr auto; gap: 12px; } .tenant-meta { grid-column: 1 / -1; grid-row: 2; } }
@media (max-width: 600px) { .page-header { align-items: stretch; flex-direction: column; } .tenant-row { grid-template-columns: 1fr; } .tenant-meta { grid-column: auto; grid-row: auto; flex-wrap: wrap; } }
</style>
