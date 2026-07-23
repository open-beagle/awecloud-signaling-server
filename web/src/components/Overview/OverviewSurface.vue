<template>
  <div class="overview-content">
    <div class="metric-strip" :style="{ '--metric-count': metrics.length }">
      <div v-for="metric in metrics" :key="metric.label" class="metric-item">
        <span>{{ metric.label }}</span>
        <strong :class="metric.tone">{{ metric.value }}</strong>
        <small>{{ metric.note }}</small>
      </div>
    </div>

    <el-alert class="scope-callout" :title="calloutTitle" :description="calloutDescription" type="info" show-icon :closable="false" />

    <section class="attention-surface">
      <div class="section-heading">
        <div><strong>{{ sectionTitle }}</strong><span>{{ sectionDescription }}</span></div>
        <span>{{ items.length }} 项</span>
      </div>
      <el-table v-if="items.length" :data="items" stripe>
        <el-table-column label="关注事项" min-width="260">
          <template #default="{ row }"><strong>{{ row.title }}</strong><span class="secondary">{{ row.detail || kindLabel(row.kind) }}</span></template>
        </el-table-column>
        <el-table-column label="业务对象" width="150"><template #default="{ row }">{{ kindLabel(row.kind) }}</template></el-table-column>
        <el-table-column label="状态" width="125"><template #default="{ row }"><el-tag size="small" :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
        <el-table-column label="更新时间" width="180"><template #default="{ row }">{{ formatTime(row.updated_at) }}</template></el-table-column>
        <el-table-column label="操作" width="100" align="right">
          <template #default="{ row }"><el-button v-if="row.route" link type="primary" @click="$emit('open', row.route)">查看</el-button><span v-else class="secondary">需切换租户</span></template>
        </el-table-column>
      </el-table>
      <el-empty v-else :description="emptyDescription" />
    </section>
  </div>
</template>

<script setup lang="ts">
import type { OverviewAttentionItem } from '@/api/overview'

export interface OverviewMetric { label: string; value: number; note: string; tone?: 'success' | 'danger' | 'warning' }

defineProps<{
  metrics: OverviewMetric[]
  items: OverviewAttentionItem[]
  calloutTitle: string
  calloutDescription: string
  sectionTitle: string
  sectionDescription: string
  emptyDescription: string
}>()
defineEmits<{ open: [route: string] }>()

const kindLabel = (kind: string) => ({ tenant: '租户', candidate: '发现候选', resource: '资源' }[kind] || kind)
const statusLabel = (status: string) => ({ pending: '待发布', degraded: '异常', stopped: '已停止', suspended: '已暂停', conflict: '冲突' }[status] || status)
const statusType = (status: string) => status === 'conflict' || status === 'degraded' ? 'danger' : 'warning'
const formatTime = (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false })
</script>

<style scoped>
.metric-strip { display: grid; grid-template-columns: repeat(var(--metric-count), minmax(0, 1fr)); overflow: hidden; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.metric-item { min-width: 0; padding: 14px 16px; border-right: 1px solid var(--border-light); }
.metric-item:last-child { border-right: 0; }
.metric-item span, .metric-item small { display: block; color: var(--text-secondary); font-size: 12px; }
.metric-item strong { display: block; margin: 4px 0 2px; color: var(--text-primary); font-size: 22px; line-height: 28px; }
.metric-item strong.success { color: var(--success-color); }
.metric-item strong.warning { color: var(--warning-color); }
.metric-item strong.danger { color: var(--danger-color); }
.scope-callout { margin-top: 14px; }
.attention-surface { margin-top: 14px; overflow: hidden; border: 1px solid var(--border-light); border-radius: 6px; background: #fff; }
.section-heading { display: flex; align-items: center; justify-content: space-between; gap: 20px; padding: 14px 16px; border-bottom: 1px solid var(--border-light); }
.section-heading div { display: flex; flex-direction: column; gap: 3px; }
.section-heading strong { color: var(--text-primary); font-size: 15px; }
.section-heading span, .secondary { color: var(--text-secondary); font-size: 12px; }
.secondary { display: block; margin-top: 3px; }
@media (max-width: 900px) { .metric-strip { grid-template-columns: repeat(3, 1fr); } .metric-item:nth-child(n+4) { border-top: 1px solid var(--border-light); } }
@media (max-width: 600px) { .metric-strip { grid-template-columns: repeat(2, 1fr); } .metric-item:nth-child(n) { border-right: 1px solid var(--border-light); } .metric-item:nth-child(even) { border-right: 0; } .metric-item:nth-child(n+3) { border-top: 1px solid var(--border-light); } }
</style>
