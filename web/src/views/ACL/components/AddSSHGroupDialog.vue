<template>
  <el-dialog v-model="visible" :title="$t('acl.addSSHGroupAuth')" width="500px" @close="handleClose">
    <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
      <el-form-item :label="$t('acl.selectGroup')" prop="groupIds">
        <el-select
          v-model="form.groupIds"
          multiple
          filterable
          :placeholder="$t('acl.selectGroupPlaceholder')"
          style="width: 100%"
          :loading="loadingGroups"
        >
          <el-option
            v-for="group in availableGroups"
            :key="group.id"
            :label="`${group.name}${group.description ? ' (' + group.description + ')' : ''}`"
            :value="group.id"
          />
        </el-select>
      </el-form-item>
      <el-form-item :label="$t('acl.sshUsers')" prop="sshUsers">
        <el-select
          v-model="form.sshUsers"
          multiple
          filterable
          allow-create
          default-first-option
          :placeholder="$t('acl.sshUsersPlaceholder')"
          style="width: 100%"
        >
          <el-option label="root" value="root" />
          <el-option label="autogroup:nonroot" value="autogroup:nonroot" />
        </el-select>
        <div class="form-tip">{{ $t('acl.sshUsersTip') }}</div>
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
import { getGroups, type Group } from '@/api/group'
import { addSSHACLGroups } from '@/api/acl'

const props = defineProps<{
  modelValue: boolean
  agentId: number
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
const loadingGroups = ref(false)
const submitting = ref(false)
const groups = ref<Group[]>([])

const form = reactive({
  groupIds: [] as number[],
  sshUsers: ['root'] as string[]
})

const rules: FormRules = {
  groupIds: [{ required: true, message: t('acl.selectGroupRequired'), trigger: 'change' }],
  sshUsers: [{ required: true, message: t('acl.sshUsersRequired'), trigger: 'change' }]
}

// 可选分组
const availableGroups = computed(() => {
  return groups.value
})

// 获取分组列表
const fetchGroups = async () => {
  loadingGroups.value = true
  try {
    const res = await getGroups({ size: 1000 })
    if (res.success && res.data) {
      groups.value = res.data
    }
  } catch (error) {
    console.error('获取分组列表失败:', error)
  } finally {
    loadingGroups.value = false
  }
}

// 提交
const handleSubmit = async () => {
  if (!formRef.value) return
  
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    
    submitting.value = true
    try {
      const res = await addSSHACLGroups(props.agentId, form.groupIds, form.sshUsers)
      
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
  form.groupIds = []
  form.sshUsers = ['root']
  formRef.value?.resetFields()
  visible.value = false
}

// 监听弹窗打开
watch(visible, (val) => {
  if (val) {
    fetchGroups()
  }
})
</script>

<style scoped>
.form-tip {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
}
</style>
