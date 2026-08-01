package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

// WorkspaceBindingAPI manages the trusted business side of ContainerSSH
// discovery. Provider IDs are external identities; Tenant IDs are selected by
// a platform administrator through an existing ProviderTenantBinding.
type WorkspaceBindingAPI struct{}

func NewWorkspaceBindingAPI() *WorkspaceBindingAPI { return &WorkspaceBindingAPI{} }

type providerTenantBindingRequest struct {
	ProviderID       string `json:"provider_id" binding:"required"`
	ExternalTenantID string `json:"external_tenant_id" binding:"required"`
	TenantID         string `json:"tenant_id" binding:"required"`
}

type workspaceBindingRequest struct {
	ProviderID          string                       `json:"provider_id" binding:"required"`
	ExternalTenantID    string                       `json:"external_tenant_id" binding:"required"`
	ExternalWorkspaceID string                       `json:"external_workspace_id" binding:"required"`
	DisplayName         string                       `json:"display_name"`
	OwnerUserID         uint64                       `json:"owner_user_id"`
	Generation          int64                        `json:"generation"`
	Status              model.WorkspaceBindingStatus `json:"status"`
	ExpiresAt           *time.Time                   `json:"expires_at"`
}

type providerTenantBindingListItem struct {
	model.ProviderTenantBinding
	TenantName string `json:"tenant_name,omitempty"`
}

type workspaceBindingListItem struct {
	model.WorkspaceBinding
	TenantName string `json:"tenant_name,omitempty"`
}

func (a *WorkspaceBindingAPI) ListProviderTenantBindings(c *gin.Context) {
	if !requirePlatformAccess(c, false) {
		return
	}
	ctx := c.Request.Context()
	page, size := pageParams(c)
	query := db.DB.WithContext(ctx).Model(&model.ProviderTenantBinding{})
	if providerID := strings.TrimSpace(c.Query("provider_id")); providerID != "" {
		query = query.Where("provider_id = ?", providerID)
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询 Provider 客户绑定失败"))
		return
	}
	var bindings []model.ProviderTenantBinding
	if err := query.Order("updated_at DESC").Offset((page - 1) * size).Limit(size).Find(&bindings).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询 Provider 客户绑定失败"))
		return
	}
	items := make([]providerTenantBindingListItem, 0, len(bindings))
	for _, binding := range bindings {
		item := providerTenantBindingListItem{ProviderTenantBinding: binding}
		var tenant model.Tenant
		if err := db.DB.WithContext(ctx).First(&tenant, "id = ?", binding.TenantID).Error; err == nil {
			item.TenantName = tenant.Name
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}

func (a *WorkspaceBindingAPI) CreateProviderTenantBinding(c *gin.Context) {
	if !requirePlatformAccess(c, true) {
		return
	}
	ctx := c.Request.Context()
	var req providerTenantBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("Provider、外部客户和内部客户不能为空"))
		return
	}
	req.ProviderID = strings.TrimSpace(req.ProviderID)
	req.ExternalTenantID = strings.TrimSpace(req.ExternalTenantID)
	req.TenantID = strings.TrimSpace(req.TenantID)
	if req.ProviderID == "" || req.ExternalTenantID == "" || req.TenantID == "" {
		c.JSON(http.StatusBadRequest, NewErrorResponse("Provider、外部客户和内部客户不能为空"))
		return
	}
	if !requireTenantPermission(c, req.TenantID, PermissionTenantResourcesWrite) {
		return
	}
	var tenant model.Tenant
	if err := db.DB.WithContext(ctx).First(&tenant, "id = ?", req.TenantID).Error; err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("客户不存在"))
		return
	}
	if tenant.Status != model.TenantStatusActive {
		c.JSON(http.StatusConflict, NewErrorResponse("客户已停用，不能绑定 Provider"))
		return
	}
	var binding model.ProviderTenantBinding
	err := db.DB.WithContext(ctx).Where("provider_id = ? AND external_tenant_id = ?", req.ProviderID, req.ExternalTenantID).First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		binding = model.ProviderTenantBinding{ID: uuid.NewString(), ProviderID: req.ProviderID, ExternalTenantID: req.ExternalTenantID, TenantID: req.TenantID, Status: model.ProviderBindingActive}
		if err := db.DB.WithContext(ctx).Create(&binding).Error; err != nil {
			c.JSON(http.StatusInternalServerError, NewErrorResponse("创建 Provider 客户绑定失败"))
			return
		}
		recordAuditLog(ctx, c, "create_provider_tenant_binding", "provider_tenant_binding", binding.ID, binding.ExternalTenantID, binding)
		a.reconcileProviderTenant(ctx, binding.ProviderID, binding.ExternalTenantID)
		c.JSON(http.StatusCreated, NewSuccessResponse(binding))
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询 Provider 客户绑定失败"))
		return
	}
	if binding.TenantID != req.TenantID {
		c.JSON(http.StatusConflict, NewErrorResponse("Provider 外部客户已绑定其他内部客户"))
		return
	}
	binding.Status = model.ProviderBindingActive
	if err := db.DB.WithContext(ctx).Save(&binding).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("恢复 Provider 客户绑定失败"))
		return
	}
	a.reconcileProviderTenant(ctx, binding.ProviderID, binding.ExternalTenantID)
	c.JSON(http.StatusOK, NewSuccessResponse(binding))
}

func (a *WorkspaceBindingAPI) ListWorkspaceBindings(c *gin.Context) {
	ctx := c.Request.Context()
	page, size := pageParams(c)
	query := db.DB.WithContext(ctx).Model(&model.WorkspaceBinding{})
	tenantIDs, unrestricted, ok := tenantReadScope(c, PermissionTenantResourcesRead)
	if !ok {
		return
	}
	if !unrestricted {
		query = query.Where("tenant_id IN ?", tenantIDs)
	}
	if providerID := strings.TrimSpace(c.Query("provider_id")); providerID != "" {
		query = query.Where("provider_id = ?", providerID)
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询 Workspace 绑定失败"))
		return
	}
	var bindings []model.WorkspaceBinding
	if err := query.Order("updated_at DESC").Offset((page - 1) * size).Limit(size).Find(&bindings).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询 Workspace 绑定失败"))
		return
	}
	items := make([]workspaceBindingListItem, 0, len(bindings))
	for _, binding := range bindings {
		item := workspaceBindingListItem{WorkspaceBinding: binding}
		var tenant model.Tenant
		if err := db.DB.WithContext(ctx).First(&tenant, "id = ?", binding.TenantID).Error; err == nil {
			item.TenantName = tenant.Name
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}

func (a *WorkspaceBindingAPI) CreateWorkspaceBinding(c *gin.Context) {
	ctx := c.Request.Context()
	var req workspaceBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("Provider、外部客户和 Workspace 不能为空"))
		return
	}
	req.ProviderID = strings.TrimSpace(req.ProviderID)
	req.ExternalTenantID = strings.TrimSpace(req.ExternalTenantID)
	req.ExternalWorkspaceID = strings.TrimSpace(req.ExternalWorkspaceID)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.ProviderID == "" || req.ExternalTenantID == "" || req.ExternalWorkspaceID == "" {
		c.JSON(http.StatusBadRequest, NewErrorResponse("Provider、外部客户和 Workspace 不能为空"))
		return
	}
	if req.Generation <= 0 {
		req.Generation = 1
	}
	if req.Status == "" {
		req.Status = model.WorkspaceBindingActive
	}
	if req.Status != model.WorkspaceBindingActive && req.Status != model.WorkspaceBindingStopped && req.Status != model.WorkspaceBindingRevoked {
		c.JSON(http.StatusBadRequest, NewErrorResponse("Workspace 生命周期状态无效"))
		return
	}
	tenantID, unrestricted, ok := tenantObjectScope(c, PermissionTenantResourcesWrite)
	if !ok {
		return
	}
	var tenantBinding model.ProviderTenantBinding
	bindingQuery := db.DB.WithContext(ctx).Where("provider_id = ? AND external_tenant_id = ? AND status = ?", req.ProviderID, req.ExternalTenantID, model.ProviderBindingActive)
	if !unrestricted {
		bindingQuery = bindingQuery.Where("tenant_id = ?", tenantID)
	}
	if err := bindingQuery.First(&tenantBinding).Error; err != nil {
		codedError(c, http.StatusNotFound, ErrorCodeTenantObjectNotFound, "当前租户范围内对象不存在")
		return
	}
	var tenant model.Tenant
	if err := db.DB.WithContext(ctx).First(&tenant, "id = ?", tenantBinding.TenantID).Error; err != nil || tenant.Status != model.TenantStatusActive {
		c.JSON(http.StatusConflict, NewErrorResponse("绑定的客户不可用"))
		return
	}
	var existing model.WorkspaceBinding
	existingErr := db.DB.WithContext(ctx).Where("provider_id = ? AND external_workspace_id = ?", req.ProviderID, req.ExternalWorkspaceID).First(&existing).Error
	if existingErr == nil {
		if existing.TenantID != tenant.ID {
			c.JSON(http.StatusConflict, NewErrorResponse("Workspace 已绑定其他客户"))
			return
		}
		if req.Generation < existing.Generation {
			c.JSON(http.StatusConflict, NewErrorResponse("Workspace generation 不能回退"))
			return
		}
	} else if !errors.Is(existingErr, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询 Workspace 绑定失败"))
		return
	}
	effectiveOwnerUserID := req.OwnerUserID
	if existingErr == nil && effectiveOwnerUserID == 0 {
		effectiveOwnerUserID = existing.OwnerUserID
	}
	if effectiveOwnerUserID != 0 {
		var membership model.TenantMembership
		if err := db.DB.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND enabled = ?", tenant.ID, effectiveOwnerUserID, true).First(&membership).Error; err != nil {
			c.JSON(http.StatusBadRequest, NewErrorResponse("Owner 不属于该客户或成员已禁用"))
			return
		}
	}
	var resource model.Resource
	resourceErr := db.DB.WithContext(ctx).Where("provider_id = ? AND external_workspace_id = ?", req.ProviderID, req.ExternalWorkspaceID).First(&resource).Error
	if errors.Is(resourceErr, gorm.ErrRecordNotFound) {
		name := req.DisplayName
		if name == "" {
			name = req.ExternalWorkspaceID
		}
		resource = model.Resource{ID: uuid.NewString(), TenantID: tenant.ID, Type: model.ResourceTypeContainerSSH, DisplayName: name, ProviderID: req.ProviderID, ExternalWorkspaceID: req.ExternalWorkspaceID, OwnerUserID: req.OwnerUserID, TargetRevision: 0, State: model.ResourceStatePending, ExpiresAt: req.ExpiresAt}
	} else if resourceErr != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询 Workspace Resource 失败"))
		return
	} else if resource.TenantID != tenant.ID {
		c.JSON(http.StatusConflict, NewErrorResponse("Workspace Resource 已属于其他客户"))
		return
	}
	if req.DisplayName != "" {
		resource.DisplayName = req.DisplayName
	}
	resource.OwnerUserID = effectiveOwnerUserID
	resource.ExpiresAt = req.ExpiresAt
	switch req.Status {
	case model.WorkspaceBindingStopped:
		resource.State = model.ResourceStateStopped
	case model.WorkspaceBindingRevoked:
		resource.State = model.ResourceStateRevoked
	case model.WorkspaceBindingActive:
		if resource.TargetRevision == 0 {
			resource.State = model.ResourceStatePending
		} else if resource.State == model.ResourceStateStopped || resource.State == model.ResourceStateRevoked {
			resource.State = model.ResourceStatePending
		}
	}
	binding := existing
	if existingErr != nil {
		binding = model.WorkspaceBinding{ID: uuid.NewString(), ProviderID: req.ProviderID, ExternalTenantID: req.ExternalTenantID, ExternalWorkspaceID: req.ExternalWorkspaceID}
	}
	binding.TenantID = tenant.ID
	binding.OwnerUserID = effectiveOwnerUserID
	binding.ResourceID = resource.ID
	binding.Generation = req.Generation
	binding.Status = req.Status
	binding.ExpiresAt = req.ExpiresAt
	if err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if resourceErr != nil {
			if err := tx.Create(&resource).Error; err != nil {
				return err
			}
		} else if err := tx.Save(&resource).Error; err != nil {
			return err
		}
		if existingErr != nil {
			return tx.Create(&binding).Error
		}
		return tx.Save(&binding).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("保存 Workspace 绑定失败"))
		return
	}
	if existingErr != nil {
		recordAuditLog(ctx, c, "create_workspace_binding", "workspace_binding", binding.ID, binding.ExternalWorkspaceID, binding)
	} else {
		recordAuditLog(ctx, c, "update_workspace_binding", "workspace_binding", binding.ID, binding.ExternalWorkspaceID, binding)
	}
	a.reconcileWorkspace(ctx, binding.ProviderID, binding.ExternalWorkspaceID)
	if existingErr != nil {
		c.JSON(http.StatusCreated, NewSuccessResponse(binding))
	} else {
		c.JSON(http.StatusOK, NewSuccessResponse(binding))
	}
}

func (a *WorkspaceBindingAPI) reconcileProviderTenant(ctx context.Context, providerID, externalTenantID string) {
	count, err := service.NewResourceReconciliationService(db.DB).ReconcileProviderTenant(ctx, providerID, externalTenantID)
	if err != nil {
		logger.Warnf("Provider Tenant Binding 变更后自动匹配失败: provider=%s external_tenant=%s matched=%d err=%v", providerID, externalTenantID, count, err)
	}
}

func (a *WorkspaceBindingAPI) reconcileWorkspace(ctx context.Context, providerID, workspaceID string) {
	count, err := service.NewResourceReconciliationService(db.DB).ReconcileWorkspace(ctx, providerID, workspaceID)
	if err != nil {
		logger.Warnf("Workspace Binding 变更后自动匹配失败: provider=%s workspace=%s matched=%d err=%v", providerID, workspaceID, count, err)
	}
}
