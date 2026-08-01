<template>
  <div class="page-container provider-overview">
    <div class="page-header">
      <div>
        <h1>资源概览</h1>
        <p>{{ context?.scope_name || context?.scope_key || context?.scope_id }}</p>
      </div>
      <el-button :icon="Refresh" :loading="workspaceStore.loading" @click="reload">刷新</el-button>
    </div>

    <div class="summary-band">
      <div><span>Provider</span><strong>{{ context?.scope_name || '-' }}</strong></div>
      <div><span>角色</span><strong>{{ roleLabel }}</strong></div>
      <div><span>权限</span><strong>{{ context?.permissions.length || 0 }}</strong></div>
      <div><span>状态</span><strong>{{ context?.scope_status === 'suspended' ? '已暂停' : '正常' }}</strong></div>
    </div>

    <section class="inventory-section">
      <div class="section-heading">
        <div><h2>资源供给</h2><p>技术资源、主机、Kubernetes 与 Namespace</p></div>
        <el-tag effect="plain" type="info">S2</el-tag>
      </div>
      <el-empty description="当前 Provider 尚无可展示的供给数据" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { useWorkspaceStore } from '@/stores/workspace'

const workspaceStore = useWorkspaceStore()
const context = computed(() => workspaceStore.currentContext)
const roleLabel = computed(() => ({
  provider_admin: '资源管理员',
  provider_operator: '资源运维员',
  provider_viewer: '资源观察员'
}[context.value?.role || ''] || context.value?.role || '-'))
const reload = () => workspaceStore.loadContexts(true).catch(() => undefined)
</script>

<style scoped>
.provider-overview { max-width: none; }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; margin-bottom: 18px; }
h1 { margin: 0; color: var(--text-primary); font-size: 24px; line-height: 32px; letter-spacing: 0; }
.page-header p, .section-heading p { margin: 5px 0 0; color: var(--text-secondary); font-size: 13px; }
.summary-band { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border: 1px solid var(--border-light); border-radius: 7px; background: #fff; }
.summary-band > div { min-width: 0; padding: 16px 18px; border-right: 1px solid var(--border-light); }
.summary-band > div:last-child { border-right: 0; }
.summary-band span, .summary-band strong { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.summary-band span { color: var(--text-secondary); font-size: 12px; }
.summary-band strong { margin-top: 5px; color: var(--text-primary); font-size: 17px; }
.inventory-section { margin-top: 18px; padding: 18px; border-top: 1px solid var(--border-light); background: #fff; }
.section-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.section-heading h2 { margin: 0; color: var(--text-primary); font-size: 17px; line-height: 24px; letter-spacing: 0; }
@media (max-width: 700px) { .summary-band { grid-template-columns: repeat(2, minmax(0, 1fr)); } .summary-band > div:nth-child(2) { border-right: 0; } .summary-band > div:nth-child(-n + 2) { border-bottom: 1px solid var(--border-light); } }
@media (max-width: 520px) { .page-header { flex-direction: column; } }
</style>
