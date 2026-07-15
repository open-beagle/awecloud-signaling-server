package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// UnifiedResourceAPI exposes the first control-plane slice used by the new
// resource console. Runtime discovery and Kubernetes Exec are added later.
type UnifiedResourceAPI struct{}

func NewUnifiedResourceAPI() *UnifiedResourceAPI { return &UnifiedResourceAPI{} }

type tenantRequest struct {
	Key  string `json:"key" binding:"required"`
	Name string `json:"name" binding:"required"`
}

type tenantMemberRequest struct {
	UserID uint64 `json:"user_id" binding:"required"`
	Role   string `json:"role"`
}

type resourceRequest struct {
	TenantID            string             `json:"tenant_id" binding:"required"`
	Type                model.ResourceType `json:"type" binding:"required"`
	DisplayName         string             `json:"display_name" binding:"required"`
	ProviderID          string             `json:"provider_id"`
	ExternalWorkspaceID string             `json:"external_workspace_id"`
	OwnerUserID         uint64             `json:"owner_user_id"`
	AgentNodeID         uint64             `json:"agent_node_id"`
	ClusterID           string             `json:"cluster_id"`
	Namespace           string             `json:"namespace"`
	PodName             string             `json:"pod_name"`
	PodUID              string             `json:"pod_uid"`
	ContainerName       string             `json:"container_name"`
	ShellProfileID      string             `json:"shell_profile_id"`
	ExpiresAt           *time.Time         `json:"expires_at"`
}

type grantRequest struct {
	SubjectType       string    `json:"subject_type"`
	SubjectUserID     uint64    `json:"subject_user_id"`
	Actions           []string  `json:"actions"`
	ShellProfileID    string    `json:"shell_profile_id"`
	ValidFrom         time.Time `json:"valid_from"`
	ExpiresAt         time.Time `json:"expires_at"`
	MaxSessionSeconds int       `json:"max_session_seconds"`
}

type targetRequest struct {
	AgentNodeID   uint64 `json:"agent_node_id" binding:"required"`
	ClusterID     string `json:"cluster_id"`
	Namespace     string `json:"namespace" binding:"required"`
	PodName       string `json:"pod_name"`
	PodUID        string `json:"pod_uid" binding:"required"`
	ContainerName string `json:"container_name" binding:"required"`
	Ready         bool   `json:"ready"`
}

type tenantListItem struct {
	model.Tenant
	MemberCount   int64 `json:"member_count"`
	ResourceCount int64 `json:"resource_count"`
}

type resourceListItem struct {
	model.Resource
	TenantName   string `json:"tenant_name"`
	OwnerName    string `json:"owner_name,omitempty"`
	GrantCount   int64  `json:"grant_count"`
	SessionCount int64  `json:"session_count"`
}

type resourceDetail struct {
	Resource model.Resource        `json:"resource"`
	Tenant   model.Tenant          `json:"tenant"`
	Target   *model.ResourceTarget `json:"target,omitempty"`
	Grants   []model.AccessGrant   `json:"grants"`
}

// ListTenants returns customer contexts for the management console.
func (a *UnifiedResourceAPI) ListTenants(c *gin.Context) {
	ctx := c.Request.Context()
	page, size := pageParams(c)
	query := db.DB.WithContext(ctx).Model(&model.Tenant{})
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		like := "%" + search + "%"
		query = query.Where("key LIKE ? OR name LIKE ?", like, like)
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询客户失败"))
		return
	}
	var tenants []model.Tenant
	if err := query.Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&tenants).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询客户失败"))
		return
	}

	items := make([]tenantListItem, 0, len(tenants))
	for _, tenant := range tenants {
		var memberCount, resourceCount int64
		db.DB.WithContext(ctx).Model(&model.TenantMembership{}).Where("tenant_id = ? AND enabled = ?", tenant.ID, true).Count(&memberCount)
		db.DB.WithContext(ctx).Model(&model.Resource{}).Where("tenant_id = ?", tenant.ID).Count(&resourceCount)
		items = append(items, tenantListItem{Tenant: tenant, MemberCount: memberCount, ResourceCount: resourceCount})
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}

// CreateTenant creates a customer boundary. The caller remains responsible
// for adding an initial membership through the member endpoint.
func (a *UnifiedResourceAPI) CreateTenant(c *gin.Context) {
	var req tenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("客户标识和名称不能为空"))
		return
	}
	req.Key = strings.TrimSpace(req.Key)
	req.Name = strings.TrimSpace(req.Name)
	if req.Key == "" || req.Name == "" {
		c.JSON(http.StatusBadRequest, NewErrorResponse("客户标识和名称不能为空"))
		return
	}
	tenant := model.Tenant{ID: uuid.NewString(), Key: req.Key, Name: req.Name, Status: model.TenantStatusActive}
	if err := db.DB.WithContext(c.Request.Context()).Create(&tenant).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			c.JSON(http.StatusConflict, NewErrorResponse("客户标识已存在"))
			return
		}
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建客户失败"))
		return
	}
	recordAuditLog(c.Request.Context(), c, "create_tenant", "tenant", tenant.ID, tenant.Name, tenant)
	c.JSON(http.StatusCreated, NewSuccessResponse(tenant))
}

// AddTenantMember connects an existing global user to a tenant.
func (a *UnifiedResourceAPI) AddTenantMember(c *gin.Context) {
	tenantID := c.Param("id")
	var tenant model.Tenant
	if err := db.DB.WithContext(c.Request.Context()).First(&tenant, "id = ?", tenantID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("客户不存在"))
		return
	}
	var req tenantMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.UserID == 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("用户 ID 无效"))
		return
	}
	if req.Role == "" {
		req.Role = "member"
	}
	if req.Role != "member" && req.Role != "tenant_admin" && req.Role != "viewer" {
		c.JSON(http.StatusBadRequest, NewErrorResponse("客户成员角色无效"))
		return
	}
	var user model.User
	if err := db.DB.WithContext(c.Request.Context()).First(&user, req.UserID).Error; err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("用户不存在"))
		return
	}
	membership := model.TenantMembership{TenantID: tenantID, UserID: req.UserID, Role: req.Role, Enabled: true}
	if err := db.DB.WithContext(c.Request.Context()).Where("tenant_id = ? AND user_id = ?", tenantID, req.UserID).
		Assign(map[string]interface{}{"role": req.Role, "enabled": true}).FirstOrCreate(&membership).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("添加客户成员失败"))
		return
	}
	recordAuditLog(c.Request.Context(), c, "add_tenant_member", "tenant", tenantID, tenant.Name, map[string]interface{}{"user_id": req.UserID, "role": req.Role})
	c.JSON(http.StatusCreated, NewSuccessResponse(membership))
}

// List returns resources in the selected tenant context. Omitting tenant_id
// is allowed for Platform Admin inventory views and remains read-only at the
// UI layer until a concrete tenant is selected.
func (a *UnifiedResourceAPI) List(c *gin.Context) {
	ctx := c.Request.Context()
	page, size := pageParams(c)
	query := db.DB.WithContext(ctx).Model(&model.Resource{})
	if tenantID := c.Query("tenant_id"); tenantID != "" {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if resourceType := c.Query("type"); resourceType != "" {
		query = query.Where("type = ?", resourceType)
	}
	if state := c.Query("state"); state != "" {
		query = query.Where("state = ?", state)
	}
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		like := "%" + search + "%"
		query = query.Where("display_name LIKE ? OR provider_id LIKE ? OR external_workspace_id LIKE ? OR pod_name LIKE ?", like, like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询资源失败"))
		return
	}
	var resources []model.Resource
	if err := query.Order("updated_at DESC").Offset((page - 1) * size).Limit(size).Find(&resources).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询资源失败"))
		return
	}
	items := make([]resourceListItem, 0, len(resources))
	for _, resource := range resources {
		item := resourceListItem{Resource: resource}
		var tenant model.Tenant
		if err := db.DB.WithContext(ctx).First(&tenant, "id = ?", resource.TenantID).Error; err == nil {
			item.TenantName = tenant.Name
		}
		if resource.OwnerUserID != 0 {
			var user model.User
			if err := db.DB.WithContext(ctx).First(&user, resource.OwnerUserID).Error; err == nil {
				item.OwnerName = user.Alias
				if item.OwnerName == "" {
					item.OwnerName = user.Name
				}
			}
		}
		db.DB.WithContext(ctx).Model(&model.AccessGrant{}).Where("resource_id = ? AND status = ?", resource.ID, "enabled").Count(&item.GrantCount)
		items = append(items, item)
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}

// Get returns one resource, its latest target and access grants.
func (a *UnifiedResourceAPI) Get(c *gin.Context) {
	ctx := c.Request.Context()
	var resource model.Resource
	if err := db.DB.WithContext(ctx).First(&resource, "id = ?", c.Param("id")).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, NewErrorResponse("资源不存在"))
			return
		}
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询资源失败"))
		return
	}
	var tenant model.Tenant
	if err := db.DB.WithContext(ctx).First(&tenant, "id = ?", resource.TenantID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("资源客户不存在"))
		return
	}
	var target model.ResourceTarget
	targetPtr := &target
	if err := db.DB.WithContext(ctx).Where("resource_id = ?", resource.ID).Order("revision DESC").First(&target).Error; err != nil {
		targetPtr = nil
	}
	var grants []model.AccessGrant
	if err := db.DB.WithContext(ctx).Where("resource_id = ?", resource.ID).Order("created_at DESC").Find(&grants).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询资源授权失败"))
		return
	}
	c.JSON(http.StatusOK, NewSuccessResponse(resourceDetail{Resource: resource, Tenant: tenant, Target: targetPtr, Grants: grants}))
}

// Create registers a manually managed resource. Beagle IDE provider events
// will use the same model after the provider binding endpoint is added.
func (a *UnifiedResourceAPI) Create(c *gin.Context) {
	var req resourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("资源类型、客户和名称不能为空"))
		return
	}
	if !validResourceType(req.Type) {
		c.JSON(http.StatusBadRequest, NewErrorResponse("不支持的资源类型"))
		return
	}
	var tenant model.Tenant
	if err := db.DB.WithContext(c.Request.Context()).First(&tenant, "id = ?", req.TenantID).Error; err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("客户不存在"))
		return
	}
	if tenant.Status != model.TenantStatusActive {
		c.JSON(http.StatusConflict, NewErrorResponse("客户已停用，不能创建资源"))
		return
	}
	if req.OwnerUserID != 0 {
		var membership model.TenantMembership
		if err := db.DB.WithContext(c.Request.Context()).Where("tenant_id = ? AND user_id = ? AND enabled = ?", req.TenantID, req.OwnerUserID, true).First(&membership).Error; err != nil {
			c.JSON(http.StatusBadRequest, NewErrorResponse("Owner 不属于该客户或成员已禁用"))
			return
		}
	}
	if req.ExternalWorkspaceID != "" {
		var existing model.Resource
		if err := db.DB.WithContext(c.Request.Context()).Where("provider_id = ? AND external_workspace_id = ?", req.ProviderID, req.ExternalWorkspaceID).First(&existing).Error; err == nil {
			c.JSON(http.StatusConflict, NewErrorResponse("Workspace 已绑定资源"))
			return
		}
	}
	resource := model.Resource{
		ID: uuid.NewString(), TenantID: req.TenantID, Type: req.Type, DisplayName: strings.TrimSpace(req.DisplayName),
		ProviderID: req.ProviderID, ExternalWorkspaceID: req.ExternalWorkspaceID, OwnerUserID: req.OwnerUserID,
		AgentNodeID: req.AgentNodeID, ClusterID: req.ClusterID, Namespace: req.Namespace, PodName: req.PodName,
		PodUID: req.PodUID, ContainerName: req.ContainerName, ShellProfileID: req.ShellProfileID,
		TargetRevision: 0, State: model.ResourceStatePending, ExpiresAt: req.ExpiresAt,
	}
	if resource.DisplayName == "" {
		c.JSON(http.StatusBadRequest, NewErrorResponse("资源名称不能为空"))
		return
	}
	if err := db.DB.WithContext(c.Request.Context()).Create(&resource).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建资源失败"))
		return
	}
	recordAuditLog(c.Request.Context(), c, "create_resource", "resource", resource.ID, resource.DisplayName, resource)
	c.JSON(http.StatusCreated, NewSuccessResponse(resource))
}

// ObserveTarget records a new runtime target revision for a known Resource.
// It is intentionally separate from Candidate observation: a candidate has
// no trusted business identity, while this endpoint is scoped to an existing
// Resource and its Agent proof.
func (a *UnifiedResourceAPI) ObserveTarget(c *gin.Context) {
	ctx := c.Request.Context()
	var resource model.Resource
	if err := db.DB.WithContext(ctx).First(&resource, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("资源不存在"))
		return
	}
	var req targetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("运行目标格式无效"))
		return
	}
	target, status, message := saveResourceTarget(ctx, &resource, req)
	if status != http.StatusCreated {
		c.JSON(status, NewErrorResponse(message))
		return
	}
	recordAuditLog(ctx, c, "observe_resource_target", "resource", resource.ID, resource.DisplayName, target)
	c.JSON(http.StatusCreated, NewSuccessResponse(target))
}

// saveResourceTarget is shared by the admin target endpoint and trusted
// Workspace reconciliation. It keeps all Pod UID/Container uniqueness and
// Resource state transitions in one place.
func saveResourceTarget(ctx context.Context, resource *model.Resource, req targetRequest) (*model.ResourceTarget, int, string) {
	if req.AgentNodeID == 0 || strings.TrimSpace(req.Namespace) == "" || strings.TrimSpace(req.PodUID) == "" || strings.TrimSpace(req.ContainerName) == "" {
		return nil, http.StatusBadRequest, "Agent、Namespace、Pod UID 和容器不能为空"
	}
	var node model.Node
	if err := db.DB.WithContext(ctx).Where("id = ? AND type = ?", req.AgentNodeID, model.NodeTypeAgent).First(&node).Error; err != nil {
		return nil, http.StatusBadRequest, "Agent 不存在或类型无效"
	}
	var conflict model.ResourceTarget
	if err := db.DB.WithContext(ctx).Where("pod_uid = ? AND container_name = ? AND resource_id <> ?", req.PodUID, req.ContainerName, resource.ID).First(&conflict).Error; err == nil {
		return nil, http.StatusConflict, "Pod UID 和容器已绑定其他资源"
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, http.StatusInternalServerError, "检查运行目标冲突失败"
	}
	var resourceConflict model.Resource
	if err := db.DB.WithContext(ctx).Where("pod_uid = ? AND container_name = ? AND id <> ?", req.PodUID, req.ContainerName, resource.ID).First(&resourceConflict).Error; err == nil {
		return nil, http.StatusConflict, "Pod UID 和容器已绑定其他资源"
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, http.StatusInternalServerError, "检查资源目标冲突失败"
	}
	revision := resource.TargetRevision + 1
	target := model.ResourceTarget{
		ResourceID: resource.ID, Revision: revision, AgentNodeID: req.AgentNodeID,
		ClusterID: strings.TrimSpace(req.ClusterID), Namespace: strings.TrimSpace(req.Namespace),
		PodName: strings.TrimSpace(req.PodName), PodUID: strings.TrimSpace(req.PodUID),
		ContainerName: strings.TrimSpace(req.ContainerName), Ready: req.Ready, ObservedAt: time.Now(),
	}
	if err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&target).Error; err != nil {
			return err
		}
		state := model.ResourceStateDegraded
		if req.Ready {
			state = model.ResourceStateAvailable
		}
		return tx.Model(resource).Updates(map[string]interface{}{
			"agent_node_id": req.AgentNodeID, "cluster_id": target.ClusterID, "namespace": target.Namespace,
			"pod_name": target.PodName, "pod_uid": target.PodUID, "container_name": target.ContainerName,
			"target_revision": revision, "state": state,
		}).Error
	}); err != nil {
		return nil, http.StatusInternalServerError, "保存运行目标失败"
	}
	return &target, http.StatusCreated, ""
}

// ListGrants lists grants for a resource after enforcing the resource scope.
func (a *UnifiedResourceAPI) ListGrants(c *gin.Context) {
	var resource model.Resource
	if err := db.DB.WithContext(c.Request.Context()).First(&resource, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("资源不存在"))
		return
	}
	var grants []model.AccessGrant
	if err := db.DB.WithContext(c.Request.Context()).Where("resource_id = ?", resource.ID).Order("created_at DESC").Find(&grants).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询访问策略失败"))
		return
	}
	c.JSON(http.StatusOK, NewSuccessResponse(grants))
}

// CreateGrant currently supports direct User grants. Group grants are kept in
// the schema but remain disabled until Group receives a tenant boundary.
func (a *UnifiedResourceAPI) CreateGrant(c *gin.Context) {
	ctx := c.Request.Context()
	var resource model.Resource
	if err := db.DB.WithContext(ctx).First(&resource, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("资源不存在"))
		return
	}
	var req grantRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.SubjectUserID == 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("当前阶段必须指定用户"))
		return
	}
	var membership model.TenantMembership
	if err := db.DB.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND enabled = ?", resource.TenantID, req.SubjectUserID, true).First(&membership).Error; err != nil {
		c.JSON(http.StatusForbidden, NewErrorResponse("用户不属于资源客户或成员已禁用"))
		return
	}
	if len(req.Actions) == 0 {
		req.Actions = []string{"shell"}
	}
	if req.ValidFrom.IsZero() {
		req.ValidFrom = time.Now()
	}
	if req.ExpiresAt.IsZero() {
		req.ExpiresAt = time.Now().Add(24 * time.Hour)
	}
	if !req.ExpiresAt.After(req.ValidFrom) {
		c.JSON(http.StatusBadRequest, NewErrorResponse("授权失效时间必须晚于生效时间"))
		return
	}
	if req.MaxSessionSeconds <= 0 {
		req.MaxSessionSeconds = 8 * 60 * 60
	}
	actions, _ := json.Marshal(req.Actions)
	grant := model.AccessGrant{ID: uuid.NewString(), TenantID: resource.TenantID, ResourceID: resource.ID, SubjectType: "user", SubjectUserID: req.SubjectUserID, Actions: string(actions), ShellProfileID: req.ShellProfileID, ValidFrom: req.ValidFrom, ExpiresAt: req.ExpiresAt, MaxSessionSeconds: req.MaxSessionSeconds, Revision: 1, Status: "enabled"}
	if err := db.DB.WithContext(ctx).Create(&grant).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建访问策略失败"))
		return
	}
	recordAuditLog(ctx, c, "create_access_grant", "resource", resource.ID, resource.DisplayName, grant)
	c.JSON(http.StatusCreated, NewSuccessResponse(grant))
}

func pageParams(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	return page, size
}

func validResourceType(resourceType model.ResourceType) bool {
	switch resourceType {
	case model.ResourceTypeHostSSH, model.ResourceTypeContainerSSH, model.ResourceTypeKubernetesAPI, model.ResourceTypeDatabaseService, model.ResourceTypeTCPService:
		return true
	default:
		return false
	}
}
