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
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/api"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	grpcserver "github.com/open-beagle/awecloud-signaling-server/internal/server/grpc"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/proxy"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

type Server struct {
	config     *config.ServerConfig
	httpServer *http.Server
	grpcServer *grpc.Server

	// gRPC服务
	agentService  *grpcserver.AgentServiceServer
	clientService *grpcserver.ClientServiceServer

	// ACL 同步服务
	aclSyncService *headscale.ACLSyncService
	aclSyncCtx     context.Context
	aclSyncCancel  context.CancelFunc
}

// GetAgentService 获取AgentService（供API使用）
func (s *Server) GetAgentService() *grpcserver.AgentServiceServer {
	return s.agentService
}

// GetACLSyncService 获取ACLSyncService（供API使用）
func (s *Server) GetACLSyncService() *headscale.ACLSyncService {
	return s.aclSyncService
}

func NewServer(cfg *config.ServerConfig) (*Server, error) {
	// 初始化数据库
	if err := db.InitDB(cfg.Database); err != nil {
		return nil, fmt.Errorf("初始化数据库失败: %w", err)
	}

	// 创建默认管理员
	if err := db.CreateDefaultAdmin(
		cfg.Web.DefaultAdminUsername,
		cfg.Web.DefaultAdminPassword,
	); err != nil {
		return nil, fmt.Errorf("创建默认管理员失败: %w", err)
	}

	// 初始化 ACL 同步服务
	var aclSyncService *headscale.ACLSyncService
	var aclSyncCtx context.Context
	var aclSyncCancel context.CancelFunc

	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		logger.Info("初始化 Headscale ACL 同步服务")
		client := headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
		aclSyncService = headscale.NewACLSyncService(client)
		aclSyncCtx, aclSyncCancel = context.WithCancel(context.Background())
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
	// 根据配置的日志级别设置 Gin 模式
	switch s.config.Log.Level {
	case "debug":
		gin.SetMode(gin.DebugMode)
		logger.Info("Gin 运行在 Debug 模式")
	case "info", "warn", "error":
		gin.SetMode(gin.ReleaseMode)
		logger.Info("Gin 运行在 Release 模式")
	default:
		gin.SetMode(gin.ReleaseMode)
		logger.Info("Gin 运行在 Release 模式（默认）")
	}

	// 创建gRPC服务
	s.agentService = grpcserver.NewAgentServiceServerWithConfig(s.config)
	s.clientService = grpcserver.NewClientServiceServer(s.config)
	s.clientService.SetAgentService(s.agentService)

	s.grpcServer = grpc.NewServer()
	pb.RegisterAgentServiceServer(s.grpcServer, s.agentService)
	pb.RegisterClientServiceServer(s.grpcServer, s.clientService)

	// 创建Gin路由
	ginRouter := s.setupRouter()

	// 创建统一处理器（HTTP/2: 同时支持HTTP和gRPC）
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(
			r.Header.Get("Content-Type"), "application/grpc") {
			s.grpcServer.ServeHTTP(w, r)
		} else {
			ginRouter.ServeHTTP(w, r)
		}
	})

	// 创建HTTP/2服务器
	h2s := &http2.Server{}

	// 构建监听地址
	listenAddr := s.config.Web.ListenAddr
	if listenAddr == "" || listenAddr == "0.0.0.0" {
		listenAddr = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%d", listenAddr, s.config.Web.ListenPort)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: h2c.NewHandler(handler, h2s),
	}

	// 启动统一服务器（HTTP + gRPC）
	go func() {
		logger.Infof("Server启动在: http://%s", addr)
		logger.Infof("  - Web管理界面: http://%s/", addr)
		logger.Infof("  - RESTful API: http://%s/api/...", addr)
		logger.Infof("  - gRPC服务: %s (HTTP/2)", addr)

		listener, err := net.Listen("tcp4", addr)
		if err != nil {
			logger.Fatalf("创建监听器失败: %v", err)
		}
		logger.Infof("监听器创建成功: %s (IPv4)", listener.Addr())

		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("服务器启动失败: %v", err)
		}
	}()

	// 启动 ACL 定时同步（如果已配置）
	if s.aclSyncService != nil {
		go s.aclSyncService.StartPeriodicSync(s.aclSyncCtx)
	}

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("正在关闭服务器...")

	// 停止 ACL 同步
	if s.aclSyncCancel != nil {
		s.aclSyncCancel()
	}

	// 优雅关闭
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 停止gRPC服务器
	s.grpcServer.GracefulStop()

	// 停止HTTP服务器
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("服务器关闭失败: %w", err)
	}

	logger.Info("服务器已关闭")
	return nil
}

// customLogger 自定义日志中间件
func (s *Server) customLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		// health接口只在状态变化时打印
		if path == "/health" || path == "/health/ready" {
			if c.Writer.Header().Get("X-Log-Status-Change") == "true" {
				logger.Infof("[gin] %s %s %d %v",
					c.Request.Method,
					path,
					c.Writer.Status(),
					time.Since(start))
			}
			return
		}

		// 其他接口正常打印
		logger.Infof("[gin] %s %s %d %v",
			c.Request.Method,
			path,
			c.Writer.Status(),
			time.Since(start))
	}
}

func (s *Server) setupRouter() *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(s.customLogger())

	// Headscale 反向代理（必须在其他路由之前）
	if s.config.Tailscale.HeadscaleURL != "" {
		headscaleProxy, err := proxy.NewHeadscaleProxy(s.config.Tailscale.HeadscaleURL)
		if err != nil {
			logger.Errorf("创建 Headscale 代理失败: %v", err)
		} else {
			router.Any("/headscale/*proxyPath", headscaleProxy.Handler())
			logger.Infof("Headscale 反向代理已启用: /headscale/* -> %s", s.config.Tailscale.HeadscaleURL)
		}
	}

	// 健康检查接口
	healthAPI := api.NewHealthAPI(s.agentService)
	router.GET("/health", healthAPI.Health)
	router.GET("/health/ready", healthAPI.Ready)

	// 静态文件服务（前端）
	router.Static("/assets", "./web/dist/assets")
	router.StaticFile("/favicon.ico", "./web/dist/favicon.ico")

	// 下载文件服务（客户端下载）
	router.Static("/downloads", "./bin")

	// API路由组
	apiGroup := router.Group("/api")
	{
		// 保留旧版API用于向后兼容（已废弃）
		clientAuthAPI := api.NewClientAuthAPI(s.config)
		apiGroup.POST("/client/auth", clientAuthAPI.Auth)

		// v1 API
		v1Group := apiGroup.Group("/v1")
		{
			// ==================== 公开API ====================
			v1Group.GET("/public/system/config", api.GetPublicSystemConfig)

			// 桌面客户端下载API
			downloadAPI := api.NewDownloadAPI()
			v1Group.GET("/public/download/desktop", downloadAPI.GetDesktopDownload)
			v1Group.GET("/public/download/desktop/direct", downloadAPI.GetDesktopDownloadDirect)
			v1Group.GET("/public/download/desktop/versions", downloadAPI.ListDesktopVersions)

			// 客户端版本检查API
			v1Group.POST("/client/version/check", api.CheckVersion)

			// ==================== 管理员API ====================
			adminGroup := v1Group.Group("/admin")
			{
				// 管理员认证
				adminAPI := api.NewAdminAPI(s.config)
				adminGroup.POST("/auth/login", adminAPI.Login)
				adminGroup.POST("/auth/logout", adminAPI.Logout)

				// 需要管理员认证的路由
				adminAuthGroup := adminGroup.Group("")
				adminAuthGroup.Use(api.AuthMiddleware(s.config.Security.JWTSecret))
				{
					// Agent管理
					agentAPI := api.NewAgentAPI()
					adminAuthGroup.GET("/agents", agentAPI.List)
					adminAuthGroup.GET("/agents/:id", agentAPI.Get)
					adminAuthGroup.POST("/agents", agentAPI.Create)
					adminAuthGroup.PUT("/agents/:id", agentAPI.Update)
					adminAuthGroup.DELETE("/agents/:id", agentAPI.Delete)
					adminAuthGroup.GET("/agents/:id/token", agentAPI.GetToken)
					adminAuthGroup.POST("/agents/:id/regenerate-token", agentAPI.RegenerateToken)

					// Client管理
					clientAPI := api.NewClientAPI()
					adminAuthGroup.GET("/clients", clientAPI.List)
					adminAuthGroup.POST("/clients", clientAPI.Create)
					adminAuthGroup.PUT("/clients/:id/disable", clientAPI.Disable)
					adminAuthGroup.PUT("/clients/:id/enable", clientAPI.Enable)
					adminAuthGroup.DELETE("/clients/:id", clientAPI.Delete)
					adminAuthGroup.POST("/clients/:id/regenerate-secret", clientAPI.RegenerateSecret)

					// 端口映射服务管理（Tailscale）
					proxyServiceAPI := api.NewProxyServiceAPI()
					proxyServiceAPI.SetAgentService(s.agentService)
					adminAuthGroup.GET("/services", proxyServiceAPI.List)
					adminAuthGroup.POST("/services", proxyServiceAPI.Create)
					adminAuthGroup.PUT("/services/:id", proxyServiceAPI.Update)
					adminAuthGroup.DELETE("/services/:id", proxyServiceAPI.Delete)
					adminAuthGroup.PUT("/services/:id/start", proxyServiceAPI.Start)
					adminAuthGroup.PUT("/services/:id/stop", proxyServiceAPI.Stop)
					adminAuthGroup.GET("/services/:id/stats", proxyServiceAPI.Stats)
					adminAuthGroup.GET("/agents/:id/services", proxyServiceAPI.ListByAgent)

					// 服务权限管理（安全架构）
					servicePermAPI := api.NewServicePermissionAPI(s.config)
					adminAuthGroup.GET("/services/permissions", servicePermAPI.ListAllServicePermissions)
					adminAuthGroup.GET("/services/:id/permissions", servicePermAPI.ListServicePermissions)
					adminAuthGroup.POST("/services/:id/permissions", servicePermAPI.AddServicePermission)
					adminAuthGroup.DELETE("/services/:id/permissions/:pid", servicePermAPI.RemoveServicePermission)
					adminAuthGroup.PUT("/services/:id/access-type", servicePermAPI.UpdateServiceAccessType)

					// Agent 服务权限管理（安全架构）
					agentPermAPI := api.NewAgentServicePermissionAPI(s.config)
					adminAuthGroup.GET("/agent-permissions", agentPermAPI.ListAgentServicePermissions)
					adminAuthGroup.POST("/agent-permissions", agentPermAPI.AddAgentServicePermission)
					adminAuthGroup.DELETE("/agent-permissions/:id", agentPermAPI.RemoveAgentServicePermission)

					// Tailscale 管理
					tailscaleAPI := api.NewTailscaleAPI(s.config)
					adminAuthGroup.GET("/tailscale/status", tailscaleAPI.Status)
					adminAuthGroup.POST("/tailscale/sync", tailscaleAPI.Sync)
					adminAuthGroup.GET("/tailscale/nodes", tailscaleAPI.Nodes)

					// 组管理
					groupAPI := api.NewGroupAPI()
					adminAuthGroup.GET("/groups", groupAPI.GetGroups)
					adminAuthGroup.POST("/groups", groupAPI.CreateGroup)
					adminAuthGroup.PUT("/groups/:id", groupAPI.UpdateGroup)
					adminAuthGroup.DELETE("/groups/:id", groupAPI.DeleteGroup)
					adminAuthGroup.GET("/groups/:id/members", groupAPI.GetGroupMembers)
					adminAuthGroup.POST("/groups/:id/members", groupAPI.AddGroupMember)
					adminAuthGroup.DELETE("/groups/:id/members/:client_id", groupAPI.RemoveGroupMember)

					// 审计日志
					auditLogAPI := api.NewAuditLogAPI()
					adminAuthGroup.GET("/audit/connection", auditLogAPI.QueryAuditLogs)
					adminAuthGroup.GET("/audit/connection/export", auditLogAPI.ExportAuditLogs)

					// 系统配置
					adminAuthGroup.GET("/system/config", api.GetSystemConfig)
					adminAuthGroup.PUT("/system/config", api.UpdateSystemConfig)

					// 收藏管理
					serviceFavoriteAPI := api.NewServiceFavoriteAPI()
					adminAuthGroup.GET("/favorites", serviceFavoriteAPI.GetAllFavorites)
				}
			}

			// ==================== Client API ====================
			clientGroup := v1Group.Group("/client")
			{
				// Client认证
				deviceTokenAPI := api.NewDeviceTokenAPI(s.config)
				clientGroup.POST("/auth/login", deviceTokenAPI.LoginWithSecret)
				clientGroup.POST("/auth/login/token", deviceTokenAPI.LoginWithToken)

				// 需要Client JWT认证的路由
				clientAuthGroup := clientGroup.Group("")
				clientAuthGroup.Use(api.ClientAuthMiddleware(s.config.Security.JWTSecret))
				{
					// 服务列表
					clientAuthGroup.GET("/services", clientAuthAPI.GetServices)
					clientAuthGroup.GET("/services/v2", clientAuthAPI.GetServicesV2)

					// Tailscale 认证
					clientAuthGroup.POST("/tailscale/auth", clientAuthAPI.GetTailscaleAuth)
					clientAuthGroup.DELETE("/tailscale/disconnect", clientAuthAPI.DisconnectTailscale)

					// Device Token管理
					clientAuthGroup.GET("/auth/login/devices", deviceTokenAPI.ListDevices)
					clientAuthGroup.POST("/auth/login/devices/:device_token/offline", deviceTokenAPI.OfflineDevice)
					clientAuthGroup.DELETE("/auth/login/devices/:device_token", deviceTokenAPI.DeleteDevice)

					// 端口偏好
					portPrefAPI := api.NewPortPreferenceAPI()
					clientAuthGroup.GET("/preferences/port", portPrefAPI.GetPortPreferences)
					clientAuthGroup.POST("/preferences/port", portPrefAPI.SavePortPreference)

					// 服务收藏
					serviceFavoriteAPI := api.NewServiceFavoriteAPI()
					clientAuthGroup.GET("/favorites", serviceFavoriteAPI.GetServiceFavorites)
					clientAuthGroup.POST("/favorites/toggle", serviceFavoriteAPI.ToggleFavorite)
					clientAuthGroup.PUT("/favorites/port", serviceFavoriteAPI.UpdateFavoritePort)

					// 审计日志
					auditLogAPI := api.NewAuditLogAPI()
					clientAuthGroup.POST("/audit/connection", auditLogAPI.RecordConnection)

					// 隧道配置
					tunnelConfigAPI := api.NewTunnelConfigAPI(s.config)
					clientAuthGroup.GET("/tunnel/config", tunnelConfigAPI.GetTunnelConfig)
				}
			}
		}
	}

	// SPA路由支持
	router.NoRoute(func(c *gin.Context) {
		if len(c.Request.URL.Path) >= 4 && c.Request.URL.Path[:4] == "/api" {
			c.JSON(404, gin.H{"error": "Not Found"})
			return
		}
		c.File("./web/dist/index.html")
	})

	return router
}
