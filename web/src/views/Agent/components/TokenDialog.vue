<template>
  <el-dialog
    v-model="visible"
    :title="t('agent.tokenTitle')"
    width="600px"
  >
    <el-alert
      :title="t('agent.tokenTip')"
      type="warning"
      :closable="false"
      style="margin-bottom: 20px"
    />
    
    <el-input
      :model-value="agent?.agent_token"
      readonly
      type="textarea"
      :rows="4"
    >
      <template #append>
        <CopyButton v-if="agent" :text="agent.agent_token" />
      </template>
    </el-input>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.confirm') }}</el-button>
      <el-button type="warning" :loading="loading" @click="handleRegenerate">
        {{ t('agent.regenerateToken') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { regenerateToken } from '@/api/agent'
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
const loading = ref(false)

watch(
  () => props.modelValue,
  (val) => {
    visible.value = val
  }
)

watch(visible, (val) => {
  emit('update:modelValue', val)
})

const handleRegenerate = async () => {
  if (!props.agent) return

  try {
    await ElMessageBox.confirm(t('agent.regenerateConfirm'), {
      type: 'warning'
    })

    loading.value = true
    const res = await regenerateToken(props.agent.id)
    if (res.success && res.data) {
      props.agent.agent_token = res.data.agent_token
      ElMessage.success(t('common.success'))
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.failed'))
    }
  } finally {
    loading.value = false
  }
}
</script>
