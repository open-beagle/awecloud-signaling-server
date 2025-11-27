<template>
  <el-dialog
    v-model="visible"
    title="创建 Client"
    width="500px"
    @close="handleClose"
  >
    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-width="100px"
    >
      <el-form-item label="Client ID" prop="client_id">
        <el-input
          v-model="form.client_id"
          placeholder="请输入用户名或邮箱"
          clearable
        />
        <div class="form-tip">用于Desktop登录的用户标识</div>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="handleClose">取消</el-button>
      <el-button type="primary" :loading="loading" @click="handleSubmit">
        创建
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
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

const visible = ref(false)
const loading = ref(false)
const formRef = ref<FormInstance>()

const form = ref({
  client_id: ''
})

const rules: FormRules = {
  client_id: [
    { required: true, message: '请输入Client ID', trigger: 'blur' },
    { min: 3, message: 'Client ID至少3个字符', trigger: 'blur' }
  ]
}

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
      ElMessage.success('创建成功')
      emit('success', res.client.client_id, res.client_secret)
      handleClose()
    } catch (error: any) {
      ElMessage.error(error.message || '创建失败')
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
