<template>
  <div class="audit-logs-page">
    <!-- 搜索筛选区域 -->
    <SearchCard :title="t('audit.title')">
      <el-form :inline="true" :model="queryParams" class="search-form">
        <el-row :gutter="20">
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item :label="t('audit.actionType')">
              <el-select
                v-model="queryParams.action_type"
                :placeholder="t('audit.actionTypePlaceholder')"
                clearable
              >
                <el-option
                  v-for="item in actionTypes"
                  :key="item.value"
                  :label="item.label"
                  :value="item.value"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item :label="t('audit.user')">
              <el-select
                v-model="queryParams.user_id"
                :placeholder="t('audit.userPlaceholder')"
                clearable
              >
                <el-option
                  v-for="item in adminList"
                  :key="item.id"
                  :label="item.username"
                  :value="item.id"
                />
              </el-select>
            </el-form-item>
          </el-col>
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
          prop="action_type"
          :label="t('audit.actionType')"
          width="150"
        >
          <template #default="{ row }">
            <el-tag size="small">{{ getActionLabel(row.action_type) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column
          prop="actor_name"
          :label="t('audit.actor')"
          width="120"
        />
        <el-table-column
          prop="target_name"
          :label="t('audit.target')"
          min-width="150"
        />
        <el-table-column
          prop="detail"
          :label="t('audit.detail')"
          min-width="250"
        >
          <template #default="{ row }">
            <span v-if="row.detail">{{ row.detail }}</span>
            <span v-else class="cell-sub">-</span>
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
        v-model:page-size="queryParams.size"
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
import { queryAuditLogs, getActionTypes, getAdminList, type QueryAuditLogsParams, type AuditLog, type ActionType, type AdminOption } from '@/api/audit'
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
const actionTypes = ref<ActionType[]>([])
const adminList = ref<AdminOption[]>([])

const queryParams = reactive<QueryAuditLogsParams>({
  action_type: '',
  user_id: undefined,
  start_date: '',
  end_date: '',
  page: 1,
  size: 50
})

// 获取操作类型标签
const getActionLabel = (actionType: string) => {
  const found = actionTypes.value.find(item => item.value === actionType)
  return found ? found.label : actionType
}

// 加载操作类型列表
const loadActionTypes = async () => {
  try {
    const response = await getActionTypes()
    if (response.success) {
      actionTypes.value = response.data || []
    }
  } catch (error) {
    console.error('Load action types error:', error)
  }
}

// 加载管理员列表
const loadAdminList = async () => {
  try {
    const response = await getAdminList()
    if (response.success) {
      adminList.value = response.data || []
    }
  } catch (error) {
    console.error('Load admin list error:', error)
  }
}

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
      auditLogs.value = response.data || []
      total.value = response.total || 0
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
  queryParams.action_type = ''
  queryParams.user_id = undefined
  queryParams.start_date = ''
  queryParams.end_date = ''
  queryParams.page = 1
  queryParams.size = 50
  dateRange.value = null
  handleQuery()
}

onMounted(() => {
  loadActionTypes()
  loadAdminList()
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

.cell-sub {
  color: #909399;
  font-size: 12px;
}

.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}

@media (max-width: 768px) {
  .search-form :deep(.el-form-item__label) {
    width: 80px;
  }
  
  .form-actions {
    text-align: center;
  }
}
</style>
