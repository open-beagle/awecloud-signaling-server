<template>
  <div class="cap-switch">
    <div><strong>{{ title }}</strong><span>{{ description }}</span></div>
    <el-switch :model-value="modelValue" :disabled="disabled" @update:model-value="$emit('update:modelValue', $event)" />
  </div>
</template>

<script setup lang="ts">
defineProps<{
  title: string
  description: string
  modelValue: boolean
  disabled?: boolean
}>()

defineEmits<{ 'update:modelValue': [value: boolean] }>()
</script>

<style scoped>
.cap-switch { min-height: 68px; display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; padding: 12px; border: 1px solid var(--border-light); border-radius: 5px; }
.cap-switch strong, .cap-switch span { display: block; }
.cap-switch span { color: var(--text-secondary); font-size: 11px; }
</style>
