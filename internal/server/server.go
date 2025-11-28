package server

import (
	"context"
	"fmt"
	"log"
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
		s.config.Server.FRPAuthToken,
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
	addr := fmt.Sprintf("%s:%d", s.config.Web.ListenAddr, s.config.Web.ListenPort)
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
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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

func (s *Server) setupRouter() *gin.Engine {
	router := gin.Default()

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
		// 管理员认证
		adminAPI := api.NewAdminAPI(s.config)
		apiGroup.POST("/admin/login", adminAPI.Login)
		apiGroup.POST("/admin/logout", adminAPI.Logout)

		// 需要认证的路由
		authGroup := apiGroup.Group("")
		authGroup.Use(api.AuthMiddleware(s.config.Security.JWTSecret))
		{
			// Agent管理
			agentAPI := api.NewAgentAPI()
			authGroup.GET("/agents", agentAPI.List)
			authGroup.POST("/agents", agentAPI.Create)
			authGroup.DELETE("/agents/:id", agentAPI.Delete)
			authGroup.POST("/agents/:id/regenerate-token", agentAPI.RegenerateToken)

			// Client管理
			clientAPI := api.NewClientAPI()
			authGroup.GET("/clients", clientAPI.List)
			authGroup.POST("/clients", clientAPI.Create)
			authGroup.PUT("/clients/:id/disable", clientAPI.Disable)
			authGroup.PUT("/clients/:id/enable", clientAPI.Enable)
			authGroup.DELETE("/clients/:id", clientAPI.Delete)
			authGroup.POST("/clients/:id/regenerate-secret", clientAPI.RegenerateSecret)

			// STCP实例管理
			stcpAPI := api.NewSTCPAPI()
			stcpAPI.SetAgentService(s.agentService) // 注入AgentService
			authGroup.GET("/stcp-instances", stcpAPI.List)
			authGroup.POST("/stcp-instances", stcpAPI.Create)
			authGroup.DELETE("/stcp-instances/:id", stcpAPI.Delete)
			authGroup.GET("/stcp-instances/:id/accesses", stcpAPI.ListAccesses)
			authGroup.POST("/stcp-instances/:id/grant", stcpAPI.GrantAccess)
			authGroup.POST("/stcp-instances/:id/revoke", stcpAPI.RevokeAccess)
			authGroup.PUT("/stcp-instances/:id/access-type", stcpAPI.SetAccessType)

			// 组管理
			groupAPI := api.NewGroupAPI()
			authGroup.GET("/groups", groupAPI.GetGroups)
			authGroup.POST("/groups", groupAPI.CreateGroup)
			authGroup.PUT("/groups/:id", groupAPI.UpdateGroup)
			authGroup.DELETE("/groups/:id", groupAPI.DeleteGroup)
			authGroup.GET("/groups/:id/members", groupAPI.GetGroupMembers)
			authGroup.POST("/groups/:id/members", groupAPI.AddGroupMember)
			authGroup.DELETE("/groups/:id/members/:client_id", groupAPI.RemoveGroupMember)
		}

		// Client端API（不需要管理员认证）
		clientAuthAPI := api.NewClientAuthAPI(s.config)
		apiGroup.POST("/client/auth", clientAuthAPI.Auth)
		apiGroup.GET("/client/services", clientAuthAPI.GetServices)

		// Device Token API（v1）
		v1Group := apiGroup.Group("/v1")
		{
			// Device Token认证API（不需要JWT认证）
			deviceTokenAPI := api.NewDeviceTokenAPI(s.config)
			v1Group.POST("/client/auth/login", deviceTokenAPI.LoginWithSecret)
			v1Group.POST("/client/auth/login/token", deviceTokenAPI.LoginWithToken)

			// Device Token管理API（需要JWT认证）
			deviceAuthGroup := v1Group.Group("/client/auth/login")
			deviceAuthGroup.Use(api.AuthMiddleware(s.config.Security.JWTSecret))
			{
				deviceAuthGroup.GET("/devices", deviceTokenAPI.ListDevices)
				deviceAuthGroup.POST("/devices/:device_token/offline", deviceTokenAPI.OfflineDevice)
				deviceAuthGroup.DELETE("/devices/:device_token", deviceTokenAPI.DeleteDevice)
			}

			// 端口偏好API（需要Client JWT认证）
			portPrefAPI := api.NewPortPreferenceAPI()
			auditLogAPI := api.NewAuditLogAPI()

			v1ClientGroup := v1Group.Group("/client")
			v1ClientGroup.Use(api.ClientAuthMiddleware(s.config.Security.JWTSecret))
			{
				v1ClientGroup.GET("/preferences/port", portPrefAPI.GetPortPreferences)
				v1ClientGroup.POST("/preferences/port", portPrefAPI.SavePortPreference)
				// 客户端记录审计日志（需要Client JWT认证）
				v1ClientGroup.POST("/audit/connection", auditLogAPI.RecordConnection)
			}

			// 管理员查询审计日志（需要管理员认证）
			v1AdminGroup := v1Group.Group("/admin")
			v1AdminGroup.Use(api.AuthMiddleware(s.config.Security.JWTSecret))
			{
				v1AdminGroup.GET("/audit/connection", auditLogAPI.QueryAuditLogs)
				v1AdminGroup.GET("/audit/connection/export", auditLogAPI.ExportAuditLogs)
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
