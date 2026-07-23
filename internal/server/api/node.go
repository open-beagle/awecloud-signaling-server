package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	grpcserver "github.com/open-beagle/awecloud-signaling-server/internal/server/grpc"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

// NodeAPI 设备管理 API
type NodeAPI struct {
	config        *config.ServerConfig
	hsClient      *headscale.Client
	domainService *service.DomainService
	agentService  *grpcserver.AgentServiceServer
}

// NewNodeAPI 创建 NodeAPI
func NewNodeAPI(cfg *config.ServerConfig) *NodeAPI {
	api := &NodeAPI{
		config:        cfg,
		domainService: service.NewDomainService(db.DB),
	}

	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		client, err := headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
		if err != nil {
			logger.Warnf("初始化 Headscale 客户端失败: %v", err)
		} else {
			api.hsClient = client
		}
	}

	return api
}

// SetAgentService 设置 AgentService，用于删除设备时清理在线连接
func (a *NodeAPI) SetAgentService(service *grpcserver.AgentServiceServer) {
	a.agentService = service
}

// NodeUserInfo 设备关联的用户信息
type NodeUserInfo struct {
	ID    uint64 `json:"id"`
	Name  string `json:"name"`
	Alias string `json:"alias,omitempty"`
	Role  string `json:"role"`
}

// NodeListItem 设备列表项
type NodeListItem struct {
	ID            uint64        `json:"id"`
	Name          string        `json:"name"`
	Type          string        `json:"type"`
	UserID        uint64        `json:"user_id"`
	User          *NodeUserInfo `json:"user,omitempty"`
	IP            string        `json:"ip"`
	Version       string        `json:"version"`
	Hostname      string        `json:"hostname"`
	Status        string        `json:"status"`
	LastHeartbeat *time.Time    `json:"last_heartbeat"`
	CreatedAt     time.Time     `json:"created_at"`
}

// List 获取设备列表
func (a *NodeAPI) List(c *gin.Context) {
	ctx := c.Request.Context()
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	search := c.Query("search")
	nodeType := c.Query("type")  // 筛选类型：agent / desktop
	userID := c.Query("user_id") // 筛选用户

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	query := db.DB.WithContext(ctx).Model(&model.Node{}).Preload("User")
	if search != "" {
		query = query.Where("name LIKE ? OR hostname LIKE ?", "%"+search+"%", "%"+search+"%")
	}
	if nodeType != "" {
		query = query.Where("type = ?", nodeType)
	}
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}

	var total int64
	query.Count(&total)

	var nodes []model.Node
	offset := (page - 1) * size
	if err := query.Order("created_at DESC").Offset(offset).Limit(size).Find(&nodes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	now := time.Now()
	result := make([]NodeListItem, len(nodes))
	for i, node := range nodes {
		status := "offline"
		if node.LastHeartbeat != nil && now.Sub(*node.LastHeartbeat) < 60*time.Second {
			status = "online"
		}

		var userInfo *NodeUserInfo
		if node.User != nil {
			userInfo = &NodeUserInfo{
				ID:    node.User.ID,
				Name:  node.User.Name,
				Alias: node.User.Alias,
				Role:  string(node.User.Role),
			}
		}

		result[i] = NodeListItem{
			ID:            node.ID,
			Name:          node.Name,
			Type:          string(node.Type),
			UserID:        node.UserID,
			User:          userInfo,
			IP:            node.IP,
			Version:       node.Version,
			Hostname:      node.Hostname,
			Status:        status,
			LastHeartbeat: node.LastHeartbeat,
			CreatedAt:     node.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// HeadscaleNodeInfo Headscale 节点信息
type HeadscaleNodeInfo struct {
	ID          uint64   `json:"id"`
	Name        string   `json:"name"`
	GivenName   string   `json:"given_name"`
	IPAddresses []string `json:"ip_addresses"`
	Online      bool     `json:"online"`
	LastSeen    string   `json:"last_seen,omitempty"`
	Expiry      string   `json:"expiry,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	ForcedTags  []string `json:"forced_tags,omitempty"`
	UserName    string   `json:"user_name,omitempty"`
}

// NodeDetail 设备详情
type NodeDetail struct {
	ID              uint64             `json:"id"`
	Name            string             `json:"name"`
	Type            string             `json:"type"`
	UserID          uint64             `json:"user_id"`
	User            *NodeUserInfo      `json:"user,omitempty"`
	HeadscaleNodeID uint64             `json:"headscale_node_id"`
	IP              string             `json:"ip"`
	Version         string             `json:"version"`
	Hostname        string             `json:"hostname"`
	SystemInfo      string             `json:"system_info"`
	Status          string             `json:"status"`
	LastHeartbeat   *time.Time         `json:"last_heartbeat"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
	Headscale       *HeadscaleNodeInfo `json:"headscale,omitempty"`
}

// Get 获取设备详情
func (a *NodeAPI) Get(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var node model.Node
	if err := db.DB.WithContext(ctx).Preload("User").First(&node, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("设备不存在"))
		return
	}

	now := time.Now()
	status := "offline"
	if node.LastHeartbeat != nil && now.Sub(*node.LastHeartbeat) < 60*time.Second {
		status = "online"
	}

	var userInfo *NodeUserInfo
	if node.User != nil {
		userInfo = &NodeUserInfo{
			ID:    node.User.ID,
			Name:  node.User.Name,
			Alias: node.User.Alias,
			Role:  string(node.User.Role),
		}
	}

	result := NodeDetail{
		ID:              node.ID,
		Name:            node.Name,
		Type:            string(node.Type),
		UserID:          node.UserID,
		User:            userInfo,
		HeadscaleNodeID: node.HeadscaleNodeID,
		IP:              node.IP,
		Version:         node.Version,
		Hostname:        node.Hostname,
		SystemInfo:      node.SystemInfo,
		Status:          status,
		LastHeartbeat:   node.LastHeartbeat,
		CreatedAt:       node.CreatedAt,
		UpdatedAt:       node.UpdatedAt,
	}

	// 获取 Headscale 节点信息
	if a.hsClient != nil && node.HeadscaleNodeID > 0 {
		hsCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if hsNode, err := a.hsClient.GetNode(hsCtx, node.HeadscaleNodeID); err == nil && hsNode != nil {
			hsInfo := &HeadscaleNodeInfo{
				ID:          hsNode.Id,
				Name:        hsNode.Name,
				GivenName:   hsNode.GivenName,
				IPAddresses: hsNode.IpAddresses,
				Online:      hsNode.Online,
				ForcedTags:  hsNode.ForcedTags,
			}
			if hsNode.User != nil {
				hsInfo.UserName = hsNode.User.Name
			}
			if hsNode.LastSeen != nil {
				hsInfo.LastSeen = hsNode.LastSeen.AsTime().Format(time.RFC3339)
			}
			if hsNode.Expiry != nil {
				hsInfo.Expiry = hsNode.Expiry.AsTime().Format(time.RFC3339)
			}
			if hsNode.CreatedAt != nil {
				hsInfo.CreatedAt = hsNode.CreatedAt.AsTime().Format(time.RFC3339)
			}
			result.Headscale = hsInfo
		} else if err != nil {
			logger.Warnf("获取 Headscale 节点信息失败: %v", err)
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// Expire 注销设备（使其过期）
func (a *NodeAPI) Expire(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("设备不存在"))
		return
	}

	// 在 Headscale 使节点过期（使用 HeadscaleNodeID）
	if a.hsClient != nil && node.HeadscaleNodeID > 0 {
		hsCtx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		if err := a.hsClient.ExpireNode(hsCtx, node.HeadscaleNodeID); err != nil {
			logger.Warnf("Headscale 注销节点失败: %v", err)
		}
	}

	logger.Infof("注销设备: id=%d, name=%s", id, node.Name)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("注销成功", nil))
}

// CapabilityResponse 能力配置响应
type CapabilityResponse struct {
	SSHEnabled         bool   `json:"ssh_enabled"`          // SSH 开关（User.SSHEnabled，始终有值）
	K8SEnabled         *bool  `json:"k8s_enabled"`          // K8S API 开关（nil=未设置）
	K8SListenPort      *int   `json:"k8s_listen_port"`      // K8S API 监听端口
	K8SApiServer       string `json:"k8s_api_server"`       // K8S API Server 地址
	SVCEnabled         *bool  `json:"svc_enabled"`          // K8S Service 开关
	SVCLabelSelector   string `json:"svc_label_selector"`   // 标签选择器
	SVCNamespaces      string `json:"svc_namespaces"`       // 命名空间列表 JSON
	SVCListenPortBase  *int   `json:"svc_listen_port_base"` // gRPC 监听端口
	EndpointEnabled    *bool  `json:"endpoint_enabled"`     // Endpoint 功能开关
	EndpointListenPort *int   `json:"endpoint_listen_port"` // Endpoint 内网 gRPC 监听端口
	EndpointToken      string `json:"endpoint_token"`       // Endpoint 注册令牌
}

// CapabilityUpdateRequest 能力配置更新请求
type CapabilityUpdateRequest struct {
	SSHEnabled         *bool   `json:"ssh_enabled"`
	K8SEnabled         *bool   `json:"k8s_enabled"`
	K8SListenPort      *int    `json:"k8s_listen_port"`
	K8SApiServer       *string `json:"k8s_api_server"`
	SVCEnabled         *bool   `json:"svc_enabled"`
	SVCLabelSelector   *string `json:"svc_label_selector"`
	SVCNamespaces      *string `json:"svc_namespaces"`
	SVCListenPortBase  *int    `json:"svc_listen_port_base"`
	EndpointEnabled    *bool   `json:"endpoint_enabled"`
	EndpointListenPort *int    `json:"endpoint_listen_port"`
}

// GetCapabilities 获取 Agent 能力配置
func (a *NodeAPI) GetCapabilities(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var node model.Node
	if err := db.DB.WithContext(ctx).Preload("User").First(&node, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("设备不存在"))
		return
	}

	// 只有 Agent 类型设备支持能力配置
	if node.Type != model.NodeTypeAgent {
		c.JSON(http.StatusBadRequest, NewErrorResponse("仅 Agent 类型设备支持能力配置"))
		return
	}

	// SSH 从 User 表读取（User 级别共享），K8S/SVC 从 Node 表读取（Node 级别独立）
	sshEnabled := false
	if node.User != nil {
		sshEnabled = node.User.SSHEnabled
	}

	resp := CapabilityResponse{
		SSHEnabled:         sshEnabled,
		K8SEnabled:         node.K8SEnabled,
		K8SListenPort:      node.K8SListenPort,
		K8SApiServer:       node.K8SApiServer,
		SVCEnabled:         node.SVCEnabled,
		SVCLabelSelector:   node.SVCLabelSelector,
		SVCNamespaces:      node.SVCNamespaces,
		SVCListenPortBase:  node.SVCListenPortBase,
		EndpointEnabled:    node.EndpointEnabled,
		EndpointListenPort: node.EndpointListenPort,
		EndpointToken:      node.EndpointToken,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(resp))
}

// UpdateCapabilities 更新 Agent 能力配置
func (a *NodeAPI) UpdateCapabilities(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var req CapabilityUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("设备不存在"))
		return
	}

	if node.Type != model.NodeTypeAgent {
		c.JSON(http.StatusBadRequest, NewErrorResponse("仅 Agent 类型设备支持能力配置"))
		return
	}

	// SSH 更新到 User 表（User 级别共享）
	if req.SSHEnabled != nil {
		if err := db.DB.WithContext(ctx).Model(&model.User{}).Where("id = ?", node.UserID).
			Update("ssh_enabled", *req.SSHEnabled).Error; err != nil {
			c.JSON(http.StatusInternalServerError, NewErrorResponse("更新 SSH 配置失败"))
			return
		}
	}

	// K8S/SVC 更新到 Node 表（Node 级别独立）
	updates := map[string]interface{}{}
	if req.K8SEnabled != nil {
		updates["k8s_enabled"] = *req.K8SEnabled
	}
	if req.K8SListenPort != nil {
		updates["k8s_listen_port"] = *req.K8SListenPort
	}
	if req.K8SApiServer != nil {
		updates["k8s_api_server"] = *req.K8SApiServer
	}
	if req.SVCEnabled != nil {
		updates["svc_enabled"] = *req.SVCEnabled
	}
	if req.SVCLabelSelector != nil {
		updates["svc_label_selector"] = *req.SVCLabelSelector
	}
	if req.SVCNamespaces != nil {
		updates["svc_namespaces"] = *req.SVCNamespaces
	}
	if req.SVCListenPortBase != nil {
		updates["svc_listen_port_base"] = *req.SVCListenPortBase
	}
	if req.EndpointEnabled != nil {
		updates["endpoint_enabled"] = *req.EndpointEnabled
		// 首次开启 Endpoint 时自动生成 token
		if *req.EndpointEnabled && node.EndpointToken == "" {
			token, err := generateEndpointToken()
			if err != nil {
				c.JSON(http.StatusInternalServerError, NewErrorResponse("生成 Endpoint Token 失败"))
				return
			}
			updates["endpoint_token"] = token
		}
	}
	if req.EndpointListenPort != nil {
		updates["endpoint_listen_port"] = *req.EndpointListenPort
	}

	if len(updates) > 0 {
		if err := db.DB.WithContext(ctx).Model(&node).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
			return
		}
	}

	if len(updates) == 0 && req.SSHEnabled == nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("没有需要更新的字段"))
		return
	}

	logger.Infof("更新 Agent 能力配置: node_id=%d, updates=%v, ssh=%v", id, updates, req.SSHEnabled)

	// 查询 User 信息（用于域名生成）
	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, node.UserID).Error; err != nil {
		logger.Warnf("查询 User 失败，跳过域名处理: user_id=%d, err=%v", node.UserID, err)
	} else {
		// 重新加载 Node 数据（获取最新的能力配置）
		if err := db.DB.WithContext(ctx).First(&node, id).Error; err != nil {
			logger.Warnf("重新加载 Node 失败，跳过域名处理: node_id=%d, err=%v", id, err)
		} else {
			// 处理 SSH 域名
			if req.SSHEnabled != nil {
				if *req.SSHEnabled {
					// SSH 开启 → 创建域名
					if err := a.domainService.CreateNodeSSHDomain(ctx, &node, &user); err != nil {
						logger.Errorf("创建 Node SSH 域名失败: node_id=%d, err=%v", id, err)
					}
				} else {
					// SSH 关闭 → 删除域名
					if err := a.domainService.DeleteNodeSSHDomain(ctx, &node, &user); err != nil {
						logger.Errorf("删除 Node SSH 域名失败: node_id=%d, err=%v", id, err)
					}
				}
			}

			// 处理 K8S API 域名
			if req.K8SEnabled != nil {
				if *req.K8SEnabled {
					// K8S API 开启 → 创建域名
					if err := a.domainService.CreateNodeK8SAPIDomain(ctx, &node, &user); err != nil {
						logger.Errorf("创建 Node K8S API 域名失败: node_id=%d, err=%v", id, err)
					}
				} else {
					// K8S API 关闭 → 删除域名
					if err := a.domainService.DeleteNodeK8SAPIDomain(ctx, &node, &user); err != nil {
						logger.Errorf("删除 Node K8S API 域名失败: node_id=%d, err=%v", id, err)
					}
				}
			}
		}
	}

	// 记录审计日志
	recordAuditLog(ctx, c, "update_capability", "node", strconv.FormatUint(id, 10), node.Name, updates)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// ResetCapabilities 重置 Agent 能力配置（清除所有 Server 远程配置）
func (a *NodeAPI) ResetCapabilities(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("设备不存在"))
		return
	}

	if node.Type != model.NodeTypeAgent {
		c.JSON(http.StatusBadRequest, NewErrorResponse("仅 Agent 类型设备支持能力配置"))
		return
	}

	// 清除 Node 上的所有远程能力配置（K8S/SVC/Endpoint 字段设为 nil/空）
	updates := map[string]any{
		"k8s_enabled":          nil,
		"k8s_listen_port":      nil,
		"k8s_api_server":       "",
		"svc_enabled":          nil,
		"svc_label_selector":   "",
		"svc_namespaces":       "",
		"svc_listen_port_base": nil,
		"endpoint_enabled":     nil,
		"endpoint_listen_port": nil,
		"endpoint_token":       "",
	}

	if err := db.DB.WithContext(ctx).Model(&node).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("重置失败"))
		return
	}

	logger.Infof("重置 Agent 能力配置: node_id=%d", id)

	recordAuditLog(ctx, c, "reset_capability", "node", strconv.FormatUint(id, 10), node.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("重置成功", nil))
}

// Delete 删除设备
func (a *NodeAPI) Delete(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("设备不存在"))
		return
	}

	if node.Type == model.NodeTypeAgent {
		if a.agentService != nil {
			a.agentService.DisconnectNode(node.ID)
		}
		cache.DeleteNodeStatus(node.ID)
		cache.ClearK8SServiceDiscovery(node.UserID)
	}

	// 删除该 Node 的所有域名
	if err := a.domainService.DeleteNodeAllDomains(ctx, id); err != nil {
		logger.Errorf("删除 Node 域名失败: node_id=%d, err=%v", id, err)
		// 继续删除 Node，不因域名删除失败而中断
	}

	// 在 Headscale 删除节点（使用 HeadscaleNodeID）
	if a.hsClient != nil && node.HeadscaleNodeID > 0 {
		hsCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := a.hsClient.DeleteNode(hsCtx, node.HeadscaleNodeID); err != nil {
			logger.Warnf("Headscale 删除节点失败: %v", err)
		}
	}

	// 删除设备
	if err := db.DB.WithContext(ctx).Delete(&node).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}

	logger.Infof("删除设备: id=%d, name=%s", id, node.Name)

	actionType := "delete_node"
	if node.Type == model.NodeTypeDesktop {
		actionType = model.ActionDeleteDesktop
	}
	recordAuditLog(ctx, c, actionType, "node", strconv.FormatUint(id, 10), node.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}

// generateEndpointToken 生成 Endpoint 注册令牌（ep_ 前缀 + 32 字节随机 hex）
func generateEndpointToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "ep_" + hex.EncodeToString(b), nil
}

// RegenerateEndpointToken 重新生成 Endpoint Token
func (a *NodeAPI) RegenerateEndpointToken(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("设备不存在"))
		return
	}

	if node.Type != model.NodeTypeAgent {
		c.JSON(http.StatusBadRequest, NewErrorResponse("仅 Agent 类型设备支持此操作"))
		return
	}

	token, err := generateEndpointToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("生成 Token 失败"))
		return
	}

	if err := db.DB.WithContext(ctx).Model(&node).Update("endpoint_token", token).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新 Token 失败"))
		return
	}

	logger.Infof("重新生成 Endpoint Token: node_id=%d", id)
	recordAuditLog(ctx, c, "regenerate_endpoint_token", "node", strconv.FormatUint(id, 10), node.Name, nil)

	c.JSON(http.StatusOK, NewSuccessResponse(map[string]string{"endpoint_token": token}))
}
