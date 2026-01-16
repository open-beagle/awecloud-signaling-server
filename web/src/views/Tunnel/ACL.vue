<template>
  <div class="tunnel-acl">
    <el-card>
      <template #header>
        <div class="card-header">
          <span class="card-title">ACL 管理</span>
          <div class="header-actions">
            <el-button :icon="Refresh" @click="loadACL">刷新</el-button>
            <el-button type="primary" @click="handleSave" :disabled="!isEdited">保存</el-button>
          </div>
        </div>
      </template>

      <!-- 视图切换 -->
      <div class="view-switch">
        <el-radio-group v-model="viewMode">
          <el-radio-button value="editor">编辑器</el-radio-button>
          <el-radio-button value="visual">可视化</el-radio-button>
        </el-radio-group>
        <el-button type="warning" :icon="RefreshRight" @click="handleSync">强制同步</el-button>
      </div>

      <!-- 编辑器视图 -->
      <div v-if="viewMode === 'editor'" class="editor-view">
        <div class="editor-container">
          <el-input
            v-model="policyContent"
            type="textarea"
            :rows="20"
            placeholder="ACL Policy JSON"
            @input="isEdited = true"
          />
        </div>
        <div class="editor-tip">
          <el-icon><Warning /></el-icon>
          警告：手动修改 ACL 可能导致与本地授权数据不一致，建议通过服务授权功能管理权限
        </div>
        <div class="sync-info" v-if="lastSyncedAt">
          最后同步时间: {{ formatDate(lastSyncedAt) }}
        </div>
      </div>

      <!-- 可视化视图 -->
      <div v-else class="visual-view">
        <h4>ACL 规则列表</h4>
        <el-table :data="aclRules" stripe border>
          <el-table-column prop="index" label="#" width="60" />
          <el-table-column label="来源" min-width="200">
            <template #default="{ row }">
              <el-tag v-for="src in row.src" :key="src" size="small" style="margin: 2px">
                {{ src }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="目标" min-width="200">
            <template #default="{ row }">
              <el-tag v-for="dst in row.dst" :key="dst" size="small" type="info" style="margin: 2px">
                {{ dst }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="action" label="动作" width="80">
            <template #default="{ row }">
              <el-tag :type="row.action === 'accept' ? 'success' : 'danger'" size="small">
                {{ row.action === 'accept' ? '允许' : '拒绝' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="description" label="说明" min-width="150" />
        </el-table>
        <div v-if="aclRules.length === 0" class="empty-tip">暂无 ACL 规则</div>

        <h4 style="margin-top: 24px">Tag Owners</h4>
        <el-table :data="tagOwners" stripe border>
          <el-table-column prop="tag" label="Tag" min-width="200" />
          <el-table-column label="Owners" min-width="300">
            <template #default="{ row }">
              <el-tag v-for="owner in row.owners" :key="owner" size="small" style="margin: 2px">
                {{ owner }}
              </el-tag>
            </template>
          </el-table-column>
        </el-table>
        <div v-if="tagOwners.length === 0" class="empty-tip">暂无 Tag Owners</div>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, RefreshRight, Warning } from '@element-plus/icons-vue'
import {
  getTunnelACL,
  updateTunnelACL,
  getTunnelACLRules,
  syncTunnelACL,
  type ACLRule,
  type TagOwner
} from '@/api/tunnel'

const viewMode = ref<'editor' | 'visual'>('editor')
const policyContent = ref('')
const lastSyncedAt = ref('')
const isEdited = ref(false)

const aclRules = ref<ACLRule[]>([])
const tagOwners = ref<TagOwner[]>([])

const formatDate = (dateStr: string) => {
  if (!dateStr) return '从未同步'
  const date = new Date(dateStr)
  // 检查是否为有效日期且不是零值
  if (isNaN(date.getTime()) || date.getFullYear() < 2000) {
    return '从未同步'
  }
  return date.toLocaleString('zh-CN')
}

const loadACL = async () => {
  try {
    const res = await getTunnelACL()
    if (res.success && res.data) {
      policyContent.value = res.data.policy
      lastSyncedAt.value = res.data.last_synced_at
      isEdited.value = false

      // 格式化 JSON
      try {
        const parsed = JSON.parse(res.data.policy)
        policyContent.value = JSON.stringify(parsed, null, 2)
      } catch {
        // 保持原样
      }
    }
  } catch (error) {
    ElMessage.error('加载 ACL Policy 失败')
  }

  // 加载可视化数据
  try {
    const rulesRes = await getTunnelACLRules()
    if (rulesRes.success && rulesRes.data) {
      aclRules.value = rulesRes.data.rules || []
      tagOwners.value = rulesRes.data.tag_owners || []
    }
  } catch (error) {
    console.error('加载 ACL 规则失败:', error)
  }
}

const handleSave = async () => {
  // 验证 JSON 格式
  try {
    JSON.parse(policyContent.value)
  } catch (error) {
    ElMessage.error('无效的 JSON 格式')
    return
  }

  try {
    await ElMessageBox.confirm(
      '确定要更新 ACL Policy 吗？\n\n手动修改可能导致与本地授权数据不一致。',
      '更新 ACL',
      { type: 'warning' }
    )

    const res = await updateTunnelACL(policyContent.value)
    if (res.success) {
      ElMessage.success('更新成功')
      isEdited.value = false
      loadACL()
    } else {
      ElMessage.error(res.message || '更新失败')
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(error.message || '更新失败')
    }
  }
}

const handleSync = async () => {
  try {
    await ElMessageBox.confirm(
      '确定要强制同步 ACL 吗？\n\n这将根据本地授权数据重新生成 ACL Policy，覆盖当前的手动修改。',
      '强制同步',
      { type: 'warning' }
    )

    const res = await syncTunnelACL()
    if (res.success) {
      ElMessage.success('同步成功')
      loadACL()
    } else {
      ElMessage.error(res.message || '同步失败')
    }
  } catch (error: any) {
    if (error !== 'cancel') {
      ElMessage.error(error.message || '同步失败')
    }
  }
}

onMounted(() => {
  loadACL()
})
</script>

<style scoped>
.tunnel-acl {
  width: 100%;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-title {
  font-size: 18px;
  font-weight: 500;
}

.header-actions {
  display: flex;
  gap: 12px;
}

.view-switch {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.editor-view {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.editor-container :deep(.el-textarea__inner) {
  font-family: 'Monaco', 'Menlo', 'Ubuntu Mono', monospace;
  font-size: 13px;
  line-height: 1.5;
}

.editor-tip {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px;
  background: var(--el-color-warning-light-9);
  border-radius: 4px;
  color: var(--el-color-warning);
  font-size: 13px;
}

.sync-info {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.visual-view h4 {
  margin: 0 0 12px 0;
  font-size: 15px;
  font-weight: 500;
}

.empty-tip {
  text-align: center;
  color: var(--el-text-color-secondary);
  padding: 20px;
  font-size: 14px;
}
</style>
