package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

// EndpointAPI Endpoint 管理 API
type EndpointAPI struct {
	config        *config.ServerConfig
	domainService *service.DomainService
}

// NewEndpointAPI 创建 EndpointAPI
func NewEndpointAPI(cfg *config.ServerConfig) *EndpointAPI {
	return &EndpointAPI{
		config:        cfg,
		domainService: service.NewDomainService(db.DB),
	}
}

// ========== Endpoint 列表 ==========

// EndpointListItem Endpoint 列表项
type EndpointListItem struct {
	ID                string    `json:"id"`
	UserID            uint64    `json:"user_id"`
	AgentName         string    `json:"agent_name"`
	Name              string    `json:"name"`
	Alias             string    `json:"alias"`
	Version           string    `json:"version"`
	Status            string    `json:"status"`
	SSHEnabled        bool      `json:"ssh_enabled"`
	K8SAPIEnabled     bool      `json:"k8sapi_enabled"`
	K8SServiceEnabled bool      `json:"k8sservice_enabled"`
	CreatedAt         time.Time `json:"created_at"`
}

// ListEndpoints Endpoint 列表
func (a *EndpointAPI) ListEndpoints(c *gin.Context) {
	ctx := c.Request.Context()
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	search := c.Query("search")
	agentID := c.Query("agent_id")
	status := c.Query("status")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	query := db.DB.WithContext(ctx).Model(&model.Endpoint{}).Preload("User").
		Where("revoked = ?", false)
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ?", "%"+search+"%", "%"+search+"%")
	}
	if agentID != "" {
		query = query.Where("user_id = ?", agentID)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}

	var total int64
	query.Count(&total)

	var endpoints []model.Endpoint
	offset := (page - 1) * size
	if err := query.Order("created_at DESC").Offset(offset).Limit(size).Find(&endpoints).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]EndpointListItem, len(endpoints))
	for i, ep := range endpoints {
		agentName := ""
		if ep.User != nil {
			agentName = ep.User.Name
		}
		result[i] = EndpointListItem{
			ID:                ep.ID,
			UserID:            ep.UserID,
			AgentName:         agentName,
			Name:              ep.Name,
			Alias:             ep.Alias,
			Version:           ep.Version,
			Status:            ep.Status,
			SSHEnabled:        ep.SSHEnabled,
			K8SAPIEnabled:     ep.K8SAPIEnabled,
			K8SServiceEnabled: ep.K8SServiceEnabled,
			CreatedAt:         ep.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// ========== Endpoint 详情 ==========

// EndpointDetailResponse Endpoint 详情响应
type EndpointDetailResponse struct {
	ID                      string    `json:"id"`
	UserID                  uint64    `json:"user_id"`
	AgentName               string    `json:"agent_name"`
	Name                    string    `json:"name"`
	Alias                   string    `json:"alias"`
	Version                 string    `json:"version"`
	Status                  string    `json:"status"`
	SSHEnabled              bool      `json:"ssh_enabled"`
	SSHUsers                []string  `json:"ssh_users"`
	K8SAPIEnabled           bool      `json:"k8sapi_enabled"`
	K8SAPIApiServer         string    `json:"k8sapi_api_server"`
	K8SServiceEnabled       bool      `json:"k8sservice_enabled"`
	K8SServiceLabelSelector string    `json:"k8sservice_label_selector"`
	K8SServiceNamespaces    []string  `json:"k8sservice_namespaces"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

// GetEndpointDetail Endpoint 详情
func (a *EndpointAPI) GetEndpointDetail(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var ep model.Endpoint
	if err := db.DB.WithContext(ctx).Preload("User").First(&ep, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	// 添加调试日志
	logger.Infof("GetEndpointDetail: id=%s, name=%s, ssh_enabled=%v, k8sapi_enabled=%v, k8sservice_enabled=%v",
		ep.ID, ep.Name, ep.SSHEnabled, ep.K8SAPIEnabled, ep.K8SServiceEnabled)

	agentName := ""
	if ep.User != nil {
		agentName = ep.User.Name
	}

	c.JSON(http.StatusOK, NewSuccessResponse(EndpointDetailResponse{
		ID:                      ep.ID,
		UserID:                  ep.UserID,
		AgentName:               agentName,
		Name:                    ep.Name,
		Alias:                   ep.Alias,
		Version:                 ep.Version,
		Status:                  ep.Status,
		SSHEnabled:              ep.SSHEnabled,
		SSHUsers:                parseJSONStringArray(ep.SSHUsers),
		K8SAPIEnabled:           ep.K8SAPIEnabled,
		K8SAPIApiServer:         ep.K8SAPIApiServer,
		K8SServiceEnabled:       ep.K8SServiceEnabled,
		K8SServiceLabelSelector: ep.K8SServiceLabelSelector,
		K8SServiceNamespaces:    parseJSONStringArray(ep.K8SServiceNamespaces),
		CreatedAt:               ep.CreatedAt,
		UpdatedAt:               ep.UpdatedAt,
	}))
}

// ========== Endpoint 更新 ==========

// UpdateEndpointRequest 更新 Endpoint 请求
type UpdateEndpointRequest struct {
	Alias                   *string  `json:"alias"`
	SSHEnabled              *bool    `json:"ssh_enabled"`
	SSHUsers                []string `json:"ssh_users"`
	K8SAPIEnabled           *bool    `json:"k8sapi_enabled"`
	K8SAPIApiServer         *string  `json:"k8sapi_api_server"`
	K8SServiceEnabled       *bool    `json:"k8sservice_enabled"`
	K8SServiceLabelSelector *string  `json:"k8sservice_label_selector"`
	K8SServiceNamespaces    []string `json:"k8sservice_namespaces"`
}

// UpdateEndpoint 更新 Endpoint
func (a *EndpointAPI) UpdateEndpoint(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var ep model.Endpoint
	if err := db.DB.WithContext(ctx).First(&ep, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	var req UpdateEndpointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	updates := map[string]any{}
	if req.Alias != nil {
		updates["alias"] = *req.Alias
	}
	if req.SSHEnabled != nil {
		updates["ssh_enabled"] = *req.SSHEnabled
	}
	if req.SSHUsers != nil {
		updates["ssh_users"] = formatJSONStringArray(req.SSHUsers)
	}
	if req.K8SAPIEnabled != nil {
		updates["k8sapi_enabled"] = *req.K8SAPIEnabled
	}
	if req.K8SAPIApiServer != nil {
		updates["k8sapi_api_server"] = *req.K8SAPIApiServer
	}
	if req.K8SServiceEnabled != nil {
		updates["k8sservice_enabled"] = *req.K8SServiceEnabled
	}
	if req.K8SServiceLabelSelector != nil {
		updates["k8sservice_label_selector"] = *req.K8SServiceLabelSelector
	}
	if req.K8SServiceNamespaces != nil {
		updates["k8sservice_namespaces"] = formatJSONStringArray(req.K8SServiceNamespaces)
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("没有需要更新的字段"))
		return
	}

	if err := db.DB.WithContext(ctx).Model(&ep).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	// 处理 SSH 域名创建/删除
	if req.SSHEnabled != nil {
		// 查询 Agent Node 和 User 信息
		var agentNode model.Node
		var user model.User
		if err := db.DB.WithContext(ctx).First(&agentNode, "user_id = ? AND type = ?", ep.UserID, model.NodeTypeAgent).Error; err == nil {
			if err := db.DB.WithContext(ctx).First(&user, ep.UserID).Error; err == nil {
				// 重新加载 Endpoint 数据（获取最新的能力配置）
				if err := db.DB.WithContext(ctx).First(&ep, "id = ?", id).Error; err == nil {
					if *req.SSHEnabled {
						// SSH 开启 → 创建域名
						if err := a.domainService.CreateEndpointSSHDomain(ctx, &ep, &agentNode, &user); err != nil {
							logger.Errorf("创建 Endpoint SSH 域名失败: endpoint=%s, err=%v", ep.Name, err)
						}
					} else {
						// SSH 关闭 → 删除域名
						if err := a.domainService.DeleteEndpointSSHDomain(ctx, ep.Name, &user); err != nil {
							logger.Errorf("删除 Endpoint SSH 域名失败: endpoint=%s, err=%v", ep.Name, err)
						}
					}
				}
			}
		}
	}

	logger.Infof("更新 Endpoint: id=%s", id)
	recordAuditLog(ctx, c, model.ActionUpdateEndpoint, "endpoint", id, ep.Name, updates)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// ========== Endpoint 注销 ==========

// RevokeEndpoint 注销 Endpoint
func (a *EndpointAPI) RevokeEndpoint(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var ep model.Endpoint
	if err := db.DB.WithContext(ctx).First(&ep, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	if err := db.DB.WithContext(ctx).Model(&ep).Updates(map[string]any{
		"revoked": true,
		"status":  "offline",
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("注销失败"))
		return
	}

	// 删除 Endpoint 的所有域名
	if err := a.domainService.DeleteEndpointAllDomains(ctx, ep.Name); err != nil {
		logger.Errorf("删除 Endpoint 所有域名失败: endpoint=%s, err=%v", ep.Name, err)
	}

	logger.Infof("注销 Endpoint: id=%s, name=%s", id, ep.Name)
	recordAuditLog(ctx, c, model.ActionDeleteEndpoint, "endpoint", id, ep.Name, nil)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("注销成功", nil))
}
