<template>
  <div class="legacy-page">
    <div class="page-header"><div><div class="eyebrow">兼容迁移</div><h1>存量认领</h1><p>平台管理员为旧 Agent Node 和 Endpoint 选择目标租户并登记明确归属。认领不会创建 Resource、Grant 或改变现有访问。</p></div><el-button :icon="Refresh" :loading="loading" @click="fetchAll">刷新</el-button></div>
    <el-alert v-if="!tenantTotal && !loading" title="当前没有可分配的启用租户。" type="warning" show-icon :closable="false" class="context-alert" />
    <div class="summary-strip"><div><span>存量对象</span><strong>{{ inventory.length }}</strong></div><div><span>已认领</span><strong class="success">{{ activeCount }}</strong></div><div><span>待认领</span><strong class="warning">{{ pendingCount }}</strong></div><div><span>可分配租户</span><strong>{{ tenantTotal }}</strong></div></div>
    <div class="surface">
      <div class="toolbar"><div class="segments"><button v-for="tab in tabs" :key="tab.value" :class="{ active: sourceFilter === tab.value }" @click="sourceFilter = tab.value">{{ tab.label }} <span>{{ tab.count }}</span></button></div><el-select v-model="claimFilter" clearable placeholder="全部归属" style="width:140px"><el-option label="待认领" value="pending" /><el-option label="已认领" value="active" /><el-option label="已撤销" value="revoked" /></el-select></div>
      <el-table v-loading="loading" :data="filteredInventory" stripe>
        <el-table-column label="存量对象" min-width="260"><template #default="{ row }"><strong>{{ row.name }}</strong><span class="secondary">{{ row.sourceType === 'agent_node' ? `Agent Node #${row.sourceId}` : `Endpoint ${row.sourceId}` }}</span></template></el-table-column>
        <el-table-column label="类型" width="130"><template #default="{ row }"><el-tag size="small" effect="plain">{{ row.sourceType === 'agent_node' ? 'Agent' : 'Endpoint' }}</el-tag></template></el-table-column>
        <el-table-column label="运行状态" width="120"><template #default="{ row }"><el-tag size="small" :type="row.runtimeState === 'online' ? 'success' : 'info'">{{ row.runtimeState === 'online' ? '在线' : '离线' }}</el-tag></template></el-table-column>
        <el-table-column label="当前归属" min-width="200"><template #default="{ row }"><template v-if="row.claim"><strong>{{ row.claim.tenant_name || row.claim.tenant_id }}</strong><span class="secondary">{{ row.claim.claim_reason || '未填写原因' }}</span></template><span v-else class="unclaimed">未认领</span></template></el-table-column>
        <el-table-column label="认领状态" width="120"><template #default="{ row }"><el-tag v-if="row.claim" size="small" :type="row.claim.status === 'active' ? 'success' : 'info'">{{ row.claim.status === 'active' ? '已认领' : '已撤销' }}</el-tag><el-tag v-else size="small" type="warning">待认领</el-tag></template></el-table-column>
        <el-table-column label="操作" width="170" fixed="right" align="right"><template #default="{ row }"><el-button link type="primary" @click="openLegacy(row)">查看旧详情</el-button><el-button v-if="!row.claim || row.claim.status === 'revoked'" link type="primary" :disabled="!canClaim" @click="openClaim(row)">认领</el-button><el-button v-else link type="danger" :disabled="!canRevoke(row)" @click="revoke(row)">撤销</el-button></template></el-table-column>
      </el-table>
      <el-empty v-if="!loading && !filteredInventory.length" description="当前筛选没有存量对象" />
    </div>
    <el-dialog v-model="claimDialogVisible" title="认领存量对象" width="520px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item label="存量对象"><strong>{{ claimingItem?.name }}</strong></el-form-item>
        <el-form-item label="目标租户" required>
          <el-select v-model="claimForm.tenantId" filterable remote :remote-method="searchTenants" :loading="tenantLoading" placeholder="搜索并选择目标租户" style="width:100%">
            <el-option v-for="tenant in tenants" :key="tenant.id" :label="tenant.name" :value="tenant.id"><span>{{ tenant.name }}</span><span class="tenant-key">{{ tenant.key }}</span></el-option>
          </el-select>
        </el-form-item>
        <el-form-item label="归属依据" required>
          <el-input v-model="claimForm.reason" type="textarea" :rows="3" maxlength="300" show-word-limit placeholder="客户资产清单编号或负责人确认记录" />
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="claimDialogVisible = false">取消</el-button><el-button type="primary" :loading="claiming" @click="submitClaim">确认认领</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { getNodes, type Node } from '@/api/node'
import { getEndpoints, type EndpointItem } from '@/api/endpoint'
import { claimLegacyResource, getLegacyResourceClaims, getTenants, revokeLegacyResourceClaim, type LegacyResourceClaim, type Tenant } from '@/api/resource'
import { useAuthStore } from '@/stores/auth'

type InventoryItem = { sourceType: 'agent_node' | 'endpoint'; sourceId: string; name: string; runtimeState: string; claim?: LegacyResourceClaim }
const router = useRouter(), authStore = useAuthStore()
const loading = ref(false), nodes = ref<Node[]>([]), endpoints = ref<EndpointItem[]>([]), claims = ref<LegacyResourceClaim[]>([]), tenants = ref<Tenant[]>([])
const sourceFilter = ref(''), claimFilter = ref('')
const claimDialogVisible = ref(false), claiming = ref(false), claimingItem = ref<InventoryItem>()
const tenantLoading = ref(false), tenantTotal = ref(0)
const claimForm = reactive({ tenantId: '', reason: '' })
const inventory = computed<InventoryItem[]>(() => {
  const claimMap = new Map(claims.value.map(claim => [`${claim.source_type}:${claim.source_id}`, claim]))
  return [
    ...nodes.value.map(node => ({ sourceType: 'agent_node' as const, sourceId: String(node.id), name: node.name, runtimeState: node.status || 'offline', claim: claimMap.get(`agent_node:${node.id}`) })),
    ...endpoints.value.map(endpoint => ({ sourceType: 'endpoint' as const, sourceId: endpoint.id, name: endpoint.alias || endpoint.name, runtimeState: endpoint.status, claim: claimMap.get(`endpoint:${endpoint.id}`) }))
  ]
})
const activeCount = computed(() => inventory.value.filter(item => item.claim?.status === 'active').length)
const pendingCount = computed(() => inventory.value.filter(item => !item.claim || item.claim.status === 'revoked').length)
const canClaim = computed(() => authStore.isPlatformAdmin && tenantTotal.value > 0)
const tabs = computed(() => [{ label: '全部', value: '', count: inventory.value.length }, { label: 'Agent', value: 'agent_node', count: nodes.value.length }, { label: 'Endpoint', value: 'endpoint', count: endpoints.value.length }])
const filteredInventory = computed(() => inventory.value.filter(item => (!sourceFilter.value || item.sourceType === sourceFilter.value) && (!claimFilter.value || (claimFilter.value === 'pending' ? !item.claim || item.claim.status === 'revoked' : item.claim?.status === claimFilter.value))))
const fetchAll = async () => { loading.value = true; try { const [nodeRes, endpointRes, claimRes, tenantRes] = await Promise.all([getNodes({ type: 'agent', page: 1, size: 100 }), getEndpoints({ page: 1, size: 100 }), getLegacyResourceClaims({ page: 1, size: 100 }), getTenants({ page: 1, size: 100, status: 'active' })]); nodes.value = nodeRes.success && nodeRes.data ? nodeRes.data : []; endpoints.value = endpointRes.success && endpointRes.data ? endpointRes.data : []; claims.value = claimRes.success && claimRes.data ? claimRes.data : []; tenants.value = tenantRes.success && tenantRes.data ? tenantRes.data : []; tenantTotal.value = tenantRes.total || tenants.value.length } finally { loading.value = false } }
const searchTenants = async (search = '') => { tenantLoading.value = true; try { const response = await getTenants({ search: search.trim() || undefined, status: 'active', page: 1, size: 100 }); tenants.value = response.success && response.data ? response.data : [] } finally { tenantLoading.value = false } }
const openClaim = (item: InventoryItem) => { if (!canClaim.value) return; claimingItem.value = item; claimForm.tenantId = ''; claimForm.reason = ''; claimDialogVisible.value = true; searchTenants() }
const submitClaim = async () => { const item = claimingItem.value; const reason = claimForm.reason.trim(); if (!item || !claimForm.tenantId) { ElMessage.warning('请选择目标租户'); return } if (!reason) { ElMessage.warning('请填写归属依据'); return } claiming.value = true; try { const res = await claimLegacyResource({ source_type: item.sourceType, source_id: item.sourceId, tenant_id: claimForm.tenantId, reason }); if (res.success) { claimDialogVisible.value = false; ElMessage.success('存量归属已登记，旧访问链路保持不变'); await fetchAll() } } finally { claiming.value = false } }
const canRevoke = (item: InventoryItem) => authStore.isPlatformAdmin && !!item.claim
const revoke = async (item: InventoryItem) => { if (!item.claim || !canRevoke(item)) return; try { await ElMessageBox.confirm(`撤销 ${item.name} 对 ${item.claim.tenant_name || item.claim.tenant_id} 的归属登记？旧访问链路不会因此断开。`, '撤销存量归属', { type: 'warning' }); const res = await revokeLegacyResourceClaim(item.claim.id); if (res.success) { ElMessage.success('存量归属已撤销'); await fetchAll() } } catch (error) { if (error !== 'cancel' && error !== 'close') throw error } }
const openLegacy = (item: InventoryItem) => item.sourceType === 'agent_node' ? router.push(`/nodes/${item.sourceId}?type=agent&name=${encodeURIComponent(item.name)}`) : router.push(`/endpoints/${item.sourceId}?name=${encodeURIComponent(item.name)}`)
onMounted(fetchAll)
</script>

<style scoped>
.legacy-page{width:100%}.page-header{display:flex;justify-content:space-between;align-items:flex-start;gap:24px;margin-bottom:18px}.eyebrow,.secondary{color:var(--text-secondary);font-size:12px}.eyebrow{margin-bottom:5px}h1{margin:0;font-size:24px;line-height:32px}.page-header p{margin:5px 0 0;color:var(--text-regular);font-size:13px}.context-alert{margin-bottom:14px}.summary-strip{display:grid;grid-template-columns:repeat(4,1fr);margin-bottom:14px;background:#fff;border:1px solid var(--border-light);border-radius:6px}.summary-strip>div{padding:13px 16px;border-right:1px solid var(--border-light)}.summary-strip>div:last-child{border-right:0}.summary-strip span,.summary-strip strong{display:block}.summary-strip span{color:var(--text-secondary);font-size:12px}.summary-strip strong{margin-top:3px;font-size:20px}.summary-strip strong.success{color:var(--success-color)}.summary-strip strong.warning{color:var(--warning-color)}.surface{overflow:hidden;background:#fff;border:1px solid var(--border-light);border-radius:6px}.toolbar{display:flex;justify-content:space-between;align-items:center;padding:10px 14px;border-bottom:1px solid var(--border-light)}.segments{display:flex;gap:3px}.segments button{height:34px;padding:0 11px;border:0;border-radius:4px;background:transparent;color:var(--text-regular);cursor:pointer}.segments button.active{background:var(--primary-lighter);color:var(--primary-color);font-weight:600}.segments button span{margin-left:4px;color:var(--text-secondary);font-size:11px}.secondary{display:block;margin-top:3px}.unclaimed{color:var(--warning-color)}.tenant-key{float:right;margin-left:24px;color:var(--text-secondary);font-size:12px}@media(max-width:700px){.page-header{flex-direction:column}.summary-strip{grid-template-columns:repeat(2,1fr)}.summary-strip>div:nth-child(2){border-right:0}.summary-strip>div:nth-child(n+3){border-top:1px solid var(--border-light)}.toolbar{align-items:stretch;flex-direction:column;gap:8px}.segments{overflow-x:auto}}
</style>
