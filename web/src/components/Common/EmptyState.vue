<template>
  <div class="empty-state">
    <el-empty 
      :description="description || t('common.noData')"
      :image-size="imageSize"
    >
      <template v-if="showAction">
        <el-button 
          type="primary" 
          @click="$emit('action')"
        >
          {{ actionText || t('common.refresh') }}
        </el-button>
      </template>
    </el-empty>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

interface Props {
  description?: string
  imageSize?: number
  showAction?: boolean
  actionText?: string
}

withDefaults(defineProps<Props>(), {
  imageSize: 120,
  showAction: true
})

defineEmits<{
  action: []
}>()

const { t } = useI18n()
</script>

<style scoped>
.empty-state {
  padding: 40px 0;
}
</style>
