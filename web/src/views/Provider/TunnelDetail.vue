<template>
  <div class="tunnel-detail-page">
    <!-- 面包屑导航 -->
    <el-breadcrumb class="breadcrumb-bar" separator="/">
      <el-breadcrumb-item :to="{ path: '/provider-technical-resources' }">技术资源</el-breadcrumb-item>
      <el-breadcrumb-item :to="{ path: '/provider-tunnels' }">Tunnel 隧道</el-breadcrumb-item>
      <el-breadcrumb-item>详情与授权</el-breadcrumb-item>
    </el-breadcrumb>

    <!-- 顶部状态与操作条 -->
    <div class="detail-header-card">
      <div class="detail-header-left">
        <el-tag size="small" type="success" effect="dark">运行中 (Healthy)</el-tag>
        <h2 class="tunnel-title">{{ tunnel.name }}</h2>
        <span class="agent-binding-badge">
          <el-icon><Connection /></el-icon>
          专属对等绑定: <strong>{{ tunnel.target_agent }}</strong> ({{ tunnel.ip_address || '100.64.0.50' }})
        </span>
      </div>
      <div class="detail-header-right">
        <el-tooltip
          content="保存当前端口映射与授权配置并下发至 Tunnel Pod"
          placement="top"
          :show-after="300"
        >
          <el-button
            type="primary"
            :loading="saving"
            :icon="Check"
            @click="handleSave"
          >
            保存
          </el-button>
        </el-tooltip>
      </div>
    </div>

    <!-- 1:1 对等网络链路拓扑图 -->
    <div class="topology-surface">
      <div class="topology-title">
        <el-icon><Share /></el-icon>
        <span>1:1 对等专线出站网络拓扑</span>
      </div>
      <div class="topology-track">
        <div class="topo-node cloud">
          <div class="node-icon"><Cpu /></div>
          <div class="node-text">
            <strong>云端客户端 / 主网关</strong>
            <span>Traefik Ingress / Dashboard</span>
          </div>
        </div>

        <div class="topo-arrow">
          <div class="arrow-line"></div>
          <span class="arrow-tag">ClusterIP</span>
        </div>

        <div class="topo-node tunnel">
          <div class="node-icon"><Connection /></div>
          <div class="node-text">
            <strong>Signal Tunnel Pod</strong>
            <span>监听 10080 / 6443</span>
          </div>
        </div>

        <div class="topo-arrow wireguard">
          <div class="arrow-line dotted"></div>
          <span class="arrow-tag wg">tsnet WireGuard</span>
        </div>

        <div class="topo-node agent">
          <div class="node-icon"><Monitor /></div>
          <div class="node-text">
            <strong>边缘 Agent</strong>
            <span>{{ tunnel.target_agent }}</span>
          </div>
        </div>

        <div class="topo-arrow">
          <div class="arrow-line"></div>
          <span class="arrow-tag">私网直连</span>
        </div>

        <div class="topo-node edge">
          <div class="node-icon"><SetUp /></div>
          <div class="node-text">
            <strong>边缘私网服务</strong>
            <span>RTX 5090 (8000) / K8s (6443)</span>
          </div>
        </div>
      </div>
    </div>

    <!-- 授权与端口配置面板 -->
    <div class="config-tabs-card">
      <el-tabs v-model="activeTab">
        <!-- Tab 1: 业务数据流 -->
        <el-tab-pane label="业务数据流暴露 (Container Services)" name="services">
          <div class="tab-header">
            <div>
              <h3>边缘业务应用端口白名单</h3>
              <p>显式设定 local_port > 0 才会开启本地监听；设为 0 或置空则保持静默，不开放端口。</p>
            </div>
          </div>

          <el-table :data="services" stripe class="config-table">
            <el-table-column label="容器服务名称" min-width="220">
              <template #default="{ row }">
                <strong>{{ row.service_name }}</strong>
                <span class="secondary mono">Namespace: {{ row.namespace }}</span>
              </template>
            </el-table-column>

            <el-table-column label="边缘目标端口" width="160">
              <template #default="{ row }">
                <el-tag size="small" type="info">{{ row.target_port }} / {{ row.protocol }}</el-tag>
              </template>
            </el-table-column>

            <el-table-column label="本地监听暴露端口 (local_port)" min-width="260">
              <template #default="{ row }">
                <div class="port-input-row">
                  <el-input-number
                    v-model="row.local_port"
                    :min="0"
                    :max="65535"
                    placeholder="0 (保持静默)"
                    controls-position="right"
                  />
                  <el-tag v-if="row.local_port > 0" size="small" type="success">
                    将监听 0.0.0.0:{{ row.local_port }}
                  </el-tag>
                  <el-tag v-else size="small" type="info">保持静默</el-tag>
                </div>
              </template>
            </el-table-column>

            <el-table-column label="主网关 Ingress 路由示例" min-width="320">
              <template #default="{ row }">
                <span v-if="row.local_port > 0" class="mono secondary">
                  Host(`mcp.example.com`) -> edge-tunnel-svc:{{ row.local_port }}
                </span>
                <span v-else class="secondary">未暴露</span>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <!-- Tab 2: 集群控制面 -->
        <el-tab-pane label="集群控制面特权暴露 (Kubernetes API)" name="k8sapi">
          <div class="tab-header">
            <div>
              <h3>边缘 Kubernetes API Server 控制面</h3>
              <p>零信任隔离原则：最高特权控制面坚决不默认静默开放，须管理员在此显式开启。</p>
            </div>
          </div>

          <el-alert
            title="安全说明：开启后，云端平台管控组件（如 Kubernetes Dashboard）或运维终端可通过该 Tunnel 访问边缘 K8s API。"
            type="warning"
            show-icon
            :closable="false"
            class="k8s-alert"
          />

          <div class="k8s-switch-card">
            <div class="switch-row">
              <div class="switch-label">
                <strong>允许代理 Kubernetes API</strong>
                <span>开启后系统自动预填本地默认端口 6443，实现免手动敲端口的极简授权。</span>
              </div>
              <el-switch
                v-model="k8sApiEnabled"
                size="large"
                @change="handleK8sSwitchChange"
              />
            </div>

            <el-divider />

            <div class="k8s-port-config" :class="{ disabled: !k8sApiEnabled }">
              <el-form label-position="top">
                <el-form-item label="本地 Kubernetes API 监听端口 (local_port)">
                  <el-input-number
                    v-model="k8sApiPort"
                    :disabled="!k8sApiEnabled"
                    :min="1"
                    :max="65535"
                    controls-position="right"
                  />
                  <span class="field-help">默认 6443，若与本地环境端口冲突可修改为 16443。</span>
                </el-form-item>

                <el-form-item label="云端 KubeConfig Server 目标地址">
                  <el-input
                    :value="k8sApiEnabled ? `https://${tunnel.name}-svc.beagle-system.svc.cluster.local:${k8sApiPort}` : '未启用'"
                    readonly
                    class="mono"
                  >
                    <template #append>
                      <el-button
                        :icon="DocumentCopy"
                        :disabled="!k8sApiEnabled"
                        @click="copyKubeConfigUrl"
                      >
                        复制
                      </el-button>
                    </template>
                  </el-input>
                </el-form-item>
              </el-form>
            </div>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  Check, Connection, Cpu, DocumentCopy, Monitor, SetUp, Share
} from '@element-plus/icons-vue'
import { getTunnelDetail, updateTunnelPorts, type TunnelPortMapping } from '@/api/signalTunnel'

const route = useRoute()
const saving = ref(false)
const activeTab = ref('services')

const tunnel = ref({
  id: 1,
  name: 'signal-tunnel-5090',
  target_agent: 'edge-gpu-5090',
  ip_address: '100.64.0.50'
})

const services = ref<TunnelPortMapping[]>([
  {
    resource_id: 'res-mcp-1',
    service_name: 'mcp-service',
    namespace: 'beagle-system',
    target_port: 8000,
    protocol: 'TCP',
    local_port: 10080
  },
  {
    resource_id: 'res-redis-1',
    service_name: 'redis-cache',
    namespace: 'beagle-system',
    target_port: 6379,
    protocol: 'TCP',
    local_port: 0
  }
])

const k8sApiEnabled = ref(true)
const k8sApiPort = ref(6443)

const handleK8sSwitchChange = (val: boolean | string | number) => {
  if (val && !k8sApiPort.value) {
    k8sApiPort.value = 6443
  }
}

const copyKubeConfigUrl = async () => {
  const url = `https://${tunnel.value.name}-svc.beagle-system.svc.cluster.local:${k8sApiPort.value}`
  try {
    await navigator.clipboard.writeText(url)
    ElMessage.success('KubeConfig Server 地址已复制')
  } catch {
    ElMessage.warning('复制失败，请手动选取复制')
  }
}

const loadDetail = async () => {
  const id = route.params.id
  if (!id) return
  try {
    const res = await getTunnelDetail(id as string)
    if (res.data) {
      const d = res.data
      tunnel.value = {
        id: d.id,
        name: d.name,
        target_agent: d.target_agent,
        ip_address: d.ip_address || '100.64.0.50'
      }
      if (d.service_ports && d.service_ports.length > 0) {
        services.value = d.service_ports
      }
      k8sApiEnabled.value = d.k8s_api_enabled !== false
      k8sApiPort.value = d.k8s_api_port || 6443
    }
  } catch (e) {
    console.warn('获取 Tunnel 详情异常:', e)
  }
}

const handleSave = async () => {
  saving.value = true
  try {
    await updateTunnelPorts(tunnel.value.id, {
      k8s_api_enabled: k8sApiEnabled.value,
      k8s_api_port: k8sApiPort.value,
      ports: services.value
    })
    ElMessage.success('保存成功！白名单监听端口与授权配置已毫秒级下发至 Tunnel Pod')
  } catch (err: any) {
    ElMessage.error(err.message || '保存失败')
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  const id = route.params.id
  if (id) {
    tunnel.value.id = Number(id)
    loadDetail()
  }
})
</script>

<style scoped>
.tunnel-detail-page { width: 100%; }

.breadcrumb-bar {
  margin-bottom: 16px;
  font-size: 13px;
}

.detail-header-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 20px;
  margin-bottom: 16px;
  border: 1px solid var(--border-light, #e2e8f0);
  border-radius: 6px;
  background: #fff;
}
.detail-header-left {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.tunnel-title {
  margin: 0;
  font-size: 18px;
  font-weight: 700;
  color: var(--text-primary, #0f172a);
}
.agent-binding-badge {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 3px 8px;
  border-radius: 4px;
  background: #f1f5f9;
  color: #475569;
  font-size: 12px;
}

.topology-surface {
  margin-bottom: 16px;
  padding: 16px 20px;
  border: 1px solid var(--border-light, #e2e8f0);
  border-radius: 6px;
  background: #f8fafc;
}
.topology-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 600;
  color: #334155;
  margin-bottom: 14px;
}
.topology-track {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  overflow-x: auto;
  padding: 4px 0;
}
.topo-node {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  border: 1px solid #cbd5e1;
  border-radius: 6px;
  background: #fff;
  min-width: 170px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
}
.topo-node.tunnel {
  border-color: #3b82f6;
  background: #eff6ff;
}
.topo-node.agent {
  border-color: #f59e0b;
  background: #fffbeb;
}
.node-icon {
  font-size: 20px;
  color: #2563eb;
}
.node-text strong {
  display: block;
  font-size: 12px;
  color: #0f172a;
}
.node-text span {
  display: block;
  font-size: 11px;
  color: #64748b;
}

.topo-arrow {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  flex: 1;
  min-width: 60px;
}
.arrow-line {
  width: 100%;
  height: 2px;
  background: #cbd5e1;
  position: relative;
}
.arrow-line.dotted {
  background: transparent;
  border-top: 2px dashed #3b82f6;
}
.arrow-tag {
  font-size: 10px;
  color: #64748b;
  white-space: nowrap;
}
.arrow-tag.wg {
  color: #2563eb;
  font-weight: 600;
}

.config-tabs-card {
  padding: 16px 20px;
  border: 1px solid var(--border-light, #e2e8f0);
  border-radius: 6px;
  background: #fff;
}
.tab-header {
  margin-bottom: 16px;
}
.tab-header h3 {
  margin: 0 0 4px;
  font-size: 15px;
  font-weight: 600;
  color: #0f172a;
}
.tab-header p {
  margin: 0;
  font-size: 12px;
  color: #64748b;
}

.config-table {
  margin-top: 10px;
}
.port-input-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.secondary { display: block; margin-top: 3px; color: #64748b; font-size: 12px; }
.mono { font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', monospace; }

.k8s-alert { margin-bottom: 16px; }
.k8s-switch-card {
  padding: 18px 20px;
  border: 1px solid var(--border-light, #e2e8f0);
  border-radius: 6px;
  background: #f8fafc;
}
.switch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.switch-label strong {
  display: block;
  font-size: 14px;
  color: #0f172a;
}
.switch-label span {
  display: block;
  margin-top: 3px;
  font-size: 12px;
  color: #64748b;
}
.k8s-port-config {
  margin-top: 16px;
  max-width: 520px;
}
.k8s-port-config.disabled {
  opacity: 0.5;
}
.field-help {
  display: block;
  margin-top: 4px;
  font-size: 12px;
  color: #64748b;
}
</style>
