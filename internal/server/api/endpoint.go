package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

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

// ========== SSH Endpoint ==========

// EndpointSSHListItem SSH Endpoint 列表项
type EndpointSSHListItem struct {
	ID        string    `json:"id"`
	UserID    uint64    `json:"user_id"`
	UserName  string    `json:"user_name"`
	Name      string    `json:"name"`
	Alias     string    `json:"alias"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	SSHUsers  []string  `json:"ssh_users"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// ListEndpointSSH 获取 SSH Endpoint 列表
func (a *EndpointAPI) ListEndpointSSH(c *gin.Context) {
	ctx := c.Request.Context()
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	search := c.Query("search")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	query := db.DB.WithContext(ctx).Model(&model.EndpointSSH{}).Preload("User")
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ? OR host LIKE ?",
			"%"+search+"%", "%"+search+"%", "%"+search+"%")
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
		userName := ""
		if ep.User != nil {
			userName = ep.User.Name
		}
		result[i] = EndpointSSHListItem{
			ID:        ep.ID,
			UserID:    ep.UserID,
			UserName:  userName,
			Name:      ep.Name,
			Alias:     ep.Alias,
			Host:      ep.Host,
			Port:      ep.Port,
			SSHUsers:  parseJSONStringArray(ep.SSHUsers),
			Enabled:   ep.Enabled,
			CreatedAt: ep.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// CreateEndpointSSHRequest 创建 SSH Endpoint 请求
type CreateEndpointSSHRequest struct {
	UserID   uint64   `json:"user_id" binding:"required"`
	Name     string   `json:"name" binding:"required"`
	Alias    string   `json:"alias"`
	Host     string   `json:"host" binding:"required"`
	Port     int      `json:"port"`
	SSHUsers []string `json:"ssh_users"`
}

// CreateEndpointSSH 创建 SSH Endpoint
func (a *EndpointAPI) CreateEndpointSSH(c *gin.Context) {
	ctx := c.Request.Context()

	var req CreateEndpointSSHRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 验证 Agent 用户存在
	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, req.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}

	if req.Port == 0 {
		req.Port = 22
	}

	endpoint := &model.EndpointSSH{
		ID:       uuid.New().String(),
		UserID:   req.UserID,
		Name:     req.Name,
		Alias:    req.Alias,
		Host:     req.Host,
		Port:     req.Port,
		SSHUsers: formatJSONStringArray(req.SSHUsers),
		Enabled:  true,
	}

	if err := db.DB.WithContext(ctx).Create(endpoint).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建失败"))
		return
	}

	logger.Infof("创建 SSH Endpoint: id=%s, name=%s", endpoint.ID, endpoint.Name)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("创建成功", endpoint))
}

// UpdateEndpointSSHRequest 更新 SSH Endpoint 请求
type UpdateEndpointSSHRequest struct {
	Name     string   `json:"name"`
	Alias    string   `json:"alias"`
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	SSHUsers []string `json:"ssh_users"`
	Enabled  *bool    `json:"enabled"`
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

	updates := map[string]interface{}{}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Alias != "" {
		updates["alias"] = req.Alias
	}
	if req.Host != "" {
		updates["host"] = req.Host
	}
	if req.Port > 0 {
		updates["port"] = req.Port
	}
	if req.SSHUsers != nil {
		updates["ssh_users"] = formatJSONStringArray(req.SSHUsers)
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if err := db.DB.WithContext(ctx).Model(&endpoint).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("更新 SSH Endpoint: id=%s", id)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// DeleteEndpointSSH 删除 SSH Endpoint
func (a *EndpointAPI) DeleteEndpointSSH(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	result := db.DB.WithContext(ctx).Delete(&model.EndpointSSH{}, "id = ?", id)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	logger.Infof("删除 SSH Endpoint: id=%s", id)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
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

// ========== K8SAPI Endpoint ==========

// EndpointK8SAPIListItem K8SAPI Endpoint 列表项
type EndpointK8SAPIListItem struct {
	ID            string    `json:"id"`
	UserID        uint64    `json:"user_id"`
	UserName      string    `json:"user_name"`
	Name          string    `json:"name"`
	Alias         string    `json:"alias"`
	APIServer     string    `json:"api_server"`
	KubeconfigRef string    `json:"kubeconfig_ref"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
}

// ListEndpointK8SAPI 获取 K8SAPI Endpoint 列表
func (a *EndpointAPI) ListEndpointK8SAPI(c *gin.Context) {
	ctx := c.Request.Context()
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	search := c.Query("search")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	query := db.DB.WithContext(ctx).Model(&model.EndpointK8SAPI{}).Preload("User")
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ? OR api_server LIKE ?",
			"%"+search+"%", "%"+search+"%", "%"+search+"%")
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
		userName := ""
		if ep.User != nil {
			userName = ep.User.Name
		}
		result[i] = EndpointK8SAPIListItem{
			ID:            ep.ID,
			UserID:        ep.UserID,
			UserName:      userName,
			Name:          ep.Name,
			Alias:         ep.Alias,
			APIServer:     ep.APIServer,
			KubeconfigRef: ep.KubeconfigRef,
			Enabled:       ep.Enabled,
			CreatedAt:     ep.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// CreateEndpointK8SAPIRequest 创建 K8SAPI Endpoint 请求
type CreateEndpointK8SAPIRequest struct {
	UserID        uint64 `json:"user_id" binding:"required"`
	Name          string `json:"name" binding:"required"`
	Alias         string `json:"alias"`
	APIServer     string `json:"api_server" binding:"required"`
	KubeconfigRef string `json:"kubeconfig_ref"`
}

// CreateEndpointK8SAPI 创建 K8SAPI Endpoint
func (a *EndpointAPI) CreateEndpointK8SAPI(c *gin.Context) {
	ctx := c.Request.Context()

	var req CreateEndpointK8SAPIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, req.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}

	endpoint := &model.EndpointK8SAPI{
		ID:            uuid.New().String(),
		UserID:        req.UserID,
		Name:          req.Name,
		Alias:         req.Alias,
		APIServer:     req.APIServer,
		KubeconfigRef: req.KubeconfigRef,
		Enabled:       true,
	}

	if err := db.DB.WithContext(ctx).Create(endpoint).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建失败"))
		return
	}

	logger.Infof("创建 K8SAPI Endpoint: id=%s, name=%s", endpoint.ID, endpoint.Name)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("创建成功", endpoint))
}

// UpdateEndpointK8SAPIRequest 更新 K8SAPI Endpoint 请求
type UpdateEndpointK8SAPIRequest struct {
	Name          string `json:"name"`
	Alias         string `json:"alias"`
	APIServer     string `json:"api_server"`
	KubeconfigRef string `json:"kubeconfig_ref"`
	Enabled       *bool  `json:"enabled"`
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

	updates := map[string]interface{}{}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Alias != "" {
		updates["alias"] = req.Alias
	}
	if req.APIServer != "" {
		updates["api_server"] = req.APIServer
	}
	if req.KubeconfigRef != "" {
		updates["kubeconfig_ref"] = req.KubeconfigRef
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if err := db.DB.WithContext(ctx).Model(&endpoint).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("更新 K8SAPI Endpoint: id=%s", id)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// DeleteEndpointK8SAPI 删除 K8SAPI Endpoint
func (a *EndpointAPI) DeleteEndpointK8SAPI(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	result := db.DB.WithContext(ctx).Delete(&model.EndpointK8SAPI{}, "id = ?", id)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	logger.Infof("删除 K8SAPI Endpoint: id=%s", id)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
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

// ========== K8SService Endpoint ==========

// EndpointK8SServiceListItem K8SService Endpoint 列表项
type EndpointK8SServiceListItem struct {
	ID          string    `json:"id"`
	UserID      uint64    `json:"user_id"`
	UserName    string    `json:"user_name"`
	Name        string    `json:"name"`
	Alias       string    `json:"alias"`
	Namespace   string    `json:"namespace"`
	ServiceName string    `json:"service_name"`
	TargetPort  int       `json:"target_port"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
}

// ListEndpointK8SService 获取 K8SService Endpoint 列表
func (a *EndpointAPI) ListEndpointK8SService(c *gin.Context) {
	ctx := c.Request.Context()
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	search := c.Query("search")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	query := db.DB.WithContext(ctx).Model(&model.EndpointK8SService{}).Preload("User")
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ? OR service_name LIKE ? OR namespace LIKE ?",
			"%"+search+"%", "%"+search+"%", "%"+search+"%", "%"+search+"%")
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
		userName := ""
		if ep.User != nil {
			userName = ep.User.Name
		}
		result[i] = EndpointK8SServiceListItem{
			ID:          ep.ID,
			UserID:      ep.UserID,
			UserName:    userName,
			Name:        ep.Name,
			Alias:       ep.Alias,
			Namespace:   ep.Namespace,
			ServiceName: ep.ServiceName,
			TargetPort:  ep.TargetPort,
			Enabled:     ep.Enabled,
			CreatedAt:   ep.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// CreateEndpointK8SServiceRequest 创建 K8SService Endpoint 请求
type CreateEndpointK8SServiceRequest struct {
	UserID      uint64 `json:"user_id" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Alias       string `json:"alias"`
	Namespace   string `json:"namespace" binding:"required"`
	ServiceName string `json:"service_name" binding:"required"`
	TargetPort  int    `json:"target_port" binding:"required"`
}

// CreateEndpointK8SService 创建 K8SService Endpoint
func (a *EndpointAPI) CreateEndpointK8SService(c *gin.Context) {
	ctx := c.Request.Context()

	var req CreateEndpointK8SServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, req.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}

	endpoint := &model.EndpointK8SService{
		ID:          uuid.New().String(),
		UserID:      req.UserID,
		Name:        req.Name,
		Alias:       req.Alias,
		Namespace:   req.Namespace,
		ServiceName: req.ServiceName,
		TargetPort:  req.TargetPort,
		Enabled:     true,
	}

	if err := db.DB.WithContext(ctx).Create(endpoint).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建失败"))
		return
	}

	logger.Infof("创建 K8SService Endpoint: id=%s, name=%s", endpoint.ID, endpoint.Name)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("创建成功", endpoint))
}

// UpdateEndpointK8SServiceRequest 更新 K8SService Endpoint 请求
type UpdateEndpointK8SServiceRequest struct {
	Name        string `json:"name"`
	Alias       string `json:"alias"`
	Namespace   string `json:"namespace"`
	ServiceName string `json:"service_name"`
	TargetPort  int    `json:"target_port"`
	Enabled     *bool  `json:"enabled"`
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

	updates := map[string]interface{}{}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Alias != "" {
		updates["alias"] = req.Alias
	}
	if req.Namespace != "" {
		updates["namespace"] = req.Namespace
	}
	if req.ServiceName != "" {
		updates["service_name"] = req.ServiceName
	}
	if req.TargetPort > 0 {
		updates["target_port"] = req.TargetPort
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if err := db.DB.WithContext(ctx).Model(&endpoint).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("更新 K8SService Endpoint: id=%s", id)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// DeleteEndpointK8SService 删除 K8SService Endpoint
func (a *EndpointAPI) DeleteEndpointK8SService(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	result := db.DB.WithContext(ctx).Delete(&model.EndpointK8SService{}, "id = ?", id)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("Endpoint 不存在"))
		return
	}

	logger.Infof("删除 K8SService Endpoint: id=%s", id)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
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
