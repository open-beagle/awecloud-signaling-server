<template>
  <el-dialog
    v-model="visible"
    :title="t('client.create')"
    width="500px"
    @close="handleClose"
  >
    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-width="100px"
    >
      <el-form-item :label="t('client.clientId')" prop="client_id">
        <el-input
          v-model="form.client_id"
          :placeholder="t('client.clientIdPlaceholder')"
          clearable
        />
        <div class="form-tip">{{ t('client.clientIdTip') }}</div>
      </el-form-item>
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
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { createClient } from '@/api/client'

interface Props {
  modelValue: boolean
}

interface Emits {
  (e: 'update:modelValue', value: boolean): void
  (e: 'success', clientId: string, secret: string): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const { t } = useI18n()

const visible = ref(false)
const loading = ref(false)
const formRef = ref<FormInstance>()

const form = ref({
  client_id: ''
})

const rules = computed<FormRules>(() => ({
  client_id: [
    { required: true, message: t('client.clientIdRequired'), trigger: 'blur' },
    { min: 3, message: t('client.clientIdMinLength'), trigger: 'blur' }
  ]
}))

watch(() => props.modelValue, (val) => {
  visible.value = val
})

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
    if (!valid) return

    loading.value = true
    try {
      const res = await createClient(form.value)
      ElMessage.success(t('common.createSuccess'))
      emit('success', res.client.client_id, res.client_secret)
      handleClose()
    } catch (error: any) {
      ElMessage.error(t('common.failed'))
    } finally {
      loading.value = false
    }
  })
}
</script>

<style scoped>
.form-tip {
  font-size: 12px;
  color: #999;
  margin-top: 5px;
}
</style>
