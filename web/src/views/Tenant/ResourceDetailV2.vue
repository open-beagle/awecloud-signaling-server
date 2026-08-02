<template>
  <div class="detail-page" v-loading="loading">
    <PageHeader title="资源详情" description="当前租户权威 TenantResource、可信目标和版本信息。">
      <template #actions><el-button @click="router.push('/resources')">返回资源目录</el-button><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button></template>
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
        <el-descriptions v-if="resource.type === 'container_ssh'" :column="3" border>
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
    </template>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Refresh } from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import { getTenantResourceV2, type TenantResourceV2 } from '@/api/tenantResourcesV2'
import { useTenantStore } from '@/stores/tenant'
const route=useRoute(),router=useRouter(),tenantStore=useTenantStore(),loading=ref(false),errorMessage=ref(''),resource=ref<TenantResourceV2|null>(null)
const load=async()=>{const tenantId=tenantStore.tenantId,resourceId=String(route.params.id||'');if(!tenantId||!resourceId){resource.value=null;errorMessage.value='当前租户或资源标识无效。';return}loading.value=true;errorMessage.value='';try{const response=await getTenantResourceV2(tenantId,resourceId);resource.value=response.success&&response.data?response.data:null}catch{resource.value=null;errorMessage.value='当前租户内资源不存在，或资源模型读取开关尚未启用。'}finally{loading.value=false}}
const typeLabel=(value:string)=>value==='container_ssh'?'ContainerSSH':value==='container_service'?'ContainerService':value,visibilityLabel=(value:string)=>({visible:'可见',hidden:'已隐藏',retired:'已退役',pending:'待发布'}[value]||value)
const availabilityLabel=(value:string)=>({available:'可用',degraded:'异常',unavailable:'不可用',unknown:'未知'}[value]||value),availabilityTag=(value:string)=>({available:'success',degraded:'warning',unavailable:'danger',unknown:'info'}[value]||'info') as any
const formatTime=(value:string)=>new Date(value).toLocaleString('zh-CN',{hour12:false});watch(()=>tenantStore.contextRevision,()=>router.replace('/resources'));onMounted(load)
</script>

<style scoped>
.detail-page{width:100%}.state-alert{margin-bottom:14px}.hero{display:flex;align-items:flex-start;justify-content:space-between;gap:24px;padding:20px;border:1px solid var(--border-light);border-radius:7px;background:#fff}.hero h1{margin:3px 0 0;font-size:24px}.hero p{margin:6px 0 0;color:var(--text-secondary)}.eyebrow{color:var(--text-secondary);font-size:12px}.tags{display:flex;gap:8px}.surface{margin-top:16px;padding:18px;border:1px solid var(--border-light);border-radius:7px;background:#fff}.surface h2{margin:0 0 14px;font-size:16px}.mono{font-family:'SFMono-Regular',Consolas,'Liberation Mono',monospace;font-size:12px}
</style>
