<template>
  <div class="policy-page">
    <PageHeader title="访问授权" eyebrow="资源授权" description="跨资源查询直接授权和用户组授权。新增授权应从资源详情发起。">
      <template #actions><el-button :icon="Refresh" :loading="loading" @click="fetchPolicies">刷新</el-button></template>
    </PageHeader>
    <div class="surface">
      <div class="toolbar">
        <el-select v-model="filters.status" clearable placeholder="全部状态" @change="handleFilter"><el-option label="生效中" value="enabled" /><el-option label="已撤销" value="revoked" /></el-select>
        <el-select v-model="filters.subject_type" clearable placeholder="全部主体" @change="handleFilter"><el-option label="人员" value="user" /><el-option label="用户组" value="group" /></el-select>
        <span class="spacer" /><span class="count">{{ pagination.total }} 条策略</span>
      </div>
      <el-table v-loading="loading" :data="policies" stripe>
        <el-table-column label="资源" min-width="230"><template #default="{ row }"><el-link type="primary" :underline="false" @click="router.push(`/resources/${row.resource_id}`)">{{ row.resource_name || row.resource_id }}</el-link><span class="secondary">{{ typeLabel(row.resource_type) }}</span></template></el-table-column>
        <el-table-column label="客户" min-width="160"><template #default="{ row }"><strong>{{ row.tenant_name || row.tenant_id }}</strong></template></el-table-column>
        <el-table-column label="授权主体" min-width="180"><template #default="{ row }"><strong>{{ row.subject_name || subjectFallback(row) }}</strong><span class="secondary">{{ row.subject_type === 'user' ? '直接授权' : '用户组授权' }}</span></template></el-table-column>
        <el-table-column label="Action" width="150"><template #default="{ row }"><el-tag v-for="action in parseActions(row.actions)" :key="action" size="small" effect="plain">{{ action }}</el-tag></template></el-table-column>
        <el-table-column label="有效期" width="190"><template #default="{ row }">{{ formatTime(row.valid_from) }}<span class="secondary">至 {{ formatTime(row.expires_at) }}</span></template></el-table-column>
        <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag size="small" :type="row.status === 'enabled' ? 'success' : 'info'">{{ row.status === 'enabled' ? '生效中' : '已撤销' }}</el-tag></template></el-table-column>
        <el-table-column label="操作" width="100" fixed="right" align="right"><template #default="{ row }"><el-button v-if="row.status === 'enabled'" link type="danger" :disabled="!canMutate(row)" @click="handleRevoke(row)">撤销</el-button></template></el-table-column>
      </el-table>
      <el-empty v-if="!loading && !policies.length" description="当前范围没有访问策略" />
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" :total="pagination.total" @size-change="fetchPolicies" @current-change="fetchPolicies" /></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import { getAccessGrants, revokeResourceGrant, type AccessGrant, type ResourceType } from '@/api/resource'
import { useAuthStore } from '@/stores/auth'
import { useTenantStore } from '@/stores/tenant'

const router = useRouter()
const authStore = useAuthStore()
const tenantStore = useTenantStore()
const loading = ref(false)
const policies = ref<AccessGrant[]>([])
const filters = reactive({ status: '', subject_type: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })
const selectedTenant = computed(() => tenantStore.tenantId)
const fetchPolicies = async () => { loading.value = true; try { const res = await getAccessGrants({ tenant_id: selectedTenant.value || undefined, status: filters.status || undefined, subject_type: filters.subject_type || undefined, page: pagination.page, size: pagination.size }); if (res.success && res.data) { policies.value = res.data; pagination.total = res.total } } finally { loading.value = false } }
const handleFilter = () => { pagination.page = 1; fetchPolicies() }
const canMutate = (row: AccessGrant) => authStore.canWrite && selectedTenant.value === row.tenant_id
const handleRevoke = async (row: AccessGrant) => { if (!canMutate(row)) return; try { await ElMessageBox.confirm(`确认撤销 ${row.subject_name || subjectFallback(row)} 对 ${row.resource_name || row.resource_id} 的访问？`, '撤销访问策略', { type: 'warning' }); const res = await revokeResourceGrant(row.id); if (res.success) { ElMessage.success('访问策略已撤销'); await fetchPolicies() } } catch (error) { if (error !== 'cancel' && error !== 'close') throw error } }
const parseActions = (value: string) => { try { return JSON.parse(value) as string[] } catch { return value ? [value] : [] } }
const subjectFallback = (row: AccessGrant) => row.subject_type === 'user' ? `User ${row.subject_user_id}` : `Group ${row.subject_group_id}`
const typeLabel = (type?: ResourceType) => ({ container_ssh: 'ContainerSSH', host_ssh: '主机', kubernetes_api: 'KubernetesAPI', database_service: 'DatabaseService', tcp_service: 'TCPService' }[type || ''] || type || '-')
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-'
watch(() => tenantStore.tenantId, () => { pagination.page = 1; fetchPolicies() })
onMounted(fetchPolicies)
</script>

<style scoped>
    .policy-page { width: 100%; }.toolbar { display:flex;align-items:center; }.secondary,.count { color:var(--text-secondary);font-size:12px; }.surface { overflow:hidden;background:#fff;border:1px solid var(--border-light);border-radius:6px; }.toolbar { gap:10px;padding:14px 16px;border-bottom:1px solid var(--border-light); }.toolbar .el-select { width:150px; }.spacer { flex:1; }.secondary { display:block;margin-top:3px; }.el-tag + .el-tag { margin-left:4px; }.pagination { display:flex;justify-content:flex-end;padding:16px; }@media(max-width:700px){.toolbar{flex-wrap:wrap}.spacer{display:none}}
</style>
