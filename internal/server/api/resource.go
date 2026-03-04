package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// ImmediateReportNotifier 立即上报通知接口
type ImmediateReportNotifier interface {
	SetRequestImmediateReport()
}

// ResourceAPI 资源发现 API
type ResourceAPI struct {
	config   *config.ServerConfig
	notifier ImmediateReportNotifier
}

// NewResourceAPI 创建资源发现 API
func NewResourceAPI(cfg *config.ServerConfig) *ResourceAPI {
	return &ResourceAPI{config: cfg}
}

// SetImmediateReportNotifier 设置立即上报通知器
func (a *ResourceAPI) SetImmediateReportNotifier(n ImmediateReportNotifier) {
	a.notifier = n
}

// SyncK8SServiceDiscovery 触发 Agent 立即上报 K8S Service 发现数据
// POST /api/v1/admin/resources/sync
func (a *ResourceAPI) SyncK8SServiceDiscovery(c *gin.Context) {
	if a.notifier == nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("通知器未初始化"))
		return
	}
	a.notifier.SetRequestImmediateReport()
	c.JSON(http.StatusOK, NewSuccessResponse(nil))
}

// SSHResource SSH 资源
type SSHResource struct {
	AgentID   uint64   `json:"agent_id"`
	AgentName string   `json:"agent_name"`
	Domain    string   `json:"domain"`
	SSHUsers  []string `json:"ssh_users"`
}

// K8SAPIResource K8S API 资源
type K8SAPIResource struct {
	AgentID    uint64   `json:"agent_id"`
	AgentName  string   `json:"agent_name"`
	Domain     string   `json:"domain"`
	K8SGroups  []string `json:"k8s_groups"`
	Namespaces []string `json:"namespaces"`
}

// K8SServiceResource K8S Service 资源
type K8SServiceResource struct {
	AgentID     uint64 `json:"agent_id"`
	AgentName   string `json:"agent_name"`
	Namespace   string `json:"namespace"`
	ServiceName string `json:"service_name"`
	Domain      string `json:"domain"`
	Port        int32  `json:"port"`
}

// ResourcesResponse 资源发现响应
type ResourcesResponse struct {
	SSH        []SSHResource        `json:"ssh"`
	K8SAPI     []K8SAPIResource     `json:"k8s_api"`
	K8SService []K8SServiceResource `json:"k8s_service"`
}

// GetResources 查询当前用户可访问的资源列表
// GET /api/v1/client/resources
func (a *ResourceAPI) GetResources(c *gin.Context) {
	ctx := c.Request.Context()

	// 从 JWT 中提取 client_id（由 ClientAuthMiddleware 设置）
	clientIDVal, exists := c.Get("client_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, NewErrorResponse("未认证"))
		return
	}
	clientID := uint64(clientIDVal.(float64))

	// 查询用户所属分组
	var groupIDs []int64
	var groupMembers []model.GroupMember
	if err := db.DB.WithContext(ctx).Where("user_id = ?", clientID).Find(&groupMembers).Error; err == nil {
		for _, gm := range groupMembers {
			groupIDs = append(groupIDs, gm.GroupID)
		}
	}

	result := ResourcesResponse{
		SSH:        make([]SSHResource, 0),
		K8SAPI:     make([]K8SAPIResource, 0),
		K8SService: make([]K8SServiceResource, 0),
	}

	// 1. 查询 SSH 资源
	result.SSH = a.querySSHResources(ctx, clientID, groupIDs)

	// 2. 查询 K8S API 资源
	result.K8SAPI = a.queryK8SAPIResources(ctx, clientID, groupIDs)

	// 3. 查询 K8S Service 资源
	result.K8SService = a.queryK8SServiceResources(ctx, clientID, groupIDs)

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

// querySSHResources 查询用户可访问的 SSH 资源
func (a *ResourceAPI) querySSHResources(ctx interface{}, clientID uint64, groupIDs []int64) []SSHResource {
	var resources []SSHResource
	// 用户名缓存（target_user_id -> user）
	userCache := make(map[uint64]*model.User)

	// 直接用户授权
	var userPerms []model.AclSSHUserPermission
	db.DB.Preload("TargetUser").Where("user_id = ? AND enabled = ?", clientID, true).Find(&userPerms)

	for _, p := range userPerms {
		if p.TargetUser == nil {
			continue
		}
		userCache[p.TargetUserID] = p.TargetUser
		// 查询域名
		domain := a.findDomain(p.TargetUserID, model.DomainTypeSSH)
		resources = append(resources, SSHResource{
			AgentID:   p.TargetUserID,
			AgentName: p.TargetUser.Name,
			Domain:    domain,
			SSHUsers:  parseJSONStringArray(p.SSHUsers),
		})
	}

	// 分组授权
	if len(groupIDs) > 0 {
		var groupPerms []model.AclSSHGroupPermission
		db.DB.Preload("TargetUser").Where("group_id IN ? AND enabled = ?", groupIDs, true).Find(&groupPerms)

		for _, p := range groupPerms {
			if p.TargetUser == nil {
				continue
			}
			// 去重：如果已有直接授权，跳过
			if _, exists := userCache[p.TargetUserID]; exists {
				continue
			}
			userCache[p.TargetUserID] = p.TargetUser
			domain := a.findDomain(p.TargetUserID, model.DomainTypeSSH)
			resources = append(resources, SSHResource{
				AgentID:   p.TargetUserID,
				AgentName: p.TargetUser.Name,
				Domain:    domain,
				SSHUsers:  parseJSONStringArray(p.SSHUsers),
			})
		}
	}

	return resources
}

// queryK8SAPIResources 查询用户可访问的 K8S API 资源
func (a *ResourceAPI) queryK8SAPIResources(ctx interface{}, clientID uint64, groupIDs []int64) []K8SAPIResource {
	var resources []K8SAPIResource
	userCache := make(map[uint64]*model.User)

	// 直接用户授权
	var userPerms []model.AclK8SUserPermission
	db.DB.Preload("TargetUser").Where("user_id = ? AND enabled = ?", clientID, true).Find(&userPerms)

	for _, p := range userPerms {
		if p.TargetUser == nil {
			continue
		}
		userCache[p.TargetUserID] = p.TargetUser
		domain := a.findDomain(p.TargetUserID, model.DomainTypeK8SAPI)
		resources = append(resources, K8SAPIResource{
			AgentID:    p.TargetUserID,
			AgentName:  p.TargetUser.Name,
			Domain:     domain,
			K8SGroups:  parseJSONStringArray(p.K8SGroups),
			Namespaces: parseJSONStringArray(p.Namespaces),
		})
	}

	// 分组授权
	if len(groupIDs) > 0 {
		var groupPerms []model.AclK8SGroupPermission
		db.DB.Preload("TargetUser").Where("group_id IN ? AND enabled = ?", groupIDs, true).Find(&groupPerms)

		for _, p := range groupPerms {
			if p.TargetUser == nil {
				continue
			}
			if _, exists := userCache[p.TargetUserID]; exists {
				continue
			}
			userCache[p.TargetUserID] = p.TargetUser
			domain := a.findDomain(p.TargetUserID, model.DomainTypeK8SAPI)
			resources = append(resources, K8SAPIResource{
				AgentID:    p.TargetUserID,
				AgentName:  p.TargetUser.Name,
				Domain:     domain,
				K8SGroups:  parseJSONStringArray(p.K8SGroups),
				Namespaces: parseJSONStringArray(p.Namespaces),
			})
		}
	}

	return resources
}

// queryK8SServiceResources 查询用户可访问的 K8S Service 资源
func (a *ResourceAPI) queryK8SServiceResources(ctx interface{}, clientID uint64, groupIDs []int64) []K8SServiceResource {
	var resources []K8SServiceResource

	// 收集用户有权限的 Agent ID 列表
	agentIDs := make(map[uint64]*model.User)

	// 直接用户授权
	var userPerms []model.AclK8SServiceUserPermission
	db.DB.Preload("TargetUser").Where("user_id = ? AND enabled = ?", clientID, true).Find(&userPerms)
	for _, p := range userPerms {
		if p.TargetUser != nil {
			agentIDs[p.TargetUserID] = p.TargetUser
		}
	}

	// 分组授权
	if len(groupIDs) > 0 {
		var groupPerms []model.AclK8SServiceGroupPermission
		db.DB.Preload("TargetUser").Where("group_id IN ? AND enabled = ?", groupIDs, true).Find(&groupPerms)
		for _, p := range groupPerms {
			if p.TargetUser != nil {
				agentIDs[p.TargetUserID] = p.TargetUser
			}
		}
	}

	// 从缓存中获取发现的 K8S Service，匹配有权限的 Agent
	for agentID, agentUser := range agentIDs {
		discoveredServices := cache.GetK8SServiceDiscovery(agentID)
		for _, ds := range discoveredServices {
			// 查询域名
			var domainReg model.DomainRegistry
			domain := ""
			if err := db.DB.Where("user_id = ? AND type = ? AND namespace = ? AND service_name = ?",
				agentID, model.DomainTypeK8SSVC, ds.Namespace, ds.ServiceName).
				First(&domainReg).Error; err == nil {
				domain = domainReg.Domain
			}

			// 取第一个端口
			var port int32
			if len(ds.Ports) > 0 {
				port = ds.Ports[0].Port
			}

			resources = append(resources, K8SServiceResource{
				AgentID:     agentID,
				AgentName:   agentUser.Name,
				Namespace:   ds.Namespace,
				ServiceName: ds.ServiceName,
				Domain:      domain,
				Port:        port,
			})
		}
	}

	return resources
}

// findDomain 查找指定 Agent 和类型的域名
func (a *ResourceAPI) findDomain(agentUserID uint64, domainType model.DomainType) string {
	var domainReg model.DomainRegistry
	if err := db.DB.Where("user_id = ? AND type = ? AND status = ?",
		agentUserID, domainType, model.DomainStatusOnline).
		First(&domainReg).Error; err == nil {
		return domainReg.Domain
	}
	return ""
}

// GetK8SServiceDiscoveries 获取所有 Agent 发现的 K8S Service（管理员 API）
// GET /api/v1/admin/resources/k8s-services
func (a *ResourceAPI) GetK8SServiceDiscoveries(c *gin.Context) {
	allDiscoveries := cache.GetAllK8SServiceDiscovery()

	// 展平为扁平列表，每条记录包含 agent 信息
	type FlatDiscovery struct {
		AgentID      uint64                        `json:"agent_id"`
		AgentName    string                        `json:"agent_name"`
		Namespace    string                        `json:"namespace"`
		ServiceName  string                        `json:"service_name"`
		ClusterIP    string                        `json:"cluster_ip"`
		Ports        []cache.DiscoveredServicePort `json:"ports"`
		Labels       map[string]string             `json:"labels"`
		EndpointName string                        `json:"endpoint_name"` // 发现来源：为空表示 Agent 本身发现，不为空表示 Endpoint 发现
	}

	var result []FlatDiscovery
	for agentID, services := range allDiscoveries {
		// 查询 Agent 名称
		var user model.User
		agentName := ""
		if err := db.DB.First(&user, agentID).Error; err == nil {
			agentName = user.Name
		}
		for _, svc := range services {
			result = append(result, FlatDiscovery{
				AgentID:      agentID,
				AgentName:    agentName,
				Namespace:    svc.Namespace,
				ServiceName:  svc.ServiceName,
				ClusterIP:    svc.ClusterIP,
				Ports:        svc.Ports,
				Labels:       svc.Labels,
				EndpointName: svc.EndpointName, // 从缓存中读取 EndpointName
			})
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}
