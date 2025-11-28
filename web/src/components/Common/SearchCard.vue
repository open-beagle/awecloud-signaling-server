<template>
  <el-card class="search-card" shadow="never">
    <template #header>
      <div class="search-header">
        <span class="search-title">{{ title }}</span>
        <el-button 
          v-if="collapsible"
          text 
          @click="toggleCollapse"
          class="collapse-btn"
        >
          {{ collapsed ? t('common.expand') : t('common.collapse') }}
          <el-icon class="collapse-icon">
            <ArrowUp v-if="!collapsed" />
            <ArrowDown v-else />
          </el-icon>
        </el-button>
      </div>
    </template>
    
    <el-collapse-transition>
      <div v-show="!collapsed">
        <slot />
      </div>
    </el-collapse-transition>
  </el-card>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowUp, ArrowDown } from '@element-plus/icons-vue'

interface Props {
  title?: string
  collapsible?: boolean
  defaultCollapsed?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  title: '搜索筛选',
  collapsible: true,
  defaultCollapsed: false
})

const { t } = useI18n()
const collapsed = ref(props.defaultCollapsed)

const toggleCollapse = () => {
  collapsed.value = !collapsed.value
}
</script>

<style scoped>
.search-card {
  margin-bottom: 20px;
}

.search-card :deep(.el-card__header) {
  padding: 16px 20px;
  background-color: #fafafa;
}

.search-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.search-title {
  font-size: 14px;
  font-weight: 500;
  color: #303133;
}

.collapse-btn {
  font-size: 14px;
  padding: 0;
}

.collapse-icon {
  margin-left: 4px;
  transition: transform 0.3s;
}
</style>
