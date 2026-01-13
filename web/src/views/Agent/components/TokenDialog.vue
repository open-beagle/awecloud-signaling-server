<template>
  <el-dialog
    v-model="visible"
    :title="t('agent.tokenTitle')"
    width="600px"
  >
    <div>
      <el-alert
        v-if="!secret"
        type="info"
        :closable="false"
        show-icon
      >
        {{ t('agent.tokenTip') }}
      </el-alert>
      
      <el-input
        v-if="secret"
        v-model="secret"
        readonly
        type="textarea"
        :rows="4"
        style="margin-top: 12px;"
      />
      
      <div v-if="!secret" style="margin-top: 12px; color: #909399; text-align: center;">
        {{ t('agent.secretHidden') }}
      </div>
      
      <div style="margin-top: 12px; display: flex; justify-content: flex-end; gap: 8px;">
        <CopyButton v-if="secret" :text="secret" />
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
import { regenerateSecret } from '@/api/agent'
import type { Agent } from '@/types/models'
import CopyButton from '@/components/Common/CopyButton.vue'

const props = defineProps<{
  modelValue: boolean
  agent: Agent | null
  initialSecret?: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

const { t } = useI18n()

const visible = ref(false)
const secret = ref('')
const regenerating = ref(false)

watch(
  () => props.modelValue,
  (val) => {
    visible.value = val
    if (val && props.initialSecret) {
      secret.value = props.initialSecret
    }
  }
)

watch(visible, (val) => {
  emit('update:modelValue', val)
  if (!val) {
    secret.value = ''
  }
})

const handleRegenerate = async () => {
  if (!props.agent) return

  try {
    await ElMessageBox.confirm(t('agent.regenerateConfirm'), {
      type: 'warning'
    })

    regenerating.value = true
    const res = await regenerateSecret(props.agent.id)
    if (res.success && res.data) {
      secret.value = res.data.secret
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
