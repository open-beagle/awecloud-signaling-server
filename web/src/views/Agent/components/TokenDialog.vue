<template>
  <el-dialog
    v-model="visible"
    :title="t('agent.tokenTitle')"
    width="600px"
  >
    <div v-loading="loadingToken">
      <el-input
        v-model="token"
        readonly
        type="textarea"
        :rows="4"
      />
      <div style="margin-top: 12px; display: flex; justify-content: flex-end; gap: 8px;">
        <CopyButton v-if="token" :text="token" />
        <el-button type="warning" :icon="Refresh" :loading="regenerating" @click="handleRegenerate">
          {{ t('agent.regenerateToken') }}
        </el-button>
      </div>
    </div>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.confirm') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { getAgentToken, regenerateToken } from '@/api/agent'
import type { Agent } from '@/types/models'
import CopyButton from '@/components/Common/CopyButton.vue'

const props = defineProps<{
  modelValue: boolean
  agent: Agent | null
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

const { t } = useI18n()

const visible = ref(false)
const token = ref('')
const loadingToken = ref(false)
const regenerating = ref(false)

watch(
  () => props.modelValue,
  (val) => {
    visible.value = val
    if (val && props.agent) {
      loadToken()
    }
  }
)

watch(visible, (val) => {
  emit('update:modelValue', val)
  if (!val) {
    token.value = ''
  }
})

const loadToken = async () => {
  if (!props.agent) return
  
  loadingToken.value = true
  try {
    const res = await getAgentToken(props.agent.id)
    if (res.success && res.data) {
      token.value = res.data.agent_token
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  } finally {
    loadingToken.value = false
  }
}

const handleRegenerate = async () => {
  if (!props.agent) return

  try {
    await ElMessageBox.confirm(t('agent.regenerateConfirm'), {
      type: 'warning'
    })

    regenerating.value = true
    const res = await regenerateToken(props.agent.id)
    if (res.success && res.data) {
      token.value = res.data.agent_token
      ElMessage.success(t('common.success'))
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.failed'))
    }
  } finally {
    regenerating.value = false
  }
}
</script>
