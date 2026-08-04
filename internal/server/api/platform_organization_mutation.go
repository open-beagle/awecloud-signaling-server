package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

var (
	errPlatformOrganizationVersionConflict = errors.New("platform organization version conflict")
	errPlatformOrganizationStateConflict   = errors.New("platform organization state conflict")
	organizationKeyPattern                 = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,98}[a-z0-9])?$`)
)

type createPlatformOrganizationRequest struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	DomainLabel string `json:"domain_label"`
	Reason      string `json:"reason"`
}

type updatePlatformOrganizationRequest struct {
	Name                     string `json:"name"`
	DomainLabel              string `json:"domain_label"`
	DomainChangeConfirmation string `json:"domain_change_confirmation"`
	Reason                   string `json:"reason"`
}

type transitionPlatformOrganizationRequest struct {
	Reason string `json:"reason"`
}

type platformOrganizationMutationResponse struct {
	ID          string    `json:"id"`
	ScopeType   string    `json:"scope_type"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	DomainLabel string    `json:"domain_label,omitempty"`
	Status      string    `json:"status"`
	Revision    int64     `json:"revision"`
	RowVersion  int64     `json:"row_version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (a *PlatformGovernanceAPI) CreateProvider(c *gin.Context) {
	a.createOrganization(c, model.ManagementScopeProvider)
}
func (a *PlatformGovernanceAPI) CreateTenant(c *gin.Context) {
	a.createOrganization(c, model.ManagementScopeTenant)
}
func (a *PlatformGovernanceAPI) UpdateProvider(c *gin.Context) {
	a.updateOrganization(c, model.ManagementScopeProvider)
}
func (a *PlatformGovernanceAPI) UpdateTenant(c *gin.Context) {
	a.updateOrganization(c, model.ManagementScopeTenant)
}
func (a *PlatformGovernanceAPI) SuspendProvider(c *gin.Context) {
	a.transitionOrganization(c, model.ManagementScopeProvider, "suspend")
}
func (a *PlatformGovernanceAPI) ResumeProvider(c *gin.Context) {
	a.transitionOrganization(c, model.ManagementScopeProvider, "resume")
}
func (a *PlatformGovernanceAPI) SuspendTenant(c *gin.Context) {
	a.transitionOrganization(c, model.ManagementScopeTenant, "suspend")
}
func (a *PlatformGovernanceAPI) ResumeTenant(c *gin.Context) {
	a.transitionOrganization(c, model.ManagementScopeTenant, "resume")
}

func (a *PlatformGovernanceAPI) createOrganization(c *gin.Context, scopeType model.ManagementScopeType) {
	identity, ok := currentUnifiedManagementIdentity(c)
	if !ok {
		return
	}
	body, ok := readSimulationJSON(c)
	if !ok {
		return
	}
	var request createPlatformOrganizationRequest
	if !decodeSimulationJSON(c, body, &request) {
		return
	}
	request.Key = strings.ToLower(strings.TrimSpace(request.Key))
	request.Name = strings.TrimSpace(request.Name)
	request.DomainLabel = strings.TrimSpace(request.DomainLabel)
	request.Reason = strings.TrimSpace(request.Reason)
	if !validOrganizationMutationFields(request.Key, request.Name, request.Reason) {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "组织标识、名称或创建原因无效")
		return
	}
	executeIdempotentOrganizationMutation(c, identity.UserID, body, http.StatusCreated, func(tx *gorm.DB) (platformOrganizationMutationResponse, error) {
		id := uuid.NewString()
		var response platformOrganizationMutationResponse
		if scopeType == model.ManagementScopeProvider {
			domainLabel, err := service.NormalizeProviderDomainLabel(request.DomainLabel)
			if err != nil {
				return response, err
			}
			var domainCount int64
			if err := tx.Model(&model.ResourceProvider{}).Where("lower(domain_label) = ?", domainLabel).Count(&domainCount).Error; err != nil {
				return response, err
			}
			if domainCount > 0 {
				return response, service.ErrProviderDomainLabelExists
			}
			item := &model.ResourceProvider{ID: id, Key: request.Key, DisplayName: request.Name, DomainLabel: domainLabel, Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
			if err := tx.Create(item).Error; err != nil {
				return response, err
			}
			response = providerOrganizationMutationView(item)
		} else {
			item := &model.Tenant{ID: id, Key: request.Key, Name: request.Name, Status: model.TenantStatusActive, Revision: 1, RowVersion: 1}
			if err := tx.Create(item).Error; err != nil {
				return response, err
			}
			response = tenantOrganizationMutationView(item)
		}
		err := recordAuditLogStrictWithDB(c.Request.Context(), tx, c, "create_"+string(scopeType), "organization", id, id, gin.H{"scope_type": scopeType, "key": request.Key, "name": request.Name, "domain_label": response.DomainLabel, "reason": request.Reason})
		return response, err
	})
}

func (a *PlatformGovernanceAPI) updateOrganization(c *gin.Context, scopeType model.ManagementScopeType) {
	rowVersion, ok := requiredRevision(c)
	if !ok {
		codedError(c, http.StatusPreconditionRequired, ErrorCodePreconditionRequired, "必须提供 If-Match revision")
		return
	}
	body, ok := readSimulationJSON(c)
	if !ok {
		return
	}
	var request updatePlatformOrganizationRequest
	if !decodeSimulationJSON(c, body, &request) {
		return
	}
	request.Name, request.Reason = strings.TrimSpace(request.Name), strings.TrimSpace(request.Reason)
	request.DomainLabel = strings.TrimSpace(request.DomainLabel)
	request.DomainChangeConfirmation = strings.TrimSpace(request.DomainChangeConfirmation)
	if request.Name == "" || len(request.Name) > 200 || request.Reason == "" || len(request.Reason) > 500 {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "组织名称或变更原因无效")
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	var response platformOrganizationMutationResponse
	err := db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if scopeType == model.ManagementScopeProvider {
			var current model.ResourceProvider
			if err := tx.First(&current, "id = ?", id).Error; err != nil {
				return err
			}
			if current.RowVersion != rowVersion {
				return errPlatformOrganizationVersionConflict
			}
			updates := map[string]any{"display_name": request.Name, "revision": gorm.Expr("revision + 1"), "row_version": gorm.Expr("row_version + 1")}
			domainChange := service.ProviderDomainChangeResult{}
			newDomainLabel := current.DomainLabel
			if request.DomainLabel != "" {
				var err error
				newDomainLabel, err = service.NormalizeProviderDomainLabel(request.DomainLabel)
				if err != nil {
					return err
				}
			}
			if newDomainLabel != current.DomainLabel {
				confirmation, err := service.NormalizeProviderDomainLabel(request.DomainChangeConfirmation)
				if err != nil || confirmation != newDomainLabel {
					return service.ErrProviderDomainConfirmation
				}
				var count int64
				if err := tx.Model(&model.ResourceProvider{}).Where("lower(domain_label) = ? AND id <> ?", newDomainLabel, id).Count(&count).Error; err != nil {
					return err
				}
				if count > 0 {
					return service.ErrProviderDomainLabelExists
				}
				domainChange, err = service.ChangeProviderDomainLabel(c.Request.Context(), tx, id, current.DomainLabel, newDomainLabel)
				if err != nil {
					return err
				}
				updates["domain_label"] = newDomainLabel
			}
			result := tx.Model(&model.ResourceProvider{}).Where("id = ? AND row_version = ?", id, rowVersion).Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return classifyOrganizationMiss(tx, scopeType, id)
			}
			var item model.ResourceProvider
			if err := tx.First(&item, "id = ?", id).Error; err != nil {
				return err
			}
			response = providerOrganizationMutationView(&item)
			return recordAuditLogStrictWithDB(c.Request.Context(), tx, c, "update_"+string(scopeType), "organization", id, id, gin.H{"scope_type": scopeType, "name": request.Name, "old_domain_label": current.DomainLabel, "domain_label": newDomainLabel, "domain_count": domainChange.DomainCount, "domain_examples": domainChange.Examples, "reason": request.Reason, "revision": response.Revision, "row_version": response.RowVersion})
		} else {
			result := tx.Model(&model.Tenant{}).Where("id = ? AND row_version = ?", id, rowVersion).Updates(map[string]any{"name": request.Name, "revision": gorm.Expr("revision + 1"), "row_version": gorm.Expr("row_version + 1")})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return classifyOrganizationMiss(tx, scopeType, id)
			}
			var item model.Tenant
			if err := tx.First(&item, "id = ?", id).Error; err != nil {
				return err
			}
			response = tenantOrganizationMutationView(&item)
		}
		return recordAuditLogStrictWithDB(c.Request.Context(), tx, c, "update_"+string(scopeType), "organization", id, id, gin.H{"scope_type": scopeType, "name": request.Name, "reason": request.Reason, "revision": response.Revision, "row_version": response.RowVersion})
	})
	if err != nil {
		writePlatformOrganizationMutationError(c, err)
		return
	}
	SetRevisionETag(c, response.RowVersion)
	c.JSON(http.StatusOK, NewSuccessResponse(response))
}

func (a *PlatformGovernanceAPI) transitionOrganization(c *gin.Context, scopeType model.ManagementScopeType, action string) {
	identity, ok := currentUnifiedManagementIdentity(c)
	if !ok {
		return
	}
	rowVersion, ok := requiredRevision(c)
	if !ok {
		codedError(c, http.StatusPreconditionRequired, ErrorCodePreconditionRequired, "必须提供 If-Match revision")
		return
	}
	body, ok := readSimulationJSON(c)
	if !ok {
		return
	}
	var request transitionPlatformOrganizationRequest
	if !decodeSimulationJSON(c, body, &request) {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len(request.Reason) > 500 {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "状态变更原因无效")
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	executeIdempotentOrganizationMutation(c, identity.UserID, body, http.StatusOK, func(tx *gorm.DB) (platformOrganizationMutationResponse, error) {
		var response platformOrganizationMutationResponse
		targetStatus := "active"
		expectedStatus := "suspended"
		if action == "suspend" {
			targetStatus, expectedStatus = "suspended", "active"
		}
		if scopeType == model.ManagementScopeProvider {
			result := tx.Model(&model.ResourceProvider{}).Where("id = ? AND row_version = ? AND status = ?", id, rowVersion, expectedStatus).Updates(map[string]any{"status": targetStatus, "revision": gorm.Expr("revision + 1"), "row_version": gorm.Expr("row_version + 1")})
			if result.Error != nil {
				return response, result.Error
			}
			if result.RowsAffected != 1 {
				return response, classifyOrganizationStateMiss(tx, scopeType, id, rowVersion)
			}
			var item model.ResourceProvider
			if err := tx.First(&item, "id = ?", id).Error; err != nil {
				return response, err
			}
			response = providerOrganizationMutationView(&item)
		} else {
			result := tx.Model(&model.Tenant{}).Where("id = ? AND row_version = ? AND status = ?", id, rowVersion, expectedStatus).Updates(map[string]any{"status": targetStatus, "revision": gorm.Expr("revision + 1"), "row_version": gorm.Expr("row_version + 1")})
			if result.Error != nil {
				return response, result.Error
			}
			if result.RowsAffected != 1 {
				return response, classifyOrganizationStateMiss(tx, scopeType, id, rowVersion)
			}
			var item model.Tenant
			if err := tx.First(&item, "id = ?", id).Error; err != nil {
				return response, err
			}
			response = tenantOrganizationMutationView(&item)
		}
		err := recordAuditLogStrictWithDB(c.Request.Context(), tx, c, action+"_"+string(scopeType), "organization", id, id, gin.H{"scope_type": scopeType, "reason": request.Reason, "status": targetStatus, "revision": response.Revision, "row_version": response.RowVersion})
		return response, err
	})
}

func executeIdempotentOrganizationMutation(c *gin.Context, userID uint64, body []byte, status int, mutate func(*gorm.DB) (platformOrganizationMutationResponse, error)) {
	route := c.FullPath()
	idempotency := service.NewAPIIdempotencyService(db.DB, map[string]service.JSONFieldPolicy{http.MethodPost + " " + route: service.NewJSONFieldPolicy("success", "data")}, 5*time.Minute, 24*time.Hour)
	begin, err := idempotency.Begin(c.Request.Context(), service.BeginIdempotencyInput{ActorType: "user", ActorID: strconv.FormatUint(userID, 10), ScopeType: string(model.ManagementScopePlatform), ScopeID: "global", Method: http.MethodPost, Route: route, Key: singleSafeHeader(c, HeaderIdempotencyKey, 128), Body: body})
	if err != nil {
		writeSimulationIdempotencyError(c, err)
		return
	}
	if begin.Replay {
		c.Data(begin.Record.ResponseStatus, "application/json; charset=utf-8", []byte(begin.Record.ResponseBody))
		return
	}
	var response platformOrganizationMutationResponse
	var responseBody []byte
	err = db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var err error
		response, err = mutate(tx)
		if err != nil {
			return err
		}
		responseBody, err = json.Marshal(NewSuccessResponse(response))
		if err != nil {
			return err
		}
		_, err = idempotency.Complete(tx, service.CompleteIdempotencyInput{RecordID: begin.Record.ID, RequestHash: begin.Record.RequestHash, Status: model.APIIdempotencyCompleted, ResponseStatus: status, ResponseBody: responseBody})
		return err
	})
	if err != nil {
		writePlatformOrganizationMutationError(c, err)
		return
	}
	SetRevisionETag(c, response.RowVersion)
	c.Data(status, "application/json; charset=utf-8", responseBody)
}

func validOrganizationMutationFields(key, name, reason string) bool {
	return organizationKeyPattern.MatchString(key) && len(name) > 0 && len(name) <= 200 && len(reason) > 0 && len(reason) <= 500
}

func classifyOrganizationMiss(tx *gorm.DB, scopeType model.ManagementScopeType, id string) error {
	var count int64
	if scopeType == model.ManagementScopeProvider {
		if err := tx.Model(&model.ResourceProvider{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return err
		}
	} else {
		if err := tx.Model(&model.Tenant{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return err
		}
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return errPlatformOrganizationVersionConflict
}

func classifyOrganizationStateMiss(tx *gorm.DB, scopeType model.ManagementScopeType, id string, rowVersion int64) error {
	err := classifyOrganizationMiss(tx, scopeType, id)
	if !errors.Is(err, errPlatformOrganizationVersionConflict) {
		return err
	}
	var count int64
	if scopeType == model.ManagementScopeProvider {
		if err := tx.Model(&model.ResourceProvider{}).Where("id = ? AND row_version = ?", id, rowVersion).Count(&count).Error; err != nil {
			return err
		}
	} else {
		if err := tx.Model(&model.Tenant{}).Where("id = ? AND row_version = ?", id, rowVersion).Count(&count).Error; err != nil {
			return err
		}
	}
	if count == 0 {
		return errPlatformOrganizationVersionConflict
	}
	return errPlatformOrganizationStateConflict
}

func providerOrganizationMutationView(item *model.ResourceProvider) platformOrganizationMutationResponse {
	return platformOrganizationMutationResponse{ID: item.ID, ScopeType: "provider", Key: item.Key, Name: item.DisplayName, DomainLabel: item.DomainLabel, Status: string(item.Status), Revision: item.Revision, RowVersion: item.RowVersion, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}
func tenantOrganizationMutationView(item *model.Tenant) platformOrganizationMutationResponse {
	return platformOrganizationMutationResponse{ID: item.ID, ScopeType: "tenant", Key: item.Key, Name: item.Name, Status: string(item.Status), Revision: item.Revision, RowVersion: item.RowVersion, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

func writePlatformOrganizationMutationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errPlatformOrganizationVersionConflict):
		codedError(c, http.StatusConflict, "PLATFORM_ORGANIZATION_VERSION_CONFLICT", "组织已被其他操作更新")
	case errors.Is(err, errPlatformOrganizationStateConflict):
		codedError(c, http.StatusConflict, "PLATFORM_ORGANIZATION_STATE_CONFLICT", "组织当前状态不允许该操作")
	case errors.Is(err, gorm.ErrRecordNotFound):
		codedError(c, http.StatusNotFound, ErrorCodeManagementObjectMissing, "组织不存在")
	case errors.Is(err, service.ErrProviderDomainLabelInvalid):
		codedError(c, http.StatusBadRequest, "PROVIDER_DOMAIN_LABEL_INVALID", "域名标识格式无效或属于系统保留名称")
	case errors.Is(err, service.ErrProviderDomainConfirmation):
		codedError(c, http.StatusBadRequest, "PROVIDER_DOMAIN_CONFIRMATION_MISMATCH", "新域名标识二次确认不一致")
	case errors.Is(err, service.ErrProviderDomainLabelExists):
		codedError(c, http.StatusConflict, "PROVIDER_DOMAIN_LABEL_EXISTS", "域名标识已被使用")
	case strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate"):
		codedError(c, http.StatusConflict, "PLATFORM_ORGANIZATION_KEY_OR_DOMAIN_EXISTS", "组织标识或域名标识已存在")
	default:
		codedError(c, http.StatusInternalServerError, "PLATFORM_ORGANIZATION_WRITE_FAILED", "组织写入失败")
	}
}
