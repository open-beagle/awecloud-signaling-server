package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// TunnelAPI 隧道管理 API
type TunnelAPI struct {
	config   *config.ServerConfig
	hsClient *headscale.Client
	aclSync  *headscale.ACLSyncService
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
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	node, err := a.hsClient.GetNode(ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("获取 Node 失败"))
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

		// 判断关联类型
		if strings.HasPrefix(node.User.Name, "agent-") {
			localUserName := strings.TrimPrefix(node.User.Name, "agent-")
			var localNode model.Node
			if err := db.DB.WithContext(ctx).Joins("JOIN user ON user.id = node.user_id").
				Where("user.name = ? AND user.role = ? AND node.id = ?", localUserName, model.UserRoleAgent, id).
				First(&localNode).Error; err == nil {
				detail.LinkedType = "agent"
				detail.LinkedID = localNode.ID
			}
		} else if strings.HasPrefix(node.User.Name, "desktop-") {
			var localNode model.Node
			if err := db.DB.WithContext(ctx).Where("id = ? AND type = ?", id, model.NodeTypeDesktop).First(&localNode).Error; err == nil {
				detail.LinkedType = "desktop"
				detail.LinkedID = localNode.ID
			}
		}
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

	// 获取 Node 信息
	node, err := a.hsClient.GetNode(ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("获取 Node 失败"))
		return
	}

	// 删除 Node
	if err := a.hsClient.DeleteNode(ctx, id); err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除 Node 失败: "+err.Error()))
		return
	}

	// 同步删除本地关联实体
	if node.User != nil {
		if strings.HasPrefix(node.User.Name, "agent-") {
			// 清空 Agent Node 的 ip
			localUserName := strings.TrimPrefix(node.User.Name, "agent-")
			db.DB.WithContext(ctx).Model(&model.Node{}).
				Joins("JOIN user ON user.id = node.user_id").
				Where("user.name = ? AND user.role = ? AND node.id = ?", localUserName, model.UserRoleAgent, id).
				Updates(map[string]interface{}{"ip": ""})
			logger.Infof("清空 Agent Node IP: user=%s, node_id=%d", localUserName, id)
		} else if strings.HasPrefix(node.User.Name, "desktop-") {
			// 删除 Desktop Node 记录
			db.DB.WithContext(ctx).Where("id = ? AND type = ?", id, model.NodeTypeDesktop).Delete(&model.Node{})
			logger.Infof("删除 Desktop Node: node_id=%d", id)
		}
	}

	nodeName := node.GivenName
	if nodeName == "" {
		nodeName = node.Name
	}

	logger.Infof("删除隧道 Node: id=%d, name=%s", id, nodeName)
	recordAuditLog(ctx, c, model.ActionDeleteTunnelNode, "tunnel_node", strconv.FormatUint(id, 10), nodeName, nil)

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
	if a.hsClient == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("Headscale 未配置"))
		return
	}

	var req UpdateTunnelACLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 验证 JSON 格式
	var jsonCheck interface{}
	if err := json.Unmarshal([]byte(req.Policy), &jsonCheck); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 JSON 格式: "+err.Error()))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	if err := a.hsClient.SetPolicy(ctx, req.Policy); err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新 ACL Policy 失败: "+err.Error()))
		return
	}

	// 记录同步时间
	now := time.Now()
	db.DB.WithContext(ctx).Where("key = ?", "acl_last_synced_at").Assign(model.SystemConfig{
		Key:   "acl_last_synced_at",
		Value: now.Format(time.RFC3339),
	}).FirstOrCreate(&model.SystemConfig{})

	logger.Infof("更新隧道 ACL Policy")
	recordAuditLog(ctx, c, model.ActionUpdateTunnelACL, "tunnel_acl", "", "", nil)

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
	ctx := c.Request.Context()
	if a.aclSync == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("ACL 同步服务未配置"))
		return
	}

	if err := a.aclSync.SyncACL(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("同步 ACL 失败: "+err.Error()))
		return
	}

	// 记录同步时间
	now := time.Now()
	db.DB.WithContext(ctx).Where("key = ?", "acl_last_synced_at").Assign(model.SystemConfig{
		Key:   "acl_last_synced_at",
		Value: now.Format(time.RFC3339),
	}).FirstOrCreate(&model.SystemConfig{})

	logger.Infof("强制同步隧道 ACL")
	recordAuditLog(ctx, c, model.ActionSyncTunnelACL, "tunnel_acl", "", "", nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("同步成功", nil))
}
