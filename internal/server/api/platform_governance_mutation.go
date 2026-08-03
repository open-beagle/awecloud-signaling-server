package api

import (
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
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

const platformMembershipVersionConflict = "PLATFORM_MEMBERSHIP_VERSION_CONFLICT"

type createPlatformMembershipRequest struct {
	UserID    uint64     `json:"user_id"`
	Role      string     `json:"role"`
	ValidFrom time.Time  `json:"valid_from"`
	ExpiresAt *time.Time `json:"expires_at"`
	Reason    string     `json:"reason"`
}

type updatePlatformMembershipRequest struct {
	Role      *string    `json:"role"`
	Enabled   *bool      `json:"enabled"`
	ValidFrom *time.Time `json:"valid_from"`
	ExpiresAt *time.Time `json:"expires_at"`
	Reason    string     `json:"reason"`
}

type platformMembershipMutationResponse struct {
	ID                 string     `json:"id"`
	ScopeType          string     `json:"scope_type"`
	ScopeID            string     `json:"scope_id"`
	UserID             uint64     `json:"user_id"`
	Role               string     `json:"role"`
	Enabled            bool       `json:"enabled"`
	ValidFrom          time.Time  `json:"valid_from"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	PermissionRevision int64      `json:"permission_revision"`
	Reason             string     `json:"reason"`
	RowVersion         int64      `json:"row_version"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func (a *PlatformGovernanceAPI) CreateProviderMembership(c *gin.Context) {
	a.createMembership(c, model.ManagementScopeProvider)
}

func (a *PlatformGovernanceAPI) CreateTenantMembership(c *gin.Context) {
	a.createMembership(c, model.ManagementScopeTenant)
}

func (a *PlatformGovernanceAPI) UpdateProviderMembership(c *gin.Context) {
	a.updateMembership(c, model.ManagementScopeProvider)
}

func (a *PlatformGovernanceAPI) UpdateTenantMembership(c *gin.Context) {
	a.updateMembership(c, model.ManagementScopeTenant)
}

func (a *PlatformGovernanceAPI) createMembership(c *gin.Context, scopeType model.ManagementScopeType) {
	identity, ok := currentUnifiedManagementIdentity(c)
	if !ok {
		return
	}
	body, ok := readSimulationJSON(c)
	if !ok {
		return
	}
	var request createPlatformMembershipRequest
	if !decodeSimulationJSON(c, body, &request) {
		return
	}
	request.Role = strings.TrimSpace(request.Role)
	request.Reason = strings.TrimSpace(request.Reason)
	now := time.Now()
	if request.ValidFrom.IsZero() {
		request.ValidFrom = now
	}
	if request.UserID == 0 || request.Reason == "" || len(request.Reason) > 500 || !validScopedManagementRole(scopeType, request.Role) ||
		(request.ExpiresAt != nil && !request.ExpiresAt.After(request.ValidFrom)) ||
		(request.UserID == identity.UserID && (request.ExpiresAt == nil || !request.ExpiresAt.After(now))) {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "管理授权参数、原因或有效期无效")
		return
	}
	scopeID := strings.TrimSpace(c.Param("id"))
	if !platformManagementMutationTargetsExist(c, scopeType, scopeID, request.UserID) {
		return
	}

	route := c.FullPath()
	idempotency := service.NewAPIIdempotencyService(db.DB, map[string]service.JSONFieldPolicy{
		http.MethodPost + " " + route: service.NewJSONFieldPolicy("success", "data"),
	}, 5*time.Minute, 24*time.Hour)
	begin, err := idempotency.Begin(c.Request.Context(), service.BeginIdempotencyInput{
		ActorType: "user", ActorID: strconv.FormatUint(identity.UserID, 10), ScopeType: string(model.ManagementScopePlatform),
		ScopeID: "global", Method: http.MethodPost, Route: route, Key: singleSafeHeader(c, HeaderIdempotencyKey, 128), Body: body,
	})
	if err != nil {
		writeSimulationIdempotencyError(c, err)
		return
	}
	if begin.Replay {
		var response struct {
			Data platformMembershipMutationResponse `json:"data"`
		}
		if json.Unmarshal([]byte(begin.Record.ResponseBody), &response) == nil && response.Data.RowVersion > 0 {
			SetRevisionETag(c, response.Data.RowVersion)
		}
		c.Data(begin.Record.ResponseStatus, "application/json; charset=utf-8", []byte(begin.Record.ResponseBody))
		return
	}

	membershipID := uuid.NewString()
	var response platformMembershipMutationResponse
	var responseBody []byte
	err = db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if scopeType == model.ManagementScopeProvider {
			membership := &model.AdminProviderMembership{
				ID: membershipID, UserID: request.UserID, ProviderID: scopeID, Role: model.ProviderManagementRole(request.Role),
				Enabled: true, ValidFrom: request.ValidFrom, ExpiresAt: request.ExpiresAt, PermissionRevision: 1,
				CreatedByUserID: identity.UserID, Reason: request.Reason, RowVersion: 1,
			}
			if err := tx.Create(membership).Error; err != nil {
				return err
			}
			response = providerMembershipMutationView(membership)
		} else {
			membership := &model.UserTenantManagementMembership{
				ID: membershipID, UserID: request.UserID, TenantID: scopeID, Role: model.TenantManagementRole(request.Role),
				Enabled: true, ValidFrom: request.ValidFrom, ExpiresAt: request.ExpiresAt, PermissionRevision: 1,
				CreatedByUserID: identity.UserID, Reason: request.Reason, RowVersion: 1,
			}
			if err := tx.Create(membership).Error; err != nil {
				return err
			}
			response = tenantMembershipMutationView(membership)
		}
		var err error
		responseBody, err = json.Marshal(NewSuccessResponse(response))
		if err != nil {
			return err
		}
		action := "create_" + string(scopeType) + "_management_membership"
		if request.UserID == identity.UserID {
			action = "self_authorization_" + string(scopeType) + "_management_membership"
		}
		if err := recordAuditLogStrictWithDB(c.Request.Context(), tx, c, action, "management_membership", membershipID, membershipID, gin.H{
			"scope_type": scopeType, "scope_id": scopeID, "user_id": request.UserID, "role": request.Role,
			"valid_from": request.ValidFrom, "expires_at": request.ExpiresAt, "reason": request.Reason,
		}); err != nil {
			return err
		}
		_, err = idempotency.Complete(tx, service.CompleteIdempotencyInput{
			RecordID: begin.Record.ID, RequestHash: begin.Record.RequestHash, Status: model.APIIdempotencyCompleted,
			ResponseStatus: http.StatusCreated, ResponseBody: responseBody,
		})
		return err
	})
	if err != nil {
		writePlatformMembershipMutationError(c, err)
		return
	}
	SetRevisionETag(c, response.RowVersion)
	c.Data(http.StatusCreated, "application/json; charset=utf-8", responseBody)
}

func (a *PlatformGovernanceAPI) updateMembership(c *gin.Context, scopeType model.ManagementScopeType) {
	rowVersion, ok := requiredRevision(c)
	if !ok {
		codedError(c, http.StatusPreconditionRequired, ErrorCodePreconditionRequired, "必须提供 If-Match revision")
		return
	}
	body, ok := readSimulationJSON(c)
	if !ok {
		return
	}
	var request updatePlatformMembershipRequest
	if !decodeSimulationJSON(c, body, &request) {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len(request.Reason) > 500 || (request.Role == nil && request.Enabled == nil && request.ValidFrom == nil && request.ExpiresAt == nil) {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "管理授权变更内容或原因无效")
		return
	}
	if request.Role != nil {
		trimmed := strings.TrimSpace(*request.Role)
		request.Role = &trimmed
		if !validScopedManagementRole(scopeType, trimmed) {
			codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "管理授权角色无效")
			return
		}
	}

	scopeID := strings.TrimSpace(c.Param("id"))
	membershipID := strings.TrimSpace(c.Param("membership_id"))
	var response platformMembershipMutationResponse
	err := db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if scopeType == model.ManagementScopeProvider {
			var membership model.AdminProviderMembership
			if err := tx.Where("id = ? AND provider_id = ?", membershipID, scopeID).First(&membership).Error; err != nil {
				return err
			}
			if err := applyProviderMembershipUpdate(tx, &membership, request, rowVersion); err != nil {
				return err
			}
			response = providerMembershipMutationView(&membership)
		} else {
			var membership model.UserTenantManagementMembership
			if err := tx.Where("id = ? AND tenant_id = ?", membershipID, scopeID).First(&membership).Error; err != nil {
				return err
			}
			if err := applyTenantMembershipUpdate(tx, &membership, request, rowVersion); err != nil {
				return err
			}
			response = tenantMembershipMutationView(&membership)
		}
		return recordAuditLogStrictWithDB(c.Request.Context(), tx, c, "update_"+string(scopeType)+"_management_membership", "management_membership", membershipID, membershipID, gin.H{
			"scope_type": scopeType, "scope_id": scopeID, "role": request.Role, "enabled": request.Enabled,
			"valid_from": request.ValidFrom, "expires_at": request.ExpiresAt, "reason": request.Reason,
			"row_version": response.RowVersion, "permission_revision": response.PermissionRevision,
		})
	})
	if err != nil {
		writePlatformMembershipMutationError(c, err)
		return
	}
	SetRevisionETag(c, response.RowVersion)
	c.JSON(http.StatusOK, NewSuccessResponse(response))
}

func validScopedManagementRole(scopeType model.ManagementScopeType, role string) bool {
	if scopeType == model.ManagementScopeProvider {
		return role == string(model.ProviderManagementRoleAdmin) || role == string(model.ProviderManagementRoleOperator) || role == string(model.ProviderManagementRoleViewer)
	}
	return role == string(model.TenantManagementRoleAdmin) || role == string(model.TenantManagementRoleSecurityAuditor) || role == string(model.TenantManagementRoleViewer)
}

func platformManagementMutationTargetsExist(c *gin.Context, scopeType model.ManagementScopeType, scopeID string, userID uint64) bool {
	if scopeID == "" {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "目标 Scope 无效")
		return false
	}
	var userCount, scopeCount int64
	if err := db.DB.WithContext(c.Request.Context()).Model(&model.User{}).Where("id = ? AND enabled = ?", userID, true).Count(&userCount).Error; err != nil {
		codedError(c, http.StatusInternalServerError, "PLATFORM_MEMBERSHIP_TARGET_QUERY_FAILED", "校验目标 User 失败")
		return false
	}
	var scopeQuery *gorm.DB
	if scopeType == model.ManagementScopeProvider {
		scopeQuery = db.DB.WithContext(c.Request.Context()).Model(&model.ResourceProvider{}).Where("id = ?", scopeID).Count(&scopeCount)
	} else {
		scopeQuery = db.DB.WithContext(c.Request.Context()).Model(&model.Tenant{}).Where("id = ?", scopeID).Count(&scopeCount)
	}
	if scopeQuery.Error != nil {
		codedError(c, http.StatusInternalServerError, "PLATFORM_MEMBERSHIP_TARGET_QUERY_FAILED", "校验目标组织失败")
		return false
	}
	if userCount != 1 || scopeCount != 1 {
		codedError(c, http.StatusNotFound, ErrorCodeManagementObjectMissing, "目标 User 或组织不存在")
		return false
	}
	return true
}

func applyProviderMembershipUpdate(tx *gorm.DB, membership *model.AdminProviderMembership, request updatePlatformMembershipRequest, rowVersion int64) error {
	updates, err := platformMembershipUpdates(string(membership.Role), membership.ValidFrom, membership.ExpiresAt, request, rowVersion, membership.PermissionRevision)
	if err != nil {
		return err
	}
	result := tx.Model(&model.AdminProviderMembership{}).Where("id = ? AND row_version = ?", membership.ID, rowVersion).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errPlatformMembershipVersionConflict
	}
	return tx.First(membership, "id = ?", membership.ID).Error
}

func applyTenantMembershipUpdate(tx *gorm.DB, membership *model.UserTenantManagementMembership, request updatePlatformMembershipRequest, rowVersion int64) error {
	updates, err := platformMembershipUpdates(string(membership.Role), membership.ValidFrom, membership.ExpiresAt, request, rowVersion, membership.PermissionRevision)
	if err != nil {
		return err
	}
	result := tx.Model(&model.UserTenantManagementMembership{}).Where("id = ? AND row_version = ?", membership.ID, rowVersion).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errPlatformMembershipVersionConflict
	}
	return tx.First(membership, "id = ?", membership.ID).Error
}

var errPlatformMembershipVersionConflict = errors.New("platform management membership version conflict")

func platformMembershipUpdates(currentRole string, currentValidFrom time.Time, currentExpiresAt *time.Time, request updatePlatformMembershipRequest, rowVersion, permissionRevision int64) (map[string]any, error) {
	role := currentRole
	validFrom := currentValidFrom
	expiresAt := currentExpiresAt
	updates := map[string]any{"reason": request.Reason, "row_version": rowVersion + 1, "permission_revision": permissionRevision + 1}
	if request.Role != nil {
		role = *request.Role
		updates["role"] = role
	}
	if request.Enabled != nil {
		updates["enabled"] = *request.Enabled
	}
	if request.ValidFrom != nil {
		validFrom = *request.ValidFrom
		updates["valid_from"] = validFrom
	}
	if request.ExpiresAt != nil {
		expiresAt = request.ExpiresAt
		updates["expires_at"] = request.ExpiresAt
	}
	if role == "" || validFrom.IsZero() || (expiresAt != nil && !expiresAt.After(validFrom)) {
		return nil, errInvalidPlatformMembershipUpdate
	}
	return updates, nil
}

var errInvalidPlatformMembershipUpdate = errors.New("invalid platform membership update")

func providerMembershipMutationView(membership *model.AdminProviderMembership) platformMembershipMutationResponse {
	return platformMembershipMutationResponse{
		ID: membership.ID, ScopeType: string(model.ManagementScopeProvider), ScopeID: membership.ProviderID,
		UserID: membership.UserID, Role: string(membership.Role), Enabled: membership.Enabled, ValidFrom: membership.ValidFrom,
		ExpiresAt: membership.ExpiresAt, PermissionRevision: membership.PermissionRevision, Reason: membership.Reason,
		RowVersion: membership.RowVersion, CreatedAt: membership.CreatedAt, UpdatedAt: membership.UpdatedAt,
	}
}

func tenantMembershipMutationView(membership *model.UserTenantManagementMembership) platformMembershipMutationResponse {
	return platformMembershipMutationResponse{
		ID: membership.ID, ScopeType: string(model.ManagementScopeTenant), ScopeID: membership.TenantID,
		UserID: membership.UserID, Role: string(membership.Role), Enabled: membership.Enabled, ValidFrom: membership.ValidFrom,
		ExpiresAt: membership.ExpiresAt, PermissionRevision: membership.PermissionRevision, Reason: membership.Reason,
		RowVersion: membership.RowVersion, CreatedAt: membership.CreatedAt, UpdatedAt: membership.UpdatedAt,
	}
}

func writePlatformMembershipMutationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errPlatformMembershipVersionConflict):
		codedError(c, http.StatusConflict, platformMembershipVersionConflict, "管理授权已被其他操作更新")
	case errors.Is(err, errInvalidPlatformMembershipUpdate):
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "管理授权有效期无效")
	case errors.Is(err, gorm.ErrRecordNotFound):
		codedError(c, http.StatusNotFound, ErrorCodeManagementObjectMissing, "管理授权不存在")
	case strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate"):
		codedError(c, http.StatusConflict, "PLATFORM_MEMBERSHIP_EXISTS", "目标 User 已有该组织的管理授权")
	default:
		codedError(c, http.StatusInternalServerError, "PLATFORM_MEMBERSHIP_WRITE_FAILED", "管理授权写入失败")
	}
}
