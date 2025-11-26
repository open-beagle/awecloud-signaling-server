<template>
  <el-button :icon="CopyDocument" size="small" @click="handleCopy">
    {{ t('common.copy') }}
  </el-button>
</template>

<script setup lang="ts">
import { CopyDocument } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'

const props = defineProps<{
  text: string
}>()

const { t } = useI18n()

const handleCopy = async () => {
  try {
    await navigator.clipboard.writeText(props.text)
    ElMessage.success(t('common.copySuccess'))
  } catch (error) {
    ElMessage.error(t('common.failed'))
  }
}
</script>
