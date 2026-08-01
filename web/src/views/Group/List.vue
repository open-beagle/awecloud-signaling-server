<template>
  <div class="group-list">
    <PageHeader title="成员分组" description="管理当前租户的成员分组及分组成员。">
      <template #actions>
        <el-button type="primary" :icon="Plus" :disabled="!canCreate" @click="showCreateDialog = true">{{ $t('group.create') }}</el-button>
      </template>
    </PageHeader>

    <!-- 搜索 -->
    <el-card class="search-card" shadow="never">
      <el-form :inline="true" :model="searchForm" class="search-form">
        <el-form-item :label="$t('common.search')">
          <el-input v-model="searchForm.search" :placeholder="$t('group.searchPlaceholder')" clearable style="width: 240px" @keyup.enter="handleSearch" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">{{ $t('common.search') }}</el-button>
          <el-button @click="handleReset">{{ $t('common.reset') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 分组列表 -->
    <el-card shadow="never">
      <el-table v-loading="loading" :data="groups" stripe>
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" :label="$t('group.name')" min-width="150">
          <template #default="{ row }">
            <el-link type="primary" :underline="false" @click="handleViewMembers(row)">{{ row.name }}</el-link>
          </template>
        </el-table-column>
        <el-table-column prop="description" :label="$t('group.description')" min-width="200" />
        <el-table-column label="客户范围" min-width="150"><template #default="{ row }"><span>{{ tenantName(row.tenant_id) }}</span></template></el-table-column>
        <el-table-column prop="member_count" :label="$t('group.memberCount')" width="100" align="center">
          <template #default="{ row }">
            <el-tag type="primary" size="small">{{ row.member_count || 0 }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" :label="$t('common.createdAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="200" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="handleViewMembers(row)">{{ $t('group.members') }}</el-button>
            <el-button type="primary" link size="small" :disabled="!canManage(row)" @click="handleEdit(row)">{{ $t('common.edit') }}</el-button>
            <el-button type="danger" link size="small" :disabled="!canManage(row)" @click="handleDelete(row)">{{ $t('common.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <div class="pagination-wrapper">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.size"
          :total="pagination.total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="fetchGroups"
          @current-change="fetchGroups"
        />
      </div>
    </el-card>

    <!-- 创建/编辑分组弹窗 -->
    <el-dialog v-model="showCreateDialog" :title="editingGroup ? $t('group.edit') : $t('group.create')" width="500px" @close="handleCloseDialog">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="80px">
        <el-form-item :label="$t('group.name')" prop="name">
          <el-input v-model="form.name" :placeholder="$t('group.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('group.description')" prop="description">
          <el-input v-model="form.description" type="textarea" :rows="3" :placeholder="$t('group.descriptionPlaceholder')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="handleCloseDialog">{{ $t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="handleSubmit">{{ $t('common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, reactive, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import type { FormInstance, FormRules } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { getGroups, createGroup, updateGroup, deleteGroup, type Group } from '@/api/group'
import { formatTime } from '@/utils/time'
import { useAuthStore } from '@/stores/auth'
import { useTenantStore } from '@/stores/tenant'
import { getTenants, type Tenant } from '@/api/resource'
import PageHeader from '@/components/Common/PageHeader.vue'

const { t } = useI18n()
const router = useRouter()
const authStore = useAuthStore()
const tenantStore = useTenantStore()
const tenants = ref<Tenant[]>([])
const canCreate = computed(() => authStore.canWrite && (authStore.isPlatformAdmin || !!tenantStore.tenantId))

const loading = ref(false)
const groups = ref<Group[]>([])
const showCreateDialog = ref(false)
const submitting = ref(false)
const editingGroup = ref<Group | null>(null)
const formRef = ref<FormInstance>()

const searchForm = reactive({
  search: ''
})

const pagination = reactive({
  page: 1,
  size: 20,
  total: 0
})

const form = reactive({
  name: '',
  description: ''
})

const rules: FormRules = {
  name: [{ required: true, message: t('group.nameRequired'), trigger: 'blur' }]
}

// 获取分组列表
const fetchGroups = async () => {
  loading.value = true
  try {
    const res = await getGroups({
      tenant_id: tenantStore.tenantId || undefined,
      search: searchForm.search || undefined,
      page: pagination.page,
      size: pagination.size
    })
    if (res.success && res.data) {
      groups.value = res.data
      pagination.total = res.total
    }
  } catch (error) {
    console.error('获取分组列表失败:', error)
  } finally {
    loading.value = false
  }
}

// 搜索
const handleSearch = () => {
  pagination.page = 1
  fetchGroups()
}

// 重置
const handleReset = () => {
  searchForm.search = ''
  pagination.page = 1
  fetchGroups()
}

// 查看成员
const handleViewMembers = (row: Group) => {
  router.push(`/groups/${row.id}/members`)
}

// 编辑
const handleEdit = (row: Group) => {
  editingGroup.value = row
  form.name = row.name
  form.description = row.description || ''
  showCreateDialog.value = true
}

// 删除
const handleDelete = async (row: Group) => {
  try {
    await ElMessageBox.confirm(
      t('group.deleteConfirm', { name: row.name }),
      t('common.warning'),
      { type: 'warning' }
    )
    const res = await deleteGroup(row.id)
    if (res.success) {
      ElMessage.success(t('common.deleteSuccess'))
      fetchGroups()
    } else {
      ElMessage.error(res.message || t('common.deleteFailed'))
    }
  } catch (error) {
    if (error !== 'cancel') {
      console.error('删除分组失败:', error)
    }
  }
}

// 提交表单
const handleSubmit = async () => {
  if (!formRef.value) return
  
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    
    submitting.value = true
    try {
      let res
      if (editingGroup.value) {
        res = await updateGroup(editingGroup.value.id, form)
      } else {
        res = await createGroup({ ...form, tenant_id: tenantStore.tenantId || undefined })
      }
      
      if (res.success) {
        ElMessage.success(editingGroup.value ? t('common.updateSuccess') : t('common.createSuccess'))
        handleCloseDialog()
        fetchGroups()
      } else {
        ElMessage.error(res.message || t('common.failed'))
      }
    } catch (error) {
      console.error('操作失败:', error)
    } finally {
      submitting.value = false
    }
  })
}

const canManage = (group: Group) => authStore.canWrite && (group.tenant_id ? tenantStore.tenantId === group.tenant_id : authStore.isPlatformAdmin)
const tenantName = (tenantId?: string) => tenantId ? (tenants.value.find(tenant => tenant.id === tenantId)?.name || tenantId) : '平台全局（旧版）'

// 关闭弹窗
const handleCloseDialog = () => {
  showCreateDialog.value = false
  editingGroup.value = null
  form.name = ''
  form.description = ''
  formRef.value?.resetFields()
}

onMounted(() => {
  fetchGroups()
  getTenants({ page: 1, size: 100 }).then(res => { if (res.success && res.data) tenants.value = res.data })
})
watch(() => tenantStore.tenantId, () => { pagination.page = 1; fetchGroups() })
</script>

<style scoped>
.group-list {
  width: 100%;
}

.search-card {
  margin-bottom: 20px;
}

.search-form {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
}

.pagination-wrapper {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}
</style>
