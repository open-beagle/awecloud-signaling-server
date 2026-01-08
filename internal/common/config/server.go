package config

import (
	"os"

	"github.com/BurntSushi/toml"
)

type ServerConfig struct {
	Server    ServerSection    `toml:"server"`
	Database  DatabaseSection  `toml:"database"`
	Web       WebSection       `toml:"web"`
	Security  SecuritySection  `toml:"security"`
	Log       LogConfig        `toml:"log"`
	Tailscale TailscaleSection `toml:"tailscale"`
}

// ServerSection FRP 配置（废弃，保留兼容）
type ServerSection struct {
	BindAddr          string `toml:"bind_addr"`
	BindPort          int    `toml:"bind_port"`
	TransportProtocol string `toml:"transport_protocol"` // tcp, websocket, wss
	TLSCertFile       string `toml:"tls_cert_file"`
	TLSKeyFile        string `toml:"tls_key_file"`
	Token             string `toml:"token"`           // FRP 认证 Token
	FRPServerAddr     string `toml:"frp_server_addr"` // FRP 服务器地址
	FRPServerPort     int    `toml:"frp_server_port"` // FRP 服务器端口
	PublicURL         string `toml:"public_url"`      // FRP 公网访问地址（完整 URL）

	// WebSocket 路径代理配置（可选）
	// 如果启用，将在 ProxyListenPort 上监听自定义 WebSocket 路径，并转发到 FRP Server
	EnableWebSocketProxy bool   `toml:"enable_websocket_proxy"` // 是否启用 WebSocket 路径代理
	ProxyListenAddr      string `toml:"proxy_listen_addr"`      // 代理监听地址
	ProxyListenPort      int    `toml:"proxy_listen_port"`      // 代理监听端口
	WebSocketPath        string `toml:"websocket_path"`         // WebSocket 路径（如 "/ws"）
}

// TailscaleSection Tailscale 配置
type TailscaleSection struct {
	HeadscaleURL    string `toml:"headscale_url"`     // Headscale API 地址
	HeadscaleAPIKey string `toml:"headscale_api_key"` // Headscale API 密钥（从环境变量获取）
	User            string `toml:"user"`              // Headscale 用户名
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
	if cfg.Server.FRPServerAddr == "" {
		cfg.Server.FRPServerAddr = "127.0.0.1"
	}
	if cfg.Server.FRPServerPort == 0 {
		cfg.Server.FRPServerPort = 7000
	}
	if cfg.Server.ProxyListenAddr == "" {
		cfg.Server.ProxyListenAddr = "0.0.0.0"
	}
	if cfg.Server.ProxyListenPort == 0 {
		cfg.Server.ProxyListenPort = 7001
	}
	if cfg.Server.WebSocketPath == "" {
		cfg.Server.WebSocketPath = "/ws"
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

	// 环境变量覆盖（优先级最高）
	if publicURL := os.Getenv("PUBLIC_URL"); publicURL != "" {
		cfg.Server.PublicURL = publicURL
	}
	if jwtSecret := os.Getenv("JWT_SECRET"); jwtSecret != "" {
		cfg.Security.JWTSecret = jwtSecret
	}
	if token := os.Getenv("TOKEN"); token != "" {
		cfg.Server.Token = token
	}

	// Tailscale 配置默认值
	if cfg.Tailscale.HeadscaleURL == "" {
		cfg.Tailscale.HeadscaleURL = "http://headscale:8080"
	}
	if cfg.Tailscale.User == "" {
		cfg.Tailscale.User = "default"
	}

	// Tailscale 环境变量覆盖
	if headscaleURL := os.Getenv("HEADSCALE_URL"); headscaleURL != "" {
		cfg.Tailscale.HeadscaleURL = headscaleURL
	}
	if headscaleAPIKey := os.Getenv("HEADSCALE_API_KEY"); headscaleAPIKey != "" {
		cfg.Tailscale.HeadscaleAPIKey = headscaleAPIKey
	}
	if user := os.Getenv("HEADSCALE_USER"); user != "" {
		cfg.Tailscale.User = user
	}

	return &cfg, nil
}
