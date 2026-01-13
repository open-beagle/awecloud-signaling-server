<template>
  <el-tooltip :content="fullTime" placement="top">
    <span class="time-ago">{{ relativeTime }}</span>
  </el-tooltip>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { formatRelativeTime, formatFullTime } from '@/utils/time'

const props = defineProps<{
  time: string | number
}>()

const relativeTime = computed(() => formatRelativeTime(props.time))
const fullTime = computed(() => formatFullTime(props.time))
</script>

<style scoped>
.time-ago {
  cursor: help;
  color: var(--text-secondary);
}
</style>
