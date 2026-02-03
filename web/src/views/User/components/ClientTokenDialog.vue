<template>
  <el-dialog
    v-model="visible"
    :title="t('clientToken.title') + ': ' + (user?.name || '')"
    width="650px"
    @close="handleClose"
  >
    <el-tabs v-model="activeTab">
      <!-- 生成 Token -->
      <el-tab-pane :label="t('clientToken.generateToken')" name="generate">
        <el-form :model="form" label-width="100px">
          <el-form-item :label="t('clientToken.tokenName')" required>
            <el-input
              v-model="form.name"
              :placeholder="t('clientToken.tokenNamePlaceholder')"
            />
          </el-form-item>
          <el-form-item :label="t('clientToken.deviceName')" required>
            <el-input
              v-model="form.deviceName"
              :placeholder="t('clientToken.deviceNamePlaceholder')"
            />
          </el-form-item>
        </el-form>

        <div v-if="tokenResult" class="token-result">
          <el-alert type="warning" :closable="false" show-icon>
            <template #title>
              <span>{{ t('clientToken.tokenWarning') }}</span>
            </template>
          </el-alert>

          <div class="env-section">
            <div class="env-label">{{ t('clientToken.envConfig') }}:</div>
            <el-input
              v-model="tokenResult.env_config"
              type="textarea"
              :rows="5"
              readonly
            />
            <el-button
              type="primary"
              :icon="CopyDocument"
              @click="handleCopy"
              style="margin-top: 10px"
            >
              {{ t('common.copy') }}
            </el-button>
          </div>
        </div>

        <template #footer>
          <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
          <el-button
            type="primary"
            :loading="generating"
            :disabled="!form.name || !form.deviceName"
            @click="handleGenerate"
          >
            {{ t('clientToken.generateToken') }}
          </el-button>
        </template>
      </el-tab-pane>

      <!-- Token 列表 -->
      <el-tab-pane :label="t('clientToken.tokenList')" name="list">
        <el-table v-loading="listLoading" :data="tokens" stripe size="small">
          <el-table-column :label="t('common.status')" width="80">
            <template #default="{ row }">
              <el-tag :type="getStatusType(row.status)" size="small">
                {{ getStatusText(row.status) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="name" :label="t('clientToken.tokenName')" min-width="100" />
          <el-table-column prop="device_name" :label="t('clientToken.deviceName')" min-width="100" />
          <el-table-column prop="created_by_name" :label="t('common.createdBy')" width="80" />
          <el-table-column :label="t('common.createdAt')" width="100">
            <template #default="{ row }">
              {{ formatDate(row.created_at) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('clientToken.boundAt')" width="100">
            <template #default="{ row }">
              {{ row.bound_at ? formatDate(row.bound_at) : '-' }}
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="80" fixed="right">
            <template #default="{ row }">
              <el-button
                size="small"
                type="danger"
                @click="handleDelete(row)"
              >
                {{ t('common.delete') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>
    </el-tabs>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CopyDocument } from '@element-plus/icons-vue'
import {
  createClientToken,
  getClientTokens,
  deleteClientToken,
  type ClientToken,
  type CreateClientTokenResponse
} from '@/api/clientToken'

const { t } = useI18n()

interface User {
  id: number
  name: string
}

const props = defineProps<{
  modelValue: boolean
  user: User | null
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (value) => emit('update:modelValue', value)
})

const activeTab = ref('generate')
const form = ref({ name: '', deviceName: '' })
const generating = ref(false)
const tokenResult = ref<CreateClientTokenResponse | null>(null)

// Token 列表
const listLoading = ref(false)
const tokens = ref<ClientToken[]>([])

// 监听对话框打开
watch(visible, (val) => {
  if (val) {
    form.value = { name: '', deviceName: '' }
    tokenResult.value = null
    if (activeTab.value === 'list') {
      loadTokens()
    }
  }
})

// 监听 tab 切换
watch(activeTab, (val) => {
  if (val === 'list' && props.user) {
    loadTokens()
  }
})

const loadTokens = async () => {
  if (!props.user) return
  listLoading.value = true
  try {
    const res = await getClientTokens({ user_id: props.user.id })
    if (res.success && res.data) {
      tokens.value = res.data
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  } finally {
    listLoading.value = false
  }
}

const handleGenerate = async () => {
  if (!props.user || !form.value.name || !form.value.deviceName) return

  generating.value = true
  try {
    const res = await createClientToken({
      user_id: props.user.id,
      name: form.value.name,
      device_name: form.value.deviceName
    })
    if (res.success && res.data) {
      tokenResult.value = res.data
      ElMessage.success(t('common.success'))
    } else {
      ElMessage.error(res.message || t('common.failed'))
    }
  } catch (error) {
    ElMessage.error(t('common.failed'))
  } finally {
    generating.value = false
  }
}

const handleCopy = async () => {
  if (!tokenResult.value) return
  try {
    await navigator.clipboard.writeText(tokenResult.value.env_config)
    ElMessage.success(t('common.copySuccess'))
  } catch {
    ElMessage.error(t('common.copyFailed'))
  }
}

const handleDelete = async (token: ClientToken) => {
  try {
    await ElMessageBox.confirm(t('clientToken.deleteConfirm'), { type: 'warning' })
    const res = await deleteClientToken(token.id)
    if (res.success) {
      ElMessage.success(t('common.success'))
      loadTokens()
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(t('common.failed'))
    }
  }
}

const handleClose = () => {
  form.value = { name: '', deviceName: '' }
  tokenResult.value = null
}

const getStatusType = (status: string) => {
  switch (status) {
    case 'pending': return 'warning'
    case 'bound': return 'success'
    default: return 'info'
  }
}

const getStatusText = (status: string) => {
  switch (status) {
    case 'pending': return t('clientToken.statusPending')
    case 'bound': return t('clientToken.statusBound')
    default: return status
  }
}

const formatDate = (dateStr: string) => {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  return `${date.getMonth() + 1}-${date.getDate()}`
}
</script>

<style scoped>
.token-result {
  margin-top: 20px;
}

.env-section {
  margin-top: 15px;
}

.env-label {
  margin-bottom: 8px;
  font-weight: 500;
}
</style>
