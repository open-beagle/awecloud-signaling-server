<template>
  <el-dialog
    v-model="visible"
    title="编辑TCP服务"
    width="600px"
    @close="handleClose"
  >
    <el-form
      ref="formRef"
      :model="form"
      label-width="120px"
    >
      <el-form-item :label="t('tcp.serviceName')">
        <el-input v-model="form.service_name" disabled />
      </el-form-item>

      <el-form-item :label="t('tcp.remotePort')">
        <el-tag type="success">{{ form.remote_port }}</el-tag>
        <span style="margin-left: 10px; color: #999; font-size: 12px">
          (端口不可修改)
        </span>
      </el-form-item>

      <el-form-item :label="t('tcp.description')">
        <el-input
          v-model="form.description"
          type="textarea"
          :rows="3"
          :placeholder="t('tcp.descriptionPlaceholder')"
        />
      </el-form-item>

      <el-form-item :label="t('tcp.accessControl')">
        <el-radio-group v-model="form.access_control">
          <el-radio label="public">{{ t('tcp.public') }}</el-radio>
          <el-radio label="whitelist">{{ t('tcp.whitelist') }}</el-radio>
        </el-radio-group>
      </el-form-item>

      <el-form-item
        v-if="form.access_control === 'whitelist'"
        :label="t('tcp.ipWhitelist')"
      >
        <el-input
          v-model="form.ip_whitelist"
          type="textarea"
          :rows="2"
          :placeholder="t('tcp.ipWhitelistPlaceholder')"
        />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="handleClose">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="loading" @click="handleSubmit">
        {{ t('common.save') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import type { FormInstance } from 'element-plus'
import { updateTCPService } from '@/api/tcp'
import type { TCPService } from '@/types/models'

const { t } = useI18n()

interface Props {
  modelValue: boolean
  service: TCPService | null
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

const form = ref({
  service_name: '',
  remote_port: 0,
  description: '',
  access_control: 'public',
  ip_whitelist: ''
})

const handleSubmit = async () => {
  if (!props.service) return

  loading.value = true
  try {
    const res = await updateTCPService(props.service.id, {
      description: form.value.description,
      access_control: form.value.access_control,
      ip_whitelist: form.value.ip_whitelist
    })

    if (res.success) {
      ElMessage.success(t('common.success'))
      emit('success')
      handleClose()
    }
  } catch (error: any) {
    ElMessage.error(error.response?.data?.error || t('common.failed'))
  } finally {
    loading.value = false
  }
}

const handleClose = () => {
  visible.value = false
}

watch(() => props.service, (service) => {
  if (service) {
    form.value = {
      service_name: service.service_name,
      remote_port: service.remote_port,
      description: service.description || '',
      access_control: service.access_control || 'public',
      ip_whitelist: service.ip_whitelist || ''
    }
  }
}, { immediate: true })
</script>
