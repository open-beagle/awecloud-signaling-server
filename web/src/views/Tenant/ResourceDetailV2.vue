<template>
  <div class="detail-page" v-loading="loading">
    <PageHeader title="资源详情" description="当前租户权威 TenantResource、可信目标和版本信息。">
      <template #actions><el-button @click="router.push('/resources')">返回资源目录</el-button><el-button v-if="canGrant" type="primary" :icon="Plus" @click="openGrantDialog">添加授权</el-button><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button></template>
    </PageHeader>
    <el-alert v-if="errorMessage" class="state-alert" title="资源详情加载失败" :description="errorMessage" type="error" show-icon :closable="false" />
    <template v-if="resource">
      <section class="hero">
        <div><span class="eyebrow">{{ typeLabel(resource.type) }}</span><h1>{{ resource.display_name }}</h1><p class="mono">{{ resource.resource_id }}</p></div>
        <div class="tags"><el-tag :type="availabilityTag(resource.availability_state)">{{ availabilityLabel(resource.availability_state) }}</el-tag><el-tag effect="plain">{{ visibilityLabel(resource.visibility_state) }}</el-tag></div>
      </section>
      <section class="surface">
        <h2>资源身份</h2>
        <el-descriptions :column="3" border>
          <el-descriptions-item label="Resource ID"><span class="mono">{{ resource.resource_id }}</span></el-descriptions-item>
          <el-descriptions-item label="资源版本">r{{ resource.revision }}</el-descriptions-item>
          <el-descriptions-item label="行版本">v{{ resource.row_version }}</el-descriptions-item>
          <el-descriptions-item label="Namespace"><span>{{ resource.namespace_name || '-' }}</span></el-descriptions-item>
          <el-descriptions-item label="Namespace Scope"><span class="mono">{{ resource.namespace_scope_id || '-' }}</span></el-descriptions-item>
          <el-descriptions-item label="Identity Quality">{{ resource.identity_quality || '-' }}</el-descriptions-item>
        </el-descriptions>
      </section>
      <section class="surface">
        <h2>可信目标</h2>
        <el-descriptions v-if="resource.type === 'host_ssh'" :column="3" border>
          <el-descriptions-item label="SSH 主机域名标识"><span class="mono">{{ resource.ssh_domain || '-' }}</span></el-descriptions-item>
          <el-descriptions-item label="Agent Node ID">{{ resource.agent_node_id || '-' }}</el-descriptions-item>
          <el-descriptions-item label="SSH 用户">{{ resource.ssh_users?.join(', ') || '-' }}</el-descriptions-item>
          <el-descriptions-item label="Target IP">{{ resource.target_ip || '-' }}</el-descriptions-item>
          <el-descriptions-item label="Target Port">{{ resource.target_port || 22 }}</el-descriptions-item>
          <el-descriptions-item label="Target Revision">{{ resource.target_revision || 0 }}</el-descriptions-item>
        </el-descriptions>
        <el-descriptions v-else-if="resource.type === 'container_ssh'" :column="3" border>
          <el-descriptions-item label="Workload">{{ resource.workload_kind || '-' }} / {{ resource.workload_name || '-' }}</el-descriptions-item>
          <el-descriptions-item label="Workload UID"><span class="mono">{{ resource.workload_uid || '-' }}</span></el-descriptions-item>
          <el-descriptions-item label="Target Revision">{{ resource.target_revision || 0 }}</el-descriptions-item>
          <el-descriptions-item label="Pod">{{ resource.pod_name || '-' }}</el-descriptions-item>
          <el-descriptions-item label="Pod UID"><span class="mono">{{ resource.pod_uid || '-' }}</span></el-descriptions-item>
          <el-descriptions-item label="Container">{{ resource.container_name || '-' }}</el-descriptions-item>
        </el-descriptions>
        <el-descriptions v-else :column="3" border>
          <el-descriptions-item label="Service">{{ resource.service_name || '-' }}</el-descriptions-item>
          <el-descriptions-item label="Service UID"><span class="mono">{{ resource.service_uid || '-' }}</span></el-descriptions-item>
          <el-descriptions-item label="Target Revision">{{ resource.target_revision || 0 }}</el-descriptions-item>
          <el-descriptions-item label="Port Name">{{ resource.port_name || '-' }}</el-descriptions-item>
          <el-descriptions-item label="Port">{{ resource.port_number || '-' }}</el-descriptions-item>
          <el-descriptions-item label="Protocol">{{ resource.protocol || '-' }}</el-descriptions-item>
        </el-descriptions>
      </section>
      <section class="surface">
        <h2>观测状态</h2>
        <el-descriptions :column="3" border>
          <el-descriptions-item label="Ready">{{ resource.ready ? '是' : '否' }}</el-descriptions-item>
          <el-descriptions-item label="Observation Revision">{{ resource.observation_revision || 0 }}</el-descriptions-item>
          <el-descriptions-item label="更新时间">{{ formatTime(resource.updated_at) }}</el-descriptions-item>
        </el-descriptions>
      </section>
      <section class="surface">
        <div class="section-head"><h2>访问授权</h2><el-button v-if="canGrant" type="primary" :icon="Plus" @click="openGrantDialog">添加授权</el-button></div>
        <el-table v-loading="grantLoading" :data="grants" stripe>
          <el-table-column label="授权主体" min-width="160"><template #default="{ row }">{{ subjectLabel(row) }}</template></el-table-column>
          <el-table-column label="Action" min-width="130"><template #default="{ row }"><el-tag v-for="action in row.actions" :key="action" size="small" effect="plain">{{ action }}</el-tag></template></el-table-column>
          <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag size="small" :type="row.status === 'enabled' ? 'success' : 'info'">{{ grantStatusLabel(row.status) }}</el-tag></template></el-table-column>
          <el-table-column label="有效期" min-width="220"><template #default="{ row }">{{ formatTime(row.valid_from) }}<span class="secondary">至 {{ formatTime(row.expires_at) }}</span></template></el-table-column>
        </el-table>
        <el-empty v-if="!grantLoading && grants.length === 0" description="当前资源还没有访问授权" />
      </section>
    </template>
    <el-dialog v-model="grantDialogVisible" title="添加资源授权" width="520px">
      <el-form label-position="top">
        <el-form-item label="授权主体"><el-radio-group v-model="grantForm.subject_type"><el-radio-button label="user">用户</el-radio-button><el-radio-button label="group">用户组</el-radio-button></el-radio-group></el-form-item>
        <el-form-item v-if="grantForm.subject_type === 'user'" label="客户成员" required><el-select v-model="grantForm.subject_user_id" filterable style="width:100%" placeholder="选择客户成员"><el-option v-for="member in members" :key="member.user_id" :label="member.alias ? `${member.alias} (${member.name})` : member.name" :value="member.user_id" :disabled="!member.enabled" /></el-select></el-form-item>
        <el-form-item v-else label="客户用户组" required><el-select v-model="grantForm.subject_group_id" filterable style="width:100%" placeholder="选择客户用户组"><el-option v-for="group in groups" :key="group.id" :label="group.alias || group.name" :value="group.id" /></el-select></el-form-item>
        <el-form-item label="Action"><el-select v-model="grantForm.actions" multiple style="width:100%"><el-option label="Shell" value="shell" /></el-select></el-form-item>
        <el-form-item label="有效期至"><el-date-picker v-model="grantForm.expires_at" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" style="width:100%" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="grantDialogVisible=false">取消</el-button><el-button type="primary" :loading="creatingGrant" @click="createGrant">创建授权</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import { createResourceGrant, getTenantMembers, type TenantMember } from '@/api/resource'
import { getGroups, type Group } from '@/api/group'
import { getTenantGrantsV2, getTenantResourceV2, type TenantGrantV2, type TenantResourceV2 } from '@/api/tenantResourcesV2'
import { useTenantStore } from '@/stores/tenant'
const route=useRoute(),router=useRouter(),tenantStore=useTenantStore(),loading=ref(false),grantLoading=ref(false),creatingGrant=ref(false),errorMessage=ref(''),resource=ref<TenantResourceV2|null>(null),grants=ref<TenantGrantV2[]>([]),members=ref<TenantMember[]>([]),groups=ref<Group[]>([]),grantDialogVisible=ref(false),grantAutoOpened=ref(false)
const grantForm=reactive<{subject_type:'user'|'group';subject_user_id?:number;subject_group_id?:number;actions:string[];expires_at?:string}>({subject_type:'user',actions:['shell']})
const canGrant=computed(()=>!!resource.value&&tenantStore.canTenant('tenant.grants.write')&&resource.value.type==='host_ssh')
const load=async()=>{const tenantId=tenantStore.tenantId,resourceId=String(route.params.id||'');if(!tenantId||!resourceId){resource.value=null;grants.value=[];errorMessage.value='当前租户或资源标识无效。';return}loading.value=true;errorMessage.value='';try{const response=await getTenantResourceV2(tenantId,resourceId);resource.value=response.success&&response.data?response.data:null;await loadGrants();if(route.query.grant==='1'&&canGrant.value&&!grantAutoOpened.value){grantAutoOpened.value=true;await openGrantDialog()}}catch{resource.value=null;grants.value=[];errorMessage.value='当前租户内资源不存在，或资源模型读取开关尚未启用。'}finally{loading.value=false}}
const loadGrants=async()=>{const tenantId=tenantStore.tenantId,resourceId=String(route.params.id||'');if(!tenantId||!resourceId)return;grantLoading.value=true;try{const response=await getTenantGrantsV2(tenantId,{resource_id:resourceId,page:1,size:100});grants.value=response.success&&response.data?response.data:[]}finally{grantLoading.value=false}}
const openGrantDialog=async()=>{if(!resource.value||!canGrant.value)return;grantDialogVisible.value=true;const [memberRes,groupRes]=await Promise.all([getTenantMembers(tenantStore.tenantId),getGroups({tenant_id:tenantStore.tenantId,page:1,size:100})]);members.value=memberRes.success&&memberRes.data?memberRes.data:[];groups.value=groupRes.success&&groupRes.data?groupRes.data.filter(group=>group.tenant_id===tenantStore.tenantId):[]}
const createGrant=async()=>{if(!resource.value)return;if(grantForm.subject_type==='user'&&!grantForm.subject_user_id){ElMessage.warning('请选择客户成员');return}if(grantForm.subject_type==='group'&&!grantForm.subject_group_id){ElMessage.warning('请选择客户用户组');return}creatingGrant.value=true;try{const response=await createResourceGrant(resource.value.resource_id,{subject_type:grantForm.subject_type,subject_user_id:grantForm.subject_type==='user'?grantForm.subject_user_id:undefined,subject_group_id:grantForm.subject_type==='group'?grantForm.subject_group_id:undefined,actions:grantForm.actions,expires_at:grantForm.expires_at},tenantStore.tenantId);if(response.success){ElMessage.success('访问授权已创建');grantDialogVisible.value=false;grantForm.subject_user_id=undefined;grantForm.subject_group_id=undefined;await loadGrants()}}finally{creatingGrant.value=false}}
const typeLabel=(value:string)=>value==='host_ssh'?'SSH 主机':value==='container_ssh'?'ContainerSSH':value==='container_service'?'ContainerService':value,visibilityLabel=(value:string)=>({visible:'可见',hidden:'已隐藏',retired:'已退役',pending:'待发布'}[value]||value)
const availabilityLabel=(value:string)=>({available:'可用',degraded:'异常',unavailable:'不可用',unknown:'未知'}[value]||value),availabilityTag=(value:string)=>({available:'success',degraded:'warning',unavailable:'danger',unknown:'info'}[value]||'info') as any
const grantStatusLabel=(value:string)=>({enabled:'生效中',suspended:'已暂停',revoked:'已撤销',expired:'已过期'}[value]||value),subjectLabel=(row:TenantGrantV2)=>row.subject_type==='user'?`User #${row.subject_user_id}`:`Group #${row.subject_group_id}`
const formatTime=(value?:string)=>value?new Date(value).toLocaleString('zh-CN',{hour12:false}):'长期有效';watch(()=>tenantStore.contextRevision,()=>router.replace('/resources'));onMounted(load)
</script>

<style scoped>
.detail-page{width:100%}.state-alert{margin-bottom:14px}.hero{display:flex;align-items:flex-start;justify-content:space-between;gap:24px;padding:20px;border:1px solid var(--border-light);border-radius:7px;background:#fff}.hero h1{margin:3px 0 0;font-size:24px}.hero p{margin:6px 0 0;color:var(--text-secondary)}.eyebrow{color:var(--text-secondary);font-size:12px}.tags,.section-head{display:flex;align-items:center;justify-content:space-between;gap:8px}.surface{margin-top:16px;padding:18px;border:1px solid var(--border-light);border-radius:7px;background:#fff}.surface h2{margin:0 0 14px;font-size:16px}.section-head h2{margin:0}.secondary{display:block;margin-top:3px;color:var(--text-secondary);font-size:12px}.mono{font-family:'SFMono-Regular',Consolas,'Liberation Mono',monospace;font-size:12px}.el-tag+.el-tag{margin-left:4px}
</style>
