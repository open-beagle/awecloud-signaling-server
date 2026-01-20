<template>
  <el-dialog
    v-model="visible"
    :title="$t('user.create')"
    width="500px"
    :close-on-click-modal="false"
    @close="handleClose"
  >
    <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
      <el-form-item :label="$t('user.name')" prop="name">
        <el-input v-model="form.name" :placeholder="$t('user.namePlaceholder')" />
      </el-form-item>
      <el-form-item :label="$t('user.alias')" prop="alias">
        <el-input v-model="form.alias" :placeholder="$t('user.aliasPlaceholder')" />
      </el-form-item>
      <el-form-item :label="$t('user.role')" prop="role">
        <el-radio-group v-model="form.role">
          <el-radio value="agent">{{ $t('user.roleAgent') }}</el-radio>
          <el-radio value="client">{{ $t('user.roleClient') }}</el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item v-if="form.role === 'agent'" :label="$t('user.sshEnabled')" prop="ssh_enabled">
        <el-switch v-model="form.ssh_enabled" />
      </el-form-item>
    </el-form>

    <!-- 创建成功后显示密钥 -->
    <div v-if="createdSecret" class="secret-display">
      <el-alert type="warning" :closable="false" show-icon>
        <template #title>
          {{ $t('user.secretWarning') }}
        </template>
      </el-alert>
      <div class="secret-box">
        <span class="secret-label">{{ $t('user.secret') }}:</span>
        <el-input v-model="createdSecret" readonly>
          <template #append>
            <el-button @click="copySecret">{{ $t('common.copy') }}</el-button>
          </template>
        </el-input>
      </div>
    </div>

    <template #footer>
      <el-button @click="handleClose">{{ createdSecret ? $t('common.close') : $t('common.cancel') }}</el-button>
      <el-button v-if="!createdSecret" type="primary" :loading="submitting" @click="handleSubmit">
        {{ $t('common.confirm') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import type { FormInstance, FormRules } from 'element-plus'
import { createUser, type CreateUserRequest } from '@/api/user'

const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'success'): void
}>()

const { t } = useI18n()

const visible = computed({
  get: () => props.modelValue,
  set: (val) => emit('update:modelValue', val)
})

const formRef = ref<FormInstance>()
const submitting = ref(false)
const createdSecret = ref('')

const form = reactive<CreateUserRequest>({
  name: '',
  alias: '',
  role: 'agent',
  ssh_enabled: false
})

const rules: FormRules = {
  name: [
    { required: true, message: () => t('user.nameRequired'), trigger: 'blur' },
    { pattern: /^[a-zA-Z][a-zA-Z0-9_-]*$/, message: () => t('user.namePattern'), trigger: 'blur' }
  ],
  role: [
    { required: true, message: () => t('user.roleRequired'), trigger: 'change' }
  ]
}

// 重置表单
const resetForm = () => {
  form.name = ''
  form.alias = ''
  form.role = 'agent'
  form.ssh_enabled = false
  createdSecret.value = ''
  formRef.value?.resetFields()
}

// 关闭弹窗
const handleClose = () => {
  if (createdSecret.value) {
    emit('success')
  }
  resetForm()
  visible.value = false
}

// 提交
const handleSubmit = async () => {
  if (!formRef.value) return
  
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    
    submitting.value = true
    try {
      const res = await createUser(form)
      if (res.success && res.data) {
        ElMessage.success(t('common.createSuccess'))
        createdSecret.value = res.data.secret
      } else {
        ElMessage.error(res.message || t('common.createFailed'))
      }
    } catch (error) {
      console.error('创建用户失败:', error)
      ElMessage.error(t('common.createFailed'))
    } finally {
      submitting.value = false
    }
  })
}

// 复制密钥
const copySecret = async () => {
  try {
    await navigator.clipboard.writeText(createdSecret.value)
    ElMessage.success(t('common.copySuccess'))
  } catch (error) {
    ElMessage.error(t('common.copyFailed'))
  }
}

// 监听弹窗关闭
watch(visible, (val) => {
  if (!val) {
    resetForm()
  }
})
</script>

<style scoped>
.secret-display {
  margin-top: 20px;
}

.secret-box {
  margin-top: 15px;
  display: flex;
  align-items: center;
  gap: 10px;
}

.secret-label {
  white-space: nowrap;
  font-weight: bold;
}
</style>
