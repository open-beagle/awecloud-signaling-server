<template>
  <div class="resource-detail" v-loading="loading">
    <el-breadcrumb separator="/" class="breadcrumb"><el-breadcrumb-item :to="{ path: '/resources' }">资源目录</el-breadcrumb-item><el-breadcrumb-item>{{ detail?.resource.display_name || '资源详情' }}</el-breadcrumb-item></el-breadcrumb>

    <el-card v-if="detail" class="hero-card" shadow="never">
      <div class="hero-top">
        <div class="hero-title"><span class="type-icon" :class="`type-${detail.resource.type}`"><el-icon><component :is="resourceIcon" /></el-icon></span><div><div class="title-line"><h1>{{ detail.resource.display_name }}</h1><el-tag size="small" :type="stateTag">{{ stateLabel }}</el-tag></div><div class="meta-line">{{ typeLabel }} · {{ detail.resource.id }} · {{ detail.resource.provider_id || '管理 API' }}</div></div></div>
        <div class="hero-actions"><el-tag effect="plain">连接入口待 Resource Entry 发布</el-tag></div>
      </div>
      <div class="hero-stats"><div><label>客户</label><strong>{{ detail.tenant.name }}</strong></div><div><label>Owner</label><strong>{{ ownerName }}</strong></div><div><label>Workspace</label><strong>{{ detail.resource.external_workspace_id || '-' }}</strong></div><div><label>Target Revision</label><strong>{{ detail.resource.target_revision }}</strong></div><div><label>更新</label><strong>{{ formatTime(detail.resource.updated_at) }}</strong></div></div>
    </el-card>

    <el-card v-if="detail" class="detail-card" shadow="never">
      <el-tabs v-model="activeTab">
        <el-tab-pane label="概览" name="overview">
          <section class="detail-section"><div class="section-head"><div><h2>业务归属</h2><p>来自受信任 Provider 的稳定业务身份。</p></div></div><el-descriptions :column="3" border><el-descriptions-item label="Tenant ID"><span class="mono">{{ detail.tenant.id }}</span></el-descriptions-item><el-descriptions-item label="Provider">{{ detail.resource.provider_id || '-' }}</el-descriptions-item><el-descriptions-item label="External Workspace ID"><span class="mono">{{ detail.resource.external_workspace_id || '-' }}</span></el-descriptions-item><el-descriptions-item label="Owner">{{ ownerName }}</el-descriptions-item><el-descriptions-item label="资源类型">{{ typeLabel }}</el-descriptions-item><el-descriptions-item label="资源状态"><el-tag size="small" :type="stateTag">{{ stateLabel }}</el-tag></el-descriptions-item></el-descriptions></section>
          <section class="detail-section"><div class="section-head"><div><h2>运行目标</h2><p>Agent 最近证明的 Kubernetes Target 摘要。</p></div><el-tag v-if="detail.target?.ready" type="success" size="small">Agent 已证明</el-tag></div><el-descriptions :column="3" border><el-descriptions-item label="Cluster">{{ detail.target?.cluster_id || detail.resource.cluster_id || '-' }}</el-descriptions-item><el-descriptions-item label="Namespace"><span class="mono">{{ detail.target?.namespace || detail.resource.namespace || '-' }}</span></el-descriptions-item><el-descriptions-item label="Agent Node ID">{{ detail.target?.agent_node_id || detail.resource.agent_node_id || '-' }}</el-descriptions-item><el-descriptions-item label="Pod Name"><span class="mono">{{ detail.target?.pod_name || detail.resource.pod_name || '-' }}</span></el-descriptions-item><el-descriptions-item label="Pod UID"><span class="mono">{{ detail.target?.pod_uid || detail.resource.pod_uid || '-' }}</span></el-descriptions-item><el-descriptions-item label="Container"><span class="mono">{{ detail.target?.container_name || detail.resource.container_name || '-' }}</span></el-descriptions-item></el-descriptions></section>
          <section v-if="detail.resource.type === 'container_ssh'" class="detail-section"><div class="section-head"><div><h2>访问能力</h2><p>第一阶段只允许固定交互式 Shell。</p></div></div><el-descriptions :column="3" border><el-descriptions-item label="Shell Profile">{{ detail.resource.shell_profile_id || '默认 Shell Profile' }}</el-descriptions-item><el-descriptions-item label="最大会话">8 小时</el-descriptions-item><el-descriptions-item label="额外能力">无</el-descriptions-item></el-descriptions></section>
        </el-tab-pane>
        <el-tab-pane :label="`访问策略 ${detail.grants.length}`" name="access">
          <el-alert v-if="detail.resource.type !== 'container_ssh'" title="该资源类型的统一授权契约尚未启用，请使用高级诊断中的兼容授权入口。" type="warning" show-icon :closable="false" class="diagnostic-alert" />
          <div class="tab-head"><div><h2>访问策略</h2><p>Subject、Resource 和 Grant 必须位于同一客户边界。</p></div><el-button type="primary" :icon="Plus" :disabled="!canManage" @click="openGrantDialog">添加授权</el-button></div>
          <el-table :data="detail.grants" stripe><el-table-column label="授权对象" min-width="180"><template #default="{ row }"><strong>{{ subjectName(row) }}</strong><span class="cell-secondary">{{ row.subject_type === 'user' ? '直接授权' : '用户组授权' }}</span></template></el-table-column><el-table-column label="Action" width="130"><template #default="{ row }"><el-tag size="small" type="success">{{ actionsLabel(row.actions) }}</el-tag></template></el-table-column><el-table-column label="有效期" width="190"><template #default="{ row }">{{ formatTime(row.valid_from) }}<span class="cell-secondary">至 {{ formatTime(row.expires_at) }}</span></template></el-table-column><el-table-column label="状态" width="110"><template #default="{ row }"><el-tag size="small" :type="row.status === 'enabled' ? 'success' : 'info'">{{ row.status === 'enabled' ? '生效中' : '已撤销' }}</el-tag></template></el-table-column><el-table-column label="" width="80" align="right"><template #default="{ row }"><el-button v-if="row.status === 'enabled'" type="danger" link :disabled="!canManage" @click="revokeGrant(row)">撤销</el-button></template></el-table-column></el-table><el-empty v-if="!detail.grants.length" description="暂无访问策略" />
        </el-tab-pane>
        <el-tab-pane label="会话" name="sessions"><div class="tab-head"><div><h2>资源会话</h2><p>查看当前和最近连接，并在明确客户上下文中强制断开。</p></div><el-button @click="router.push(`/sessions?resource_id=${detail.resource.id}`)">查看会话</el-button></div></el-tab-pane>
        <el-tab-pane :label="`事件 ${events.length}`" name="events"><el-table v-loading="loadingEvents" :data="events" stripe><el-table-column label="事件" min-width="230"><template #default="{ row }"><strong>{{ eventLabel(row.action_type) }}</strong><span class="cell-secondary">{{ row.target_type }} · {{ row.target_name || row.target_id }}</span></template></el-table-column><el-table-column label="时间" width="190"><template #default="{ row }">{{ formatTime(row.created_at) }}</template></el-table-column><el-table-column label="审计详情" min-width="320"><template #default="{ row }"><code class="event-detail">{{ eventDetail(row.detail) }}</code></template></el-table-column></el-table><el-empty v-if="!loadingEvents && !events.length" description="暂无资源事件" /></el-tab-pane>
        <el-tab-pane label="诊断" name="diagnostics"><el-alert title="诊断字段只读，不能通过页面绕过 Provider 或 Agent 绑定流程。" type="info" show-icon :closable="false" class="diagnostic-alert" /><el-descriptions v-if="detail" :column="3" border><el-descriptions-item label="Resource ID"><span class="mono">{{ detail.resource.id }}</span></el-descriptions-item><el-descriptions-item label="Target Revision">{{ detail.resource.target_revision }}</el-descriptions-item><el-descriptions-item label="Pod IP">不向 Desktop 返回</el-descriptions-item><el-descriptions-item label="Agent 内部端口">由 Resource Entry 管理</el-descriptions-item><el-descriptions-item label="Runtime Target"><span class="mono">{{ detail.target?.pod_uid || '-' }}</span></el-descriptions-item></el-descriptions></el-tab-pane>
      </el-tabs>
    </el-card>

    <el-dialog v-model="showGrant" title="添加资源授权" width="520px">
      <el-alert :title="`${detail?.resource.display_name || ''} · ${detail?.tenant.name || ''}`" type="info" :closable="false" show-icon class="dialog-alert" />
      <el-form label-position="top">
        <el-form-item label="授权主体" required><el-radio-group v-model="grantForm.subject_type"><el-radio-button label="user">用户</el-radio-button><el-radio-button label="group">用户组</el-radio-button></el-radio-group></el-form-item>
        <el-form-item v-if="grantForm.subject_type === 'user'" label="客户成员" required><el-select v-model="grantForm.subject_user_id" filterable :loading="loadingSubjects" style="width: 100%" placeholder="选择客户成员"><el-option v-for="member in members" :key="member.user_id" :label="member.alias ? `${member.alias} (${member.name})` : member.name" :value="member.user_id" :disabled="!member.enabled" /></el-select></el-form-item>
        <el-form-item v-else label="客户用户组" required><el-select v-model="grantForm.subject_group_id" filterable :loading="loadingSubjects" style="width: 100%" placeholder="选择同客户用户组"><el-option v-for="group in groups" :key="group.id" :label="group.alias || group.name" :value="group.id" /></el-select></el-form-item>
        <el-form-item label="Action"><el-select v-model="grantForm.actions" multiple style="width: 100%"><el-option label="Shell" value="shell" /></el-select></el-form-item>
        <el-form-item label="有效期至"><el-date-picker v-model="grantForm.expires_at" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" style="width: 100%" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="showGrant = false">取消</el-button><el-button type="primary" :loading="granting" @click="createGrant">创建授权</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Coin, Connection, Monitor, Plus, Ship, TakeawayBox } from '@element-plus/icons-vue'
import { createResourceGrant, getManagedResource, getResourceEvents, getTenantMembers, revokeResourceGrant, type AccessGrant, type ResourceDetail, type ResourceEvent, type ResourceType, type TenantMember } from '@/api/resource'
import { getGroups, type Group } from '@/api/group'
import { useAuthStore } from '@/stores/auth'
import { useTenantStore } from '@/stores/tenant'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const tenantStore = useTenantStore()
const loading = ref(false)
const granting = ref(false)
const showGrant = ref(false)
const activeTab = ref('overview')
const detail = ref<ResourceDetail | null>(null)
const loadingSubjects = ref(false)
const loadingEvents = ref(false)
const events = ref<ResourceEvent[]>([])
const members = ref<TenantMember[]>([])
const groups = ref<Group[]>([])
const grantForm = reactive<{ subject_type: 'user' | 'group'; subject_user_id?: number; subject_group_id?: number; actions: string[]; expires_at?: string }>({ subject_type: 'user', subject_user_id: undefined, subject_group_id: undefined, actions: ['shell'], expires_at: undefined })
const canManage = computed(() => authStore.canWrite && detail.value?.resource.type === 'container_ssh' && tenantStore.tenantId === detail.value.resource.tenant_id)

const fetchDetail = async () => {
  loading.value = true
  try {
    const res = await getManagedResource(String(route.params.id))
    if (res.success && res.data) detail.value = res.data
  } finally {
    loading.value = false
  }
}

const fetchEvents = async () => {
  loadingEvents.value = true
  try { const res = await getResourceEvents(String(route.params.id), { page: 1, size: 100 }); events.value = res.success && res.data ? res.data : [] } finally { loadingEvents.value = false }
}

const createGrant = async () => {
  if (!detail.value || !canManage.value) return ElMessage.warning('请先进入该资源的客户上下文')
  if (grantForm.subject_type === 'user' && !grantForm.subject_user_id) return ElMessage.warning('请选择客户成员')
  if (grantForm.subject_type === 'group' && !grantForm.subject_group_id) return ElMessage.warning('请选择客户用户组')
  granting.value = true
  try {
    const res = await createResourceGrant(detail.value.resource.id, {
      subject_type: grantForm.subject_type,
      subject_user_id: grantForm.subject_type === 'user' ? grantForm.subject_user_id : undefined,
      subject_group_id: grantForm.subject_type === 'group' ? grantForm.subject_group_id : undefined,
      actions: grantForm.actions,
      expires_at: grantForm.expires_at
    })
    if (res.success) { ElMessage.success('访问策略已创建'); showGrant.value = false; grantForm.subject_user_id = undefined; grantForm.subject_group_id = undefined; await fetchDetail() }
  } finally { granting.value = false }
}

const openGrantDialog = async () => {
  if (!detail.value || !canManage.value) return
  showGrant.value = true
  loadingSubjects.value = true
  try {
    const [memberRes, groupRes] = await Promise.all([
      getTenantMembers(detail.value.resource.tenant_id),
      getGroups({ tenant_id: detail.value.resource.tenant_id, page: 1, size: 100 })
    ])
    members.value = memberRes.success && memberRes.data ? memberRes.data : []
    groups.value = groupRes.success && groupRes.data ? groupRes.data.filter(group => group.tenant_id === detail.value?.resource.tenant_id) : []
  } finally {
    loadingSubjects.value = false
  }
}

const revokeGrant = async (grant: AccessGrant) => {
  if (!canManage.value) return
  try {
    await ElMessageBox.confirm(`确认撤销 ${subjectName(grant)} 的访问策略？`, '撤销授权', { type: 'warning' })
    const res = await revokeResourceGrant(grant.id)
    if (res.success) {
      ElMessage.success('访问策略已撤销')
      await fetchDetail()
    }
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') throw error
  }
}

const typeLabel = computed(() => ({ container_ssh: 'ContainerSSH', host_ssh: 'HostSSH', kubernetes_api: 'KubernetesAPI', database_service: 'DatabaseService', tcp_service: 'TCPService' }[detail.value?.resource.type || ''] || detail.value?.resource.type || '-'))
const stateLabel = computed(() => ({ pending: '待发布', available: '可用', degraded: '异常', draining: '排空中', stopped: '已停止', revoked: '已撤销' }[detail.value?.resource.state || ''] || detail.value?.resource.state || '-'))
const stateTag = computed(() => ({ available: 'success', degraded: 'danger', draining: 'warning', stopped: 'info', revoked: 'info', pending: 'warning' }[detail.value?.resource.state || ''] || 'info') as any)
const resourceIcon = computed(() => ({ container_ssh: TakeawayBox, host_ssh: Monitor, kubernetes_api: Ship, database_service: Coin, tcp_service: Connection }[detail.value?.resource.type || ''] || TakeawayBox))
const ownerName = computed(() => detail.value?.resource.owner_user_id ? `User ${detail.value.resource.owner_user_id}` : '未指定 Owner')
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'
const subjectName = (grant: AccessGrant) => grant.subject_type === 'user' ? `User ${grant.subject_user_id}` : `Group ${grant.subject_group_id}`
const actionsLabel = (value: string) => { try { return (JSON.parse(value) as string[]).join(' / ') } catch { return value || '-' } }
const eventLabel = (value: string) => ({ create_resource: '创建资源', create_workspace_binding: '绑定 Workspace', update_workspace_binding: '更新 Workspace', publish_resource_candidate: '发布发现候选', observe_resource_target: '更新运行目标', create_access_grant: '创建访问策略', revoke_access_grant: '撤销访问策略', revoke_container_session: '撤销会话', force_disconnect_container_session: '强制断开会话' }[value] || value)
const eventDetail = (value?: string) => { if (!value) return '-'; try { const parsed = JSON.parse(value); return parsed.reason || parsed.close_reason || parsed.grant_id || '已记录结构化审计详情' } catch { return value } }
onMounted(() => { fetchDetail(); fetchEvents() })
</script>

<style scoped>
.resource-detail { width: 100%; }
.breadcrumb { margin-bottom: 14px; }
.hero-card, .detail-card { margin-bottom: 16px; border-radius: 6px; }
.hero-top, .hero-title, .title-line, .hero-actions, .tab-head, .section-head { display: flex; align-items: center; }
.hero-top, .tab-head, .section-head { justify-content: space-between; gap: 16px; }
.hero-title { min-width: 0; gap: 12px; }
.type-icon { display: inline-flex; width: 42px; height: 42px; align-items: center; justify-content: center; flex: 0 0 42px; border-radius: 6px; }
.type-container_ssh { color: #176b55; background: #e5f3ed; }
.type-host_ssh { color: #2f6fba; background: #eaf2fb; }
.type-kubernetes_api { color: #9b600e; background: #fff3dc; }
.type-database_service { color: #725096; background: #f1ebf8; }
.type-tcp_service { color: #7c5550; background: #f4eeee; }
.title-line { gap: 9px; }
h1 { margin: 0; font-size: 22px; }
.meta-line, .section-head p { margin-top: 4px; color: var(--text-secondary); font-size: 12px; }
.hero-stats { display: grid; grid-template-columns: repeat(5, 1fr); margin-top: 18px; padding-top: 15px; border-top: 1px solid var(--border-light); }
.hero-stats > div { padding: 0 14px; border-right: 1px solid var(--border-light); }
.hero-stats > div:first-child { padding-left: 0; }
.hero-stats > div:last-child { border-right: 0; }
.hero-stats label { display: block; color: var(--text-secondary); font-size: 11px; margin-bottom: 4px; }
.hero-stats strong { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.detail-section { margin-bottom: 26px; }
.detail-section:last-child { margin-bottom: 0; }
.section-head { margin-bottom: 12px; }
.section-head h2, .tab-head h2 { margin: 0; font-size: 16px; }
.section-head p, .tab-head p { margin-bottom: 0; }
.cell-secondary { display: block; color: var(--text-secondary); font-size: 11px; margin-top: 2px; }
.mono { font-family: Consolas, monospace; font-size: 12px; }
.diagnostic-alert, .dialog-alert { margin-bottom: 16px; }
.event-detail { display: block; max-width: 100%; overflow: hidden; color: var(--text-regular); font-family: Consolas, monospace; font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
@media (max-width: 760px) { .hero-top, .tab-head, .section-head { align-items: flex-start; flex-direction: column; } .hero-actions { flex-wrap: wrap; } .hero-stats { grid-template-columns: repeat(2, 1fr); gap: 14px 0; } .hero-stats > div, .hero-stats > div:first-child { padding: 0 10px; border-right: 1px solid var(--border-light); } .hero-stats > div:nth-child(even) { border-right: 0; } }
</style>
