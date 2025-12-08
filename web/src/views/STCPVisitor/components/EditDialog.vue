<template>
  <el-dialog v-model="visible" title="编辑STCP访问" width="600px" @close="handleClose">
    <el-form ref="formRef" :model="form" :rules="rules" label-width="120px">
      <el-form-item label="访问名称">
        <el-input v-model="form.visitor_name" disabled />
      </el-form-item>

      <el-form-item label="所属Agent">
        <el-input v-model="form.agent_name" disabled />
      </el-form-item>

      <el-form-item label="目标服务名称">
        <el-input v-model="form.server_name" disabled />
      </el-form-item>

      <el-form-item label="绑定地址" prop="bind_addr">
        <el-input v-model="form.bind_addr" placeholder="本地绑定地址" />
      </el-form-item>

      <el-form-item label="绑定端口" prop="bind_port">
        <el-input-number v-model="form.bind_port" :min="1" :max="65535" placeholder="本地绑定端口"
          style="width: 100%" />
      </el-form-item>

      <el-form-item label="描述" prop="description">
        <el-input v-model="form.description" type="textarea" :rows="3" placeholder="请输入描述信息" />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="handleClose">取消</el-button>
      <el-button type="primary" @click="handleSubmit" :loading="submitting">确定</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { updateSTCPVisitor } from '@/api/stcp-visitor'

const props = defineProps({
  modelValue: Boolean,
  visitor: Object
})

const emit = defineEmits(['update:modelValue', 'success'])

const visible = ref(false)
const formRef = ref(null)
const submitting = ref(false)

const form = reactive({
  id: null,
  visitor_name: '',
  agent_name: '',
  server_name: '',
  bind_addr: '',
  bind_port: null,
  description: ''
})

const rules = {
  bind_addr: [
    { required: true, message: '请输入绑定地址', trigger: 'blur' }
  ],
  bind_port: [
    { required: true, message: '请输入绑定端口', trigger: 'blur' },
    { type: 'number', min: 1, max: 65535, message: '端口范围1-65535', trigger: 'blur' }
  ]
}

watch(() => props.modelValue, (val) => {
  visible.value = val
  if (val && props.visitor) {
    Object.assign(form, props.visitor)
  }
})

watch(visible, (val) => {
  emit('update:modelValue', val)
})

const handleSubmit = async () => {
  try {
    await formRef.value.validate()
    submitting.value = true

    await updateSTCPVisitor(form.id, {
      bind_addr: form.bind_addr,
      bind_port: form.bind_port,
      description: form.description
    })

    ElMessage.success('更新成功')
    emit('success')
    handleClose()
  } catch (error) {
    if (error.response?.data?.error) {
      ElMessage.error(error.response.data.error)
    } else if (error !== false) {
      ElMessage.error('更新失败')
    }
  } finally {
    submitting.value = false
  }
}

const handleClose = () => {
  visible.value = false
}
</script>
