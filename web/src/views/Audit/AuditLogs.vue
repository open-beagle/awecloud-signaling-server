<template>
  <div class="audit-logs-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ $t('audit.title') }}</span>
          <el-button type="primary" @click="handleExport">
            {{ $t('audit.export') }}
          </el-button>
        </div>
      </template>

      <!-- 过滤条件 -->
      <el-form :inline="true" :model="queryParams" class="filter-form">
        <el-form-item :label="$t('audit.clientId')">
          <el-input
            v-model="queryParams.client_id"
            :placeholder="$t('audit.clientIdPlaceholder')"
            clearable
            style="width: 200px"
          />
        </el-form-item>

        <el-form-item :label="$t('audit.instanceId')">
          <el-input
            v-model="queryParams.stcp_instance_id"
            :placeholder="$t('audit.instanceIdPlaceholder')"
            clearable
            style="width: 200px"
          />
        </el-form-item>

        <el-form-item :label="$t('audit.action')">
          <el-select
            v-model="queryParams.action"
            :placeholder="$t('audit.actionPlaceholder')"
            clearable
            style="width: 150px"
          >
            <el-option label="Connect" value="connect" />
            <el-option label="Disconnect" value="disconnect" />
          </el-select>
        </el-form-item>

        <el-form-item :label="$t('audit.dateRange')">
          <el-date-picker
            v-model="dateRange"
            type="daterange"
            :start-placeholder="$t('audit.startDate')"
            :end-placeholder="$t('audit.endDate')"
            value-format="YYYY-MM-DD"
            style="width: 300px"
          />
        </el-form-item>

        <el-form-item>
          <el-button type="primary" @click="handleQuery">
            {{ $t('common.search') }}
          </el-button>
          <el-button @click="handleReset">
            {{ $t('common.reset') }}
          </el-button>
        </el-form-item>
      </el-form>

      <!-- 审计日志表格 -->
      <el-table
        v-loading="loading"
        :data="auditLogs"
        border
        style="width: 100%"
      >
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column
          prop="client_name"
          :label="$t('audit.clientName')"
          width="150"
        />
        <el-table-column
          prop="stcp_instance_name"
          :label="$t('audit.instanceName')"
          width="150"
        />
        <el-table-column
          prop="action"
          :label="$t('audit.action')"
          width="100"
        >
          <template #default="{ row }">
            <el-tag :type="row.action === 'connect' ? 'success' : 'info'">
              {{ row.action }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="local_port"
          :label="$t('audit.localPort')"
          width="100"
        />
        <el-table-column
          prop="ip_address"
          :label="$t('audit.ipAddress')"
          width="150"
        />
        <el-table-column
          prop="success"
          :label="$t('audit.status')"
          width="100"
        >
          <template #default="{ row }">
            <el-tag :type="row.success ? 'success' : 'danger'">
              {{ row.success ? $t('audit.success') : $t('audit.failed') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="device_info"
          :label="$t('audit.deviceInfo')"
          width="200"
        >
          <template #default="{ row }">
            <div>{{ row.device_info.os }} {{ row.device_info.os_version }}</div>
            <div class="text-secondary">{{ row.device_info.hostname }}</div>
          </template>
        </el-table-column>
        <el-table-column
          prop="created_at"
          :label="$t('audit.createdAt')"
          width="180"
        >
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="error_message"
          :label="$t('audit.errorMessage')"
          min-width="200"
        >
          <template #default="{ row }">
            <span v-if="row.error_message" class="text-danger">
              {{ row.error_message }}
            </span>
            <span v-else class="text-secondary">-</span>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <el-pagination
        v-model:current-page="queryParams.page"
        v-model:page-size="queryParams.page_size"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="handleQuery"
        @current-change="handleQuery"
        style="margin-top: 20px; justify-content: flex-end"
      />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { queryAuditLogs, exportAuditLogs, type QueryAuditLogsParams, type AuditLog } from '@/api/audit'
import { formatTime } from '@/utils/time'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const loading = ref(false)
const auditLogs = ref<AuditLog[]>([])
const total = ref(0)
const dateRange = ref<[string, string] | null>(null)

const queryParams = reactive<QueryAuditLogsParams>({
  client_id: '',
  stcp_instance_id: '',
  action: '',
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

    const response = await queryAuditLogs(queryParams)
    if (response.success) {
      auditLogs.value = response.logs || []
      total.value = response.total
    } else {
      ElMessage.error(response.message || t('audit.queryFailed'))
    }
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
    const params = { ...queryParams }
    if (dateRange.value) {
      params.start_date = dateRange.value[0]
      params.end_date = dateRange.value[1]
    }

    const blob = await exportAuditLogs(params)
    const url = window.URL.createObjectURL(blob as Blob)
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

onMounted(() => {
  handleQuery()
})
</script>

<style scoped>
.audit-logs-container {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.filter-form {
  margin-bottom: 20px;
}

.text-secondary {
  color: #909399;
  font-size: 12px;
}

.text-danger {
  color: #f56c6c;
}
</style>
