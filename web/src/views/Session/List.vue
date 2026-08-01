<template>
  <div class="session-page">
    <PageHeader title="访问会话" eyebrow="访问运行态" description="查看 ContainerSSH 会话、目标 revision 和关闭原因。强制断开要求 Agent 支持协议 v1。">
      <template #actions><el-button :icon="Refresh" :loading="loading" @click="fetchSessions">刷新</el-button></template>
    </PageHeader>
    <div class="status-strip"><button v-for="item in statusOptions" :key="item.value" :class="{ active: filters.status === item.value }" @click="selectStatus(item.value)">{{ item.label }}</button></div>
    <div class="surface">
      <div class="toolbar"><span>{{ pagination.total }} 条会话</span></div>
      <el-table v-loading="loading" :data="sessions" stripe>
        <el-table-column label="资源" min-width="220"><template #default="{ row }"><el-link type="primary" :underline="false" @click="router.push(`/resources/${row.resource_id}`)">{{ row.resource_name || row.resource_id }}</el-link><span class="secondary">{{ row.workspace_id || 'ContainerSSH' }}</span></template></el-table-column>
        <el-table-column label="用户 / 设备" min-width="180"><template #default="{ row }"><strong>{{ row.user_name || `User ${row.user_id}` }}</strong><span class="secondary">Device {{ row.device_id || '-' }}</span></template></el-table-column>
        <el-table-column label="版本" width="150"><template #default="{ row }"><span>Grant {{ row.grant_revision }}</span><span class="secondary">Target {{ row.target_revision }}</span></template></el-table-column>
        <el-table-column label="开始时间" width="180"><template #default="{ row }">{{ formatTime(row.started_at) }}</template></el-table-column>
        <el-table-column label="状态" width="120"><template #default="{ row }"><el-tag size="small" :type="statusTag(row.status)">{{ statusLabel(row.status) }}</el-tag><span v-if="row.close_reason" class="secondary reason">{{ row.close_reason }}</span></template></el-table-column>
        <el-table-column label="操作" width="120" fixed="right" align="right"><template #default="{ row }"><el-button v-if="row.status === 'active'" link type="danger" :disabled="!canDisconnect(row)" @click="disconnect(row)">强制断开</el-button></template></el-table-column>
      </el-table>
      <el-empty v-if="!loading && !sessions.length" description="当前范围没有会话" />
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" :total="pagination.total" @size-change="fetchSessions" @current-change="fetchSessions" /></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import { forceDisconnectSession, getSessions, type ContainerSession, type ContainerSessionStatus } from '@/api/resource'
import { useAuthStore } from '@/stores/auth'
import { useTenantStore } from '@/stores/tenant'

const router = useRouter(), route = useRoute(), authStore = useAuthStore(), tenantStore = useTenantStore()
const loading = ref(false), sessions = ref<ContainerSession[]>([])
const filters = reactive<{ status?: ContainerSessionStatus }>({ status: 'active' })
const pagination = reactive({ page: 1, size: 20, total: 0 })
const statusOptions: { label: string; value?: ContainerSessionStatus }[] = [{ label: '活动中', value: 'active' }, { label: '已结束', value: 'ended' }, { label: '已撤销', value: 'revoked' }, { label: '全部', value: undefined }]
const fetchSessions = async () => { loading.value = true; try { const res = await getSessions({ tenant_id: tenantStore.tenantId || undefined, resource_id: typeof route.query.resource_id === 'string' ? route.query.resource_id : undefined, status: filters.status, page: pagination.page, size: pagination.size }); if (res.success && res.data) { sessions.value = res.data; pagination.total = res.total } } finally { loading.value = false } }
const selectStatus = (status?: ContainerSessionStatus) => { filters.status = status; pagination.page = 1; fetchSessions() }
const canDisconnect = (row: ContainerSession) => authStore.canWrite && tenantStore.tenantId === row.tenant_id
const disconnect = async (row: ContainerSession) => { if (!canDisconnect(row)) return; try { const result = await ElMessageBox.prompt('请输入断开原因，原因将写入审计记录。', '强制断开会话', { confirmButtonText: '断开', cancelButtonText: '取消', inputValue: '管理员强制断开会话', inputPattern: /\S+/, inputErrorMessage: '请输入断开原因', type: 'warning' }); const res = await forceDisconnectSession(row.id, result.value); if (res.success) { ElMessage.success('会话断开指令已下发'); await fetchSessions() } } catch (error) { if (error !== 'cancel' && error !== 'close') throw error } }
const statusLabel = (status: ContainerSessionStatus) => ({ active: '活动中', ended: '已结束', revoked: '已撤销', rejected: '已拒绝' }[status])
const statusTag = (status: ContainerSessionStatus) => ({ active: 'success', ended: 'info', revoked: 'warning', rejected: 'danger' }[status] as any)
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'
watch(() => tenantStore.tenantId, () => { pagination.page = 1; fetchSessions() })
onMounted(fetchSessions)
</script>

<style scoped>
    .session-page{width:100%}.secondary,.toolbar{color:var(--text-secondary);font-size:12px}.status-strip{display:flex;gap:4px;margin-bottom:12px}.status-strip button{height:34px;padding:0 13px;border:1px solid var(--border-light);border-radius:4px;background:#fff;color:var(--text-regular);cursor:pointer}.status-strip button.active{border-color:var(--primary-color);color:var(--primary-color);background:#eef6ff}.surface{overflow:hidden;background:#fff;border:1px solid var(--border-light);border-radius:6px}.toolbar{padding:13px 16px;border-bottom:1px solid var(--border-light)}.secondary{display:block;margin-top:3px}.reason{max-width:150px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.pagination{display:flex;justify-content:flex-end;padding:16px}@media(max-width:700px){.status-strip{overflow-x:auto}}
</style>
