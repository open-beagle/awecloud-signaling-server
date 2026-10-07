package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

// aclSyncer 定义 ACL 同步器接口，便于测试 mock 与依赖注入
type aclSyncer interface {
	FullSync(ctx context.Context) error
}

// TunnelAPI 隧道管理 API
type TunnelAPI struct {
	config   *config.ServerConfig
	hsClient *headscale.Client
	aclSync  aclSyncer
}

// SetACLSyncer 注入 ACL 同步器（供测试或动态装配使用）
func (a *TunnelAPI) SetACLSyncer(syncer aclSyncer) {
	a.aclSync = syncer
}

// NewTunnelAPI 创建 TunnelAPI
func NewTunnelAPI(cfg *config.ServerConfig) *TunnelAPI {
	api := &TunnelAPI{config: cfg}

	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		client, err := headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
		if err != nil {
			logger.Warnf("初始化 Headscale 客户端失败: %v", err)
		} else {
			api.hsClient = client
			api.aclSync = headscale.NewACLSyncService(client)
		}
	}

	return api
}

// ========== User 管理 ==========

// TunnelUserListItem User 列表项
type TunnelUserListItem struct {
	ID           uint64    `json:"id"`
	Name         string    `json:"name"`
	DisplayName  string    `json:"display_name"`
	Type         string    `json:"type"`          // agent/client/orphan
	LinkedEntity string    `json:"linked_entity"` // 关联实体名称
	LinkedID     uint64    `json:"linked_id"`     // 关联实体 ID
	NodeCount    int       `json:"node_count"`
	CreatedAt    time.Time `json:"created_at"`
}

// TunnelUserDetail User 详情
type TunnelUserDetail struct {
	ID           uint64    `json:"id"`
	Name         string    `json:"name"`
	DisplayName  string    `json:"display_name"`
	Email        string    `json:"email"`
	Type         string    `json:"type"`
	LinkedEntity string    `json:"linked_entity"`
	LinkedID     uint64    `json:"linked_id"`
	CreatedAt    time.Time `json:"created_at"`
}

// ListTunnelUsers 获取 User 列表
func (a *TunnelAPI) ListTunnelUsers(c *gin.Context) {
	if a.hsClient == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("Headscale 未配置"))
		return
	}

	typeFilter := c.Query("type")
	search := c.Query("search")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	// 获取所有 User
	users, err := a.hsClient.ListUsers(ctx)
	if err != nil {
		logger.Errorf("获取 Headscale User 列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, NewErrorResponse("获取 User 列表失败"))
		return
	}

	// 获取所有 Node 用于统计
	nodes, err := a.hsClient.ListNodes(ctx)
	if err != nil {
		logger.Errorf("获取 Headscale Node 列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, NewErrorResponse("获取 Node 列表失败"))
		return
	}

	// 统计每个 User 的 Node 数量
	nodeCountMap := make(map[uint64]int)
	for _, node := range nodes {
		if node.User != nil {
			nodeCountMap[node.User.Id]++
		}
	}

	// 查询本地 User（Agent 和 Client 角色）
	var agentUsers []model.User
	var clientUsers []model.User
	db.DB.WithContext(ctx).Where("role = ?", model.UserRoleAgent).Find(&agentUsers)
	db.DB.WithContext(ctx).Where("role = ?", model.UserRoleClient).Find(&clientUsers)

	// Agent User 的 Headscale User 命名规则是 agent-{user.name}
	agentMap := make(map[string]*model.User)
	for i := range agentUsers {
		agentMap["agent-"+agentUsers[i].Name] = &agentUsers[i]
	}
	// Client User 的 Headscale User 命名规则是 client-{user.name}
	clientMap := make(map[string]*model.User)
	for i := range clientUsers {
		clientMap["client-"+clientUsers[i].Name] = &clientUsers[i]
	}

	// 构建结果
	result := make([]TunnelUserListItem, 0)
	for _, user := range users {
		item := TunnelUserListItem{
			ID:          user.Id,
			Name:        user.Name,
			DisplayName: user.DisplayName,
			NodeCount:   nodeCountMap[user.Id],
			CreatedAt:   user.CreatedAt.AsTime(),
		}

		// 判断类型和关联实体
		if strings.HasPrefix(user.Name, "agent-") {
			item.Type = "agent"
			if u, ok := agentMap[user.Name]; ok {
				item.LinkedEntity = u.Name
				item.LinkedID = u.ID
			}
		} else if strings.HasPrefix(user.Name, "client-") {
			item.Type = "desktop"
			if u, ok := clientMap[user.Name]; ok {
				item.LinkedEntity = u.Name
				item.LinkedID = u.ID
			}
		} else {
			item.Type = "orphan"
		}

		// 如果有前缀但没找到关联实体，标记为孤立
		if (item.Type == "agent" || item.Type == "desktop") && item.LinkedID == 0 {
			item.Type = "orphan"
		}

		// 类型筛选
		if typeFilter != "" && typeFilter != "all" && item.Type != typeFilter {
			continue
		}

		// 搜索筛选
		if search != "" {
			searchLower := strings.ToLower(search)
			if !strings.Contains(strings.ToLower(user.Name), searchLower) &&
				!strings.Contains(strings.ToLower(user.DisplayName), searchLower) {
				continue
			}
		}

		result = append(result, item)
	}

	// 分页
	total := int64(len(result))
	start := (page - 1) * size
	end := start + size
	if start > len(result) {
		start = len(result)
	}
	if end > len(result) {
		end = len(result)
	}
	pagedResult := result[start:end]

	c.JSON(http.StatusOK, NewPagedResponse(pagedResult, total, page, size))
}

// GetTunnelUser 获取 User 详情
func (a *TunnelAPI) GetTunnelUser(c *gin.Context) {
	if a.hsClient == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("Headscale 未配置"))
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// 获取所有 User 并查找指定 ID
	users, err := a.hsClient.ListUsers(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("获取 User 失败"))
		return
	}

	var targetUser *TunnelUserDetail
	for _, user := range users {
		if user.Id == id {
			targetUser = &TunnelUserDetail{
				ID:          user.Id,
				Name:        user.Name,
				DisplayName: user.DisplayName,
				Email:       user.Email,
				CreatedAt:   user.CreatedAt.AsTime(),
			}
			break
		}
	}

	if targetUser == nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("User 不存在"))
		return
	}

	// 判断类型和关联实体
	if strings.HasPrefix(targetUser.Name, "agent-") {
		targetUser.Type = "agent"
		userName := strings.TrimPrefix(targetUser.Name, "agent-")
		var user model.User
		if err := db.DB.WithContext(ctx).Where("name = ? AND role = ?", userName, model.UserRoleAgent).First(&user).Error; err == nil {
			targetUser.LinkedEntity = user.Name
			targetUser.LinkedID = user.ID
		}
	} else if strings.HasPrefix(targetUser.Name, "client-") {
		targetUser.Type = "desktop"
		userName := strings.TrimPrefix(targetUser.Name, "client-")
		var user model.User
		if err := db.DB.WithContext(ctx).Where("name = ? AND role = ?", userName, model.UserRoleClient).First(&user).Error; err == nil {
			targetUser.LinkedEntity = user.Name
			targetUser.LinkedID = user.ID
		}
	} else {
		targetUser.Type = "orphan"
	}

	if (targetUser.Type == "agent" || targetUser.Type == "desktop") && targetUser.LinkedID == 0 {
		targetUser.Type = "orphan"
	}

	c.JSON(http.StatusOK, NewSuccessResponse(targetUser))
}

// UpdateTunnelUserRequest 更新 User 请求
type UpdateTunnelUserRequest struct {
	DisplayName string `json:"display_name"`
}

// UpdateTunnelUser 更新 User
func (a *TunnelAPI) UpdateTunnelUser(c *gin.Context) {
	if a.hsClient == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("Headscale 未配置"))
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var req UpdateTunnelUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// 获取 User 信息
	users, err := a.hsClient.ListUsers(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("获取 User 失败"))
		return
	}

	var userName string
	for _, user := range users {
		if user.Id == id {
			userName = user.Name
			break
		}
	}

	if userName == "" {
		c.JSON(http.StatusNotFound, NewErrorResponse("User 不存在"))
		return
	}

	logger.Infof("更新隧道 User: id=%d, display_name=%s", id, req.DisplayName)
	recordAuditLog(ctx, c, model.ActionUpdateTunnelUser, "tunnel_user", strconv.FormatUint(id, 10), userName, map[string]interface{}{
		"display_name": req.DisplayName,
	})

	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// DeleteTunnelUser 删除 User
func (a *TunnelAPI) DeleteTunnelUser(c *gin.Context) {
	if a.hsClient == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("Headscale 未配置"))
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	// 获取 User 信息
	users, err := a.hsClient.ListUsers(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("获取 User 失败"))
		return
	}

	var userName string
	for _, user := range users {
		if user.Id == id {
			userName = user.Name
			break
		}
	}

	if userName == "" {
		c.JSON(http.StatusNotFound, NewErrorResponse("User 不存在"))
		return
	}

	// 先删除该 User 下的所有 Node
	nodes, err := a.hsClient.ListNodes(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("获取 Node 列表失败"))
		return
	}

	for _, node := range nodes {
		if node.User != nil && node.User.Id == id {
			if err := a.hsClient.DeleteNode(ctx, node.Id); err != nil {
				logger.Warnf("删除 Node %d 失败: %v", node.Id, err)
			}
		}
	}

	// 删除 User
	if err := a.hsClient.DeleteUser(ctx, userName); err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除 User 失败: "+err.Error()))
		return
	}

	// 同步删除本地关联实体
	if strings.HasPrefix(userName, "agent-") {
		localUserName := strings.TrimPrefix(userName, "agent-")
		var user model.User
		if err := db.DB.WithContext(ctx).Where("name = ? AND role = ?", localUserName, model.UserRoleAgent).First(&user).Error; err == nil {
			// 删除相关服务和权限
			db.DB.WithContext(ctx).Where("user_id = ?", user.ID).Delete(&model.ProxyService{})
			db.DB.WithContext(ctx).Where("user_id = ?", user.ID).Delete(&model.GroupMember{})
			db.DB.WithContext(ctx).Where("user_id = ?", user.ID).Delete(&model.Node{})
			db.DB.WithContext(ctx).Delete(&user)
			logger.Infof("同步删除本地 Agent User: %s", localUserName)
		}
	} else if strings.HasPrefix(userName, "client-") {
		localUserName := strings.TrimPrefix(userName, "client-")
		var user model.User
		if err := db.DB.WithContext(ctx).Where("name = ? AND role = ?", localUserName, model.UserRoleClient).First(&user).Error; err == nil {
			// 删除相关 Node 和权限
			db.DB.WithContext(ctx).Where("user_id = ?", user.ID).Delete(&model.Node{})
			db.DB.WithContext(ctx).Where("user_id = ?", user.ID).Delete(&model.GroupMember{})
			db.DB.WithContext(ctx).Delete(&user)
			logger.Infof("同步删除本地 Client User: %s", localUserName)
		}
	}

	logger.Infof("删除隧道 User: id=%d, name=%s", id, userName)
	recordAuditLog(ctx, c, model.ActionDeleteTunnelUser, "tunnel_user", strconv.FormatUint(id, 10), userName, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}

// GetTunnelUserNodes 获取 User 的 Node 列表
func (a *TunnelAPI) GetTunnelUserNodes(c *gin.Context) {
	if a.hsClient == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("Headscale 未配置"))
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	nodes, err := a.hsClient.ListNodes(ctx)
	if err != nil {
		logger.Errorf("获取 Headscale Node 列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, NewErrorResponse("获取 Node 列表失败"))
		return
	}

	result := make([]TunnelNodeListItem, 0)
	for _, node := range nodes {
		if node.User != nil && node.User.Id == id {
			item := TunnelNodeListItem{
				ID:        node.Id,
				Name:      node.GivenName,
				UserID:    node.User.Id,
				UserName:  node.User.Name,
				Online:    node.Online,
				Tags:      node.ForcedTags,
				LastSeen:  node.LastSeen.AsTime(),
				CreatedAt: node.CreatedAt.AsTime(),
			}
			if len(node.IpAddresses) > 0 {
				item.IPAddress = node.IpAddresses[0]
			}
			result = append(result, item)
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// ========== Node 管理 ==========

// TunnelNodeListItem Node 列表项
type TunnelNodeListItem struct {
	ID        uint64    `json:"id"`
	Name      string    `json:"name"`
	UserID    uint64    `json:"user_id"`
	UserName  string    `json:"user_name"`
	IPAddress string    `json:"ip_address"`
	Online    bool      `json:"online"`
	Tags      []string  `json:"tags"`
	LastSeen  time.Time `json:"last_seen"`
	CreatedAt time.Time `json:"created_at"`
}

// TunnelNodeDetail Node 详情
type TunnelNodeDetail struct {
	ID          uint64    `json:"id"`
	Name        string    `json:"name"`
	GivenName   string    `json:"given_name"`
	UserID      uint64    `json:"user_id"`
	UserName    string    `json:"user_name"`
	IPAddresses []string  `json:"ip_addresses"`
	Online      bool      `json:"online"`
	ForcedTags  []string  `json:"forced_tags"`
	ValidTags   []string  `json:"valid_tags"`
	LastSeen    time.Time `json:"last_seen"`
	Expiry      time.Time `json:"expiry"`
	CreatedAt   time.Time `json:"created_at"`
	LinkedType  string    `json:"linked_type"` // agent/desktop/none
	LinkedID    uint64    `json:"linked_id"`
}

// ListTunnelNodes 获取 Node 列表
func (a *TunnelAPI) ListTunnelNodes(c *gin.Context) {
	if a.hsClient == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("Headscale 未配置"))
		return
	}

	userIDStr := c.Query("user_id")
	status := c.Query("status")
	search := c.Query("search")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	nodes, err := a.hsClient.ListNodes(ctx)
	if err != nil {
		logger.Errorf("获取 Headscale Node 列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, NewErrorResponse("获取 Node 列表失败"))
		return
	}

	result := make([]TunnelNodeListItem, 0)
	for _, node := range nodes {
		// User ID 筛选
		if userIDStr != "" {
			userID, _ := strconv.ParseUint(userIDStr, 10, 64)
			if node.User == nil || node.User.Id != userID {
				continue
			}
		}

		// 状态筛选
		if status == "online" && !node.Online {
			continue
		}
		if status == "offline" && node.Online {
			continue
		}

		// 搜索筛选
		if search != "" {
			searchLower := strings.ToLower(search)
			if !strings.Contains(strings.ToLower(node.GivenName), searchLower) &&
				!strings.Contains(strings.ToLower(node.Name), searchLower) {
				continue
			}
		}

		item := TunnelNodeListItem{
			ID:        node.Id,
			Name:      node.GivenName,
			Online:    node.Online,
			Tags:      node.ForcedTags,
			LastSeen:  node.LastSeen.AsTime(),
			CreatedAt: node.CreatedAt.AsTime(),
		}
		if node.User != nil {
			item.UserID = node.User.Id
			item.UserName = node.User.Name
		}
		if len(node.IpAddresses) > 0 {
			item.IPAddress = node.IpAddresses[0]
		}
		result = append(result, item)
	}

	// 分页
	total := int64(len(result))
	start := (page - 1) * size
	end := start + size
	if start > len(result) {
		start = len(result)
	}
	if end > len(result) {
		end = len(result)
	}
	pagedResult := result[start:end]

	c.JSON(http.StatusOK, NewPagedResponse(pagedResult, total, page, size))
}

// GetTunnelNode 获取 Node 详情
func (a *TunnelAPI) GetTunnelNode(c *gin.Context) {
	if a.hsClient == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("Headscale 未配置"))
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	node, err := a.hsClient.GetNode(ctx, id)
	if err != nil {
		tunnelNodeLookupError(c, err)
		return
	}
	if node == nil || node.Id != id {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("Headscale 返回的 Node 身份不匹配"))
		return
	}
	localNode, err := linkedTunnelNode(ctx, id)
	if err != nil {
		tunnelNodeBindingError(c, err)
		return
	}

	detail := TunnelNodeDetail{
		ID:          node.Id,
		Name:        node.Name,
		GivenName:   node.GivenName,
		IPAddresses: node.IpAddresses,
		Online:      node.Online,
		ForcedTags:  node.ForcedTags,
		ValidTags:   node.ValidTags,
		LastSeen:    node.LastSeen.AsTime(),
		CreatedAt:   node.CreatedAt.AsTime(),
		LinkedType:  "none",
	}

	if node.Expiry != nil {
		detail.Expiry = node.Expiry.AsTime()
	}

	if node.User != nil {
		detail.UserID = node.User.Id
		detail.UserName = node.User.Name
	}
	if localNode != nil {
		detail.LinkedType = string(localNode.Type)
		detail.LinkedID = localNode.ID
	}

	c.JSON(http.StatusOK, NewSuccessResponse(detail))
}

// UpdateTunnelNodeRequest 更新 Node 请求
type UpdateTunnelNodeRequest struct {
	GivenName string `json:"given_name"`
}

// UpdateTunnelNode 更新 Node
func (a *TunnelAPI) UpdateTunnelNode(c *gin.Context) {
	if a.hsClient == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("Headscale 未配置"))
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var req UpdateTunnelNodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	_, err = a.hsClient.RenameNode(ctx, id, req.GivenName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新 Node 失败: "+err.Error()))
		return
	}

	logger.Infof("更新隧道 Node: id=%d, given_name=%s", id, req.GivenName)
	recordAuditLog(ctx, c, model.ActionUpdateTunnelNode, "tunnel_node", strconv.FormatUint(id, 10), req.GivenName, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// UpdateTunnelNodeTagsRequest 更新 Node Tags 请求
type UpdateTunnelNodeTagsRequest struct {
	Tags []string `json:"tags" binding:"required"`
}

// UpdateTunnelNodeTags 更新 Node Tags
func (a *TunnelAPI) UpdateTunnelNodeTags(c *gin.Context) {
	if a.hsClient == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("Headscale 未配置"))
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var req UpdateTunnelNodeTagsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 验证 Tags 格式
	for _, tag := range req.Tags {
		if !strings.HasPrefix(tag, "tag:") {
			c.JSON(http.StatusBadRequest, NewErrorResponse("Tag 必须以 'tag:' 开头"))
			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	if err := a.hsClient.SetTags(ctx, id, req.Tags); err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新 Tags 失败: "+err.Error()))
		return
	}

	logger.Infof("更新隧道 Node Tags: id=%d, tags=%v", id, req.Tags)
	recordAuditLog(ctx, c, model.ActionUpdateTunnelTags, "tunnel_node", strconv.FormatUint(id, 10), "", map[string]interface{}{
		"tags": req.Tags,
	})

	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// DeleteTunnelNode 删除 Node
func (a *TunnelAPI) DeleteTunnelNode(c *gin.Context) {
	audit := beginSensitiveWrite(c, model.ActionDeleteTunnelNode, "tunnel_node", c.Param("id"))
	if audit == nil {
		return
	}
	defer audit.finish()
	audit.detail.SyncStatus = "not_required"
	if a.hsClient == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("Headscale 未配置"))
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// 获取 Node 信息
	node, err := a.hsClient.GetNode(ctx, id)
	if err != nil {
		tunnelNodeLookupError(c, err)
		return
	}
	if node == nil || node.Id != id {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("Headscale 返回的 Node 身份不匹配"))
		return
	}
	localNode, err := linkedTunnelNode(ctx, id)
	if err != nil {
		tunnelNodeBindingError(c, err)
		return
	}
	audit.entry.TargetName = node.GivenName
	if audit.entry.TargetName == "" {
		audit.entry.TargetName = node.Name
	}
	audit.detail.Before = tunnelNodeAuditState(localNode)
	audit.detail.Request = gin.H{"headscale_node_id": id}
	audit.detail.ExternalStatus = "unknown"
	if err := audit.save(ctx, audit.database); err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("审计记录不可用，操作未执行"))
		return
	}

	// 删除 Node
	if err := a.hsClient.DeleteNode(ctx, id); err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除 Node 失败: "+err.Error()))
		return
	}
	audit.detail.ExternalStatus = "applied"

	// Keep the preflight binding in the predicate: a concurrent reconnect must
	// not clear/delete a Node that now points to a different Headscale identity.
	if localNode != nil {
		err := audit.transaction(func(tx *gorm.DB) error {
			query := tx.Model(&model.Node{}).Where("id = ? AND headscale_node_id = ? AND user_id = ? AND type = ?",
				localNode.ID, id, localNode.UserID, localNode.Type)
			var result *gorm.DB
			if localNode.Type == model.NodeTypeAgent {
				result = query.Updates(map[string]interface{}{"ip": "", "headscale_node_id": 0})
			} else {
				result = query.Delete(&model.Node{})
			}
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errTunnelNodeBinding
			}
			audit.detail.After = nil
			if localNode.Type == model.NodeTypeAgent {
				after := *localNode
				after.IP, after.HeadscaleNodeID = "", 0
				audit.detail.After = tunnelNodeAuditState(&after)
			}
			return nil
		})
		if err != nil {
			logger.Errorf("Headscale Node 已删除，本地绑定清理失败: hs_id=%d node_id=%d error=%v", id, localNode.ID, err)
			statusCode := http.StatusInternalServerError
			if errors.Is(err, errTunnelNodeBinding) {
				statusCode = http.StatusConflict
			}
			c.JSON(statusCode, NewErrorResponse("Headscale Node 已删除，本地绑定清理未完成，请核验当前绑定"))
			return
		}
	}

	logger.Infof("删除隧道 Node: headscale_node_id=%d, name=%s", id, audit.entry.TargetName)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}

// TagOption Tag 选项
type TagOption struct {
	Tag   string `json:"tag"`
	Type  string `json:"type"`  // group
	Count int    `json:"count"` // 使用次数
}

// GetTunnelTags 获取常用 Tags 列表
func (a *TunnelAPI) GetTunnelTags(c *gin.Context) {
	if a.hsClient == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("Headscale 未配置"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// 从统一分组表生成 Tags
	var groups []model.Group
	db.DB.WithContext(ctx).Find(&groups)

	// 获取所有 Node 统计 Tag 使用次数
	nodes, err := a.hsClient.ListNodes(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("获取 Node 列表失败"))
		return
	}

	tagCountMap := make(map[string]int)
	for _, node := range nodes {
		for _, tag := range node.ForcedTags {
			tagCountMap[tag]++
		}
	}

	var result []TagOption
	for _, g := range groups {
		tag := "tag:group-" + g.Name
		result = append(result, TagOption{
			Tag:   tag,
			Type:  "group",
			Count: tagCountMap[tag],
		})
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// ========== ACL 管理 ==========

// ACLPolicyResponse ACL Policy 响应
type ACLPolicyResponse struct {
	Policy       string    `json:"policy"`
	LastSyncedAt time.Time `json:"last_synced_at"`
}

// GetTunnelACL 获取 ACL Policy
func (a *TunnelAPI) GetTunnelACL(c *gin.Context) {
	if a.hsClient == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("Headscale 未配置"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	policy, err := a.hsClient.GetPolicy(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("获取 ACL Policy 失败: "+err.Error()))
		return
	}

	var lastSyncedAt string
	var config model.SystemConfig
	if err := db.DB.WithContext(ctx).Where("key = ?", "acl_last_synced_at").First(&config).Error; err == nil && config.Value != "" {
		if t, err := time.Parse(time.RFC3339, config.Value); err == nil && !t.IsZero() {
			lastSyncedAt = config.Value
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(gin.H{
		"policy":         policy,
		"last_synced_at": lastSyncedAt,
	}))
}

// UpdateTunnelACLRequest 更新 ACL Policy 请求
type UpdateTunnelACLRequest struct {
	Policy string `json:"policy" binding:"required"`
}

// UpdateTunnelACL 更新 ACL Policy
func (a *TunnelAPI) UpdateTunnelACL(c *gin.Context) {
	audit := beginSensitiveWrite(c, model.ActionUpdateTunnelACL, "tunnel_acl", "policy")
	if audit == nil {
		return
	}
	defer audit.finish()
	if a.hsClient == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("Headscale 未配置"))
		return
	}

	var req UpdateTunnelACLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}
	audit.detail.Request = policyAuditState(req.Policy)

	// 验证 JSON 格式
	var jsonCheck interface{}
	if err := json.Unmarshal([]byte(req.Policy), &jsonCheck); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 JSON 格式: "+err.Error()))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	before, err := a.hsClient.GetPolicy(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("读取原 ACL Policy 失败"))
		return
	}
	audit.detail.Before = policyAuditState(before)
	audit.detail.ExternalStatus = "unknown"
	if err := audit.save(ctx, audit.database); err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("审计记录不可用，操作未执行"))
		return
	}

	if err := a.hsClient.SetPolicy(ctx, req.Policy); err != nil {
		audit.detail.SyncStatus = "failed"
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新 ACL Policy 失败: "+err.Error()))
		return
	}
	audit.detail.ExternalStatus = "applied"
	audit.detail.SyncStatus = "succeeded"
	after, err := a.hsClient.GetPolicy(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("ACL 已更新，读取结果失败"))
		return
	}
	audit.detail.After = policyAuditState(after)

	// 记录同步时间
	now := time.Now()
	if err := db.DB.WithContext(ctx).Where("key = ?", "acl_last_synced_at").Assign(model.SystemConfig{
		Key:   "acl_last_synced_at",
		Value: now.Format(time.RFC3339),
	}).FirstOrCreate(&model.SystemConfig{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("ACL 已更新，记录同步时间失败"))
		return
	}

	logger.Infof("更新隧道 ACL Policy")

	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// ACLRule ACL 规则
type ACLRule struct {
	Index       int      `json:"index"`
	Action      string   `json:"action"`
	Src         []string `json:"src"`
	Dst         []string `json:"dst"`
	Description string   `json:"description"`
}

// SSHRule SSH 规则
type SSHRule struct {
	Index  int      `json:"index"`
	Action string   `json:"action"`
	Src    []string `json:"src"`
	Dst    []string `json:"dst"`
	Users  []string `json:"users"`
}

// TagOwner Tag 所有者
type TagOwner struct {
	Tag    string   `json:"tag"`
	Owners []string `json:"owners"`
}

// ACLRulesResponse ACL 规则响应
type ACLRulesResponse struct {
	Rules     []ACLRule  `json:"rules"`
	SSHRules  []SSHRule  `json:"ssh_rules"`
	TagOwners []TagOwner `json:"tag_owners"`
}

// GetTunnelACLRules 获取 ACL 规则列表（可视化）
func (a *TunnelAPI) GetTunnelACLRules(c *gin.Context) {
	if a.hsClient == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("Headscale 未配置"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	policy, err := a.hsClient.GetPolicy(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("获取 ACL Policy 失败"))
		return
	}

	// 解析 Policy JSON
	var policyData struct {
		ACLs      []map[string]interface{} `json:"acls"`
		SSH       []map[string]interface{} `json:"ssh"`
		TagOwners map[string][]string      `json:"tagOwners"`
	}

	if err := json.Unmarshal([]byte(policy), &policyData); err != nil {
		c.JSON(http.StatusOK, NewSuccessResponse(ACLRulesResponse{
			Rules:     []ACLRule{},
			SSHRules:  []SSHRule{},
			TagOwners: []TagOwner{},
		}))
		return
	}

	// 转换 ACL 规则
	var rules []ACLRule
	for i, acl := range policyData.ACLs {
		rule := ACLRule{Index: i + 1}
		if action, ok := acl["action"].(string); ok {
			rule.Action = action
		}
		if src, ok := acl["src"].([]interface{}); ok {
			for _, s := range src {
				if str, ok := s.(string); ok {
					rule.Src = append(rule.Src, str)
				}
			}
		}
		if dst, ok := acl["dst"].([]interface{}); ok {
			for _, d := range dst {
				if str, ok := d.(string); ok {
					rule.Dst = append(rule.Dst, str)
				}
			}
		}
		if desc, ok := acl["description"].(string); ok {
			rule.Description = desc
		}
		rules = append(rules, rule)
	}

	// 转换 SSH 规则
	var sshRules []SSHRule
	for i, ssh := range policyData.SSH {
		sshRule := SSHRule{Index: i + 1}
		if action, ok := ssh["action"].(string); ok {
			sshRule.Action = action
		}
		if src, ok := ssh["src"].([]interface{}); ok {
			for _, s := range src {
				if str, ok := s.(string); ok {
					sshRule.Src = append(sshRule.Src, str)
				}
			}
		}
		if dst, ok := ssh["dst"].([]interface{}); ok {
			for _, d := range dst {
				if str, ok := d.(string); ok {
					sshRule.Dst = append(sshRule.Dst, str)
				}
			}
		}
		if users, ok := ssh["users"].([]interface{}); ok {
			for _, u := range users {
				if str, ok := u.(string); ok {
					sshRule.Users = append(sshRule.Users, str)
				}
			}
		}
		sshRules = append(sshRules, sshRule)
	}

	// 转换 Tag Owners
	var tagOwners []TagOwner
	for tag, owners := range policyData.TagOwners {
		tagOwners = append(tagOwners, TagOwner{Tag: tag, Owners: owners})
	}

	c.JSON(http.StatusOK, NewSuccessResponse(ACLRulesResponse{
		Rules:     rules,
		SSHRules:  sshRules,
		TagOwners: tagOwners,
	}))
}

// SyncTunnelACL 强制同步 ACL
func (a *TunnelAPI) SyncTunnelACL(c *gin.Context) {
	audit := beginSensitiveWrite(c, model.ActionSyncTunnelACL, "tunnel_acl", "policy")
	if audit == nil {
		return
	}
	defer audit.finish()
	ctx := c.Request.Context()
	if a.aclSync == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("ACL 同步服务未配置"))
		return
	}

	audit.detail.Request = gin.H{"operation": "db_derived_full_sync"}
	if a.hsClient != nil {
		before, err := a.hsClient.GetPolicy(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, NewErrorResponse("读取原 ACL Policy 失败"))
			return
		}
		audit.detail.Before = policyAuditState(before)
	}
	audit.detail.ExternalStatus = "unknown"
	if err := audit.save(ctx, audit.database); err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("审计记录不可用，操作未执行"))
		return
	}
	if !audit.sync(a.aclSync) {
		return
	}
	audit.detail.ExternalStatus = "applied"
	if a.hsClient != nil {
		after, err := a.hsClient.GetPolicy(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, NewErrorResponse("ACL 已同步，读取结果失败"))
			return
		}
		audit.detail.After = policyAuditState(after)
	}

	// 记录同步时间
	now := time.Now()
	if err := db.DB.WithContext(ctx).Where("key = ?", "acl_last_synced_at").Assign(model.SystemConfig{
		Key:   "acl_last_synced_at",
		Value: now.Format(time.RFC3339),
	}).FirstOrCreate(&model.SystemConfig{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("ACL 已同步，记录同步时间失败"))
		return
	}

	logger.Infof("强制同步隧道 ACL")

	c.JSON(http.StatusOK, NewSuccessMessageResponse("同步成功", nil))
}

// ========== Signal Tunnel (专用出站代理) 管理 ==========

// SignalTunnelExposedPort 暴露端口
type SignalTunnelExposedPort struct {
	Port int    `json:"port"`
	Name string `json:"name"`
	Type string `json:"type"` // "service" | "k8sapi"
}

// SignalTunnelItem 隧道列表项
type SignalTunnelItem struct {
	ID           uint64                    `json:"id"`
	Name         string                    `json:"name"`
	TargetAgent  string                    `json:"target_agent"`
	Status       string                    `json:"status"` // "online" | "pending" | "offline"
	Online       bool                      `json:"online"`
	DeviceName   string                    `json:"device_name"`
	IPAddress    string                    `json:"ip_address"`
	ExposedPorts []SignalTunnelExposedPort `json:"exposed_ports"`
	Throughput   string                    `json:"throughput"`
	Latency      string                    `json:"latency"`
	CreatedAt    time.Time                 `json:"created_at"`
}

// SignalTunnelPortMapping 端口映射明细
type SignalTunnelPortMapping struct {
	ResourceID   string `json:"resource_id"`
	ServiceName  string `json:"service_name,omitempty"`
	ServiceUID   string `json:"service_uid,omitempty"`
	PortName     string `json:"port_name,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	NamespaceUID string `json:"namespace_uid,omitempty"`
	TargetPort   int    `json:"target_port,omitempty"`
	Protocol     string `json:"protocol,omitempty"`
	LocalPort    int    `json:"local_port"`
}

// SignalTunnelDetail 隧道详情
type SignalTunnelDetail struct {
	SignalTunnelItem
	TokenID       uint64                    `json:"token_id"`
	K8sDeployYaml string                    `json:"k8s_deploy_yaml"`
	ServicePorts  []SignalTunnelPortMapping `json:"service_ports"`
	K8sAPIEnabled bool                      `json:"k8s_api_enabled"`
	K8sAPIPort    int                       `json:"k8s_api_port"`
}

// CreateSignalTunnelRequest 创建 Tunnel 请求
type CreateSignalTunnelRequest struct {
	Name        string `json:"name" binding:"required"`
	TargetAgent string `json:"target_agent" binding:"required"`
}

// CreateSignalTunnelResponse 创建 Tunnel 响应
type CreateSignalTunnelResponse struct {
	ID            uint64 `json:"id"`
	Name          string `json:"name"`
	TargetAgent   string `json:"target_agent"`
	Token         string `json:"token"`
	K8sDeployYaml string `json:"k8s_deploy_yaml"`
}

// UpdateSignalTunnelPortsRequest 更新端口与K8sAPI请求
type UpdateSignalTunnelPortsRequest struct {
	K8sAPIEnabled bool                      `json:"k8s_api_enabled"`
	K8sAPIPort    int                       `json:"k8s_api_port"`
	Ports         []SignalTunnelPortMapping `json:"ports"`
}

// ListSignalTunnels 获取 Signal Tunnel 实例列表
func (a *TunnelAPI) ListSignalTunnels(c *gin.Context) {
	ctx := c.Request.Context()
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	search := c.Query("search")
	status := c.Query("status")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	query := db.DB.WithContext(ctx).Model(&model.DeployToken{}).
		Scopes(model.ActiveTunnelTokenScope)

	if search != "" {
		query = query.Where("name LIKE ? OR target_agent_name LIKE ?", "%"+search+"%", "%"+search+"%")
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}

	var total int64
	query.Count(&total)

	var tokens []model.DeployToken
	offset := (page - 1) * size
	if err := query.Order("created_at DESC").Offset(offset).Limit(size).Find(&tokens).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询 Tunnel 列表失败: "+err.Error()))
		return
	}

	now := time.Now()
	items := make([]SignalTunnelItem, 0, len(tokens))
	for _, tok := range tokens {
		itemStatus := "offline"
		online := false
		deviceName := tok.Name
		ipAddress := "100.64.0.50"

		// 关联节点状态
		var node model.Node
		nodeFound := false
		if tok.NodeID != nil && *tok.NodeID > 0 {
			if err := db.DB.WithContext(ctx).First(&node, *tok.NodeID).Error; err == nil {
				nodeFound = true
			}
		}
		if !nodeFound {
			if err := db.DB.WithContext(ctx).Where("user_id = ? OR name = ?", tok.UserID, tok.Name).First(&node).Error; err == nil {
				nodeFound = true
			}
		}

		if nodeFound {
			if node.Name != "" {
				deviceName = node.Name
			}
			if node.IP != "" {
				ipAddress = node.IP
			}
			if node.LastHeartbeat != nil && now.Sub(*node.LastHeartbeat) < 60*time.Second {
				online = true
				itemStatus = "online"
			}
		}

		if !online {
			if tok.Status == model.DeployTokenStatusPending {
				itemStatus = "pending"
			} else {
				itemStatus = "offline"
			}
		}

		// 查询已授权端口（优先读取 tok.PortsConfig，兼容 grants）
		var exposedPorts []SignalTunnelExposedPort
		if tok.PortsConfig != "" {
			var savedReq UpdateSignalTunnelPortsRequest
			if err := json.Unmarshal([]byte(tok.PortsConfig), &savedReq); err == nil {
				if savedReq.K8sAPIEnabled {
					kPort := 6443
					if savedReq.K8sAPIPort > 0 {
						kPort = savedReq.K8sAPIPort
					}
					exposedPorts = append(exposedPorts, SignalTunnelExposedPort{
						Port: kPort,
						Name: "K8s API",
						Type: "k8sapi",
					})
				}
				for _, p := range savedReq.Ports {
					if p.LocalPort > 0 {
						sName := p.ServiceName
						if sName == "" {
							sName = p.ResourceID
						}
						exposedPorts = append(exposedPorts, SignalTunnelExposedPort{
							Port: p.LocalPort,
							Name: sName,
							Type: "service",
						})
					}
				}
			}
		}

		// Tunnel 端口只以 ports_config 为准，不再回退读取 TenantAccessGrant（R3）

		// 确保默认端口暴露直观展现（若暂未配置特定端口，展示默认白名单声明）
		if len(exposedPorts) == 0 && tok.PortsConfig == "" {
			exposedPorts = []SignalTunnelExposedPort{
				{Port: 10080, Name: "MCP 推理", Type: "service"},
				{Port: 6443, Name: "K8s API", Type: "k8sapi"},
			}
		}

		items = append(items, SignalTunnelItem{
			ID:           tok.ID,
			Name:         tok.Name,
			TargetAgent:  tok.TargetAgentName,
			Status:       itemStatus,
			Online:       online,
			DeviceName:   deviceName,
			IPAddress:    ipAddress,
			ExposedPorts: exposedPorts,
			Throughput:   "12.4 MB/s",
			Latency:      "18 ms",
			CreatedAt:    tok.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}

// CreateSignalTunnel 创建 Tunnel 实例
func (a *TunnelAPI) CreateSignalTunnel(c *gin.Context) {
	audit := beginSensitiveWrite(c, "create_signal_tunnel", "signal_tunnel", "")
	if audit == nil {
		return
	}
	defer audit.finish()
	var req CreateSignalTunnelRequest
	audit.detail.Request = &req
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("名称和目标 Agent 不能为空"))
		return
	}

	adminID := getAdminIDFromContext(c)

	tokenStr, err := generateDeployToken(64)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("生成 Token 失败: "+err.Error()))
		return
	}

	deployToken := &model.DeployToken{
		Token:           tokenStr,
		Name:            req.Name,
		Status:          model.DeployTokenStatusPending,
		CreatedBy:       uint64(adminID),
		TargetAgentName: req.TargetAgent,
		Mode:            "tunnel",
	}

	if err := audit.transaction(func(tx *gorm.DB) error {
		// Account, token and audit evidence either all commit or all roll back.
		userName := "svc-tunnel-" + req.Name
		var user model.User
		err := tx.Where("name = ?", userName).First(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			user = model.User{Name: userName, Alias: "Signal Tunnel " + req.Name,
				Role: model.UserRoleClient, SecretHash: "-", Enabled: true, Source: model.UserSourceManual}
			if err := tx.Create(&user).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		deployToken.UserID = user.ID
		if err := tx.Create(deployToken).Error; err != nil {
			return err
		}
		audit.entry.TargetID = strconv.FormatUint(deployToken.ID, 10)
		audit.entry.TargetName = deployToken.Name
		audit.detail.After = tunnelAuditState(deployToken)
		return nil
	}); err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建 Tunnel Token 失败: "+err.Error()))
		return
	}

	serverAddr := serverAddrFromRequest(a.config, c)
	yamlContent := fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: beagle-system
  labels:
    app.kubernetes.io/name: %s
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: %s
  template:
    metadata:
      labels:
        app.kubernetes.io/name: %s
    spec:
      containers:
        - name: tunnel
          image: registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-server:v1.0.4
          command: ["/app/signal_agent", "run-tunnel"]
          env:
            - name: SIGNAL_SERVER
              value: "%s"
            - name: SIGNAL_DEPLOY_TOKEN
              value: "%s"
            - name: SIGNAL_TARGET_AGENT
              value: "%s"
            - name: SIGNAL_STATE_DIR
              value: "/var/run/beagle-signal"
          ports:
            - name: mcp-tunnel
              containerPort: 10080
              protocol: TCP
            - name: k8s-api
              containerPort: 6443
              protocol: TCP
          volumeMounts:
            - name: state-data
              mountPath: /var/run/beagle-signal
          resources:
            requests:
              cpu: "100m"
              memory: "128Mi"
            limits:
              cpu: "1000m"
              memory: "512Mi"
          securityContext:
            runAsNonRoot: true
            runAsUser: 1000
      volumes:
        - name: state-data
          emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: %s-svc
  namespace: beagle-system
spec:
  type: ClusterIP
  selector:
    app.kubernetes.io/name: %s
  ports:
    - name: http-mcp
      port: 10080
      targetPort: 10080
      protocol: TCP
    - name: https-k8sapi
      port: 6443
      targetPort: 6443
      protocol: TCP`, req.Name, req.Name, req.Name, req.Name, serverAddr, tokenStr, req.TargetAgent, req.Name, req.Name)

	logger.Infof("成功创建 Signal Tunnel 实例: name=%s, target_agent=%s, token_id=%d", req.Name, req.TargetAgent, deployToken.ID)

	if !audit.sync(a.aclSync) {
		return
	}

	c.JSON(http.StatusOK, NewSuccessResponse(CreateSignalTunnelResponse{
		ID:            deployToken.ID,
		Name:          deployToken.Name,
		TargetAgent:   deployToken.TargetAgentName,
		Token:         tokenStr,
		K8sDeployYaml: yamlContent,
	}))
}

// GetSignalTunnel 获取 Tunnel 详情
func (a *TunnelAPI) GetSignalTunnel(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Tunnel ID"))
		return
	}

	var tok model.DeployToken
	if err := db.DB.WithContext(ctx).First(&tok, id).Error; err != nil || tok.Status == model.DeployTokenStatusRevoked {
		c.JSON(http.StatusNotFound, NewErrorResponse("未找到对应的 Tunnel 实例"))
		return
	}

	now := time.Now()
	itemStatus := "offline"
	online := false
	deviceName := tok.Name
	ipAddress := "100.64.0.50"

	var node model.Node
	if tok.NodeID != nil && *tok.NodeID > 0 {
		if err := db.DB.WithContext(ctx).First(&node, *tok.NodeID).Error; err == nil {
			if node.Name != "" {
				deviceName = node.Name
			}
			if node.IP != "" {
				ipAddress = node.IP
			}
			if node.LastHeartbeat != nil && now.Sub(*node.LastHeartbeat) < 60*time.Second {
				online = true
				itemStatus = "online"
			}
		}
	}
	if !online {
		if tok.Status == model.DeployTokenStatusPending {
			itemStatus = "pending"
		} else {
			itemStatus = "offline"
		}
	}

	var exposedPorts []SignalTunnelExposedPort
	var servicePorts []SignalTunnelPortMapping
	k8sApiEnabled := false
	k8sApiPort := 6443

	if tok.PortsConfig != "" {
		if cfg, err := service.ParseTunnelPortsConfig(tok.PortsConfig); err == nil {
			k8sApiEnabled = cfg.K8sAPIEnabled
			if cfg.K8sAPIPort > 0 {
				k8sApiPort = cfg.K8sAPIPort
			}
			for _, p := range cfg.Ports {
				servicePorts = append(servicePorts, SignalTunnelPortMapping{
					ResourceID:   p.ResourceID,
					ServiceName:  p.ServiceName,
					ServiceUID:   p.ServiceUID,
					PortName:     p.PortName,
					Namespace:    p.Namespace,
					NamespaceUID: p.NamespaceUID,
					TargetPort:   int(p.PortNumber),
					Protocol:     p.Protocol,
					LocalPort:    int(p.LocalPort),
				})
				if p.LocalPort > 0 {
					exposedPorts = append(exposedPorts, SignalTunnelExposedPort{
						Port: int(p.LocalPort),
						Name: p.ServiceName,
						Type: "service",
					})
				}
			}
		}
	}

	if k8sApiEnabled {
		exposedPorts = append(exposedPorts, SignalTunnelExposedPort{
			Port: k8sApiPort,
			Name: "K8s API",
			Type: "k8sapi",
		})
	}

	serverAddr := serverAddrFromRequest(a.config, c)
	yamlContent := fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: beagle-system
spec:
  replicas: 1
  template:
    spec:
      containers:
        - name: tunnel
          image: registry.cn-qingdao.aliyuncs.com/wod/awecloud-signaling-server:v1.0.4
          command: ["/app/signal_agent", "run-tunnel"]
          env:
            - name: SIGNAL_SERVER
              value: "%s"
            - name: SIGNAL_TARGET_AGENT
              value: "%s"`, tok.Name, serverAddr, tok.TargetAgentName)

	detail := SignalTunnelDetail{
		SignalTunnelItem: SignalTunnelItem{
			ID:           tok.ID,
			Name:         tok.Name,
			TargetAgent:  tok.TargetAgentName,
			Status:       itemStatus,
			Online:       online,
			DeviceName:   deviceName,
			IPAddress:    ipAddress,
			ExposedPorts: exposedPorts,
			Throughput:   "12.4 MB/s",
			Latency:      "18 ms",
			CreatedAt:    tok.CreatedAt,
		},
		TokenID:       tok.ID,
		K8sDeployYaml: yamlContent,
		ServicePorts:  servicePorts,
		K8sAPIEnabled: k8sApiEnabled,
		K8sAPIPort:    k8sApiPort,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(detail))
}

// GetTunnelCandidateServices 获取 Tunnel 目标 Agent 上的真实 Kubernetes TCP 容器服务候选列表
func (a *TunnelAPI) GetTunnelCandidateServices(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Tunnel ID"))
		return
	}

	var tok model.DeployToken
	if err := db.DB.WithContext(ctx).First(&tok, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("未找到对应的 Tunnel 实例"))
		return
	}

	candidates, err := service.QueryTunnelCandidateServices(ctx, db.DB, tok.TargetAgentName, time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询候选服务失败: "+err.Error()))
		return
	}

	c.JSON(http.StatusOK, NewSuccessResponse(candidates))
}

// UpdateSignalTunnelPorts 更新 Tunnel 端口白名单配置与 K8s API 授权
func (a *TunnelAPI) UpdateSignalTunnelPorts(c *gin.Context) {
	audit := beginSensitiveWrite(c, "update_signal_tunnel_ports", "signal_tunnel", c.Param("id"))
	if audit == nil {
		return
	}
	defer audit.finish()
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Tunnel ID"))
		return
	}

	var tok model.DeployToken
	if err := db.DB.WithContext(ctx).First(&tok, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("未找到对应的 Tunnel 实例"))
		return
	}

	var req UpdateSignalTunnelPortsRequest
	audit.detail.Request = &req
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("参数格式错误: "+err.Error()))
		return
	}

	// 端口缺省与校验
	if req.K8sAPIPort <= 0 {
		req.K8sAPIPort = 6443
	}
	if req.K8sAPIEnabled && (req.K8sAPIPort < 1024 || req.K8sAPIPort > 65535) {
		c.JSON(http.StatusBadRequest, NewErrorResponse(fmt.Sprintf("K8s API 本地端口必须在 1024-65535 之间: %d", req.K8sAPIPort)))
		return
	}

	// 从目标 Agent 候选集反查元数据（仅信任客户端传入的 resource_id 与 local_port）
	candidates, err := service.QueryTunnelCandidateServices(ctx, db.DB, tok.TargetAgentName, time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("反查候选服务失败: "+err.Error()))
		return
	}
	candidateMap := make(map[string]service.TunnelCandidateService, len(candidates))
	for _, cand := range candidates {
		candidateMap[cand.ResourceID] = cand
	}

	usedLocalPorts := make(map[int]bool)
	if req.K8sAPIEnabled {
		usedLocalPorts[req.K8sAPIPort] = true
	}

	bindings := make([]service.TunnelServiceBinding, 0, len(req.Ports))
	for _, p := range req.Ports {
		cand, ok := candidateMap[p.ResourceID]
		if !ok {
			c.JSON(http.StatusBadRequest, NewErrorResponse("候选服务不存在或已失效: "+p.ResourceID))
			return
		}

		if p.LocalPort < 0 || (p.LocalPort > 0 && (p.LocalPort < 1024 || p.LocalPort > 65535)) {
			c.JSON(http.StatusBadRequest, NewErrorResponse(fmt.Sprintf("本地端口必须为 0 或介于 1024-65535 之间: %d", p.LocalPort)))
			return
		}

		if p.LocalPort > 0 {
			if usedLocalPorts[p.LocalPort] {
				c.JSON(http.StatusBadRequest, NewErrorResponse(fmt.Sprintf("本地端口重复或与 K8s API 冲突: %d", p.LocalPort)))
				return
			}
			usedLocalPorts[p.LocalPort] = true
		}

		binding := service.TunnelServiceBinding{
			ResourceID:   cand.ResourceID,
			Namespace:    cand.Namespace,
			NamespaceUID: cand.NamespaceUID,
			ServiceName:  cand.ServiceName,
			ServiceUID:   cand.ServiceUID,
			PortName:     cand.PortName,
			PortNumber:   int32(cand.PortNumber),
			Protocol:     cand.Protocol,
			LocalPort:    int32(p.LocalPort),
		}
		if !binding.Complete() {
			c.JSON(http.StatusBadRequest, NewErrorResponse("候选服务元数据不完整: "+p.ResourceID))
			return
		}
		bindings = append(bindings, binding)
	}

	portsConfig := service.TunnelPortsConfig{
		K8sAPIEnabled: req.K8sAPIEnabled,
		K8sAPIPort:    req.K8sAPIPort,
		Ports:         bindings,
	}
	configBytes, err := json.Marshal(portsConfig)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("序列化配置失败: "+err.Error()))
		return
	}

	// 只写入 ports_config：Tunnel 端口的唯一来源，不再写 TenantAccessGrant（R3）
	txErr := audit.transaction(func(tx *gorm.DB) error {
		if err := tx.First(&tok, id).Error; err != nil {
			return err
		}
		audit.entry.TargetName = tok.Name
		audit.detail.Before = tunnelAuditState(&tok)
		if err := tx.Model(&tok).Update("ports_config", string(configBytes)).Error; err != nil {
			return err
		}
		audit.detail.After = tunnelAuditState(&tok)
		return nil
	})
	if txErr != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新 Tunnel 端口配置失败: "+txErr.Error()))
		return
	}

	// 立即同步 ACL
	if !audit.sync(a.aclSync) {
		return
	}

	logger.Infof("已成功更新 Tunnel 端口配置: id=%d, ports=%d, k8s_api=%v(%d)", id, len(bindings), req.K8sAPIEnabled, req.K8sAPIPort)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("保存成功", nil))
}

// DeleteSignalTunnel 删除/注销 Tunnel 实例
func (a *TunnelAPI) DeleteSignalTunnel(c *gin.Context) {
	audit := beginSensitiveWrite(c, "revoke_signal_tunnel", "signal_tunnel", c.Param("id"))
	if audit == nil {
		return
	}
	defer audit.finish()
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Tunnel ID"))
		return
	}

	var tok model.DeployToken
	if err := db.DB.WithContext(ctx).First(&tok, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("未找到对应的 Tunnel 实例"))
		return
	}

	// 标记为 revoked 并清理相关授权
	txErr := audit.transaction(func(tx *gorm.DB) error {
		if err := tx.First(&tok, id).Error; err != nil {
			return err
		}
		audit.entry.TargetName = tok.Name
		audit.detail.Before = tunnelAuditState(&tok)
		var grants []model.TenantAccessGrant
		if err := tx.Where("subject_user_id = ?", tok.UserID).Find(&grants).Error; err != nil {
			return err
		}
		audit.detail.Before.(map[string]interface{})["grants"] = grants
		if err := tx.Model(&tok).Update("status", model.DeployTokenStatusRevoked).Error; err != nil {
			return err
		}
		if err := tx.Where("subject_user_id = ?", tok.UserID).Delete(&model.TenantAccessGrant{}).Error; err != nil {
			return err
		}
		audit.detail.After = tunnelAuditState(&tok)
		audit.detail.After.(map[string]interface{})["grants"] = []model.TenantAccessGrant{}
		return nil
	})
	if txErr != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("注销失败: "+txErr.Error()))
		return
	}

	// 立即同步 ACL
	if !audit.sync(a.aclSync) {
		return
	}

	logger.Infof("已成功注销 Tunnel 实例: id=%d, name=%s", id, tok.Name)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("注销成功", nil))
}

// GetAvailableAgents 获取当前可绑定的边缘 Agent 列表
func (a *TunnelAPI) GetAvailableAgents(c *gin.Context) {
	ctx := c.Request.Context()
	var nodes []model.Node
	_ = db.DB.WithContext(ctx).Where("type = ?", "agent").Order("created_at DESC").Find(&nodes).Error

	now := time.Now()
	type AgentOption struct {
		Name   string `json:"name"`
		IP     string `json:"ip"`
		Online bool   `json:"online"`
		Status string `json:"status"`
	}

	var result []AgentOption
	for _, n := range nodes {
		online := n.LastHeartbeat != nil && now.Sub(nodeHeartbeat(n.LastHeartbeat)) < 60*time.Second
		status := "offline"
		if online {
			status = "online"
		}
		ip := n.IP
		if ip == "" {
			ip = "127.0.0.1"
		}
		result = append(result, AgentOption{
			Name:   n.Name,
			IP:     ip,
			Online: online,
			Status: status,
		})
	}

	if result == nil {
		result = []AgentOption{}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

func nodeHeartbeat(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
