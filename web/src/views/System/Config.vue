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
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { getSystemConfig, updateSystemConfig } from '@/api/system'
import { useWorkspaceStore } from '@/stores/workspace'
import PageHeader from '@/components/Common/PageHeader.vue'

const router = useRouter()
const workspaceStore = useWorkspaceStore()
const canWrite = computed(() => workspaceStore.can('platform.settings.write'))
const formRef = ref()
const saving = ref(false)

const form = ref({
  client_download_url: '',
  desktop_min_version: '1.0.0',
  headscale_public_url: '',
  stun_port: 3479,
  ip_prefix: '100.64.0.0/10',
  auth_key_expiry_hours: 24,
  domain_suffix: '.beagle'
})

const downloadPageUrl = computed(() => {
  return window.location.origin + '/download'
})

const goToDownloadPage = () => {
  window.open('/download', '_blank')
}

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

onMounted(() => {
  loadConfig()
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
</style>
