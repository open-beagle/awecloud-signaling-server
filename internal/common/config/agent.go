package config

import (
	"os"
	"strconv"

	"github.com/BurntSushi/toml"
)

type AgentConfig struct {
	Agent     AgentSection     `toml:"agent"`
	Server    ServerConnect    `toml:"server"`
	Tunnel    TunnelSection    `toml:"tunnel"`
	Visitor   VisitorSection   `toml:"visitor"`
	Health    HealthSection    `toml:"health"`
	Log       LogConfig        `toml:"log"`
	Telemetry TelemetrySection `toml:"telemetry"`
}

type HealthSection struct {
	Port int `toml:"port"` // 健康检查HTTP端口，默认8090
}

type AgentSection struct {
	AgentName  string `toml:"name"`  // 配置文件中使用 name
	AgentToken string `toml:"token"` // 配置文件中使用 token
}

type ServerConnect struct {
	Address   string `toml:"address"`    // Server地址（HTTP/2统一端口，支持完整URL）
	PublicURL string `toml:"public_url"` // FRP公网地址（可选），如果配置则忽略Server返回的地址
}

// TunnelSection Agent 端隧道配置
type TunnelSection struct {
	StateDir          string `toml:"state_dir"`           // 隧道状态存储目录，支持 ~ 扩展
	StateSyncInterval int    `toml:"state_sync_interval"` // 状态同步到 Server 的间隔（分钟），默认 5
	EnableSSH         bool   `toml:"enable_ssh"`          // 是否启用 SSH，默认 false
}

// VisitorSection Visitor 配置（服务访问）
type VisitorSection struct {
	ListenAddr string `toml:"listen_addr"` // 可选，手动指定监听地址，留空则自动检测局域网 IP
}

func LoadAgentConfig(path string) (*AgentConfig, error) {
	var cfg AgentConfig

	// 读取配置文件
	data, err := os.ReadFile(path)
	if err != nil {
		// 如果配置文件不存在，检查环境变量
		if os.IsNotExist(err) {
			if name := os.Getenv("AGENT_NAME"); name != "" {
				cfg.Agent.AgentName = name
			}
			if token := os.Getenv("AGENT_TOKEN"); token != "" {
				cfg.Agent.AgentToken = token
			}
			if addr := os.Getenv("AGENT_ADDRESS"); addr != "" {
				cfg.Server.Address = addr
			}
			// 如果环境变量已设置，则继续
			if cfg.Agent.AgentName != "" {
				return &cfg, nil
			}
		}
		return nil, err
	}

	// 解析TOML
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// 环境变量优先级更高（覆盖配置文件）
	if name := os.Getenv("AGENT_NAME"); name != "" {
		cfg.Agent.AgentName = name
	}
	if token := os.Getenv("AGENT_TOKEN"); token != "" {
		cfg.Agent.AgentToken = token
	}
	if addr := os.Getenv("AGENT_ADDRESS"); addr != "" {
		cfg.Server.Address = addr
	}
	if publicURL := os.Getenv("AGENT_PUBLIC_URL"); publicURL != "" {
		cfg.Server.PublicURL = publicURL
	}
	if stateDir := os.Getenv("TUNNEL_STATE_DIR"); stateDir != "" {
		cfg.Tunnel.StateDir = stateDir
	}
	if syncInterval := os.Getenv("TUNNEL_STATE_SYNC_INTERVAL"); syncInterval != "" {
		// 尝试解析为整数
		if interval, err := strconv.Atoi(syncInterval); err == nil {
			cfg.Tunnel.StateSyncInterval = interval
		}
	}
	if enableSSH := os.Getenv("TUNNEL_ENABLE_SSH"); enableSSH != "" {
		cfg.Tunnel.EnableSSH = enableSSH == "true" || enableSSH == "1"
	}

	// 设置默认值
	if cfg.Health.Port == 0 {
		cfg.Health.Port = 8090
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Server.Address == "" {
		cfg.Server.Address = "http://localhost:8080"
	}
	if cfg.Tunnel.StateDir == "" {
		cfg.Tunnel.StateDir = "~/.config/awecloud-signaling/"
	}
	if cfg.Tunnel.StateSyncInterval == 0 {
		cfg.Tunnel.StateSyncInterval = 5
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
