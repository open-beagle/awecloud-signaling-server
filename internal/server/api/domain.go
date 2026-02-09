package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// DomainAPI 域名管理 API
type DomainAPI struct{}

// NewDomainAPI 创建 DomainAPI
func NewDomainAPI() *DomainAPI {
	return &DomainAPI{}
}

// DomainListItem 域名列表项
type DomainListItem struct {
	ID           int64              `json:"id"`
	Domain       string             `json:"domain"`
	Type         model.DomainType   `json:"type"`
	AgentUserID  uint64             `json:"agent_user_id"`
	AgentName    string             `json:"agent_name"`
	EndpointID   string             `json:"endpoint_id,omitempty"`
	TargetPort   int                `json:"target_port,omitempty"`
	Namespace    string             `json:"namespace,omitempty"`
	ServiceName  string             `json:"service_name,omitempty"`
	Status       model.DomainStatus `json:"status"`
	CreatedAt    string             `json:"created_at"`
}

// List 获取域名列表
func (a *DomainAPI) List(c *gin.Context) {
	ctx := c.Request.Context()
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	search := c.Query("search")
	domainType := c.Query("type")
	agentID := c.Query("agent_id")
	status := c.Query("status")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	query := db.DB.WithContext(ctx).Model(&model.DomainRegistry{}).Preload("AgentUser")

	// 筛选条件
	if search != "" {
		query = query.Where("domain LIKE ?", "%"+search+"%")
	}
	if domainType != "" {
		query = query.Where("type = ?", domainType)
	}
	if agentID != "" {
		id, _ := strconv.ParseUint(agentID, 10, 64)
		query = query.Where("agent_user_id = ?", id)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}

	var total int64
	query.Count(&total)

	var domains []model.DomainRegistry
	offset := (page - 1) * size
	if err := query.Order("created_at DESC").Offset(offset).Limit(size).Find(&domains).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	// 组装列表
	items := make([]DomainListItem, 0, len(domains))
	for _, d := range domains {
		item := DomainListItem{
			ID:          d.ID,
			Domain:      d.Domain,
			Type:        d.Type,
			AgentUserID: d.AgentUserID,
			EndpointID:  d.EndpointID,
			TargetPort:  d.TargetPort,
			Namespace:   d.Namespace,
			ServiceName: d.ServiceName,
			Status:      d.Status,
			CreatedAt:   d.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		if d.AgentUser != nil {
			item.AgentName = d.AgentUser.Name
		}
		items = append(items, item)
	}

	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}


// Resolve DNS 域名解析（供 Desktop 查询）
func (a *DomainAPI) Resolve(c *gin.Context) {
	ctx := c.Request.Context()
	domain := c.Query("domain")
	if domain == "" {
		c.JSON(http.StatusBadRequest, NewErrorResponse("domain 参数必填"))
		return
	}

	var record model.DomainRegistry
	if err := db.DB.WithContext(ctx).Preload("AgentUser").
		Where("domain = ? AND status = ?", domain, model.DomainStatusOnline).
		First(&record).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("域名未注册或已离线"))
		return
	}

	agentName := ""
	if record.AgentUser != nil {
		agentName = record.AgentUser.Name
	}

	c.JSON(http.StatusOK, NewSuccessResponse(gin.H{
		"domain":        record.Domain,
		"type":          record.Type,
		"agent_user_id": record.AgentUserID,
		"agent_name":    agentName,
		"target_ip":     record.TargetIP,
		"target_port":   record.TargetPort,
		"endpoint_id":   record.EndpointID,
		"namespace":     record.Namespace,
		"service_name":  record.ServiceName,
	}))
}

// DomainRegisterRequest 域名注册请求
type DomainRegisterRequest struct {
	Domain      string             `json:"domain" binding:"required"`
	Type        model.DomainType   `json:"type" binding:"required"`
	AgentUserID uint64             `json:"agent_user_id" binding:"required"`
	EndpointID  string             `json:"endpoint_id,omitempty"`
	TargetIP    string             `json:"target_ip,omitempty"`
	TargetPort  int                `json:"target_port,omitempty"`
	Namespace   string             `json:"namespace,omitempty"`
	ServiceName string             `json:"service_name,omitempty"`
}

// Register 注册或更新域名（供 Agent 心跳调用，内部 API）
func (a *DomainAPI) Register(c *gin.Context) {
	ctx := c.Request.Context()

	var req DomainRegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("参数错误: "+err.Error()))
		return
	}

	// Upsert：存在则更新，不存在则创建
	var existing model.DomainRegistry
	err := db.DB.WithContext(ctx).Where("domain = ?", req.Domain).First(&existing).Error

	if err != nil {
		// 不存在，创建
		record := model.DomainRegistry{
			Domain:      req.Domain,
			Type:        req.Type,
			AgentUserID: req.AgentUserID,
			EndpointID:  req.EndpointID,
			TargetIP:    req.TargetIP,
			TargetPort:  req.TargetPort,
			Namespace:   req.Namespace,
			ServiceName: req.ServiceName,
			Status:      model.DomainStatusOnline,
		}
		if err := db.DB.WithContext(ctx).Create(&record).Error; err != nil {
			c.JSON(http.StatusInternalServerError, NewErrorResponse("注册失败"))
			return
		}
		c.JSON(http.StatusOK, NewSuccessMessageResponse("域名注册成功", gin.H{"id": record.ID}))
		return
	}

	// 存在，更新
	updates := map[string]any{
		"type":          req.Type,
		"agent_user_id": req.AgentUserID,
		"endpoint_id":   req.EndpointID,
		"target_ip":     req.TargetIP,
		"target_port":   req.TargetPort,
		"namespace":     req.Namespace,
		"service_name":  req.ServiceName,
		"status":        model.DomainStatusOnline,
	}
	if err := db.DB.WithContext(ctx).Model(&existing).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}
	c.JSON(http.StatusOK, NewSuccessMessageResponse("域名更新成功", gin.H{"id": existing.ID}))
}

// BatchRegisterRequest 批量域名注册请求
type BatchRegisterRequest struct {
	AgentUserID uint64                 `json:"agent_user_id" binding:"required"`
	Domains     []DomainRegisterRequest `json:"domains" binding:"required"`
}

// BatchRegister 批量注册域名（供 Agent 心跳批量上报）
func (a *DomainAPI) BatchRegister(c *gin.Context) {
	ctx := c.Request.Context()

	var req BatchRegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("参数错误: "+err.Error()))
		return
	}

	registered := 0
	updated := 0

	for _, d := range req.Domains {
		d.AgentUserID = req.AgentUserID

		var existing model.DomainRegistry
		err := db.DB.WithContext(ctx).Where("domain = ?", d.Domain).First(&existing).Error

		if err != nil {
			// 创建
			record := model.DomainRegistry{
				Domain:      d.Domain,
				Type:        d.Type,
				AgentUserID: d.AgentUserID,
				EndpointID:  d.EndpointID,
				TargetIP:    d.TargetIP,
				TargetPort:  d.TargetPort,
				Namespace:   d.Namespace,
				ServiceName: d.ServiceName,
				Status:      model.DomainStatusOnline,
			}
			db.DB.WithContext(ctx).Create(&record)
			registered++
		} else {
			// 更新
			db.DB.WithContext(ctx).Model(&existing).Updates(map[string]any{
				"type":          d.Type,
				"agent_user_id": d.AgentUserID,
				"endpoint_id":   d.EndpointID,
				"target_ip":     d.TargetIP,
				"target_port":   d.TargetPort,
				"namespace":     d.Namespace,
				"service_name":  d.ServiceName,
				"status":        model.DomainStatusOnline,
			})
			updated++
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(gin.H{
		"registered": registered,
		"updated":    updated,
	}))
}

// SetOffline 将 Agent 的所有域名设为离线
func (a *DomainAPI) SetOffline(c *gin.Context) {
	ctx := c.Request.Context()
	agentIDStr := c.Param("agent_id")
	agentID, err := strconv.ParseUint(agentIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 agent_id"))
		return
	}

	if err := db.DB.WithContext(ctx).Model(&model.DomainRegistry{}).
		Where("agent_user_id = ?", agentID).
		Update("status", model.DomainStatusOffline).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse("域名已设为离线", nil))
}
