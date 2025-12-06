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
      >
        <el-form-item label="客户端下载地址">
          <div class="form-item-content">
            <el-input
              v-model="form.client_download_url"
              placeholder="请输入客户端文件存储的基础URL（如：https://cdn.example.com/downloads）"
              clearable
            />
            <div class="form-item-tip">
              设置客户端文件存储的基础URL，系统会自动拼接文件名生成完整下载链接
            </div>
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
import { ref, onMounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { getSystemConfig, updateSystemConfig } from '@/api/system'

const router = useRouter()
const formRef = ref()
const saving = ref(false)

const form = ref({
  client_download_url: ''
})

const downloadPageUrl = computed(() => {
  return window.location.origin + '/download'
})

const goToDownloadPage = () => {
  window.open('/download', '_blank')
}

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

onMounted(() => {
  loadConfig()
})

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
  color: var(--text-primary);
}

:deep(.el-form-item__content) {
  flex: 1;
}

.form-item-content {
  width: 100%;
  max-width: 800px;
}

.form-item-content .el-input {
  width: 100%;
}

.form-item-tip {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 8px;
  line-height: 1.5;
}
</style>
