package config

import (
	"os"

	"github.com/BurntSushi/toml"
)

type ServerConfig struct {
	Server   ServerSection   `toml:"server"`
	Database DatabaseSection `toml:"database"`
	Web      WebSection      `toml:"web"`
	Security SecuritySection `toml:"security"`
	Log      LogConfig       `toml:"log"`
}

type ServerSection struct {
	BindAddr          string `toml:"bind_addr"`
	BindPort          int    `toml:"bind_port"`
	TransportProtocol string `toml:"transport_protocol"` // tcp, websocket, wss
	TLSCertFile       string `toml:"tls_cert_file"`
	TLSKeyFile        string `toml:"tls_key_file"`
	FRPAuthToken      string `toml:"frp_auth_token"` // FRP 认证 Token
}

type DatabaseSection struct {
	Type string `toml:"type"` // sqlite
	Path string `toml:"path"`
}

type WebSection struct {
	ListenAddr           string `toml:"listen_addr"`
	ListenPort           int    `toml:"listen_port"`
	DefaultAdminUsername string `toml:"default_admin_username"`
	DefaultAdminPassword string `toml:"default_admin_password"`
}

type SecuritySection struct {
	JWTSecret      string `toml:"jwt_secret"`
	JWTExpireHours int    `toml:"jwt_expire_hours"`
}

type LogConfig struct {
	Level string `toml:"level"`
	File  string `toml:"file"`
}

func LoadServerConfig(path string) (*ServerConfig, error) {
	var cfg ServerConfig

	// 读取配置文件
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// 解析TOML
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// 设置默认值
	if cfg.Server.BindAddr == "" {
		cfg.Server.BindAddr = "0.0.0.0"
	}
	if cfg.Server.BindPort == 0 {
		cfg.Server.BindPort = 7000
	}
	if cfg.Server.TransportProtocol == "" {
		cfg.Server.TransportProtocol = "tcp"
	}
	if cfg.Web.ListenAddr == "" {
		cfg.Web.ListenAddr = "0.0.0.0"
	}
	if cfg.Web.ListenPort == 0 {
		cfg.Web.ListenPort = 8080
	}
	if cfg.Security.JWTExpireHours == 0 {
		cfg.Security.JWTExpireHours = 24
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}

	return &cfg, nil
}
