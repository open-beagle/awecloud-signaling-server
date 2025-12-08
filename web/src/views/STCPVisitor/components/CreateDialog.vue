<template>
  <el-dialog v-model="visible" title="新建STCP访问" width="600px" @close="handleClose">
    <el-form ref="formRef" :model="form" :rules="rules" label-width="120px">
      <el-form-item label="访问名称" prop="visitor_name">
        <el-input v-model="form.visitor_name" placeholder="请输入访问名称" />
      </el-form-item>

      <el-form-item label="所属Agent" prop="agent_name">
        <el-select v-model="form.agent_name" placeholder="请选择Agent" style="width: 100%">
          <el-option v-for="agent in agents" :key="agent.agent_name" :label="agent.agent_name"
            :value="agent.agent_name">
            <span>{{ agent.agent_name }}</span>
            <span style="float: right; color: var(--el-text-color-secondary); font-size: 13px">
              {{ agent.status === 'online' ? '在线' : '离线' }}
            </span>
          </el-option>
        </el-select>
      </el-form-item>

      <el-form-item label="目标服务名称" prop="server_name">
        <el-input v-model="form.server_name" placeholder="要访问的STCP服务名称" />
        <div class="form-tip">填写目标Agent上的STCP实例名称</div>
      </el-form-item>

      <el-form-item label="密钥" prop="secret_key">
        <el-input v-model="form.secret_key" type="password" show-password placeholder="与目标服务的密钥一致" />
        <div class="form-tip">必须与目标STCP服务的密钥完全一致</div>
      </el-form-item>

      <el-form-item label="绑定地址" prop="bind_addr">
        <el-input v-model="form.bind_addr" placeholder="本地绑定地址" />
        <div class="form-tip">默认 127.0.0.1，仅本机访问</div>
      </el-form-item>

      <el-form-item label="绑定端口" prop="bind_port">
        <el-input-number v-model="form.bind_port" :min="1" :max="65535" placeholder="本地绑定端口"
          style="width: 100%" />
        <div class="form-tip">访问此端口即可连接到目标服务</div>
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
import { createSTCPVisitor } from '@/api/stcp-visitor'
import { getAgents } from '@/api/agent'

const props = defineProps({
  modelValue: Boolean
})

const emit = defineEmits(['update:modelValue', 'success'])

const visible = ref(false)
const formRef = ref(null)
const submitting = ref(false)
const agents = ref([])

const form = reactive({
  visitor_name: '',
  agent_name: '',
  server_name: '',
  secret_key: '',
  bind_addr: '127.0.0.1',
  bind_port: null,
  description: ''
})

const rules = {
  visitor_name: [
    { required: true, message: '请输入访问名称', trigger: 'blur' }
  ],
  agent_name: [
    { required: true, message: '请选择Agent', trigger: 'change' }
  ],
  server_name: [
    { required: true, message: '请输入目标服务名称', trigger: 'blur' }
  ],
  secret_key: [
    { required: true, message: '请输入密钥', trigger: 'blur' }
  ],
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
  if (val) {
    fetchAgents()
  }
})

watch(visible, (val) => {
  emit('update:modelValue', val)
})

const fetchAgents = async () => {
  try {
    const res = await getAgents()
    agents.value = res.data || []
  } catch (error) {
    console.error('获取Agent列表失败', error)
  }
}

const handleSubmit = async () => {
  try {
    await formRef.value.validate()
    submitting.value = true

    await createSTCPVisitor(form)
    ElMessage.success('创建成功')
    emit('success')
    handleClose()
  } catch (error) {
    if (error.response?.data?.error) {
      ElMessage.error(error.response.data.error)
    } else if (error !== false) {
      ElMessage.error('创建失败')
    }
  } finally {
    submitting.value = false
  }
}

const handleClose = () => {
  formRef.value?.resetFields()
  visible.value = false
}
</script>

<style scoped>
.form-tip {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}
</style>
