<template>
  <div class="page-container settings-page">
    <div class="page-header">
      <div>
        <h1>租户设置</h1>
        <p>维护当前租户可自行管理的基础信息。稳定标识和租户状态由平台侧治理。</p>
      </div>
      <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
    </div>

    <el-alert v-if="settings?.status === 'suspended'" title="当前租户已暂停，只能查看设置；恢复租户需由平台管理员在租户管理中操作。" type="warning" show-icon :closable="false" />
    <section v-loading="loading" class="settings-surface">
      <div class="section-heading">
        <div><strong>基础信息</strong><span>租户业务空间中使用的显示名称</span></div>
        <el-tag v-if="settings" size="small" :type="settings.status === 'active' ? 'success' : 'warning'">{{ settings.status === 'active' ? '正常' : '已暂停' }}</el-tag>
      </div>
      <el-form label-position="top" class="settings-form" @submit.prevent="save">
        <el-form-item label="租户名称" required>
          <el-input v-model="form.name" maxlength="200" show-word-limit :disabled="!canWrite || settings?.status === 'suspended'" />
          <div class="form-hint">名称会显示在租户上下文、导航和审计记录中。</div>
        </el-form-item>
        <el-form-item label="稳定标识">
          <el-input :model-value="settings?.key || ''" disabled />
          <div class="form-hint">稳定标识用于可信业务绑定，创建后不能在租户侧修改。</div>
        </el-form-item>
        <div class="form-actions">
          <el-button type="primary" native-type="submit" :loading="saving" :disabled="!canSave">保存设置</el-button>
        </div>
      </el-form>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { getTenantSettings, updateTenantSettings, type TenantSettings } from '@/api/tenantManagement'
import { useTenantStore } from '@/stores/tenant'

const tenantStore = useTenantStore()
const loading = ref(false)
const saving = ref(false)
const settings = ref<TenantSettings>()
const form = reactive({ name: '' })
const canWrite = computed(() => tenantStore.canTenant('tenant.settings.write'))
const canSave = computed(() => canWrite.value && settings.value?.status !== 'suspended' && form.name.trim() !== '' && form.name.trim() !== settings.value?.name)

const load = async () => {
  if (!tenantStore.tenantId) return
  loading.value = true
  try {
    const response = await getTenantSettings(tenantStore.tenantId)
    if (response.success && response.data) {
      settings.value = response.data
      form.name = response.data.name
    }
  } finally { loading.value = false }
}

const save = async () => {
  if (!canSave.value || !tenantStore.tenantId) return
  saving.value = true
  try {
    const response = await updateTenantSettings(tenantStore.tenantId, { name: form.name.trim() })
    if (response.success && response.data) {
      settings.value = response.data
      form.name = response.data.name
      await tenantStore.loadContexts(true)
      ElMessage.success('租户设置已保存')
    }
  } finally { saving.value = false }
}

onMounted(load)
</script>

<style scoped>
.settings-page { max-width: none; }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; margin-bottom: 18px; }
h1 { margin: 0; color: var(--text-primary); font-size: 24px; line-height: 32px; }
.page-header p { margin: 5px 0 0; color: var(--text-secondary); font-size: 13px; }
.settings-surface { max-width: 760px; margin-top: 14px; overflow: hidden; border: 1px solid var(--border-light); border-radius: 7px; background: #fff; }
.section-heading { display: flex; align-items: center; justify-content: space-between; gap: 18px; padding: 16px 18px; border-bottom: 1px solid var(--border-light); }
.section-heading div { display: flex; flex-direction: column; gap: 4px; }
.section-heading strong { color: var(--text-primary); font-size: 15px; }
.section-heading span, .form-hint { color: var(--text-secondary); font-size: 12px; }
.settings-form { padding: 18px; }
.form-hint { margin-top: 6px; line-height: 18px; }
.form-actions { display: flex; justify-content: flex-end; padding-top: 4px; }
@media (max-width: 650px) { .page-header { flex-direction: column; } .settings-surface { max-width: none; } }
</style>
