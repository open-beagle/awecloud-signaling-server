<template>
  <div class="acl-endpoint-k8s-service-detail">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>{{ $t('aclEndpoint.k8sserviceInfo') }}</span>
          <el-tag :type="detail?.status === 'online' ? 'success' : 'info'" size="small">
            {{ detail?.status === 'online' ? $t('common.online') : $t('common.offline') }}
          </el-tag>
        </div>
      </template>
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="$t('endpoint.name')">{{ detail?.name }}</el-descriptions-item>
        <el-descriptions-item :label="$t('endpoint.alias')">{{ detail?.alias || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="$t('endpoint.ownerAgent')">{{ detail?.agent_name || '-' }}</el-descriptions-item>
        <el-descriptions-item label="">&nbsp;</el-descriptions-item>
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
        <el-table-column prop="namespaces" :label="$t('acl.namespaces')" min-width="150">
          <template #default="{ row }">
            <template v-if="!row.namespaces || row.namespaces.length === 0"><el-tag type="success" size="small">{{ $t('common.all') }}</el-tag></template>
            <template v-else><el-tag v-for="ns in row.namespaces" :key="ns" size="small" type="info" style="margin-right: 4px">{{ ns }}</el-tag></template>
          </template>
        </el-table-column>
        <el-table-column prop="service_names" :label="$t('acl.serviceNames')" min-width="150">
          <template #default="{ row }">
            <template v-if="!row.service_names || row.service_names.length === 0"><el-tag type="success" size="small">{{ $t('common.all') }}</el-tag></template>
            <template v-else><el-tag v-for="sn in row.service_names" :key="sn" size="small" style="margin-right: 4px">{{ sn }}</el-tag></template>
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
        <el-table-column prop="namespaces" :label="$t('acl.namespaces')" min-width="150">
          <template #default="{ row }">
            <template v-if="!row.namespaces || row.namespaces.length === 0"><el-tag type="success" size="small">{{ $t('common.all') }}</el-tag></template>
            <template v-else><el-tag v-for="ns in row.namespaces" :key="ns" size="small" type="info" style="margin-right: 4px">{{ ns }}</el-tag></template>
          </template>
        </el-table-column>
        <el-table-column prop="service_names" :label="$t('acl.serviceNames')" min-width="150">
          <template #default="{ row }">
            <template v-if="!row.service_names || row.service_names.length === 0"><el-tag type="success" size="small">{{ $t('common.all') }}</el-tag></template>
            <template v-else><el-tag v-for="sn in row.service_names" :key="sn" size="small" style="margin-right: 4px">{{ sn }}</el-tag></template>
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
    <AuthGrantDialog ref="addUserDialogRef" v-model="showAddUserDialog" :title="$t('aclEndpoint.addK8SServiceUserAuth')" mode="user" @confirm="handleConfirmUser">
      <template #extra>
        <el-form-item :label="$t('acl.namespaces')">
          <el-select v-model="extraForm.namespaces" multiple filterable allow-create default-first-option :placeholder="$t('acl.namespacesPlaceholder')" style="width: 100%">
            <el-option label="* (全部)" value="*" />
            <el-option label="default" value="default" />
          </el-select>
          <div class="form-tip">{{ $t('acl.namespacesTip') }}</div>
        </el-form-item>
        <el-form-item :label="$t('acl.serviceNames')">
          <el-select v-model="extraForm.serviceNames" multiple filterable allow-create default-first-option :placeholder="$t('acl.serviceNamesPlaceholder')" style="width: 100%">
            <el-option label="* (全部)" value="*" />
          </el-select>
          <div class="form-tip">{{ $t('acl.serviceNamesTip') }}</div>
        </el-form-item>
      </template>
    </AuthGrantDialog>

    <!-- 添加分组授权弹窗 -->
    <AuthGrantDialog ref="addGroupDialogRef" v-model="showAddGroupDialog" :title="$t('aclEndpoint.addK8SServiceGroupAuth')" mode="group" @confirm="handleConfirmGroup">
      <template #extra>
        <el-form-item :label="$t('acl.namespaces')">
          <el-select v-model="extraForm.namespaces" multiple filterable allow-create default-first-option :placeholder="$t('acl.namespacesPlaceholder')" style="width: 100%">
            <el-option label="* (全部)" value="*" />
            <el-option label="default" value="default" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('acl.serviceNames')">
          <el-select v-model="extraForm.serviceNames" multiple filterable allow-create default-first-option :placeholder="$t('acl.serviceNamesPlaceholder')" style="width: 100%">
            <el-option label="* (全部)" value="*" />
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
import { getEndpointK8SServiceACL, addEndpointK8SServiceACLUsers, addEndpointK8SServiceACLGroups, removeEndpointK8SServiceACLUser, removeEndpointK8SServiceACLGroup, type EndpointK8SServiceACLDetail, type EndpointK8SServiceACLPermissionItem } from '@/api/aclEndpointK8sservice'
import { formatTime } from '@/utils/time'
import AuthGrantDialog from '@/components/Common/AuthGrantDialog.vue'

const { t } = useI18n()
const route = useRoute()
const endpointId = route.params.id as string
const detail = ref<EndpointK8SServiceACLDetail | null>(null)
const showAddUserDialog = ref(false)
const showAddGroupDialog = ref(false)
const addUserDialogRef = ref<InstanceType<typeof AuthGrantDialog>>()
const addGroupDialogRef = ref<InstanceType<typeof AuthGrantDialog>>()

const extraForm = reactive({ namespaces: ['*'] as string[], serviceNames: ['*'] as string[] })
const resetExtraForm = () => { extraForm.namespaces = ['*']; extraForm.serviceNames = ['*'] }

const fetchDetail = async () => {
  try {
    const res = await getEndpointK8SServiceACL(endpointId)
    if (res.success && res.data) { detail.value = res.data }
  } catch (error) { console.error('获取详情失败:', error) }
}

const handleConfirmUser = async (userIds: number[]) => {
  addUserDialogRef.value?.setSubmitting(true)
  try {
    const res = await addEndpointK8SServiceACLUsers(endpointId, { user_ids: userIds, namespaces: extraForm.namespaces, service_names: extraForm.serviceNames })
    if (res?.success) { ElMessage.success(t('acl.authSuccess')); addUserDialogRef.value?.close(); resetExtraForm(); fetchDetail() }
    else { ElMessage.error(res?.message || t('acl.authFailed')) }
  } catch { ElMessage.error(t('acl.authFailed')) }
  finally { addUserDialogRef.value?.setSubmitting(false) }
}

const handleConfirmGroup = async (groupIds: number[]) => {
  addGroupDialogRef.value?.setSubmitting(true)
  try {
    const res = await addEndpointK8SServiceACLGroups(endpointId, { group_ids: groupIds, namespaces: extraForm.namespaces, service_names: extraForm.serviceNames })
    if (res?.success) { ElMessage.success(t('acl.authSuccess')); addGroupDialogRef.value?.close(); resetExtraForm(); fetchDetail() }
    else { ElMessage.error(res?.message || t('acl.authFailed')) }
  } catch { ElMessage.error(t('acl.authFailed')) }
  finally { addGroupDialogRef.value?.setSubmitting(false) }
}

const handleRevokeUser = async (row: EndpointK8SServiceACLPermissionItem) => {
  try {
    await ElMessageBox.confirm(t('acl.revokeUserConfirm', { name: row.name }), t('common.warning'), { type: 'warning' })
    const res = await removeEndpointK8SServiceACLUser(endpointId, row.id)
    if (res.success) { ElMessage.success(t('acl.revokeSuccess')); fetchDetail() }
    else { ElMessage.error(res.message || t('acl.revokeFailed')) }
  } catch (error) { if (error !== 'cancel') console.error('撤销授权失败:', error) }
}

const handleRevokeGroup = async (row: EndpointK8SServiceACLPermissionItem) => {
  try {
    await ElMessageBox.confirm(t('acl.revokeGroupConfirm', { name: row.name }), t('common.warning'), { type: 'warning' })
    const res = await removeEndpointK8SServiceACLGroup(endpointId, row.id)
    if (res.success) { ElMessage.success(t('acl.revokeSuccess')); fetchDetail() }
    else { ElMessage.error(res.message || t('acl.revokeFailed')) }
  } catch (error) { if (error !== 'cancel') console.error('撤销授权失败:', error) }
}

onMounted(() => { fetchDetail() })
</script>

<style scoped>
.acl-endpoint-k8s-service-detail { width: 100%; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
</style>

<style>
.acl-endpoint-k8s-service-detail .el-descriptions__body .el-descriptions__table { table-layout: fixed; }
.acl-endpoint-k8s-service-detail .el-descriptions__label { width: 100px !important; min-width: 100px !important; max-width: 100px !important; }
</style>