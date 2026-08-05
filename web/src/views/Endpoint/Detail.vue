<template>
  <div class="endpoint-detail" v-loading="loading">
    <template v-if="endpoint">
      <!-- 基本信息 -->
      <el-card shadow="never" class="info-card">
        <template #header>
          <div class="card-header">
            <span>{{ $t('endpoint.basicInfo') }}</span>
            <el-tag :type="endpoint.status === 'online' ? 'success' : 'info'" size="small">
              {{ endpoint.status === 'online' ? $t('common.online') : $t('common.offline') }}
            </el-tag>
          </div>
        </template>
        <el-descriptions :column="2" border label-class-name="desc-label">
          <el-descriptions-item :label="$t('endpoint.name')">{{ endpoint.name }}</el-descriptions-item>
          <el-descriptions-item :label="$t('endpoint.alias')">{{ endpoint.alias || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('endpoint.ownerAgent')">{{ endpoint.agent_name || '-' }}</el-descriptions-item>
		  <el-descriptions-item label="上报主机名">{{ endpoint.hostname || '-' }}</el-descriptions-item>
		  <el-descriptions-item label="SSH 域名标识"><span class="mono">{{ endpoint.host_domain_label || '待配置' }}</span><el-button link type="primary" @click="editHostDomainLabel">编辑</el-button></el-descriptions-item>
          <el-descriptions-item :label="$t('endpoint.version')">{{ endpoint.version || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('endpoint.capabilities')">
            <el-tag v-if="endpoint.ssh_enabled" size="small" class="tag-item">SSH</el-tag>
            <el-tag v-if="endpoint.k8sapi_enabled" type="warning" size="small" class="tag-item">K8S API</el-tag>
            <el-tag v-if="endpoint.k8sservice_enabled" type="success" size="small" class="tag-item">K8S Service</el-tag>
            <span v-if="!endpoint.ssh_enabled && !endpoint.k8sapi_enabled && !endpoint.k8sservice_enabled">-</span>
          </el-descriptions-item>
          <el-descriptions-item :label="$t('common.createdAt')">{{ endpoint.created_at ? formatTime(endpoint.created_at) : '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('endpoint.updatedAt')">{{ endpoint.updated_at ? formatTime(endpoint.updated_at) : '-' }}</el-descriptions-item>
          <el-descriptions-item label="">&nbsp;</el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- SSH 能力 -->
      <el-card shadow="never" class="info-card">
        <template #header>
          <div class="card-header">
            <span>{{ $t('endpoint.sshCapability') }}</span>
            <el-switch :model-value="endpoint.ssh_enabled" :loading="toggling" @change="(val: boolean) => handleToggleCapability('ssh_enabled', val)" />
          </div>
        </template>
        <el-descriptions :column="2" border label-class-name="desc-label">
          <el-descriptions-item :label="$t('endpoint.sshUsers')">
            <template v-if="endpoint.ssh_users?.length">
              <el-tag v-for="u in endpoint.ssh_users" :key="u" size="small" class="tag-item">{{ u }}</el-tag>
            </template>
            <span v-else>-</span>
          </el-descriptions-item>
          <el-descriptions-item :label="$t('endpoint.port')">{{ endpoint.ssh_port || '-' }}</el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- K8SAPI 能力 -->
      <el-card shadow="never" class="info-card">
        <template #header>
          <div class="card-header">
            <span>{{ $t('endpoint.k8sapiCapability') }}</span>
            <el-switch :model-value="endpoint.k8sapi_enabled" :loading="toggling" @change="(val: boolean) => handleToggleCapability('k8sapi_enabled', val)" />
          </div>
        </template>
        <el-descriptions :column="2" border label-class-name="desc-label">
          <el-descriptions-item :label="$t('endpoint.apiServer')">{{ endpoint.k8sapi_api_server || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('endpoint.port')">{{ endpoint.k8sapi_port || '-' }}</el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- K8SService 能力 -->
      <el-card shadow="never" class="info-card">
        <template #header>
          <div class="card-header">
            <span>{{ $t('endpoint.k8sserviceCapability') }}</span>
            <el-switch :model-value="endpoint.k8sservice_enabled" :loading="toggling" @change="(val: boolean) => handleToggleCapability('k8sservice_enabled', val)" />
          </div>
        </template>
        <el-descriptions :column="2" border label-class-name="desc-label">
          <el-descriptions-item :label="$t('endpoint.labelSelector')">{{ endpoint.k8sservice_label_selector || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('endpoint.namespaces')">
            <template v-if="endpoint.k8sservice_namespaces?.length">
              <el-tag v-for="ns in endpoint.k8sservice_namespaces" :key="ns" size="small" class="tag-item">{{ ns }}</el-tag>
            </template>
            <span v-else>-</span>
          </el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- 操作区 -->
      <el-card shadow="never" class="info-card">
        <template #header>
          <span>{{ $t('common.actions') }}</span>
        </template>
        <el-space>
          <el-button type="primary" @click="handleEdit">{{ $t('endpoint.editAlias') }}</el-button>
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
import { getEndpointDetail, updateEndpoint, updateEndpointHostDomainLabel, revokeEndpoint, type EndpointDetail } from '@/api/endpoint'
import { formatTime } from '@/utils/time'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const loading = ref(false)
const submitting = ref(false)
const toggling = ref(false)
const endpoint = ref<EndpointDetail | null>(null)
const showEditDialog = ref(false)
const editAlias = ref('')

const editHostDomainLabel = async () => {
  if (!endpoint.value) return
  try {
    const result = await ElMessageBox.prompt('修改后旧 SSH 域名立即失效。', '编辑 SSH 域名标识', {
      inputValue: endpoint.value.host_domain_label,
      inputPattern: /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/,
      inputErrorMessage: '请输入有效的 DNS 单标签',
      confirmButtonText: '保存',
      cancelButtonText: '取消',
    })
    await updateEndpointHostDomainLabel(endpoint.value.id, result.value.trim().toLowerCase())
    ElMessage.success('SSH 域名标识已更新')
    await fetchDetail()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') throw error
  }
}

// 获取详情
const fetchDetail = async () => {
  const id = route.params.id as string
  if (!id) return
  loading.value = true
  try {
    const res = await getEndpointDetail(id)
    if (res.success && res.data) {
      endpoint.value = res.data
    }
  } catch (error) {
    console.error(error)
  } finally {
    loading.value = false
  }
}

// 切换能力开关
const handleToggleCapability = async (field: string, val: boolean) => {
  if (!endpoint.value) return
  toggling.value = true
  try {
    const res = await updateEndpoint(endpoint.value.id, { [field]: val })
    if (res.success) {
      ElMessage.success(t('common.updateSuccess'))
    } else {
      ElMessage.error(res.message || t('common.updateFailed'))
    }
  } catch (error) {
    console.error(error)
    ElMessage.error(t('common.updateFailed'))
  } finally {
    // 无论成功失败，都重新获取数据以确保显示正确状态
    await fetchDetail()
    toggling.value = false
  }
}

// 编辑别名
const handleEdit = () => {
  editAlias.value = endpoint.value?.alias || ''
  showEditDialog.value = true
}

const handleEditSubmit = async () => {
  if (!endpoint.value) return
  submitting.value = true
  try {
    const res = await updateEndpoint(endpoint.value.id, { alias: editAlias.value })
    if (res.success) {
      ElMessage.success(t('common.updateSuccess'))
      showEditDialog.value = false
      fetchDetail()
    }
  } catch (e) {
    console.error(e)
  } finally {
    submitting.value = false
  }
}

// 注销
const handleRevoke = async () => {
  if (!endpoint.value) return
  try {
    await ElMessageBox.confirm(t('endpoint.revokeConfirm'), t('common.warning'), { type: 'warning' })
    const res = await revokeEndpoint(endpoint.value.id)
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
