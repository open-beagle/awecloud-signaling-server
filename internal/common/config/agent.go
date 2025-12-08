package config

import (
	"os"

	"github.com/BurntSushi/toml"
)

type AgentConfig struct {
	Agent  AgentSection  `toml:"agent"`
	Server ServerConnect `toml:"server"`
	Health HealthSection `toml:"health"`
	Log    LogConfig     `toml:"log"`
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

	return &cfg, nil
}
