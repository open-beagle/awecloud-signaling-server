<template>
  <div class="table-toolbar">
    <div class="toolbar-left">
      <span class="total-text">{{ t('common.total') }} {{ total }} {{ t('common.records') }}</span>
      <slot name="left" />
    </div>
    <div class="toolbar-right">
      <el-button 
        v-if="showRefresh"
        text 
        @click="$emit('refresh')"
        class="toolbar-btn"
      >
        <el-icon><Refresh /></el-icon>
        {{ t('common.refresh') }}
      </el-button>
      <el-button 
        v-if="showColumnSetting"
        text 
        @click="$emit('columnSetting')"
        class="toolbar-btn"
      >
        <el-icon><Setting /></el-icon>
        {{ t('common.columnSetting') }}
      </el-button>
      <el-button 
        v-if="showExport"
        text 
        @click="$emit('export')"
        class="toolbar-btn"
      >
        <el-icon><Download /></el-icon>
        {{ t('common.export') }}
      </el-button>
      <slot name="right" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Refresh, Setting, Download } from '@element-plus/icons-vue'

interface Props {
  total?: number
  showRefresh?: boolean
  showColumnSetting?: boolean
  showExport?: boolean
}

withDefaults(defineProps<Props>(), {
  total: 0,
  showRefresh: true,
  showColumnSetting: false,
  showExport: true
})

defineEmits<{
  refresh: []
  columnSetting: []
  export: []
}>()

const { t } = useI18n()
</script>

<style scoped>
.table-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 0;
  border-bottom: 1px solid #ebeef5;
  margin-bottom: 16px;
}

.toolbar-left {
  display: flex;
  align-items: center;
  gap: 16px;
}

.total-text {
  font-size: 14px;
  color: #606266;
}

.toolbar-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.toolbar-btn {
  font-size: 14px;
  padding: 8px 12px;
}

.toolbar-btn:hover {
  color: #409eff;
  background-color: #ecf5ff;
}
</style>
