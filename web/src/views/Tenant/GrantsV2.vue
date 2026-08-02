<template>
  <div class="tenant-page">
    <PageHeader title="访问授权" description="查看当前租户基于稳定 Resource ID 的权威授权。该页面不展示或修改其他租户授权。">
      <template #actions><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button></template>
    </PageHeader>
    <el-alert v-if="errorMessage" class="state-alert" title="访问授权加载失败" :description="errorMessage" type="error" show-icon :closable="false" />
    <section class="surface">
      <div class="toolbar">
        <el-select v-model="filters.status" class="filter-select" clearable placeholder="全部状态" @change="applyFilters"><el-option label="生效中" value="enabled" /><el-option label="已暂停" value="suspended" /><el-option label="已撤销" value="revoked" /><el-option label="已过期" value="expired" /></el-select>
        <el-select v-model="filters.subjectType" class="filter-select" clearable placeholder="全部主体" @change="applyFilters"><el-option label="用户" value="user" /><el-option label="成员组" value="group" /></el-select>
        <span class="result-count">{{ pagination.total }} 条授权</span>
      </div>
      <el-table v-loading="loading" :data="items" stripe>
        <el-table-column label="Resource ID" min-width="250"><template #default="{ row }"><span class="mono">{{ row.resource_id }}</span></template></el-table-column>
        <el-table-column label="授权主体" min-width="160"><template #default="{ row }"><strong>{{ subjectLabel(row) }}</strong><span class="secondary">{{ row.subject_type === 'user' ? '直接用户' : '租户成员组' }}</span></template></el-table-column>
        <el-table-column label="Action" min-width="150"><template #default="{ row }"><el-tag v-for="action in row.actions" :key="action" size="small" effect="plain">{{ action }}</el-tag></template></el-table-column>
        <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag size="small" :type="statusTag(row.status)">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
        <el-table-column label="有效区间" min-width="230"><template #default="{ row }">{{ formatTime(row.valid_from) }}<span class="secondary">至 {{ formatTime(row.expires_at) }}</span></template></el-table-column>
        <el-table-column label="会话上限" width="110"><template #default="{ row }">{{ durationLabel(row.max_session_seconds) }}</template></el-table-column>
        <el-table-column label="版本" width="110"><template #default="{ row }">r{{ row.revision }} / v{{ row.row_version }}</template></el-table-column>
      </el-table>
      <el-empty v-if="!loading && !errorMessage && items.length === 0" description="当前租户没有符合条件的访问授权" />
      <div class="pagination"><el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :total="pagination.total" :page-sizes="[20,50,100]" layout="total, sizes, prev, pager, next" @size-change="load" @current-change="load" /></div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import { getTenantGrantsV2, type TenantGrantV2 } from '@/api/tenantResourcesV2'
import { useTenantStore } from '@/stores/tenant'
const tenantStore=useTenantStore(),loading=ref(false),errorMessage=ref(''),items=ref<TenantGrantV2[]>([])
const filters=reactive({status:'',subjectType:''}),pagination=reactive({page:1,size:20,total:0})
const load=async()=>{const tenantId=tenantStore.tenantId;if(!tenantId){items.value=[];pagination.total=0;errorMessage.value='当前没有有效的租户上下文。';return}loading.value=true;errorMessage.value='';try{const response=await getTenantGrantsV2(tenantId,{status:filters.status||undefined,subject_type:filters.subjectType||undefined,page:pagination.page,size:pagination.size});items.value=response.success&&response.data?response.data:[];pagination.total=response.total||0}catch{items.value=[];pagination.total=0;errorMessage.value='请确认当前租户权限、资源模型读取开关和服务状态后重试。'}finally{loading.value=false}}
const applyFilters=()=>{pagination.page=1;load()},subjectLabel=(row:TenantGrantV2)=>row.subject_type==='user'?`User #${row.subject_user_id}`:`Group #${row.subject_group_id}`
const statusLabel=(value:string)=>({enabled:'生效中',suspended:'已暂停',revoked:'已撤销',expired:'已过期'}[value]||value),statusTag=(value:string)=>({enabled:'success',suspended:'warning',revoked:'info',expired:'info'}[value]||'info') as any
const formatTime=(value?:string)=>value?new Date(value).toLocaleString('zh-CN',{hour12:false}):'长期有效',durationLabel=(seconds:number)=>seconds>=3600?`${Math.round(seconds/3600)} 小时`:`${Math.round(seconds/60)} 分钟`
watch(()=>tenantStore.contextRevision,()=>{filters.status='';filters.subjectType='';pagination.page=1;load()});onMounted(load)
</script>

<style scoped>
.tenant-page{width:100%}.state-alert{margin-bottom:14px}.surface{overflow:hidden;border:1px solid var(--border-light);border-radius:6px;background:#fff}.toolbar{display:flex;align-items:center;gap:10px;padding:14px 16px;border-bottom:1px solid var(--border-light)}.filter-select{width:160px}.result-count{margin-left:auto;color:var(--text-secondary);font-size:12px}.secondary{display:block;margin-top:3px;color:var(--text-secondary);font-size:12px}.mono{font-family:'SFMono-Regular',Consolas,'Liberation Mono',monospace}.el-tag+.el-tag{margin-left:4px}.pagination{display:flex;justify-content:flex-end;padding:16px}
</style>
