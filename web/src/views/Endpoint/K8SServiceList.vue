<template>
  <div class="endpoint-list">
    <!-- 搜索 -->
    <el-card class="search-card" shadow="never">
      <el-form :inline="true" :model="searchForm" class="search-form">
        <el-form-item :label="$t('common.search')">
          <el-input v-model="searchForm.search" :placeholder="$t('endpoint.searchPlaceholder')" clearable style="width: 240px" @keyup.enter="handleSearch" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">{{ $t('common.search') }}</el-button>
          <el-button @click="handleReset">{{ $t('common.reset') }}</el-button>
          <el-button type="success" @click="showCreateDialog = true">{{ $t('common.create') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 列表 -->
    <el-card shadow="never">
      <el-table v-loading="loading" :data="list" stripe>
        <el-table-column prop="name" :label="$t('endpoint.name')" min-width="150" />
        <el-table-column prop="alias" :label="$t('endpoint.alias')" min-width="120">
          <template #default="{ row }">{{ row.alias || '-' }}</template>
        </el-table-column>
        <el-table-column prop="user_name" :label="$t('endpoint.ownerAgent')" min-width="120">
          <template #default="{ row }">{{ row.user_name || '-' }}</template>
        </el-table-column>
        <el-table-column prop="namespace" :label="$t('endpoint.namespace')" min-width="120" />
        <el-table-column prop="service_name" :label="$t('endpoint.serviceName')" min-width="140" />
        <el-table-column prop="target_port" :label="$t('endpoint.targetPort')" width="100" align="center" />
        <el-table-column prop="enabled" :label="$t('common.status')" width="80" align="center">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? $t('common.enabled') : $t('common.disabled') }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" :label="$t('common.createdAt')" width="180">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="handleEdit(row)">{{ $t('common.edit') }}</el-button>
            <el-popconfirm :title="$t('common.deleteConfirm')" @confirm="handleDelete(row)">
              <template #reference>
                <el-button type="danger" link size="small">{{ $t('common.delete') }}</el-button>
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>
      <div class="pagination-wrapper">
        <el-pagination v-model:current-page="pagination.page" v-model:page-size="pagination.size" :total="pagination.total" :page-sizes="[10, 20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @size-change="fetchList" @current-change="fetchList" />
      </div>
    </el-card>

    <!-- 创建/编辑弹窗 -->
    <el-dialog v-model="showCreateDialog" :title="editingItem ? $t('endpoint.editK8SService') : $t('endpoint.createK8SService')" width="500px" @closed="resetForm">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="120px">
        <el-form-item :label="$t('endpoint.ownerAgent')" prop="user_id">
          <el-select v-model="form.user_id" :placeholder="$t('endpoint.selectAgent')" filterable style="width: 100%">
            <el-option v-for="u in agents" :key="u.id" :label="u.alias || u.name" :value="u.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('endpoint.name')" prop="name">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item :label="$t('endpoint.alias')">
          <el-input v-model="form.alias" />
        </el-form-item>
        <el-form-item :label="$t('endpoint.namespace')" prop="namespace">
          <el-input v-model="form.namespace" placeholder="default" />
        </el-form-item>
        <el-form-item :label="$t('endpoint.serviceName')" prop="service_name">
          <el-input v-model="form.service_name" />
        </el-form-item>
        <el-form-item :label="$t('endpoint.targetPort')" prop="target_port">
          <el-input-number v-model="form.target_port" :min="1" :max="65535" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="handleSubmit">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import type { FormInstance } from 'element-plus'
import { ElMessage } from 'element-plus'
import { getEndpoints, createEndpoint, updateEndpoint, deleteEndpoint, type EndpointK8SService } from '@/api/endpoint'
import { getUsers } from '@/api/user'
import { formatTime } from '@/utils/time'

const { t } = useI18n()
const loading = ref(false)
const submitting = ref(false)
const list = ref<EndpointK8SService[]>([])
const agents = ref<{ id: number; name: string; alias?: string }[]>([])
const searchForm = reactive({ search: '' })
const pagination = reactive({ page: 1, size: 20, total: 0 })
const showCreateDialog = ref(false)
const editingItem = ref<EndpointK8SService | null>(null)
const formRef = ref<FormInstance>()

const form = reactive({ user_id: undefined as number | undefined, name: '', alias: '', namespace: '', service_name: '', target_port: 80 })
const rules = {
  user_id: [{ required: true, message: t('endpoint.selectAgent'), trigger: 'change' }],
  name: [{ required: true, message: t('endpoint.nameRequired'), trigger: 'blur' }],
  namespace: [{ required: true, message: t('endpoint.namespaceRequired'), trigger: 'blur' }],
  service_name: [{ required: true, message: t('endpoint.serviceNameRequired'), trigger: 'blur' }],
  target_port: [{ required: true, message: t('endpoint.targetPortRequired'), trigger: 'blur' }]
}

const fetchList = async () => {
  loading.value = true
  try {
    const res = await getEndpoints('k8sservice', { search: searchForm.search || undefined, page: pagination.page, size: pagination.size })
    if (res.success && res.data) { list.value = res.data; pagination.total = res.total }
  } catch (e) { console.error('获取 K8SService Endpoint 列表失败:', e) } finally { loading.value = false }
}

const fetchAgents = async () => {
  try {
    const res = await getUsers({ role: 'agent', size: 1000 })
    if (res.success && res.data) agents.value = res.data.map(u => ({ id: u.id, name: u.name, alias: u.alias }))
  } catch (e) { console.error('获取 Agent 列表失败:', e) }
}

const handleSearch = () => { pagination.page = 1; fetchList() }
const handleReset = () => { searchForm.search = ''; pagination.page = 1; fetchList() }

const handleEdit = (row: EndpointK8SService) => {
  editingItem.value = row
  form.user_id = row.user_id; form.name = row.name; form.alias = row.alias || ''; form.namespace = row.namespace; form.service_name = row.service_name; form.target_port = row.target_port
  showCreateDialog.value = true
}

const resetForm = () => {
  editingItem.value = null
  form.user_id = undefined; form.name = ''; form.alias = ''; form.namespace = ''; form.service_name = ''; form.target_port = 80
  formRef.value?.resetFields()
}

const handleSubmit = async () => {
  await formRef.value?.validate()
  submitting.value = true
  try {
    const data = { ...form }
    const res = editingItem.value
      ? await updateEndpoint('k8sservice', editingItem.value.id, data)
      : await createEndpoint('k8sservice', data)
    if (res.success) {
      ElMessage.success(editingItem.value ? t('common.updateSuccess') : t('common.createSuccess'))
      showCreateDialog.value = false; fetchList()
    }
  } catch (e) { console.error('提交失败:', e) } finally { submitting.value = false }
}

const handleDelete = async (row: EndpointK8SService) => {
  try {
    const res = await deleteEndpoint('k8sservice', row.id)
    if (res.success) { ElMessage.success(t('common.deleteSuccess')); fetchList() }
  } catch (e) { console.error('删除失败:', e) }
}

onMounted(() => { fetchList(); fetchAgents() })
</script>

<style scoped>
.endpoint-list { width: 100%; }
.search-card { margin-bottom: 20px; }
.search-form { display: flex; flex-wrap: wrap; align-items: center; }
.pagination-wrapper { margin-top: 20px; display: flex; justify-content: flex-end; }
</style>
