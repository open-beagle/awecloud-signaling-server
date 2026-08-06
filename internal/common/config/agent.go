package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

type AgentConfig struct {
	Agent     AgentSection     `toml:"agent"`
	Tunnel    TunnelSection    `toml:"tunnel"` // 使用 [tunnel] 屏蔽技术细节
	Visitor   VisitorSection   `toml:"visitor"`
	Health    HealthSection    `toml:"health"`
	Log       LogConfig        `toml:"log"`
	Telemetry TelemetrySection `toml:"telemetry"`
	CloudIDE  CloudIDESection  `toml:"cloudide"`  // CloudIDE 专属配置
	K8S       K8SSection       `toml:"k8s"`       // K8S API 代理配置
	SVC       SVCSection       `toml:"svc"`       // K8S Service 发现配置
	Container ContainerSection `toml:"container"` // ContainerSSH 候选发现配置
}

type HealthSection struct {
	Port int `toml:"port"` // 健康检查HTTP端口，默认8090
}

type AgentSection struct {
	AgentToken string `toml:"token"`  // 配置文件中使用 token
	Server     string `toml:"server"` // Server 地址（HTTP/2统一端口，支持完整URL）
}

// TunnelSection Agent 端隧道配置（使用 [tunnel] 屏蔽技术细节）
type TunnelSection struct {
	StateDir          string `toml:"state_dir"`           // 隧道状态存储目录，支持 ~ 扩展
	StateSyncInterval int    `toml:"state_sync_interval"` // 状态同步到 Server 的间隔（分钟），默认 5
	EnableSSH         bool   `toml:"enable_ssh"`          // 是否启用 SSH，默认 false
	SSHPort           int    `toml:"ssh_port"`            // Agent 节点实际 SSH 端口，默认 22
}

// VisitorSection Visitor 配置（服务访问）
type VisitorSection struct {
	ListenAddr string `toml:"listen_addr"` // 可选，手动指定监听地址，留空则自动检测局域网 IP
}

// CloudIDESection CloudIDE 专属配置
type CloudIDESection struct {
	Socks      bool   `toml:"socks"`       // 是否启用 SOCKS5 代理
	SocksAddr  string `toml:"socks_addr"`  // SOCKS5 监听地址，默认 127.0.0.1:1080
	DialSocket string `toml:"dial_socket"` // dial 子命令的 Unix Socket 路径，默认 /tmp/signaling.sock
}

// K8SSection K8S API 代理配置
type K8SSection struct {
	Enabled    bool   `toml:"enabled"`     // 是否启用 K8S API 代理
	Kubeconfig string `toml:"kubeconfig"`  // kubeconfig 路径（空则使用 InCluster）
	APIServer  string `toml:"api_server"`  // K8S API Server 地址（可选覆盖）
	ListenPort int    `toml:"listen_port"` // tsnet 监听端口，默认 50050
}

// SVCSection K8S Service 发现配置
type SVCSection struct {
	Enabled        bool     `toml:"enabled"`          // 是否启用 K8S Service 发现
	Kubeconfig     string   `toml:"kubeconfig"`       // kubeconfig 路径（空则使用 InCluster）
	LabelSelector  string   `toml:"label_selector"`   // Service 标签选择器（如 "signal.beagle.io/expose=true"）
	Namespaces     []string `toml:"namespaces"`       // 监听的命名空间列表（空表示全部）
	ListenPortBase int      `toml:"listen_port_base"` // tsnet gRPC 监听端口，默认 50051
}

// ContainerSection controls the opt-in Pod discovery used for ContainerSSH.
// A restrictive label selector is required so ordinary cluster Pods are not
// treated as SSH candidates.
type ContainerSection struct {
	Enabled            bool     `toml:"enabled"`
	Kubeconfig         string   `toml:"kubeconfig"`
	LabelSelector      string   `toml:"label_selector"`
	Namespaces         []string `toml:"namespaces"`
	ProviderLabel      string   `toml:"provider_label"`
	WorkspaceLabel     string   `toml:"workspace_label"`
	GenerationLabel    string   `toml:"generation_label"`
	ContainerNameLabel string   `toml:"container_name_label"`
	LeaseSeconds       int      `toml:"lease_seconds"`
}

// RegisterResult 统一注册接口的响应结果
// 由 cmd/agent/main.go 调用 HTTP /api/v1/register 后填充
type RegisterResult struct {
	UserRole     string // "agent" 或 "client"
	UserID       uint64 // 用户 ID（Server 端 user.ID）
	HeadscaleURL string // Headscale 控制服务器地址
	AuthKey      string // Headscale 认证密钥
	UserName     string // 用户名
	DeviceName   string // 设备名称（Node.Name，即 DeployToken.Name）
}

// IsClientMode 判断是否为 Client 模式（CloudIDE 等）
func (r *RegisterResult) IsClientMode() bool {
	return r.UserRole == "client"
}

// getEnvWithDeprecation 读取环境变量，优先使用新变量名，旧变量名作为兼容
// 返回值和是否使用了旧变量名
func getEnvWithDeprecation(newKey, oldKey string) (string, bool) {
	if v := os.Getenv(newKey); v != "" {
		return v, false
	}
	if oldKey != "" {
		if v := os.Getenv(oldKey); v != "" {
			return v, true
		}
	}
	return "", false
}

// envBool 解析环境变量为布尔值
func envBool(val string) bool {
	return val == "true" || val == "1"
}

func LoadAgentConfig(path string) (*AgentConfig, error) {
	var cfg AgentConfig
	fileLoaded := false

	// 读取配置文件
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// 配置文件不存在，继续用环境变量
	} else {
		// 解析TOML
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
		fileLoaded = true
	}

	// === SIGNAL_* 环境变量（最高优先级），兼容旧 AGENT_*/TUNNEL_* 变量 ===

	// 认证
	if v, deprecated := getEnvWithDeprecation("SIGNAL_TOKEN", "AGENT_TOKEN"); v != "" {
		cfg.Agent.AgentToken = v
		if deprecated {
			logDeprecation("AGENT_TOKEN", "SIGNAL_TOKEN")
		}
	}
	if v, deprecated := getEnvWithDeprecation("SIGNAL_SERVER", "AGENT_ADDRESS"); v != "" {
		cfg.Agent.Server = v
		if deprecated {
			logDeprecation("AGENT_ADDRESS", "SIGNAL_SERVER")
		}
	}

	// 隧道
	if v, deprecated := getEnvWithDeprecation("SIGNAL_SSH", "TUNNEL_ENABLE_SSH"); v != "" {
		cfg.Tunnel.EnableSSH = envBool(v)
		if deprecated {
			logDeprecation("TUNNEL_ENABLE_SSH", "SIGNAL_SSH")
		}
	}
	if v, deprecated := getEnvWithDeprecation("SIGNAL_STATE_DIR", "TUNNEL_STATE_DIR"); v != "" {
		cfg.Tunnel.StateDir = v
		if deprecated {
			logDeprecation("TUNNEL_STATE_DIR", "SIGNAL_STATE_DIR")
		}
	}

	// 旧变量（补充 SIGNAL_ 对应）
	if v, deprecated := getEnvWithDeprecation("SIGNAL_STATE_SYNC_INTERVAL", "TUNNEL_STATE_SYNC_INTERVAL"); v != "" {
		if interval, err := strconv.Atoi(v); err == nil {
			cfg.Tunnel.StateSyncInterval = interval
		}
		if deprecated {
			logDeprecation("TUNNEL_STATE_SYNC_INTERVAL", "SIGNAL_STATE_SYNC_INTERVAL")
		}
	}
	if v, deprecated := getEnvWithDeprecation("SIGNAL_SSH_PORT", "TUNNEL_SSH_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Tunnel.SSHPort = port
		}
		if deprecated {
			logDeprecation("TUNNEL_SSH_PORT", "SIGNAL_SSH_PORT")
		}
	}

	// CloudIDE 专属（仅 SIGNAL_* 前缀）
	if v := os.Getenv("SIGNAL_SOCKS"); v != "" {
		cfg.CloudIDE.Socks = envBool(v)
	}
	if v := os.Getenv("SIGNAL_SOCKS_ADDR"); v != "" {
		cfg.CloudIDE.SocksAddr = v
	}
	if v := os.Getenv("SIGNAL_DIAL_SOCKET"); v != "" {
		cfg.CloudIDE.DialSocket = v
	}

	// K8S API 代理
	if v := os.Getenv("SIGNAL_K8S_ENABLED"); v != "" {
		cfg.K8S.Enabled = envBool(v)
	}
	if v := os.Getenv("SIGNAL_K8S_KUBECONFIG"); v != "" {
		cfg.K8S.Kubeconfig = v
	}
	if v := os.Getenv("SIGNAL_K8S_API_SERVER"); v != "" {
		cfg.K8S.APIServer = v
	}
	if v := os.Getenv("SIGNAL_K8S_LISTEN_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.K8S.ListenPort = port
		}
	}

	// K8S Service 发现
	if v := os.Getenv("SIGNAL_SVC_ENABLED"); v != "" {
		cfg.SVC.Enabled = envBool(v)
	}
	if v := os.Getenv("SIGNAL_SVC_KUBECONFIG"); v != "" {
		cfg.SVC.Kubeconfig = v
	}
	if v := os.Getenv("SIGNAL_SVC_LABEL_SELECTOR"); v != "" {
		cfg.SVC.LabelSelector = v
	}
	if v := os.Getenv("SIGNAL_SVC_NAMESPACES"); v != "" {
		cfg.SVC.Namespaces = strings.Split(v, ",")
	}
	if v := os.Getenv("SIGNAL_SVC_LISTEN_PORT_BASE"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.SVC.ListenPortBase = port
		}
	}

	// ContainerSSH Pod discovery
	if v := os.Getenv("SIGNAL_CONTAINER_ENABLED"); v != "" {
		cfg.Container.Enabled = envBool(v)
	}
	if v := os.Getenv("SIGNAL_CONTAINER_KUBECONFIG"); v != "" {
		cfg.Container.Kubeconfig = v
	}
	if v := os.Getenv("SIGNAL_CONTAINER_LABEL_SELECTOR"); v != "" {
		cfg.Container.LabelSelector = v
	}
	if v := os.Getenv("SIGNAL_CONTAINER_NAMESPACES"); v != "" {
		cfg.Container.Namespaces = strings.Split(v, ",")
	}
	if v := os.Getenv("SIGNAL_CONTAINER_PROVIDER_LABEL"); v != "" {
		cfg.Container.ProviderLabel = v
	}
	if v := os.Getenv("SIGNAL_CONTAINER_WORKSPACE_LABEL"); v != "" {
		cfg.Container.WorkspaceLabel = v
	}
	if v := os.Getenv("SIGNAL_CONTAINER_GENERATION_LABEL"); v != "" {
		cfg.Container.GenerationLabel = v
	}
	if v := os.Getenv("SIGNAL_CONTAINER_NAME_LABEL"); v != "" {
		cfg.Container.ContainerNameLabel = v
	}
	if v := os.Getenv("SIGNAL_CONTAINER_LEASE_SECONDS"); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil {
			cfg.Container.LeaseSeconds = seconds
		}
	}

	// 日志
	if v := os.Getenv("SIGNAL_LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}

	// 健康检查
	if v := os.Getenv("SIGNAL_HEALTH_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Health.Port = port
		}
	}

	// 如果配置文件不存在,检查是否有足够的环境变量启动
	if !fileLoaded {
		// 有 Token + Server 即可启动
		if cfg.Agent.AgentToken == "" || cfg.Agent.Server == "" {
			return nil, err // 返回原始的文件不存在错误
		}
	}

	// === 设置默认值 ===
	if cfg.Health.Port == 0 {
		cfg.Health.Port = 8090
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	// 注意：Server.Address 的默认值由 cmd/agent/main.go 处理（需要考虑 BUILD_URL）
	if cfg.Tunnel.StateDir == "" {
		cfg.Tunnel.StateDir = "./"
	}
	if cfg.Tunnel.StateSyncInterval == 0 {
		cfg.Tunnel.StateSyncInterval = 5
	}
	if cfg.Tunnel.SSHPort <= 0 {
		cfg.Tunnel.SSHPort = 22
	}

	// CloudIDE 默认值
	if cfg.CloudIDE.SocksAddr == "" {
		cfg.CloudIDE.SocksAddr = "127.0.0.1:1080"
	}
	if cfg.CloudIDE.DialSocket == "" {
		cfg.CloudIDE.DialSocket = "/tmp/signaling.sock"
	}

	// K8S 默认值
	if cfg.K8S.ListenPort == 0 {
		cfg.K8S.ListenPort = 50050
	}
	if cfg.K8S.Kubeconfig == "" {
		cfg.K8S.Kubeconfig = "~/.kube/config"
	}

	// SVC 默认值
	if cfg.SVC.LabelSelector == "" {
		cfg.SVC.LabelSelector = "signal.beagle.io/expose=true"
	}
	if cfg.SVC.ListenPortBase == 0 {
		cfg.SVC.ListenPortBase = 50051
	}

	// ContainerSSH discovery defaults are deliberately opt-in and restrictive.
	if cfg.Container.LabelSelector == "" {
		cfg.Container.LabelSelector = "signal.beagle.io/container-ssh=true"
	}
	// Keep an omitted kubeconfig empty. The shared Kubernetes loader first
	// tries the Pod service account and only falls back to ~/.kube/config when
	// in-cluster configuration is unavailable. Expanding the fallback here
	// bypasses that order and breaks ContainerSSH discovery inside Kubernetes.
	if cfg.Container.ProviderLabel == "" {
		cfg.Container.ProviderLabel = "beagle.io/provider"
	}
	if cfg.Container.WorkspaceLabel == "" {
		cfg.Container.WorkspaceLabel = "beagle.io/workspace"
	}
	if cfg.Container.GenerationLabel == "" {
		cfg.Container.GenerationLabel = "beagle.io/workspace-generation"
	}
	if cfg.Container.ContainerNameLabel == "" {
		cfg.Container.ContainerNameLabel = "beagle.io/container"
	}
	if cfg.Container.LeaseSeconds <= 0 {
		cfg.Container.LeaseSeconds = 120
	}

	// Telemetry 默认值
	if cfg.Telemetry.Name == "" {
		cfg.Telemetry.Name = "signaling-agent"
	}
	if cfg.Telemetry.Namespace == "" {
		cfg.Telemetry.Namespace = "default"
	}
	if cfg.Telemetry.Cluster == "" {
		cfg.Telemetry.Cluster = "default"
	}

	// Telemetry 环境变量覆盖
	if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); endpoint != "" {
		cfg.Telemetry.Endpoint = endpoint
	}
	if serviceName := os.Getenv("OTEL_SERVICE_NAME"); serviceName != "" {
		cfg.Telemetry.Name = serviceName
	}
	if namespace := os.Getenv("OTEL_SERVICE_NAMESPACE"); namespace != "" {
		cfg.Telemetry.Namespace = namespace
	}
	if cluster := os.Getenv("OTEL_SERVICE_CLUSTER"); cluster != "" {
		cfg.Telemetry.Cluster = cluster
	}

	return &cfg, nil
}

// logDeprecation 输出旧环境变量的弃用警告
func logDeprecation(oldKey, newKey string) {
	// 使用 logger 包（如果已初始化），否则用标准输出
	// 注意：LoadAgentConfig 在 logger 初始化之前调用，所以先用 fmt
	fmt.Fprintf(os.Stderr, "[WARN] 环境变量 %s 已弃用，请使用 %s\n", oldKey, newKey)
}
