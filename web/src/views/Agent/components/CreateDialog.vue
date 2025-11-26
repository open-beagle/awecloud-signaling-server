<template>
  <el-dialog
    v-model="visible"
    :title="t('agent.create')"
    width="500px"
    @close="handleClose"
  >
    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-width="100px"
    >
      <el-form-item :label="t('agent.name')" prop="agent_name">
        <el-input
          v-model="form.agent_name"
          :placeholder="t('agent.namePlaceholder')"
        />
      </el-form-item>
      <el-form-item :label="t('agent.description')">
        <el-input
          v-model="form.description"
          type="textarea"
          :rows="3"
          :placeholder="t('agent.descriptionPlaceholder')"
        />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="handleClose">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="loading" @click="handleSubmit">
        {{ t('common.confirm') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { createAgent } from '@/api/agent'

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

const form = reactive({
  agent_name: '',
  description: ''
})

const rules: FormRules = {
  agent_name: [{ required: true, message: t('agent.nameRequired'), trigger: 'blur' }]
}

watch(
  () => props.modelValue,
  (val) => {
    visible.value = val
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
        const res = await createAgent(form)
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
</script>
