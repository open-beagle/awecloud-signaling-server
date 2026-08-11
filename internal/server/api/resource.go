package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// ResourceAPI 资源发现 API
type ResourceAPI struct {
	config *config.ServerConfig
}

// NewResourceAPI 创建资源发现 API
func NewResourceAPI(cfg *config.ServerConfig) *ResourceAPI {
	return &ResourceAPI{config: cfg}
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

// ResourcesResponse 资源发现响应
type ResourcesResponse struct {
	SSH    []SSHResource    `json:"ssh"`
	K8SAPI []K8SAPIResource `json:"k8s_api"`
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
		SSH:    make([]SSHResource, 0),
		K8SAPI: make([]K8SAPIResource, 0),
	}

	// 1. 查询 SSH 资源
	result.SSH = a.querySSHResources(ctx, clientID, groupIDs)

	// 2. 查询 K8S API 资源
	result.K8SAPI = a.queryK8SAPIResources(ctx, clientID, groupIDs)

	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

func resourceContainsAction(actions []string, expected string) bool {
	for _, action := range actions {
		if action == expected {
			return true
		}
	}
	return false
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
