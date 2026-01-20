<template>
  <el-dialog v-model="visible" :title="$t('acl.addUserAuth')" width="500px" @close="handleClose">
    <el-form ref="formRef" :model="form" :rules="rules" label-width="80px">
      <el-form-item :label="$t('acl.selectUser')" prop="userIds">
        <el-select
          v-model="form.userIds"
          multiple
          filterable
          :placeholder="$t('acl.selectUserPlaceholder')"
          style="width: 100%"
          :loading="loadingUsers"
        >
          <el-option
            v-for="user in availableUsers"
            :key="user.id"
            :label="`${user.name}${user.alias ? ' (' + user.alias + ')' : ''}`"
            :value="user.id"
          />
        </el-select>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="handleClose">{{ $t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">{{ $t('acl.authorize') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, reactive, watch, computed } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { getUsers, type User } from '@/api/user'
import { addServiceACLUsers, addUserACLUsers, addGroupACLUsers } from '@/api/acl'

const props = defineProps<{
  modelValue: boolean
  serviceId?: string
  type?: 'service' | 'user' | 'group'
  targetId?: number
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
const loadingUsers = ref(false)
const submitting = ref(false)
const users = ref<User[]>([])

const form = reactive({
  userIds: [] as number[]
})

const rules: FormRules = {
  userIds: [{ required: true, message: t('acl.selectUserRequired'), trigger: 'change' }]
}

// 可选用户（排除已授权的）
const availableUsers = computed(() => {
  return users.value
})

// 获取用户列表
const fetchUsers = async () => {
  loadingUsers.value = true
  try {
    const res = await getUsers({ size: 1000 })
    if (res.success && res.data) {
      users.value = res.data
    }
  } catch (error) {
    console.error('获取用户列表失败:', error)
  } finally {
    loadingUsers.value = false
  }
}

// 提交
const handleSubmit = async () => {
  if (!formRef.value) return
  
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    
    submitting.value = true
    try {
      let res
      if (props.serviceId) {
        res = await addServiceACLUsers(props.serviceId, form.userIds)
      } else if (props.type === 'user' && props.targetId) {
        res = await addUserACLUsers(props.targetId, form.userIds)
      } else if (props.type === 'group' && props.targetId) {
        res = await addGroupACLUsers(props.targetId, form.userIds)
      }
      
      if (res?.success) {
        ElMessage.success(t('acl.authSuccess'))
        emit('success')
        handleClose()
      } else {
        ElMessage.error(res?.message || t('acl.authFailed'))
      }
    } catch (error) {
      console.error('授权失败:', error)
      ElMessage.error(t('acl.authFailed'))
    } finally {
      submitting.value = false
    }
  })
}

// 关闭
const handleClose = () => {
  form.userIds = []
  formRef.value?.resetFields()
  visible.value = false
}

// 监听弹窗打开
watch(visible, (val) => {
  if (val) {
    fetchUsers()
  }
})
</script>
