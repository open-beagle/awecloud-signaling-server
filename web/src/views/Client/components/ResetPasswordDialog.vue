<template>
  <el-dialog
    v-model="visible"
    title="重置密码"
    width="500px"
    @close="handleClose"
  >
    <div class="account-info">
      <span class="label">账号:</span>
      <span class="value">{{ clientName }}</span>
    </div>

    <el-form ref="formRef" :model="form" :rules="rules" label-width="80px">
      <el-form-item label="新密码" prop="password">
        <el-input
          v-model="form.password"
          placeholder="请输入新密码"
          show-password
        />
      </el-form-item>
    </el-form>

    <div class="generate-btn">
      <el-button type="primary" link @click="generatePassword">
        自动生成
      </el-button>
    </div>

    <div class="password-hint">
      <el-icon><InfoFilled /></el-icon>
      <span>密码要求：64位十六进制字符串</span>
    </div>

    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" :loading="loading" @click="handleSubmit">
        确定
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { InfoFilled } from '@element-plus/icons-vue'
import type { FormInstance, FormRules } from 'element-plus'
import { resetPassword } from '@/api/client'

interface Props {
  modelValue: boolean
  clientId: number
  clientName: string
}

interface Emits {
  (e: 'update:modelValue', value: boolean): void
  (e: 'success', secret: string): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const visible = ref(false)
const loading = ref(false)
const formRef = ref<FormInstance>()

const form = reactive({
  password: ''
})

// 密码验证：64位十六进制字符串
const validatePassword = (_rule: any, value: string, callback: any) => {
  if (!value) {
    callback(new Error('请输入新密码'))
    return
  }
  if (value.length !== 64) {
    callback(new Error('密码需要64位'))
    return
  }
  if (!/^[0-9a-fA-F]+$/.test(value)) {
    callback(new Error('密码只能包含十六进制字符(0-9, a-f)'))
    return
  }
  callback()
}

const rules: FormRules = {
  password: [{ validator: validatePassword, trigger: 'blur' }]
}

watch(() => props.modelValue, (val) => {
  visible.value = val
  if (val) {
    form.password = ''
  }
})

watch(visible, (val) => {
  emit('update:modelValue', val)
})

// 生成64位随机密码（十六进制字符）
const generatePassword = () => {
  const chars = '0123456789abcdef'
  let password = ''
  for (let i = 0; i < 64; i++) {
    password += chars.charAt(Math.floor(Math.random() * chars.length))
  }
  form.password = password
}

const handleSubmit = async () => {
  if (!formRef.value) return
  
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  loading.value = true
  try {
    const res = await resetPassword(props.clientId, form.password)
    if (res.success && res.data) {
      ElMessage.success('密码重置成功')
      visible.value = false
      emit('success', res.data.secret)
    } else {
      ElMessage.error(res.message || '重置失败')
    }
  } catch (error) {
    ElMessage.error('重置失败')
  } finally {
    loading.value = false
  }
}

const handleClose = () => {
  form.password = ''
  formRef.value?.resetFields()
}
</script>

<style scoped>
.account-info {
  margin-bottom: 20px;
  padding: 12px 16px;
  background: var(--el-fill-color-lighter);
  border-radius: 6px;
}

.account-info .label {
  color: var(--el-text-color-regular);
  margin-right: 8px;
}

.account-info .value {
  font-weight: 500;
  color: var(--el-text-color-primary);
}

.generate-btn {
  text-align: right;
  margin-top: -8px;
  margin-bottom: 12px;
}

.password-hint {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
</style>
