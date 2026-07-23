<template>
  <div class="page-container overview-page">
    <div class="page-header">
      <div><h1>租户概览</h1><p>查看当前租户的成员、资源、访问与风险摘要。</p></div>
      <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
    </div>
    <el-skeleton v-if="loading && !data" :rows="6" animated />
    <OverviewSurface
      v-else
      :metrics="metrics"
      :items="data?.attention || []"
      callout-title="租户安全边界"
      callout-description="本页统计和事项均限定在当前租户；成员设备是基于成员关系得到的视图，不表示租户拥有设备。"
      section-title="需要关注"
      section-description="当前租户中待发布、异常或已停止的资源"
      empty-description="当前租户没有需要处理的风险事项"
      @open="router.push"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Refresh } from '@element-plus/icons-vue'
import { getTenantOverview, type TenantOverview } from '@/api/overview'
import OverviewSurface, { type OverviewMetric } from '@/components/Overview/OverviewSurface.vue'
import { useTenantStore } from '@/stores/tenant'

const router = useRouter()
const tenantStore = useTenantStore()
const loading = ref(false)
const data = ref<TenantOverview>()
const metrics = computed<OverviewMetric[]>(() => [
  { label: '有效成员', value: data.value?.member_count || 0, note: 'TenantMembership' },
  { label: '用户组', value: data.value?.group_count || 0, note: '当前租户' },
  { label: '资源', value: data.value?.resource_count || 0, note: '当前租户' },
  { label: '活动会话', value: data.value?.active_sessions || 0, note: '实时连接' },
  { label: '风险事项', value: data.value?.risk_count || 0, note: '当前租户', tone: data.value?.risk_count ? 'danger' : 'success' }
])
const load = async () => {
  if (!tenantStore.tenantId) return
  loading.value = true
  try { const response = await getTenantOverview(tenantStore.tenantId); data.value = response.success ? response.data : undefined }
  finally { loading.value = false }
}
watch(() => tenantStore.contextRevision, load)
onMounted(load)
</script>

<style scoped>
.overview-page { max-width: none; }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; margin-bottom: 18px; }
h1 { margin: 0; color: var(--text-primary); font-size: 24px; line-height: 32px; }
.page-header p { margin: 5px 0 0; color: var(--text-secondary); font-size: 13px; }
@media (max-width: 600px) { .page-header { flex-direction: column; } }
</style>
