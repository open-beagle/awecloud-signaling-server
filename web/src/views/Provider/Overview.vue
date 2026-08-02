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

    <el-alert v-if="errorMessage" class="state-alert" title="资源供给概览加载失败" :description="errorMessage" type="error" show-icon :closable="false" />

    <section v-loading="loading" class="inventory-section">
      <div class="section-heading">
        <div><h2>资源供给</h2><p>当前资源方的可信入口与待处理发现结果</p></div>
        <span>{{ inventoryRows.length }} 类对象</span>
      </div>
      <div class="inventory-list">
        <button v-for="item in inventoryRows" :key="item.route" type="button" @click="router.push(item.route)">
          <span><strong>{{ item.label }}</strong><small>{{ item.description }}</small></span>
          <span class="inventory-count">{{ item.count }}</span>
          <el-icon><ArrowRight /></el-icon>
        </button>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowRight, Refresh } from '@element-plus/icons-vue'
import { getProviderPlatformResources, getProviderResourceScopes, getProviderSupplyCandidates, getProviderTechnicalResources } from '@/api/providerSupply'
import { useWorkspaceStore } from '@/stores/workspace'

const router = useRouter()
const workspaceStore = useWorkspaceStore()
const context = computed(() => workspaceStore.currentContext)
const loading = ref(false)
const errorMessage = ref('')
const technicalResourceCount = ref(0)
const supplyCandidateCount = ref(0)
const platformResourceCount = ref(0)
const allocatableScopeCount = ref(0)
const roleLabel = computed(() => ({
  provider_admin: '资源管理员',
  provider_operator: '资源运维员',
  provider_viewer: '资源观察员'
}[context.value?.role || ''] || context.value?.role || '-'))
const inventoryRows = computed(() => [
  { label: '技术资源', description: 'Agent 与 Endpoint 注册、健康和库存租约', count: technicalResourceCount.value, route: '/provider-technical-resources' },
  { label: '供给候选', description: 'Host 与 Kubernetes 可供给对象的身份审核', count: supplyCandidateCount.value, route: '/provider-supply-candidates' },
  { label: '供给资源', description: '已确认的 Host 与 Kubernetes 资源', count: platformResourceCount.value, route: '/provider-kubernetes' },
  { label: '可分配 Scope', description: '已完成隔离证据校验的 Scope', count: allocatableScopeCount.value, route: '/provider-namespaces' },
])

const loadInventory = async () => {
  const providerId = workspaceStore.providerId
  if (!providerId) return
  loading.value = true
  errorMessage.value = ''
  try {
    const [technical, candidates, resources, scopes] = await Promise.all([
      getProviderTechnicalResources(providerId, { page: 1, size: 1 }),
      getProviderSupplyCandidates(providerId, { page: 1, size: 1 }),
      getProviderPlatformResources(providerId, { page: 1, size: 1 }),
      getProviderResourceScopes(providerId, { state: 'allocatable', page: 1, size: 1 }),
    ])
    technicalResourceCount.value = technical.total || 0
    supplyCandidateCount.value = candidates.total || 0
    platformResourceCount.value = resources.total || 0
    allocatableScopeCount.value = scopes.total || 0
  } catch {
    technicalResourceCount.value = 0
    supplyCandidateCount.value = 0
    platformResourceCount.value = 0
    allocatableScopeCount.value = 0
    errorMessage.value = '请确认当前资源方权限和服务状态后重试。'
  } finally {
    loading.value = false
  }
}
const reload = async () => {
  await workspaceStore.loadContexts(true).catch(() => undefined)
  await loadInventory()
}

watch(() => workspaceStore.providerId, () => loadInventory())
onMounted(loadInventory)
</script>

<style scoped>
.provider-overview { max-width: none; }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; margin-bottom: 18px; }
h1 { margin: 0; color: var(--text-primary); font-size: 24px; line-height: 32px; letter-spacing: 0; }
.page-header p, .section-heading p { margin: 5px 0 0; color: var(--text-secondary); font-size: 13px; }
.state-alert { margin-bottom: 14px; }
.summary-band { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border: 1px solid var(--border-light); border-radius: 7px; background: #fff; }
.summary-band > div { min-width: 0; padding: 16px 18px; border-right: 1px solid var(--border-light); }
.summary-band > div:last-child { border-right: 0; }
.summary-band span, .summary-band strong { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.summary-band span { color: var(--text-secondary); font-size: 12px; }
.summary-band strong { margin-top: 5px; color: var(--text-primary); font-size: 17px; }
.inventory-section { margin-top: 18px; padding: 18px; border-top: 1px solid var(--border-light); background: #fff; }
.section-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.section-heading h2 { margin: 0; color: var(--text-primary); font-size: 17px; line-height: 24px; letter-spacing: 0; }
.section-heading > span { color: var(--text-secondary); font-size: 12px; }
.inventory-list { margin-top: 14px; border-top: 1px solid var(--border-light); }
.inventory-list button { display: grid; grid-template-columns: minmax(0, 1fr) auto 20px; align-items: center; gap: 18px; width: 100%; padding: 15px 4px; border: 0; border-bottom: 1px solid var(--border-lighter); background: transparent; color: var(--text-primary); cursor: pointer; text-align: left; }
.inventory-list button:last-child { border-bottom: 0; }
.inventory-list button:hover { background: var(--bg-subtle); }
.inventory-list button:focus-visible { outline: 2px solid var(--primary-color); outline-offset: -2px; }
.inventory-list strong, .inventory-list small { display: block; }
.inventory-list small { margin-top: 3px; color: var(--text-secondary); font-size: 12px; }
.inventory-count { min-width: 46px; color: var(--text-primary); font-size: 20px; font-weight: 650; text-align: right; }
.inventory-list .el-icon { color: var(--text-secondary); }
</style>
