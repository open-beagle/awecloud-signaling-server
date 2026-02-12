<template>
  <div class="endpoint-detail" v-loading="loading">
    <template v-if="endpoint">
      <!-- 基本信息 -->
      <el-card shadow="never" class="info-card">
        <template #header>
          <div class="card-header">
            <span>{{ $t('endpoint.basicInfo') }}</span>
            <div>
              <el-tag :type="endpoint.status === 'online' ? 'success' : 'info'" size="small" style="margin-right: 8px">
                {{ endpoint.status === 'online' ? $t('common.online') : $t('common.offline') }}
              </el-tag>
              <el-tag :type="typeTagMap[endpoint.type]" size="small">
                {{ typeLabelMap[endpoint.type] }}
              </el-tag>
            </div>
          </div>
        </template>
        <el-descriptions :column="2" border label-class-name="desc-label">
          <el-descriptions-item :label="$t('endpoint.name')">{{ endpoint.name }}</el-descriptions-item>
          <el-descriptions-item :label="$t('endpoint.alias')">{{ endpoint.alias || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('endpoint.ownerAgent')">{{ endpoint.agent_name || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('endpoint.type')">
            <el-tag :type="typeTagMap[endpoint.type]" size="small">{{ typeLabelMap[endpoint.type] }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="$t('common.enabled')">
            <el-tag :type="endpoint.enabled ? 'success' : 'danger'" size="small">
              {{ endpoint.enabled ? $t('common.enabled') : $t('common.disabled') }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="$t('common.createdAt')">
            {{ endpoint.created_at ? formatTime(endpoint.created_at) : '-' }}
          </el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- SSH 能力 -->
      <el-card v-if="endpoint.type === 'ssh'" shadow="never" class="info-card">
        <template #header>
          <span>{{ $t('endpoint.sshCapability') }}</span>
        </template>
        <el-descriptions :column="2" border label-class-name="desc-label">
          <el-descriptions-item :label="$t('endpoint.host')">{{ endpoint.host || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('endpoint.port')">{{ endpoint.port || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('endpoint.domain')">{{ endpoint.domain || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('endpoint.sshUsers')">
            <template v-if="endpoint.ssh_users?.length">
              <el-tag v-for="u in endpoint.ssh_users" :key="u" size="small" class="tag-item">{{ u }}</el-tag>
            </template>
            <span v-else>-</span>
          </el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- K8SAPI 能力 -->
      <el-card v-if="endpoint.type === 'k8sapi'" shadow="never" class="info-card">
        <template #header>
          <span>{{ $t('endpoint.k8sapiCapability') }}</span>
        </template>
        <el-descriptions :column="2" border label-class-name="desc-label">
          <el-descriptions-item :label="$t('endpoint.apiServer')">{{ endpoint.api_server || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('endpoint.domain')">{{ endpoint.domain || '-' }}</el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- K8SService 能力 -->
      <el-card v-if="endpoint.type === 'k8sservice'" shadow="never" class="info-card">
        <template #header>
          <span>{{ $t('endpoint.k8sserviceCapability') }}</span>
        </template>
        <el-descriptions :column="2" border label-class-name="desc-label">
          <el-descriptions-item :label="$t('endpoint.domain')">{{ endpoint.domain || '-' }}</el-descriptions-item>
          <el-descriptions-item label="">&nbsp;</el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- 操作区 -->
      <el-card shadow="never" class="info-card">
        <template #header>
          <span>{{ $t('common.actions') }}</span>
        </template>
        <el-space>
          <el-button type="primary" @click="handleEdit">{{ $t('endpoint.editAlias') }}</el-button>
          <el-button :type="endpoint.enabled ? 'warning' : 'success'" @click="handleToggle">
            {{ endpoint.enabled ? $t('common.disabled') : $t('common.enabled') }}
          </el-button>
          <el-button type="danger" @click="handleRevoke">{{ $t('endpoint.revoke') }}</el-button>
        </el-space>
      </el-card>
    </template>

    <!-- 编辑别名弹窗 -->
    <el-dialog v-model="showEditDialog" :title="$t('endpoint.editAlias')" width="400px">
      <el-form label-width="80px">
        <el-form-item :label="$t('endpoint.alias')">
          <el-input v-model="editAlias" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showEditDialog = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="handleEditSubmit">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getEndpointDetail, updateEndpoint, revokeEndpoint, type EndpointDetail } from '@/api/endpoint'
import { formatTime } from '@/utils/time'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const loading = ref(false)
const submitting = ref(false)
const endpoint = ref<EndpointDetail | null>(null)
const showEditDialog = ref(false)
const editAlias = ref('')

const typeTagMap: Record<string, string> = { ssh: '', k8sapi: 'warning', k8sservice: 'success' }
const typeLabelMap: Record<string, string> = { ssh: 'SSH', k8sapi: 'K8S API', k8sservice: 'K8S Service' }

const fetchDetail = async () => {
  const epType = route.params.type as string
  const id = route.params.id as string
  if (!epType || !id) return

  loading.value = true
  try {
    const res = await getEndpointDetail(epType, id)
    if (res.success && res.data) {
      endpoint.value = res.data
    }
  } catch (error) {
    console.error(error)
  } finally {
    loading.value = false
  }
}

const handleEdit = () => {
  editAlias.value = endpoint.value?.alias || ''
  showEditDialog.value = true
}

const handleEditSubmit = async () => {
  if (!endpoint.value) return
  submitting.value = true
  try {
    const res = await updateEndpoint(endpoint.value.type, endpoint.value.id, { alias: editAlias.value })
    if (res.success) {
      ElMessage.success(t('common.updateSuccess'))
      showEditDialog.value = false
      fetchDetail()
    }
  } catch (e) { console.error(e) } finally { submitting.value = false }
}

const handleToggle = async () => {
  if (!endpoint.value) return
  const msg = endpoint.value.enabled ? t('endpoint.disableConfirm') : t('endpoint.enableConfirm')
  try {
    await ElMessageBox.confirm(msg, t('common.warning'), { type: 'warning' })
    const res = await updateEndpoint(endpoint.value.type, endpoint.value.id, { enabled: !endpoint.value.enabled })
    if (res.success) { ElMessage.success(t('common.updateSuccess')); fetchDetail() }
  } catch { /* cancelled */ }
}

const handleRevoke = async () => {
  if (!endpoint.value) return
  try {
    await ElMessageBox.confirm(t('endpoint.revokeConfirm'), t('common.warning'), { type: 'warning' })
    const res = await revokeEndpoint(endpoint.value.type, endpoint.value.id)
    if (res.success) {
      ElMessage.success(t('endpoint.revokeSuccess'))
      router.push('/endpoints')
    }
  } catch { /* cancelled */ }
}

onMounted(() => { fetchDetail() })
</script>

<style scoped>
.endpoint-detail { width: 100%; }
.info-card { margin-bottom: 20px; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.tag-item { margin-right: 8px; margin-bottom: 4px; }
</style>

<style>
.endpoint-detail .el-descriptions__body .el-descriptions__table {
  table-layout: fixed;
}
.endpoint-detail .el-descriptions__label {
  width: 100px !important;
  min-width: 100px !important;
  max-width: 100px !important;
}
</style>
