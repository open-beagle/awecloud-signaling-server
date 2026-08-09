<template>
  <div class="system-config">
    <PageHeader title="系统配置" description="维护客户端下载、网络接入和认证有效期等平台级配置。">
      <template #actions>
        <el-button type="primary" :loading="saving" :disabled="!canWrite" @click="handleSave">保存</el-button>
      </template>
    </PageHeader>
    <el-card>
      <el-form
        ref="formRef"
        :model="form"
        label-width="200px"
        :disabled="!canWrite"
      >
        <!-- 基础配置 -->
        <div class="config-section">
          <div class="section-title">基础配置</div>
          
          <el-form-item label="客户端下载地址">
            <div class="form-item-content">
              <el-input
                v-model="form.client_download_url"
                placeholder="请输入客户端文件存储的基础URL（如：https://cdn.example.com/downloads）"
                clearable
              />
              <div class="form-item-tip">
                设置客户端文件存储的基础URL，系统会自动拼接文件名生成完整下载链接
              </div>
            </div>
          </el-form-item>

          <el-form-item label="客户端最低版本">
            <div class="form-item-content">
              <el-input
                v-model="form.desktop_min_version"
                placeholder="请输入最低支持版本（如：1.0.0 或 v1.0.0）"
                clearable
                style="width: 300px"
              />
              <div class="form-item-tip">
                低于此版本的客户端将无法登录，强制用户升级到新版本（支持 v 前缀）
              </div>
            </div>
          </el-form-item>

          <el-form-item label="域名后缀">
            <div class="form-item-content">
              <el-input
                v-model="form.domain_suffix"
                placeholder="默认 .beagle"
                clearable
                style="width: 300px"
              />
              <div class="form-item-tip">
                ZTNA 域名体系的根域名后缀，如 .beagle，域名示例：beagle-242.beijing.beagle
              </div>
            </div>
          </el-form-item>
        </div>

        <!-- 隧道配置 -->
        <div class="config-section">
          <div class="section-title">{{ $t('tailscale.title') }}</div>
          
          <el-form-item :label="$t('tailscale.headscalePublicUrl')">
            <div class="form-item-content">
              <el-input
                v-model="form.headscale_public_url"
                :placeholder="$t('tailscale.headscalePublicUrlPlaceholder')"
                clearable
              />
              <div class="form-item-tip">
                {{ $t('tailscale.headscalePublicUrlTip') }}
              </div>
            </div>
          </el-form-item>

          <el-form-item :label="$t('tailscale.stunPort')">
            <div class="form-item-content">
              <el-input-number
                v-model="form.stun_port"
                :placeholder="$t('tailscale.stunPortPlaceholder')"
                :min="1"
                :max="65535"
                style="width: 300px"
              />
              <div class="form-item-tip">
                {{ $t('tailscale.stunPortTip') }}
              </div>
            </div>
          </el-form-item>

          <el-form-item :label="$t('tailscale.ipPrefix')">
            <div class="form-item-content">
              <el-input
                v-model="form.ip_prefix"
                :placeholder="$t('tailscale.ipPrefixPlaceholder')"
                clearable
                style="width: 300px"
              />
              <div class="form-item-tip">
                {{ $t('tailscale.ipPrefixTip') }}
              </div>
            </div>
          </el-form-item>

          <el-form-item :label="$t('tailscale.authKeyExpiryHours')">
            <div class="form-item-content">
              <el-input-number
                v-model="form.auth_key_expiry_hours"
                :placeholder="$t('tailscale.authKeyExpiryHoursPlaceholder')"
                :min="1"
                :max="8760"
                style="width: 300px"
              />
              <div class="form-item-tip">
                {{ $t('tailscale.authKeyExpiryHoursTip') }}
              </div>
            </div>
          </el-form-item>
        </div>

        <el-form-item>
          <el-button type="primary" :loading="saving" :disabled="!canWrite" @click="handleSave">
            保存
          </el-button>
        </el-form-item>
      </el-form>

      <section class="catalog-section">
        <div class="section-title">制品与版本</div>
        <div class="catalog-toolbar">
          <el-button :icon="Refresh" :loading="syncingCatalog" :disabled="!canWrite" @click="handleCatalogSync">
            同步制品与版本
          </el-button>
          <div v-if="catalogSyncResult" class="catalog-sync-result">
            <el-tag size="small" type="success">新增 {{ catalogSyncResult.created }}</el-tag>
            <el-tag size="small" type="info">已存在 {{ catalogSyncResult.existing }}</el-tag>
            <el-tag v-if="catalogSyncResult.revoked" size="small" type="warning">已撤销 {{ catalogSyncResult.revoked }}</el-tag>
            <el-tag v-if="catalogSyncResult.failed" size="small" type="danger">失败 {{ catalogSyncResult.failed }}</el-tag>
          </div>
          <span class="catalog-toolbar-spacer" />
          <el-radio-group v-model="releaseComponent" size="small" @change="loadReleases">
            <el-radio-button value="">全部</el-radio-button>
            <el-radio-button value="agent">Agent</el-radio-button>
            <el-radio-button value="endpoint">Endpoint</el-radio-button>
            <el-radio-button value="desktop">Desktop</el-radio-button>
          </el-radio-group>
        </div>

        <el-table v-loading="loadingReleases" :data="releases" stripe class="release-table" empty-text="暂无已同步制品">
          <el-table-column label="组件" width="120">
            <template #default="{ row }">
              <el-tag size="small" effect="plain" :type="componentTag(row.component)">{{ componentLabel(row.component) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="版本 / Commit" min-width="280">
            <template #default="{ row }">
              <strong>{{ row.version }}</strong>
              <code class="commit-id">{{ row.commit_id }}</code>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="110">
            <template #default="{ row }">
              <el-tag size="small" :type="releaseStatusTag(row.status)">{{ releaseStatusLabel(row.status) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="制品" width="90" align="center">
            <template #default="{ row }"><strong>{{ row.artifact_count }}</strong></template>
          </el-table-column>
          <el-table-column label="发布时间" width="180">
            <template #default="{ row }">{{ formatTime(row.published_at || row.created_at) }}</template>
          </el-table-column>
          <el-table-column label="" width="64" align="center" fixed="right">
            <template #default="{ row }">
              <el-tooltip content="查看制品详情" placement="top">
                <el-button text :icon="View" :aria-label="`查看 ${row.component} ${row.version} 制品详情`" @click="showReleaseDetail(row)" />
              </el-tooltip>
            </template>
          </el-table-column>
        </el-table>
      </section>
    </el-card>

    <el-drawer v-model="releaseDrawerVisible" :title="releaseDrawerTitle" size="960px">
      <div v-loading="loadingReleaseDetail" class="release-detail">
        <el-descriptions v-if="releaseDetail" :column="3" border class="release-summary">
          <el-descriptions-item label="组件">{{ componentLabel(releaseDetail.release.component) }}</el-descriptions-item>
          <el-descriptions-item label="版本">{{ releaseDetail.release.version }}</el-descriptions-item>
          <el-descriptions-item label="状态">{{ releaseStatusLabel(releaseDetail.release.status) }}</el-descriptions-item>
          <el-descriptions-item label="Commit" :span="3">
            <div class="copy-value"><code>{{ releaseDetail.release.commit_id }}</code><CopyButton :text="releaseDetail.release.commit_id" /></div>
          </el-descriptions-item>
        </el-descriptions>

        <el-table v-if="releaseDetail" :data="releaseDetail.artifacts" stripe>
          <el-table-column label="平台" width="150">
            <template #default="{ row }"><strong>{{ row.os }} / {{ row.arch }}</strong><span class="artifact-meta">{{ row.role }} · {{ row.package_type }}</span></template>
          </el-table-column>
          <el-table-column label="文件" min-width="210">
            <template #default="{ row }"><strong>{{ row.filename }}</strong><span class="artifact-meta">{{ formatBytes(row.size) }}</span></template>
          </el-table-column>
          <el-table-column label="SHA256" min-width="230">
            <template #default="{ row }"><div class="hash-cell"><code>{{ row.sha256 }}</code><CopyButton :text="row.sha256" /></div></template>
          </el-table-column>
          <el-table-column label="下载" width="92" align="center">
            <template #default="{ row }">
              <el-tooltip content="打开公开下载地址" placement="top">
                <el-button tag="a" text :icon="Download" :href="row.download_url" target="_blank" rel="noopener noreferrer" />
              </el-tooltip>
              <el-tooltip content="复制下载地址" placement="top">
                <el-button text :icon="CopyDocument" @click="copyText(row.download_url)" />
              </el-tooltip>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { CopyDocument, Download, Refresh, View } from '@element-plus/icons-vue'
import {
  getSystemConfig,
  getUpdaterRelease,
  getUpdaterReleases,
  syncUpdaterCatalog,
  updateSystemConfig,
  type UpdaterCatalogSyncResult,
  type UpdaterComponent,
  type UpdaterRelease,
  type UpdaterReleaseDetail
} from '@/api/system'
import { useWorkspaceStore } from '@/stores/workspace'
import PageHeader from '@/components/Common/PageHeader.vue'
import CopyButton from '@/components/Common/CopyButton.vue'
import { formatTime } from '@/utils/time'

const workspaceStore = useWorkspaceStore()
const canWrite = computed(() => workspaceStore.can('platform.settings.write'))
const formRef = ref()
const saving = ref(false)
const syncingCatalog = ref(false)
const catalogSyncResult = ref<UpdaterCatalogSyncResult | null>(null)
const loadingReleases = ref(false)
const releases = ref<UpdaterRelease[]>([])
const releaseComponent = ref<UpdaterComponent | ''>('')
const releaseDrawerVisible = ref(false)
const loadingReleaseDetail = ref(false)
const releaseDetail = ref<UpdaterReleaseDetail | null>(null)
const releaseDrawerTitle = computed(() => releaseDetail.value
  ? `${componentLabel(releaseDetail.value.release.component)} ${releaseDetail.value.release.version}`
  : '制品详情')

const form = ref({
  client_download_url: '',
  desktop_min_version: '1.0.0',
  headscale_public_url: '',
  stun_port: 3479,
  ip_prefix: '100.64.0.0/10',
  auth_key_expiry_hours: 24,
  domain_suffix: '.beagle'
})

const loadConfig = async () => {
  try {
    const res = await getSystemConfig()
    if (res.success && res.data) {
      form.value.client_download_url = res.data.client_download_url || ''
      form.value.desktop_min_version = res.data.desktop_min_version || '1.0.0'
      form.value.headscale_public_url = res.data.headscale_public_url || ''
      form.value.stun_port = res.data.stun_port || 3479
      form.value.ip_prefix = res.data.ip_prefix || '100.64.0.0/10'
      form.value.auth_key_expiry_hours = res.data.auth_key_expiry_hours || 24
      form.value.domain_suffix = res.data.domain_suffix || '.beagle'
    }
  } catch (error) {
    console.error('加载配置失败:', error)
  }
}

const loadReleases = async () => {
  loadingReleases.value = true
  try {
    const response = await getUpdaterReleases(releaseComponent.value || undefined)
    releases.value = response.data || []
  } finally {
    loadingReleases.value = false
  }
}

onMounted(() => {
  loadConfig()
  loadReleases()
})

const validateIPPrefix = (ipPrefix: string): boolean => {
  if (!ipPrefix) return true // 允许为空
  // 验证 CIDR 格式
  const cidrRegex = /^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$/
  if (!cidrRegex.test(ipPrefix)) {
    return false
  }
  // 验证 IP 地址部分
  const [ip, mask] = ipPrefix.split('/')
  const parts = ip.split('.')
  for (const part of parts) {
    const num = parseInt(part)
    if (num < 0 || num > 255) {
      return false
    }
  }
  // 验证掩码
  const maskNum = parseInt(mask)
  if (maskNum < 0 || maskNum > 32) {
    return false
  }
  return true
}

const handleSave = async () => {
  if (!canWrite.value) return
  // 验证版本号格式（支持可选的 v 或 V 前缀）
  const versionRegex = /^[vV]?\d+\.\d+\.\d+$/
  if (form.value.desktop_min_version && !versionRegex.test(form.value.desktop_min_version)) {
    ElMessage.error('版本号格式不正确，请使用 x.y.z 或 vx.y.z 格式（如：1.0.0 或 v1.0.0）')
    return
  }

  // 验证 IP 地址段格式
  if (form.value.ip_prefix && !validateIPPrefix(form.value.ip_prefix)) {
    ElMessage.error('IP 地址段格式不正确，请使用 CIDR 格式（如：100.64.0.0/10）')
    return
  }

  // 验证 STUN 端口
  if (form.value.stun_port && (form.value.stun_port < 1 || form.value.stun_port > 65535)) {
    ElMessage.error('STUN 端口必须在 1-65535 之间')
    return
  }

  // 验证预认证密钥有效期
  if (form.value.auth_key_expiry_hours && (form.value.auth_key_expiry_hours < 1 || form.value.auth_key_expiry_hours > 8760)) {
    ElMessage.error('预认证密钥有效期必须在 1-8760 小时之间')
    return
  }

  saving.value = true
  try {
    const res = await updateSystemConfig({
      client_download_url: form.value.client_download_url,
      desktop_min_version: form.value.desktop_min_version,
      headscale_public_url: form.value.headscale_public_url,
      stun_port: form.value.stun_port,
      ip_prefix: form.value.ip_prefix,
      auth_key_expiry_hours: form.value.auth_key_expiry_hours,
      domain_suffix: form.value.domain_suffix
    })
    if (res.success) {
      ElMessage.success('保存成功')
    }
  } catch (error) {
    ElMessage.error('保存失败')
  } finally {
    saving.value = false
  }
}

const handleCatalogSync = async () => {
  if (!canWrite.value) return
  syncingCatalog.value = true
  try {
    const response = await syncUpdaterCatalog()
    catalogSyncResult.value = response.data
    ElMessage.success(`同步完成：新增 ${response.data.created}，已存在 ${response.data.existing}`)
    await loadReleases()
  } finally {
    syncingCatalog.value = false
  }
}

const showReleaseDetail = async (release: UpdaterRelease) => {
  releaseDrawerVisible.value = true
  loadingReleaseDetail.value = true
  releaseDetail.value = null
  try {
    const response = await getUpdaterRelease(release.id)
    releaseDetail.value = response.data
  } finally {
    loadingReleaseDetail.value = false
  }
}

const componentLabel = (component: UpdaterComponent) => ({ agent: 'Agent', endpoint: 'Endpoint', desktop: 'Desktop' })[component]
const componentTag = (component: UpdaterComponent) => ({ agent: 'success', endpoint: 'warning', desktop: 'primary' })[component] as 'success' | 'warning' | 'primary'
const releaseStatusLabel = (status: UpdaterRelease['status']) => ({ draft: '草稿', published: '已发布', revoked: '已撤销' })[status]
const releaseStatusTag = (status: UpdaterRelease['status']) => ({ draft: 'info', published: 'success', revoked: 'danger' })[status] as 'info' | 'success' | 'danger'

const formatBytes = (bytes: number) => {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

const copyText = async (value: string) => {
  try {
    await navigator.clipboard.writeText(value)
    ElMessage.success('已复制')
  } catch {
    ElMessage.error('复制失败')
  }
}
</script>

<style scoped>
.system-config {
  width: 100%;
}

.config-section {
  margin-bottom: 32px;
}

.config-section:last-of-type {
  margin-bottom: 0;
}

.section-title {
  font-size: 16px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  margin-bottom: 20px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

:deep(.el-form-item__content) {
  flex: 1;
}

.form-item-content {
  width: 100%;
  max-width: 800px;
}

.form-item-content .el-input {
  width: 100%;
}

.form-item-tip {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 8px;
  line-height: 1.5;
}

.catalog-section {
  margin-top: 32px;
}

.catalog-toolbar,
.catalog-sync-result {
  display: flex;
  align-items: center;
  gap: 8px;
}

.catalog-toolbar {
  margin-bottom: 12px;
}

.catalog-toolbar-spacer {
  flex: 1;
}

.release-table strong {
  color: var(--el-text-color-primary);
  font-weight: 600;
}

.commit-id,
.artifact-meta {
  display: block;
  margin-top: 3px;
  color: var(--el-text-color-secondary);
  font-size: 11px;
}

.commit-id {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.release-summary {
  margin-bottom: 20px;
}

.copy-value,
.hash-cell {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
}

.hash-cell code {
  overflow: hidden;
  color: var(--el-text-color-regular);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.copy-value :deep(.el-button),
.hash-cell :deep(.el-button) {
  flex: 0 0 auto;
}
</style>
