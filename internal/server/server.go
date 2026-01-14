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

	// gRPC 服务
	agentService   *grpcserver.AgentServiceServer
	desktopService *grpcserver.DesktopServiceServer

	// ACL 同步服务
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
	// 根据配置的日志级别设置 Gin 模式
	switch s.config.Log.Level {
	case "debug":
		gin.SetMode(gin.DebugMode)
		logger.Info("Gin 运行在 Debug 模式")
	default:
		gin.SetMode(gin.ReleaseMode)
		logger.Info("Gin 运行在 Release 模式")
	}

	// 创建 gRPC 服务
	s.agentService = grpcserver.NewAgentServiceServer(s.config)
	s.desktopService = grpcserver.NewDesktopServiceServer(s.config)
	s.desktopService.SetAgentService(s.agentService)

	s.grpcServer = grpc.NewServer()
	pb.RegisterAgentServiceServer(s.grpcServer, s.agentService)
	pb.RegisterDesktopServiceServer(s.grpcServer, s.desktopService)

	// 创建 Gin 路由
	ginRouter := s.setupRouter()

	// 创建统一处理器（HTTP/2: 同时支持 HTTP 和 gRPC）
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			s.grpcServer.ServeHTTP(w, r)
		} else {
			ginRouter.ServeHTTP(w, r)
		}
	})

	// 创建 HTTP/2 服务器
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

	// 停止 gRPC 服务器
	s.grpcServer.GracefulStop()

	// 停止 HTTP 服务器
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

		// health 接口只在状态变化时打印
		if path == "/health" || path == "/health/ready" {
			if c.Writer.Header().Get("X-Log-Status-Change") == "true" {
				logger.Infof("[gin] %s %s %d %v", c.Request.Method, path, c.Writer.Status(), time.Since(start))
			}
			return
		}

		// 其他接口正常打印
		logger.Infof("[gin] %s %s %d %v", c.Request.Method, path, c.Writer.Status(), time.Since(start))
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
			// 代理 /headscale/* 路径（管理 API）
			router.Any("/headscale/*proxyPath", headscaleProxy.Handler())
			logger.Infof("Headscale 反向代理已启用: /headscale/* -> %s", s.config.Tailscale.HeadscaleURL)
		}

		// 创建 Tailscale 控制平面代理（根路径）
		tailscaleProxy, err := proxy.NewTailscaleControlProxy(s.config.Tailscale.HeadscaleURL)
		if err != nil {
			logger.Errorf("创建 Tailscale 控制平面代理失败: %v", err)
		} else {
			// 代理 Tailscale 客户端需要的路径
			// 这些路径是 Tailscale 客户端硬编码的，不能修改
			router.Any("/ts2021", tailscaleProxy.Handler())
			router.Any("/key", tailscaleProxy.Handler())
			router.Any("/machine/*path", tailscaleProxy.Handler())
			router.Any("/noise", tailscaleProxy.Handler())
			router.Any("/derp", tailscaleProxy.Handler())
			router.Any("/derp/*path", tailscaleProxy.Handler())
			router.Any("/bootstrap-dns", tailscaleProxy.Handler())
			logger.Infof("Tailscale 控制平面代理已启用: /ts2021, /key, /machine/*, /noise, /derp/* -> %s", s.config.Tailscale.HeadscaleURL)
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

	// API 路由组
	apiGroup := router.Group("/api")
	{
		// v1 API
		v1Group := apiGroup.Group("/v1")
		{
			// ==================== 公开 API ====================
			v1Group.GET("/public/system/config", api.GetPublicSystemConfig)

			// ==================== 管理员 API ====================
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
					// 管理员信息
					adminAuthGroup.GET("/auth/me", adminAPI.GetMe)
					adminAuthGroup.PUT("/auth/password", adminAPI.ChangePassword)

					// Agent 管理
					agentAPI := api.NewAgentAPI(s.config)
					agentAPI.SetAgentService(s.agentService)
					adminAuthGroup.GET("/agents", agentAPI.List)
					adminAuthGroup.GET("/agents/:id", agentAPI.Get)
					adminAuthGroup.GET("/agents/:id/realtime", agentAPI.GetRealtime)
					adminAuthGroup.GET("/agents/:id/services", agentAPI.GetServices)
					adminAuthGroup.GET("/agents/:id/forwards", agentAPI.GetForwards)
					adminAuthGroup.POST("/agents", agentAPI.Create)
					adminAuthGroup.PUT("/agents/:id", agentAPI.Update)
					adminAuthGroup.DELETE("/agents/:id", agentAPI.Delete)
					adminAuthGroup.POST("/agents/:id/regenerate-secret", agentAPI.RegenerateSecret)

					// Client 管理
					clientAPI := api.NewClientAPI(s.config)
					adminAuthGroup.GET("/clients", clientAPI.List)
					adminAuthGroup.GET("/clients/:id", clientAPI.Get)
					adminAuthGroup.GET("/clients/:id/groups", clientAPI.GetGroups)
					adminAuthGroup.GET("/clients/:id/desktops", clientAPI.GetDesktops)
					adminAuthGroup.GET("/clients/:id/services", clientAPI.GetServices)
					adminAuthGroup.POST("/clients", clientAPI.Create)
					adminAuthGroup.PUT("/clients/:id", clientAPI.Update)
					adminAuthGroup.DELETE("/clients/:id", clientAPI.Delete)
					adminAuthGroup.POST("/clients/:id/desktops/:did/logout", clientAPI.LogoutDesktop)
					adminAuthGroup.DELETE("/clients/:id/desktops/:did", clientAPI.DeleteDesktop)
					adminAuthGroup.POST("/clients/:id/regenerate-secret", clientAPI.RegenerateSecret)

					// 端口映射服务管理
					serviceAPI := api.NewProxyServiceAPI(s.config)
					adminAuthGroup.GET("/services", serviceAPI.List)
					adminAuthGroup.POST("/services", serviceAPI.Create)

					// 端口转发管理
					forwardAPI := api.NewPortForwardAPI(s.config)
					adminAuthGroup.POST("/port-forwards", forwardAPI.Create)
					adminAuthGroup.PUT("/port-forwards/:id", forwardAPI.Update)
					adminAuthGroup.PUT("/port-forwards/:id/toggle", forwardAPI.Toggle)
					adminAuthGroup.POST("/port-forwards/:id/retry", forwardAPI.Retry)
					adminAuthGroup.DELETE("/port-forwards/:id", forwardAPI.Delete)

					// 服务权限管理（放在 /services/:id 之前，避免路由冲突）
					permAPI := api.NewServicePermissionAPI(s.config)
					// 全局权限查询
					adminAuthGroup.GET("/services/permissions", permAPI.GetAllClientPermissions)
					adminAuthGroup.GET("/agent-permissions", permAPI.GetAllAgentPermissions)

					// 单个服务操作（放在具体路径之后）
					adminAuthGroup.GET("/services/:id", serviceAPI.Get)
					adminAuthGroup.PUT("/services/:id", serviceAPI.Update)
					adminAuthGroup.PUT("/services/:id/toggle", serviceAPI.Toggle)
					adminAuthGroup.POST("/services/:id/retry", serviceAPI.Retry)
					adminAuthGroup.DELETE("/services/:id", serviceAPI.Delete)

					// 桌面授权 - 用户
					adminAuthGroup.GET("/services/:id/clients", permAPI.GetClients)
					adminAuthGroup.POST("/services/:id/clients", permAPI.AddClient)
					adminAuthGroup.DELETE("/services/:id/clients/:cid", permAPI.RemoveClient)
					// 桌面授权 - 用户分组
					adminAuthGroup.GET("/services/:id/client-groups", permAPI.GetClientGroups)
					adminAuthGroup.POST("/services/:id/client-groups", permAPI.AddClientGroup)
					adminAuthGroup.DELETE("/services/:id/client-groups/:gid", permAPI.RemoveClientGroup)
					// 代理授权 - Agent
					adminAuthGroup.GET("/services/:id/agents", permAPI.GetAgents)
					adminAuthGroup.POST("/services/:id/agents", permAPI.AddAgent)
					adminAuthGroup.DELETE("/services/:id/agents/:aid", permAPI.RemoveAgent)
					// 代理授权 - Agent 分组
					adminAuthGroup.GET("/services/:id/agent-groups", permAPI.GetAgentGroups)
					adminAuthGroup.POST("/services/:id/agent-groups", permAPI.AddAgentGroup)
					adminAuthGroup.DELETE("/services/:id/agent-groups/:gid", permAPI.RemoveAgentGroup)

					// 分组管理
					groupAPI := api.NewGroupAPI(s.config)
					// 用户分组
					adminAuthGroup.GET("/client-groups", groupAPI.ListClientGroups)
					adminAuthGroup.GET("/client-groups/:id", groupAPI.GetClientGroup)
					adminAuthGroup.POST("/client-groups", groupAPI.CreateClientGroup)
					adminAuthGroup.PUT("/client-groups/:id", groupAPI.UpdateClientGroup)
					adminAuthGroup.DELETE("/client-groups/:id", groupAPI.DeleteClientGroup)
					adminAuthGroup.GET("/client-groups/:id/members", groupAPI.GetClientGroupMembers)
					adminAuthGroup.POST("/client-groups/:id/members", groupAPI.AddClientGroupMember)
					adminAuthGroup.DELETE("/client-groups/:id/members/:cid", groupAPI.RemoveClientGroupMember)
					// 代理分组
					adminAuthGroup.GET("/agent-groups", groupAPI.ListAgentGroups)
					adminAuthGroup.GET("/agent-groups/:id", groupAPI.GetAgentGroup)
					adminAuthGroup.POST("/agent-groups", groupAPI.CreateAgentGroup)
					adminAuthGroup.PUT("/agent-groups/:id", groupAPI.UpdateAgentGroup)
					adminAuthGroup.DELETE("/agent-groups/:id", groupAPI.DeleteAgentGroup)
					adminAuthGroup.GET("/agent-groups/:id/members", groupAPI.GetAgentGroupMembers)
					adminAuthGroup.POST("/agent-groups/:id/members", groupAPI.AddAgentGroupMember)
					adminAuthGroup.DELETE("/agent-groups/:id/members/:aid", groupAPI.RemoveAgentGroupMember)

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
					// User 管理
					adminAuthGroup.GET("/tunnel/users", tunnelAPI.ListTunnelUsers)
					adminAuthGroup.GET("/tunnel/users/:id", tunnelAPI.GetTunnelUser)
					adminAuthGroup.PUT("/tunnel/users/:id", tunnelAPI.UpdateTunnelUser)
					adminAuthGroup.DELETE("/tunnel/users/:id", tunnelAPI.DeleteTunnelUser)
					adminAuthGroup.GET("/tunnel/users/:id/nodes", tunnelAPI.GetTunnelUserNodes)
					// Node 管理
					adminAuthGroup.GET("/tunnel/nodes", tunnelAPI.ListTunnelNodes)
					adminAuthGroup.GET("/tunnel/nodes/:id", tunnelAPI.GetTunnelNode)
					adminAuthGroup.PUT("/tunnel/nodes/:id", tunnelAPI.UpdateTunnelNode)
					adminAuthGroup.PUT("/tunnel/nodes/:id/tags", tunnelAPI.UpdateTunnelNodeTags)
					adminAuthGroup.DELETE("/tunnel/nodes/:id", tunnelAPI.DeleteTunnelNode)
					adminAuthGroup.GET("/tunnel/tags", tunnelAPI.GetTunnelTags)
					// ACL 管理
					adminAuthGroup.GET("/tunnel/acl", tunnelAPI.GetTunnelACL)
					adminAuthGroup.PUT("/tunnel/acl", tunnelAPI.UpdateTunnelACL)
					adminAuthGroup.GET("/tunnel/acl/rules", tunnelAPI.GetTunnelACLRules)
					adminAuthGroup.POST("/tunnel/acl/sync", tunnelAPI.SyncTunnelACL)
				}
			}

			// ==================== Client API ====================
			clientGroup := v1Group.Group("/client")
			{
				// Client 认证
				deviceTokenAPI := api.NewDeviceTokenAPI(s.config)
				clientGroup.POST("/auth/login", deviceTokenAPI.LoginWithSecret)
				clientGroup.POST("/auth/login/token", deviceTokenAPI.LoginWithToken)

				// 需要 Client JWT 认证的路由
				clientAuthGroup := clientGroup.Group("")
				clientAuthGroup.Use(api.ClientAuthMiddleware(s.config.Security.JWTSecret))
				{
					// 隧道配置
					tunnelConfigAPI := api.NewTunnelConfigAPI(s.config)
					clientAuthGroup.GET("/tunnel/config", tunnelConfigAPI.GetTunnelConfig)
				}
			}
		}
	}

	// SPA 路由支持
	router.NoRoute(func(c *gin.Context) {
		if len(c.Request.URL.Path) >= 4 && c.Request.URL.Path[:4] == "/api" {
			c.JSON(404, gin.H{"error": "Not Found"})
			return
		}
		c.File("./web/dist/index.html")
	})

	return router
}
