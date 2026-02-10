<template>
  <div class="node-detail" v-loading="loading">
    <template v-if="node">
      <!-- 基本信息 -->
      <el-card shadow="never" class="info-card">
        <template #header>
          <div class="card-header">
            <span>{{ $t('node.basicInfo') }}</span>
            <el-tag :type="node.status === 'online' ? 'success' : 'info'" size="small">
              {{ node.status === 'online' ? $t('common.online') : $t('common.offline') }}
            </el-tag>
          </div>
        </template>
        <el-descriptions :column="2" border label-class-name="desc-label">
          <el-descriptions-item label="ID">{{ node.id }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.name')">{{ node.name }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.type')">
            <el-tag :type="node.type === 'agent' ? 'success' : 'primary'" size="small">
              {{ node.type === 'agent' ? $t('node.typeAgent') : $t('node.typeDesktop') }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="$t('node.user')">
            <router-link v-if="node.user" :to="`/users/${node.user.id}`" class="user-link">
              {{ node.user.name }}
            </router-link>
            <span v-else>-</span>
          </el-descriptions-item>
          <el-descriptions-item :label="$t('node.ip')">{{ node.ip || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.hostname')">{{ node.hostname || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.version')">{{ node.version || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.lastHeartbeat')">
            {{ node.last_heartbeat ? formatTime(node.last_heartbeat) : '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="$t('common.createdAt')">
            {{ formatTime(node.created_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="$t('node.updatedAt')">
            {{ formatTime(node.updated_at) }}
          </el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- Headscale 信息 -->
      <el-card shadow="never" class="info-card">
        <template #header>
          <div class="card-header">
            <span>Headscale {{ $t('node.nodeInfo') }}</span>
            <el-tag v-if="node.headscale" :type="node.headscale.online ? 'success' : 'info'" size="small">
              {{ node.headscale.online ? $t('common.online') : $t('common.offline') }}
            </el-tag>
          </div>
        </template>
        <template v-if="node.headscale">
          <el-descriptions :column="2" border label-class-name="desc-label">
            <el-descriptions-item label="Node ID">{{ node.headscale.id }}</el-descriptions-item>
            <el-descriptions-item :label="$t('node.name')">{{ node.headscale.name }}</el-descriptions-item>
            <el-descriptions-item :label="$t('node.givenName')">{{ node.headscale.given_name }}</el-descriptions-item>
            <el-descriptions-item :label="$t('node.hsUser')">{{ node.headscale.user_name || '-' }}</el-descriptions-item>
            <el-descriptions-item :label="$t('node.ipAddresses')">
              <el-tag v-for="ip in node.headscale.ip_addresses" :key="ip" size="small" class="ip-tag">
                {{ ip }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item :label="$t('node.forcedTags')">
              <el-tag v-for="tag in node.headscale.forced_tags" :key="tag" size="small" type="warning" class="tag-item">
                {{ tag }}
              </el-tag>
              <span v-if="!node.headscale.forced_tags?.length">-</span>
            </el-descriptions-item>
            <el-descriptions-item :label="$t('node.lastSeen')">
              {{ node.headscale.last_seen ? formatTime(node.headscale.last_seen) : '-' }}
            </el-descriptions-item>
            <el-descriptions-item :label="$t('node.expiry')">
              {{ node.headscale.expiry ? formatTime(node.headscale.expiry) : '-' }}
            </el-descriptions-item>
            <el-descriptions-item :label="$t('common.createdAt')">
              {{ node.headscale.created_at ? formatTime(node.headscale.created_at) : '-' }}
            </el-descriptions-item>
            <el-descriptions-item label="">&nbsp;</el-descriptions-item>
          </el-descriptions>
        </template>
        <template v-else>
          <el-empty :description="$t('node.noHeadscaleInfo')">
            <template #image><span></span></template>
          </el-empty>
        </template>
      </el-card>

      <!-- 系统信息 -->
      <el-card v-if="systemInfo" shadow="never" class="info-card">
        <template #header>
          <span>{{ $t('node.systemInfo') }}</span>
        </template>
        <el-descriptions :column="2" border label-class-name="desc-label">
          <el-descriptions-item :label="$t('node.os')">{{ systemInfo.os || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.osVersion')">{{ systemInfo.os_version || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.arch')">{{ systemInfo.arch || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.hostname')">{{ systemInfo.hostname || '-' }}</el-descriptions-item>
          <el-descriptions-item label="CPU">{{ systemInfo.cpu || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.cpuCores')">{{ systemInfo.cpu_cores || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="$t('node.memory')">
            {{ systemInfo.memory_gb ? `${systemInfo.memory_gb} GB` : '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="">&nbsp;</el-descriptions-item>
        </el-descriptions>
      </el-card>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { getNode, type NodeDetail, type NodeSystemInfo } from '@/api/node'
import { formatTime } from '@/utils/time'

const route = useRoute()
const loading = ref(false)
const node = ref<NodeDetail | null>(null)

// 解析系统信息
const systemInfo = computed<NodeSystemInfo | null>(() => {
  if (!node.value?.system_info) return null
  try {
    return JSON.parse(node.value.system_info)
  } catch {
    return null
  }
})

// 获取设备详情
const fetchNode = async () => {
  const id = Number(route.params.id)
  if (!id) return

  loading.value = true
  try {
    const res = await getNode(id)
    if (res.success && res.data) {
      node.value = res.data
    }
  } catch (error) {
    console.error('获取设备详情失败:', error)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  fetchNode()
})
</script>

<style scoped>
.node-detail {
  width: 100%;
}

.info-card {
  margin-bottom: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.user-link {
  color: var(--el-color-primary);
  text-decoration: none;
}

.user-link:hover {
  text-decoration: underline;
}

.ip-tag {
  margin-right: 8px;
  margin-bottom: 4px;
}

.tag-item {
  margin-right: 8px;
  margin-bottom: 4px;
}
</style>

<style>
.node-detail .el-descriptions__label {
  width: 100px !important;
  min-width: 100px !important;
  max-width: 100px !important;
}
</style>
