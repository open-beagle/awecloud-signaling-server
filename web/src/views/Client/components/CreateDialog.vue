<template>
  <el-dialog
    v-model="visible"
    title="创建客户"
    width="500px"
    @close="handleClose"
  >
    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-width="80px"
    >
      <el-form-item label="用户名" prop="name">
        <el-input
          v-model="form.name"
          placeholder="请输入用户名"
          clearable
        />
      </el-form-item>
      <el-form-item label="别名">
        <el-input
          v-model="form.alias"
          placeholder="请输入别名（可选）"
          clearable
        />
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
  name: '',
  alias: ''
})

const rules = computed<FormRules>(() => ({
  name: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
    { min: 2, message: '用户名至少2个字符', trigger: 'blur' }
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
      if (res.success && res.data) {
        ElMessage.success(t('common.createSuccess'))
        emit('success', res.data.name, res.data.secret)
        handleClose()
      } else {
        ElMessage.error(res.message || t('common.failed'))
      }
    } catch (error: any) {
      ElMessage.error(error.message || t('common.failed'))
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
