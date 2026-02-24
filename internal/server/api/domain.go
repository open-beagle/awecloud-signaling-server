package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

// DomainAPI 域名管理 API
type DomainAPI struct {
	statusService *service.DomainStatusService
}

// NewDomainAPI 创建 DomainAPI
func NewDomainAPI(headscaleClient *headscale.Client) *DomainAPI {
	return &DomainAPI{
		statusService: service.NewDomainStatusService(headscaleClient),
	}
}

// DomainListItem 域名列表项
type DomainListItem struct {
	ID           int64              `json:"id"`
	Domain       string             `json:"domain"`
	Type         model.DomainType   `json:"type"`
	UserID       uint64             `json:"user_id"`
	UserName     string             `json:"user_name"`
	NodeID       uint64             `json:"node_id,omitempty"`
	NodeName     string             `json:"node_name,omitempty"`
	DeviceName   string             `json:"device_name,omitempty"` // 设备名（Node.Hostname）
	EndpointID   string             `json:"endpoint_id,omitempty"`
	EndpointName string             `json:"endpoint_name,omitempty"`
	TargetIP     string             `json:"target_ip,omitempty"`
	TargetPort   int                `json:"target_port,omitempty"`
	Namespace    string             `json:"namespace,omitempty"`
	ServiceName  string             `json:"service_name,omitempty"`
	Status       model.DomainStatus `json:"status"`
	CreatedAt    string             `json:"created_at"`
	UpdatedAt    string             `json:"updated_at"`
}

// List 获取域名列表
func (a *DomainAPI) List(c *gin.Context) {
	ctx := c.Request.Context()
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	search := c.Query("search")
	domainType := c.Query("type")
	userID := c.Query("user_id")
	statusFilter := c.Query("status")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	query := db.DB.WithContext(ctx).Model(&model.DomainRegistry{}).
		Preload("User").
		Preload("Node").
		Preload("Endpoint")

	// 筛选条件
	if search != "" {
		query = query.Where("domain LIKE ?", "%"+search+"%")
	}
	if domainType != "" {
		query = query.Where("type = ?", domainType)
	}
	if userID != "" {
		id, _ := strconv.ParseUint(userID, 10, 64)
		query = query.Where("user_id = ?", id)
	}

	var total int64
	query.Count(&total)

	var domains []model.DomainRegistry
	offset := (page - 1) * size
	if err := query.Order("updated_at DESC").Offset(offset).Limit(size).Find(&domains).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	// 组装列表，使用状态服务动态判断状态
	items := make([]DomainListItem, 0, len(domains))
	for _, d := range domains {
		// 使用状态服务判断域名状态
		status := a.statusService.GetDomainStatus(ctx, &d)

		// 状态筛选
		if statusFilter != "" && string(status) != statusFilter {
			continue
		}

		// 获取 Node 名称和设备名
		nodeName := ""
		deviceName := ""
		if d.Node != nil {
			nodeName = d.Node.Name
			deviceName = d.Node.Hostname // 设备名来自 Hostname
		}

		// 获取 Endpoint 名称
		endpointName := ""
		if d.Endpoint != nil {
			endpointName = d.Endpoint.Name
		} else if d.EndpointID != "" {
			// Endpoint 可能未预加载，使用 endpoint_id
			endpointName = d.EndpointID
		}

		item := DomainListItem{
			ID:           d.ID,
			Domain:       d.Domain,
			Type:         d.Type,
			UserID:       d.UserID,
			NodeID:       d.NodeID,
			EndpointID:   d.EndpointID,
			TargetIP:     d.TargetIP,
			TargetPort:   d.TargetPort,
			Namespace:    d.Namespace,
			ServiceName:  d.ServiceName,
			Status:       status,
			NodeName:     nodeName,
			DeviceName:   deviceName,
			EndpointName: endpointName,
			CreatedAt:    d.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt:    d.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
		if d.User != nil {
			item.UserName = d.User.Name
		}
		items = append(items, item)
	}

	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}

// Resolve DNS 域名解析（供 Desktop 查询）
// 同一域名可能有多条记录（负载均衡），返回所有在线记录
func (a *DomainAPI) Resolve(c *gin.Context) {
	ctx := c.Request.Context()
	domain := c.Query("domain")
	if domain == "" {
		c.JSON(http.StatusBadRequest, NewErrorResponse("domain 参数必填"))
		return
	}

	// 查询所有匹配的域名记录
	var records []model.DomainRegistry
	if err := db.DB.WithContext(ctx).Preload("User").
		Where("domain = ?", domain).
		Find(&records).Error; err != nil || len(records) == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("域名未注册"))
		return
	}

	// 使用状态服务判断在线状态，只返回在线记录
	results := make([]gin.H, 0, len(records))
	for _, record := range records {
		// 使用状态服务判断域名状态
		status := a.statusService.GetDomainStatus(ctx, &record)

		// 只返回在线记录
		if status != model.DomainStatusOnline {
			continue
		}

		userName := ""
		if record.User != nil {
			userName = record.User.Name
		}
		results = append(results, gin.H{
			"domain":       record.Domain,
			"type":         record.Type,
			"user_id":      record.UserID,
			"user_name":    userName,
			"node_id":      record.NodeID,
			"target_ip":    record.TargetIP,
			"target_port":  record.TargetPort,
			"endpoint_id":  record.EndpointID,
			"namespace":    record.Namespace,
			"service_name": record.ServiceName,
		})
	}

	// 如果没有在线记录，返回 404
	if len(results) == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("域名已离线"))
		return
	}

	c.JSON(http.StatusOK, NewSuccessResponse(results))
}

// Delete 删除域名记录
func (a *DomainAPI) Delete(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	if err := db.DB.WithContext(ctx).Delete(&model.DomainRegistry{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}

// Refresh 刷新域名记录的 target_ip
// 从 Node 表中查找对应用户的 Agent 节点 IP，回填到域名记录
func (a *DomainAPI) Refresh(c *gin.Context) {
	ctx := c.Request.Context()

	// 查询所有 target_ip 为空的在线域名记录
	var domains []model.DomainRegistry
	if err := db.DB.WithContext(ctx).
		Where("target_ip = '' OR target_ip IS NULL").
		Find(&domains).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询域名记录失败"))
		return
	}

	updated := 0
	for _, d := range domains {
		// 通过 user_id 查找该用户的 Node（Agent 或 Desktop），获取 IP
		var node model.Node
		if err := db.DB.WithContext(ctx).
			Where("user_id = ? AND ip != ''", d.UserID).
			First(&node).Error; err != nil {
			continue
		}

		if err := db.DB.WithContext(ctx).Model(&d).Update("target_ip", node.IP).Error; err != nil {
			continue
		}
		updated++
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse(fmt.Sprintf("已更新 %d 条域名记录", updated), nil))
}

// SetOffline 将指定用户的所有域名设为离线
func (a *DomainAPI) SetOffline(c *gin.Context) {
	ctx := c.Request.Context()
	userIDStr := c.Param("user_id")
	uid, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 user_id"))
		return
	}

	if err := db.DB.WithContext(ctx).Model(&model.DomainRegistry{}).
		Where("user_id = ?", uid).
		Update("status", model.DomainStatusOffline).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse("域名已设为离线", nil))
}
