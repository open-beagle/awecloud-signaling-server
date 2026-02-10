package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
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

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/telemetry"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/api"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	grpcserver "github.com/open-beagle/awecloud-signaling-server/internal/server/grpc"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
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

	aclSyncService *headscale.ACLSyncService
	aclSyncCtx     context.Context
	aclSyncCancel  context.CancelFunc
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

	var aclSyncService *headscale.ACLSyncService
	var aclSyncCtx context.Context
	var aclSyncCancel context.CancelFunc

	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		logger.Info("初始化 Headscale ACL 同步服务")
		client, err := headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
		if err != nil {
			logger.Warnf("初始化 Headscale 客户端失败: %v", err)
		} else {
			aclSyncService = headscale.NewACLSyncService(client)
			aclSyncCtx, aclSyncCancel = context.WithCancel(context.Background())
		}
	} else {
		logger.Warn("Headscale 配置未设置，ACL 同步服务未启动")
	}

	return &Server{
		config:         cfg,
		aclSyncService: aclSyncService,
		aclSyncCtx:     aclSyncCtx,
		aclSyncCancel:  aclSyncCancel,
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
	s.grpcServer = grpc.NewServer(grpcOpts...)
	pb.RegisterAgentServiceServer(s.grpcServer, s.agentService)
	pb.RegisterDesktopServiceServer(s.grpcServer, s.desktopService)

	ginRouter := s.setupRouter()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			s.grpcServer.ServeHTTP(w, r)
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

	if s.aclSyncService != nil {
		go s.aclSyncService.StartPeriodicSync(s.aclSyncCtx)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("正在关闭服务器...")

	if s.aclSyncCancel != nil {
		s.aclSyncCancel()
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
	router.Use(s.customLogger())

	// CORS 中间件 - 允许所有来源的请求
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")

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
			v1Group.GET("/download/agent", downloadAPI.GetAgentDownload)
			v1Group.GET("/download/agent/version", downloadAPI.GetAgentVersion)

			// 统一注册 API（公开，Agent 和 Client 共用）
			deployPublicAPI := api.NewDeployAPI(s.config)
			v1Group.POST("/register", deployPublicAPI.Register)
			// 兼容旧版 Agent 注册接口（deprecated）
			v1Group.POST("/agent/register", deployPublicAPI.RegisterCompat)

			// 管理员 API
			adminGroup := v1Group.Group("/admin")
			{
				adminAPI := api.NewAdminAPI(s.config)
				adminGroup.POST("/auth/login", adminAPI.Login)
				adminGroup.POST("/auth/logout", adminAPI.Logout)

				adminAuthGroup := adminGroup.Group("")
				adminAuthGroup.Use(api.AuthMiddleware(s.config.Security.JWTSecret))
				{
					adminAuthGroup.GET("/auth/me", adminAPI.GetMe)
					adminAuthGroup.PUT("/auth/password", adminAPI.ChangePassword)

					// 用户管理
					userAPI := api.NewUserAPI(s.config)
					userAPI.SetAgentService(s.agentService)
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
					adminAuthGroup.GET("/nodes", nodeAPI.List)
					adminAuthGroup.GET("/nodes/:id", nodeAPI.Get)
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

					// 域名管理
					domainAPI := api.NewDomainAPI()
					adminAuthGroup.GET("/domains", domainAPI.List)
					adminAuthGroup.POST("/domains/refresh", domainAPI.Refresh)
					adminAuthGroup.DELETE("/domains/:id", domainAPI.Delete)
					adminAuthGroup.PUT("/domains/offline/:user_id", domainAPI.SetOffline)

					// 审计日志
					auditAPI := api.NewAuditLogAPI()
					adminAuthGroup.GET("/audit/logs", auditAPI.QueryAuditLogs)
					adminAuthGroup.GET("/audit/action-types", auditAPI.GetActionTypes)
					adminAuthGroup.GET("/audit/users", auditAPI.GetUsers)

					// 系统配置
					adminAuthGroup.GET("/system/config", api.GetSystemConfig)
					adminAuthGroup.PUT("/system/config", api.UpdateSystemConfig)

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
					clientDomainAPI := api.NewDomainAPI()
					clientAuthGroup.GET("/dns/resolve", clientDomainAPI.Resolve)
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
