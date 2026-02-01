package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// NodeAPI 设备管理 API
type NodeAPI struct {
	config   *config.ServerConfig
	hsClient *headscale.Client
}

// NewNodeAPI 创建 NodeAPI
func NewNodeAPI(cfg *config.ServerConfig) *NodeAPI {
	api := &NodeAPI{config: cfg}

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

// NodeDetail 设备详情
type NodeDetail struct {
	ID            uint64        `json:"id"`
	Name          string        `json:"name"`
	Type          string        `json:"type"`
	UserID        uint64        `json:"user_id"`
	User          *NodeUserInfo `json:"user,omitempty"`
	IP            string        `json:"ip"`
	Version       string        `json:"version"`
	Hostname      string        `json:"hostname"`
	SystemInfo    string        `json:"system_info"`
	Status        string        `json:"status"`
	LastHeartbeat *time.Time    `json:"last_heartbeat"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
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
		ID:            node.ID,
		Name:          node.Name,
		Type:          string(node.Type),
		UserID:        node.UserID,
		User:          userInfo,
		IP:            node.IP,
		Version:       node.Version,
		Hostname:      node.Hostname,
		SystemInfo:    node.SystemInfo,
		Status:        status,
		LastHeartbeat: node.LastHeartbeat,
		CreatedAt:     node.CreatedAt,
		UpdatedAt:     node.UpdatedAt,
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
