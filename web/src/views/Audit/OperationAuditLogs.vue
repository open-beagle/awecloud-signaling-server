<template>
  <div class="operation-audit-page">
    <PageHeader title="连接操作审计" description="查询连接期间执行的操作、访问目标和耗时记录。" />

    <!-- 搜索筛选区域 -->
    <SearchCard title="筛选条件">
      <el-form :inline="true" :model="queryParams" class="search-form">
        <el-row :gutter="20">
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item :label="$t('operationAudit.operationType')">
              <el-select v-model="queryParams.operation_type" :placeholder="$t('operationAudit.operationTypePlaceholder')" clearable>
                <el-option v-for="item in operationTypes" :key="item.value" :label="item.label" :value="item.value" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item :label="$t('operationAudit.endpointName')">
              <el-input v-model="queryParams.endpoint_name" :placeholder="$t('operationAudit.endpointNamePlaceholder')" clearable />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="24" :md="12" :lg="8">
            <el-form-item :label="$t('audit.dateRange')">
              <el-date-picker v-model="dateRange" type="daterange"
                :start-placeholder="$t('audit.startDate')" :end-placeholder="$t('audit.endDate')"
                value-format="YYYY-MM-DD" style="width: 100%" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row>
          <el-col :span="24" class="form-actions">
            <el-button @click="handleReset">{{ $t('common.reset') }}</el-button>
            <el-button type="primary" @click="handleQuery">{{ $t('common.search') }}</el-button>
          </el-col>
        </el-row>
      </el-form>
    </SearchCard>

    <!-- 数据表格 -->
    <el-card shadow="never">
      <TableToolbar :total="total" :show-column-setting="false" :show-export="false" @refresh="handleQuery" />

      <el-table v-loading="loading" :data="auditLogs" border stripe style="width: 100%">
        <el-table-column prop="id" label="ID" width="80" align="center" />
        <el-table-column prop="operation_type" :label="$t('operationAudit.operationType')" width="150">
          <template #default="{ row }">
            <el-tag size="small">{{ getOperationLabel(row.operation_type) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="agent_name" :label="$t('operationAudit.agentName')" width="120" />
        <el-table-column prop="client_name" :label="$t('operationAudit.clientName')" width="120" />
        <el-table-column prop="endpoint_name" :label="$t('operationAudit.endpointName')" width="140">
          <template #default="{ row }">
            <span v-if="row.endpoint_name">{{ row.endpoint_name }}</span>
            <span v-else class="cell-sub">-</span>
          </template>
        </el-table-column>
        <el-table-column prop="target" :label="$t('operationAudit.target')" min-width="180" />
        <el-table-column prop="detail" :label="$t('operationAudit.detail')" min-width="200">
          <template #default="{ row }">
            <span v-if="row.detail">{{ row.detail }}</span>
            <span v-else class="cell-sub">-</span>
          </template>
        </el-table-column>
        <el-table-column prop="duration_ms" :label="$t('operationAudit.duration')" width="100" align="center">
          <template #default="{ row }">
            {{ row.duration_ms > 0 ? `${row.duration_ms}ms` : '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="started_at" :label="$t('operationAudit.startedAt')" width="180">
          <template #default="{ row }">{{ formatTime(row.started_at) }}</template>
        </el-table-column>
      </el-table>

      <EmptyState v-if="!loading && auditLogs.length === 0" @action="handleQuery" />

      <el-pagination v-if="auditLogs.length > 0"
        v-model:current-page="queryParams.page" v-model:page-size="queryParams.size"
        :total="total" :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="handleQuery" @current-change="handleQuery" class="pagination" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { getOperationAuditList, getOperationTypes as fetchOperationTypes, type OperationAuditItem, type OperationType, type OperationAuditParams } from '@/api/operationAudit'
import { formatTime } from '@/utils/time'
import { useI18n } from 'vue-i18n'
import SearchCard from '@/components/Common/SearchCard.vue'
import TableToolbar from '@/components/Common/TableToolbar.vue'
import EmptyState from '@/components/Common/EmptyState.vue'
import PageHeader from '@/components/Common/PageHeader.vue'

const { t } = useI18n()
const loading = ref(false)
const auditLogs = ref<OperationAuditItem[]>([])
const total = ref(0)
const dateRange = ref<[string, string] | null>(null)
const operationTypes = ref<OperationType[]>([])

const queryParams = reactive<OperationAuditParams>({
  operation_type: '',
  endpoint_name: '',
  start_date: '',
  end_date: '',
  page: 1,
  size: 50
})

const getOperationLabel = (type: string) => {
  const found = operationTypes.value.find(item => item.value === type)
  return found ? found.label : type
}

const loadOperationTypes = async () => {
  try {
    const response = await fetchOperationTypes()
    if (response.success) { operationTypes.value = response.data || [] }
  } catch (error) { console.error('加载操作类型失败:', error) }
}

const handleQuery = async () => {
  loading.value = true
  try {
    if (dateRange.value) { queryParams.start_date = dateRange.value[0]; queryParams.end_date = dateRange.value[1] }
    else { queryParams.start_date = ''; queryParams.end_date = '' }

    const response = await getOperationAuditList(queryParams)
    if (response.success) { auditLogs.value = response.data || []; total.value = response.total || 0 }
    else { ElMessage.error(response.message || t('audit.queryFailed')) }
  } catch (error) { console.error('查询失败:', error); ElMessage.error(t('audit.queryFailed')) }
  finally { loading.value = false }
}

const handleReset = () => {
  queryParams.operation_type = ''; queryParams.endpoint_name = ''
  queryParams.start_date = ''; queryParams.end_date = ''
  queryParams.page = 1; queryParams.size = 50
  dateRange.value = null; handleQuery()
}

onMounted(() => { loadOperationTypes(); handleQuery() })
</script>

<style scoped>
.operation-audit-page { padding: 0; }
.search-form { width: 100%; }
.search-form :deep(.el-form-item) { width: 100%; margin-bottom: 16px; }
.search-form :deep(.el-form-item__label) { width: 100px; }
.search-form :deep(.el-form-item__content) { flex: 1; }
.search-form :deep(.el-input), .search-form :deep(.el-select) { width: 100%; }
.form-actions { text-align: right; margin-top: 8px; }
.cell-sub { color: #909399; font-size: 12px; }
.pagination { margin-top: 20px; display: flex; justify-content: flex-end; }
</style>
