package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/telemetry"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/api"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	grpcserver "github.com/open-beagle/awecloud-signaling-server/internal/server/grpc"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/proxy"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

type Server struct {
	config     *config.ServerConfig
	httpServer *http.Server
	grpcServer *grpc.Server

	agentService   *grpcserver.AgentServiceServer
	desktopService *grpcserver.DesktopServiceServer
	loginService   *service.DesktopLoginService
	desktopAuthAPI *api.DesktopAuthAPI

	headscaleClient       *headscale.Client
	aclSyncService        *headscale.ACLSyncService
	aclSyncCtx            context.Context
	aclSyncCancel         context.CancelFunc
	reconciliationService *service.ResourceReconciliationService
	reconciliationCtx     context.Context
	reconciliationCancel  context.CancelFunc
}

// GetAgentService 获取 AgentService（供 API 使用）
func (s *Server) GetAgentService() *grpcserver.AgentServiceServer {
	return s.agentService
}

// GetACLSyncService 获取 ACLSyncService（供 API 使用）
func (s *Server) GetACLSyncService() *headscale.ACLSyncService {
	return s.aclSyncService
}

func NewServer(cfg *config.ServerConfig) (*Server, error) {
	if err := db.InitDB(cfg.Database); err != nil {
		return nil, fmt.Errorf("初始化数据库失败: %w", err)
	}

	// 如果配置了 OpenTelemetry，启用 GORM 追踪
	if cfg.Telemetry.Endpoint != "" {
		if err := db.EnableTracing(); err != nil {
			logger.Warnf("启用 GORM 追踪失败: %v", err)
		}
	}

	if err := db.CreateDefaultAdmin(
		cfg.Web.DefaultAdminUsername,
		cfg.Web.DefaultAdminPassword,
	); err != nil {
		return nil, fmt.Errorf("创建默认管理员失败: %w", err)
	}
	if err := db.EnsureBeagleWorkspace(cfg.Web.DefaultAdminUsername); err != nil {
		return nil, fmt.Errorf("初始化 Beagle 默认租户失败: %w", err)
	}
	if err := db.EnsureDefaultAdminIdentity(cfg.Web.DefaultAdminUsername); err != nil {
		return nil, fmt.Errorf("初始化默认管理员统一身份失败: %w", err)
	}

	var headscaleClient *headscale.Client
	var aclSyncService *headscale.ACLSyncService
	var aclSyncCtx context.Context
	var aclSyncCancel context.CancelFunc
	reconciliationCtx, reconciliationCancel := context.WithCancel(context.Background())

	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		logger.Info("初始化 Headscale ACL 同步服务")
		client, err := headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
		if err != nil {
			logger.Warnf("初始化 Headscale 客户端失败: %v", err)
		} else {
			headscaleClient = client
			aclSyncService = headscale.NewACLSyncService(client)
			aclSyncCtx, aclSyncCancel = context.WithCancel(context.Background())
		}
	} else {
		logger.Warn("Headscale 配置未设置，ACL 同步服务未启动")
	}

	return &Server{
		config:                cfg,
		headscaleClient:       headscaleClient,
		aclSyncService:        aclSyncService,
		aclSyncCtx:            aclSyncCtx,
		aclSyncCancel:         aclSyncCancel,
		reconciliationService: service.NewResourceReconciliationService(db.DB),
		reconciliationCtx:     reconciliationCtx,
		reconciliationCancel:  reconciliationCancel,
	}, nil
}

func (s *Server) Run() error {
	switch s.config.Log.Level {
	case "debug":
		gin.SetMode(gin.DebugMode)
		logger.Info("Gin 运行在 Debug 模式")
	default:
		gin.SetMode(gin.ReleaseMode)
		logger.Info("Gin 运行在 Release 模式")
	}

	s.agentService = grpcserver.NewAgentServiceServer(s.config)
	s.desktopService = grpcserver.NewDesktopServiceServer(s.config)
	s.desktopService.SetAgentService(s.agentService)

	// 创建 Desktop 登录服务和认证 API
	s.loginService = service.NewDesktopLoginService(s.config)
	s.desktopService.SetLoginService(s.loginService)
	s.desktopAuthAPI = api.NewDesktopAuthAPI(s.config, s.loginService)

	// 创建 gRPC Server，根据配置启用 OpenTelemetry 拦截器
	var grpcOpts []grpc.ServerOption
	if s.config.Telemetry.Endpoint != "" {
		serverOpts := []otelgrpc.Option{}
		if filter := telemetry.GetGRPCLimiterFilter(); filter != nil {
			serverOpts = append(serverOpts, otelgrpc.WithFilter(filter))
			logger.Info("gRPC OpenTelemetry 追踪已启用（带限流）")
		} else {
			logger.Info("gRPC OpenTelemetry 追踪已启用")
		}
		grpcOpts = append(grpcOpts,
			grpc.StatsHandler(otelgrpc.NewServerHandler(serverOpts...)),
		)
	}
	// 添加 Device Token 认证拦截器
	grpcOpts = append(grpcOpts, grpc.UnaryInterceptor(deviceTokenAuthInterceptor()))
	s.grpcServer = grpc.NewServer(grpcOpts...)
	pb.RegisterAgentServiceServer(s.grpcServer, s.agentService)
	pb.RegisterDesktopServiceServer(s.grpcServer, s.desktopService)

	ginRouter := s.setupRouter()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 记录 gRPC 请求的协议信息，用于诊断 Traefik 转发问题
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			logger.Infof("[gRPC-Debug] proto=%d, method=%s, path=%s, content-type=%s, host=%s",
				r.ProtoMajor, r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.Host)
		}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			if r.ProtoMajor == 2 {
				// HTTP/2：标准 gRPC（集群内部直连或 h2c 正常工作时）
				s.grpcServer.ServeHTTP(w, r)
			} else {
				// HTTP/1.1 + application/grpc：反向代理到本地 gRPC 端口
				// 解决 Traefik h2c 转发降级为 HTTP/1.1 的问题
				logger.Warnf("[gRPC-Debug] HTTP/1.1 gRPC 请求，代理到本地 %d 端口", s.config.Web.GrpcPort)
				grpcProxy := &httputil.ReverseProxy{
					Director: func(req *http.Request) {
						req.URL.Scheme = "http"
						req.URL.Host = fmt.Sprintf("127.0.0.1:%d", s.config.Web.GrpcPort)
					},
					Transport: &http2.Transport{
						AllowHTTP: true,
						DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
							return net.Dial(network, addr)
						},
					},
				}
				grpcProxy.ServeHTTP(w, r)
			}
		} else {
			ginRouter.ServeHTTP(w, r)
		}
	})

	h2s := &http2.Server{}

	listenAddr := s.config.Web.ListenAddr
	if listenAddr == "" || listenAddr == "0.0.0.0" {
		listenAddr = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%d", listenAddr, s.config.Web.ListenPort)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: h2c.NewHandler(handler, h2s),
	}

	go func() {
		logger.Infof("Server 启动在: http://%s", addr)
		logger.Infof("  - Web 管理界面: http://%s/", addr)
		logger.Infof("  - RESTful API: http://%s/api/...", addr)
		logger.Infof("  - gRPC 服务: %s (HTTP/2)", addr)

		listener, err := net.Listen("tcp4", addr)
		if err != nil {
			logger.Fatalf("创建监听器失败: %v", err)
		}
		logger.Infof("监听器创建成功: %s (IPv4)", listener.Addr())

		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("服务器启动失败: %v", err)
		}
	}()

	// 启动独立 gRPC 端口（解决反向代理不支持 h2c 转发的问题）
	grpcAddr := fmt.Sprintf("%s:%d", listenAddr, s.config.Web.GrpcPort)
	go func() {
		grpcListener, err := net.Listen("tcp4", grpcAddr)
		if err != nil {
			logger.Fatalf("gRPC 监听器创建失败: %v", err)
		}
		logger.Infof("gRPC 独立端口启动: %s", grpcAddr)
		if err := s.grpcServer.Serve(grpcListener); err != nil {
			logger.Errorf("gRPC 服务错误: %v", err)
		}
	}()

	if s.aclSyncService != nil {
		if s.config.Tailscale.HeadscaleAutoSync {
			go s.aclSyncService.StartPeriodicSync(s.aclSyncCtx)
		} else {
			logger.Info("Headscale ACL/Tag 自动同步已禁用，显式 Headscale API 仍可用")
		}
	}
	if s.reconciliationService != nil {
		go s.reconciliationService.StartPeriodicMaintenance(s.reconciliationCtx)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("正在关闭服务器...")

	if s.aclSyncCancel != nil {
		s.aclSyncCancel()
	}
	if s.reconciliationCancel != nil {
		s.reconciliationCancel()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s.grpcServer.GracefulStop()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("服务器关闭失败: %w", err)
	}

	logger.Info("服务器已关闭")
	return nil
}

func (s *Server) customLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		if path == "/health" || path == "/health/ready" {
			if c.Writer.Header().Get("X-Log-Status-Change") == "true" {
				logger.Infof("[gin] %s %s %d %v", c.Request.Method, path, c.Writer.Status(), time.Since(start))
			}
			return
		}

		logger.Infof("[gin] %s %s %d %v", c.Request.Method, path, c.Writer.Status(), time.Since(start))
	}
}

func (s *Server) setupRouter() *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(api.RequestMetadataMiddleware())
	router.Use(s.customLogger())

	// CORS 中间件 - 允许所有来源的请求
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, X-Tenant-ID, X-Management-Scope-Type, X-Management-Scope-ID, X-User-Simulation-ID, X-Request-ID, Idempotency-Key, If-Match, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")
		c.Writer.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, ETag")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// OpenTelemetry 中间件
	if s.config.Telemetry.Endpoint != "" {
		router.Use(telemetry.GinMiddleware(s.config.Telemetry.Name))
	}

	// Headscale 反向代理
	if s.config.Tailscale.HeadscaleURL != "" {
		headscaleProxy, err := proxy.NewHeadscaleProxy(s.config.Tailscale.HeadscaleURL)
		if err != nil {
			logger.Errorf("创建 Headscale 代理失败: %v", err)
		} else {
			router.Any("/headscale/*proxyPath", headscaleProxy.Handler())
			logger.Infof("Headscale 反向代理已启用: /headscale/* -> %s", s.config.Tailscale.HeadscaleURL)
		}

		tailscaleProxy, err := proxy.NewTailscaleControlProxy(s.config.Tailscale.HeadscaleURL)
		if err != nil {
			logger.Errorf("创建 Tailscale 控制平面代理失败: %v", err)
		} else {
			router.Any("/ts2021", tailscaleProxy.Handler())
			router.Any("/key", tailscaleProxy.Handler())
			router.Any("/machine/*path", tailscaleProxy.Handler())
			router.Any("/noise", tailscaleProxy.Handler())
			router.Any("/derp", tailscaleProxy.Handler())
			router.Any("/derp/*path", tailscaleProxy.Handler())
			router.Any("/bootstrap-dns", tailscaleProxy.Handler())
			logger.Infof("Tailscale 控制平面代理已启用")
		}
	}

	// 健康检查
	healthAPI := api.NewHealthAPI(s.agentService)
	router.GET("/health", healthAPI.Health)
	router.GET("/health/ready", healthAPI.Ready)

	// Desktop Logto 登录回调（公开）
	if s.desktopAuthAPI != nil && s.desktopAuthAPI.IsLogtoConfigured() {
		router.GET("/auth/desktop/:session_id", s.desktopAuthAPI.DesktopLoginRedirect)
		router.GET("/auth/desktop/callback", s.desktopAuthAPI.DesktopLoginCallback)
		logger.Info("Desktop Logto 登录已启用")
	}

	// Desktop REST API（gRPC 降级兜底，公开接口）
	desktopRESTAPI := api.NewDesktopRESTAPI(s.desktopService, s.loginService)
	desktopRESTGroup := router.Group("/api/v1/desktop")
	{
		// 认证相关（无需预认证）
		desktopRESTGroup.POST("/authenticate", desktopRESTAPI.Authenticate)
		desktopRESTGroup.POST("/create-login-session", desktopRESTAPI.CreateLoginSession)
		desktopRESTGroup.GET("/login-result/:session_id", desktopRESTAPI.GetLoginResult)

		// 需要 Desktop 凭证认证的接口
		desktopRESTGroup.POST("/heartbeat", desktopRESTAPI.Heartbeat)
		desktopRESTGroup.GET("/data", desktopRESTAPI.GetData)
		desktopRESTGroup.POST("/logout", desktopRESTAPI.Logout)
		desktopRESTGroup.GET("/hosts", desktopRESTAPI.GetHosts)
		desktopRESTGroup.GET("/hosts/:host_id/services", desktopRESTAPI.GetHostServices)
		desktopRESTGroup.GET("/devices", desktopRESTAPI.GetDevices)
		desktopRESTGroup.POST("/devices/:token/offline", desktopRESTAPI.OfflineDevice)
		desktopRESTGroup.DELETE("/devices/:token", desktopRESTAPI.DeleteDevice)
		desktopRESTGroup.POST("/favorites/toggle", desktopRESTAPI.ToggleFavorite)
		desktopRESTGroup.GET("/favorites", desktopRESTAPI.GetFavorites)
		desktopRESTGroup.GET("/resolve-domain", desktopRESTAPI.ResolveDomain)
		desktopRESTGroup.GET("/resources", desktopRESTAPI.GetResources)
		desktopRESTGroup.GET("/domains", desktopRESTAPI.GetDomains)
	}
	logger.Info("Desktop REST API 已启用（gRPC 降级兜底）")

	// 静态文件
	webRoot := s.config.Web.WebRoot
	router.Static("/assets", webRoot+"/assets")
	router.StaticFile("/favicon.ico", webRoot+"/favicon.ico")
	router.Static("/downloads", "./bin")

	// API 路由组
	apiGroup := router.Group("/api")
	{
		v1Group := apiGroup.Group("/v1")
		{
			// 公开 API
			v1Group.GET("/public/system/config", api.GetPublicSystemConfig)

			// 下载 API（公开）
			downloadAPI := api.NewDownloadAPI()
			v1Group.GET("/public/download/desktop", downloadAPI.GetDesktopDownload)
			v1Group.GET("/public/download/desktop/direct", downloadAPI.GetDesktopDownloadDirect)
			v1Group.GET("/public/download/desktop/versions", downloadAPI.ListDesktopVersions)
			v1Group.GET("/download/install_agent.sh", downloadAPI.GetAgentInstallScript)
			v1Group.GET("/download/install_signal.sh", downloadAPI.GetSignalInstallScript) // 统一安装脚本
			v1Group.GET("/download/agent", downloadAPI.GetAgentDownload)
			v1Group.GET("/download/agent/version", downloadAPI.GetAgentVersion)
			v1Group.GET("/download/install_endpoint.sh", downloadAPI.GetEndpointInstallScript)
			v1Group.GET("/download/endpoint", downloadAPI.GetEndpointDownload)
			v1Group.GET("/download/endpoint/version", downloadAPI.GetEndpointVersion)

			// 统一注册 API（公开，Agent 和 Client 共用）
			deployPublicAPI := api.NewDeployAPI(s.config)
			v1Group.POST("/register", deployPublicAPI.Register)
			// 兼容旧版 Agent 注册接口（deprecated）
			v1Group.POST("/agent/register", deployPublicAPI.RegisterCompat)

			// 新管理上下文 API。默认关闭，只接受显式映射后的统一 User。
			managementGroup := v1Group.Group("/management")
			managementGroup.Use(api.AuthMiddleware(s.config.Security.JWTSecret, s.config.Security.AllowLocalhostAdminDebug))
			managementGroup.Use(api.RequireFeatureFlag(s.config.FeatureFlags, config.FeatureManagementContextV2, false))
			managementGroup.Use(api.UnifiedManagementIdentityMiddleware())
			{
				managementContextAPI := api.NewManagementContextAPI()
				managementGroup.GET("/contexts", managementContextAPI.List)
				managementGroup.GET("/contexts/current", managementContextAPI.Current)
				userSimulationAPI := api.NewUserSimulationAPI(s.config.Security.UserSimulationMaxHours)
				managementGroup.GET("/user-simulations", api.RequireManagementPermission(service.PermissionPlatformUserSimulationsRead), userSimulationAPI.List)
				managementGroup.POST("/user-simulations", api.ForbidUserSimulation(), api.RequireManagementPermission(service.PermissionPlatformUserSimulationsWrite), api.RequireIdempotencyKey(), userSimulationAPI.Create)
				managementGroup.POST("/user-simulations/:id/revoke", api.RequireManagementPermission(service.PermissionPlatformUserSimulationsWrite), api.RequireIfMatch(), userSimulationAPI.Revoke)
			}

			// 管理员 API
			adminGroup := v1Group.Group("/admin")
			{
				adminAPI := api.NewAdminAPI(s.config)
				adminGroup.POST("/auth/login", adminAPI.Login)
				adminGroup.POST("/auth/logout", adminAPI.Logout)

				adminAuthGroup := adminGroup.Group("")
				adminAuthGroup.Use(api.AuthMiddleware(s.config.Security.JWTSecret, s.config.Security.AllowLocalhostAdminDebug))
				adminAuthGroup.Use(api.ForbidUserSimulation())
				adminAuthGroup.Use(api.ManagementAuthorizationMiddleware())
				{
					adminAuthGroup.GET("/auth/me", adminAPI.GetMe)
					adminAuthGroup.PUT("/auth/password", adminAPI.ChangePassword)

					// 管理账号与 Tenant 管理范围（Platform Admin 专属）
					managementAccountAPI := api.NewManagementAccountAPI()
					adminAuthGroup.GET("/management-accounts", managementAccountAPI.List)
					adminAuthGroup.POST("/management-accounts", managementAccountAPI.Create)
					adminAuthGroup.POST("/management-accounts/:id/password", managementAccountAPI.ResetPassword)
					adminAuthGroup.POST("/management-accounts/:id/enable", managementAccountAPI.Enable)
					adminAuthGroup.POST("/management-accounts/:id/disable", managementAccountAPI.Disable)
					adminAuthGroup.GET("/management-accounts/:id/tenant-memberships", managementAccountAPI.ListTenantMemberships)
					adminAuthGroup.POST("/management-accounts/:id/tenant-memberships", managementAccountAPI.BindTenant)
					adminAuthGroup.POST("/management-accounts/:id/tenant-memberships/:tenant_id/disable", managementAccountAPI.DisableTenantMembership)

					// 用户管理
					userAPI := api.NewUserAPI(s.config)
					userAPI.SetAgentService(s.agentService)
					userAPI.SetDesktopService(s.desktopService)
					adminAuthGroup.GET("/users", userAPI.List)
					adminAuthGroup.GET("/users/:id", userAPI.Get)
					adminAuthGroup.POST("/users", userAPI.Create)
					adminAuthGroup.PUT("/users/:id", userAPI.Update)
					adminAuthGroup.PUT("/users/:id/ssh", userAPI.UpdateSSH)
					adminAuthGroup.PUT("/users/:id/enable", userAPI.Enable)
					adminAuthGroup.PUT("/users/:id/disable", userAPI.Disable)
					adminAuthGroup.DELETE("/users/:id", userAPI.Delete)
					adminAuthGroup.POST("/users/:id/regenerate-secret", userAPI.RegenerateSecret)

					// 部署 Token（统一管理 Agent 和 Client）
					deployAPI := api.NewDeployAPI(s.config)
					adminAuthGroup.POST("/users/:id/deploy-token", deployAPI.CreateDeployToken)
					adminAuthGroup.GET("/users/:id/deploy-tokens", deployAPI.ListDeployTokens)
					adminAuthGroup.GET("/deploy-tokens/:token_id/command", deployAPI.GetDeployCommand)
					adminAuthGroup.DELETE("/deploy-tokens/:token_id", deployAPI.RevokeDeployToken)

					// 设备管理
					nodeAPI := api.NewNodeAPI(s.config)
					nodeAPI.SetAgentService(s.agentService)
					adminAuthGroup.GET("/nodes", nodeAPI.List)
					adminAuthGroup.GET("/nodes/:id", nodeAPI.Get)
					adminAuthGroup.GET("/nodes/:id/capabilities", nodeAPI.GetCapabilities)
					adminAuthGroup.PUT("/nodes/:id/capabilities", nodeAPI.UpdateCapabilities)
					adminAuthGroup.DELETE("/nodes/:id/capabilities", nodeAPI.ResetCapabilities)
					adminAuthGroup.DELETE("/nodes/:id", nodeAPI.Delete)

					// 分组管理
					groupAPI := api.NewGroupAPINew(s.config)
					adminAuthGroup.GET("/groups", groupAPI.List)
					adminAuthGroup.GET("/groups/:id", groupAPI.Get)
					adminAuthGroup.POST("/groups", groupAPI.Create)
					adminAuthGroup.PUT("/groups/:id", groupAPI.Update)
					adminAuthGroup.DELETE("/groups/:id", groupAPI.Delete)
					adminAuthGroup.GET("/groups/:id/members", groupAPI.GetMembers)
					adminAuthGroup.POST("/groups/:id/members", groupAPI.AddMembers)
					adminAuthGroup.DELETE("/groups/:id/members/:uid", groupAPI.RemoveMember)

					// 服务管理
					serviceAPI := api.NewProxyServiceAPI(s.config)
					serviceAPI.SetConfigNotifier(s.agentService)
					adminAuthGroup.GET("/services", serviceAPI.List)
					adminAuthGroup.GET("/services/:id", serviceAPI.Get)
					adminAuthGroup.POST("/services", serviceAPI.Create)
					adminAuthGroup.PUT("/services/:id", serviceAPI.Update)
					adminAuthGroup.PUT("/services/:id/toggle", serviceAPI.Toggle)
					adminAuthGroup.POST("/services/:id/retry", serviceAPI.Retry)
					adminAuthGroup.DELETE("/services/:id", serviceAPI.Delete)

					// 端口转发管理
					forwardAPI := api.NewPortForwardAPI(s.config)
					forwardAPI.SetConfigNotifier(s.agentService)
					adminAuthGroup.POST("/port-forwards", forwardAPI.Create)
					adminAuthGroup.PUT("/port-forwards/:id", forwardAPI.Update)
					adminAuthGroup.PUT("/port-forwards/:id/toggle", forwardAPI.Toggle)
					adminAuthGroup.POST("/port-forwards/:id/retry", forwardAPI.Retry)
					adminAuthGroup.DELETE("/port-forwards/:id", forwardAPI.Delete)

					// ACL 授权管理
					aclAPI := api.NewACLAPI(s.config)
					// 服务授权
					adminAuthGroup.GET("/acl/services", aclAPI.ListServiceACL)
					adminAuthGroup.GET("/acl/services/:id", aclAPI.GetServiceACL)
					adminAuthGroup.POST("/acl/services/:id/users", aclAPI.AddServiceACLUsers)
					adminAuthGroup.POST("/acl/services/:id/groups", aclAPI.AddServiceACLGroups)
					adminAuthGroup.DELETE("/acl/services/:id/users/:uid", aclAPI.RemoveServiceACLUser)
					adminAuthGroup.DELETE("/acl/services/:id/groups/:gid", aclAPI.RemoveServiceACLGroup)
					// 用户授权
					adminAuthGroup.GET("/acl/users", aclAPI.ListUserACL)
					adminAuthGroup.GET("/acl/users/:id", aclAPI.GetUserACL)
					adminAuthGroup.POST("/acl/users/:id/users", aclAPI.AddUserACLUsers)
					adminAuthGroup.POST("/acl/users/:id/groups", aclAPI.AddUserACLGroups)
					adminAuthGroup.DELETE("/acl/users/:id/users/:uid", aclAPI.RemoveUserACLUser)
					adminAuthGroup.DELETE("/acl/users/:id/groups/:gid", aclAPI.RemoveUserACLGroup)
					// 分组授权
					adminAuthGroup.GET("/acl/groups", aclAPI.ListGroupACL)
					adminAuthGroup.GET("/acl/groups/:id", aclAPI.GetGroupACL)
					adminAuthGroup.POST("/acl/groups/:id/users", aclAPI.AddGroupACLUsers)
					adminAuthGroup.POST("/acl/groups/:id/groups", aclAPI.AddGroupACLGroups)
					adminAuthGroup.DELETE("/acl/groups/:id/users/:uid", aclAPI.RemoveGroupACLUser)
					adminAuthGroup.DELETE("/acl/groups/:id/groups/:gid", aclAPI.RemoveGroupACLGroup)
					// SSH 授权
					adminAuthGroup.GET("/acl/ssh", aclAPI.ListSSHACL)
					adminAuthGroup.GET("/acl/ssh/:id", aclAPI.GetSSHACL)
					adminAuthGroup.POST("/acl/ssh/:id/users", aclAPI.AddSSHACLUsers)
					adminAuthGroup.POST("/acl/ssh/:id/groups", aclAPI.AddSSHACLGroups)
					adminAuthGroup.DELETE("/acl/ssh/:id/users/:uid", aclAPI.RemoveSSHACLUser)
					adminAuthGroup.DELETE("/acl/ssh/:id/groups/:gid", aclAPI.RemoveSSHACLGroup)
					// K8S API 授权
					adminAuthGroup.GET("/acl/k8s", aclAPI.ListK8SACL)
					adminAuthGroup.GET("/acl/k8s/:id", aclAPI.GetK8SACL)
					adminAuthGroup.POST("/acl/k8s/:id/users", aclAPI.AddK8SACLUsers)
					adminAuthGroup.POST("/acl/k8s/:id/groups", aclAPI.AddK8SACLGroups)
					adminAuthGroup.DELETE("/acl/k8s/:id/users/:uid", aclAPI.RemoveK8SACLUser)
					adminAuthGroup.DELETE("/acl/k8s/:id/groups/:gid", aclAPI.RemoveK8SACLGroup)
					// K8S API 统一授权（按集群聚合）
					adminAuthGroup.GET("/acl/k8s-unified", aclAPI.ListK8SUnifiedACL)
					// K8S Service 授权
					adminAuthGroup.GET("/acl/k8s-service", aclAPI.ListK8SServiceACL)
					adminAuthGroup.GET("/acl/k8s-service/:id", aclAPI.GetK8SServiceACL)
					adminAuthGroup.POST("/acl/k8s-service/:id/users", aclAPI.AddK8SServiceACLUsers)
					adminAuthGroup.POST("/acl/k8s-service/:id/groups", aclAPI.AddK8SServiceACLGroups)
					adminAuthGroup.DELETE("/acl/k8s-service/:id/users/:uid", aclAPI.RemoveK8SServiceACLUser)
					adminAuthGroup.DELETE("/acl/k8s-service/:id/groups/:gid", aclAPI.RemoveK8SServiceACLGroup)
					// K8S Service 统一授权（按集群聚合）
					adminAuthGroup.GET("/acl/k8s-service-unified", aclAPI.ListK8SServiceUnifiedACL)
					adminAuthGroup.GET("/acl/endpoint-k8sapi", aclAPI.ListEndpointK8SAPIACL)
					adminAuthGroup.GET("/acl/endpoint-k8sapi/:id", aclAPI.GetEndpointK8SAPIACL)
					adminAuthGroup.POST("/acl/endpoint-k8sapi/:id/users", aclAPI.AddEndpointK8SAPIACLUsers)
					adminAuthGroup.POST("/acl/endpoint-k8sapi/:id/groups", aclAPI.AddEndpointK8SAPIACLGroups)
					adminAuthGroup.DELETE("/acl/endpoint-k8sapi/:id/users/:uid", aclAPI.RemoveEndpointK8SAPIACLUser)
					adminAuthGroup.DELETE("/acl/endpoint-k8sapi/:id/groups/:gid", aclAPI.RemoveEndpointK8SAPIACLGroup)
					adminAuthGroup.GET("/acl/endpoint-k8sservice", aclAPI.ListEndpointK8SServiceACL)
					adminAuthGroup.GET("/acl/endpoint-k8sservice/:id", aclAPI.GetEndpointK8SServiceACL)
					adminAuthGroup.POST("/acl/endpoint-k8sservice/:id/users", aclAPI.AddEndpointK8SServiceACLUsers)
					adminAuthGroup.POST("/acl/endpoint-k8sservice/:id/groups", aclAPI.AddEndpointK8SServiceACLGroups)
					adminAuthGroup.DELETE("/acl/endpoint-k8sservice/:id/users/:uid", aclAPI.RemoveEndpointK8SServiceACLUser)
					adminAuthGroup.DELETE("/acl/endpoint-k8sservice/:id/groups/:gid", aclAPI.RemoveEndpointK8SServiceACLGroup)

					// Endpoint 管理（Endpoint 由 Agent 自动发现上报，不支持手动创建）
					endpointAPI := api.NewEndpointAPI(s.config)
					adminAuthGroup.GET("/endpoints", endpointAPI.ListEndpoints)
					adminAuthGroup.GET("/endpoints/:id", endpointAPI.GetEndpointDetail)
					adminAuthGroup.PUT("/endpoints/:id", endpointAPI.UpdateEndpoint)
					adminAuthGroup.DELETE("/endpoints/:id", endpointAPI.RevokeEndpoint)

					// Node Endpoint Token 重新生成
					adminAuthGroup.POST("/nodes/:id/capabilities/endpoint-token/regenerate", nodeAPI.RegenerateEndpointToken)

					// 域名管理
					domainAPI := api.NewDomainAPI(s.headscaleClient)
					adminAuthGroup.GET("/domains", domainAPI.List)
					adminAuthGroup.POST("/domains/refresh", domainAPI.Refresh)
					adminAuthGroup.DELETE("/domains/:id", domainAPI.Delete)
					adminAuthGroup.PUT("/domains/offline/:user_id", domainAPI.SetOffline)

					// 资源发现（管理员查看 K8S Service 发现数据）
					resourceAPI := api.NewResourceAPI(s.config)
					resourceAPI.SetImmediateReportNotifier(s.agentService)
					unifiedResourceAPI := api.NewUnifiedResourceAPI()
					candidateAPI := api.NewResourceCandidateAPI()
					legacyClaimAPI := api.NewLegacyResourceClaimAPI()
					tenantContextAPI := api.NewTenantContextAPI()
					tenantBusinessAPI := api.NewTenantBusinessAPI()
					tenantSettingsAPI := api.NewTenantSettingsAPI()
					tenantAdminMembershipAPI := api.NewTenantAdminMembershipAPI()
					overviewAPI := api.NewOverviewAPI()
					platformAdminAPI := api.NewPlatformAdminAPI()
					adminAuthGroup.GET("/overview/platform", overviewAPI.Platform)
					adminAuthGroup.GET("/platform-admins", platformAdminAPI.List)
					adminAuthGroup.POST("/platform-admins", platformAdminAPI.Create)
					adminAuthGroup.PUT("/platform-admins/:id", platformAdminAPI.Update)
					adminAuthGroup.GET("/platform/resources", unifiedResourceAPI.List)
					adminAuthGroup.GET("/platform/resources/summary", unifiedResourceAPI.Summary)
					adminAuthGroup.GET("/tenant-contexts", tenantContextAPI.List)
					adminAuthGroup.GET("/tenant-contexts/:id", tenantContextAPI.Get)
					adminAuthGroup.GET("/tenant-admin-memberships", tenantAdminMembershipAPI.List)
					adminAuthGroup.GET("/tenant-admin-memberships/options", tenantAdminMembershipAPI.ListAdminOptions)
					adminAuthGroup.POST("/tenant-admin-memberships", tenantAdminMembershipAPI.Create)
					adminAuthGroup.PUT("/tenant-admin-memberships/:id", tenantAdminMembershipAPI.Update)
					adminAuthGroup.GET("/tenants", unifiedResourceAPI.ListTenants)
					adminAuthGroup.POST("/tenants", unifiedResourceAPI.CreateTenant)
					adminAuthGroup.GET("/tenants/:id/overview", overviewAPI.Tenant)
					adminAuthGroup.GET("/tenants/:id/settings", tenantSettingsAPI.Get)
					adminAuthGroup.PUT("/tenants/:id/settings", tenantSettingsAPI.Update)
					adminAuthGroup.GET("/tenants/:id/members", unifiedResourceAPI.ListTenantMembers)
					adminAuthGroup.POST("/tenants/:id/members", unifiedResourceAPI.AddTenantMember)
					adminAuthGroup.POST("/tenants/:id/members/:user_id/disable", unifiedResourceAPI.DisableTenantMember)
					adminAuthGroup.GET("/tenants/:id/member-devices", tenantBusinessAPI.ListMemberDevices)
					adminAuthGroup.GET("/tenants/:id/audit-logs", tenantBusinessAPI.ListAuditLogs)
					adminAuthGroup.GET("/resources", unifiedResourceAPI.List)
					adminAuthGroup.GET("/resources/summary", unifiedResourceAPI.Summary)
					adminAuthGroup.POST("/resources", unifiedResourceAPI.Create)
					adminAuthGroup.POST("/resources/:id/targets", unifiedResourceAPI.ObserveTarget)
					adminAuthGroup.GET("/resource-candidates", candidateAPI.List)
					adminAuthGroup.POST("/resource-candidates", candidateAPI.Observe)
					adminAuthGroup.POST("/resource-candidates/:id/reject", candidateAPI.Reject)
					adminAuthGroup.POST("/resource-candidates/:id/reconcile", candidateAPI.Reconcile)
					adminAuthGroup.GET("/legacy-resource-claims", legacyClaimAPI.List)
					adminAuthGroup.POST("/legacy-resource-claims", legacyClaimAPI.Claim)
					adminAuthGroup.POST("/legacy-resource-claims/:id/revoke", legacyClaimAPI.Revoke)
					workspaceBindingAPI := api.NewWorkspaceBindingAPI()
					adminAuthGroup.GET("/provider-tenant-bindings", workspaceBindingAPI.ListProviderTenantBindings)
					adminAuthGroup.POST("/provider-tenant-bindings", workspaceBindingAPI.CreateProviderTenantBinding)
					adminAuthGroup.GET("/workspace-bindings", workspaceBindingAPI.ListWorkspaceBindings)
					adminAuthGroup.POST("/workspace-bindings", workspaceBindingAPI.CreateWorkspaceBinding)
					adminAuthGroup.GET("/resources/k8s-services", resourceAPI.GetK8SServiceDiscoveries)
					adminAuthGroup.POST("/resources/sync", resourceAPI.SyncK8SServiceDiscovery)
					adminAuthGroup.GET("/resources/:id", unifiedResourceAPI.Get)
					adminAuthGroup.GET("/resources/:id/events", unifiedResourceAPI.ListEvents)
					adminAuthGroup.GET("/resources/:id/grants", unifiedResourceAPI.ListGrants)
					adminAuthGroup.POST("/resources/:id/grants", unifiedResourceAPI.CreateGrant)
					adminAuthGroup.GET("/grants", unifiedResourceAPI.ListAllGrants)
					adminAuthGroup.POST("/grants/:id/revoke", unifiedResourceAPI.RevokeGrant)
					sessionAPI := api.NewContainerSessionAPI()
					adminAuthGroup.GET("/sessions", sessionAPI.List)
					adminAuthGroup.GET("/sessions/:id", sessionAPI.Get)
					adminAuthGroup.POST("/sessions/:id/revoke", sessionAPI.Revoke)
					adminAuthGroup.POST("/sessions/:id/force-disconnect", sessionAPI.ForceDisconnect)

					// 审计日志
					auditAPI := api.NewAuditLogAPI()
					adminAuthGroup.GET("/audit/logs", auditAPI.QueryAuditLogs)
					adminAuthGroup.GET("/audit/action-types", auditAPI.GetActionTypes)
					adminAuthGroup.GET("/audit/users", auditAPI.GetUsers)

					// 操作审计日志
					opAuditAPI := api.NewOperationAuditAPI()
					adminAuthGroup.GET("/audit/operations", opAuditAPI.ListOperationAudit)
					adminAuthGroup.GET("/audit/operation-types", opAuditAPI.GetOperationTypes)

					// 系统配置
					adminAuthGroup.GET("/system/config", api.GetSystemConfig)
					adminAuthGroup.PUT("/system/config", api.UpdateSystemConfig)

					// 版本管理
					versionAPI := api.NewVersionAPI(s.config)
					adminAuthGroup.GET("/version/latest", versionAPI.GetLatest)

					// Updater 发布和更新任务管理
					updaterAPI := api.NewUpdaterAPI()
					adminAuthGroup.POST("/updater/releases", updaterAPI.CreateRelease)
					adminAuthGroup.GET("/updater/releases", updaterAPI.ListReleases)
					adminAuthGroup.GET("/updater/releases/:id", updaterAPI.GetRelease)
					adminAuthGroup.POST("/updater/releases/:id/publish", updaterAPI.PublishRelease)
					adminAuthGroup.POST("/updater/tasks", updaterAPI.CreateTask)
					adminAuthGroup.GET("/updater/tasks", updaterAPI.ListTasks)
					adminAuthGroup.GET("/updater/tasks/:id", updaterAPI.GetTask)
					adminAuthGroup.POST("/updater/tasks/:id/retry", updaterAPI.RetryTask)
					adminAuthGroup.POST("/updater/tasks/:id/cancel", updaterAPI.CancelTask)

					// 隧道管理
					tunnelAPI := api.NewTunnelAPI(s.config)
					adminAuthGroup.GET("/tunnel/users", tunnelAPI.ListTunnelUsers)
					adminAuthGroup.GET("/tunnel/users/:id", tunnelAPI.GetTunnelUser)
					adminAuthGroup.PUT("/tunnel/users/:id", tunnelAPI.UpdateTunnelUser)
					adminAuthGroup.DELETE("/tunnel/users/:id", tunnelAPI.DeleteTunnelUser)
					adminAuthGroup.GET("/tunnel/users/:id/nodes", tunnelAPI.GetTunnelUserNodes)
					adminAuthGroup.GET("/tunnel/nodes", tunnelAPI.ListTunnelNodes)
					adminAuthGroup.GET("/tunnel/nodes/:id", tunnelAPI.GetTunnelNode)
					adminAuthGroup.PUT("/tunnel/nodes/:id", tunnelAPI.UpdateTunnelNode)
					adminAuthGroup.PUT("/tunnel/nodes/:id/tags", tunnelAPI.UpdateTunnelNodeTags)
					adminAuthGroup.DELETE("/tunnel/nodes/:id", tunnelAPI.DeleteTunnelNode)
					adminAuthGroup.GET("/tunnel/tags", tunnelAPI.GetTunnelTags)
					adminAuthGroup.GET("/tunnel/acl", tunnelAPI.GetTunnelACL)
					adminAuthGroup.PUT("/tunnel/acl", tunnelAPI.UpdateTunnelACL)
					adminAuthGroup.GET("/tunnel/acl/rules", tunnelAPI.GetTunnelACLRules)
					adminAuthGroup.POST("/tunnel/acl/sync", tunnelAPI.SyncTunnelACL)
				}
			}

			// Client API
			clientGroup := v1Group.Group("/client")
			{
				deviceTokenAPI := api.NewDeviceTokenAPI(s.config)
				clientGroup.POST("/auth/login", deviceTokenAPI.LoginWithSecret)
				clientGroup.POST("/auth/login/token", deviceTokenAPI.LoginWithToken)

				clientAuthGroup := clientGroup.Group("")
				clientAuthGroup.Use(api.ClientAuthMiddleware(s.config.Security.JWTSecret))
				{
					tunnelConfigAPI := api.NewTunnelConfigAPI(s.config)
					clientAuthGroup.GET("/tunnel/config", tunnelConfigAPI.GetTunnelConfig)

					// 域名解析（Desktop 查询）
					clientDomainAPI := api.NewDomainAPI(s.headscaleClient)
					clientAuthGroup.GET("/dns/resolve", clientDomainAPI.Resolve)

					// 资源发现（Desktop 查询可访问的资源）
					clientResourceAPI := api.NewResourceAPI(s.config)
					clientAuthGroup.GET("/resources", clientResourceAPI.GetResources)
				}
			}
		}
	}

	// SPA 路由支持
	indexFile := webRoot + "/index.html"
	router.NoRoute(func(c *gin.Context) {
		if len(c.Request.URL.Path) >= 4 && c.Request.URL.Path[:4] == "/api" {
			c.JSON(404, gin.H{"error": "Not Found"})
			return
		}
		c.File(indexFile)
	})

	return router
}

// deviceTokenAuthInterceptor gRPC 认证拦截器，处理 Device Token 和 Agent Token 认证
// 从 metadata 中提取 Bearer Token，验证后将 user_id 注入到 context
func deviceTokenAuthInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// 从 metadata 中提取 authorization header
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			// 没有 metadata，继续执行（某些方法可能不需要认证）
			return handler(ctx, req)
		}

		authHeaders := md.Get("authorization")
		if len(authHeaders) == 0 {
			// 没有 authorization header，继续执行
			return handler(ctx, req)
		}

		// 解析 Bearer Token
		authHeader := authHeaders[0]
		if !strings.HasPrefix(authHeader, "Bearer ") {
			// 不是 Bearer Token，继续执行
			return handler(ctx, req)
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == "" {
			// Token 为空，继续执行
			return handler(ctx, req)
		}

		// 先尝试作为 Device Token 查询
		var deviceToken model.DeviceToken
		if err := db.DB.WithContext(ctx).Where("device_token = ?", token).First(&deviceToken).Error; err == nil {
			// Device Token 找到了
			// 检查是否已撤销
			if deviceToken.Revoked {
				logger.Debugf("Device Token 已撤销: client_id=%d", deviceToken.ClientID)
				return handler(ctx, req)
			}

			// 检查是否过期
			if time.Now().After(deviceToken.ExpiresAt) {
				logger.Debugf("Device Token 已过期: client_id=%d", deviceToken.ClientID)
				return handler(ctx, req)
			}

			// 将 user_id 注入到 context（使用 ClientID）
			ctx = context.WithValue(ctx, "user_id", uint64(deviceToken.ClientID))
			logger.Debugf("Device Token 认证成功: user_id=%d", deviceToken.ClientID)

			return handler(ctx, req)
		}

		// Device Token 未找到，尝试作为 Agent Token（Deploy Token）查询
		// Agent Token 是 Deploy Token，存储在 deploy_tokens 表
		var deployToken model.DeployToken
		if err := db.DB.WithContext(ctx).Where("token = ? AND status = ?", token, model.DeployTokenStatusBound).First(&deployToken).Error; err == nil {
			// Deploy Token 找到了
			// 将 user_id 注入到 context
			ctx = context.WithValue(ctx, "user_id", uint64(deployToken.UserID))
			logger.Debugf("Deploy Token 认证成功: user_id=%d", deployToken.UserID)

			return handler(ctx, req)
		}

		// 两种 Token 都未找到，继续执行（让具体方法决定是否需要认证）
		logger.Debugf("Token 未找到或无效")
		return handler(ctx, req)
	}
}
