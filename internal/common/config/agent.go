package config

import (
	"os"

	"github.com/BurntSushi/toml"
)

type AgentConfig struct {
	Agent  AgentSection  `toml:"agent"`
	Server ServerConnect `toml:"server"`
	Log    LogConfig     `toml:"log"`
}

type AgentSection struct {
	AgentName  string `toml:"agent_name"`
	AgentToken string `toml:"agent_token"`
}

type ServerConnect struct {
	Address   string `toml:"address"`
	Port      int    `toml:"port"`      // FRP服务端口
	GRPCPort  int    `toml:"grpc_port"` // gRPC API端口
	Protocol  string `toml:"protocol"`  // tcp, wss
	TLSEnable bool   `toml:"tls_enable"`
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

	// 设置默认值
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 7000
	}
	if cfg.Server.GRPCPort == 0 {
		cfg.Server.GRPCPort = 8081
	}
	if cfg.Server.Protocol == "" {
		cfg.Server.Protocol = "tcp"
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}

	return &cfg, nil
}
