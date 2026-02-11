<template>
  <el-dialog v-model="visible" :title="$t('acl.addK8SUserAuth')" width="560px" @close="handleClose">
    <el-form ref="formRef" :model="form" :rules="rules" label-width="120px">
      <el-form-item :label="$t('acl.selectUser')" prop="userIds">
        <el-select v-model="form.userIds" multiple filterable :placeholder="$t('acl.selectUserPlaceholder')" style="width: 100%" :loading="loadingUsers">
          <el-option v-for="user in availableUsers" :key="user.id" :label="`${user.name}${user.alias ? ' (' + user.alias + ')' : ''}`" :value="user.id" />
        </el-select>
      </el-form-item>
      <el-form-item :label="$t('acl.k8sGroups')" prop="k8sGroups">
        <el-select v-model="form.k8sGroups" multiple filterable allow-create default-first-option :placeholder="$t('acl.k8sGroupsPlaceholder')" style="width: 100%">
          <el-option label="system:masters" value="system:masters" />
          <el-option label="system:authenticated" value="system:authenticated" />
        </el-select>
        <div class="form-tip">{{ $t('acl.k8sGroupsTip') }}</div>
      </el-form-item>
      <el-form-item :label="$t('acl.namespaces')" prop="namespaces">
        <el-select v-model="form.namespaces" multiple filterable allow-create default-first-option :placeholder="$t('acl.namespacesPlaceholder')" style="width: 100%">
          <el-option label="* (全部)" value="*" />
          <el-option label="default" value="default" />
          <el-option label="kube-system" value="kube-system" />
        </el-select>
        <div class="form-tip">{{ $t('acl.namespacesTip') }}</div>
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
import { addK8SACLUsers } from '@/api/aclK8s'

const props = defineProps<{ modelValue: boolean; agentId: number }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: boolean): void; (e: 'success'): void }>()
const { t } = useI18n()

const visible = computed({ get: () => props.modelValue, set: (val) => emit('update:modelValue', val) })
const formRef = ref<FormInstance>()
const loadingUsers = ref(false)
const submitting = ref(false)
const users = ref<User[]>([])

const form = reactive({ userIds: [] as number[], k8sGroups: ['system:masters'] as string[], namespaces: ['*'] as string[] })
const rules: FormRules = {
  userIds: [{ required: true, message: t('acl.selectUserRequired'), trigger: 'change' }],
  k8sGroups: [{ required: true, message: t('acl.k8sGroupsRequired'), trigger: 'change' }]
}

const availableUsers = computed(() => users.value.filter(u => u.role === 'client'))

const fetchUsers = async () => {
  loadingUsers.value = true
  try {
    const res = await getUsers({ role: 'client', size: 1000 })
    if (res.success && res.data) { users.value = res.data }
  } finally { loadingUsers.value = false }
}

const handleSubmit = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      const res = await addK8SACLUsers(props.agentId, { user_ids: form.userIds, k8s_groups: form.k8sGroups, namespaces: form.namespaces })
      if (res?.success) { ElMessage.success(t('acl.authSuccess')); emit('success'); handleClose() }
      else { ElMessage.error(res?.message || t('acl.authFailed')) }
    } catch { ElMessage.error(t('acl.authFailed')) }
    finally { submitting.value = false }
  })
}

const handleClose = () => { form.userIds = []; form.k8sGroups = ['system:masters']; form.namespaces = ['*']; formRef.value?.resetFields(); visible.value = false }
watch(visible, (val) => { if (val) fetchUsers() })
</script>

<style scoped>
.form-tip { font-size: 12px; color: #909399; margin-top: 4px; }
</style>
