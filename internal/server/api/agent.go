package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	grpcserver "github.com/open-beagle/awecloud-signaling-server/internal/server/grpc"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// AgentAPI Agent 管理 API
type AgentAPI struct {
	config       *config.ServerConfig
	hsClient     *headscale.Client
	agentService *grpcserver.AgentServiceServer
}

// NewAgentAPI 创建 AgentAPI
func NewAgentAPI(cfg *config.ServerConfig) *AgentAPI {
	api := &AgentAPI{config: cfg}

	// 初始化 Headscale 客户端
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

// SetAgentService 设置 AgentService（用于获取实时状态）
func (a *AgentAPI) SetAgentService(service *grpcserver.AgentServiceServer) {
	a.agentService = service
}

// AgentListItem Agent 列表项
type AgentListItem struct {
	ID           uint64     `json:"id"`
	Name         string     `json:"name"`
	Alias        string     `json:"alias"`
	IP           string     `json:"ip"`
	ServiceCount int64      `json:"service_count"` // 本地服务数量
	ForwardCount int64      `json:"forward_count"` // 远程服务数量
	GroupCount   int64      `json:"group_count"`
	Connections  int        `json:"connections"`
	Status       string     `json:"status"`
	Version      string     `json:"version"`
	LastOnline   *time.Time `json:"last_online"`
	SSHEnabled   bool       `json:"ssh_enabled"` // SSH 是否启用
}

// List 获取 Agent 列表
func (a *AgentAPI) List(c *gin.Context) {
	// 解析分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	search := c.Query("search")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	// 构建查询
	query := db.DB.Model(&model.Agent{})
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	// 查询总数
	var total int64
	query.Count(&total)

	// 查询列表
	var agents []model.Agent
	offset := (page - 1) * size
	if err := query.Order("created_at DESC").Offset(offset).Limit(size).Find(&agents).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	// 查询每个 Agent 的服务数量
	var serviceCounts []struct {
		AgentID uint64 `gorm:"column:agent_id"`
		Count   int64  `gorm:"column:count"`
	}
	db.DB.Model(&model.ProxyService{}).
		Select("agent_id, COUNT(*) as count").
		Group("agent_id").
		Find(&serviceCounts)

	serviceCountMap := make(map[uint64]int64)
	for _, sc := range serviceCounts {
		serviceCountMap[sc.AgentID] = sc.Count
	}

	// 查询每个 Agent 的远程服务数量
	var forwardCounts []struct {
		AgentID uint64 `gorm:"column:agent_id"`
		Count   int64  `gorm:"column:count"`
	}
	db.DB.Model(&model.PortForward{}).
		Select("agent_id, COUNT(*) as count").
		Group("agent_id").
		Find(&forwardCounts)

	forwardCountMap := make(map[uint64]int64)
	for _, fc := range forwardCounts {
		forwardCountMap[fc.AgentID] = fc.Count
	}

	// 查询每个 Agent 的分组数量
	var groupCounts []struct {
		AgentID uint64 `gorm:"column:agent_id"`
		Count   int64  `gorm:"column:count"`
	}
	db.DB.Model(&model.AgentGroupMember{}).
		Select("agent_id, COUNT(*) as count").
		Group("agent_id").
		Find(&groupCounts)

	groupCountMap := make(map[uint64]int64)
	for _, gc := range groupCounts {
		groupCountMap[gc.AgentID] = gc.Count
	}

	// 构建响应
	now := time.Now()
	result := make([]AgentListItem, len(agents))
	for i, agent := range agents {
		// 计算在线状态（60秒内有心跳认为在线）
		status := "offline"
		connections := 0
		if agent.LastHeartbeat != nil {
			if now.Sub(*agent.LastHeartbeat) < 60*time.Second {
				status = "online"
			}
		}

		// 获取连接数（从 gRPC 服务获取实时连接数）
		if a.agentService != nil && a.agentService.IsAgentOnline(agent.ID) {
			conn := a.agentService.GetAgentConnection(agent.ID)
			if conn != nil && conn.Connected {
				connections = 1 // Agent 本身的连接
			}
		}

		result[i] = AgentListItem{
			ID:           agent.ID,
			Name:         agent.Name,
			Alias:        agent.Alias,
			IP:           agent.IP,
			ServiceCount: serviceCountMap[agent.ID],
			ForwardCount: forwardCountMap[agent.ID],
			GroupCount:   groupCountMap[agent.ID],
			Connections:  connections,
			Status:       status,
			Version:      agent.Version,
			LastOnline:   agent.LastHeartbeat,
			SSHEnabled:   agent.SSHEnabled,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// AgentDetail Agent 详情
type AgentDetail struct {
	ID            uint64             `json:"id"`
	Name          string             `json:"name"`
	Alias         string             `json:"alias"`
	IP            string             `json:"ip"`
	Version       string             `json:"version"`
	CreatedAt     time.Time          `json:"created_at"`
	LastHeartbeat *time.Time         `json:"last_heartbeat"`
	Status        string             `json:"status"`
	ConnectedAt   *time.Time         `json:"connected_at"`
	Services      []AgentServiceItem `json:"services"` // 端口映射服务列表
	Forwards      []AgentForwardItem `json:"forwards"` // 端口访问服务列表
}

// AgentServiceItem Agent 服务项
type AgentServiceItem struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Alias         string `json:"alias"`
	SourceAddr    string `json:"source_addr"`
	TargetAddr    string `json:"target_addr"`
	Enabled       bool   `json:"enabled"`
	DisplayStatus string `json:"display_status"` // 合并后的显示状态
	ErrorMsg      string `json:"error_msg,omitempty"`
}

// AgentForwardItem Agent 端口访问项
type AgentForwardItem struct {
	ID                string `json:"id"`
	Name              string `json:"name"`  // 从关联服务获取
	Alias             string `json:"alias"` // 从关联服务获取
	SourceAddr        string `json:"source_addr"`
	TargetAddr        string `json:"target_addr"`
	Enabled           bool   `json:"enabled"`
	DisplayStatus     string `json:"display_status"` // 合并后的显示状态
	ErrorMsg          string `json:"error_msg,omitempty"`
	TargetAgentName   string `json:"target_agent_name"`
	TargetServiceName string `json:"target_service_name"`
}

// Get 获取 Agent 详情（静态信息）
func (a *AgentAPI) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var agent model.Agent
	if err := db.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Agent 不存在"))
		return
	}

	// 计算在线状态
	now := time.Now()
	status := "offline"
	if agent.LastHeartbeat != nil {
		if now.Sub(*agent.LastHeartbeat) < 60*time.Second {
			status = "online"
		}
	}

	// 获取内存缓存中的连接时间
	var connectedAt *time.Time
	if tsStatus := cache.GetAgentTsStatus(int64(id)); tsStatus != nil {
		connectedAt = tsStatus.TsConnectedAt
	}

	// 查询端口映射服务列表
	var services []model.ProxyService
	db.DB.Where("agent_id = ?", id).Find(&services)
	serviceItems := make([]AgentServiceItem, len(services))

	// 计算 Agent 在线状态
	agentOnline := status == "online"

	for i, svc := range services {
		// 获取运行时状态并计算显示状态
		runtimeStatus := cache.GetProxyServiceStatus(svc.ID)
		displayStatus, errorMsg := cache.GetDisplayStatus(svc.Enabled, agentOnline, runtimeStatus)

		serviceItems[i] = AgentServiceItem{
			ID:            svc.ID,
			Name:          svc.Name,
			Alias:         svc.Alias,
			SourceAddr:    svc.SourceAddr,
			TargetAddr:    svc.TargetAddr,
			Enabled:       svc.Enabled,
			DisplayStatus: displayStatus,
			ErrorMsg:      errorMsg,
		}
	}

	// 查询端口访问服务列表（PortForward）
	var forwards []model.PortForward
	db.DB.Preload("TargetService").Preload("TargetService.Agent").Where("agent_id = ?", id).Find(&forwards)

	forwardItems := make([]AgentForwardItem, len(forwards))
	for i, fwd := range forwards {
		targetAgentName := ""
		targetServiceName := ""
		name := ""
		alias := ""
		if fwd.TargetService != nil {
			name = fwd.TargetService.Name
			alias = fwd.TargetService.Alias
			targetServiceName = fwd.TargetService.Name
			if fwd.TargetService.Agent != nil {
				targetAgentName = fwd.TargetService.Agent.Name
			}
		}

		// 获取运行时状态并计算显示状态
		runtimeStatus := cache.GetPortForwardStatus(fwd.ID)
		displayStatus, errorMsg := cache.GetDisplayStatus(fwd.Enabled, agentOnline, runtimeStatus)

		forwardItems[i] = AgentForwardItem{
			ID:                fwd.ID,
			Name:              name,
			Alias:             alias,
			SourceAddr:        fwd.SourceAddr,
			TargetAddr:        fwd.TargetAddr,
			Enabled:           fwd.Enabled,
			DisplayStatus:     displayStatus,
			ErrorMsg:          errorMsg,
			TargetAgentName:   targetAgentName,
			TargetServiceName: targetServiceName,
		}
	}

	result := AgentDetail{
		ID:            agent.ID,
		Name:          agent.Name,
		Alias:         agent.Alias,
		IP:            agent.IP,
		Version:       agent.Version,
		CreatedAt:     agent.CreatedAt,
		LastHeartbeat: agent.LastHeartbeat,
		Status:        status,
		ConnectedAt:   connectedAt,
		Services:      serviceItems,
		Forwards:      forwardItems,
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// NetworkInterface 网络接口信息
type NetworkInterface struct {
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Mask    string `json:"mask"`
	Gateway string `json:"gateway"`
}

// AgentRealtimeInfo Agent 实时信息
type AgentRealtimeInfo struct {
	Hostname            string             `json:"hostname"`
	Runtime             string             `json:"runtime"`
	Networks            []NetworkInterface `json:"networks"`
	TunnelIP            string             `json:"tunnel_ip"`
	TunnelConnected     bool               `json:"tunnel_connected"`
	TunnelConnectedTime *time.Time         `json:"tunnel_connected_time"`
}

// GetRealtime 获取 Agent 实时信息
func (a *AgentAPI) GetRealtime(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	// 检查 Agent 是否存在
	var agent model.Agent
	if err := db.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Agent 不存在"))
		return
	}

	// 检查 AgentService 是否可用
	if a.agentService == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("gRPC 服务不可用"))
		return
	}

	// 检查 Agent 是否在线
	if !a.agentService.IsAgentOnline(id) {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("Agent 离线"))
		return
	}

	// 从连接信息获取实时状态
	conn := a.agentService.GetAgentConnection(id)
	if conn == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("Agent 连接不存在"))
		return
	}

	// 获取内存缓存中的连接时间
	var connectedTime *time.Time
	if tsStatus := cache.GetAgentTsStatus(int64(id)); tsStatus != nil {
		connectedTime = tsStatus.TsConnectedAt
	}

	// 解析系统信息
	var systemInfo model.SystemInfoData
	if agent.SystemInfo != "" {
		_ = json.Unmarshal([]byte(agent.SystemInfo), &systemInfo)
	}

	result := AgentRealtimeInfo{
		Hostname:            systemInfo.Hostname,
		Runtime:             "physical", // 默认值
		Networks:            []NetworkInterface{},
		TunnelIP:            conn.TunnelIP,
		TunnelConnected:     conn.Connected,
		TunnelConnectedTime: connectedTime,
	}

	// 从缓存获取网络信息
	if tsStatus := cache.GetAgentTsStatus(int64(id)); tsStatus != nil {
		if tsStatus.Hostname != "" {
			result.Hostname = tsStatus.Hostname
		}
		if tsStatus.Runtime != "" {
			result.Runtime = tsStatus.Runtime
		}
		for _, n := range tsStatus.Networks {
			result.Networks = append(result.Networks, NetworkInterface{
				Name:    n.Name,
				IP:      n.IP,
				Mask:    n.Mask,
				Gateway: n.Gateway,
			})
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// ProxyServiceItem 端口映射服务项
type ProxyServiceItem struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Alias         string `json:"alias"`
	SourceAddr    string `json:"source_addr"`
	TargetAddr    string `json:"target_addr"`
	Enabled       bool   `json:"enabled"`
	DisplayStatus string `json:"display_status"` // 合并后的显示状态
	ErrorMsg      string `json:"error_msg,omitempty"`
}

// GetServices 获取 Agent 的端口映射列表
func (a *AgentAPI) GetServices(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	// 获取 Agent 在线状态
	var agent model.Agent
	if err := db.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Agent 不存在"))
		return
	}
	agentOnline := agent.LastHeartbeat != nil && time.Since(*agent.LastHeartbeat) < 60*time.Second

	var services []model.ProxyService
	if err := db.DB.Where("agent_id = ?", id).Order("created_at DESC").Find(&services).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]ProxyServiceItem, len(services))
	for i, svc := range services {
		// 获取运行时状态并计算显示状态
		runtimeStatus := cache.GetProxyServiceStatus(svc.ID)
		displayStatus, errorMsg := cache.GetDisplayStatus(svc.Enabled, agentOnline, runtimeStatus)

		result[i] = ProxyServiceItem{
			ID:            svc.ID,
			Name:          svc.Name,
			Alias:         svc.Alias,
			SourceAddr:    svc.SourceAddr,
			TargetAddr:    svc.TargetAddr,
			Enabled:       svc.Enabled,
			DisplayStatus: displayStatus,
			ErrorMsg:      errorMsg,
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// PortForwardItem 端口访问项
type PortForwardItem struct {
	ID              string `json:"id"`
	Name            string `json:"name"`  // 从关联服务获取
	Alias           string `json:"alias"` // 从关联服务获取
	TargetAgentName string `json:"target_agent_name"`
	TargetService   string `json:"target_service"`
	SourceAddr      string `json:"source_addr"`
	TargetAddr      string `json:"target_addr"`
	Enabled         bool   `json:"enabled"`
	DisplayStatus   string `json:"display_status"` // 合并后的显示状态
	ErrorMsg        string `json:"error_msg,omitempty"`
}

// GetForwards 获取 Agent 的端口访问列表
func (a *AgentAPI) GetForwards(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	// 获取 Agent 在线状态
	var agent model.Agent
	if err := db.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Agent 不存在"))
		return
	}
	agentOnline := agent.LastHeartbeat != nil && time.Since(*agent.LastHeartbeat) < 60*time.Second

	var forwards []model.PortForward
	if err := db.DB.Preload("TargetService").Preload("TargetService.Agent").
		Where("agent_id = ?", id).Order("created_at DESC").Find(&forwards).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]PortForwardItem, len(forwards))
	for i, fwd := range forwards {
		name := ""
		alias := ""
		if fwd.TargetService != nil {
			name = fwd.TargetService.Name
			alias = fwd.TargetService.Alias
		}

		// 获取运行时状态并计算显示状态
		runtimeStatus := cache.GetPortForwardStatus(fwd.ID)
		displayStatus, errorMsg := cache.GetDisplayStatus(fwd.Enabled, agentOnline, runtimeStatus)

		item := PortForwardItem{
			ID:            fwd.ID,
			Name:          name,
			Alias:         alias,
			SourceAddr:    fwd.SourceAddr,
			TargetAddr:    fwd.TargetAddr,
			Enabled:       fwd.Enabled,
			DisplayStatus: displayStatus,
			ErrorMsg:      errorMsg,
		}
		if fwd.TargetService != nil {
			item.TargetService = fwd.TargetService.Name
			if fwd.TargetService.Agent != nil {
				item.TargetAgentName = fwd.TargetService.Agent.Name
			}
		}
		result[i] = item
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// CreateAgentRequest 创建 Agent 请求
type CreateAgentRequest struct {
	Name  string `json:"name" binding:"required"`
	Alias string `json:"alias"`
}

// CreateAgentResponse 创建 Agent 响应
type CreateAgentResponse struct {
	ID     uint64 `json:"id"`
	Name   string `json:"name"`
	Secret string `json:"secret"`
}

// Create 创建 Agent
func (a *AgentAPI) Create(c *gin.Context) {
	var req CreateAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 检查名称是否已存在
	var existing model.Agent
	if err := db.DB.Where("name = ?", req.Name).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, NewErrorResponse("Agent 名称已存在"))
		return
	}

	// 生成密钥
	secret, err := generateSecret(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("生成密钥失败"))
		return
	}

	// 哈希密钥
	secretHash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("加密密钥失败"))
		return
	}

	// 在 Headscale 创建 User
	// User 命名规则: agent-{agent_name}，参见 docs/design_headscale_integration.md
	var userID uint64
	if a.hsClient != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		userName := fmt.Sprintf("agent-%s", req.Name)
		user, err := a.hsClient.CreateUser(ctx, userName)
		if err != nil {
			logger.Warnf("Headscale 创建用户失败: %v", err)
			// 继续创建，使用自增 ID
		} else {
			userID = user.Id
		}
	}

	// 创建 Agent
	agent := &model.Agent{
		Name:       req.Name,
		Alias:      req.Alias,
		SecretHash: string(secretHash),
	}
	if userID > 0 {
		agent.ID = userID
	}

	if err := db.DB.Create(agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建失败: "+err.Error()))
		return
	}

	logger.Infof("创建 Agent: id=%d, name=%s", agent.ID, agent.Name)

	// 记录审计日志
	recordAuditLog(c, model.ActionCreateAgent, "agent", strconv.FormatUint(agent.ID, 10), agent.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("创建成功", CreateAgentResponse{
		ID:     agent.ID,
		Name:   agent.Name,
		Secret: secret, // 仅创建时返回一次
	}))
}

// UpdateAgentRequest 更新 Agent 请求
type UpdateAgentRequest struct {
	Alias string `json:"alias"`
}

// Update 更新 Agent
func (a *AgentAPI) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var req UpdateAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var agent model.Agent
	if err := db.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Agent 不存在"))
		return
	}

	// 更新字段
	agent.Alias = req.Alias

	if err := db.DB.Save(&agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("更新 Agent: id=%d", id)

	// 记录审计日志
	recordAuditLog(c, model.ActionUpdateAgent, "agent", strconv.FormatUint(id, 10), agent.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// UpdateSSHConfigRequest 更新 SSH 配置请求
type UpdateSSHConfigRequest struct {
	Enabled bool `json:"enabled"`
}

// UpdateSSHConfig 更新 Agent SSH 配置
func (a *AgentAPI) UpdateSSHConfig(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var req UpdateSSHConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var agent model.Agent
	if err := db.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Agent 不存在"))
		return
	}

	// 更新 SSH 启用状态
	oldEnabled := agent.SSHEnabled
	agent.SSHEnabled = req.Enabled

	if err := db.DB.Save(&agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("更新 Agent SSH 配置: id=%d, enabled=%v", id, req.Enabled)

	// 通知 Agent 更新 SSH 配置（通过 gRPC）
	if a.agentService != nil && a.agentService.IsAgentOnline(id) {
		// 注意：通知机制将在心跳响应中实现，这里只记录日志
		logger.Infof("Agent %d 在线，SSH 配置将在下次心跳时同步", id)
	} else {
		logger.Infof("Agent %d 离线，SSH 配置将在下次上线时同步", id)
	}

	// 记录审计日志
	detail := map[string]interface{}{
		"old_enabled": oldEnabled,
		"new_enabled": req.Enabled,
	}
	recordAuditLog(c, model.ActionUpdateAgent, "agent", strconv.FormatUint(id, 10), agent.Name, detail)

	// 根据 Agent 在线状态返回不同提示
	message := "SSH 配置更新成功"
	if a.agentService == nil || !a.agentService.IsAgentOnline(id) {
		message = "SSH 配置更新成功，Agent 离线，配置将在下次上线时生效"
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse(message, nil))
}

// Delete 删除 Agent
func (a *AgentAPI) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var agent model.Agent
	if err := db.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Agent 不存在"))
		return
	}

	// 在 Headscale 删除 Node 和 User
	if a.hsClient != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		// 删除 Node
		if agent.NodeID > 0 {
			if err := a.hsClient.DeleteNode(ctx, agent.NodeID); err != nil {
				logger.Warnf("Headscale 删除节点失败: %v", err)
			}
		}

		// 删除 User
		// User 命名规则: agent-{agent_name}，参见 docs/design_headscale_integration.md
		userName := fmt.Sprintf("agent-%s", agent.Name)
		if err := a.hsClient.DeleteUser(ctx, userName); err != nil {
			logger.Warnf("Headscale 删除用户失败: %v", err)
		}
	}

	// 删除相关的服务和权限
	db.DB.Where("agent_id = ?", id).Delete(&model.ProxyService{})
	db.DB.Where("agent_id = ?", id).Delete(&model.PortForward{})
	db.DB.Where("agent_id = ?", id).Delete(&model.AgentGroupMember{})
	db.DB.Where("agent_id = ?", id).Delete(&model.ServiceAgentPermission{})

	// 删除 Agent
	if err := db.DB.Delete(&model.Agent{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}

	logger.Infof("删除 Agent: id=%d, name=%s", id, agent.Name)

	// 记录审计日志
	recordAuditLog(c, model.ActionDeleteAgent, "agent", strconv.FormatUint(id, 10), agent.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}

// RegenerateSecret 重新生成 Agent 密钥
func (a *AgentAPI) RegenerateSecret(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var agent model.Agent
	if err := db.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Agent 不存在"))
		return
	}

	// 生成新密钥
	secret, err := generateSecret(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("生成密钥失败"))
		return
	}

	// 哈希密钥
	secretHash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("加密密钥失败"))
		return
	}

	agent.SecretHash = string(secretHash)
	if err := db.DB.Save(&agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("重置 Agent 密钥: id=%d", id)

	// 记录审计日志
	recordAuditLog(c, model.ActionResetAgentSecret, "agent", strconv.FormatUint(id, 10), agent.Name, nil)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("密钥重置成功", map[string]string{
		"secret": secret,
	}))
}

// generateSecret 生成随机密钥
func generateSecret(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// recordAuditLog 记录审计日志
func recordAuditLog(c *gin.Context, actionType, targetType, targetID, targetName string, detail interface{}) {
	userID := getAdminIDFromContext(c)

	log := &model.AuditLog{
		UserID:     userID,
		UserType:   "admin", // 目前只支持 admin，未来可扩展 desktop
		ActionType: actionType,
		TargetType: targetType,
		TargetID:   targetID,
		TargetName: targetName,
	}

	if detail != nil {
		// 序列化详情为 JSON
		// 这里简化处理，实际可以使用 json.Marshal
	}

	if err := db.DB.Create(log).Error; err != nil {
		logger.Warnf("记录审计日志失败: %v", err)
	}
}
