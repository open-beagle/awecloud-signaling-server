<template>
  <el-dialog
    v-model="visible"
    title="Client Secret"
    width="600px"
  >
    <el-alert
      title="重要提示"
      type="warning"
      :closable="false"
      style="margin-bottom: 20px"
    >
      <p>请妥善保存此Secret，它只会显示一次！</p>
      <p>Client需要使用此Secret在Desktop应用中登录。</p>
    </el-alert>

    <div class="secret-info">
      <div class="info-item">
        <label>Client ID:</label>
        <div class="value">{{ clientId }}</div>
      </div>

      <div class="info-item">
        <label>Client Secret:</label>
        <div class="secret-value">
          <el-input
            :model-value="clientSecret"
            readonly
            type="textarea"
            :rows="3"
          />
          <el-button
            type="primary"
            :icon="CopyDocument"
            @click="handleCopy"
            style="margin-top: 10px"
          >
            复制Secret
          </el-button>
        </div>
      </div>
    </div>

    <template #footer>
      <el-button type="primary" @click="visible = false">
        我已保存
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { CopyDocument } from '@element-plus/icons-vue'

interface Props {
  modelValue: boolean
  clientId: string
  clientSecret: string
}

interface Emits {
  (e: 'update:modelValue', value: boolean): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const visible = ref(false)

watch(() => props.modelValue, (val) => {
  visible.value = val
})

watch(visible, (val) => {
  emit('update:modelValue', val)
})

const handleCopy = async () => {
  try {
    await navigator.clipboard.writeText(props.clientSecret)
    ElMessage.success('已复制到剪贴板')
  } catch (error) {
    ElMessage.error('复制失败，请手动复制')
  }
}
</script>

<style scoped>
.secret-info {
  padding: 10px 0;
}

.info-item {
  margin-bottom: 20px;
}

.info-item label {
  display: block;
  font-weight: bold;
  margin-bottom: 8px;
  color: #333;
}

.value {
  padding: 10px;
  background: #f5f5f5;
  border-radius: 4px;
  font-family: monospace;
}

.secret-value {
  display: flex;
  flex-direction: column;
}
</style>
