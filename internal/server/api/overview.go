package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type OverviewAPI struct{}

func NewOverviewAPI() *OverviewAPI { return &OverviewAPI{} }

type overviewAttentionItem struct {
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail"`
	Status    string    `json:"status"`
	TargetID  string    `json:"target_id,omitempty"`
	Route     string    `json:"route,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type tenantOverviewResponse struct {
	TenantID       string                  `json:"tenant_id"`
	MemberCount    int64                   `json:"member_count"`
	GroupCount     int64                   `json:"group_count"`
	ResourceCount  int64                   `json:"resource_count"`
	ActiveSessions int64                   `json:"active_sessions"`
	RiskCount      int64                   `json:"risk_count"`
	Attention      []overviewAttentionItem `json:"attention"`
}

type platformOverviewResponse struct {
	TenantCount          int64                   `json:"tenant_count"`
	AdminMembershipCount int64                   `json:"admin_membership_count"`
	ResourceCount        int64                   `json:"resource_count"`
	AgentCount           int64                   `json:"agent_count"`
	EndpointCount        int64                   `json:"endpoint_count"`
	HighRiskCount        int64                   `json:"high_risk_count"`
	Attention            []overviewAttentionItem `json:"attention"`
}

func (a *OverviewAPI) Tenant(c *gin.Context) {
	tenantID := c.Param("id")
	if !requireTenantPermission(c, tenantID, PermissionTenantOverviewRead) {
		return
	}
	ctx := c.Request.Context()
	now := time.Now()
	result := tenantOverviewResponse{TenantID: tenantID, Attention: []overviewAttentionItem{}}
	if err := db.DB.WithContext(ctx).Model(&model.TenantMembership{}).
		Where("tenant_id = ? AND enabled = ? AND (expires_at IS NULL OR expires_at > ?)", tenantID, true, now).
		Count(&result.MemberCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("统计租户有效成员失败"))
		return
	}
	if err := db.DB.WithContext(ctx).Model(&model.Group{}).Where("tenant_id = ?", tenantID).Count(&result.GroupCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("统计租户用户组失败"))
		return
	}
	if err := db.DB.WithContext(ctx).Model(&model.Resource{}).Where("tenant_id = ?", tenantID).Count(&result.ResourceCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("统计租户资源失败"))
		return
	}
	if err := db.DB.WithContext(ctx).Model(&model.ContainerSession{}).
		Where("tenant_id = ? AND status = ?", tenantID, model.ContainerSessionActive).Count(&result.ActiveSessions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("统计租户活动会话失败"))
		return
	}
	abnormalStates := []model.ResourceState{model.ResourceStatePending, model.ResourceStateDegraded, model.ResourceStateStopped}
	if err := db.DB.WithContext(ctx).Model(&model.Resource{}).
		Where("tenant_id = ? AND state IN ?", tenantID, abnormalStates).Count(&result.RiskCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("统计租户风险事项失败"))
		return
	}
	var resources []model.Resource
	if err := db.DB.WithContext(ctx).Where("tenant_id = ? AND state IN ?", tenantID, abnormalStates).
		Order("updated_at DESC").Limit(8).Find(&resources).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询租户关注事项失败"))
		return
	}
	for _, resource := range resources {
		result.Attention = append(result.Attention, overviewAttentionItem{
			Kind: "resource", Title: resource.DisplayName, Detail: resourceTypeLabel(resource.Type),
			Status: string(resource.State), TargetID: resource.ID, Route: "/resources/" + resource.ID, UpdatedAt: resource.UpdatedAt,
		})
	}
	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

func (a *OverviewAPI) Platform(c *gin.Context) {
	if !requirePlatformAccess(c, false) {
		return
	}
	ctx := c.Request.Context()
	now := time.Now()
	result := platformOverviewResponse{Attention: []overviewAttentionItem{}}
	if err := db.DB.WithContext(ctx).Model(&model.Tenant{}).Count(&result.TenantCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("统计租户失败"))
		return
	}
	if err := db.DB.WithContext(ctx).Model(&model.AdminTenantMembership{}).
		Where("enabled = ? AND (expires_at IS NULL OR expires_at > ?)", true, now).Count(&result.AdminMembershipCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("统计租户管理员授权失败"))
		return
	}
	if err := db.DB.WithContext(ctx).Model(&model.Resource{}).Count(&result.ResourceCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("统计全局资源失败"))
		return
	}
	if err := db.DB.WithContext(ctx).Model(&model.Node{}).Where("type = ?", model.NodeTypeAgent).Count(&result.AgentCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("统计 Agent 失败"))
		return
	}
	if err := db.DB.WithContext(ctx).Model(&model.Endpoint{}).Where("revoked = ?", false).Count(&result.EndpointCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("统计 Endpoint 失败"))
		return
	}
	var suspendedTenants, conflictCandidates, degradedResources int64
	if err := db.DB.WithContext(ctx).Model(&model.Tenant{}).Where("status = ?", model.TenantStatusSuspended).Count(&suspendedTenants).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("统计暂停租户失败"))
		return
	}
	if err := db.DB.WithContext(ctx).Model(&model.DiscoveryCandidate{}).Where("status = ?", model.DiscoveryCandidateConflict).Count(&conflictCandidates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("统计发现冲突失败"))
		return
	}
	if err := db.DB.WithContext(ctx).Model(&model.Resource{}).Where("state = ?", model.ResourceStateDegraded).Count(&degradedResources).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("统计异常资源失败"))
		return
	}
	result.HighRiskCount = suspendedTenants + conflictCandidates + degradedResources

	var tenants []model.Tenant
	if err := db.DB.WithContext(ctx).Where("status = ?", model.TenantStatusSuspended).Order("updated_at DESC").Limit(4).Find(&tenants).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询暂停租户失败"))
		return
	}
	for _, tenant := range tenants {
		result.Attention = append(result.Attention, overviewAttentionItem{Kind: "tenant", Title: tenant.Name, Detail: "租户已暂停", Status: "suspended", TargetID: tenant.ID, Route: "/tenants", UpdatedAt: tenant.UpdatedAt})
	}
	remaining := 8 - len(result.Attention)
	if remaining > 0 {
		var candidates []model.DiscoveryCandidate
		if err := db.DB.WithContext(ctx).Where("status = ?", model.DiscoveryCandidateConflict).Order("updated_at DESC").Limit(remaining).Find(&candidates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, NewErrorResponse("查询发现冲突失败"))
			return
		}
		for _, candidate := range candidates {
			title := candidate.WorkspaceHint
			if title == "" {
				title = candidate.PodName + " / " + candidate.ContainerName
			}
			result.Attention = append(result.Attention, overviewAttentionItem{Kind: "candidate", Title: title, Detail: candidate.ConflictReason, Status: "conflict", TargetID: candidate.ID, Route: "/resource-candidates", UpdatedAt: candidate.UpdatedAt})
		}
	}
	remaining = 8 - len(result.Attention)
	if remaining > 0 {
		var resources []model.Resource
		if err := db.DB.WithContext(ctx).Where("state = ?", model.ResourceStateDegraded).Order("updated_at DESC").Limit(remaining).Find(&resources).Error; err != nil {
			c.JSON(http.StatusInternalServerError, NewErrorResponse("查询异常资源失败"))
			return
		}
		for _, resource := range resources {
			result.Attention = append(result.Attention, overviewAttentionItem{Kind: "resource", Title: resource.DisplayName, Detail: resourceTypeLabel(resource.Type), Status: "degraded", TargetID: resource.ID, UpdatedAt: resource.UpdatedAt})
		}
	}
	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

func resourceTypeLabel(resourceType model.ResourceType) string {
	switch resourceType {
	case model.ResourceTypeContainerSSH:
		return "ContainerSSH"
	case model.ResourceTypeHostSSH:
		return "SSH 主机"
	case model.ResourceTypeKubernetesAPI:
		return "Kubernetes API"
	case model.ResourceTypeDatabaseService:
		return "数据库服务"
	case model.ResourceTypeTCPService:
		return "TCP 服务"
	default:
		return string(resourceType)
	}
}
