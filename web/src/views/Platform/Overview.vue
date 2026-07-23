<template>
  <div class="page-container overview-page">
    <div class="page-header">
      <div><h1>平台概览</h1><p>查看租户安全边界、全局资源和基础设施的运行情况。</p></div>
      <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
    </div>
    <el-skeleton v-if="loading && !data" :rows="6" animated />
    <OverviewSurface
      v-else
      :metrics="metrics"
      :items="data?.attention || []"
      callout-title="当前页面属于管理员侧"
      callout-description="本页只呈现平台治理信息；进入某个租户的业务数据前，仍需具备有效租户管理授权并切换租户上下文。"
      section-title="平台关注事项"
      section-description="暂停租户、发现冲突和异常资源的只读汇总"
      empty-description="平台当前没有高风险关注事项"
      @open="router.push"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Refresh } from '@element-plus/icons-vue'
import { getPlatformOverview, type PlatformOverview } from '@/api/overview'
import OverviewSurface, { type OverviewMetric } from '@/components/Overview/OverviewSurface.vue'

const router = useRouter()
const loading = ref(false)
const data = ref<PlatformOverview>()
const metrics = computed<OverviewMetric[]>(() => [
  { label: '租户', value: data.value?.tenant_count || 0, note: '安全边界' },
  { label: '租户管理员授权', value: data.value?.admin_membership_count || 0, note: '当前有效' },
  { label: '资源', value: data.value?.resource_count || 0, note: '全局资源' },
  { label: '基础设施', value: (data.value?.agent_count || 0) + (data.value?.endpoint_count || 0), note: `${data.value?.agent_count || 0} Agent + ${data.value?.endpoint_count || 0} Endpoint` },
  { label: '高风险事项', value: data.value?.high_risk_count || 0, note: '跨租户治理', tone: data.value?.high_risk_count ? 'danger' : 'success' }
])
const load = async () => {
  loading.value = true
  try { const response = await getPlatformOverview(); data.value = response.success ? response.data : undefined }
  finally { loading.value = false }
}
onMounted(load)
</script>

<style scoped>
.overview-page { max-width: none; }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; margin-bottom: 18px; }
h1 { margin: 0; color: var(--text-primary); font-size: 24px; line-height: 32px; }
.page-header p { margin: 5px 0 0; color: var(--text-secondary); font-size: 13px; }
@media (max-width: 600px) { .page-header { flex-direction: column; } }
</style>
