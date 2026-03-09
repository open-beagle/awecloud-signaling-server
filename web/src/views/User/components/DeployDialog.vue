<template>
  <el-dialog
    v-model="visible"
    :title="getTitle()"
    width="650px"
    @close="handleClose"
  >
    <!-- Agent 部署 -->
    <div v-if="user?.role === 'agent'">
      <el-form :model="agentForm" label-width="100px">
        <el-form-item :label="$t('agent.tokenName')" required>
          <el-input
            v-model="agentForm.name"
            :placeholder="$t('agent.tokenNamePlaceholder')"
          />
          <template #extra>
            <span class="form-item-tip">{{ $t('agent.tokenNameTip') }}</span>
          </template>
        </el-form-item>
      </el-form>

      <div v-if="agentDeployResult" class="deploy-result">
        <el-alert type="warning" :closable="false" show-icon>
          <template #title>
            <span>{{ $t('agent.tokenExpireWarning') }}</span>
          </template>
        </el-alert>

        <div class="command-section">
          <div class="command-label">{{ $t('agent.installCommand') }}:</div>
          <el-input
            v-model="agentDeployResult.install_command"
            type="textarea"
            :rows="6"
            readonly
          />
          <el-button
            type="primary"
            :icon="CopyDocument"
            @click="handleCopyAgent"
            style="margin-top: 10px"
          >
            {{ $t('common.copy') }}
          </el-button>
        </div>
      </div>
    </div>

    <!-- Client Token -->
    <div v-else-if="user?.role === 'client'">
      <el-form :model="clientForm" label-width="100px">
        <el-form-item :label="$t('clientToken.tokenName')" required>
          <el-input
            v-model="clientForm.name"
            :placeholder="$t('clientToken.tokenNamePlaceholder')"
          />
          <template #extra>
            <span class="form-item-tip">{{ $t('clientToken.tokenNameTip') }}</span>
          </template>
        </el-form-item>
      </el-form>

      <div v-if="clientTokenResult" class="deploy-result">
        <el-alert type="warning" :closable="false" show-icon>
          <template #title>
            <span>{{ $t('clientToken.tokenWarning') }}</span>
          </template>
        </el-alert>

        <div class="command-section">
          <div class="command-label">{{ $t('clientToken.envConfig') }}:</div>
          <el-input
            v-model="clientTokenResult.env_config"
            type="textarea"
            :rows="5"
            readonly
          />
          <el-button
            type="primary"
            :icon="CopyDocument"
            @click="handleCopyClient"
            style="margin-top: 10px"
          >
            {{ $t('common.copy') }}
          </el-button>
        </div>
      </div>
    </div>

    <template #footer>
      <el-button @click="visible = false">{{ $t('common.cancel') }}</el-button>
      <el-button
        v-if="hasResult()"
        type="primary"
        @click="handleConfirm"
      >
        {{ $t('common.confirm') }}
      </el-button>
      <el-button
        v-else
        type="primary"
        :loading="generating"
        :disabled="!canGenerate()"
        @click="handleGenerate"
      >
        {{ $t('user.deploy') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { CopyDocument } from '@element-plus/icons-vue'
import {
  createDeployToken,
  type CreateDeployTokenResponse
} from '@/api/deployToken'
import type { User } from '@/api/user'

const { t } = useI18n()

const props = defineProps<{
  modelValue: boolean
  user: User | null
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'success'): void
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (value) => emit('update:modelValue', value)
})

const generating = ref(false)

// Agent 表单和结果
const agentForm = ref({ name: '' })
const agentDeployResult = ref<CreateDeployTokenResponse | null>(null)

// Client 表单和结果
const clientForm = ref({ name: '' })
const clientTokenResult = ref<CreateDeployTokenResponse | null>(null)

// 监听对话框打开
watch(visible, (val) => {
  if (val) {
    agentForm.value = { name: '' }
    clientForm.value = { name: '' }
    agentDeployResult.value = null
    clientTokenResult.value = null
  }
})

const getTitle = () => {
  if (!props.user) return t('user.deploy')
  return `${t('user.deploy')}: ${props.user.name}`
}

const canGenerate = () => {
  if (props.user?.role === 'agent') {
    return !!agentForm.value.name
  } else if (props.user?.role === 'client') {
    return !!clientForm.value.name
  }
  return false
}

const hasResult = () => {
  return !!agentDeployResult.value || !!clientTokenResult.value
}

const handleGenerate = async () => {
  if (!props.user) return

  generating.value = true
  try {
    if (props.user.role === 'agent') {
      await generateAgentDeploy()
    } else if (props.user.role === 'client') {
      await generateClientToken()
    }
  } finally {
    generating.value = false
  }
}

const generateAgentDeploy = async () => {
  if (!props.user || !agentForm.value.name) return

  try {
    const res = await createDeployToken(props.user.name, {
      name: agentForm.value.name
    })
    if (res.success && res.data) {
      agentDeployResult.value = res.data
      ElMessage.success(t('common.success'))
    } else {
      ElMessage.error(res.message || t('common.failed'))
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  }
}

const generateClientToken = async () => {
  if (!props.user || !clientForm.value.name) return

  try {
    const res = await createDeployToken(props.user.name, {
      name: clientForm.value.name
    })
    if (res.success && res.data) {
      clientTokenResult.value = res.data
      ElMessage.success(t('common.success'))
    } else {
      ElMessage.error(res.message || t('common.failed'))
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  }
}

const handleCopyAgent = async () => {
  if (!agentDeployResult.value) return
  try {
    await navigator.clipboard.writeText(agentDeployResult.value.install_command)
    ElMessage.success(t('common.copySuccess'))
  } catch {
    ElMessage.error(t('common.copyFailed'))
  }
}

const handleCopyClient = async () => {
  if (!clientTokenResult.value) return
  try {
    await navigator.clipboard.writeText(clientTokenResult.value.env_config)
    ElMessage.success(t('common.copySuccess'))
  } catch {
    ElMessage.error(t('common.copyFailed'))
  }
}

const handleConfirm = () => {
  visible.value = false
  emit('success')
}

const handleClose = () => {
  agentForm.value = { name: '' }
  clientForm.value = { name: '' }
  agentDeployResult.value = null
  clientTokenResult.value = null
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

.form-item-tip {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>
