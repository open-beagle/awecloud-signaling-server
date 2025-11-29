package api

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/frp"
	grpcserver "github.com/open-beagle/awecloud-signaling-server/internal/server/grpc"
)

// HealthAPI 健康检查API
type HealthAPI struct {
	startTime    time.Time
	agentService *grpcserver.AgentServiceServer
	frpServer    *frp.FRPServer

	// 状态缓存，用于检测变化
	lastHealthStatus string
	lastReadyStatus  string
}

// NewHealthAPI 创建健康检查API实例
func NewHealthAPI(agentService *grpcserver.AgentServiceServer, frpServer *frp.FRPServer) *HealthAPI {
	return &HealthAPI{
		startTime:        time.Now(),
		agentService:     agentService,
		frpServer:        frpServer,
		lastHealthStatus: "",
		lastReadyStatus:  "",
	}
}

// Health 基础健康检查
// @Summary 基础健康检查
// @Description 用于Kubernetes Liveness Probe，检查进程是否存活
// @Tags Health
// @Produce json
// @Success 200 {object} map[string]interface{} "服务正常运行"
// @Failure 503 {object} map[string]interface{} "服务不可用"
// @Router /health [get]
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
// @Summary 就绪性检查
// @Description 用于Kubernetes Readiness Probe，检查服务是否准备好接收流量
// @Tags Health
// @Produce json
// @Success 200 {object} map[string]interface{} "服务就绪"
// @Failure 503 {object} map[string]interface{} "服务未就绪"
// @Router /health/ready [get]
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

	// 检查FRP Server状态
	if h.frpServer != nil && h.frpServer.IsRunning() {
		checks["frp_server"] = "ok"
	} else {
		checks["frp_server"] = "error"
		errors["frp_server"] = "not running"
		allReady = false
	}

	// 检查gRPC Server状态（如果AgentService存在则认为gRPC正常）
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
