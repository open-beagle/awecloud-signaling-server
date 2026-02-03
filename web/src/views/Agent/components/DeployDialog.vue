<template>
  <el-dialog
    v-model="visible"
    :title="t('agent.deploy') + ': ' + (agent?.name || '')"
    width="650px"
    @close="handleClose"
  >
    <el-tabs v-model="activeTab">
      <!-- 生成部署命令 -->
      <el-tab-pane :label="t('agent.generateCommand')" name="generate">
        <el-form :model="form" label-width="100px">
          <el-form-item :label="t('agent.deviceName')" required>
            <el-input
              v-model="form.deviceName"
              :placeholder="t('agent.deviceNamePlaceholder')"
            />
          </el-form-item>
        </el-form>

        <div v-if="deployResult" class="deploy-result">
          <el-alert type="warning" :closable="false" show-icon>
            <template #title>
              <span>{{ t('agent.tokenExpireWarning') }}</span>
            </template>
          </el-alert>

          <div class="command-section">
            <div class="command-label">{{ t('agent.installCommand') }}:</div>
            <el-input
              v-model="deployResult.install_command"
              type="textarea"
              :rows="6"
              readonly
            />
            <el-button
              type="primary"
              :icon="CopyDocument"
              @click="handleCopy"
              style="margin-top: 10px"
            >
              {{ t('common.copy') }}
            </el-button>
          </div>
        </div>

        <template #footer>
          <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
          <el-button
            type="primary"
            :loading="generating"
            :disabled="!form.deviceName"
            @click="handleGenerate"
          >
            {{ t('agent.generateCommand') }}
          </el-button>
        </template>
      </el-tab-pane>

      <!-- 部署历史 -->
      <el-tab-pane :label="t('agent.deployHistory')" name="history">
        <el-table v-loading="historyLoading" :data="deployTokens" stripe size="small">
          <el-table-column :label="t('common.status')" width="80">
            <template #default="{ row }">
              <el-tag :type="getStatusType(row.status)" size="small">
                {{ getStatusText(row.status) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="device_name" :label="t('agent.deviceName')" min-width="120" />
          <el-table-column prop="created_by_name" :label="t('common.createdBy')" width="100" />
          <el-table-column :label="t('common.createdAt')" width="100">
            <template #default="{ row }">
              {{ formatDate(row.created_at) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('agent.boundAt')" width="100">
            <template #default="{ row }">
              {{ row.bound_at ? formatDate(row.bound_at) : '-' }}
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="120" fixed="right">
            <template #default="{ row }">
              <el-button
                v-if="row.status === 'pending'"
                size="small"
                :icon="CopyDocument"
                @click="handleCopyCommand(row)"
              >
                {{ t('common.copy') }}
              </el-button>
              <el-button
                v-if="row.status === 'pending'"
                size="small"
                type="danger"
                @click="handleRevoke(row)"
              >
                {{ t('common.revoke') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>
    </el-tabs>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CopyDocument } from '@element-plus/icons-vue'
import {
  createDeployToken,
  getDeployTokens,
  getDeployCommand,
  revokeDeployToken,
  type DeployToken,
  type CreateDeployTokenResponse
} from '@/api/agentDeploy'
import type { Agent } from '@/types/models'

const { t } = useI18n()

const props = defineProps<{
  modelValue: boolean
  agent: Agent | null
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (value) => emit('update:modelValue', value)
})

const activeTab = ref('generate')
const form = ref({ deviceName: '' })
const generating = ref(false)
const deployResult = ref<CreateDeployTokenResponse | null>(null)

// 历史记录
const historyLoading = ref(false)
const deployTokens = ref<DeployToken[]>([])

// 监听对话框打开
watch(visible, (val) => {
  if (val) {
    form.value.deviceName = ''
    deployResult.value = null
    if (activeTab.value === 'history') {
      loadHistory()
    }
  }
})

// 监听 tab 切换
watch(activeTab, (val) => {
  if (val === 'history' && props.agent) {
    loadHistory()
  }
})

const loadHistory = async () => {
  if (!props.agent) return
  historyLoading.value = true
  try {
    const res = await getDeployTokens(props.agent.id)
    if (res.success && res.data) {
      deployTokens.value = res.data
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  } finally {
    historyLoading.value = false
  }
}

const handleGenerate = async () => {
  if (!props.agent || !form.value.deviceName) return

  generating.value = true
  try {
    const res = await createDeployToken(props.agent.id, {
      device_name: form.value.deviceName
    })
    if (res.success && res.data) {
      deployResult.value = res.data
      ElMessage.success(t('common.success'))
    } else {
      ElMessage.error(res.message || t('common.failed'))
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  } finally {
    generating.value = false
  }
}

const handleCopy = async () => {
  if (!deployResult.value) return
  try {
    await navigator.clipboard.writeText(deployResult.value.install_command)
    ElMessage.success(t('common.copySuccess'))
  } catch {
    ElMessage.error(t('common.copyFailed'))
  }
}

const handleCopyCommand = async (token: DeployToken) => {
  try {
    const res = await getDeployCommand(token.id)
    if (res.success && res.data) {
      await navigator.clipboard.writeText(res.data.install_command)
      ElMessage.success(t('common.copySuccess'))
    }
  } catch {
    ElMessage.error(t('common.copyFailed'))
  }
}

const handleRevoke = async (token: DeployToken) => {
  try {
    await ElMessageBox.confirm(t('agent.revokeConfirm'), { type: 'warning' })
    const res = await revokeDeployToken(token.id)
    if (res.success) {
      ElMessage.success(t('common.success'))
      loadHistory()
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.failed'))
    }
  }
}

const handleClose = () => {
  form.value.deviceName = ''
  deployResult.value = null
}

const getStatusType = (status: string) => {
  switch (status) {
    case 'pending': return 'warning'
    case 'bound': return 'success'
    case 'expired': return 'info'
    default: return 'info'
  }
}

const getStatusText = (status: string) => {
  switch (status) {
    case 'pending': return t('agent.statusPending')
    case 'bound': return t('agent.statusBound')
    case 'expired': return t('agent.statusExpired')
    default: return status
  }
}

const formatDate = (dateStr: string) => {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  return `${date.getMonth() + 1}-${date.getDate()}`
}
</script>

<style scoped>
.deploy-result {
  margin-top: 20px;
}

.command-section {
  margin-top: 15px;
}

.command-label {
  margin-bottom: 8px;
  font-weight: 500;
}
</style>
