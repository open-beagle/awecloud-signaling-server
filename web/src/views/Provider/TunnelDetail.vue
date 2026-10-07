<template>
  <div class="provider-page">

    <!-- 顶部状态与操作条 -->
    <div class="detail-header-card">
      <div class="detail-header-left">
        <el-tag size="small" :type="tunnel.online ? 'success' : 'info'" effect="dark">
          {{ tunnel.online ? '在线 (Online)' : '离线 (Offline)' }}
        </el-tag>
        <h2 class="tunnel-title">{{ tunnel.name }}</h2>
        <span class="agent-binding-badge">
          <el-icon><Connection /></el-icon>
          专属对等绑定: <strong>{{ tunnel.target_agent }}</strong> ({{ tunnel.ip_address || '100.64.0.50' }})
        </span>
      </div>
      <div class="detail-header-right">
        <el-button :icon="ArrowLeft" @click="returnToList">返回列表</el-button>
        <el-button :icon="Refresh" :loading="loading" @click="loadData">刷新候选</el-button>
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
            <strong>云端调用方 / Ingress</strong>
            <span>本地端口映射访问</span>
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
            <span>按配置暴露 LocalPort</span>
          </div>
        </div>

        <div class="topo-arrow wireguard">
          <div class="arrow-line dotted"></div>
          <span class="arrow-tag wg">WireGuard / tsnet</span>
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
          <span class="arrow-tag">SVCProxy 流转发</span>
        </div>

        <div class="topo-node edge">
          <div class="node-icon"><SetUp /></div>
          <div class="node-text">
            <strong>边缘私网服务</strong>
            <span>集群内网 ClusterIP / K8s 6443</span>
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
              <p>数据源严格来自目标 Agent 真实上报的工作负载清单。显式设定 local_port ≥ 1024 才会开放本地监听；设为 0 或置空则保持静默，不开放端口。</p>
            </div>
          </div>

          <el-table v-loading="loading" :data="services" stripe class="config-table" :row-class-name="tableRowClassName">
            <el-table-column label="命名空间" width="180">
              <template #default="{ row }">
                <el-tag size="small" effect="plain">{{ row.namespace }}</el-tag>
              </template>
            </el-table-column>

            <el-table-column label="容器服务名称" min-width="220">
              <template #default="{ row }">
                <div class="service-name-cell">
                  <strong>{{ row.service_name }}</strong>
                  <el-tag v-if="!row.is_candidate" size="small" type="danger" effect="dark" class="invalid-tag">
                    目标已失效
                  </el-tag>
                  <span v-if="row.port_name" class="secondary mono">PortName: {{ row.port_name }}</span>
                </div>
              </template>
            </el-table-column>

            <el-table-column label="边缘目标端口" width="160">
              <template #default="{ row }">
                <el-tag size="small" :type="row.is_candidate ? 'info' : 'danger'">
                  {{ row.target_port }} / {{ row.protocol }}
                </el-tag>
              </template>
            </el-table-column>

            <el-table-column label="本地监听暴露端口 (local_port)" min-width="280">
              <template #default="{ row }">
                <div class="port-input-row">
                  <el-input-number
                    v-model="row.local_port"
                    :min="0"
                    :max="65535"
                    :disabled="!row.is_candidate"
                    placeholder="0 (保持静默)"
                    controls-position="right"
                  />
                  <el-tag v-if="!row.is_candidate" size="small" type="danger">
                    未在 Agent 发现
                  </el-tag>
                  <el-tag v-else-if="row.local_port > 0" size="small" type="success">
                    将监听 0.0.0.0:{{ row.local_port }}
                  </el-tag>
                  <el-tag v-else size="small" type="info">保持静默</el-tag>
                </div>
              </template>
            </el-table-column>

            <el-table-column label="访问方式 / 路由提示" min-width="300">
              <template #default="{ row }">
                <span v-if="!row.is_candidate" class="danger-text">
                  目标服务在边缘已不可达，保存时请将端口置 0
                </span>
                <span v-else-if="row.local_port > 0" class="mono secondary">
                  {{ tunnel.name }}-svc:{{ row.local_port }}
                </span>
                <span v-else class="secondary">未暴露</span>
              </template>
            </el-table-column>
          </el-table>

          <div v-if="!loading && services.length === 0" class="empty-candidate-hint">
            <el-empty description="当前目标 Agent 暂无可用工作负载上报，请确认 Agent 已正常启动并上报 workload inventory" />
          </div>
        </el-tab-pane>

        <!-- Tab 2: 集群控制面 -->
        <el-tab-pane label="集群控制面特权暴露 (Kubernetes API)" name="k8sapi">
          <div class="tab-header">
            <div>
              <h3>边缘 Kubernetes API Server 控制面</h3>
              <p>零信任隔离原则：最高特权控制面坚决不默认静默开放，须管理员在此显式开启。目标端口固定为 6443。</p>
            </div>
          </div>

          <el-alert
            title="安全说明：开启后，云端平台管控组件或运维终端可通过该 Tunnel 透明直达边缘 Kubernetes API Server (6443)。"
            type="warning"
            show-icon
            :closable="false"
            class="k8s-alert"
          />

          <div class="k8s-switch-card">
            <div class="switch-row">
              <div class="switch-label">
                <strong>允许代理 Kubernetes API</strong>
                <span>开启后系统自动预填本地默认端口 6443，可根据需要自定义本地端口（如 16443）。</span>
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
                    :min="1024"
                    :max="65535"
                    controls-position="right"
                  />
                  <span class="field-help">默认 6443 或 16443，必须在 1024-65535 范围内。</span>
                </el-form-item>

                <el-form-item label="云端 KubeConfig Server 目标地址">
                  <el-input
                    :value="k8sApiEnabled ? `https://${tunnel.name}-svc:${k8sApiPort}` : '未启用'"
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
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  ArrowLeft, Check, Connection, Cpu, DocumentCopy, Monitor, Refresh, SetUp, Share
} from '@element-plus/icons-vue'
import {
  getTunnelDetail,
  getTunnelCandidateServices,
  updateTunnelPorts,
  type TunnelCandidateService,
  type TunnelPortMapping
} from '@/api/signalTunnel'

interface DisplayServiceRow {
  resource_id: string
  service_name: string
  service_uid?: string
  port_name?: string
  target_port: number
  protocol: string
  namespace: string
  namespace_uid?: string
  local_port: number
  is_candidate: boolean
  ready?: boolean
}

const route = useRoute()
const router = useRouter()
const loading = ref(false)
const saving = ref(false)
const activeTab = ref('services')

const returnToList = () => {
  router.push('/provider-tunnels')
}

const tunnel = ref({
  id: 0,
  name: '',
  target_agent: '',
  ip_address: '',
  online: false
})

const services = ref<DisplayServiceRow[]>([])
const k8sApiEnabled = ref(false)
const k8sApiPort = ref(6443)

const tableRowClassName = ({ row }: { row: DisplayServiceRow }) => {
  return !row.is_candidate ? 'row-invalid' : ''
}

const handleK8sSwitchChange = (val: boolean | string | number) => {
  if (val && !k8sApiPort.value) {
    k8sApiPort.value = 6443
  }
}

const copyKubeConfigUrl = async () => {
  const url = `https://${tunnel.value.name}-svc:${k8sApiPort.value}`
  try {
    await navigator.clipboard.writeText(url)
    ElMessage.success('KubeConfig Server 地址已复制')
  } catch {
    ElMessage.warning('复制失败，请手动选取复制')
  }
}

const loadData = async () => {
  const id = route.params.id
  if (!id) return
  loading.value = true
  try {
    const [detailRes, candRes] = await Promise.all([
      getTunnelDetail(id as string),
      getTunnelCandidateServices(id as string).catch(() => ({ data: [] as TunnelCandidateService[] }))
    ])

    const d = detailRes.data
    if (d) {
      tunnel.value = {
        id: d.id,
        name: d.name,
        target_agent: d.target_agent,
        ip_address: d.ip_address || '',
        online: d.online !== false
      }
      k8sApiEnabled.value = d.k8s_api_enabled === true
      k8sApiPort.value = d.k8s_api_port || 6443

      const savedPorts: TunnelPortMapping[] = d.service_ports || []
      const savedMap = new Map<string, TunnelPortMapping>()
      savedPorts.forEach(sp => {
        savedMap.set(sp.resource_id, sp)
      })

      const candidates: TunnelCandidateService[] = Array.isArray(candRes.data) ? candRes.data : []
      const candMap = new Map<string, TunnelCandidateService>()

      const mergedRows: DisplayServiceRow[] = []

      // 1. 遍历真实候选服务
      candidates.forEach(c => {
        candMap.set(c.resource_id, c)
        const saved = savedMap.get(c.resource_id)
        mergedRows.push({
          resource_id: c.resource_id,
          service_name: c.service_name,
          service_uid: c.service_uid,
          port_name: c.port_name,
          target_port: c.port_number,
          protocol: c.protocol,
          namespace: c.namespace,
          namespace_uid: c.namespace_uid,
          local_port: saved ? saved.local_port : 0,
          is_candidate: true,
          ready: c.ready
        })
      })

      // 2. 检查此前已保存但目前不在候选集中的残留配置（目标已失效）
      savedPorts.forEach(sp => {
        if (!candMap.has(sp.resource_id)) {
          mergedRows.push({
            resource_id: sp.resource_id,
            service_name: sp.service_name || '未知服务',
            target_port: sp.target_port,
            protocol: sp.protocol || 'TCP',
            namespace: sp.namespace || '-',
            local_port: sp.local_port,
            is_candidate: false,
            ready: false
          })
        }
      })

      services.value = mergedRows
    }
  } catch (err: any) {
    ElMessage.error(err.message || '加载详情失败')
  } finally {
    loading.value = false
  }
}

const handleSave = async () => {
  // 1. 校验端口范围与冲突
  const usedPorts = new Set<number>()

  if (k8sApiEnabled.value) {
    if (k8sApiPort.value < 1024 || k8sApiPort.value > 65535) {
      ElMessage.warning('K8s API 映射端口必须在 1024-65535 之间')
      return
    }
    usedPorts.add(k8sApiPort.value)
  }

  for (const item of services.value) {
    if (item.local_port > 0) {
      if (!item.is_candidate) {
        ElMessage.warning(`服务 ${item.service_name} 目标已失效，无法暴露，请先将其 local_port 置为 0`)
        return
      }
      if (item.local_port < 1024 || item.local_port > 65535) {
        ElMessage.warning(`服务 ${item.service_name} 的本地映射端口必须在 1024-65535 之间或为 0 (静默)`)
        return
      }
      if (usedPorts.has(item.local_port)) {
        ElMessage.warning(`本地映射端口冲突: ${item.local_port}，每个服务必须映射唯一端口`)
        return
      }
      usedPorts.add(item.local_port)
    }
  }

  // 2. 过滤仅构造有效的 payload
  const portsPayload: TunnelPortMapping[] = services.value
    .filter(s => s.is_candidate)
    .map(s => ({
      resource_id: s.resource_id,
      service_name: s.service_name,
      target_port: s.target_port,
      protocol: s.protocol,
      local_port: s.local_port,
      namespace: s.namespace
    }))

  saving.value = true
  try {
    await updateTunnelPorts(tunnel.value.id, {
      k8s_api_enabled: k8sApiEnabled.value,
      k8s_api_port: k8sApiPort.value,
      ports: portsPayload
    })
    ElMessage.success('保存成功！白名单监听端口与授权配置已毫秒级下发至 Tunnel Pod')
    await loadData()
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
    loadData()
  }
})
</script>

<style scoped>
.provider-page {
  width: 100%;
  max-width: 100%;
  box-sizing: border-box;
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
.detail-header-right {
  display: flex;
  align-items: center;
  gap: 10px;
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
  width: 100%;
}
:deep(.row-invalid) {
  background-color: #fff1f2 !important;
}
.service-name-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.invalid-tag {
  width: fit-content;
  margin-top: 2px;
}
.danger-text {
  color: #dc2626;
  font-size: 12px;
}
.port-input-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.secondary { display: block; margin-top: 3px; color: #64748b; font-size: 12px; }
.mono { font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', monospace; }

.empty-candidate-hint {
  padding: 30px 0;
}

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

/* 适配 1080p 与 2K 分辨率 */
@media screen and (min-width: 1920px) {
  .detail-header-card, .topology-surface, .config-tabs-card {
    padding: 20px 28px;
  }
  .tunnel-title {
    font-size: 20px;
  }
  .config-table {
    font-size: 14px;
  }
}
@media screen and (min-width: 2560px) {
  .detail-header-card, .topology-surface, .config-tabs-card {
    padding: 24px 32px;
  }
  .tunnel-title {
    font-size: 22px;
  }
}
</style>
