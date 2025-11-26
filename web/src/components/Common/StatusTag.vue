<template>
  <el-tag :type="tagType" size="small">
    {{ statusText }}
  </el-tag>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  status: 'online' | 'offline'
}>()

const { t } = useI18n()

const tagType = computed(() => {
  return props.status === 'online' ? 'success' : 'info'
})

const statusText = computed(() => {
  return props.status === 'online' ? t('common.online') : t('common.offline')
})
</script>
