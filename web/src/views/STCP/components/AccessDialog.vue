<template>
  <el-dialog
    v-model="visible"
    title="访问权限设置"
    width="500px"
    @close="handleClose"
  >
    <el-form :model="form" label-width="100px">
      <el-form-item label="服务名称">
        <el-input :value="instance?.instance_name" disabled />
      </el-form-item>
      
      <el-form-item label="访问权限">
        <el-radio-group v-model="form.access_type">
          <el-radio label="public">
            <div>
              <div><strong>Public</strong> - 所有用户可访问</div>
              <div style="font-size: 12px; color: #999">适用于团队共享的开发环境</div>
            </div>
          </el-radio>
          <el-radio label="private">
            <div>
              <div><strong>Private</strong> - 仅授权用户可访问</div>
              <div style="font-size: 12px; color: #999">适用于个人开发环境或敏感服务</div>
            </div>
          </el-radio>
          <el-radio label="group">
            <div>
              <div><strong>Group</strong> - 组成员可访问</div>
              <div style="font-size: 12px; color: #999">适用于团队协作场景</div>
            </div>
          </el-radio>
        </el-radio-group>
      </el-form-item>

      <el-form-item v-if="form.access_type === 'group'" label="选择组">
        <el-select v-model="form.group_id" placeholder="请选择组" style="width: 100%">
          <el-option
            v-for="group in groups"
            :key="group.id"
            :label="`${group.name} (${group.member_count || 0} 成员)`"
            :value="group.id"
          />
        </el-select>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" @click="handleSubmit">确定</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { setAccessType } from '@/api/stcp'
import { getGroups } from '@/api/group'
import type { STCPInstance } from '@/types/models'
import type { Group } from '@/api/group'

interface Props {
  modelValue: boolean
  instance: STCPInstance | null
}

interface Emits {
  (e: 'update:modelValue', value: boolean): void
  (e: 'success'): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const visible = computed({
  get: () => props.modelValue,
  set: (val) => emit('update:modelValue', val)
})

const groups = ref<Group[]>([])
const form = ref({
  access_type: 'public',
  group_id: null as number | null
})

const loadGroups = async () => {
  try {
    const res = await getGroups()
    if (res.success && res.data) {
      groups.value = res.data
    }
  } catch (error) {
    ElMessage.error('加载组列表失败')
  }
}

const handleSubmit = async () => {
  if (!props.instance) return

  if (form.value.access_type === 'group' && !form.value.group_id) {
    ElMessage.warning('请选择组')
    return
  }

  try {
    const res = await setAccessType(
      props.instance.id,
      form.value.access_type,
      form.value.group_id || undefined
    )
    
    if (res.success) {
      ElMessage.success('权限设置成功')
      visible.value = false
      emit('success')
    }
  } catch (error) {
    ElMessage.error('设置失败')
  }
}

const handleClose = () => {
  form.value = {
    access_type: 'public',
    group_id: null
  }
}

watch(() => props.modelValue, (val) => {
  if (val && props.instance) {
    form.value.access_type = props.instance.access_type || 'public'
    form.value.group_id = props.instance.group_id || null
    loadGroups()
  }
})
</script>

<style scoped>
:deep(.el-radio) {
  display: block;
  margin: 10px 0;
  height: auto;
  line-height: 1.5;
}

:deep(.el-radio__label) {
  white-space: normal;
}
</style>
