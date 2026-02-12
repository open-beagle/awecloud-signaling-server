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
)

// EndpointAPI Endpoint 管理 API
type EndpointAPI struct {
	config *config.ServerConfig
}

// NewEndpointAPI 创建 EndpointAPI
func NewEndpointAPI(cfg *config.ServerConfig) *EndpointAPI {
	return &EndpointAPI{config: cfg}
}

// ========== 统一 Endpoint 列表 ==========

// EndpointListItem 统一 Endpoint 列表项（跨三种类型）
type EndpointListItem struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`        // ssh / k8sapi / k8sservice
	UserID     uint64   `json:"user_id"`
	AgentName  string   `json:"agent_name"`
	Name       string   `json:"name"`
	Alias      string   `json:"alias"`
	Host       string   `json:"host"`        // SSH 专有
	Port       int      `json:"port"`        // SSH 专有
	APIServer  string   `json:"api_server"`  // K8SAPI 专有
	Status     string   `json:"status"`
	Enabled    bool     `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListEndpoints 统一 Endpoint 列表（跨三种类型）
func (a *EndpointAPI) ListEndpoints(c *gin.Context) {
	ctx := c.Request.Context()
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	search := c.Query("search")
	agentID := c.Query("agent_id")
	status := c.Query("status")
	epType := c.Query("type") // ssh / k8sapi / k8sservice，空表示全部

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	var result []EndpointListItem
	var total int64

	// 根据类型筛选查询
	types := []string{"ssh", "k8sapi", "k8sservice"}
	if epType != "" {
		types = []string{epType}
	}

	for _, t := range types {
		switch t {
		case "ssh":
			var items []model.EndpointSSH
			q := db.DB.WithContext(ctx).Model(&model.EndpointSSH{}).Preload("User").Where("revoked = ?", false)
			if search != "" {
				q = q.Where("name LIKE ? OR alias LIKE ? OR host LIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%")
			}
			if agentID != "" {
				q = q.Where("user_id = ?", agentID)
			}
			if status != "" {
				q = q.Where("status = ?", status)
			}
			var cnt int64
			q.Count(&cnt)
			total += cnt
			q.Order("created_at DESC").Find(&items)
			for _, ep := range items {
				agentName := ""
				if ep.User != nil {
					agentName = ep.User.Name
				}
				result = append(result, EndpointListItem{
					ID: ep.ID, Type: "ssh", UserID: ep.UserID, AgentName: agentName,
					Name: ep.Name, Alias: ep.Alias, Host: ep.Host, Port: ep.Port,
					Status: ep.Status, Enabled: ep.Enabled, CreatedAt: ep.CreatedAt,
				})
			}
		case "k8sapi":
			var items []model.EndpointK8SAPI
			q := db.DB.WithContext(ctx).Model(&model.EndpointK8SAPI{}).Preload("User").Where("revoked = ?", false)
			if search != "" {
				q = q.Where("name LIKE ? OR alias LIKE ? OR api_server LIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%")
			}
			if agentID != "" {
				q = q.Where("user_id = ?", agentID)
			}
			if status != "" {
				q = q.Where("status = ?", status)
			}
			var cnt int64
			q.Count(&cnt)
			total += cnt
			q.Order("created_at DESC").Find(&items)
			for _, ep := range items {
				agentName := ""
				if ep.User != nil {
					agentName = ep.User.Name
				}
				result = append(result, EndpointListItem{
					ID: ep.ID, Type: "k8sapi", UserID: ep.UserID, AgentName: agentName,
					Name: ep.Name, Alias: ep.Alias, APIServer: ep.APIServer,
					Status: ep.Status, Enabled: ep.Enabled, CreatedAt: ep.CreatedAt,
				})
			}
		case "k8sservice":
			var items []model.EndpointK8SService
			q := db.DB.WithContext(ctx).Model(&model.EndpointK8SService{}).Preload("User").Where("revoked = ?", false)
			if search != "" {
				q = q.Where("name LIKE ? OR alias LIKE ?", "%"+search+"%", "%"+search+"%")
			}
			if agentID != "" {
				q = q.Where("user_id = ?", agentID)
			}
			if status != "" {
				q = q.Where("status = ?", status)
			}
			var cnt int64
			q.Count(&cnt)
			total += cnt
			q.Order("created_at DESC").Find(&items)
			for _, ep := range items {
				agentName := ""
				if ep.User != nil {
					agentName = ep.User.Name
				}
				result = append(result, EndpointListItem{
					ID: ep.ID, Type: "k8sservice", UserID: ep.UserID, AgentName: agentName,
					Name: ep.Name, Alias: ep.Alias,
					Status: ep.Status, Enabled: ep.Enabled, CreatedAt: ep.CreatedAt,
				})
			}
		}
	}

	// 内存分页（跨表查询无法在 SQL 层统一分页）
	offset := (page - 1) * size
	end := offset + size
	if offset > len(result) {
		offset = len(result)
	}
	if end > len(result) {
		end = len(result)
	}
	paged := result[offset:end]

	c.JSON(http.StatusOK, NewPagedResponse(paged, total, page, size))
}

// EndpointDetailResponse 统一 Endpoint 详情响应
type EndpointDetailResponse struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	UserID    uint64    `json:"user_id"`
	AgentName string    `json:"agent_name"`
	Name      string    `json:"name"`
	Alias     string    `json:"alias"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	SSHUsers  []string  `json:"ssh_users"`
	APIServer string    `json:"api_server"`
	Domain    string    `json:"domain"`
	Status    string    `json:"status"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GetEndpointDetail 统一 Endpoint 详情（根据 type + id）
func (a *EndpointAPI) GetEndpointDetail(c *gin.Context) {
	ctx := c.Request.Context()
	epType := c.Param("type")
	id := c.Param("id")

	switch epType {
	case "ssh":
		var ep model.EndpointSSH
		if err := db.DB.WithContext(ctx).Preload("User").First(&ep, "id = ?", id).Error; err != nil {
			c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
			return
		}
		agentName := ""
		if ep.User != nil {
			agentName = ep.User.Name
		}
		domain := ep.Name + "." + agentName + ".beagle:" + strconv.Itoa(ep.Port)
		c.JSON(http.StatusOK, NewSuccessResponse(EndpointDetailResponse{
			ID: ep.ID, Type: "ssh", UserID: ep.UserID, AgentName: agentName,
			Name: ep.Name, Alias: ep.Alias, Host: ep.Host, Port: ep.Port,
			SSHUsers: parseJSONStringArray(ep.SSHUsers), Domain: domain,
			Status: ep.Status, Enabled: ep.Enabled,
			CreatedAt: ep.CreatedAt, UpdatedAt: ep.UpdatedAt,
		}))
	case "k8sapi":
		var ep model.EndpointK8SAPI
		if err := db.DB.WithContext(ctx).Preload("User").First(&ep, "id = ?", id).Error; err != nil {
			c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
			return
		}
		agentName := ""
		if ep.User != nil {
			agentName = ep.User.Name
		}
		domain := "kubernetes." + ep.Name + "." + agentName + ".beagle:50050"
		c.JSON(http.StatusOK, NewSuccessResponse(EndpointDetailResponse{
			ID: ep.ID, Type: "k8sapi", UserID: ep.UserID, AgentName: agentName,
			Name: ep.Name, Alias: ep.Alias, APIServer: ep.APIServer, Domain: domain,
			Status: ep.Status, Enabled: ep.Enabled,
			CreatedAt: ep.CreatedAt, UpdatedAt: ep.UpdatedAt,
		}))
	case "k8sservice":
		var ep model.EndpointK8SService
		if err := db.DB.WithContext(ctx).Preload("User").First(&ep, "id = ?", id).Error; err != nil {
			c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
			return
		}
		agentName := ""
		if ep.User != nil {
			agentName = ep.User.Name
		}
		c.JSON(http.StatusOK, NewSuccessResponse(EndpointDetailResponse{
			ID: ep.ID, Type: "k8sservice", UserID: ep.UserID, AgentName: agentName,
			Name: ep.Name, Alias: ep.Alias,
			Status: ep.Status, Enabled: ep.Enabled,
			CreatedAt: ep.CreatedAt, UpdatedAt: ep.UpdatedAt,
		}))
	default:
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Endpoint 类型"))
	}
}

// UpdateEndpointByType 统一更新 Endpoint（根据 type + id）
func (a *EndpointAPI) UpdateEndpointByType(c *gin.Context) {
	epType := c.Param("type")
	switch epType {
	case "ssh":
		a.UpdateEndpointSSH(c)
	case "k8sapi":
		a.UpdateEndpointK8SAPI(c)
	case "k8sservice":
		a.UpdateEndpointK8SService(c)
	default:
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Endpoint 类型"))
	}
}

// RevokeEndpointByType 统一注销 Endpoint（根据 type + id）
func (a *EndpointAPI) RevokeEndpointByType(c *gin.Context) {
	epType := c.Param("type")
	switch epType {
	case "ssh":
		a.RevokeEndpointSSH(c)
	case "k8sapi":
		a.RevokeEndpointK8SAPI(c)
	case "k8sservice":
		a.RevokeEndpointK8SService(c)
	default:
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 Endpoint 类型"))
	}
}

// ========== SSH Endpoint ==========

// EndpointSSHListItem SSH Endpoint 列表项
type EndpointSSHListItem struct {
	ID        string    `json:"id"`
	UserID    uint64    `json:"user_id"`
	AgentName string    `json:"agent_name"`
	Name      string    `json:"name"`
	Alias     string    `json:"alias"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	SSHUsers  []string  `json:"ssh_users"`
	Domain    string    `json:"domain"`
	Status    string    `json:"status"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// ListEndpointSSH 获取 SSH Endpoint 列表
func (a *EndpointAPI) ListEndpointSSH(c *gin.Context) {
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

	query := db.DB.WithContext(ctx).Model(&model.EndpointSSH{}).Preload("User").
		Where("revoked = ?", false)
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ? OR host LIKE ?",
			"%"+search+"%", "%"+search+"%", "%"+search+"%")
	}
	if agentID != "" {
		query = query.Where("user_id = ?", agentID)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}

	var total int64
	query.Count(&total)

	var endpoints []model.EndpointSSH
	offset := (page - 1) * size
	if err := query.Order("created_at DESC").Offset(offset).Limit(size).Find(&endpoints).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]EndpointSSHListItem, len(endpoints))
	for i, ep := range endpoints {
		agentName := ""
		if ep.User != nil {
			agentName = ep.User.Name
		}
		domain := ep.Name + "." + agentName + ".beagle:" + strconv.Itoa(ep.Port)
		result[i] = EndpointSSHListItem{
			ID:        ep.ID,
			UserID:    ep.UserID,
			AgentName: agentName,
			Name:      ep.Name,
			Alias:     ep.Alias,
			Host:      ep.Host,
			Port:      ep.Port,
			SSHUsers:  parseJSONStringArray(ep.SSHUsers),
			Domain:    domain,
			Status:    ep.Status,
			Enabled:   ep.Enabled,
			CreatedAt: ep.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// GetEndpointSSH 获取 SSH Endpoint 详情
func (a *EndpointAPI) GetEndpointSSH(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var endpoint model.EndpointSSH
	if err := db.DB.WithContext(ctx).Preload("User").First(&endpoint, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	c.JSON(http.StatusOK, NewSuccessResponse(endpoint))
}

// UpdateEndpointSSHRequest 更新 SSH Endpoint 请求（仅允许修改别名和启用状态）
type UpdateEndpointSSHRequest struct {
	Alias   string `json:"alias"`
	Enabled *bool  `json:"enabled"`
}

// UpdateEndpointSSH 更新 SSH Endpoint
func (a *EndpointAPI) UpdateEndpointSSH(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var endpoint model.EndpointSSH
	if err := db.DB.WithContext(ctx).First(&endpoint, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	var req UpdateEndpointSSHRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	updates := map[string]any{}
	if req.Alias != "" {
		updates["alias"] = req.Alias
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("没有需要更新的字段"))
		return
	}

	if err := db.DB.WithContext(ctx).Model(&endpoint).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("更新 SSH Endpoint: id=%s", id)
	recordAuditLog(ctx, c, model.ActionUpdateEndpoint, "endpoint_ssh", id, endpoint.Name, updates)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// RevokeEndpointSSH 注销 SSH Endpoint
func (a *EndpointAPI) RevokeEndpointSSH(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var endpoint model.EndpointSSH
	if err := db.DB.WithContext(ctx).First(&endpoint, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	if err := db.DB.WithContext(ctx).Model(&endpoint).Updates(map[string]any{
		"revoked": true,
		"status":  "offline",
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("注销失败"))
		return
	}

	logger.Infof("注销 SSH Endpoint: id=%s, name=%s", id, endpoint.Name)
	recordAuditLog(ctx, c, model.ActionDeleteEndpoint, "endpoint_ssh", id, endpoint.Name, nil)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("注销成功", nil))
}

// ========== K8SAPI Endpoint ==========

// EndpointK8SAPIListItem K8SAPI Endpoint 列表项
type EndpointK8SAPIListItem struct {
	ID        string    `json:"id"`
	UserID    uint64    `json:"user_id"`
	AgentName string    `json:"agent_name"`
	Name      string    `json:"name"`
	Alias     string    `json:"alias"`
	APIServer string    `json:"api_server"`
	Domain    string    `json:"domain"`
	Status    string    `json:"status"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// ListEndpointK8SAPI 获取 K8SAPI Endpoint 列表
func (a *EndpointAPI) ListEndpointK8SAPI(c *gin.Context) {
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

	query := db.DB.WithContext(ctx).Model(&model.EndpointK8SAPI{}).Preload("User").
		Where("revoked = ?", false)
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ? OR api_server LIKE ?",
			"%"+search+"%", "%"+search+"%", "%"+search+"%")
	}
	if agentID != "" {
		query = query.Where("user_id = ?", agentID)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}

	var total int64
	query.Count(&total)

	var endpoints []model.EndpointK8SAPI
	offset := (page - 1) * size
	if err := query.Order("created_at DESC").Offset(offset).Limit(size).Find(&endpoints).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]EndpointK8SAPIListItem, len(endpoints))
	for i, ep := range endpoints {
		agentName := ""
		if ep.User != nil {
			agentName = ep.User.Name
		}
		domain := "kubernetes." + ep.Name + "." + agentName + ".beagle:50050"
		result[i] = EndpointK8SAPIListItem{
			ID:        ep.ID,
			UserID:    ep.UserID,
			AgentName: agentName,
			Name:      ep.Name,
			Alias:     ep.Alias,
			APIServer: ep.APIServer,
			Domain:    domain,
			Status:    ep.Status,
			Enabled:   ep.Enabled,
			CreatedAt: ep.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// GetEndpointK8SAPI 获取 K8SAPI Endpoint 详情
func (a *EndpointAPI) GetEndpointK8SAPI(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var endpoint model.EndpointK8SAPI
	if err := db.DB.WithContext(ctx).Preload("User").First(&endpoint, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	c.JSON(http.StatusOK, NewSuccessResponse(endpoint))
}

// UpdateEndpointK8SAPIRequest 更新 K8SAPI Endpoint 请求
type UpdateEndpointK8SAPIRequest struct {
	Alias   string `json:"alias"`
	Enabled *bool  `json:"enabled"`
}

// UpdateEndpointK8SAPI 更新 K8SAPI Endpoint
func (a *EndpointAPI) UpdateEndpointK8SAPI(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var endpoint model.EndpointK8SAPI
	if err := db.DB.WithContext(ctx).First(&endpoint, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	var req UpdateEndpointK8SAPIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	updates := map[string]any{}
	if req.Alias != "" {
		updates["alias"] = req.Alias
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("没有需要更新的字段"))
		return
	}

	if err := db.DB.WithContext(ctx).Model(&endpoint).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("更新 K8SAPI Endpoint: id=%s", id)
	recordAuditLog(ctx, c, model.ActionUpdateEndpoint, "endpoint_k8sapi", id, endpoint.Name, updates)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// RevokeEndpointK8SAPI 注销 K8SAPI Endpoint
func (a *EndpointAPI) RevokeEndpointK8SAPI(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var endpoint model.EndpointK8SAPI
	if err := db.DB.WithContext(ctx).First(&endpoint, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	if err := db.DB.WithContext(ctx).Model(&endpoint).Updates(map[string]any{
		"revoked": true,
		"status":  "offline",
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("注销失败"))
		return
	}

	logger.Infof("注销 K8SAPI Endpoint: id=%s, name=%s", id, endpoint.Name)
	recordAuditLog(ctx, c, model.ActionDeleteEndpoint, "endpoint_k8sapi", id, endpoint.Name, nil)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("注销成功", nil))
}

// ========== K8SService Endpoint ==========

// EndpointK8SServiceListItem K8SService Endpoint 列表项
type EndpointK8SServiceListItem struct {
	ID        string    `json:"id"`
	UserID    uint64    `json:"user_id"`
	AgentName string    `json:"agent_name"`
	Name      string    `json:"name"`
	Alias     string    `json:"alias"`
	Status    string    `json:"status"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// ListEndpointK8SService 获取 K8SService Endpoint 列表
func (a *EndpointAPI) ListEndpointK8SService(c *gin.Context) {
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

	query := db.DB.WithContext(ctx).Model(&model.EndpointK8SService{}).Preload("User").
		Where("revoked = ?", false)
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ?",
			"%"+search+"%", "%"+search+"%")
	}
	if agentID != "" {
		query = query.Where("user_id = ?", agentID)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}

	var total int64
	query.Count(&total)

	var endpoints []model.EndpointK8SService
	offset := (page - 1) * size
	if err := query.Order("created_at DESC").Offset(offset).Limit(size).Find(&endpoints).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]EndpointK8SServiceListItem, len(endpoints))
	for i, ep := range endpoints {
		agentName := ""
		if ep.User != nil {
			agentName = ep.User.Name
		}
		result[i] = EndpointK8SServiceListItem{
			ID:        ep.ID,
			UserID:    ep.UserID,
			AgentName: agentName,
			Name:      ep.Name,
			Alias:     ep.Alias,
			Status:    ep.Status,
			Enabled:   ep.Enabled,
			CreatedAt: ep.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// GetEndpointK8SService 获取 K8SService Endpoint 详情
func (a *EndpointAPI) GetEndpointK8SService(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var endpoint model.EndpointK8SService
	if err := db.DB.WithContext(ctx).Preload("User").First(&endpoint, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	c.JSON(http.StatusOK, NewSuccessResponse(endpoint))
}

// UpdateEndpointK8SServiceRequest 更新 K8SService Endpoint 请求
type UpdateEndpointK8SServiceRequest struct {
	Alias   string `json:"alias"`
	Enabled *bool  `json:"enabled"`
}

// UpdateEndpointK8SService 更新 K8SService Endpoint
func (a *EndpointAPI) UpdateEndpointK8SService(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var endpoint model.EndpointK8SService
	if err := db.DB.WithContext(ctx).First(&endpoint, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	var req UpdateEndpointK8SServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	updates := map[string]any{}
	if req.Alias != "" {
		updates["alias"] = req.Alias
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("没有需要更新的字段"))
		return
	}

	if err := db.DB.WithContext(ctx).Model(&endpoint).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("更新 K8SService Endpoint: id=%s", id)
	recordAuditLog(ctx, c, model.ActionUpdateEndpoint, "endpoint_k8sservice", id, endpoint.Name, updates)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// RevokeEndpointK8SService 注销 K8SService Endpoint
func (a *EndpointAPI) RevokeEndpointK8SService(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var endpoint model.EndpointK8SService
	if err := db.DB.WithContext(ctx).First(&endpoint, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	if err := db.DB.WithContext(ctx).Model(&endpoint).Updates(map[string]any{
		"revoked": true,
		"status":  "offline",
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("注销失败"))
		return
	}

	logger.Infof("注销 K8SService Endpoint: id=%s, name=%s", id, endpoint.Name)
	recordAuditLog(ctx, c, model.ActionDeleteEndpoint, "endpoint_k8sservice", id, endpoint.Name, nil)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("注销成功", nil))
}
