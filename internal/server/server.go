package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/api"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/frp"
)

type Server struct {
	config     *config.ServerConfig
	httpServer *http.Server
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

	// 创建路由
	router := s.setupRouter()

	// 创建HTTP服务器
	addr := fmt.Sprintf("%s:%d", s.config.Web.ListenAddr, s.config.Web.ListenPort)
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: router,
	}

	// 启动Web服务器
	go func() {
		log.Printf("Web管理界面启动在: http://%s", addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP服务器启动失败: %v", err)
		}
	}()

	// 启动FRP Server
	frpServer, err := frp.NewFRPServer(s.config)
	if err != nil {
		return fmt.Errorf("创建FRP Server失败: %w", err)
	}

	go func() {
		if err := frpServer.Run(); err != nil {
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

	// 停止FRP Server
	if err := frpServer.Stop(); err != nil {
		log.Printf("停止FRP Server失败: %v", err)
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

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

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
			authGroup.GET("/stcp-instances", stcpAPI.List)
			authGroup.POST("/stcp-instances", stcpAPI.Create)
			authGroup.DELETE("/stcp-instances/:id", stcpAPI.Delete)
			authGroup.POST("/stcp-instances/:id/grant-access", stcpAPI.GrantAccess)
			authGroup.DELETE("/stcp-instances/:id/revoke-access", stcpAPI.RevokeAccess)
		}

		// Client端API（不需要管理员认证）
		clientAuthAPI := api.NewClientAuthAPI(s.config)
		apiGroup.POST("/client/auth", clientAuthAPI.Auth)
		apiGroup.GET("/client/services", clientAuthAPI.GetServices)
	}

	return router
}
