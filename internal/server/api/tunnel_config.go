package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
)

type TunnelConfigAPI struct {
	config *config.ServerConfig
}

func NewTunnelConfigAPI(cfg *config.ServerConfig) *TunnelConfigAPI {
	return &TunnelConfigAPI{config: cfg}
}

type TunnelConfigResponse struct {
	Success      bool   `json:"success"`
	TunnelServer string `json:"tunnel_server,omitempty"` // 隧道服务器地址（完整URL或空）
	TunnelPort   int    `json:"tunnel_port,omitempty"`   // 隧道服务器端口
	TunnelToken  string `json:"tunnel_token,omitempty"`  // 隧道认证Token
	Message      string `json:"message,omitempty"`
}

// GetTunnelConfig 获取隧道配置
// 此API需要Client认证（Bearer Token）
func (a *TunnelConfigAPI) GetTunnelConfig(c *gin.Context) {
	// 认证已由中间件完成，这里直接返回配置

	// 构建隧道配置响应
	tunnelServer := ""
	tunnelPort := a.config.Server.BindPort

	// 如果配置了公网URL，使用公网URL
	if a.config.Server.PublicURL != "" {
		tunnelServer = a.config.Server.PublicURL
		tunnelPort = 0 // 使用完整URL时，端口信息已包含在URL中
	}

	c.JSON(http.StatusOK, TunnelConfigResponse{
		Success:      true,
		TunnelServer: tunnelServer,
		TunnelPort:   tunnelPort,
		TunnelToken:  a.config.Server.Token,
	})
}
