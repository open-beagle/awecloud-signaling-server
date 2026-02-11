<template>
  <div class="node-detail" v-loading="loading">
    <template v-if="node">
      <!-- 基本信息 -->
      <el-card shadow="never" class="info-card">
        <template #header>
          <div class="card-header">
            <span>{{ $t('node.basicInfo') }}</span>
            <el-tag :type="node.status === 'online' ? 'success' : 'info'" size="small">
              {{ node.status === 'online' ? $t('common.online') : $t('common.offline') }}
            </el-tag>
          </div>
        </template>
        <el-descriptions :column="2" border label-class-name="desc-label">
          <el-descriptions-item label="ID">{{ node.id }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.name')">{{ node.name }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.type')">
            <el-tag :type="node.type === 'agent' ? 'success' : 'primary'" size="small">
              {{ node.type === 'agent' ? $t('node.typeAgent') : $t('node.typeDesktop') }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="$t('node.user')">
            <router-link v-if="node.user" :to="`/users/${node.user.id}`" class="user-link">
              {{ node.user.name }}
            </router-link>
            <span v-else>-</span>
          </el-descriptions-item>
          <el-descriptions-item :label="$t('node.ip')">{{ node.ip || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.hostname')">{{ node.hostname || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.version')">{{ node.version || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.lastHeartbeat')">
            {{ node.last_heartbeat ? formatTime(node.last_heartbeat) : '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="$t('common.createdAt')">
            {{ formatTime(node.created_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="$t('node.updatedAt')">
            {{ formatTime(node.updated_at) }}
          </el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- Headscale 信息 -->
      <el-card shadow="never" class="info-card">
        <template #header>
          <div class="card-header">
            <span>Headscale {{ $t('node.nodeInfo') }}</span>
            <el-tag v-if="node.headscale" :type="node.headscale.online ? 'success' : 'info'" size="small">
              {{ node.headscale.online ? $t('common.online') : $t('common.offline') }}
            </el-tag>
          </div>
        </template>
        <template v-if="node.headscale">
          <el-descriptions :column="2" border label-class-name="desc-label">
            <el-descriptions-item label="Node ID">{{ node.headscale.id }}</el-descriptions-item>
            <el-descriptions-item :label="$t('node.name')">{{ node.headscale.name }}</el-descriptions-item>
            <el-descriptions-item :label="$t('node.givenName')">{{ node.headscale.given_name }}</el-descriptions-item>
            <el-descriptions-item :label="$t('node.hsUser')">{{ node.headscale.user_name || '-' }}</el-descriptions-item>
            <el-descriptions-item :label="$t('node.ipAddresses')">
              <el-tag v-for="ip in node.headscale.ip_addresses" :key="ip" size="small" class="ip-tag">
                {{ ip }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item :label="$t('node.forcedTags')">
              <el-tag v-for="tag in node.headscale.forced_tags" :key="tag" size="small" type="warning" class="tag-item">
                {{ tag }}
              </el-tag>
              <span v-if="!node.headscale.forced_tags?.length">-</span>
            </el-descriptions-item>
            <el-descriptions-item :label="$t('node.lastSeen')">
              {{ node.headscale.last_seen ? formatTime(node.headscale.last_seen) : '-' }}
            </el-descriptions-item>
            <el-descriptions-item :label="$t('node.expiry')">
              {{ node.headscale.expiry ? formatTime(node.headscale.expiry) : '-' }}
            </el-descriptions-item>
            <el-descriptions-item :label="$t('common.createdAt')">
              {{ node.headscale.created_at ? formatTime(node.headscale.created_at) : '-' }}
            </el-descriptions-item>
            <el-descriptions-item label="">&nbsp;</el-descriptions-item>
          </el-descriptions>
        </template>
        <template v-else>
          <el-empty :description="$t('node.noHeadscaleInfo')">
            <template #image><span></span></template>
          </el-empty>
        </template>
      </el-card>

      <!-- 系统信息 -->
      <el-card v-if="systemInfo" shadow="never" class="info-card">
        <template #header>
          <span>{{ $t('node.systemInfo') }}</span>
        </template>
        <el-descriptions :column="2" border label-class-name="desc-label">
          <el-descriptions-item :label="$t('node.os')">{{ systemInfo.os || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.osVersion')">{{ systemInfo.os_version || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.arch')">{{ systemInfo.arch || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.hostname')">{{ systemInfo.hostname || '-' }}</el-descriptions-item>
          <el-descriptions-item label="CPU">{{ systemInfo.cpu || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.cpuCores')">{{ systemInfo.cpu_cores || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.memory')">
            {{ systemInfo.memory_gb ? `${systemInfo.memory_gb} GB` : '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="">&nbsp;</el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- 能力配置（仅 Agent 类型） -->
      <el-card v-if="node.type === 'agent'" shadow="never" class="info-card" v-loading="capLoading">
        <template #header>
          <div class="card-header">
            <span>{{ $t('capability.title') }}</span>
            <div>
              <el-button type="primary" size="small" @click="saveCapabilities" :loading="capSaving">
                {{ $t('common.save') }}
              </el-button>
              <el-button size="small" @click="resetCapabilities">
                {{ $t('capability.reset') }}
              </el-button>
            </div>
          </div>
        </template>

        <el-form label-width="120px">
          <!-- SSH -->
          <el-divider content-position="left">{{ $t('capability.ssh') }}</el-divider>
          <el-form-item :label="$t('capability.sshEnabled')">
            <el-switch v-model="capForm.ssh_enabled" />
          </el-form-item>

          <!-- K8S API -->
          <el-divider content-position="left">{{ $t('capability.k8s') }}</el-divider>
          <el-form-item :label="$t('capability.k8sEnabled')">
            <el-switch v-model="capForm.k8s_enabled" />
            <el-tag :type="capOriginal.k8s_enabled !== null ? 'primary' : 'info'" size="small" class="source-tag">
              {{ capOriginal.k8s_enabled !== null ? $t('capability.sourceRemote') : $t('capability.sourceLocal') }}
            </el-tag>
          </el-form-item>
          <el-form-item :label="$t('capability.k8sListenPort')">
            <el-input-number v-model="capForm.k8s_listen_port" :min="1" :max="65535"
              :placeholder="$t('capability.k8sListenPortPlaceholder')" controls-position="right" />
          </el-form-item>
          <el-form-item :label="$t('capability.k8sApiServer')">
            <el-input v-model="capForm.k8s_api_server"
              :placeholder="$t('capability.k8sApiServerPlaceholder')" />
          </el-form-item>

          <!-- K8S Service -->
          <el-divider content-position="left">{{ $t('capability.svc') }}</el-divider>
          <el-form-item :label="$t('capability.svcEnabled')">
            <el-switch v-model="capForm.svc_enabled" />
            <el-tag :type="capOriginal.svc_enabled !== null ? 'primary' : 'info'" size="small" class="source-tag">
              {{ capOriginal.svc_enabled !== null ? $t('capability.sourceRemote') : $t('capability.sourceLocal') }}
            </el-tag>
          </el-form-item>
          <el-form-item :label="$t('capability.svcLabelSelector')">
            <el-input v-model="capForm.svc_label_selector"
              :placeholder="$t('capability.svcLabelSelectorPlaceholder')" />
          </el-form-item>
          <el-form-item :label="$t('capability.svcNamespaces')">
            <el-input v-model="capForm.svc_namespaces"
              :placeholder="$t('capability.svcNamespacesPlaceholder')" />
          </el-form-item>
          <el-form-item :label="$t('capability.svcListenPortBase')">
            <el-input-number v-model="capForm.svc_listen_port_base" :min="1" :max="65535"
              :placeholder="$t('capability.svcListenPortBasePlaceholder')" controls-position="right" />
          </el-form-item>
        </el-form>

        <el-alert type="info" :closable="false" show-icon>
          <template #title>{{ $t('capability.titleTip') }}</template>
        </el-alert>
      </el-card>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, reactive, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getNode, getNodeCapabilities, updateNodeCapabilities, resetNodeCapabilities, type NodeDetail, type NodeSystemInfo } from '@/api/node'
import { formatTime } from '@/utils/time'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const route = useRoute()
const loading = ref(false)
const node = ref<NodeDetail | null>(null)

// 能力配置
const capLoading = ref(false)
const capSaving = ref(false)
const capForm = reactive({
  ssh_enabled: false,
  k8s_enabled: false,
  k8s_listen_port: undefined as number | undefined,
  k8s_api_server: '',
  svc_enabled: false,
  svc_label_selector: '',
  svc_namespaces: '',
  svc_listen_port_base: undefined as number | undefined,
})
// 保存原始值用于判断来源（远程/本地）
const capOriginal = reactive({
  k8s_enabled: null as boolean | null,
  svc_enabled: null as boolean | null,
})

// 解析系统信息
const systemInfo = computed<NodeSystemInfo | null>(() => {
  if (!node.value?.system_info) return null
  try {
    return JSON.parse(node.value.system_info)
  } catch {
    return null
  }
})

// 获取设备详情
const fetchNode = async () => {
  const id = Number(route.params.id)
  if (!id) return

  loading.value = true
  try {
    const res = await getNode(id)
    if (res.success && res.data) {
      node.value = res.data
      // Agent 类型设备加载能力配置
      if (res.data.type === 'agent') {
        await fetchCapabilities()
      }
    }
  } catch (error) {
    console.error('获取设备详情失败:', error)
  } finally {
    loading.value = false
  }
}

// 获取能力配置
const fetchCapabilities = async () => {
  const id = Number(route.params.id)
  if (!id) return

  capLoading.value = true
  try {
    const res = await getNodeCapabilities(id)
    if (res.success && res.data) {
      const data = res.data
      capForm.ssh_enabled = data.ssh_enabled
      capForm.k8s_enabled = data.k8s_enabled ?? false
      capForm.k8s_listen_port = data.k8s_listen_port ?? undefined
      capForm.k8s_api_server = data.k8s_api_server || ''
      capForm.svc_enabled = data.svc_enabled ?? false
      capForm.svc_label_selector = data.svc_label_selector || ''
      capForm.svc_namespaces = data.svc_namespaces || ''
      capForm.svc_listen_port_base = data.svc_listen_port_base ?? undefined
      // 记录原始值
      capOriginal.k8s_enabled = data.k8s_enabled
      capOriginal.svc_enabled = data.svc_enabled
    }
  } catch (error) {
    console.error('获取能力配置失败:', error)
  } finally {
    capLoading.value = false
  }
}

// 保存能力配置
const saveCapabilities = async () => {
  const id = Number(route.params.id)
  if (!id) return

  capSaving.value = true
  try {
    const data: Record<string, any> = {
      ssh_enabled: capForm.ssh_enabled,
      k8s_enabled: capForm.k8s_enabled,
      svc_enabled: capForm.svc_enabled,
    }
    if (capForm.k8s_listen_port !== undefined) {
      data.k8s_listen_port = capForm.k8s_listen_port
    }
    if (capForm.k8s_api_server) {
      data.k8s_api_server = capForm.k8s_api_server
    }
    if (capForm.svc_label_selector) {
      data.svc_label_selector = capForm.svc_label_selector
    }
    if (capForm.svc_namespaces) {
      data.svc_namespaces = capForm.svc_namespaces
    }
    if (capForm.svc_listen_port_base !== undefined) {
      data.svc_listen_port_base = capForm.svc_listen_port_base
    }
    const res = await updateNodeCapabilities(id, data)
    if (res.success) {
      ElMessage.success(t('capability.saveSuccess'))
      await fetchCapabilities()
    } else {
      ElMessage.error(res.message || t('capability.saveFailed'))
    }
  } catch (error) {
    ElMessage.error(t('capability.saveFailed'))
  } finally {
    capSaving.value = false
  }
}

// 重置能力配置
const resetCapabilitiesAction = async () => {
  const id = Number(route.params.id)
  if (!id) return

  try {
    const res = await resetNodeCapabilities(id)
    if (res.success) {
      ElMessage.success(t('capability.resetSuccess'))
      await fetchCapabilities()
    }
  } catch (error) {
    ElMessage.error(t('common.operationFailed'))
  }
}

const resetCapabilities = () => {
  ElMessageBox.confirm(t('capability.resetConfirm'), t('common.warning'), {
    confirmButtonText: t('common.confirm'),
    cancelButtonText: t('common.cancel'),
    type: 'warning',
  }).then(() => {
    resetCapabilitiesAction()
  }).catch(() => {})
}

onMounted(() => {
  fetchNode()
})
</script>

<style scoped>
.node-detail {
  width: 100%;
}

.info-card {
  margin-bottom: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.user-link {
  color: var(--el-color-primary);
  text-decoration: none;
}

.user-link:hover {
  text-decoration: underline;
}

.ip-tag {
  margin-right: 8px;
  margin-bottom: 4px;
}

.tag-item {
  margin-right: 8px;
  margin-bottom: 4px;
}

.source-tag {
  margin-left: 8px;
}
</style>

<style>
.node-detail .el-descriptions__body .el-descriptions__table {
  table-layout: fixed;
}

.node-detail .el-descriptions__label {
  width: 100px !important;
  min-width: 100px !important;
  max-width: 100px !important;
}
</style>
