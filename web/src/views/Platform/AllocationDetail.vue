<template>
  <div class="platform-page">
    <PageHeader title="资源分配详情" description="查看权威 Allocation 聚合、Namespace Scope 快照和生命周期证据。">
      <template #actions><el-button @click="router.push('/platform-allocations')">返回列表</el-button><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button></template>
    </PageHeader>

    <el-alert v-if="errorMessage" class="state-alert" title="资源分配详情加载失败" :description="errorMessage" type="error" show-icon :closable="false" />

    <template v-if="allocation">
      <section class="detail-card">
        <div class="card-heading"><div><span class="eyebrow">Allocation</span><h2 class="mono">{{ allocation.id }}</h2></div><el-tag :type="stateTag(allocation.state)">{{ stateLabel(allocation.state) }}</el-tag></div>
        <el-descriptions :column="3" border>
          <el-descriptions-item label="Tenant"><span class="mono">{{ allocation.tenant_id }}</span></el-descriptions-item>
          <el-descriptions-item label="分配方式">{{ modeLabel(allocation.mode) }}</el-descriptions-item>
          <el-descriptions-item label="对象 Revision">{{ allocation.row_version }}</el-descriptions-item>
          <el-descriptions-item label="生效时间">{{ formatTime(allocation.valid_from) }}</el-descriptions-item>
          <el-descriptions-item label="到期时间">{{ formatTime(allocation.expires_at) }}</el-descriptions-item>
          <el-descriptions-item label="合同引用">{{ allocation.contract_ref || '-' }}</el-descriptions-item>
          <el-descriptions-item label="创建人">{{ allocation.created_by_user_id }}</el-descriptions-item>
          <el-descriptions-item label="激活时间">{{ formatTime(allocation.activated_at) }}</el-descriptions-item>
          <el-descriptions-item label="终止时间">{{ formatTime(allocation.terminated_at) }}</el-descriptions-item>
          <el-descriptions-item v-if="allocation.renewed_from_id" label="续期来源"><span class="mono">{{ allocation.renewed_from_id }}</span></el-descriptions-item>
          <el-descriptions-item v-if="allocation.termination_reason" label="终止原因" :span="2">{{ allocation.termination_reason }}</el-descriptions-item>
        </el-descriptions>
      </section>

      <section class="detail-card">
        <div class="section-heading"><div><h2>Namespace Scope</h2><p>Allocation Item 保存分配时的 Scope revision 快照。</p></div><span>{{ allocation.items.length }} 项</span></div>
        <el-table :data="allocation.items" stripe>
          <el-table-column label="Scope ID" min-width="280"><template #default="{ row }"><span class="mono">{{ row.scope_id }}</span></template></el-table-column>
          <el-table-column label="Scope Revision 快照" width="190"><template #default="{ row }">{{ row.scope_row_version_snapshot }}</template></el-table-column>
          <el-table-column label="Allocation Item" min-width="280"><template #default="{ row }"><span class="mono">{{ row.id }}</span></template></el-table-column>
          <el-table-column label="创建时间" width="190"><template #default="{ row }">{{ formatTime(row.created_at) }}</template></el-table-column>
        </el-table>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { useRoute, useRouter } from 'vue-router'
import PageHeader from '@/components/Common/PageHeader.vue'
import { getPlatformAllocation, type ResourceAllocation, type ResourceAllocationMode, type ResourceAllocationState } from '@/api/platformAllocations'

const route = useRoute()
const router = useRouter()
const loading = ref(false)
const errorMessage = ref('')
const allocation = ref<ResourceAllocation>()

const load = async () => {
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await getPlatformAllocation(String(route.params.id))
    allocation.value = response.success ? response.data : undefined
    if (!allocation.value) errorMessage.value = '资源分配不存在或当前身份不可见。'
  } catch {
    allocation.value = undefined
    errorMessage.value = '请确认平台资源分配读取权限、对象状态和服务状态后重试。'
  } finally {
    loading.value = false
  }
}

const modeLabel = (mode: ResourceAllocationMode) => ({ assigned: '独占分配', leased: '租约分配', shared: '共享分配' }[mode])
const stateLabel = (state: ResourceAllocationState) => ({ draft: '草稿', scheduled: '待生效', active: '生效中', suspended: '已暂停', expired: '已到期', revoked: '已撤销' }[state])
const stateTag = (state: ResourceAllocationState) => ({ draft: 'info', scheduled: 'warning', active: 'success', suspended: 'warning', expired: 'info', revoked: 'danger' }[state] as any)
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'

onMounted(load)
</script>

<style scoped>
.platform-page { width: 100%; }
.state-alert { margin-bottom: 14px; }
.detail-card { margin-bottom: 16px; padding: 18px; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.card-heading, .section-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; margin-bottom: 16px; }
.eyebrow { color: var(--text-secondary); font-size: 12px; }
h2 { margin: 0; color: var(--text-primary); font-size: 18px; }
.card-heading h2 { margin-top: 4px; }
.section-heading p { margin: 4px 0 0; color: var(--text-secondary); font-size: 12px; }
.section-heading > span { color: var(--text-secondary); font-size: 12px; }
.mono { font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', monospace; font-size: 12px; }
</style>
