<template>
  <el-dialog
    v-model="visible"
    title="客户密钥"
    width="600px"
  >
    <el-alert
      title="请妥善保存此密钥，它只会显示一次！客户需要使用此密钥在 Desktop 应用中登录。"
      type="warning"
      :closable="false"
      style="margin-bottom: 20px"
    />

    <div class="secret-info">
      <div class="info-item">
        <label>用户名:</label>
        <div class="value">{{ clientId }}</div>
      </div>

      <div class="info-item">
        <label>密钥:</label>
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
            {{ t('common.copy') }}
          </el-button>
        </div>
      </div>
    </div>

    <template #footer>
      <el-button type="primary" @click="visible = false">
        {{ t('common.confirm') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
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

const { t } = useI18n()

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
    ElMessage.success(t('common.copySuccess'))
  } catch (error) {
    ElMessage.error(t('common.failed'))
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
