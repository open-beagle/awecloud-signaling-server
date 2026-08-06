<template>
  <div class="page-container">
    <div class="page-header">
      <div>
        <h1>成员</h1>
        <p>这里展示当前租户与实名用户之间的成员关系，不是平台用户目录。</p>
      </div>
      <el-button :loading="loading" @click="fetchMembers">刷新</el-button>
    </div>
    <el-table v-loading="loading" :data="members" empty-text="当前租户还没有成员">
      <el-table-column label="实名用户" min-width="220"><template #default="scope"><strong>{{ scope.row.alias || scope.row.name }}</strong><div v-if="scope.row.alias" class="secondary">{{ scope.row.name }}</div></template></el-table-column>
      <el-table-column prop="role" label="成员角色" width="150" />
      <el-table-column label="状态" width="120"><template #default="scope"><el-tag size="small" effect="plain" :type="scope.row.enabled ? 'success' : 'info'">{{ scope.row.enabled ? '有效' : '已停用' }}</el-tag></template></el-table-column>
      <el-table-column prop="expires_at" label="有效期" min-width="190"><template #default="scope">{{ scope.row.expires_at || '长期有效' }}</template></el-table-column>
      <el-table-column v-if="tenantStore.canTenant('tenant.members.write')" label="操作" width="140" fixed="right">
        <template #default="scope">
          <el-button v-if="scope.row.enabled" type="warning" link :loading="disabling === scope.row.user_id" :disabled="deleting === scope.row.user_id" @click="disableMember(scope.row)">停用</el-button>
          <el-button type="danger" link :loading="deleting === scope.row.user_id" :disabled="disabling === scope.row.user_id" @click="deleteMember(scope.row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { deleteTenantMember, disableTenantMember, getTenantMembers, type TenantMember } from '@/api/resource'
import { useTenantStore } from '@/stores/tenant'

const tenantStore = useTenantStore()
const loading = ref(false)
const members = ref<TenantMember[]>([])
const disabling = ref<number | null>(null)
const deleting = ref<number | null>(null)
const fetchMembers = async () => {
  if (!tenantStore.tenantId) return
  loading.value = true
  try {
    const response = await getTenantMembers(tenantStore.tenantId)
    members.value = response.success && response.data ? response.data : []
  } finally {
    loading.value = false
  }
}
const disableMember = async (member: TenantMember) => {
  try {
    await ElMessageBox.confirm(`停用后，${member.alias || member.name} 将立即失去当前租户的资源访问资格。`, '停用租户成员', { type: 'warning', confirmButtonText: '确认停用', cancelButtonText: '取消' })
  } catch { return }
  disabling.value = member.user_id
  try {
    const response = await disableTenantMember(tenantStore.tenantId, member.user_id)
    if (response.success) {
      ElMessage.success('成员已停用')
      await fetchMembers()
    }
  } finally { disabling.value = null }
}
const deleteMember = async (member: TenantMember) => {
  try {
    await ElMessageBox.confirm(`删除后，${member.alias || member.name} 将失去当前租户的资源访问资格，并从成员列表中移除。`, '删除租户成员', { type: 'warning', confirmButtonText: '确认删除', cancelButtonText: '取消' })
  } catch { return }
  deleting.value = member.user_id
  try {
    const response = await deleteTenantMember(tenantStore.tenantId, member.user_id)
    if (response.success) {
      ElMessage.success('成员已删除')
      await fetchMembers()
    }
  } finally { deleting.value = null }
}
onMounted(fetchMembers)
watch(() => tenantStore.contextRevision, fetchMembers)
</script>

<style scoped>
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; margin-bottom: 18px; }
h1 { margin: 0; color: var(--text-primary); font-size: 24px; line-height: 32px; }
.page-header p { margin: 5px 0 0; color: var(--text-secondary); font-size: 13px; }
.secondary { margin-top: 2px; color: var(--text-secondary); font-size: 12px; }
</style>
