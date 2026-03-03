package api

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// ========== K8S API 聚合查询（P9 新增） ==========

// K8SUnifiedACLListItem K8S API 授权合并列表项
type K8SUnifiedACLListItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Alias      string    `json:"alias"`
	Type       string    `json:"type"`       // "agent" 或 "endpoint"
	AgentName  string    `json:"agent_name"` // 所属 Agent 名称（endpoint 类型时有值）
	AgentID    uint64    `json:"agent_id"`   // 所属 Agent User ID（endpoint 类型时有值）
	APIServer  string    `json:"api_server"` // K8S API Server 地址（endpoint 类型时有值）
	Status     string    `json:"status"`     // online/offline（endpoint 类型时有值）
	UserCount  int64     `json:"user_count"`
	GroupCount int64     `json:"group_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListK8SUnifiedACL 获取 K8S API 授权合并列表（Agent + Endpoint）
func (a *ACLAPI) ListK8SUnifiedACL(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	search := c.Query("search")
	typeFilter := c.DefaultQuery("type", "all")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	var items []K8SUnifiedACLListItem

	// 查询 Agent K8S API 授权（type=all 或 type=agent 时）
	if typeFilter == "all" || typeFilter == "agent" {
		items = append(items, a.queryAgentK8SItems(search)...)
	}

	// 查询 Endpoint K8S API 授权（type=all 或 type=endpoint 时）
	if typeFilter == "all" || typeFilter == "endpoint" {
		items = append(items, a.queryEndpointK8SAPIItems(search)...)
	}

	// 按创建时间倒序排序
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})

	// 内存分页
	total := int64(len(items))
	offset := (page - 1) * size
	end := offset + size
	if offset > len(items) {
		offset = len(items)
	}
	if end > len(items) {
		end = len(items)
	}

	c.JSON(http.StatusOK, NewPagedResponse(items[offset:end], total, page, size))
}

// queryAgentK8SItems 查询 Agent K8S API 授权列表项
func (a *ACLAPI) queryAgentK8SItems(search string) []K8SUnifiedACLListItem {
	query := db.DB.Model(&model.User{}).Where("role = ?", model.UserRoleAgent)
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	var users []model.User
	if err := query.Order("created_at DESC").Find(&users).Error; err != nil {
		return nil
	}

	// 查询用户级授权数量
	var userCounts []struct {
		TargetUserID uint64 `gorm:"column:target_user_id"`
		Count        int64  `gorm:"column:count"`
	}
	db.DB.Model(&model.AclK8SUserPermission{}).
		Select("target_user_id, COUNT(*) as count").
		Group("target_user_id").Find(&userCounts)

	userCountMap := make(map[uint64]int64)
	for _, uc := range userCounts {
		userCountMap[uc.TargetUserID] = uc.Count
	}

	// 查询分组级授权数量
	var groupCounts []struct {
		TargetUserID uint64 `gorm:"column:target_user_id"`
		Count        int64  `gorm:"column:count"`
	}
	db.DB.Model(&model.AclK8SGroupPermission{}).
		Select("target_user_id, COUNT(*) as count").
		Group("target_user_id").Find(&groupCounts)

	groupCountMap := make(map[uint64]int64)
	for _, gc := range groupCounts {
		groupCountMap[gc.TargetUserID] = gc.Count
	}

	items := make([]K8SUnifiedACLListItem, len(users))
	for i, user := range users {
		items[i] = K8SUnifiedACLListItem{
			ID:         strconv.FormatUint(user.ID, 10),
			Name:       user.Name,
			Alias:      user.Alias,
			Type:       "agent",
			UserCount:  userCountMap[user.ID],
			GroupCount: groupCountMap[user.ID],
			CreatedAt:  user.CreatedAt,
		}
	}
	return items
}

// queryEndpointK8SAPIItems 查询 Endpoint K8S API 授权列表项
func (a *ACLAPI) queryEndpointK8SAPIItems(search string) []K8SUnifiedACLListItem {
	query := db.DB.Model(&model.Endpoint{}).Where("revoked = ? AND k8sapi_enabled = ?", false, true)
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	var endpoints []model.Endpoint
	if err := query.Preload("User").Order("created_at DESC").Find(&endpoints).Error; err != nil {
		return nil
	}

	endpointIDs := make([]string, len(endpoints))
	for i, ep := range endpoints {
		endpointIDs[i] = ep.ID
	}

	userCountMap := make(map[string]int64)
	groupCountMap := make(map[string]int64)

	if len(endpointIDs) > 0 {
		var userCounts []struct {
			EndpointID string `gorm:"column:endpoint_id"`
			Count      int64  `gorm:"column:count"`
		}
		db.DB.Model(&model.AclEndpointK8SAPIUserPermission{}).
			Select("endpoint_id, COUNT(*) as count").
			Where("endpoint_id IN ?", endpointIDs).
			Group("endpoint_id").Find(&userCounts)
		for _, uc := range userCounts {
			userCountMap[uc.EndpointID] = uc.Count
		}

		var groupCounts []struct {
			EndpointID string `gorm:"column:endpoint_id"`
			Count      int64  `gorm:"column:count"`
		}
		db.DB.Model(&model.AclEndpointK8SAPIGroupPermission{}).
			Select("endpoint_id, COUNT(*) as count").
			Where("endpoint_id IN ?", endpointIDs).
			Group("endpoint_id").Find(&groupCounts)
		for _, gc := range groupCounts {
			groupCountMap[gc.EndpointID] = gc.Count
		}
	}

	items := make([]K8SUnifiedACLListItem, len(endpoints))
	for i, ep := range endpoints {
		agentName := ""
		if ep.User != nil {
			agentName = ep.User.Name
		}
		items[i] = K8SUnifiedACLListItem{
			ID:         ep.ID,
			Name:       ep.Name,
			Alias:      ep.Alias,
			Type:       "endpoint",
			AgentName:  agentName,
			AgentID:    ep.UserID,
			APIServer:  ep.K8SAPIApiServer,
			Status:     ep.Status,
			UserCount:  userCountMap[ep.ID],
			GroupCount: groupCountMap[ep.ID],
			CreatedAt:  ep.CreatedAt,
		}
	}
	return items
}

// ========== K8S Service 聚合查询（P9 新增） ==========

// K8SServiceUnifiedACLListItem K8S Service 授权合并列表项
type K8SServiceUnifiedACLListItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Alias      string    `json:"alias"`
	Type       string    `json:"type"`       // "agent" 或 "endpoint"
	AgentName  string    `json:"agent_name"` // 所属 Agent 名称（endpoint 类型时有值）
	AgentID    uint64    `json:"agent_id"`   // 所属 Agent User ID（endpoint 类型时有值）
	Status     string    `json:"status"`     // online/offline（endpoint 类型时有值）
	UserCount  int64     `json:"user_count"`
	GroupCount int64     `json:"group_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListK8SServiceUnifiedACL 获取 K8S Service 授权合并列表（Agent + Endpoint）
func (a *ACLAPI) ListK8SServiceUnifiedACL(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	search := c.Query("search")
	typeFilter := c.DefaultQuery("type", "all")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	var items []K8SServiceUnifiedACLListItem

	// 查询 Agent K8S Service 授权（type=all 或 type=agent 时）
	if typeFilter == "all" || typeFilter == "agent" {
		items = append(items, a.queryAgentK8SServiceItems(search)...)
	}

	// 查询 Endpoint K8S Service 授权（type=all 或 type=endpoint 时）
	if typeFilter == "all" || typeFilter == "endpoint" {
		items = append(items, a.queryEndpointK8SServiceItems(search)...)
	}

	// 按创建时间倒序排序
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})

	// 内存分页
	total := int64(len(items))
	offset := (page - 1) * size
	end := offset + size
	if offset > len(items) {
		offset = len(items)
	}
	if end > len(items) {
		end = len(items)
	}

	c.JSON(http.StatusOK, NewPagedResponse(items[offset:end], total, page, size))
}

// queryAgentK8SServiceItems 查询 Agent K8S Service 授权列表项
func (a *ACLAPI) queryAgentK8SServiceItems(search string) []K8SServiceUnifiedACLListItem {
	query := db.DB.Model(&model.User{}).Where("role = ?", model.UserRoleAgent)
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	var users []model.User
	if err := query.Order("created_at DESC").Find(&users).Error; err != nil {
		return nil
	}

	// 查询用户级授权数量
	var userCounts []struct {
		TargetUserID uint64 `gorm:"column:target_user_id"`
		Count        int64  `gorm:"column:count"`
	}
	db.DB.Model(&model.AclK8SServiceUserPermission{}).
		Select("target_user_id, COUNT(*) as count").
		Group("target_user_id").Find(&userCounts)

	userCountMap := make(map[uint64]int64)
	for _, uc := range userCounts {
		userCountMap[uc.TargetUserID] = uc.Count
	}

	// 查询分组级授权数量
	var groupCounts []struct {
		TargetUserID uint64 `gorm:"column:target_user_id"`
		Count        int64  `gorm:"column:count"`
	}
	db.DB.Model(&model.AclK8SServiceGroupPermission{}).
		Select("target_user_id, COUNT(*) as count").
		Group("target_user_id").Find(&groupCounts)

	groupCountMap := make(map[uint64]int64)
	for _, gc := range groupCounts {
		groupCountMap[gc.TargetUserID] = gc.Count
	}

	items := make([]K8SServiceUnifiedACLListItem, len(users))
	for i, user := range users {
		items[i] = K8SServiceUnifiedACLListItem{
			ID:         strconv.FormatUint(user.ID, 10),
			Name:       user.Name,
			Alias:      user.Alias,
			Type:       "agent",
			UserCount:  userCountMap[user.ID],
			GroupCount: groupCountMap[user.ID],
			CreatedAt:  user.CreatedAt,
		}
	}
	return items
}

// queryEndpointK8SServiceItems 查询 Endpoint K8S Service 授权列表项
func (a *ACLAPI) queryEndpointK8SServiceItems(search string) []K8SServiceUnifiedACLListItem {
	query := db.DB.Model(&model.Endpoint{}).Where("revoked = ? AND k8sservice_enabled = ?", false, true)
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	var endpoints []model.Endpoint
	if err := query.Preload("User").Order("created_at DESC").Find(&endpoints).Error; err != nil {
		return nil
	}

	endpointIDs := make([]string, len(endpoints))
	for i, ep := range endpoints {
		endpointIDs[i] = ep.ID
	}

	userCountMap := make(map[string]int64)
	groupCountMap := make(map[string]int64)

	if len(endpointIDs) > 0 {
		var userCounts []struct {
			EndpointID string `gorm:"column:endpoint_id"`
			Count      int64  `gorm:"column:count"`
		}
		db.DB.Model(&model.AclEndpointK8SServiceUserPermission{}).
			Select("endpoint_id, COUNT(*) as count").
			Where("endpoint_id IN ?", endpointIDs).
			Group("endpoint_id").Find(&userCounts)
		for _, uc := range userCounts {
			userCountMap[uc.EndpointID] = uc.Count
		}

		var groupCounts []struct {
			EndpointID string `gorm:"column:endpoint_id"`
			Count      int64  `gorm:"column:count"`
		}
		db.DB.Model(&model.AclEndpointK8SServiceGroupPermission{}).
			Select("endpoint_id, COUNT(*) as count").
			Where("endpoint_id IN ?", endpointIDs).
			Group("endpoint_id").Find(&groupCounts)
		for _, gc := range groupCounts {
			groupCountMap[gc.EndpointID] = gc.Count
		}
	}

	items := make([]K8SServiceUnifiedACLListItem, len(endpoints))
	for i, ep := range endpoints {
		agentName := ""
		if ep.User != nil {
			agentName = ep.User.Name
		}
		items[i] = K8SServiceUnifiedACLListItem{
			ID:         ep.ID,
			Name:       ep.Name,
			Alias:      ep.Alias,
			Type:       "endpoint",
			AgentName:  agentName,
			AgentID:    ep.UserID,
			Status:     ep.Status,
			UserCount:  userCountMap[ep.ID],
			GroupCount: groupCountMap[ep.ID],
			CreatedAt:  ep.CreatedAt,
		}
	}
	return items
}
