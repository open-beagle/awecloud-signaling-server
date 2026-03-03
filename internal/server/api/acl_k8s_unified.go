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
// 按"集群"（Agent User）维度聚合，每个集群一行
// 服务提供者从 domain_registry 表获取具体的 Node 名和 Endpoint 名

// K8SUnifiedACLClusterItem K8S API 授权集群列表项
type K8SUnifiedACLClusterItem struct {
	ID           uint64    `json:"id"`            // 集群 ID（Agent User ID）
	Name         string    `json:"name"`          // 集群名（Agent User name）
	Alias        string    `json:"alias"`         // 集群别名
	ProviderType string    `json:"provider_type"` // 提供者类型："agent" 或 "endpoint"
	ProviderID   string    `json:"provider_id"`   // 提供者 ID（Agent User ID 字符串 或 Endpoint ID）
	ProviderName string    `json:"provider_name"` // 提供者名称（如 "beagle-242" 或 "unicom-08 / beagle-002"）
	UserCount    int64     `json:"user_count"`    // 用户级授权条数
	GroupCount   int64     `json:"group_count"`   // 分组级授权条数
	CreatedAt    time.Time `json:"created_at"`    // 集群创建时间
}

// ListK8SUnifiedACL 获取 K8S API 授权合并列表（按集群聚合）
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

	clusterMap := make(map[uint64]*K8SUnifiedACLClusterItem)

	if typeFilter == "all" || typeFilter == "agent" {
		a.collectAgentK8SClusters(search, clusterMap)
	}
	if typeFilter == "all" || typeFilter == "endpoint" {
		a.collectEndpointK8SAPIClusters(search, clusterMap)
	}

	// 从 domain_registry 获取服务提供者名称
	a.fillK8SAPIProviderNames(clusterMap)

	items := make([]K8SUnifiedACLClusterItem, 0, len(clusterMap))
	for _, item := range clusterMap {
		items = append(items, *item)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})

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

// fillK8SAPIProviderNames 从 domain_registry 查询 k8sapi 类型记录，填充服务提供者名称
func (a *ACLAPI) fillK8SAPIProviderNames(clusterMap map[uint64]*K8SUnifiedACLClusterItem) {
	if len(clusterMap) == 0 {
		return
	}

	userIDs := make([]uint64, 0, len(clusterMap))
	for uid := range clusterMap {
		userIDs = append(userIDs, uid)
	}

	// 查询 domain_registry 中 type='k8sapi' 的记录，关联 Node 获取名称
	var domainRegs []model.DomainRegistry
	db.DB.Model(&model.DomainRegistry{}).
		Where("type = ? AND user_id IN ?", model.DomainTypeK8SAPI, userIDs).
		Preload("Node").
		Find(&domainRegs)

	for _, dr := range domainRegs {
		item, ok := clusterMap[dr.UserID]
		if !ok {
			continue
		}

		nodeName := ""
		if dr.Node != nil {
			nodeName = dr.Node.Name
		}

		if dr.EndpointID != "" {
			// Endpoint 提供：显示 "node_name / endpoint_name"
			if nodeName != "" {
				item.ProviderName = nodeName + " / " + dr.EndpointID
			} else {
				item.ProviderName = dr.EndpointID
			}
		} else if nodeName != "" {
			// Agent Node 直接提供：显示 node_name
			item.ProviderName = nodeName
		}
	}
}

// collectAgentK8SClusters 收集由 Agent 直接提供 K8S API 的集群
func (a *ACLAPI) collectAgentK8SClusters(search string, clusterMap map[uint64]*K8SUnifiedACLClusterItem) {
	// 查询有 K8S 用户授权记录的 Agent User ID
	var userCounts []struct {
		TargetUserID uint64 `gorm:"column:target_user_id"`
		Count        int64  `gorm:"column:count"`
	}
	db.DB.Model(&model.AclK8SUserPermission{}).
		Select("target_user_id, COUNT(*) as count").
		Group("target_user_id").Find(&userCounts)

	userCountMap := make(map[uint64]int64)
	agentUserIDs := make(map[uint64]bool)
	for _, uc := range userCounts {
		userCountMap[uc.TargetUserID] = uc.Count
		agentUserIDs[uc.TargetUserID] = true
	}

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
		agentUserIDs[gc.TargetUserID] = true
	}

	if len(agentUserIDs) == 0 {
		return
	}

	ids := make([]uint64, 0, len(agentUserIDs))
	for id := range agentUserIDs {
		ids = append(ids, id)
	}

	query := db.DB.Model(&model.User{}).Where("id IN ? AND role = ?", ids, model.UserRoleAgent)
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	var users []model.User
	if err := query.Find(&users).Error; err != nil {
		return
	}

	for _, user := range users {
		clusterMap[user.ID] = &K8SUnifiedACLClusterItem{
			ID:           user.ID,
			Name:         user.Name,
			Alias:        user.Alias,
			ProviderType: "agent",
			ProviderID:   strconv.FormatUint(user.ID, 10),
			UserCount:    userCountMap[user.ID],
			GroupCount:   groupCountMap[user.ID],
			CreatedAt:    user.CreatedAt,
		}
	}
}

// collectEndpointK8SAPIClusters 收集由 Endpoint 提供 K8S API 的集群
func (a *ACLAPI) collectEndpointK8SAPIClusters(search string, clusterMap map[uint64]*K8SUnifiedACLClusterItem) {
	var endpoints []model.Endpoint
	if err := db.DB.Model(&model.Endpoint{}).
		Where("revoked = ? AND k8sapi_enabled = ?", false, true).
		Preload("User").
		Find(&endpoints).Error; err != nil {
		return
	}

	if len(endpoints) == 0 {
		return
	}

	endpointIDs := make([]string, len(endpoints))
	for i, ep := range endpoints {
		endpointIDs[i] = ep.ID
	}

	userCountMap := make(map[string]int64)
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

	groupCountMap := make(map[string]int64)
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

	for _, ep := range endpoints {
		if ep.User == nil {
			continue
		}
		user := ep.User

		if search != "" {
			if !containsIgnoreCase(user.Name, search) && !containsIgnoreCase(user.Alias, search) {
				continue
			}
		}

		if _, exists := clusterMap[user.ID]; exists {
			continue
		}

		clusterMap[user.ID] = &K8SUnifiedACLClusterItem{
			ID:           user.ID,
			Name:         user.Name,
			Alias:        user.Alias,
			ProviderType: "endpoint",
			ProviderID:   ep.ID,
			ProviderName: ep.Name, // 会被 fillK8SAPIProviderNames 覆盖为 "node / endpoint"
			UserCount:    userCountMap[ep.ID],
			GroupCount:   groupCountMap[ep.ID],
			CreatedAt:    user.CreatedAt,
		}
	}
}

// ========== K8S Service 聚合查询（P9 新增） ==========

// K8SServiceUnifiedACLClusterItem K8S Service 授权集群列表项
type K8SServiceUnifiedACLClusterItem struct {
	ID           uint64    `json:"id"`
	Name         string    `json:"name"`
	Alias        string    `json:"alias"`
	ProviderType string    `json:"provider_type"`
	ProviderID   string    `json:"provider_id"`
	ProviderName string    `json:"provider_name"`
	UserCount    int64     `json:"user_count"`
	GroupCount   int64     `json:"group_count"`
	CreatedAt    time.Time `json:"created_at"`
}

// ListK8SServiceUnifiedACL 获取 K8S Service 授权合并列表（按集群聚合）
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

	clusterMap := make(map[uint64]*K8SServiceUnifiedACLClusterItem)

	if typeFilter == "all" || typeFilter == "agent" {
		a.collectAgentK8SServiceClusters(search, clusterMap)
	}
	if typeFilter == "all" || typeFilter == "endpoint" {
		a.collectEndpointK8SServiceClusters(search, clusterMap)
	}

	// 从 domain_registry 获取服务提供者名称
	a.fillK8SServiceProviderNames(clusterMap)

	items := make([]K8SServiceUnifiedACLClusterItem, 0, len(clusterMap))
	for _, item := range clusterMap {
		items = append(items, *item)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})

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

// fillK8SServiceProviderNames 从 domain_registry 查询 k8ssvc 类型记录，填充服务提供者名称
func (a *ACLAPI) fillK8SServiceProviderNames(clusterMap map[uint64]*K8SServiceUnifiedACLClusterItem) {
	if len(clusterMap) == 0 {
		return
	}

	userIDs := make([]uint64, 0, len(clusterMap))
	for uid := range clusterMap {
		userIDs = append(userIDs, uid)
	}

	// k8ssvc 可能有多条记录（每个 service 一条），取第一条即可获取 Node/Endpoint 信息
	var domainRegs []model.DomainRegistry
	db.DB.Model(&model.DomainRegistry{}).
		Where("type = ? AND user_id IN ?", model.DomainTypeK8SSVC, userIDs).
		Preload("Node").
		Find(&domainRegs)

	// 按 user_id 去重，只取第一条
	seen := make(map[uint64]bool)
	for _, dr := range domainRegs {
		if seen[dr.UserID] {
			continue
		}
		seen[dr.UserID] = true

		item, ok := clusterMap[dr.UserID]
		if !ok {
			continue
		}

		nodeName := ""
		if dr.Node != nil {
			nodeName = dr.Node.Name
		}

		if dr.EndpointID != "" {
			if nodeName != "" {
				item.ProviderName = nodeName + " / " + dr.EndpointID
			} else {
				item.ProviderName = dr.EndpointID
			}
		} else if nodeName != "" {
			item.ProviderName = nodeName
		}
	}
}

// collectAgentK8SServiceClusters 收集由 Agent 直接提供 K8S Service 的集群
func (a *ACLAPI) collectAgentK8SServiceClusters(search string, clusterMap map[uint64]*K8SServiceUnifiedACLClusterItem) {
	var userCounts []struct {
		TargetUserID uint64 `gorm:"column:target_user_id"`
		Count        int64  `gorm:"column:count"`
	}
	db.DB.Model(&model.AclK8SServiceUserPermission{}).
		Select("target_user_id, COUNT(*) as count").
		Group("target_user_id").Find(&userCounts)

	userCountMap := make(map[uint64]int64)
	agentUserIDs := make(map[uint64]bool)
	for _, uc := range userCounts {
		userCountMap[uc.TargetUserID] = uc.Count
		agentUserIDs[uc.TargetUserID] = true
	}

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
		agentUserIDs[gc.TargetUserID] = true
	}

	if len(agentUserIDs) == 0 {
		return
	}

	ids := make([]uint64, 0, len(agentUserIDs))
	for id := range agentUserIDs {
		ids = append(ids, id)
	}

	query := db.DB.Model(&model.User{}).Where("id IN ? AND role = ?", ids, model.UserRoleAgent)
	if search != "" {
		query = query.Where("name LIKE ? OR alias LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	var users []model.User
	if err := query.Find(&users).Error; err != nil {
		return
	}

	for _, user := range users {
		clusterMap[user.ID] = &K8SServiceUnifiedACLClusterItem{
			ID:           user.ID,
			Name:         user.Name,
			Alias:        user.Alias,
			ProviderType: "agent",
			ProviderID:   strconv.FormatUint(user.ID, 10),
			UserCount:    userCountMap[user.ID],
			GroupCount:   groupCountMap[user.ID],
			CreatedAt:    user.CreatedAt,
		}
	}
}

// collectEndpointK8SServiceClusters 收集由 Endpoint 提供 K8S Service 的集群
func (a *ACLAPI) collectEndpointK8SServiceClusters(search string, clusterMap map[uint64]*K8SServiceUnifiedACLClusterItem) {
	var endpoints []model.Endpoint
	if err := db.DB.Model(&model.Endpoint{}).
		Where("revoked = ? AND k8sservice_enabled = ?", false, true).
		Preload("User").
		Find(&endpoints).Error; err != nil {
		return
	}

	if len(endpoints) == 0 {
		return
	}

	endpointIDs := make([]string, len(endpoints))
	for i, ep := range endpoints {
		endpointIDs[i] = ep.ID
	}

	userCountMap := make(map[string]int64)
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

	groupCountMap := make(map[string]int64)
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

	for _, ep := range endpoints {
		if ep.User == nil {
			continue
		}
		user := ep.User

		if search != "" {
			if !containsIgnoreCase(user.Name, search) && !containsIgnoreCase(user.Alias, search) {
				continue
			}
		}

		if _, exists := clusterMap[user.ID]; exists {
			continue
		}

		clusterMap[user.ID] = &K8SServiceUnifiedACLClusterItem{
			ID:           user.ID,
			Name:         user.Name,
			Alias:        user.Alias,
			ProviderType: "endpoint",
			ProviderID:   ep.ID,
			ProviderName: ep.Name,
			UserCount:    userCountMap[ep.ID],
			GroupCount:   groupCountMap[ep.ID],
			CreatedAt:    user.CreatedAt,
		}
	}
}

// containsIgnoreCase 不区分大小写的字符串包含检查
func containsIgnoreCase(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			sc := s[i+j]
			tc := substr[j]
			if sc >= 'A' && sc <= 'Z' {
				sc += 32
			}
			if tc >= 'A' && tc <= 'Z' {
				tc += 32
			}
			if sc != tc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
