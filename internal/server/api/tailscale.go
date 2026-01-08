package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// TailscaleAPI Tailscale 管理 API
type TailscaleAPI struct {
	headscaleClient *headscale.Client
	config          *config.ServerConfig
}

// NewTailscaleAPI 创建 TailscaleAPI
func NewTailscaleAPI(cfg *config.ServerConfig) *TailscaleAPI {
	api := &TailscaleAPI{
		config: cfg,
	}

	// 初始化 Headscale 客户端
	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		api.headscaleClient = headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
	}

	return api
}

// TailscaleResponse API 响应
type TailscaleResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// TailscaleStatus Tailscale 状态
type TailscaleStatus struct {
	HeadscaleURL     string `json:"headscale_url"`
	HeadscaleOnline  bool   `json:"headscale_online"`
	User             string `json:"user"`
	TotalNodes       int    `json:"total_nodes"`
	OnlineNodes      int    `json:"online_nodes"`
	AgentsConnected  int    `json:"agents_connected"`
	ClientsConnected int    `json:"clients_connected"`
}

// Status 获取 Tailscale 状态
func (a *TailscaleAPI) Status(c *gin.Context) {
	if a.headscaleClient == nil {
		c.JSON(http.StatusServiceUnavailable, TailscaleResponse{
			Success: false,
			Message: "Headscale 未配置",
		})
		return
	}

	status := TailscaleStatus{
		HeadscaleURL: a.config.Tailscale.HeadscaleURL,
		User:         a.config.Tailscale.User,
	}

	// 获取 Headscale 节点列表
	nodes, err := a.headscaleClient.ListNodes(c.Request.Context())
	if err != nil {
		logger.Warnf("获取 Headscale 节点列表失败: %v", err)
		status.HeadscaleOnline = false
	} else {
		status.HeadscaleOnline = true
		status.TotalNodes = len(nodes)
		for _, node := range nodes {
			if node.Online {
				status.OnlineNodes++
			}
		}
	}

	// 统计已连接的 Agent 数量
	var agentCount int64
	db.DB.Model(&model.Agent{}).Where("ts_connected = ?", true).Count(&agentCount)
	status.AgentsConnected = int(agentCount)

	// 统计已连接的 Client 数量
	var clientCount int64
	db.DB.Model(&model.Client{}).Where("tailscale_ip != ''").Count(&clientCount)
	status.ClientsConnected = int(clientCount)

	c.JSON(http.StatusOK, TailscaleResponse{
		Success: true,
		Data:    status,
	})
}

// Sync 同步 Headscale 状态到数据库
func (a *TailscaleAPI) Sync(c *gin.Context) {
	if a.headscaleClient == nil {
		c.JSON(http.StatusServiceUnavailable, TailscaleResponse{
			Success: false,
			Message: "Headscale 未配置",
		})
		return
	}

	// 获取 Headscale 节点列表
	nodes, err := a.headscaleClient.ListNodes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, TailscaleResponse{
			Success: false,
			Message: "获取 Headscale 节点列表失败: " + err.Error(),
		})
		return
	}

	// 同步 Agent 状态
	syncedAgents := 0
	for _, node := range nodes {
		if len(node.IPAddresses) == 0 {
			continue
		}

		ip := node.IPAddresses[0]
		var agent model.Agent
		if err := db.DB.Where("tailscale_ip = ?", ip).First(&agent).Error; err == nil {
			agent.TsConnected = node.Online
			if err := db.DB.Save(&agent).Error; err == nil {
				syncedAgents++
			}
		}
	}

	logger.Infof("Tailscale 状态同步完成: 同步了 %d 个 Agent", syncedAgents)

	c.JSON(http.StatusOK, TailscaleResponse{
		Success: true,
		Message: "同步完成",
		Data: map[string]interface{}{
			"total_nodes":   len(nodes),
			"synced_agents": syncedAgents,
		},
	})
}

// Nodes 获取 Headscale 节点列表
func (a *TailscaleAPI) Nodes(c *gin.Context) {
	if a.headscaleClient == nil {
		c.JSON(http.StatusServiceUnavailable, TailscaleResponse{
			Success: false,
			Message: "Headscale 未配置",
		})
		return
	}

	nodes, err := a.headscaleClient.ListNodes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, TailscaleResponse{
			Success: false,
			Message: "获取节点列表失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, TailscaleResponse{
		Success: true,
		Data:    nodes,
	})
}
