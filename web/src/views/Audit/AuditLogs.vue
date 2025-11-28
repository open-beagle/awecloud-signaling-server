<template>
  <div class="audit-logs-page">
    <!-- 搜索筛选区域 -->
    <SearchCard :title="t('audit.title')">
      <el-form :inline="true" :model="queryParams" class="search-form">
        <el-row :gutter="20">
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item :label="t('audit.clientId')">
              <el-input
                v-model="queryParams.client_id"
                :placeholder="t('audit.clientIdPlaceholder')"
                clearable
              />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item :label="t('audit.instanceId')">
              <el-input
                v-model="queryParams.stcp_instance_id"
                :placeholder="t('audit.instanceIdPlaceholder')"
                clearable
              />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item :label="t('audit.action')">
              <el-select
                v-model="queryParams.action"
                :placeholder="t('audit.actionPlaceholder')"
                clearable
              >
                <el-option label="Connect" value="connect" />
                <el-option label="Disconnect" value="disconnect" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item :label="t('common.status')">
              <el-select
                v-model="queryParams.success"
                :placeholder="t('audit.actionPlaceholder')"
                clearable
              >
                <el-option :label="t('audit.success')" :value="true" />
                <el-option :label="t('audit.failed')" :value="false" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="20">
          <el-col :xs="24" :sm="24" :md="12" :lg="8">
            <el-form-item :label="t('audit.dateRange')">
              <el-date-picker
                v-model="dateRange"
                type="daterange"
                :start-placeholder="t('audit.startDate')"
                :end-placeholder="t('audit.endDate')"
                value-format="YYYY-MM-DD"
                style="width: 100%"
              />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row>
          <el-col :span="24" class="form-actions">
            <el-button @click="handleReset">
              {{ t('common.reset') }}
            </el-button>
            <el-button type="primary" @click="handleQuery">
              {{ t('common.search') }}
            </el-button>
            <el-button type="success" @click="handleExport">
              <el-icon><Download /></el-icon>
              {{ t('common.export') }}
            </el-button>
          </el-col>
        </el-row>
      </el-form>
    </SearchCard>

    <!-- 数据表格区域 -->
    <el-card shadow="never">
      <TableToolbar 
        :total="total"
        :show-column-setting="false"
        :show-export="false"
        @refresh="handleQuery"
      />

      <el-table
        v-loading="loading"
        :data="auditLogs"
        border
        stripe
        style="width: 100%"
      >
        <el-table-column prop="id" label="ID" width="80" align="center" />
        <el-table-column
          prop="client_name"
          :label="t('audit.clientName')"
          min-width="150"
        >
          <template #default="{ row }">
            <div class="cell-main">{{ row.client_name }}</div>
            <div class="cell-sub">ID: {{ row.client_id }}</div>
          </template>
        </el-table-column>
        <el-table-column
          prop="stcp_instance_name"
          :label="t('audit.instanceName')"
          min-width="200"
        >
          <template #default="{ row }">
            <div class="cell-main">{{ row.stcp_instance_name }}</div>
            <div class="cell-sub">{{ row.server_address }}</div>
          </template>
        </el-table-column>
        <el-table-column
          prop="action"
          :label="t('audit.action')"
          width="120"
          align="center"
        >
          <template #default="{ row }">
            <el-tag :type="row.action === 'connect' ? 'success' : 'info'" size="small">
              {{ row.action }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="local_port"
          :label="t('audit.localPort')"
          width="100"
          align="center"
        />
        <el-table-column
          prop="ip_address"
          :label="t('audit.ipAddress')"
          width="150"
        />
        <el-table-column
          prop="success"
          :label="t('common.status')"
          width="100"
          align="center"
        >
          <template #default="{ row }">
            <el-tag :type="row.success ? 'success' : 'danger'" size="small">
              {{ row.success ? t('audit.success') : t('audit.failed') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="device_info"
          :label="t('audit.deviceInfo')"
          min-width="180"
        >
          <template #default="{ row }">
            <div class="cell-main">{{ formatOSName(row.device_info) }}</div>
            <div class="cell-sub">{{ row.device_info?.hostname || '-' }}</div>
          </template>
        </el-table-column>
        <el-table-column
          prop="created_at"
          :label="t('audit.createdAt')"
          width="180"
        >
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="error_message"
          :label="t('audit.errorMessage')"
          min-width="200"
        >
          <template #default="{ row }">
            <span v-if="row.error_message" class="error-text">
              {{ row.error_message }}
            </span>
            <span v-else class="cell-sub">-</span>
          </template>
        </el-table-column>
      </el-table>

      <!-- 空状态 -->
      <EmptyState 
        v-if="!loading && auditLogs.length === 0"
        @action="handleQuery"
      />

      <!-- 分页 -->
      <el-pagination
        v-if="auditLogs.length > 0"
        v-model:current-page="queryParams.page"
        v-model:page-size="queryParams.page_size"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="handleQuery"
        @current-change="handleQuery"
        class="pagination"
      />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Download } from '@element-plus/icons-vue'
import { queryAuditLogs, exportAuditLogs, type QueryAuditLogsParams, type AuditLog } from '@/api/audit'
import { formatTime } from '@/utils/time'
import { useI18n } from 'vue-i18n'
import SearchCard from '@/components/Common/SearchCard.vue'
import TableToolbar from '@/components/Common/TableToolbar.vue'
import EmptyState from '@/components/Common/EmptyState.vue'

const { t } = useI18n()

const loading = ref(false)
const auditLogs = ref<AuditLog[]>([])
const total = ref(0)
const dateRange = ref<[string, string] | null>(null)

interface ExtendedQueryParams extends QueryAuditLogsParams {
  success?: boolean | string
}

const queryParams = reactive<ExtendedQueryParams>({
  client_id: '',
  stcp_instance_id: '',
  action: '',
  success: '',
  start_date: '',
  end_date: '',
  page: 1,
  page_size: 50
})

// 查询审计日志
const handleQuery = async () => {
  loading.value = true
  try {
    // 设置日期范围
    if (dateRange.value) {
      queryParams.start_date = dateRange.value[0]
      queryParams.end_date = dateRange.value[1]
    } else {
      queryParams.start_date = ''
      queryParams.end_date = ''
    }

    const response = await queryAuditLogs(queryParams as QueryAuditLogsParams)
    auditLogs.value = response.logs || []
    total.value = response.total
  } catch (error) {
    console.error('Query audit logs error:', error)
    ElMessage.error(t('audit.queryFailed'))
  } finally {
    loading.value = false
  }
}

// 重置查询条件
const handleReset = () => {
  queryParams.client_id = ''
  queryParams.stcp_instance_id = ''
  queryParams.action = ''
  queryParams.success = ''
  queryParams.start_date = ''
  queryParams.end_date = ''
  queryParams.page = 1
  queryParams.page_size = 50
  dateRange.value = null
  handleQuery()
}

// 导出审计日志
const handleExport = async () => {
  try {
    const params = { ...queryParams } as QueryAuditLogsParams
    if (dateRange.value) {
      params.start_date = dateRange.value[0]
      params.end_date = dateRange.value[1]
    }

    const blob = await exportAuditLogs(params)
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `audit_logs_${new Date().getTime()}.csv`
    link.click()
    window.URL.revokeObjectURL(url)
    ElMessage.success(t('audit.exportSuccess'))
  } catch (error) {
    console.error('Export audit logs error:', error)
    ElMessage.error(t('audit.exportFailed'))
  }
}

// 格式化操作系统显示名称
const formatOSName = (deviceInfo: any) => {
  if (!deviceInfo) return ''
  
  const os = deviceInfo.os?.toLowerCase() || ''
  const osVersion = deviceInfo.os_version || ''
  
  // 如果有os_version，直接使用
  if (osVersion && osVersion !== os) {
    return osVersion
  }
  
  // 否则根据os类型返回友好名称
  if (os === 'windows') {
    return 'Windows'
  }
  
  if (os === 'darwin') {
    return 'macOS'
  }
  
  if (os === 'linux') {
    return 'Linux'
  }
  
  // 其他系统，首字母大写
  return os ? os.charAt(0).toUpperCase() + os.slice(1) : 'Unknown'
}

onMounted(() => {
  handleQuery()
})
</script>

<style scoped>
.audit-logs-page {
  padding: 0;
}

.search-form {
  width: 100%;
}

.search-form :deep(.el-form-item) {
  width: 100%;
  margin-bottom: 16px;
}

.search-form :deep(.el-form-item__label) {
  width: 100px;
}

.search-form :deep(.el-form-item__content) {
  flex: 1;
}

.search-form :deep(.el-input),
.search-form :deep(.el-select) {
  width: 100%;
}

.form-actions {
  text-align: right;
  margin-top: 8px;
}

.cell-main {
  color: #303133;
  font-size: 14px;
  line-height: 1.5;
}

.cell-sub {
  color: #909399;
  font-size: 12px;
  line-height: 1.5;
  margin-top: 2px;
}

.error-text {
  color: #f56c6c;
  font-size: 13px;
}

.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}

/* 响应式适配 */
@media (max-width: 768px) {
  .search-form :deep(.el-form-item__label) {
    width: 80px;
  }
  
  .form-actions {
    text-align: center;
  }
  
  .form-actions .el-button {
    margin-bottom: 8px;
  }
}
</style>
