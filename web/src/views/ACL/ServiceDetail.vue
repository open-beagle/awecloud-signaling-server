<template>
  <div class="acl-service-detail">
    <!-- 基本信息 -->
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>{{ $t('acl.serviceInfo') }}</span>
          <el-button type="primary" link @click="router.back()">{{ $t('common.back') }}</el-button>
        </div>
      </template>
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="$t('acl.serviceName')">{{ service?.name }}</el-descriptions-item>
        <el-descriptions-item :label="$t('acl.ownerUser')">{{ service?.user_name }}</el-descriptions-item>
        <el-descriptions-item :label="$t('acl.sourceAddr')">{{ service?.source_addr }}</el-descriptions-item>
        <el-descriptions-item :label="$t('acl.targetAddr')">{{ service?.target_addr }}</el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 用户授权 -->
    <el-card shadow="never" style="margin-top: 20px">
      <template #header>
        <div class="card-header">
          <span>{{ $t('acl.userAuth') }} ({{ service?.users?.length || 0 }})</span>
          <el-button type="primary" size="small" @click="showAddUserDialog = true">
            <el-icon><Plus /></el-icon>
            {{ $t('acl.addUser') }}
          </el-button>
        </div>
      </template>
      <el-table :data="service?.users || []" stripe>
        <el-table-column prop="name" :label="$t('acl.userName')" min-width="150" />
        <el-table-column prop="alias" :label="$t('user.alias')" min-width="120" />
        <el-table-column prop="granted_at" :label="$t('acl.grantedAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.granted_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" link size="small" @click="handleRevokeUser(row)">{{ $t('acl.revoke') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 分组授权 -->
    <el-card shadow="never" style="margin-top: 20px">
      <template #header>
        <div class="card-header">
          <span>{{ $t('acl.groupAuth') }} ({{ service?.groups?.length || 0 }})</span>
          <el-button type="primary" size="small" @click="showAddGroupDialog = true">
            <el-icon><Plus /></el-icon>
            {{ $t('acl.addGroup') }}
          </el-button>
        </div>
      </template>
      <el-table :data="service?.groups || []" stripe>
        <el-table-column prop="name" :label="$t('acl.groupName')" min-width="150" />
        <el-table-column prop="alias" :label="$t('group.description')" min-width="120" />
        <el-table-column prop="granted_at" :label="$t('acl.grantedAt')" width="180">
          <template #default="{ row }">
            {{ formatTime(row.granted_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('common.actions')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button type="danger" link size="small" @click="handleRevokeGroup(row)">{{ $t('acl.revoke') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 添加用户授权弹窗 -->
    <AddUserDialog v-model="showAddUserDialog" :service-id="serviceId" @success="fetchService" />
    
    <!-- 添加分组授权弹窗 -->
    <AddGroupDialog v-model="showAddGroupDialog" :service-id="serviceId" @success="fetchService" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { getServiceACL, removeServiceACLUser, removeServiceACLGroup, type ServiceACLDetail, type ACLPermissionItem } from '@/api/acl'
import { formatTime } from '@/utils/time'
import AddUserDialog from './components/AddUserDialog.vue'
import AddGroupDialog from './components/AddGroupDialog.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const serviceId = route.params.id as string
const service = ref<ServiceACLDetail | null>(null)
const showAddUserDialog = ref(false)
const showAddGroupDialog = ref(false)

// 获取服务详情
const fetchService = async () => {
  try {
    const res = await getServiceACL(serviceId)
    if (res.success && res.data) {
      service.value = res.data
    }
  } catch (error) {
    console.error('获取服务详情失败:', error)
  }
}

// 撤销用户授权
const handleRevokeUser = async (row: ACLPermissionItem) => {
  try {
    await ElMessageBox.confirm(
      t('acl.revokeUserConfirm', { name: row.name }),
      t('common.warning'),
      { type: 'warning' }
    )
    const res = await removeServiceACLUser(serviceId, row.id)
    if (res.success) {
      ElMessage.success(t('acl.revokeSuccess'))
      fetchService()
    } else {
      ElMessage.error(res.message || t('acl.revokeFailed'))
    }
  } catch (error) {
    if (error !== 'cancel') {
      console.error('撤销授权失败:', error)
    }
  }
}

// 撤销分组授权
const handleRevokeGroup = async (row: ACLPermissionItem) => {
  try {
    await ElMessageBox.confirm(
      t('acl.revokeGroupConfirm', { name: row.name }),
      t('common.warning'),
      { type: 'warning' }
    )
    const res = await removeServiceACLGroup(serviceId, row.id)
    if (res.success) {
      ElMessage.success(t('acl.revokeSuccess'))
      fetchService()
    } else {
      ElMessage.error(res.message || t('acl.revokeFailed'))
    }
  } catch (error) {
    if (error !== 'cancel') {
      console.error('撤销授权失败:', error)
    }
  }
}

onMounted(() => {
  fetchService()
})
</script>

<style scoped>
.acl-service-detail {
  width: 100%;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>
