<template>
  <div class="system-config">
    <el-card>
      <template #header>
        <div class="card-header">
          <span class="card-title">系统配置</span>
        </div>
      </template>

      <el-form
        ref="formRef"
        :model="form"
        label-width="150px"
        style="max-width: 600px"
      >
        <el-form-item label="客户端下载地址">
          <el-input
            v-model="form.client_download_url"
            placeholder="请输入客户端下载地址"
            clearable
          />
          <div class="form-item-tip">
            设置后，登录页将显示"下载客户端"按钮
          </div>
        </el-form-item>

        <el-form-item>
          <el-button type="primary" :loading="saving" @click="handleSave">
            保存
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { getSystemConfig, updateSystemConfig } from '@/api/system'

const formRef = ref()
const saving = ref(false)

const form = ref({
  client_download_url: ''
})

const loadConfig = async () => {
  try {
    const res = await getSystemConfig()
    if (res.success && res.data) {
      form.value.client_download_url = res.data.client_download_url || ''
    }
  } catch (error) {
    console.error('加载配置失败:', error)
  }
}

const handleSave = async () => {
  saving.value = true
  try {
    const res = await updateSystemConfig({
      client_download_url: form.value.client_download_url
    })
    if (res.success) {
      ElMessage.success('保存成功')
    }
  } catch (error) {
    ElMessage.error('保存失败')
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  loadConfig()
})
</script>

<style scoped>
.system-config {
  width: 100%;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-title {
  font-size: 18px;
  font-weight: 500;
}

.form-item-tip {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}
</style>
