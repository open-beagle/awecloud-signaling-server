<template>
  <header class="page-header">
    <div class="page-header__copy">
      <div v-if="eyebrow" class="page-header__eyebrow">{{ eyebrow }}</div>
      <h1>{{ title }}</h1>
      <p v-if="description">{{ description }}</p>
    </div>
    <div v-if="$slots.actions" class="page-header__actions">
      <slot name="actions" />
    </div>
  </header>
</template>

<script setup lang="ts">
defineProps<{
  title: string
  description?: string
  eyebrow?: string
}>()
</script>

<style scoped>
.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  margin-bottom: 18px;
}

.page-header__copy {
  min-width: 0;
}

.page-header__eyebrow {
  margin-bottom: 5px;
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 18px;
}

h1 {
  margin: 0;
  color: var(--text-primary);
  font-size: 24px;
  line-height: 32px;
}

p {
  margin: 5px 0 0;
  color: var(--text-regular);
  font-size: 13px;
  line-height: 20px;
}

.page-header__actions {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 8px;
}
</style>
