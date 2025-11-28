package agent

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
)

// HealthAPI Agent健康检查API
type HealthAPI struct {
	agent     *Agent
	startTime time.Time
}

// NewHealthAPI 创建健康检查API实例
func NewHealthAPI(agent *Agent) *HealthAPI {
	return &HealthAPI{
		agent:     agent,
		startTime: time.Now(),
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
	c.JSON(200, gin.H{
		"status":    "ok",
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

// Ready 就绪性检查
// @Summary 就绪性检查
// @Description 用于Kubernetes Readiness Probe，检查Agent是否准备好工作
// @Tags Health
// @Produce json
// @Success 200 {object} map[string]interface{} "Agent就绪"
// @Failure 503 {object} map[string]interface{} "Agent未就绪"
// @Router /health/ready [get]
func (h *HealthAPI) Ready(c *gin.Context) {
	checks := make(map[string]string)
	errors := make(map[string]string)
	allReady := true

	// 检查gRPC连接
	if h.agent.IsGRPCConnected() {
		checks["grpc_connection"] = "ok"
	} else {
		checks["grpc_connection"] = "error"
		errors["grpc_connection"] = "not connected"
		allReady = false
		log.Printf("[HEALTH] gRPC connection check failed")
	}

	// 检查FRP连接（仅作为信息，不影响就绪状态）
	// FRP 会自动重连，不需要健康检查干预
	if h.agent.IsFRPConnected() {
		checks["frp_connection"] = "ok"
	} else {
		checks["frp_connection"] = "initializing"
		// 不设置 allReady = false，因为 FRP 会自动重连
	}

	status := "ready"
	statusCode := 200
	if !allReady {
		status = "not_ready"
		statusCode = 503
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
