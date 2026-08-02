<template>
  <div class="tenant-page">
    <PageHeader title="访问会话" description="查看当前租户 ResourceSession 的授权快照、连接状态和终止原因。">
      <template #actions><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button></template>
    </PageHeader>
    <el-alert v-if="errorMessage" class="state-alert" title="访问会话加载失败" :description="errorMessage" type="error" show-icon :closable="false" />
    <section class="surface">
      <div class="toolbar"><el-select v-model="filters.status" class="filter-select" clearable placeholder="全部状态" @change="applyFilters"><el-option v-for="option in statusOptions" :key="option.value" :label="option.label" :value="option.value" /></el-select><span class="result-count">{{ pagination.total }} 条会话</span></div>
      <el-table v-loading="loading" :data="items" stripe>
        <el-table-column label="会话 / 资源" min-width="260"><template #default="{ row }"><strong class="mono">{{ row.id }}</strong><span class="secondary mono">{{ row.resource_id }}</span></template></el-table-column>
        <el-table-column label="用户 / 设备" min-width="155"><template #default="{ row }"><strong>User #{{ row.user_id }}</strong><span class="secondary">Device #{{ row.device_id }}</span></template></el-table-column>
        <el-table-column label="类型 / Action" width="165"><template #default="{ row }">{{ typeLabel(row.session_type) }}<span class="secondary">{{ row.action }}</span></template></el-table-column>
        <el-table-column label="授权版本" width="140"><template #default="{ row }">auth r{{ row.authorization_revision }}<span class="secondary">Grant r{{ row.grant_revision }}</span></template></el-table-column>
        <el-table-column label="状态" width="120"><template #default="{ row }"><el-tag size="small" :type="statusTag(row.status)">{{ statusLabel(row.status) }}</el-tag><span v-if="row.close_reason" class="secondary reason">{{ row.close_reason }}</span></template></el-table-column>
        <el-table-column label="开始时间" width="180"><template #default="{ row }">{{ formatTime(row.started_at) }}</template></el-table-column>
        <el-table-column label="有效至" width="180"><template #default="{ row }">{{ formatTime(row.valid_until) }}</template></el-table-column>
        <el-table-column prop="request_id" label="Request ID" min-width="210" show-overflow-tooltip />
      </el-table>
      <el-empty v-if="!loading && !errorMessage && items.length === 0" description="当前租户没有符合条件的访问会话" />
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :total="pagination.total" :page-sizes="[20,50,100]" layout="total, sizes, prev, pager, next" @size-change="load" @current-change="load" /></div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import { getTenantSessionsV2, type ResourceSessionV2 } from '@/api/tenantResourcesV2'
import { useTenantStore } from '@/stores/tenant'
const tenantStore=useTenantStore(),loading=ref(false),errorMessage=ref(''),items=ref<ResourceSessionV2[]>([])
const filters=reactive({status:''}),pagination=reactive({page:1,size:20,total:0}),statusOptions=[{label:'授权中',value:'authorizing'},{label:'活动中',value:'active'},{label:'结束中',value:'ending'},{label:'已结束',value:'ended'},{label:'已终止',value:'terminated'},{label:'已拒绝',value:'rejected'}]
const load=async()=>{const tenantId=tenantStore.tenantId;if(!tenantId){items.value=[];pagination.total=0;errorMessage.value='当前没有有效的租户上下文。';return}loading.value=true;errorMessage.value='';try{const response=await getTenantSessionsV2(tenantId,{status:filters.status||undefined,page:pagination.page,size:pagination.size});items.value=response.success&&response.data?response.data:[];pagination.total=response.total||0}catch{items.value=[];pagination.total=0;errorMessage.value='请确认当前租户权限、会话授权开关和服务状态后重试。'}finally{loading.value=false}}
const applyFilters=()=>{pagination.page=1;load()},typeLabel=(value:string)=>value==='container_ssh'?'ContainerSSH':value==='container_service'?'ContainerService':value
const statusLabel=(value:string)=>({authorizing:'授权中',active:'活动中',ending:'结束中',ended:'已结束',terminated:'已终止',rejected:'已拒绝'}[value]||value),statusTag=(value:string)=>({authorizing:'warning',active:'success',ending:'warning',ended:'info',terminated:'danger',rejected:'danger'}[value]||'info') as any
const formatTime=(value:string)=>new Date(value).toLocaleString('zh-CN',{hour12:false});watch(()=>tenantStore.contextRevision,()=>{filters.status='';pagination.page=1;load()});onMounted(load)
</script>

<style scoped>
.tenant-page{width:100%}.state-alert{margin-bottom:14px}.surface{overflow:hidden;border:1px solid var(--border-light);border-radius:6px;background:#fff}.toolbar{display:flex;align-items:center;gap:10px;padding:14px 16px;border-bottom:1px solid var(--border-light)}.filter-select{width:170px}.result-count{margin-left:auto;color:var(--text-secondary);font-size:12px}.secondary{display:block;margin-top:3px;color:var(--text-secondary);font-size:12px}.mono{font-family:'SFMono-Regular',Consolas,'Liberation Mono',monospace}.reason{max-width:150px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.pagination{display:flex;justify-content:flex-end;padding:16px}
</style>
