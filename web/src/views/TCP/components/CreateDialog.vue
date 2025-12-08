<template>
  <el-dialog
    v-model="visible"
    :title="t('tcp.create')"
    width="600px"
    @close="handleClose"
  >
    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-width="120px"
    >
      <el-form-item :label="t('tcp.serviceName')" prop="service_name">
        <el-input
          v-model="form.service_name"
          :placeholder="t('tcp.serviceNamePlaceholder')"
        />
      </el-form-item>

      <el-form-item :label="t('tcp.agent')" prop="agent_id">
        <el-select
          v-model="form.agent_id"
          :placeholder="t('tcp.selectAgent')"
          style="width: 100%"
        >
          <el-option
            v-for="agent in agents"
            :key="agent.id"
            :label="`${agent.agent_name} ${agent.status === 'offline' ? '(离线)' : ''}`"
            :value="agent.id"
          />
        </el-select>
        <div v-if="agents.length === 0" class="tip-text">
          {{ t('tcp.noAgent') }}
        </div>
      </el-form-item>

      <el-form-item :label="t('tcp.localIp')" prop="local_ip">
        <el-input
          v-model="form.local_ip"
          :placeholder="t('tcp.localIpPlaceholder')"
        />
      </el-form-item>

      <el-form-item :label="t('tcp.localPort')" prop="local_port">
        <el-input-number
          v-model="form.local_port"
          :min="1"
          :max="65535"
          :placeholder="t('tcp.localPortPlaceholder')"
          style="width: 100%"
        />
      </el-form-item>

      <el-form-item :label="t('tcp.description')">
        <el-input
          v-model="form.description"
          type="textarea"
          :rows="3"
          :placeholder="t('tcp.descriptionPlaceholder')"
        />
      </el-form-item>

      <el-form-item :label="t('tcp.accessControl')" prop="access_control">
        <el-radio-group v-model="form.access_control">
          <el-radio label="public">{{ t('tcp.public') }}</el-radio>
          <el-radio label="whitelist">{{ t('tcp.whitelist') }}</el-radio>
        </el-radio-group>
      </el-form-item>

      <el-form-item
        v-if="form.access_control === 'whitelist'"
        :label="t('tcp.ipWhitelist')"
        prop="ip_whitelist"
      >
        <el-input
          v-model="form.ip_whitelist"
          type="textarea"
          :rows="2"
          :placeholder="t('tcp.ipWhitelistPlaceholder')"
        />
      </el-form-item>

      <el-alert
        type="info"
        :closable="false"
        show-icon
        style="margin-bottom: 20px"
      >
        <template #title>
          <div>端口将自动分配，创建后默认为禁用状态，需手动启用</div>
        </template>
      </el-alert>
    </el-form>

    <template #footer>
      <el-button @click="handleClose">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="loading" @click="handleSubmit">
        {{ t('common.create') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { createTCPService } from '@/api/tcp'
import { getAgents } from '@/api/agent'
import type { Agent } from '@/types/models'

const { t } = useI18n()

interface Props {
  modelValue: boolean
}

interface Emits {
  (e: 'update:modelValue', value: boolean): void
  (e: 'success'): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const visible = computed({
  get: () => props.modelValue,
  set: (value) => emit('update:modelValue', value)
})

const formRef = ref<FormInstance>()
const loading = ref(false)
const agents = ref<Agent[]>([])

const form = ref({
  service_name: '',
  agent_id: undefined as number | undefined,
  local_ip: '127.0.0.1',
  local_port: undefined as number | undefined,
  description: '',
  access_control: 'public',
  ip_whitelist: ''
})

const rules: FormRules = {
  service_name: [
    { required: true, message: t('tcp.serviceNameRequired'), trigger: 'blur' }
  ],
  agent_id: [
    { required: true, message: t('tcp.agentRequired'), trigger: 'change' }
  ],
  local_ip: [
    { required: true, message: t('tcp.localIpRequired'), trigger: 'blur' }
  ],
  local_port: [
    { required: true, message: t('tcp.localPortRequired'), trigger: 'blur' }
  ]
}

// 移除onlineAgents过滤，显示所有Agent（包括离线的）
// const onlineAgents = computed(() => {
//   return agents.value.filter(agent => agent.status === 'online')
// })

const loadAgents = async () => {
  try {
    const res = await getAgents()
    if (res.success && res.data) {
      agents.value = res.data
    }
  } catch (error) {
    console.error('Failed to load agents:', error)
  }
}

const handleSubmit = async () => {
  if (!formRef.value) return

  await formRef.value.validate(async (valid) => {
    if (!valid) return

    loading.value = true
    try {
      const res = await createTCPService({
        service_name: form.value.service_name,
        agent_id: form.value.agent_id!,
        local_ip: form.value.local_ip,
        local_port: form.value.local_port!,
        description: form.value.description,
        access_control: form.value.access_control
      })

      if (res.success) {
        ElMessage.success(res.message || t('common.createSuccess'))
        emit('success')
        handleClose()
      }
    } catch (error: any) {
      ElMessage.error(error.response?.data?.error || t('common.failed'))
    } finally {
      loading.value = false
    }
  })
}

const handleClose = () => {
  formRef.value?.resetFields()
  form.value = {
    service_name: '',
    agent_id: undefined,
    local_ip: '127.0.0.1',
    local_port: undefined,
    description: '',
    access_control: 'public',
    ip_whitelist: ''
  }
  visible.value = false
}

watch(visible, (val) => {
  if (val) {
    loadAgents()
  }
})
</script>

<style scoped>
.tip-text {
  font-size: 12px;
  color: #999;
  margin-top: 4px;
}
</style>
