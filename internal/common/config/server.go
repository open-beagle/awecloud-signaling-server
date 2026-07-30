package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

type ServerConfig struct {
	Server       ServerSection       `toml:"server"`
	Database     DatabaseSection     `toml:"database"`
	Web          WebSection          `toml:"web"`
	Security     SecuritySection     `toml:"security"`
	FeatureFlags FeatureFlagsSection `toml:"feature_flags"`
	Log          LogConfig           `toml:"log"`
	Tailscale    TailscaleSection    `toml:"tailscale"`
	Telemetry    TelemetrySection    `toml:"telemetry"`
	Logto        LogtoSection        `toml:"logto"`
}

type FeatureFlag string

const (
	FeatureResourceModelWrite     FeatureFlag = "resource_model_write"
	FeatureResourceReconciliation FeatureFlag = "resource_reconciliation"
	FeatureResourceAllocation     FeatureFlag = "resource_allocation"
	FeatureManagementContextV2    FeatureFlag = "management_context_v2"
	FeatureManagementWebV2        FeatureFlag = "management_web_v2"
	FeatureTenantResourceReadV2   FeatureFlag = "tenant_resource_read_v2"
	FeatureSessionAuthorizationV2 FeatureFlag = "session_authorization_v2"
	FeatureLegacyWriteFreeze      FeatureFlag = "legacy_write_freeze"
)

type FeatureFlagsSection struct {
	ResourceModelWrite     bool `toml:"resource_model_write"`
	ResourceReconciliation bool `toml:"resource_reconciliation"`
	ResourceAllocation     bool `toml:"resource_allocation"`
	ManagementContextV2    bool `toml:"management_context_v2"`
	ManagementWebV2        bool `toml:"management_web_v2"`
	TenantResourceReadV2   bool `toml:"tenant_resource_read_v2"`
	SessionAuthorizationV2 bool `toml:"session_authorization_v2"`
	LegacyWriteFreeze      bool `toml:"legacy_write_freeze"`
}

func (f FeatureFlagsSection) Enabled(flag FeatureFlag) bool {
	switch flag {
	case FeatureResourceModelWrite:
		return f.ResourceModelWrite
	case FeatureResourceReconciliation:
		return f.ResourceReconciliation
	case FeatureResourceAllocation:
		return f.ResourceAllocation
	case FeatureManagementContextV2:
		return f.ManagementContextV2
	case FeatureManagementWebV2:
		return f.ManagementWebV2
	case FeatureTenantResourceReadV2:
		return f.TenantResourceReadV2
	case FeatureSessionAuthorizationV2:
		return f.SessionAuthorizationV2
	case FeatureLegacyWriteFreeze:
		return f.LegacyWriteFreeze
	default:
		return false
	}
}

// TelemetrySection OpenTelemetry 配置
type TelemetrySection struct {
	Endpoint  string `toml:"endpoint"`  // OTLP Endpoint，设置后自动启用
	Name      string `toml:"name"`      // 服务名称
	Namespace string `toml:"namespace"` // 服务命名空间
	Cluster   string `toml:"cluster"`   // 集群标识（业务集群/数据来源）
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
	HeadscaleURL       string `toml:"headscale_url"`        // Headscale API 地址（Server 访问）
	HeadscaleAPIKey    string `toml:"headscale_api_key"`    // Headscale API 密钥（从环境变量获取）
	HeadscalePublicURL string `toml:"headscale_public_url"` // Headscale 公网地址（Agent/Desktop 访问）
	HeadscaleAutoSync  bool   `toml:"headscale_auto_sync"`  // 是否在启动时及每 5 分钟全量同步 ACL/Tag
	// User 字段已废弃，每个 Agent/Desktop 使用独立的 User
	// Agent User: agent-{agent_name}
	// Desktop User: desktop-{client_id}
}

// LogtoSection Logto 配置
type LogtoSection struct {
	Endpoint    string `toml:"endpoint"`     // Logto 端点地址
	AppID       string `toml:"app_id"`       // Logto 应用 ID
	AppSecret   string `toml:"app_secret"`   // Logto 应用密钥
	CallbackURL string `toml:"callback_url"` // OAuth 回调 URL
	Resource    string `toml:"resource"`     // API Resource（可选）
	Scopes      string `toml:"scopes"`       // 请求的 Scopes（逗号分隔）
}

type DatabaseSection struct {
	Type string `toml:"type"` // sqlite
	Path string `toml:"path"`
}

type WebSection struct {
	ListenAddr           string `toml:"listen_addr"`
	ListenPort           int    `toml:"listen_port"`
	GrpcPort             int    `toml:"grpc_port"` // 独立 gRPC 端口（可选，默认 9090）
	WebRoot              string `toml:"web_root"`  // 前端静态文件根目录，默认 ./web/dist
	DefaultAdminUsername string `toml:"default_admin_username"`
	DefaultAdminPassword string `toml:"default_admin_password"`
}

type SecuritySection struct {
	JWTSecret                string `toml:"jwt_secret"`
	JWTExpireHours           int    `toml:"jwt_expire_hours"`
	UserSimulationMaxHours   int    `toml:"user_simulation_max_hours"`
	AllowLocalhostAdminDebug bool   `toml:"allow_localhost_admin_debug"`
}

type LogConfig struct {
	Level string `toml:"level"`
	File  string `toml:"file"`
}

func LoadServerConfig(path string) (*ServerConfig, error) {
	cfg := ServerConfig{
		Tailscale: TailscaleSection{
			HeadscaleAutoSync: true,
		},
	}

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
	if cfg.Web.GrpcPort == 0 {
		cfg.Web.GrpcPort = 9090
	}
	if cfg.Security.JWTExpireHours == 0 {
		cfg.Security.JWTExpireHours = 24
	}
	if cfg.Security.UserSimulationMaxHours <= 0 {
		cfg.Security.UserSimulationMaxHours = 8
	}
	if cfg.Web.WebRoot == "" {
		cfg.Web.WebRoot = "./web/dist"
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}

	// 环境变量覆盖（优先级最高）
	if webRoot := os.Getenv("SIGNAL_WEB_ROOT"); webRoot != "" {
		cfg.Web.WebRoot = webRoot
	}
	if publicURL := os.Getenv("PUBLIC_URL"); publicURL != "" {
		cfg.Server.PublicURL = publicURL
	}
	if jwtSecret := os.Getenv("JWT_SECRET"); jwtSecret != "" {
		cfg.Security.JWTSecret = jwtSecret
	}
	if value := strings.TrimSpace(os.Getenv("SIGNAL_ALLOW_LOCALHOST_ADMIN_DEBUG")); value != "" {
		cfg.Security.AllowLocalhostAdminDebug = parseBool(value)
	}
	applyFeatureFlagEnv(&cfg.FeatureFlags, FeatureResourceModelWrite, "SIGNAL_FEATURE_RESOURCE_MODEL_WRITE")
	applyFeatureFlagEnv(&cfg.FeatureFlags, FeatureResourceReconciliation, "SIGNAL_FEATURE_RESOURCE_RECONCILIATION")
	applyFeatureFlagEnv(&cfg.FeatureFlags, FeatureResourceAllocation, "SIGNAL_FEATURE_RESOURCE_ALLOCATION")
	applyFeatureFlagEnv(&cfg.FeatureFlags, FeatureManagementContextV2, "SIGNAL_FEATURE_MANAGEMENT_CONTEXT_V2")
	applyFeatureFlagEnv(&cfg.FeatureFlags, FeatureManagementWebV2, "SIGNAL_FEATURE_MANAGEMENT_WEB_V2")
	applyFeatureFlagEnv(&cfg.FeatureFlags, FeatureTenantResourceReadV2, "SIGNAL_FEATURE_TENANT_RESOURCE_READ_V2")
	applyFeatureFlagEnv(&cfg.FeatureFlags, FeatureSessionAuthorizationV2, "SIGNAL_FEATURE_SESSION_AUTHORIZATION_V2")
	applyFeatureFlagEnv(&cfg.FeatureFlags, FeatureLegacyWriteFreeze, "SIGNAL_FEATURE_LEGACY_WRITE_FREEZE")
	if token := os.Getenv("TOKEN"); token != "" {
		cfg.Server.Token = token
	}
	if username := os.Getenv("ADMIN_USERNAME"); username != "" {
		cfg.Web.DefaultAdminUsername = username
	}
	if password := os.Getenv("ADMIN_PASSWORD"); password != "" {
		cfg.Web.DefaultAdminPassword = password
	}
	if cfg.Web.DefaultAdminUsername == "" {
		return nil, fmt.Errorf("ADMIN_USERNAME is required when web.default_admin_username is not configured")
	}
	if cfg.Web.DefaultAdminPassword == "" {
		return nil, fmt.Errorf("ADMIN_PASSWORD is required when web.default_admin_password is not configured")
	}

	// Tailscale 配置默认值
	if cfg.Tailscale.HeadscaleURL == "" {
		cfg.Tailscale.HeadscaleURL = "http://headscale:8080"
	}
	if cfg.Tailscale.HeadscalePublicURL == "" {
		// 默认使用 Server 地址 + /headscale 路径
		cfg.Tailscale.HeadscalePublicURL = "http://localhost:8080/headscale"
	}

	// Tailscale 环境变量覆盖
	if headscaleURL := os.Getenv("HEADSCALE_URL"); headscaleURL != "" {
		cfg.Tailscale.HeadscaleURL = headscaleURL
	}
	if headscalePublicURL := os.Getenv("HEADSCALE_PUBLIC_URL"); headscalePublicURL != "" {
		cfg.Tailscale.HeadscalePublicURL = headscalePublicURL
	}
	if headscaleAPIKey := os.Getenv("HEADSCALE_API_KEY"); headscaleAPIKey != "" {
		cfg.Tailscale.HeadscaleAPIKey = headscaleAPIKey
	}
	if value := strings.TrimSpace(os.Getenv("HEADSCALE_AUTO_SYNC")); value != "" {
		cfg.Tailscale.HeadscaleAutoSync = parseBool(value)
	}

	// Telemetry 默认值
	if cfg.Telemetry.Name == "" {
		cfg.Telemetry.Name = "signaling-server"
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

	// Logto 默认值
	if cfg.Logto.Scopes == "" {
		cfg.Logto.Scopes = "openid,profile,email"
	}

	// Logto 环境变量覆盖
	if endpoint := os.Getenv("LOGTO_ENDPOINT"); endpoint != "" {
		cfg.Logto.Endpoint = endpoint
	}
	if appID := os.Getenv("LOGTO_APP_ID"); appID != "" {
		cfg.Logto.AppID = appID
	}
	if appSecret := os.Getenv("LOGTO_APP_SECRET"); appSecret != "" {
		cfg.Logto.AppSecret = appSecret
	}
	if callbackURL := os.Getenv("LOGTO_CALLBACK_URL"); callbackURL != "" {
		cfg.Logto.CallbackURL = callbackURL
	}
	if resource := os.Getenv("LOGTO_RESOURCE"); resource != "" {
		cfg.Logto.Resource = resource
	}
	if scopes := os.Getenv("LOGTO_SCOPES"); scopes != "" {
		cfg.Logto.Scopes = scopes
	}

	return &cfg, nil
}

func applyFeatureFlagEnv(flags *FeatureFlagsSection, flag FeatureFlag, key string) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return
	}
	enabled := parseBool(value)
	switch flag {
	case FeatureResourceModelWrite:
		flags.ResourceModelWrite = enabled
	case FeatureResourceReconciliation:
		flags.ResourceReconciliation = enabled
	case FeatureResourceAllocation:
		flags.ResourceAllocation = enabled
	case FeatureManagementContextV2:
		flags.ManagementContextV2 = enabled
	case FeatureManagementWebV2:
		flags.ManagementWebV2 = enabled
	case FeatureTenantResourceReadV2:
		flags.TenantResourceReadV2 = enabled
	case FeatureSessionAuthorizationV2:
		flags.SessionAuthorizationV2 = enabled
	case FeatureLegacyWriteFreeze:
		flags.LegacyWriteFreeze = enabled
	}
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
