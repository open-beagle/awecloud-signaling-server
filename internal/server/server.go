package server

import (
	"context"
	"fmt"
	"log"
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
	"github.com/open-beagle/awecloud-signaling-server/internal/server/api"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/frp"
	grpcserver "github.com/open-beagle/awecloud-signaling-server/internal/server/grpc"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

type Server struct {
	config     *config.ServerConfig
	httpServer *http.Server
	grpcServer *grpc.Server

	// gRPC服务
	agentService  *grpcserver.AgentServiceServer
	clientService *grpcserver.ClientServiceServer

	// FRP服务
	frpServer *frp.FRPServer
}

// GetAgentService 获取AgentService（供API使用）
func (s *Server) GetAgentService() *grpcserver.AgentServiceServer {
	return s.agentService
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

	return &Server{
		config: cfg,
	}, nil
}

func (s *Server) Run() error {
	// 设置Gin模式
	gin.SetMode(gin.ReleaseMode)

	// 创建gRPC服务
	s.agentService = grpcserver.NewAgentServiceServer(
		s.config.Server.Token,
		s.config.Server.PublicURL,
		s.config.Server.BindPort,
	)
	s.clientService = grpcserver.NewClientServiceServer(s.config)
	s.clientService.SetAgentService(s.agentService) // 设置AgentService用于检查状态

	s.grpcServer = grpc.NewServer()
	pb.RegisterAgentServiceServer(s.grpcServer, s.agentService)
	pb.RegisterClientServiceServer(s.grpcServer, s.clientService)

	// 创建FRP Server（在设置路由之前，确保健康检查可以访问）
	var err error
	s.frpServer, err = frp.NewFRPServer(s.config)
	if err != nil {
		return fmt.Errorf("创建FRP Server失败: %w", err)
	}

	// 创建Gin路由（HTTP处理）
	ginRouter := s.setupRouter()

	// 创建统一处理器（HTTP/2: 同时支持HTTP和gRPC）
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 根据Content-Type区分请求类型
		if r.ProtoMajor == 2 && strings.HasPrefix(
			r.Header.Get("Content-Type"), "application/grpc") {
			// gRPC请求
			s.grpcServer.ServeHTTP(w, r)
		} else {
			// HTTP请求
			ginRouter.ServeHTTP(w, r)
		}
	})

	// 创建HTTP/2服务器
	// 使用h2c（HTTP/2 Cleartext）支持非TLS的HTTP/2
	h2s := &http2.Server{}

	// 构建监听地址
	listenAddr := s.config.Web.ListenAddr
	if listenAddr == "" || listenAddr == "0.0.0.0" {
		// 明确使用 0.0.0.0 以确保监听IPv4
		listenAddr = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%d", listenAddr, s.config.Web.ListenPort)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: h2c.NewHandler(handler, h2s),
	}

	// 启动统一服务器（HTTP + gRPC）
	go func() {
		log.Printf("Server启动在: http://%s", addr)
		log.Printf("  - Web管理界面: http://%s/", addr)
		log.Printf("  - RESTful API: http://%s/api/...", addr)
		log.Printf("  - gRPC服务: %s (HTTP/2)", addr)

		// 明确使用tcp4网络以确保监听IPv4
		listener, err := net.Listen("tcp4", addr)
		if err != nil {
			log.Fatalf("创建监听器失败: %v", err)
		}
		log.Printf("监听器创建成功: %s (IPv4)", listener.Addr())

		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务器启动失败: %v", err)
		}
	}()

	// 启动FRP Server（在goroutine中运行）
	go func() {
		if err := s.frpServer.Run(); err != nil {
			log.Printf("FRP Server运行错误: %v", err)
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("正在关闭服务器...")

	// 优雅关闭
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 停止gRPC服务器
	s.grpcServer.GracefulStop()

	// 停止FRP Server
	if s.frpServer != nil {
		if err := s.frpServer.Stop(); err != nil {
			log.Printf("停止FRP Server失败: %v", err)
		}
	}

	// 停止HTTP服务器
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("服务器关闭失败: %w", err)
	}

	log.Println("服务器已关闭")
	return nil
}

// customLogger 自定义日志中间件，health接口只在状态变化时打印
func (s *Server) customLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		// health接口只在状态变化时打印
		if path == "/health" || path == "/health/ready" {
			if c.Writer.Header().Get("X-Log-Status-Change") == "true" {
				log.Printf("[%s] %s %d %v",
					c.Request.Method,
					path,
					c.Writer.Status(),
					time.Since(start))
			}
			return
		}

		// 其他接口正常打印
		log.Printf("[%s] %s %d %v",
			c.Request.Method,
			path,
			c.Writer.Status(),
			time.Since(start))
	}
}

func (s *Server) setupRouter() *gin.Engine {
	// 使用自定义日志中间件
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(s.customLogger())

	// 健康检查接口
	healthAPI := api.NewHealthAPI(s.agentService, s.frpServer)
	router.GET("/health", healthAPI.Health)
	router.GET("/health/ready", healthAPI.Ready)

	// 静态文件服务（前端）
	router.Static("/assets", "./web/dist/assets")
	router.StaticFile("/favicon.ico", "./web/dist/favicon.ico")

	// API路由组
	apiGroup := router.Group("/api")
	{
		// 保留旧版API用于向后兼容（已废弃）
		clientAuthAPI := api.NewClientAuthAPI(s.config)
		apiGroup.POST("/client/auth", clientAuthAPI.Auth) // 已废弃，使用 /api/v1/client/auth/login

		// v1 API
		v1Group := apiGroup.Group("/v1")
		{
			// ==================== 公开API（不需要认证）====================
			v1Group.GET("/public/system/config", api.GetPublicSystemConfig)

			// 桌面客户端下载API
			downloadAPI := api.NewDownloadAPI()
			v1Group.GET("/public/download/desktop", downloadAPI.GetDesktopDownload)              // 获取下载信息（JSON）
			v1Group.GET("/public/download/desktop/direct", downloadAPI.GetDesktopDownloadDirect) // 直接重定向下载
			v1Group.GET("/public/download/desktop/versions", downloadAPI.ListDesktopVersions)    // 列出所有版本

			// ==================== 管理员API ====================
			adminGroup := v1Group.Group("/admin")
			{
				// 管理员认证（不需要JWT认证）
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
					adminAuthGroup.POST("/agents", agentAPI.Create)
					adminAuthGroup.DELETE("/agents/:id", agentAPI.Delete)
					adminAuthGroup.POST("/agents/:id/regenerate-token", agentAPI.RegenerateToken)

					// Client管理
					clientAPI := api.NewClientAPI()
					adminAuthGroup.GET("/clients", clientAPI.List)
					adminAuthGroup.POST("/clients", clientAPI.Create)
					adminAuthGroup.PUT("/clients/:id/disable", clientAPI.Disable)
					adminAuthGroup.PUT("/clients/:id/enable", clientAPI.Enable)
					adminAuthGroup.DELETE("/clients/:id", clientAPI.Delete)
					adminAuthGroup.POST("/clients/:id/regenerate-secret", clientAPI.RegenerateSecret)

					// STCP实例管理
					stcpAPI := api.NewSTCPAPI()
					stcpAPI.SetAgentService(s.agentService) // 注入AgentService
					adminAuthGroup.GET("/stcp-instances", stcpAPI.List)
					adminAuthGroup.POST("/stcp-instances", stcpAPI.Create)
					adminAuthGroup.DELETE("/stcp-instances/:id", stcpAPI.Delete)
					adminAuthGroup.GET("/stcp-instances/:id/accesses", stcpAPI.ListAccesses)
					adminAuthGroup.POST("/stcp-instances/:id/grant", stcpAPI.GrantAccess)
					adminAuthGroup.POST("/stcp-instances/:id/revoke", stcpAPI.RevokeAccess)
					adminAuthGroup.PUT("/stcp-instances/:id/access-type", stcpAPI.SetAccessType)

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
				// Client认证（不需要JWT认证）
				deviceTokenAPI := api.NewDeviceTokenAPI(s.config)
				clientGroup.POST("/auth/login", deviceTokenAPI.LoginWithSecret)
				clientGroup.POST("/auth/login/token", deviceTokenAPI.LoginWithToken)

				// 需要Client JWT认证的路由
				clientAuthGroup := clientGroup.Group("")
				clientAuthGroup.Use(api.ClientAuthMiddleware(s.config.Security.JWTSecret))
				{
					// 服务列表
					clientAuthGroup.GET("/services", clientAuthAPI.GetServices)

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

	// SPA路由支持（必须在所有路由之后）
	router.NoRoute(func(c *gin.Context) {
		// 如果是API请求，返回404
		if len(c.Request.URL.Path) >= 4 && c.Request.URL.Path[:4] == "/api" {
			c.JSON(404, gin.H{"error": "Not Found"})
			return
		}
		// 否则返回index.html（SPA）
		c.File("./web/dist/index.html")
	})

	return router
}
