<template>
  <el-dialog
    v-model="visible"
    :title="t('stcp.create')"
    width="500px"
    @close="handleClose"
  >
    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-width="100px"
    >
      <el-form-item :label="t('stcp.agent')" prop="agent_id">
        <el-select
          v-model="form.agent_id"
          :placeholder="t('stcp.selectAgent')"
          style="width: 100%"
        >
          <el-option
            v-for="agent in onlineAgents"
            :key="agent.id"
            :label="agent.agent_name"
            :value="agent.id"
          />
        </el-select>
        <div v-if="onlineAgents.length === 0" class="no-agent-tip">
          {{ t('stcp.noOnlineAgent') }}
        </div>
      </el-form-item>
      <el-form-item :label="t('stcp.instanceName')" prop="instance_name">
        <el-input
          v-model="form.instance_name"
          :placeholder="t('stcp.instanceNamePlaceholder')"
        />
      </el-form-item>
      <el-form-item :label="t('stcp.localIp')" prop="local_ip">
        <el-input
          v-model="form.local_ip"
          :placeholder="t('stcp.localIpPlaceholder')"
        />
      </el-form-item>
      <el-form-item :label="t('stcp.localPort')" prop="local_port">
        <el-input-number
          v-model="form.local_port"
          :min="1"
          :max="65535"
          :placeholder="t('stcp.localPortPlaceholder')"
          style="width: 100%"
        />
      </el-form-item>
      <el-form-item :label="t('agent.description')">
        <el-input
          v-model="form.description"
          type="textarea"
          :rows="3"
          :placeholder="t('stcp.descriptionPlaceholder')"
        />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="handleClose">{{ t('common.cancel') }}</el-button>
      <el-button
        type="primary"
        :loading="loading"
        :disabled="onlineAgents.length === 0"
        @click="handleSubmit"
      >
        {{ t('common.confirm') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, reactive, watch, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { createSTCPInstance } from '@/api/stcp'
import { getAgents } from '@/api/agent'
import type { Agent } from '@/types/models'

const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'success'): void
}>()

const { t } = useI18n()

const visible = ref(false)
const loading = ref(false)
const formRef = ref<FormInstance>()
const agents = ref<Agent[]>([])

const form = reactive({
  agent_id: 0,
  instance_name: '',
  local_ip: '',
  local_port: 0,
  description: ''
})

const rules: FormRules = {
  agent_id: [{ required: true, message: t('stcp.agentRequired'), trigger: 'change' }],
  instance_name: [{ required: true, message: t('stcp.instanceNameRequired'), trigger: 'blur' }],
  local_ip: [{ required: true, message: t('stcp.localIpRequired'), trigger: 'blur' }],
  local_port: [{ required: true, message: t('stcp.localPortRequired'), trigger: 'blur' }]
}

const onlineAgents = computed(() => {
  return agents.value.filter((agent) => agent.status === 'online')
})

const loadAgents = async () => {
  try {
    const res = await getAgents()
    if (res.success && res.data) {
      agents.value = res.data
    }
  } catch (error) {
    // ignore
  }
}

watch(
  () => props.modelValue,
  (val) => {
    visible.value = val
    if (val) {
      loadAgents()
    }
  }
)

watch(visible, (val) => {
  emit('update:modelValue', val)
})

const handleClose = () => {
  formRef.value?.resetFields()
  visible.value = false
}

const handleSubmit = async () => {
  if (!formRef.value) return

  await formRef.value.validate(async (valid) => {
    if (valid) {
      loading.value = true
      try {
        const res = await createSTCPInstance(form)
        if (res.success) {
          ElMessage.success(t('common.createSuccess'))
          emit('success')
          handleClose()
        }
      } catch (error) {
        ElMessage.error(t('common.failed'))
      } finally {
        loading.value = false
      }
    }
  })
}

onMounted(() => {
  loadAgents()
})
</script>

<style scoped>
.no-agent-tip {
  margin-top: 5px;
  font-size: 12px;
  color: var(--warning-color);
}
</style>
