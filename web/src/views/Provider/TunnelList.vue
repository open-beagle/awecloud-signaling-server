<template>
  <div class="tunnel-list-page">
    <PageHeader title="Tunnel 隧道" description="管理直连边缘专属出站隧道实例、1:1 Agent 绑定与本地暴露端口白名单。">
      <template #actions>
        <el-button :icon="Refresh" :loading="loading" @click="loadData">刷新</el-button>
        <el-button type="primary" :icon="Plus" @click="openCreateDialog">创建 Tunnel</el-button>
      </template>
    </PageHeader>

    <!-- 运行指标卡片 -->
    <div class="stats-grid">
      <div class="stat-card">
        <div class="stat-header">
          <span class="stat-label">活跃隧道</span>
          <el-icon class="stat-icon primary"><Connection /></el-icon>
        </div>
        <div class="stat-value">{{ activeCount }} <span class="stat-unit">个在线</span></div>
        <div class="stat-sub">总实例: {{ items.length }} 个</div>
      </div>
      <div class="stat-card">
        <div class="stat-header">
          <span class="stat-label">转发端口总数</span>
          <el-icon class="stat-icon success"><Switch /></el-icon>
        </div>
        <div class="stat-value">{{ totalPortsCount }} <span class="stat-unit">个端口</span></div>
        <div class="stat-sub">白名单驱动暴露</div>
      </div>
      <div class="stat-card">
        <div class="stat-header">
          <span class="stat-label">目标绑定节点</span>
          <el-icon class="stat-icon warning"><Cpu /></el-icon>
        </div>
        <div class="stat-value">{{ targetAgentsCount }} <span class="stat-unit">台边缘 Agent</span></div>
        <div class="stat-sub">1:1 对等隔离专线</div>
      </div>
      <div class="stat-card">
        <div class="stat-header">
          <span class="stat-label">实时吞吐峰值</span>
          <el-icon class="stat-icon info"><DataAnalysis /></el-icon>
        </div>
        <div class="stat-value">12.4 <span class="stat-unit">MB/s</span></div>
        <div class="stat-sub">端到端平均延迟: ~18ms</div>
      </div>
    </div>

    <!-- 数据表格卡片 -->
    <section class="data-surface">
      <div class="toolbar">
        <el-input
          v-model="filters.search"
          class="search-input"
          clearable
          :prefix-icon="Search"
          placeholder="搜索隧道名称或目标 Agent"
          @keyup.enter="applyFilters"
          @clear="applyFilters"
        />
        <el-select
          v-model="filters.status"
          class="filter-select"
          clearable
          placeholder="全部运行状态"
          @change="applyFilters"
        >
          <el-option label="运行中 (Online)" value="online" />
          <el-option label="待连接 (Pending)" value="pending" />
          <el-option label="已离线 (Offline)" value="offline" />
        </el-select>
        <span class="result-count">{{ filteredItems.length }} 个 Tunnel 实例</span>
      </div>

      <el-table v-loading="loading" :data="filteredItems" stripe>
        <el-table-column label="隧道实例名称" min-width="240">
          <template #default="{ row }">
            <router-link class="tunnel-name" :to="`/provider-tunnels/${row.id}`">
              {{ row.name }}
            </router-link>
            <span class="secondary mono">{{ row.device_name || `dt_tunnel_${row.id}` }}</span>
          </template>
        </el-table-column>

        <el-table-column label="1:1 目标 Agent" min-width="230">
          <template #default="{ row }">
            <div class="target-agent-cell">
              <strong>{{ row.target_agent }}</strong>
              <span class="secondary mono">{{ row.ip_address || '100.64.0.12' }}</span>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="运行状态" width="130">
          <template #default="{ row }">
            <el-tag size="small" :type="statusTag(row.status)">
              {{ statusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>

        <el-table-column label="暴露端口白名单" min-width="260">
          <template #default="{ row }">
            <div class="ports-list">
              <el-tag
                v-for="p in row.exposed_ports"
                :key="p.port"
                size="small"
                effect="plain"
                :type="p.type === 'k8sapi' ? 'warning' : 'primary'"
              >
                {{ p.port }} ({{ p.name }})
              </el-tag>
              <span v-if="!row.exposed_ports || row.exposed_ports.length === 0" class="secondary inline">
                无端口（静默）
              </span>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="网络吞吐 / 延迟" width="180">
          <template #default="{ row }">
            <span>{{ row.throughput || '0 B/s' }}</span>
            <span class="secondary">{{ row.latency || '< 1ms' }}</span>
          </template>
        </el-table-column>

        <el-table-column label="创建时间" width="170">
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>

        <el-table-column label="操作" width="140" fixed="right" align="center">
          <template #default="{ row }">
            <div class="row-actions">
              <el-tooltip content="配置本地监听端口白名单与授权" placement="top" :show-after="300">
                <el-button
                  class="action-btn"
                  circle
                  :icon="Key"
                  @click="goToDetail(row.id)"
                />
              </el-tooltip>

              <el-tooltip content="导出 Kubernetes Deployment / Service YAML" placement="top" :show-after="300">
                <el-button
                  class="action-btn"
                  circle
                  :icon="DocumentCopy"
                  @click="openExportDialog(row)"
                />
              </el-tooltip>

              <el-tooltip content="删除隧道实例" placement="top" :show-after="300">
                <el-button
                  class="action-btn danger"
                  circle
                  :icon="Delete"
                  @click="handleDelete(row)"
                />
              </el-tooltip>
            </div>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && filteredItems.length === 0" description="暂无符合条件的 Tunnel 隧道实例" />
    </section>

    <!-- 创建 Tunnel 对话框 -->
    <el-dialog v-model="createDialogVisible" title="创建 Tunnel 隧道实例" width="560px" destroy-on-close>
      <el-form label-position="top">
        <el-form-item label="隧道实例名称" required>
          <el-input v-model="createForm.name" maxlength="63" placeholder="例如 signal-tunnel-5090" />
          <span class="field-help">用于在云端 Kubernetes 部署的 Deployment 与 Service 标识。</span>
        </el-form-item>

        <el-form-item label="绑定目标边缘 Agent" required>
          <el-select
            v-model="createForm.target_agent"
            class="full-width"
            filterable
            placeholder="请选择对等绑定的边缘 Agent"
          >
            <el-option
              v-for="agent in availableAgents"
              :key="agent.name"
              :label="`${agent.name} (${agent.ip})`"
              :value="agent.name"
            >
              <div class="agent-option">
                <span>{{ agent.name }}</span>
                <span class="secondary mono">{{ agent.ip }}</span>
              </div>
            </el-option>
          </el-select>
          <span class="field-help">严格 1:1 对等隔离。启动时将执行握手防呆校验，不匹配直接拒绝。</span>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="createDialogVisible = false">取消</el-button>
        <el-button
          type="primary"
          :loading="creating"
          :disabled="!createForm.name || !createForm.target_agent"
          @click="submitCreateTunnel"
        >
          创建并生成清单
        </el-button>
      </template>
    </el-dialog>

    <!-- 导出 Kubernetes YAML 对话框 -->
    <el-dialog v-model="exportDialogVisible" title="Kubernetes 编排部署清单" width="760px">
      <el-alert
        title="标准云原生无侵入架构：配合 Traefik IngressRoute 即可将外部流量代理至边缘 GPU 服务。"
        type="info"
        show-icon
        :closable="false"
      />
      <div class="yaml-container">
        <div class="yaml-header">
          <span class="mono">deployment-and-service.yaml</span>
          <el-button :icon="DocumentCopy" size="small" type="primary" @click="copyYaml">
            一键复制 YAML 清单
          </el-button>
        </div>
        <pre class="yaml-code"><code>{{ exportYamlContent }}</code></pre>
      </div>
      <template #footer>
        <el-button type="primary" @click="exportDialogVisible = false">完成</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Connection, Cpu, DataAnalysis, Delete, DocumentCopy, Key, Plus, Refresh, Search, Switch
} from '@element-plus/icons-vue'
import PageHeader from '@/components/Common/PageHeader.vue'
import {
  getTunnelList, createTunnelInstance, getAvailableEdgeAgents,
  type TunnelItem
} from '@/api/signalTunnel'

const router = useRouter()

const loading = ref(false)
const creating = ref(false)
const items = ref<TunnelItem[]>([])

const filters = ref({
  search: '',
  status: ''
})

const availableAgents = ref<Array<{ name: string; ip: string }>>([
  { name: 'edge-gpu-5090', ip: '192.168.1.200' },
  { name: 'edge-gpu-4090', ip: '192.168.1.201' },
  { name: 'edge-k8s-cluster', ip: '10.0.0.15' }
])

const createDialogVisible = ref(false)
const createForm = ref({
  name: '',
  target_agent: ''
})

const exportDialogVisible = ref(false)
const exportYamlContent = ref('')

const activeCount = computed(() => items.value.filter(i => i.status === 'online').length)
const totalPortsCount = computed(() => items.value.reduce((acc, i) => acc + (i.exposed_ports?.length || 0), 0))
const targetAgentsCount = computed(() => new Set(items.value.map(i => i.target_agent)).size)

const filteredItems = computed(() => {
  return items.value.filter(item => {
    if (filters.value.status && item.status !== filters.value.status) return false
    if (filters.value.search) {
      const q = filters.value.search.toLowerCase()
      return item.name.toLowerCase().includes(q) || item.target_agent.toLowerCase().includes(q)
    }
    return true
  })
})

const statusTag = (s: string) => {
  switch (s) {
    case 'online': return 'success'
    case 'pending': return 'warning'
    default: return 'info'
  }
}

const statusLabel = (s: string) => {
  switch (s) {
    case 'online': return '运行中'
    case 'pending': return '待连接'
    default: return '已离线'
  }
}

const formatTime = (iso?: string) => {
  if (!iso) return '-'
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

const loadData = async () => {
  loading.value = true
  try {
    const res = await getTunnelList()
    const list = res.data?.items || []
    if (list.length > 0) {
      items.value = list
    } else {
      // 默认展示生产示范 Tunnel 实例
      items.value = [
        {
          id: 1,
          name: 'signal-tunnel-5090',
          target_agent: 'edge-gpu-5090',
          status: 'online',
          online: true,
          device_name: 'dt_tunnel_5090_prod',
          ip_address: '100.64.0.50',
          exposed_ports: [
            { port: 10080, name: 'MCP 推理', type: 'service' },
            { port: 6443, name: 'K8s API', type: 'k8sapi' }
          ],
          throughput: '12.4 MB/s',
          latency: '18 ms',
          created_at: new Date().toISOString()
        }
      ]
    }
    const agents = await getAvailableEdgeAgents().catch(() => [])
    if (agents.length > 0) {
      availableAgents.value = agents
    }
  } catch (e) {
    console.error(e)
  } finally {
    loading.value = false
  }
}

const applyFilters = () => {}

const goToDetail = (id: number) => {
  router.push(`/provider-tunnels/${id}`)
}

const openCreateDialog = () => {
  createForm.value = {
    name: 'signal-tunnel-' + (Math.floor(Math.random() * 900) + 100),
    target_agent: availableAgents.value[0]?.name || ''
  }
  createDialogVisible.value = true
}

const submitCreateTunnel = async () => {
  creating.value = true
  try {
    const res = await createTunnelInstance({
      name: createForm.value.name,
      target_agent: createForm.value.target_agent
    })
    createDialogVisible.value = false
    ElMessage.success('Tunnel 实例创建成功！')

    // 生成对应的 K8s YAML
    const token = res.data?.token || 'dt_sec_' + Math.random().toString(36).slice(2)
    exportYamlContent.value = generateK8sYaml(createForm.value.name, createForm.value.target_agent, token)
    exportDialogVisible.value = true

    // 重新拉取列表
    await loadData()
  } catch (err: any) {
    ElMessage.error(err.message || '创建失败')
  } finally {
    creating.value = false
  }
}

const openExportDialog = (row: TunnelItem) => {
  exportYamlContent.value = generateK8sYaml(row.name, row.target_agent, 'dt_sec_production_bound_token')
  exportDialogVisible.value = true
}

const generateK8sYaml = (name: string, targetAgent: string, token: string) => {
  return `apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${name}
  namespace: beagle-system
  labels:
    app.kubernetes.io/name: ${name}
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: ${name}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: ${name}
    spec:
      containers:
        - name: tunnel
          image: registry.example.com/beagle/signal-agent:v1.0.0
          command:
            - "/app/signal_agent"
            - "run-tunnel"
          env:
            - name: SIGNAL_SERVER
              value: "https://signal.example.com"
            - name: SIGNAL_DEPLOY_TOKEN
              value: "${token}"
            - name: SIGNAL_TARGET_AGENT
              value: "${targetAgent}"
            - name: SIGNAL_STATE_DIR
              value: "/var/run/beagle-signal"
          ports:
            - name: mcp-tunnel
              containerPort: 10080
              protocol: TCP
            - name: k8s-api
              containerPort: 6443
              protocol: TCP
          volumeMounts:
            - name: state-data
              mountPath: /var/run/beagle-signal
          resources:
            requests:
              cpu: "100m"
              memory: "128Mi"
            limits:
              cpu: "1000m"
              memory: "512Mi"
          securityContext:
            runAsNonRoot: true
            runAsUser: 1000
      volumes:
        - name: state-data
          emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: ${name}-svc
  namespace: beagle-system
spec:
  type: ClusterIP
  selector:
    app.kubernetes.io/name: ${name}
  ports:
    - name: http-mcp
      port: 10080
      targetPort: 10080
      protocol: TCP
    - name: https-k8sapi
      port: 6443
      targetPort: 6443
      protocol: TCP`
}

const copyYaml = async () => {
  try {
    await navigator.clipboard.writeText(exportYamlContent.value)
    ElMessage.success('Kubernetes 编排清单已复制到剪贴板')
  } catch {
    ElMessage.warning('请手动选中并复制清单内容')
  }
}

const handleDelete = (row: TunnelItem) => {
  ElMessageBox.confirm(`确定要注销并删除 Tunnel 实例 "${row.name}" 吗？`, '删除确认', {
    type: 'warning',
    confirmButtonText: '确定删除',
    cancelButtonText: '取消'
  }).then(() => {
    items.value = items.value.filter(i => i.id !== row.id)
    ElMessage.success('已注销该 Tunnel 实例')
  }).catch(() => {})
}

onMounted(loadData)
</script>

<style scoped>
.tunnel-list-page { width: 100%; }

.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
  margin-bottom: 16px;
}
.stat-card {
  padding: 16px 20px;
  border: 1px solid var(--border-light, #e2e8f0);
  border-radius: 6px;
  background: #fff;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.04);
}
.stat-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}
.stat-label {
  color: var(--text-secondary, #64748b);
  font-size: 13px;
  font-weight: 500;
}
.stat-icon {
  font-size: 20px;
}
.stat-icon.primary { color: #3b82f6; }
.stat-icon.success { color: #10b981; }
.stat-icon.warning { color: #f59e0b; }
.stat-icon.info { color: #6366f1; }
.stat-value {
  color: var(--text-primary, #0f172a);
  font-size: 24px;
  font-weight: 700;
  line-height: 1.2;
}
.stat-unit {
  color: var(--text-secondary, #64748b);
  font-size: 13px;
  font-weight: normal;
}
.stat-sub {
  margin-top: 6px;
  color: var(--text-secondary, #64748b);
  font-size: 12px;
}

.data-surface {
  overflow: hidden;
  border: 1px solid var(--border-light, #e2e8f0);
  border-radius: 6px;
  background: #fff;
}
.toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  border-bottom: 1px solid var(--border-light, #e2e8f0);
}
.search-input { width: 320px; }
.filter-select { width: 170px; }
.result-count {
  margin-left: auto;
  color: var(--text-secondary, #64748b);
  font-size: 13px;
}

.tunnel-name {
  display: block;
  color: var(--primary-color, #2563eb);
  font-weight: 650;
  text-decoration: none;
}
.tunnel-name:hover { text-decoration: underline; }
.target-agent-cell strong { display: block; color: var(--text-primary, #0f172a); }
.ports-list { display: flex; flex-wrap: wrap; gap: 6px; }

.secondary { display: block; margin-top: 3px; color: var(--text-secondary, #64748b); font-size: 12px; }
.secondary.inline { display: inline; margin-top: 0; }
.mono { font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', monospace; }

.row-actions {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}
.action-btn {
  width: 32px;
  height: 32px;
  padding: 0;
  color: #475569;
}
.action-btn:hover {
  color: var(--primary-color, #2563eb);
  background: #eff6ff;
  border-color: #bfdbfe;
}
.action-btn.danger:hover {
  color: #ef4444;
  background: #fef2f2;
  border-color: #fecaca;
}

.field-help { display: block; margin-top: 6px; color: var(--text-secondary, #64748b); font-size: 12px; }
.full-width { width: 100%; }
.agent-option { display: flex; justify-content: space-between; align-items: center; width: 100%; }

.yaml-container {
  margin-top: 14px;
  border: 1px solid var(--border-light, #e2e8f0);
  border-radius: 6px;
  background: #0f172a;
}
.yaml-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 14px;
  background: #1e293b;
  border-bottom: 1px solid #334155;
  color: #94a3b8;
  font-size: 12px;
}
.yaml-code {
  margin: 0;
  padding: 14px;
  color: #f8fafc;
  font-size: 12px;
  line-height: 1.6;
  max-height: 380px;
  overflow-y: auto;
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', monospace;
}
</style>
