<template>
  <div class="page-container">
    <div class="page-header">
      <div>
        <h1>成员</h1>
        <p>这里展示当前租户与实名用户之间的成员关系，不是平台用户目录。</p>
      </div>
      <div class="header-actions">
        <el-button :loading="loading" @click="fetchMembers">刷新</el-button>
        <el-button v-if="canWrite" type="primary" @click="openAddDialog">添加成员</el-button>
      </div>
    </div>
    <el-table v-loading="loading" :data="members" empty-text="当前租户还没有成员">
      <el-table-column label="实名用户" min-width="220"><template #default="scope"><strong>{{ scope.row.alias || scope.row.name }}</strong><div v-if="scope.row.alias" class="secondary">{{ scope.row.name }}</div></template></el-table-column>
      <el-table-column prop="role" label="成员角色" width="150" />
      <el-table-column label="状态" width="120"><template #default="scope"><el-tag size="small" effect="plain" :type="scope.row.enabled ? 'success' : 'info'">{{ scope.row.enabled ? '有效' : '已停用' }}</el-tag></template></el-table-column>
      <el-table-column prop="expires_at" label="有效期" min-width="190"><template #default="scope">{{ scope.row.expires_at || '长期有效' }}</template></el-table-column>
      <el-table-column v-if="tenantStore.canTenant('tenant.members.write')" label="操作" width="140" fixed="right">
        <template #default="scope">
          <el-button v-if="scope.row.enabled" type="warning" link :loading="disabling === scope.row.user_id" :disabled="deleting === scope.row.user_id" @click="disableMember(scope.row)">停用</el-button>
          <el-button v-else type="primary" link :loading="restoring === scope.row.user_id" :disabled="deleting === scope.row.user_id" @click="restoreMember(scope.row)">恢复</el-button>
          <el-button type="danger" link :loading="deleting === scope.row.user_id" :disabled="disabling === scope.row.user_id" @click="deleteMember(scope.row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="addDialogVisible" title="添加租户成员" width="520px" @closed="resetAddDialog">
      <el-form label-position="top">
        <el-form-item label="用户">
          <el-select
            v-model="selectedUserId"
            filterable
            remote
            clearable
            :remote-method="searchCandidates"
            :loading="candidateLoading"
            placeholder="输入用户名或别名搜索"
            style="width: 100%"
          >
            <el-option v-for="candidate in candidates" :key="candidate.user_id" :label="candidate.alias || candidate.name" :value="candidate.user_id">
              <span>{{ candidate.alias || candidate.name }}</span>
              <span v-if="candidate.alias" class="candidate-name">{{ candidate.name }}</span>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item label="成员角色">
          <el-radio-group v-model="selectedRole">
            <el-radio-button value="member">成员</el-radio-button>
            <el-radio-button value="viewer">只读成员</el-radio-button>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="adding" :disabled="!selectedUserId" @click="addMember">确认添加</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { addTenantMember, deleteTenantMember, disableTenantMember, getTenantMemberCandidates, getTenantMembers, type TenantMember, type TenantMemberCandidate } from '@/api/resource'
import { useTenantStore } from '@/stores/tenant'

const tenantStore = useTenantStore()
const loading = ref(false)
const members = ref<TenantMember[]>([])
const canWrite = computed(() => tenantStore.canTenant('tenant.members.write'))
const disabling = ref<number | null>(null)
const deleting = ref<number | null>(null)
const restoring = ref<number | null>(null)
const addDialogVisible = ref(false)
const candidateLoading = ref(false)
const adding = ref(false)
const candidates = ref<TenantMemberCandidate[]>([])
const selectedUserId = ref<number | null>(null)
const selectedRole = ref<'member' | 'viewer'>('member')
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
const restoreMember = async (member: TenantMember) => {
  restoring.value = member.user_id
  try {
    const response = await addTenantMember(tenantStore.tenantId, { user_id: member.user_id, role: member.role === 'viewer' ? 'viewer' : 'member' })
    if (response.success) {
      ElMessage.success('成员已恢复')
      await fetchMembers()
    }
  } finally { restoring.value = null }
}
const searchCandidates = async (search = '') => {
  if (!tenantStore.tenantId) return
  candidateLoading.value = true
  try {
    const response = await getTenantMemberCandidates(tenantStore.tenantId, { search, page: 1, size: 20 })
    candidates.value = response.success && response.data ? response.data : []
  } finally { candidateLoading.value = false }
}
const openAddDialog = async () => {
  addDialogVisible.value = true
  await searchCandidates()
}
const resetAddDialog = () => {
  selectedUserId.value = null
  selectedRole.value = 'member'
  candidates.value = []
}
const addMember = async () => {
  if (!selectedUserId.value) return
  adding.value = true
  try {
    const response = await addTenantMember(tenantStore.tenantId, { user_id: selectedUserId.value, role: selectedRole.value })
    if (response.success) {
      ElMessage.success('成员已添加')
      addDialogVisible.value = false
      await fetchMembers()
    }
  } finally { adding.value = false }
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
.header-actions { display: flex; gap: 8px; }
h1 { margin: 0; color: var(--text-primary); font-size: 24px; line-height: 32px; }
.page-header p { margin: 5px 0 0; color: var(--text-secondary); font-size: 13px; }
.secondary { margin-top: 2px; color: var(--text-secondary); font-size: 12px; }
.candidate-name { float: right; color: var(--text-secondary); font-size: 12px; }
</style>
