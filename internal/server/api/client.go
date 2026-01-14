package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// ClientAPI Client 管理 API
type ClientAPI struct {
	config   *config.ServerConfig
	hsClient *headscale.Client
}

// NewClientAPI 创建 ClientAPI
func NewClientAPI(cfg *config.ServerConfig) *ClientAPI {
	api := &ClientAPI{config: cfg}
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

// ClientListItem Client 列表项
type ClientListItem struct {
	ID           uint64     `json:"id"`
	Name         string     `json:"name"`
	Alias        string     `json:"alias"`
	DesktopCount int64      `json:"desktop_count"`
	Status       string     `json:"status"`
	LastOnline   *time.Time `json:"last_online"`
}

// List 获取 Client 列表
func (a *ClientAPI) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	search := c.Query("search")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	query := db.DB.Model(&model.Client{})
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	var total int64
	query.Count(&total)

	var clients []model.Client
	offset := (page - 1) * size
	if err := query.Order("created_at DESC").Offset(offset).Limit(size).Find(&clients).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	type DesktopStats struct {
		ClientID   uint64     `gorm:"column:client_id"`
		Count      int64      `gorm:"column:count"`
		LastOnline *time.Time `gorm:"column:last_online"`
	}
	var desktopStats []DesktopStats
	db.DB.Model(&model.Desktop{}).
		Select("client_id, COUNT(*) as count, MAX(last_online) as last_online").
		Group("client_id").Find(&desktopStats)

	statsMap := make(map[uint64]DesktopStats)
	for _, ds := range desktopStats {
		statsMap[ds.ClientID] = ds
	}

	now := time.Now()
	result := make([]ClientListItem, len(clients))
	for i, client := range clients {
		stats := statsMap[client.ID]
		status := "offline"
		if stats.LastOnline != nil && now.Sub(*stats.LastOnline) < 60*time.Second {
			status = "online"
		}
		result[i] = ClientListItem{
			ID:           client.ID,
			Name:         client.Name,
			Alias:        client.Alias,
			DesktopCount: stats.Count,
			Status:       status,
			LastOnline:   stats.LastOnline,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// ClientDetail Client 详情
type ClientDetail struct {
	ID        uint64    `json:"id"`
	Name      string    `json:"name"`
	Alias     string    `json:"alias"`
	CreatedAt time.Time `json:"created_at"`
}

// Get 获取 Client 详情
func (a *ClientAPI) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var client model.Client
	if err := db.DB.First(&client, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Client 不存在"))
		return
	}

	result := ClientDetail{
		ID:        client.ID,
		Name:      client.Name,
		Alias:     client.Alias,
		CreatedAt: client.CreatedAt,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// ClientGroupItem Client 所属分组项
type ClientGroupItem struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// GetGroups 获取 Client 所属分组
func (a *ClientAPI) GetGroups(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var members []model.ClientGroupMember
	if err := db.DB.Preload("Group").Where("client_id = ?", id).Find(&members).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]ClientGroupItem, 0, len(members))
	for _, m := range members {
		if m.Group != nil {
			result = append(result, ClientGroupItem{ID: m.Group.ID, Name: m.Group.Name})
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// DesktopItem Desktop 列表项
type DesktopItem struct {
	ID         uint64     `json:"id"`
	DeviceName string     `json:"device_name"`
	TunnelIP   string     `json:"tunnel_ip"`
	Status     string     `json:"status"`
	LastOnline *time.Time `json:"last_online"`
}

// GetDesktops 获取 Client 的 Desktop 列表
func (a *ClientAPI) GetDesktops(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var desktops []model.Desktop
	if err := db.DB.Where("client_id = ?", id).Order("created_at DESC").Find(&desktops).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	now := time.Now()
	result := make([]DesktopItem, len(desktops))
	for i, d := range desktops {
		status := "offline"
		if d.LastOnline != nil && now.Sub(*d.LastOnline) < 60*time.Second {
			status = "online"
		}
		result[i] = DesktopItem{
			ID:         d.ID,
			DeviceName: d.Name,
			TunnelIP:   d.IP,
			Status:     status,
			LastOnline: d.LastOnline,
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// AuthorizedServiceItem 已授权服务项
type AuthorizedServiceItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	AgentName  string    `json:"agent_name"`
	SourceAddr string    `json:"source_addr"`
	AuthType   string    `json:"auth_type"`
	GrantedAt  time.Time `json:"granted_at"`
}

// GetServices 获取 Client 已授权服务列表
func (a *ClientAPI) GetServices(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var directPerms []model.ServiceClientPermission
	db.DB.Preload("Service").Preload("Service.Agent").Where("client_id = ?", id).Find(&directPerms)

	var groupIDs []int64
	db.DB.Model(&model.ClientGroupMember{}).Where("client_id = ?", id).Pluck("group_id", &groupIDs)

	var groupPerms []model.ServiceClientGroupPermission
	if len(groupIDs) > 0 {
		db.DB.Preload("Service").Preload("Service.Agent").Where("group_id IN ?", groupIDs).Find(&groupPerms)
	}

	serviceMap := make(map[string]AuthorizedServiceItem)
	for _, p := range directPerms {
		if p.Service != nil {
			item := AuthorizedServiceItem{
				ID: p.Service.ID, Name: p.Service.Name, SourceAddr: p.Service.SourceAddr,
				AuthType: "单独授权", GrantedAt: p.GrantedAt,
			}
			if p.Service.Agent != nil {
				item.AgentName = p.Service.Agent.Name
			}
			serviceMap[p.Service.ID] = item
		}
	}
	for _, p := range groupPerms {
		if p.Service != nil {
			if _, exists := serviceMap[p.Service.ID]; !exists {
				item := AuthorizedServiceItem{
					ID: p.Service.ID, Name: p.Service.Name, SourceAddr: p.Service.SourceAddr,
					AuthType: "分组授权", GrantedAt: p.GrantedAt,
				}
				if p.Service.Agent != nil {
					item.AgentName = p.Service.Agent.Name
				}
				serviceMap[p.Service.ID] = item
			}
		}
	}

	result := make([]AuthorizedServiceItem, 0, len(serviceMap))
	for _, item := range serviceMap {
		result = append(result, item)
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// CreateClientRequest 创建 Client 请求
type CreateClientRequest struct {
	Name  string `json:"name" binding:"required"`
	Alias string `json:"alias"`
}

// CreateClientResponse 创建 Client 响应
type CreateClientResponse struct {
	ID     uint64 `json:"id"`
	Name   string `json:"name"`
	Secret string `json:"secret"`
}

// Create 创建 Client
func (a *ClientAPI) Create(c *gin.Context) {
	var req CreateClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var existing model.Client
	if err := db.DB.Where("name = ?", req.Name).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("Client 名称已存在"))
		return
	}

	secret, err := generateSecret(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("生成密钥失败"))
		return
	}

	secretHash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("加密密钥失败"))
		return
	}

	var userID uint64
	if a.hsClient != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		// User 命名规则: desktop-{client_name}，参见 docs/design_headscale_integration.md
		userName := fmt.Sprintf("desktop-%s", req.Name)
		user, err := a.hsClient.CreateUser(ctx, userName)
		if err != nil {
			logger.Warnf("Headscale 创建用户失败: %v", err)
		} else {
			userID = user.Id
		}
	}

	client := &model.Client{Name: req.Name, Alias: req.Alias, SecretHash: string(secretHash)}
	if userID > 0 {
		client.ID = userID
	}

	if err := db.DB.Create(client).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建失败: "+err.Error()))
		return
	}

	logger.Infof("创建 Client: id=%d, name=%s", client.ID, client.Name)
	recordAuditLog(c, model.ActionCreateClient, "client", strconv.FormatUint(client.ID, 10), client.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("创建成功", CreateClientResponse{
		ID: client.ID, Name: client.Name, Secret: secret,
	}))
}

// UpdateClientRequest 更新 Client 请求
type UpdateClientRequest struct {
	Alias string `json:"alias"`
}

// Update 更新 Client
func (a *ClientAPI) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var req UpdateClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var client model.Client
	if err := db.DB.First(&client, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Client 不存在"))
		return
	}

	client.Alias = req.Alias
	if err := db.DB.Save(&client).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("更新 Client: id=%d", id)
	recordAuditLog(c, model.ActionUpdateClient, "client", strconv.FormatUint(id, 10), client.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// Delete 删除 Client
func (a *ClientAPI) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var client model.Client
	if err := db.DB.First(&client, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Client 不存在"))
		return
	}

	if a.hsClient != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		var desktops []model.Desktop
		db.DB.Where("client_id = ?", id).Find(&desktops)
		for _, d := range desktops {
			if d.ID > 0 {
				_ = a.hsClient.DeleteNode(ctx, d.ID)
			}
		}
		_ = a.hsClient.DeleteUser(ctx, client.Name)
	}

	db.DB.Where("client_id = ?", id).Delete(&model.Desktop{})
	db.DB.Where("client_id = ?", id).Delete(&model.ClientGroupMember{})
	db.DB.Where("client_id = ?", id).Delete(&model.ServiceClientPermission{})

	if err := db.DB.Delete(&model.Client{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}

	logger.Infof("删除 Client: id=%d, name=%s", id, client.Name)
	recordAuditLog(c, model.ActionDeleteClient, "client", strconv.FormatUint(id, 10), client.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}

// LogoutDesktop 注销 Desktop
func (a *ClientAPI) LogoutDesktop(c *gin.Context) {
	clientID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	desktopID, _ := strconv.ParseUint(c.Param("did"), 10, 64)

	var desktop model.Desktop
	if err := db.DB.Where("id = ? AND client_id = ?", desktopID, clientID).First(&desktop).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Desktop 不存在"))
		return
	}

	if a.hsClient != nil && desktop.ID > 0 {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		_ = a.hsClient.ExpireNode(ctx, desktop.ID)
	}

	logger.Infof("注销 Desktop: id=%d", desktopID)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("注销成功", nil))
}

// DeleteDesktop 删除 Desktop
func (a *ClientAPI) DeleteDesktop(c *gin.Context) {
	clientID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	desktopID, _ := strconv.ParseUint(c.Param("did"), 10, 64)

	var desktop model.Desktop
	if err := db.DB.Where("id = ? AND client_id = ?", desktopID, clientID).First(&desktop).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Desktop 不存在"))
		return
	}

	if a.hsClient != nil && desktop.ID > 0 {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		_ = a.hsClient.DeleteNode(ctx, desktop.ID)
	}

	if err := db.DB.Delete(&desktop).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}

	logger.Infof("删除 Desktop: id=%d", desktopID)
	recordAuditLog(c, model.ActionDeleteDesktop, "desktop", strconv.FormatUint(desktopID, 10), desktop.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}

// RegenerateSecret 重新生成 Client 密钥
func (a *ClientAPI) RegenerateSecret(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var client model.Client
	if err := db.DB.First(&client, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Client 不存在"))
		return
	}

	secret, _ := generateSecret(32)
	secretHash, _ := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)

	client.SecretHash = string(secretHash)
	if err := db.DB.Save(&client).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("重置 Client 密钥: id=%d", id)
	recordAuditLog(c, model.ActionResetClientSecret, "client", strconv.FormatUint(id, 10), client.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("密钥重置成功", map[string]string{"secret": secret}))
}
