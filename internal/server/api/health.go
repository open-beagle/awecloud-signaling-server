package api

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	grpcserver "github.com/open-beagle/awecloud-signaling-server/internal/server/grpc"
)

// HealthAPI 健康检查API
type HealthAPI struct {
	startTime    time.Time
	agentService *grpcserver.AgentServiceServer

	// 状态缓存，用于检测变化
	lastHealthStatus string
	lastReadyStatus  string
}

// NewHealthAPI 创建健康检查API实例
func NewHealthAPI(agentService *grpcserver.AgentServiceServer) *HealthAPI {
	return &HealthAPI{
		startTime:        time.Now(),
		agentService:     agentService,
		lastHealthStatus: "",
		lastReadyStatus:  "",
	}
}

// Health 基础健康检查
func (h *HealthAPI) Health(c *gin.Context) {
	status := "ok"

	// 只在状态变化时打印日志
	if h.lastHealthStatus != status {
		c.Writer.Header().Set("X-Log-Status-Change", "true")
		h.lastHealthStatus = status
	}

	c.JSON(200, gin.H{
		"status":    status,
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

// Ready 就绪性检查
func (h *HealthAPI) Ready(c *gin.Context) {
	checks := make(map[string]string)
	errors := make(map[string]string)
	allReady := true

	// 检查数据库连接
	if err := db.Ping(); err != nil {
		checks["database"] = "error"
		errors["database"] = err.Error()
		allReady = false
	} else {
		checks["database"] = "ok"
	}

	// 检查gRPC Server状态
	if h.agentService != nil {
		checks["grpc_server"] = "ok"
	} else {
		checks["grpc_server"] = "error"
		errors["grpc_server"] = "not initialized"
		allReady = false
	}

	status := "ready"
	statusCode := 200
	if !allReady {
		status = "not_ready"
		statusCode = 503
	}

	// 只在状态变化时打印日志
	if h.lastReadyStatus != status {
		c.Writer.Header().Set("X-Log-Status-Change", "true")
		h.lastReadyStatus = status
	}

	response := gin.H{
		"status":    status,
		"timestamp": time.Now().Format(time.RFC3339),
		"checks":    checks,
	}

	if len(errors) > 0 {
		response["errors"] = errors
	}

	c.JSON(statusCode, response)
}
