<template>
  <div class="acl-endpoint-k8s-detail">
    <!-- 基本信息 -->
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>{{ $t('aclEndpoint.k8sapiInfo') }}</span>
          <el-tag :type="detail?.status === 'online' ? 'success' : 'info'" size="small">
            {{ detail?.status === 'online' ? $t('common.online') : $t('common.offline') }}
          </el-tag>
        </div>
      </template>
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="$t('endpoint.name')">{{ detail?.name }}</el-descriptions-item>
        <el-descriptions-item :label="$t('endpoint.alias')">{{ detail?.alias || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('endpoint.ownerAgent')">{{ detail?.agent_name || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('endpoint.apiServer')">{{ detail?.api_server || '-' }}</el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 用户授权 -->
    <el-card shadow="never" style="margin-top: 20px">
      <template #header>
        <div class="card-header">
          <span>{{ $t('acl.userAuth') }} ({{ detail?.users?.length || 0 }})</span>
          <el-button type="primary" size="small" @click="showAddUserDialog = true">
            <el-icon><Plus /></el-icon>{{ $t('acl.addUser') }}
          </el-button>
        </div>
      </template>
      <el-table :data="detail?.users || []" stripe>
        <el-table-column prop="name" :label="$t('acl.userName')" min-width="120" />
        <el-table-column prop="alias" :label="$t('user.alias')" min-width="100" />
        <el-table-column prop="k8s_groups" :label="$t('acl.k8sGroups')" min-width="180">
          <template #default="{ row }">
            <el-tag v-for="g in row.k8s_groups" :key="g" size="small" style="margin-right: 4px">{{ g }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="namespaces" :label="$t('acl.namespaces')" min-width="150">
          <template #default="{ row }">
            <template v-if="!row.namespaces || row.namespaces.length === 0"><el-tag type="success" size="small">{{ $t('common.all') }}</el-tag></template>
            <template v-else><el-tag v-for="ns in row.namespaces" :key="ns" size="small" type="info" style="margin-right: 4px">{{ ns }}</el-tag></template>
          </template>
        </el-table-column>
        <el-table-column prop="granted_at" :label="$t('acl.grantedAt')" width="180">
          <template #default="{ row }">{{ formatTime(row.granted_at) }}</template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" link size="small" @click="handleRevokeUser(row)">{{ $t('acl.revoke') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 分组授权 -->
    <el-card shadow="never" style="margin-top: 20px">
      <template #header>
        <div class="card-header">
          <span>{{ $t('acl.groupAuth') }} ({{ detail?.groups?.length || 0 }})</span>
          <el-button type="primary" size="small" @click="showAddGroupDialog = true">
            <el-icon><Plus /></el-icon>{{ $t('acl.addGroup') }}
          </el-button>
        </div>
      </template>
      <el-table :data="detail?.groups || []" stripe>
        <el-table-column prop="name" :label="$t('acl.groupName')" min-width="120" />
        <el-table-column prop="alias" :label="$t('group.description')" min-width="100" />
        <el-table-column prop="k8s_groups" :label="$t('acl.k8sGroups')" min-width="180">
          <template #default="{ row }">
            <el-tag v-for="g in row.k8s_groups" :key="g" size="small" style="margin-right: 4px">{{ g }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="namespaces" :label="$t('acl.namespaces')" min-width="150">
          <template #default="{ row }">
            <template v-if="!row.namespaces || row.namespaces.length === 0"><el-tag type="success" size="small">{{ $t('common.all') }}</el-tag></template>
            <template v-else><el-tag v-for="ns in row.namespaces" :key="ns" size="small" type="info" style="margin-right: 4px">{{ ns }}</el-tag></template>
          </template>
        </el-table-column>
        <el-table-column prop="granted_at" :label="$t('acl.grantedAt')" width="180">
          <template #default="{ row }">{{ formatTime(row.granted_at) }}</template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" link size="small" @click="handleRevokeGroup(row)">{{ $t('acl.revoke') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 添加用户授权弹窗 -->
    <AuthGrantDialog ref="addUserDialogRef" v-model="showAddUserDialog" :title="$t('aclEndpoint.addK8SUserAuth')" mode="user" @confirm="handleConfirmUser">
      <template #extra>
        <el-form-item :label="$t('acl.k8sGroups')">
          <el-select v-model="extraForm.k8sGroups" multiple filterable allow-create default-first-option :placeholder="$t('acl.k8sGroupsPlaceholder')" style="width: 100%">
            <el-option label="system:masters" value="system:masters" />
            <el-option label="system:authenticated" value="system:authenticated" />
          </el-select>
          <div class="form-tip">{{ $t('acl.k8sGroupsTip') }}</div>
        </el-form-item>
        <el-form-item :label="$t('acl.namespaces')">
          <el-select v-model="extraForm.namespaces" multiple filterable allow-create default-first-option :placeholder="$t('acl.namespacesPlaceholder')" style="width: 100%">
            <el-option label="* (全部)" value="*" />
            <el-option label="default" value="default" />
            <el-option label="kube-system" value="kube-system" />
          </el-select>
          <div class="form-tip">{{ $t('acl.namespacesTip') }}</div>
        </el-form-item>
      </template>
    </AuthGrantDialog>

    <!-- 添加分组授权弹窗 -->
    <AuthGrantDialog ref="addGroupDialogRef" v-model="showAddGroupDialog" :title="$t('aclEndpoint.addK8SGroupAuth')" mode="group" @confirm="handleConfirmGroup">
      <template #extra>
        <el-form-item :label="$t('acl.k8sGroups')">
          <el-select v-model="extraForm.k8sGroups" multiple filterable allow-create default-first-option :placeholder="$t('acl.k8sGroupsPlaceholder')" style="width: 100%">
            <el-option label="system:masters" value="system:masters" />
            <el-option label="system:authenticated" value="system:authenticated" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('acl.namespaces')">
          <el-select v-model="extraForm.namespaces" multiple filterable allow-create default-first-option :placeholder="$t('acl.namespacesPlaceholder')" style="width: 100%">
            <el-option label="* (全部)" value="*" />
            <el-option label="default" value="default" />
            <el-option label="kube-system" value="kube-system" />
          </el-select>
        </el-form-item>
      </template>
    </AuthGrantDialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { getEndpointK8SAPIACL, addEndpointK8SAPIACLUsers, addEndpointK8SAPIACLGroups, removeEndpointK8SAPIACLUser, removeEndpointK8SAPIACLGroup, type EndpointK8SAPIACLDetail, type EndpointK8SAPIACLPermissionItem } from '@/api/aclEndpointK8sapi'
import { formatTime } from '@/utils/time'
import AuthGrantDialog from '@/components/Common/AuthGrantDialog.vue'

const { t } = useI18n()
const route = useRoute()
const endpointId = route.params.id as string
const detail = ref<EndpointK8SAPIACLDetail | null>(null)
const showAddUserDialog = ref(false)
const showAddGroupDialog = ref(false)
const addUserDialogRef = ref<InstanceType<typeof AuthGrantDialog>>()
const addGroupDialogRef = ref<InstanceType<typeof AuthGrantDialog>>()

const extraForm = reactive({ k8sGroups: ['system:masters'] as string[], namespaces: ['*'] as string[] })
const resetExtraForm = () => { extraForm.k8sGroups = ['system:masters']; extraForm.namespaces = ['*'] }

const fetchDetail = async () => {
  try {
    const res = await getEndpointK8SAPIACL(endpointId)
    if (res.success && res.data) { detail.value = res.data }
  } catch (error) { console.error('获取详情失败:', error) }
}

const handleConfirmUser = async (userIds: number[]) => {
  addUserDialogRef.value?.setSubmitting(true)
  try {
    const res = await addEndpointK8SAPIACLUsers(endpointId, { user_ids: userIds, k8s_groups: extraForm.k8sGroups, namespaces: extraForm.namespaces })
    if (res?.success) { ElMessage.success(t('acl.authSuccess')); addUserDialogRef.value?.close(); resetExtraForm(); fetchDetail() }
    else { ElMessage.error(res?.message || t('acl.authFailed')) }
  } catch { ElMessage.error(t('acl.authFailed')) }
  finally { addUserDialogRef.value?.setSubmitting(false) }
}

const handleConfirmGroup = async (groupIds: number[]) => {
  addGroupDialogRef.value?.setSubmitting(true)
  try {
    const res = await addEndpointK8SAPIACLGroups(endpointId, { group_ids: groupIds, k8s_groups: extraForm.k8sGroups, namespaces: extraForm.namespaces })
    if (res?.success) { ElMessage.success(t('acl.authSuccess')); addGroupDialogRef.value?.close(); resetExtraForm(); fetchDetail() }
    else { ElMessage.error(res?.message || t('acl.authFailed')) }
  } catch { ElMessage.error(t('acl.authFailed')) }
  finally { addGroupDialogRef.value?.setSubmitting(false) }
}

const handleRevokeUser = async (row: EndpointK8SAPIACLPermissionItem) => {
  try {
    await ElMessageBox.confirm(t('acl.revokeUserConfirm', { name: row.name }), t('common.warning'), { type: 'warning' })
    const res = await removeEndpointK8SAPIACLUser(endpointId, row.id)
    if (res.success) { ElMessage.success(t('acl.revokeSuccess')); fetchDetail() }
    else { ElMessage.error(res.message || t('acl.revokeFailed')) }
  } catch (error) { if (error !== 'cancel') console.error('撤销授权失败:', error) }
}

const handleRevokeGroup = async (row: EndpointK8SAPIACLPermissionItem) => {
  try {
    await ElMessageBox.confirm(t('acl.revokeGroupConfirm', { name: row.name }), t('common.warning'), { type: 'warning' })
    const res = await removeEndpointK8SAPIACLGroup(endpointId, row.id)
    if (res.success) { ElMessage.success(t('acl.revokeSuccess')); fetchDetail() }
    else { ElMessage.error(res.message || t('acl.revokeFailed')) }
  } catch (error) { if (error !== 'cancel') console.error('撤销授权失败:', error) }
}

onMounted(() => { fetchDetail() })
</script>

<style scoped>
.acl-endpoint-k8s-detail { width: 100%; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
</style>

<style>
.acl-endpoint-k8s-detail .el-descriptions__body .el-descriptions__table { table-layout: fixed; }
.acl-endpoint-k8s-detail .el-descriptions__label { width: 100px !important; min-width: 100px !important; max-width: 100px !important; }
</style>
