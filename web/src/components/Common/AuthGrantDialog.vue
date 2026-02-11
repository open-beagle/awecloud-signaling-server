<template>
  <el-dialog v-model="visible" :title="title" :width="width" @close="handleClose">
    <el-form ref="formRef" :model="form" :rules="rules" :label-width="labelWidth">
      <!-- 用户/分组选择 -->
      <el-form-item :label="mode === 'user' ? $t('acl.selectUser') : $t('acl.selectGroup')" prop="selectedIds">
        <el-select
          v-model="form.selectedIds"
          multiple
          filterable
          :placeholder="mode === 'user' ? $t('acl.selectUserPlaceholder') : $t('acl.selectGroupPlaceholder')"
          style="width: 100%"
          :loading="loadingOptions"
        >
          <el-option
            v-for="item in filteredOptions"
            :key="item.id"
            :label="item.label"
            :value="item.id"
          />
        </el-select>
      </el-form-item>

      <!-- 额外参数区域（由父组件通过 slot 传入） -->
      <slot name="extra" :form="form" />
    </el-form>

    <template #footer>
      <el-button @click="handleClose">{{ $t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">{{ $t('acl.authorize') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { getUsers, type User } from '@/api/user'
import { getGroups, type Group } from '@/api/group'

export interface SelectOption {
  id: number
  label: string
}

const props = withDefaults(defineProps<{
  modelValue: boolean
  title: string
  mode: 'user' | 'group'
  width?: string
  labelWidth?: string
  /** 排除已授权的 ID（不在选项中显示） */
  excludeIds?: number[]
  /** 仅用户模式：是否只显示 client 角色 */
  clientOnly?: boolean
}>(), {
  width: '560px',
  labelWidth: '120px',
  excludeIds: () => [],
  clientOnly: true
})

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'confirm', selectedIds: number[]): void
}>()

const { t } = useI18n()

const visible = computed({
  get: () => props.modelValue,
  set: (val) => emit('update:modelValue', val)
})

const formRef = ref<FormInstance>()
const loadingOptions = ref(false)
const submitting = ref(false)
const options = ref<SelectOption[]>([])

const form = ref({
  selectedIds: [] as number[]
})

const rules: FormRules = {
  selectedIds: [{
    required: true,
    message: () => props.mode === 'user' ? t('acl.selectUserRequired') : t('acl.selectGroupRequired'),
    trigger: 'change'
  }]
}

// 过滤掉已授权的选项
const filteredOptions = computed(() => {
  if (!props.excludeIds?.length) return options.value
  return options.value.filter(o => !props.excludeIds!.includes(o.id))
})

// 获取用户列表
const fetchUsers = async () => {
  loadingOptions.value = true
  try {
    const params: any = { size: 1000 }
    if (props.clientOnly) params.role = 'client'
    const res = await getUsers(params)
    if (res.success && res.data) {
      options.value = res.data.map((u: User) => ({
        id: u.id,
        label: `${u.name}${u.alias ? ' (' + u.alias + ')' : ''}`
      }))
    }
  } catch (error) {
    console.error('获取用户列表失败:', error)
  } finally {
    loadingOptions.value = false
  }
}

// 获取分组列表
const fetchGroups = async () => {
  loadingOptions.value = true
  try {
    const res = await getGroups({ size: 1000 })
    if (res.success && res.data) {
      options.value = res.data.map((g: Group) => ({
        id: g.id,
        label: `${g.name}${g.description ? ' (' + g.description + ')' : ''}`
      }))
    }
  } catch (error) {
    console.error('获取分组列表失败:', error)
  } finally {
    loadingOptions.value = false
  }
}

// 提交
const handleSubmit = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      emit('confirm', form.value.selectedIds)
    } finally {
      submitting.value = false
    }
  })
}

// 关闭并重置
const handleClose = () => {
  form.value.selectedIds = []
  formRef.value?.resetFields()
  visible.value = false
}

// 提供给父组件调用的关闭方法（提交成功后关闭）
const close = () => {
  handleClose()
}

// 设置提交中状态（父组件在异步提交时调用）
const setSubmitting = (val: boolean) => {
  submitting.value = val
}

defineExpose({ close, setSubmitting, formRef })

// 弹窗打开时加载数据
watch(visible, (val) => {
  if (val) {
    if (props.mode === 'user') {
      fetchUsers()
    } else {
      fetchGroups()
    }
  }
})
</script>

<style scoped>
:deep(.form-tip) {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
}
</style>
