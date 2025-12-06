<template>
  <div class="logo-container" :class="{ collapsed: collapsed }">
    <img src="@/assets/logo.png" class="logo-icon" alt="Logo" />
    
    <transition name="fade">
      <span v-if="!collapsed" class="logo-text">{{ t('app.title') }}</span>
    </transition>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

defineProps<{
  collapsed?: boolean
}>()
</script>

<style scoped>
.logo-container {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0 20px;
  transition: all 0.3s;
}

.logo-container.collapsed {
  justify-content: center;
  padding: 0;
}

.logo-icon {
  width: 40px;
  height: 40px;
  flex-shrink: 0;
  transition: all 0.3s;
  object-fit: contain;
}

.logo-container.collapsed .logo-icon {
  width: 36px;
  height: 36px;
}

.logo-text {
  font-size: 18px;
  font-weight: 600;
  color: #303133;
  letter-spacing: 0.5px;
  white-space: nowrap;
}

/* 淡入淡出动画 */
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.3s;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
