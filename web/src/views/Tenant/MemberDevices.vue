<template>
  <div class="page-container">
    <div class="page-header">
      <div>
        <h1>成员设备</h1>
        <p>设备归属实名用户个人。此处展示当前租户有效成员登录过的 Desktop 设备，支持在机主显式开启共享许可的前提下签发带有效期的同租户远程桌面协助授权。</p>
      </div>
      <el-button :loading="loading" @click="fetchDevices">刷新</el-button>
    </div>

    <div class="filters">
      <el-input
        v-model="search"
        clearable
        placeholder="搜索成员、设备或主机名"
        @keyup.enter="applySearch"
        @clear="applySearch"
      />
    </div>

    <el-table v-loading="loading" :data="devices" empty-text="当前租户还没有成员设备">
      <el-table-column label="成员" min-width="170">
        <template #default="scope">
          <strong>{{ scope.row.user_alias || scope.row.user_name }}</strong>
          <div class="secondary">{{ scope.row.user_name }}</div>
        </template>
      </el-table-column>

      <el-table-column label="设备" min-width="300">
        <template #default="scope">
          <div class="device-name">{{ scope.row.device_name || '-' }}</div>
          <div class="device-details">
            <span>主机名：{{ scope.row.hostname || '-' }}</span>
            <span>IP：{{ scope.row.ip || '-' }}</span>
            <span>版本：{{ scope.row.version || '-' }}</span>
          </div>
        </template>
      </el-table-column>

      <el-table-column label="在线状态" width="105">
        <template #default="scope">
          <el-tag size="small" effect="plain" :type="scope.row.online ? 'success' : 'info'">
            {{ scope.row.online ? '在线' : '离线' }}
          </el-tag>
        </template>
      </el-table-column>

      <el-table-column label="远程桌面与共享隐私" min-width="250">
        <template #default="{ row }">
          <div class="rdp-tags">
            <el-tag size="small" effect="plain" :type="rdpStatusTag(row).type">
              {{ rdpStatusTag(row).label }}
            </el-tag>
            <el-tag
              size="small"
              effect="plain"
              :type="row.rdp_allow_share && !row.rdp_admin_disabled ? 'warning' : 'info'"
            >
              {{ row.rdp_allow_share ? '机主允许临时共享' : '仅限机主本人' }}
            </el-tag>
            <el-tag v-if="row.active_grant_count" size="small" type="primary" effect="light">
              生效授权 {{ row.active_grant_count }}
            </el-tag>
          </div>
        </template>
      </el-table-column>

      <el-table-column label="最后心跳" min-width="170">
        <template #default="scope">{{ formatTime(scope.row.last_heartbeat) }}</template>
      </el-table-column>

      <el-table-column label="操作" width="150" fixed="right">
        <template #default="{ row }">
          <el-button type="primary" link @click="openGrantDialog(row)">
            {{ canWriteGrants ? 'RDP 临时授权' : '查看授权记录' }}
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <div class="pagination">
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="size"
        layout="total, sizes, prev, pager, next"
        :total="total"
        @current-change="fetchDevices"
        @size-change="resetAndFetch"
      />
    </div>

    <!-- RDP 临时授权对话框 -->
    <el-dialog
      v-model="grantDialogVisible"
      :title="selectedDevice ? `远程桌面临时协助授权 — ${selectedDevice.device_name}` : '远程桌面临时协助授权'"
      width="740px"
      @closed="resetGrantDialog"
    >
      <template v-if="selectedDevice">
        <div class="dialog-device-summary">
          <span><strong>设备归属人：</strong>{{ selectedDevice.user_alias || selectedDevice.user_name }} ({{ selectedDevice.user_name }})</span>
          <span><strong>隧道 IP：</strong>{{ selectedDevice.ip || '-' }}</span>
          <span><strong>机主模式：</strong>{{ formatRDPMode(selectedDevice.rdp_mode) }}</span>
        </div>

        <el-alert
          v-if="!canIssueGrantForSelected"
          type="warning"
          :closable="false"
          show-icon
          class="consent-alert"
        >
          <template #title>
            {{ cannotIssueReason }}
          </template>
        </el-alert>

        <el-form
          v-if="canWriteGrants && canIssueGrantForSelected"
          label-position="top"
          class="grant-form"
        >
          <div class="grant-form-grid">
            <el-form-item label="被授权同租户成员">
              <el-select
                v-model="grantForm.granteeUserId"
                filterable
                clearable
                :loading="membersLoading"
                placeholder="选择需要临时远程协助的租户成员"
                style="width: 100%"
              >
                <el-option
                  v-for="m in eligibleGrantees"
                  :key="m.user_id"
                  :label="m.alias ? `${m.alias} (${m.name})` : m.name"
                  :value="m.user_id"
                />
              </el-select>
            </el-form-item>

            <el-form-item label="授权有效时长（到期自动断连回收）">
              <el-select v-model="grantForm.durationMinutes" style="width: 100%">
                <el-option label="1 小时" :value="60" />
                <el-option label="2 小时（推荐）" :value="120" />
                <el-option label="4 小时" :value="240" />
                <el-option label="8 小时" :value="480" />
                <el-option label="24 小时" :value="1440" />
                <el-option label="72 小时" :value="4320" />
              </el-select>
            </el-form-item>
          </div>

          <div class="grant-reason-row">
            <el-input
              v-model="grantForm.reason"
              maxlength="200"
              placeholder="选填：输入临时远程协助或联调原因（记入租户审计日志）"
            />
            <el-button
              type="primary"
              :loading="creatingGrant"
              :disabled="!grantForm.granteeUserId"
              @click="submitCreateGrant"
            >
              签发临时授权
            </el-button>
          </div>
        </el-form>

        <div class="grants-list-header">
          <strong>当前与近期临时授权记录</strong>
          <el-button size="small" link :loading="grantsLoading" @click="loadDeviceGrants">刷新</el-button>
        </div>

        <el-table v-loading="grantsLoading" :data="deviceGrants" size="small" empty-text="暂无跨人临时授权记录">
          <el-table-column label="被授权成员" min-width="150">
            <template #default="{ row }">
              <span>{{ row.grantee_alias || row.grantee_user_name || `UID:${row.grantee_user_id}` }}</span>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="95">
            <template #default="{ row }">
              <el-tag
                size="small"
                :type="row.status === 'active' ? 'success' : row.status === 'expired' ? 'info' : 'warning'"
              >
                {{ row.status === 'active' ? '生效中' : row.status === 'expired' ? '已到期' : '已撤销' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="到期时间" min-width="165">
            <template #default="{ row }">{{ formatTime(row.expires_at) }}</template>
          </el-table-column>
          <el-table-column label="原因 / 备注" min-width="160">
            <template #default="{ row }">{{ row.revoke_reason || row.reason || '-' }}</template>
          </el-table-column>
          <el-table-column v-if="canWriteGrants" label="操作" width="85" fixed="right">
            <template #default="{ row }">
              <el-button
                v-if="row.status === 'active'"
                type="danger"
                link
                size="small"
                :loading="revokingGrantId === row.id"
                @click="handleRevokeGrant(row)"
              >
                立即撤销
              </el-button>
              <span v-else class="secondary">-</span>
            </template>
          </el-table-column>
        </el-table>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  createTenantDeviceRDPGrant,
  getTenantDeviceRDPGrants,
  getTenantMemberDevices,
  revokeTenantDeviceRDPGrant,
  type TenantDeviceRDPGrant,
  type TenantMemberDevice,
} from '@/api/tenantBusiness'
import { getTenantMembers, type TenantMember } from '@/api/resource'
import { useTenantStore } from '@/stores/tenant'

const tenantStore = useTenantStore()
const loading = ref(false)
const devices = ref<TenantMemberDevice[]>([])
const search = ref('')
const page = ref(1)
const size = ref(20)
const total = ref(0)

const canWriteGrants = computed(() => tenantStore.canTenant('tenant.grants.write'))

const grantDialogVisible = ref(false)
const selectedDevice = ref<TenantMemberDevice | null>(null)
const deviceGrants = ref<TenantDeviceRDPGrant[]>([])
const grantsLoading = ref(false)
const membersLoading = ref(false)
const tenantMembers = ref<TenantMember[]>([])
const creatingGrant = ref(false)
const revokingGrantId = ref<number | null>(null)

const grantForm = reactive({
  granteeUserId: undefined as number | undefined,
  durationMinutes: 120,
  reason: '',
})

const formatTime = (value?: string) => (value ? new Date(value).toLocaleString() : '从未在线')

const formatRDPMode = (mode?: string) => {
  if (mode === 'enabled') return '强制开启'
  if (mode === 'disabled') return '已关闭'
  return '自动开放'
}

const rdpStatusTag = (row: TenantMemberDevice): { label: string; type: 'success' | 'warning' | 'danger' | 'info' } => {
  if (row.rdp_admin_disabled) return { label: '平台已熔断', type: 'danger' }
  if (row.rdp_mode === 'disabled') return { label: 'RDP 已关闭', type: 'info' }
  if (!row.online) return { label: '设备离线', type: 'info' }
  const port = row.rdp_local_port || 3389
  if (row.rdp_tunnel_exposed) return { label: `RDP 就绪 (:${port})`, type: 'success' }
  if (row.rdp_local_ready) return { label: `本机监听 (:${port})`, type: 'warning' }
  return { label: '未监听 RDP', type: 'info' }
}

const canIssueGrantForSelected = computed(() => {
  const d = selectedDevice.value
  if (!d) return false
  if (d.rdp_admin_disabled) return false
  if (!d.rdp_allow_share) return false
  if (d.rdp_mode === 'disabled') return false
  return true
})

const cannotIssueReason = computed(() => {
  const d = selectedDevice.value
  if (!d) return ''
  if (d.rdp_admin_disabled) {
    return '平台管理员已对该设备启用远程桌面紧急熔断，当前禁止签发新的临时授权。'
  }
  if (!d.rdp_allow_share) {
    return '隐私保护生效中：该设备归属人尚未在 Signal Desktop 开启“允许同租户临时共享”，租户管理员无权越权代开跨人远程桌面。'
  }
  if (d.rdp_mode === 'disabled') {
    return '该设备归属人当前已主动关闭本机远程桌面暴露，需机主本人开启后方可签发临时协助授权。'
  }
  return ''
})

const eligibleGrantees = computed(() => {
  const ownerId = selectedDevice.value?.user_id
  return tenantMembers.value.filter(m => m.enabled && m.user_id !== ownerId)
})

const fetchDevices = async () => {
  if (!tenantStore.tenantId) return
  loading.value = true
  try {
    const response = await getTenantMemberDevices(tenantStore.tenantId, {
      search: search.value || undefined,
      page: page.value,
      size: size.value,
    })
    devices.value = response.success && response.data ? response.data : []
    total.value = response.total || 0
  } finally {
    loading.value = false
  }
}

const loadTenantMembersIfNeeded = async () => {
  if (!tenantStore.tenantId || !canWriteGrants.value) return
  membersLoading.value = true
  try {
    const res = await getTenantMembers(tenantStore.tenantId)
    tenantMembers.value = res.success && res.data ? res.data : []
  } finally {
    membersLoading.value = false
  }
}

const loadDeviceGrants = async () => {
  if (!tenantStore.tenantId || !selectedDevice.value) return
  grantsLoading.value = true
  try {
    const res = await getTenantDeviceRDPGrants(tenantStore.tenantId, selectedDevice.value.node_id)
    deviceGrants.value = res.success && res.data ? res.data : []
  } finally {
    grantsLoading.value = false
  }
}

const openGrantDialog = async (device: TenantMemberDevice) => {
  selectedDevice.value = device
  grantDialogVisible.value = true
  await Promise.all([loadDeviceGrants(), loadTenantMembersIfNeeded()])
}

const resetGrantDialog = () => {
  selectedDevice.value = null
  deviceGrants.value = []
  grantForm.granteeUserId = undefined
  grantForm.durationMinutes = 120
  grantForm.reason = ''
}

const submitCreateGrant = async () => {
  if (!tenantStore.tenantId || !selectedDevice.value || !grantForm.granteeUserId) return
  creatingGrant.value = true
  try {
    const res = await createTenantDeviceRDPGrant(tenantStore.tenantId, selectedDevice.value.node_id, {
      grantee_user_id: grantForm.granteeUserId,
      duration_minutes: grantForm.durationMinutes,
      reason: grantForm.reason.trim() || undefined,
    })
    if (res.success) {
      ElMessage.success('临时远程桌面授权已签发并下发同步')
      grantForm.granteeUserId = undefined
      grantForm.reason = ''
      await Promise.all([loadDeviceGrants(), fetchDevices()])
    }
  } finally {
    creatingGrant.value = false
  }
}

const handleRevokeGrant = async (grant: TenantDeviceRDPGrant) => {
  if (!tenantStore.tenantId || !selectedDevice.value) return
  try {
    await ElMessageBox.confirm(
      `确定立即撤销授予 "${grant.grantee_alias || grant.grantee_user_name}" 的临时远程桌面权限吗？正在进行中的远程桌面会话将被立即强制断开。`,
      '撤销临时授权',
      { type: 'warning', confirmButtonText: '立即撤销', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  revokingGrantId.value = grant.id
  try {
    const res = await revokeTenantDeviceRDPGrant(tenantStore.tenantId, selectedDevice.value.node_id, grant.id)
    if (res.success) {
      ElMessage.success('已撤销临时授权并通知被控端立即断开连接')
      await Promise.all([loadDeviceGrants(), fetchDevices()])
    }
  } finally {
    revokingGrantId.value = null
  }
}

const applySearch = () => {
  page.value = 1
  fetchDevices()
}
const resetAndFetch = () => {
  page.value = 1
  fetchDevices()
}
onMounted(fetchDevices)
watch(
  () => tenantStore.contextRevision,
  () => {
    search.value = ''
    page.value = 1
    fetchDevices()
  }
)
</script>

<style scoped>
.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  margin-bottom: 18px;
}
h1 {
  margin: 0;
  color: var(--text-primary);
  font-size: 24px;
  line-height: 32px;
}
.page-header p {
  margin: 5px 0 0;
  color: var(--text-secondary);
  font-size: 13px;
}
.filters {
  display: flex;
  width: min(420px, 100%);
  margin-bottom: 12px;
}
.secondary {
  margin-top: 2px;
  color: var(--text-secondary);
  font-size: 12px;
}
.device-name {
  color: var(--text-primary);
  font-weight: 600;
}
.device-details {
  display: flex;
  flex-wrap: wrap;
  gap: 2px 16px;
  margin-top: 4px;
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 18px;
}
.rdp-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}
.pagination {
  display: flex;
  justify-content: flex-end;
  padding-top: 14px;
}
.dialog-device-summary {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 24px;
  padding: 10px 14px;
  background: var(--el-fill-color-light);
  border-radius: 6px;
  font-size: 13px;
  margin-bottom: 14px;
}
.consent-alert {
  margin-bottom: 16px;
}
.grant-form {
  padding: 14px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  margin-bottom: 16px;
}
.grant-form-grid {
  display: grid;
  grid-template-columns: 1fr 220px;
  gap: 14px;
}
.grant-reason-row {
  display: flex;
  gap: 10px;
  align-items: center;
}
.grants-list-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
  font-size: 13px;
}
</style>
