package agent

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
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
func (h *HealthAPI) Health(c *gin.Context) {
	c.JSON(200, gin.H{
		"status":    "ok",
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

// Ready 就绪性检查
func (h *HealthAPI) Ready(c *gin.Context) {
	checks := make(map[string]string)
	errors := make(map[string]string)
	allReady := true

	// 检查 gRPC 连接
	grpcConnected := h.agent.IsGRPCConnected()
	if grpcConnected {
		checks["grpc_connection"] = "ok"
	} else {
		checks["grpc_connection"] = "error"
		errors["grpc_connection"] = "not connected"
		allReady = false
		logger.Infof("[HEALTH] gRPC connection check failed")
	}

	// 检查 Tailscale 连接
	tailscaleConnected := h.agent.IsTailscaleConnected()
	if tailscaleConnected {
		checks["tailscale_connection"] = "ok"
	} else {
		checks["tailscale_connection"] = "initializing"
		// 不设置 allReady = false，因为 Tailscale 会自动重连
	}

	status := "ready"
	statusCode := 200
	if !allReady {
		status = "not_ready"
		statusCode = 503
	}

	response := gin.H{
		"status":              status,
		"timestamp":           time.Now().Format(time.RFC3339),
		"checks":              checks,
		"grpc_connected":      grpcConnected,
		"tailscale_connected": tailscaleConnected,
		"tailscale_ip":        h.agent.GetTailscaleIP(),
		"proxy_count":         h.agent.GetProxyCount(),
		"visitor_count":       h.agent.GetVisitorCount(),
		"visitor_lan_ip":      h.agent.GetVisitorLANIP(),
	}

	if len(errors) > 0 {
		response["errors"] = errors
	}

	c.JSON(statusCode, response)
}
