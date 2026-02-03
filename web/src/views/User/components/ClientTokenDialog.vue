<template>
  <el-dialog
    v-model="visible"
    :title="t('clientToken.title') + ': ' + (user?.name || '')"
    width="650px"
    @close="handleClose"
  >
    <el-form :model="form" label-width="100px">
      <el-form-item :label="t('clientToken.tokenName')" required>
        <el-input
          v-model="form.name"
          :placeholder="t('clientToken.tokenNamePlaceholder')"
        />
      </el-form-item>
      <el-form-item :label="t('clientToken.deviceName')" required>
        <el-input
          v-model="form.deviceName"
          :placeholder="t('clientToken.deviceNamePlaceholder')"
        />
      </el-form-item>
    </el-form>

    <div v-if="tokenResult" class="token-result">
      <el-alert type="warning" :closable="false" show-icon>
        <template #title>
          <span>{{ t('clientToken.tokenWarning') }}</span>
        </template>
      </el-alert>

      <div class="env-section">
        <div class="env-label">{{ t('clientToken.envConfig') }}:</div>
        <el-input
          v-model="tokenResult.env_config"
          type="textarea"
          :rows="5"
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
        v-if="tokenResult"
        type="primary"
        @click="handleConfirm"
      >
        {{ t('common.confirm') }}
      </el-button>
      <el-button
        v-else
        type="primary"
        :loading="generating"
        :disabled="!form.name || !form.deviceName"
        @click="handleGenerate"
      >
        {{ t('clientToken.generateToken') }}
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
  createClientToken,
  type CreateClientTokenResponse
} from '@/api/clientToken'

const { t } = useI18n()

interface User {
  id: number
  name: string
}

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

const form = ref({ name: '', deviceName: '' })
const generating = ref(false)
const tokenResult = ref<CreateClientTokenResponse | null>(null)

// 监听对话框打开
watch(visible, (val) => {
  if (val) {
    form.value = { name: '', deviceName: '' }
    tokenResult.value = null
  }
})

const handleGenerate = async () => {
  if (!props.user || !form.value.name || !form.value.deviceName) return

  generating.value = true
  try {
    const res = await createClientToken({
      user_id: props.user.id,
      name: form.value.name,
      device_name: form.value.deviceName
    })
    if (res.success && res.data) {
      tokenResult.value = res.data
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
  if (!tokenResult.value) return
  try {
    await navigator.clipboard.writeText(tokenResult.value.env_config)
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
  form.value = { name: '', deviceName: '' }
  tokenResult.value = null
}
</script>

<style scoped>
.token-result {
  margin-top: 20px;
}

.env-section {
  margin-top: 15px;
}

.env-label {
  margin-bottom: 8px;
  font-weight: 500;
}
</style>
