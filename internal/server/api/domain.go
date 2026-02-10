package api

import (
	"fmt"
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
	ID          int64              `json:"id"`
	Domain      string             `json:"domain"`
	Type        model.DomainType   `json:"type"`
	UserID      uint64             `json:"user_id"`
	UserName    string             `json:"user_name"`
	NodeID      uint64             `json:"node_id,omitempty"`
	EndpointID  string             `json:"endpoint_id,omitempty"`
	TargetIP    string             `json:"target_ip,omitempty"`
	TargetPort  int                `json:"target_port,omitempty"`
	Namespace   string             `json:"namespace,omitempty"`
	ServiceName string             `json:"service_name,omitempty"`
	Status      model.DomainStatus `json:"status"`
	CreatedAt   string             `json:"created_at"`
	UpdatedAt   string             `json:"updated_at"`
}

// List 获取域名列表
func (a *DomainAPI) List(c *gin.Context) {
	ctx := c.Request.Context()
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	search := c.Query("search")
	domainType := c.Query("type")
	userID := c.Query("user_id")
	status := c.Query("status")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	query := db.DB.WithContext(ctx).Model(&model.DomainRegistry{}).Preload("User")

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
	if status != "" {
		query = query.Where("status = ?", status)
	}

	var total int64
	query.Count(&total)

	var domains []model.DomainRegistry
	offset := (page - 1) * size
	if err := query.Order("updated_at DESC").Offset(offset).Limit(size).Find(&domains).Error; err != nil {
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
			UserID:      d.UserID,
			NodeID:      d.NodeID,
			EndpointID:  d.EndpointID,
			TargetIP:    d.TargetIP,
			TargetPort:  d.TargetPort,
			Namespace:   d.Namespace,
			ServiceName: d.ServiceName,
			Status:      d.Status,
			CreatedAt:   d.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt:   d.UpdatedAt.Format("2006-01-02 15:04:05"),
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

	var records []model.DomainRegistry
	if err := db.DB.WithContext(ctx).Preload("User").
		Where("domain = ? AND status = ?", domain, model.DomainStatusOnline).
		Find(&records).Error; err != nil || len(records) == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("域名未注册或已离线"))
		return
	}

	// 返回所有在线记录，支持负载均衡
	results := make([]gin.H, 0, len(records))
	for _, record := range records {
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
		// 通过 user_id 查找该用户的 Agent 类型 Node，获取 IP
		var node model.Node
		if err := db.DB.WithContext(ctx).
			Where("user_id = ? AND type = ? AND ip != ''", d.UserID, model.NodeTypeAgent).
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
