package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var (
	ErrTenantGrantInvalidInput      = errors.New("invalid Tenant grant input")
	ErrTenantGrantNotFound          = errors.New("Tenant grant not found")
	ErrTenantGrantVersionConflict   = errors.New("Tenant grant version conflict")
	ErrTenantGrantStateTransition   = errors.New("Tenant grant state transition is invalid")
	ErrTenantGrantActionUnsupported = errors.New("Tenant grant action is unsupported")
	ErrTenantGrantSubjectInvalid    = errors.New("Tenant grant subject is invalid")
	ErrTenantGrantTimeInvalid       = errors.New("Tenant grant time window is invalid")
	ErrTenantGrantConflict          = errors.New("an active Tenant grant already exists")
)

type TenantGrantView struct {
	ID                string                             `json:"id"`
	ResourceID        string                             `json:"resource_id"`
	ResourceName      string                             `json:"resource_name,omitempty"`
	SubjectType       model.TenantAccessGrantSubjectType `json:"subject_type"`
	SubjectUserID     *uint64                            `json:"subject_user_id,omitempty"`
	SubjectGroupID    *int64                             `json:"subject_group_id,omitempty"`
	SubjectName       string                             `json:"subject_name,omitempty"`
	Actions           []string                           `json:"actions"`
	ValidFrom         time.Time                          `json:"valid_from"`
	ExpiresAt         *time.Time                         `json:"expires_at,omitempty"`
	MaxSessionSeconds int                                `json:"max_session_seconds"`
	Status            model.TenantAccessGrantStatus      `json:"status"`
	Revision          int64                              `json:"revision"`
	RowVersion        int64                              `json:"row_version"`
	RevokedAt         *time.Time                         `json:"revoked_at,omitempty"`
	RevokeReason      string                             `json:"revoke_reason,omitempty"`
	CreatedAt         time.Time                          `json:"created_at"`
	UpdatedAt         time.Time                          `json:"updated_at"`
}

type TenantGrantListInput struct {
	ResourceID  string
	SubjectType string
	Status      string
	Page        int
	PageSize    int
}

type TenantGrantListResult struct {
	Items []TenantGrantView
	Total int64
}

type TenantAccessGrantService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewTenantAccessGrantService(database *gorm.DB) *TenantAccessGrantService {
	return &TenantAccessGrantService{db: database, now: time.Now}
}

func (s *TenantAccessGrantService) View(grant *model.TenantAccessGrant) (*TenantGrantView, error) {
	if grant == nil {
		return nil, ErrTenantGrantInvalidInput
	}
	return tenantGrantView(grant)
}

func (s *TenantAccessGrantService) List(ctx context.Context, authorization *ManagementAuthorizationContext, tenantID string, input TenantGrantListInput) (*TenantGrantListResult, error) {
	if s == nil || s.db == nil {
		return nil, ErrTenantGrantInvalidInput
	}
	tenantID = strings.TrimSpace(tenantID)
	input.ResourceID, input.SubjectType, input.Status = strings.TrimSpace(input.ResourceID), strings.TrimSpace(input.SubjectType), strings.TrimSpace(input.Status)
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PageSize == 0 {
		input.PageSize = 20
	}
	if validateRequired("tenant_id", tenantID, 36) != nil || input.Page < 1 || input.PageSize < 1 || input.PageSize > 100 || len(input.ResourceID) > 36 {
		return nil, ErrTenantGrantInvalidInput
	}
	if input.SubjectType != "" && input.SubjectType != string(model.TenantAccessGrantSubjectUser) && input.SubjectType != string(model.TenantAccessGrantSubjectGroup) {
		return nil, ErrTenantGrantInvalidInput
	}
	if input.Status != "" && !validTenantGrantStatus(model.TenantAccessGrantStatus(input.Status)) {
		return nil, ErrTenantGrantInvalidInput
	}
	now := s.now().UTC()
	if err := reauthorizeTenantPermission(s.db.WithContext(ctx), authorization, tenantID, PermissionTenantGrantsRead, now); err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Model(&model.TenantAccessGrant{}).Where("tenant_id = ?", tenantID)
	if input.ResourceID != "" {
		query = query.Where("tenant_resource_id = ?", input.ResourceID)
	}
	if input.SubjectType != "" {
		query = query.Where("subject_type = ?", input.SubjectType)
	}
	if input.Status != "" {
		query = query.Where("status = ?", input.Status)
	}
	result := &TenantGrantListResult{Items: []TenantGrantView{}}
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	var grants []model.TenantAccessGrant
	if err := query.Order("created_at DESC, id DESC").Offset((input.Page - 1) * input.PageSize).Limit(input.PageSize).Find(&grants).Error; err != nil {
		return nil, err
	}
	result.Items = make([]TenantGrantView, 0, len(grants))
	for i := range grants {
		view, err := tenantGrantView(&grants[i])
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, *view)
	}
	hostGrants, hostTotal, err := s.listUnifiedHostSSHGrants(ctx, tenantID, input, now)
	if err != nil {
		return nil, err
	}
	result.Total += hostTotal
	if len(result.Items) < input.PageSize {
		remaining := input.PageSize - len(result.Items)
		if len(hostGrants) > remaining {
			hostGrants = hostGrants[:remaining]
		}
		result.Items = append(result.Items, hostGrants...)
	}
	if err := s.enrichGrantViews(ctx, tenantID, result.Items); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *TenantAccessGrantService) enrichGrantViews(ctx context.Context, tenantID string, views []TenantGrantView) error {
	resourceIDs := make([]string, 0, len(views))
	userIDs := make([]uint64, 0, len(views))
	groupIDs := make([]int64, 0, len(views))
	for i := range views {
		resourceIDs = append(resourceIDs, views[i].ResourceID)
		if views[i].SubjectUserID != nil {
			userIDs = append(userIDs, *views[i].SubjectUserID)
		}
		if views[i].SubjectGroupID != nil {
			groupIDs = append(groupIDs, *views[i].SubjectGroupID)
		}
	}
	resourceNames := make(map[string]string, len(resourceIDs))
	if len(resourceIDs) > 0 {
		var tenantResources []struct {
			ID          string
			DisplayName string
		}
		if err := s.db.WithContext(ctx).Model(&model.TenantResource{}).
			Select("id, display_name").Where("tenant_id = ? AND id IN ?", tenantID, resourceIDs).Scan(&tenantResources).Error; err != nil {
			return err
		}
		for _, resource := range tenantResources {
			resourceNames[resource.ID] = resource.DisplayName
		}
		var legacyResources []struct {
			ID          string
			DisplayName string
		}
		if err := s.db.WithContext(ctx).Model(&model.Resource{}).
			Select("id, display_name").Where("tenant_id = ? AND id IN ?", tenantID, resourceIDs).Scan(&legacyResources).Error; err != nil {
			return err
		}
		for _, resource := range legacyResources {
			resourceNames[resource.ID] = resource.DisplayName
		}
	}
	userNames := make(map[uint64]string, len(userIDs))
	if len(userIDs) > 0 {
		var users []model.User
		if err := s.db.WithContext(ctx).Select("id, name, alias").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
			return err
		}
		for _, user := range users {
			userNames[user.ID] = firstNonEmpty(user.Alias, user.Name)
		}
	}
	groupNames := make(map[int64]string, len(groupIDs))
	if len(groupIDs) > 0 {
		var groups []model.Group
		if err := s.db.WithContext(ctx).Select("id, name, alias").Where("tenant_id = ? AND id IN ?", tenantID, groupIDs).Find(&groups).Error; err != nil {
			return err
		}
		for _, group := range groups {
			groupNames[group.ID] = firstNonEmpty(group.Alias, group.Name)
		}
	}
	for i := range views {
		views[i].ResourceName = resourceNames[views[i].ResourceID]
		if views[i].SubjectUserID != nil {
			views[i].SubjectName = userNames[*views[i].SubjectUserID]
		} else if views[i].SubjectGroupID != nil {
			views[i].SubjectName = groupNames[*views[i].SubjectGroupID]
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func (s *TenantAccessGrantService) listUnifiedHostSSHGrants(ctx context.Context, tenantID string, input TenantGrantListInput, now time.Time) ([]TenantGrantView, int64, error) {
	query := s.db.WithContext(ctx).Model(&model.AccessGrant{}).
		Joins("JOIN resource ON resource.id = access_grant.resource_id AND resource.tenant_id = access_grant.tenant_id").
		Where("access_grant.tenant_id = ? AND resource.type = ?", tenantID, model.ResourceTypeHostSSH)
	if input.ResourceID != "" {
		query = query.Where("access_grant.resource_id = ?", input.ResourceID)
	}
	if input.SubjectType != "" {
		query = query.Where("access_grant.subject_type = ?", input.SubjectType)
	}
	if input.Status != "" {
		query = query.Where("access_grant.status = ?", input.Status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	if total == 0 {
		return []TenantGrantView{}, 0, nil
	}
	var grants []model.AccessGrant
	offset := (input.Page - 1) * input.PageSize
	if err := query.Order("access_grant.created_at DESC, access_grant.id DESC").Offset(offset).Limit(input.PageSize).Find(&grants).Error; err != nil {
		return nil, 0, err
	}
	views := make([]TenantGrantView, 0, len(grants))
	for i := range grants {
		view, err := unifiedHostSSHGrantView(&grants[i], now)
		if err != nil {
			return nil, 0, err
		}
		views = append(views, *view)
	}
	return views, total, nil
}

func (s *TenantAccessGrantService) Get(ctx context.Context, authorization *ManagementAuthorizationContext, tenantID, grantID string) (*TenantGrantView, error) {
	if s == nil || s.db == nil {
		return nil, ErrTenantGrantInvalidInput
	}
	tenantID, grantID = strings.TrimSpace(tenantID), strings.TrimSpace(grantID)
	if validateRequired("tenant_id", tenantID, 36) != nil || validateRequired("grant_id", grantID, 36) != nil {
		return nil, ErrTenantGrantInvalidInput
	}
	if err := reauthorizeTenantPermission(s.db.WithContext(ctx), authorization, tenantID, PermissionTenantGrantsRead, s.now().UTC()); err != nil {
		return nil, err
	}
	grant, err := loadTenantGrant(s.db.WithContext(ctx), tenantID, grantID)
	if err != nil {
		return nil, err
	}
	view, err := tenantGrantView(grant)
	if err != nil {
		return nil, err
	}
	enriched := []TenantGrantView{*view}
	if err := s.enrichGrantViews(ctx, tenantID, enriched); err != nil {
		return nil, err
	}
	return &enriched[0], nil
}

type CreateTenantGrantInput struct {
	TenantID          string
	ResourceID        string
	SubjectType       model.TenantAccessGrantSubjectType
	SubjectUserID     *uint64
	SubjectGroupID    *int64
	Actions           []string
	ValidFrom         time.Time
	ExpiresAt         *time.Time
	MaxSessionSeconds int
	RequestID         string
}

func (s *TenantAccessGrantService) Create(ctx context.Context, authorization *ManagementAuthorizationContext, input CreateTenantGrantInput) (*model.TenantAccessGrant, error) {
	if s == nil || s.db == nil {
		return nil, ErrTenantGrantInvalidInput
	}
	input.TenantID, input.ResourceID, input.RequestID = strings.TrimSpace(input.TenantID), strings.TrimSpace(input.ResourceID), strings.TrimSpace(input.RequestID)
	now := s.now().UTC()
	if validateRequired("tenant_id", input.TenantID, 36) != nil || validateRequired("resource_id", input.ResourceID, 36) != nil || validateRequired("request_id", input.RequestID, 100) != nil {
		return nil, ErrTenantGrantInvalidInput
	}
	if input.ValidFrom.IsZero() {
		input.ValidFrom = now
	}
	input.ValidFrom = input.ValidFrom.UTC()
	if input.ExpiresAt != nil {
		value := input.ExpiresAt.UTC()
		input.ExpiresAt = &value
	}
	if input.MaxSessionSeconds == 0 {
		input.MaxSessionSeconds = 8 * 60 * 60
	}
	if err := validateTenantGrantWindow(input.ValidFrom, input.ExpiresAt, input.MaxSessionSeconds, now); err != nil {
		return nil, err
	}
	actions, actionsJSON, err := normalizeTenantGrantActions(input.Actions)
	if err != nil {
		return nil, err
	}
	input.Actions = actions

	var grant model.TenantAccessGrant
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := reauthorizeTenantPermission(tx, authorization, input.TenantID, PermissionTenantGrantsWrite, now); err != nil {
			return err
		}
		chain, err := loadTenantResourceChain(tx, input.TenantID, input.ResourceID, now, true)
		if err != nil {
			return err
		}
		if chain.Resource.VisibilityState != model.TenantResourceVisible {
			return ErrTenantResourceUpstreamUnavailable
		}
		if err := validateActionsForResource(chain.Resource.Type, actions); err != nil {
			return err
		}
		subjectKey, err := validateTenantGrantSubject(tx, input.TenantID, input.SubjectType, input.SubjectUserID, input.SubjectGroupID, tenantGrantSubjectValidationTime(now, input.ValidFrom))
		if err != nil {
			return err
		}
		grant = model.TenantAccessGrant{
			ID: uuid.NewString(), TenantID: input.TenantID, TenantResourceID: input.ResourceID,
			SubjectType: input.SubjectType, SubjectKey: subjectKey, SubjectUserID: input.SubjectUserID, SubjectGroupID: input.SubjectGroupID,
			Actions: actionsJSON, ValidFrom: input.ValidFrom, ExpiresAt: input.ExpiresAt, MaxSessionSeconds: input.MaxSessionSeconds,
			Status: model.TenantAccessGrantEnabled, Revision: 1, RowVersion: 1, CreatedByUserID: authorization.EffectiveUserID,
		}
		if err := tx.Create(&grant).Error; err != nil {
			return mapTenantGrantConstraint(err)
		}
		return createTenantGrantEvent(tx, authorization, &grant, "created", input.RequestID, "", now)
	})
	return &grant, err
}

type UpdateTenantGrantInput struct {
	TenantID           string
	GrantID            string
	ExpectedRowVersion int64
	Actions            *[]string
	ValidFrom          *time.Time
	ExpiresAt          *time.Time
	SetExpiresAt       bool
	MaxSessionSeconds  *int
	RequestID          string
}

func (s *TenantAccessGrantService) Update(ctx context.Context, authorization *ManagementAuthorizationContext, input UpdateTenantGrantInput) (*model.TenantAccessGrant, error) {
	if s == nil || s.db == nil || input.Actions == nil && input.ValidFrom == nil && !input.SetExpiresAt && input.MaxSessionSeconds == nil {
		return nil, ErrTenantGrantInvalidInput
	}
	input.TenantID, input.GrantID, input.RequestID = strings.TrimSpace(input.TenantID), strings.TrimSpace(input.GrantID), strings.TrimSpace(input.RequestID)
	if validateRequired("tenant_id", input.TenantID, 36) != nil || validateRequired("grant_id", input.GrantID, 36) != nil || input.ExpectedRowVersion <= 0 || validateRequired("request_id", input.RequestID, 100) != nil {
		return nil, ErrTenantGrantInvalidInput
	}
	now := s.now().UTC()
	var grant model.TenantAccessGrant
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := reauthorizeTenantPermission(tx, authorization, input.TenantID, PermissionTenantGrantsWrite, now); err != nil {
			return err
		}
		current, err := loadTenantGrant(tx, input.TenantID, input.GrantID)
		if err != nil {
			return err
		}
		if current.RowVersion != input.ExpectedRowVersion {
			return ErrTenantGrantVersionConflict
		}
		if current.Status != model.TenantAccessGrantEnabled && current.Status != model.TenantAccessGrantSuspended {
			return ErrTenantGrantStateTransition
		}
		validFrom, expiresAt, maxSeconds := current.ValidFrom, current.ExpiresAt, current.MaxSessionSeconds
		updates := map[string]any{"revision": gorm.Expr("revision + 1"), "row_version": gorm.Expr("row_version + 1")}
		if input.Actions != nil {
			actions, encoded, err := normalizeTenantGrantActions(*input.Actions)
			if err != nil {
				return err
			}
			var resource model.TenantResource
			if err := tx.Where("tenant_id = ? AND id = ?", input.TenantID, current.TenantResourceID).First(&resource).Error; err != nil {
				return ErrTenantResourceNotFound
			}
			if err := validateActionsForResource(resource.Type, actions); err != nil {
				return err
			}
			updates["actions"] = encoded
		}
		if input.ValidFrom != nil {
			validFrom = input.ValidFrom.UTC()
			updates["valid_from"] = validFrom
		}
		if input.SetExpiresAt {
			expiresAt = input.ExpiresAt
			if expiresAt != nil {
				value := expiresAt.UTC()
				expiresAt = &value
			}
			updates["expires_at"] = expiresAt
		}
		if input.MaxSessionSeconds != nil {
			maxSeconds = *input.MaxSessionSeconds
			updates["max_session_seconds"] = maxSeconds
		}
		if err := validateTenantGrantWindow(validFrom, expiresAt, maxSeconds, now); err != nil {
			return err
		}
		if _, err := validateTenantGrantSubject(tx, input.TenantID, current.SubjectType, current.SubjectUserID, current.SubjectGroupID, tenantGrantSubjectValidationTime(now, validFrom)); err != nil {
			return err
		}
		result := tx.Model(&model.TenantAccessGrant{}).Where("tenant_id = ? AND id = ? AND row_version = ?", input.TenantID, input.GrantID, input.ExpectedRowVersion).Updates(updates)
		if result.Error != nil {
			return mapTenantGrantConstraint(result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrTenantGrantVersionConflict
		}
		if err := tx.Where("tenant_id = ? AND id = ?", input.TenantID, input.GrantID).First(&grant).Error; err != nil {
			return err
		}
		if err := createTenantGrantEvent(tx, authorization, &grant, "updated", input.RequestID, "", now); err != nil {
			return err
		}
		return endSessionsForGrant(tx, &grant, "GRANT_UPDATED", "grant authorization changed", input.RequestID, now)
	})
	return &grant, err
}

type TenantGrantActionInput struct {
	TenantID           string
	GrantID            string
	ExpectedRowVersion int64
	Reason             string
	RequestID          string
}

func (s *TenantAccessGrantService) Suspend(ctx context.Context, authorization *ManagementAuthorizationContext, input TenantGrantActionInput) (*model.TenantAccessGrant, error) {
	return s.transition(ctx, authorization, input, model.TenantAccessGrantSuspended)
}

func (s *TenantAccessGrantService) Resume(ctx context.Context, authorization *ManagementAuthorizationContext, input TenantGrantActionInput) (*model.TenantAccessGrant, error) {
	return s.transition(ctx, authorization, input, model.TenantAccessGrantEnabled)
}

func (s *TenantAccessGrantService) Revoke(ctx context.Context, authorization *ManagementAuthorizationContext, input TenantGrantActionInput) (*model.TenantAccessGrant, error) {
	return s.transition(ctx, authorization, input, model.TenantAccessGrantRevoked)
}

func (s *TenantAccessGrantService) transition(ctx context.Context, authorization *ManagementAuthorizationContext, input TenantGrantActionInput, target model.TenantAccessGrantStatus) (*model.TenantAccessGrant, error) {
	if s == nil || s.db == nil {
		return nil, ErrTenantGrantInvalidInput
	}
	input.TenantID, input.GrantID, input.Reason, input.RequestID = strings.TrimSpace(input.TenantID), strings.TrimSpace(input.GrantID), strings.TrimSpace(input.Reason), strings.TrimSpace(input.RequestID)
	if validateRequired("tenant_id", input.TenantID, 36) != nil || validateRequired("grant_id", input.GrantID, 36) != nil || input.ExpectedRowVersion <= 0 ||
		len(input.Reason) > 500 || validateRequired("request_id", input.RequestID, 100) != nil || (target == model.TenantAccessGrantRevoked && input.Reason == "") {
		return nil, ErrTenantGrantInvalidInput
	}
	now := s.now().UTC()
	var grant model.TenantAccessGrant
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := reauthorizeTenantPermission(tx, authorization, input.TenantID, PermissionTenantGrantsWrite, now); err != nil {
			return err
		}
		current, err := loadTenantGrant(tx, input.TenantID, input.GrantID)
		if err != nil {
			return err
		}
		if current.RowVersion != input.ExpectedRowVersion {
			return ErrTenantGrantVersionConflict
		}
		valid := target == model.TenantAccessGrantSuspended && current.Status == model.TenantAccessGrantEnabled ||
			target == model.TenantAccessGrantEnabled && current.Status == model.TenantAccessGrantSuspended ||
			target == model.TenantAccessGrantRevoked && (current.Status == model.TenantAccessGrantEnabled || current.Status == model.TenantAccessGrantSuspended)
		if !valid {
			return ErrTenantGrantStateTransition
		}
		if target == model.TenantAccessGrantEnabled {
			chain, err := loadTenantResourceChain(tx, input.TenantID, current.TenantResourceID, now, true)
			if err != nil || chain.Resource.VisibilityState != model.TenantResourceVisible {
				return ErrTenantResourceUpstreamUnavailable
			}
			if _, err := validateTenantGrantSubject(tx, input.TenantID, current.SubjectType, current.SubjectUserID, current.SubjectGroupID, tenantGrantSubjectValidationTime(now, current.ValidFrom)); err != nil {
				return err
			}
			if err := validateTenantGrantWindow(current.ValidFrom, current.ExpiresAt, current.MaxSessionSeconds, now); err != nil {
				return err
			}
		}
		updates := map[string]any{"status": target, "revision": gorm.Expr("revision + 1"), "row_version": gorm.Expr("row_version + 1")}
		if target == model.TenantAccessGrantRevoked {
			updates["revoked_by_user_id"] = authorization.EffectiveUserID
			updates["revoked_at"] = now
			updates["revoke_reason"] = input.Reason
		}
		result := tx.Model(&model.TenantAccessGrant{}).Where("tenant_id = ? AND id = ? AND row_version = ? AND status = ?", input.TenantID, input.GrantID, input.ExpectedRowVersion, current.Status).Updates(updates)
		if result.Error != nil {
			return mapTenantGrantConstraint(result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrTenantGrantVersionConflict
		}
		if err := tx.Where("tenant_id = ? AND id = ?", input.TenantID, input.GrantID).First(&grant).Error; err != nil {
			return err
		}
		eventType := string(target)
		if err := createTenantGrantEvent(tx, authorization, &grant, eventType, input.RequestID, input.Reason, now); err != nil {
			return err
		}
		if target == model.TenantAccessGrantSuspended || target == model.TenantAccessGrantRevoked {
			return endSessionsForGrant(tx, &grant, "GRANT_"+strings.ToUpper(string(target)), input.Reason, input.RequestID, now)
		}
		return nil
	})
	return &grant, err
}

func validateTenantGrantSubject(tx *gorm.DB, tenantID string, subjectType model.TenantAccessGrantSubjectType, userID *uint64, groupID *int64, now time.Time) (string, error) {
	switch subjectType {
	case model.TenantAccessGrantSubjectUser:
		if userID == nil || *userID == 0 || groupID != nil {
			return "", ErrTenantGrantSubjectInvalid
		}
		var count int64
		if err := tx.Table("tenant_membership AS membership").Joins("JOIN user ON user.id = membership.user_id").
			Where("membership.tenant_id = ? AND membership.user_id = ? AND membership.enabled = ? AND user.enabled = ? AND (membership.expires_at IS NULL OR membership.expires_at > ?)", tenantID, *userID, true, true, now).
			Count(&count).Error; err != nil {
			return "", err
		}
		if count != 1 {
			return "", ErrTenantResourceCrossTenantReference
		}
		return strconv.FormatUint(*userID, 10), nil
	case model.TenantAccessGrantSubjectGroup:
		if groupID == nil || *groupID <= 0 || userID != nil {
			return "", ErrTenantGrantSubjectInvalid
		}
		var count int64
		if err := tx.Model(&model.Group{}).Where("id = ? AND tenant_id = ?", *groupID, tenantID).Count(&count).Error; err != nil {
			return "", err
		}
		if count != 1 {
			return "", ErrTenantResourceCrossTenantReference
		}
		return strconv.FormatInt(*groupID, 10), nil
	default:
		return "", ErrTenantGrantSubjectInvalid
	}
}

func normalizeTenantGrantActions(input []string) ([]string, string, error) {
	if len(input) == 0 || len(input) > 4 {
		return nil, "", ErrTenantGrantActionUnsupported
	}
	seen := make(map[string]struct{}, len(input))
	actions := make([]string, 0, len(input))
	for _, raw := range input {
		action := strings.TrimSpace(raw)
		if action == "" || len(action) > 30 {
			return nil, "", ErrTenantGrantActionUnsupported
		}
		if _, exists := seen[action]; exists {
			return nil, "", ErrTenantGrantActionUnsupported
		}
		seen[action] = struct{}{}
		actions = append(actions, action)
	}
	sort.Strings(actions)
	encoded, err := json.Marshal(actions)
	if err != nil {
		return nil, "", ErrTenantGrantInvalidInput
	}
	return actions, string(encoded), nil
}

func validateActionsForResource(resourceType model.TenantResourceType, actions []string) error {
	expected := ""
	switch resourceType {
	case model.TenantResourceContainerService:
		expected = "connect"
	case model.TenantResourceContainerSSH:
		expected = "shell"
	default:
		return ErrTenantGrantActionUnsupported
	}
	if len(actions) != 1 || actions[0] != expected {
		return ErrTenantGrantActionUnsupported
	}
	return nil
}

func validateTenantGrantWindow(validFrom time.Time, expiresAt *time.Time, maxSessionSeconds int, now time.Time) error {
	if validFrom.IsZero() || maxSessionSeconds <= 0 || maxSessionSeconds > 86400 {
		return ErrTenantGrantTimeInvalid
	}
	if expiresAt != nil && (!expiresAt.After(validFrom) || !expiresAt.After(now)) {
		return ErrTenantGrantTimeInvalid
	}
	return nil
}

func tenantGrantSubjectValidationTime(now, validFrom time.Time) time.Time {
	if validFrom.After(now) {
		return validFrom
	}
	return now
}

func createTenantGrantEvent(tx *gorm.DB, authorization *ManagementAuthorizationContext, grant *model.TenantAccessGrant, eventType, requestID, reason string, now time.Time) error {
	snapshot, err := json.Marshal(map[string]any{
		"resource_id": grant.TenantResourceID, "subject_type": grant.SubjectType, "subject_key": grant.SubjectKey,
		"actions": json.RawMessage(grant.Actions), "valid_from": grant.ValidFrom, "expires_at": grant.ExpiresAt,
		"max_session_seconds": grant.MaxSessionSeconds, "status": grant.Status,
	})
	if err != nil {
		return err
	}
	var simulationID *string
	if authorization.SimulationSessionID != "" {
		value := authorization.SimulationSessionID
		simulationID = &value
	}
	event := model.TenantAccessGrantEvent{
		ID: uuid.NewString(), GrantID: grant.ID, GrantRevision: grant.Revision, EventType: eventType,
		ActorUserID: authorization.ActorUserID, EffectiveUserID: authorization.EffectiveUserID,
		SimulationSessionID: simulationID, RequestID: requestID, Reason: reason, Snapshot: string(snapshot), OccurredAt: now,
	}
	return tx.Create(&event).Error
}

func endSessionsForGrant(tx *gorm.DB, grant *model.TenantAccessGrant, reasonCode, reason, requestID string, now time.Time) error {
	var sessions []model.ResourceSession
	if err := tx.Where("tenant_id = ? AND grant_id = ? AND status IN ?", grant.TenantID, grant.ID,
		[]model.ResourceSessionStatus{model.ResourceSessionAuthorizing, model.ResourceSessionActive}).Order("id ASC").Find(&sessions).Error; err != nil {
		return err
	}
	for i := range sessions {
		result := tx.Model(&model.ResourceSession{}).Where("tenant_id = ? AND id = ? AND row_version = ? AND status IN ?", grant.TenantID, sessions[i].ID, sessions[i].RowVersion,
			[]model.ResourceSessionStatus{model.ResourceSessionAuthorizing, model.ResourceSessionActive}).
			Updates(map[string]any{"status": model.ResourceSessionEnding, "close_reason": reasonCode, "row_version": gorm.Expr("row_version + 1")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTenantGrantVersionConflict
		}
		sessions[i].Status = model.ResourceSessionEnding
		sessions[i].RowVersion++
		if err := createSessionTermination(tx, &sessions[i], reasonCode, reason, now); err != nil {
			return err
		}
		if err := AppendTenantManagementOutbox(tx, TenantManagementOutboxInput{
			EventType: "resource_session.ending", AggregateType: "resource_session", AggregateID: sessions[i].ID,
			AggregateRevision: sessions[i].RowVersion, TenantID: grant.TenantID, ResourceID: sessions[i].TenantResourceID,
			GrantID: grant.ID, SessionID: sessions[i].ID, Status: string(sessions[i].Status), RowVersion: sessions[i].RowVersion,
			ReasonCode: reasonCode, RequestID: requestID, AvailableAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

func createSessionTermination(tx *gorm.DB, session *model.ResourceSession, reasonCode, reason string, now time.Time) error {
	var latest int64
	if err := tx.Model(&model.ResourceSessionTermination{}).Where("session_id = ?", session.ID).Select("COALESCE(MAX(command_revision), 0)").Scan(&latest).Error; err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" {
		reason = reasonCode
	}
	termination := model.ResourceSessionTermination{
		ID: uuid.NewString(), SessionID: session.ID, CommandRevision: latest + 1,
		ReasonCode: reasonCode, Reason: reason, Status: model.ResourceSessionTerminationPending,
		NextAttemptAt: &now,
	}
	return tx.Create(&termination).Error
}

func loadTenantGrant(tx *gorm.DB, tenantID, grantID string) (*model.TenantAccessGrant, error) {
	var grant model.TenantAccessGrant
	if err := tx.Where("tenant_id = ? AND id = ?", tenantID, grantID).First(&grant).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTenantGrantNotFound
		}
		return nil, err
	}
	return &grant, nil
}

func tenantGrantView(grant *model.TenantAccessGrant) (*TenantGrantView, error) {
	var actions []string
	if err := json.Unmarshal([]byte(grant.Actions), &actions); err != nil {
		return nil, fmt.Errorf("decode Tenant grant actions: %w", err)
	}
	if actions == nil {
		actions = []string{}
	}
	return &TenantGrantView{
		ID: grant.ID, ResourceID: grant.TenantResourceID, SubjectType: grant.SubjectType,
		SubjectUserID: grant.SubjectUserID, SubjectGroupID: grant.SubjectGroupID, Actions: actions,
		ValidFrom: grant.ValidFrom, ExpiresAt: grant.ExpiresAt, MaxSessionSeconds: grant.MaxSessionSeconds,
		Status: grant.Status, Revision: grant.Revision, RowVersion: grant.RowVersion,
		RevokedAt: grant.RevokedAt, RevokeReason: grant.RevokeReason, CreatedAt: grant.CreatedAt, UpdatedAt: grant.UpdatedAt,
	}, nil
}

func unifiedHostSSHGrantView(grant *model.AccessGrant, now time.Time) (*TenantGrantView, error) {
	var actions []string
	if err := json.Unmarshal([]byte(grant.Actions), &actions); err != nil {
		return nil, fmt.Errorf("decode HostSSH grant actions: %w", err)
	}
	if actions == nil {
		actions = []string{}
	}
	status := model.TenantAccessGrantStatus(grant.Status)
	if grant.Status == "enabled" && !grant.ExpiresAt.IsZero() && !grant.ExpiresAt.After(now) {
		status = model.TenantAccessGrantExpired
	}
	var userID *uint64
	if grant.SubjectUserID != 0 {
		value := grant.SubjectUserID
		userID = &value
	}
	expiresAt := &grant.ExpiresAt
	if grant.ExpiresAt.IsZero() {
		expiresAt = nil
	}
	return &TenantGrantView{
		ID: grant.ID, ResourceID: grant.ResourceID, SubjectType: model.TenantAccessGrantSubjectType(grant.SubjectType),
		SubjectUserID: userID, SubjectGroupID: grant.SubjectGroupID, Actions: actions,
		ValidFrom: grant.ValidFrom, ExpiresAt: expiresAt, MaxSessionSeconds: grant.MaxSessionSeconds,
		Status: status, Revision: max(grant.Revision, 1), RowVersion: max(grant.Revision, 1),
		CreatedAt: grant.CreatedAt, UpdatedAt: grant.UpdatedAt,
	}, nil
}

func validTenantGrantStatus(value model.TenantAccessGrantStatus) bool {
	return value == model.TenantAccessGrantEnabled || value == model.TenantAccessGrantSuspended || value == model.TenantAccessGrantRevoked || value == model.TenantAccessGrantExpired
}

func mapTenantGrantConstraint(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "uk_active_tenant_access_grant"), strings.Contains(message, "UNIQUE constraint failed: tenant_access_grant"):
		return ErrTenantGrantConflict
	case strings.Contains(message, "S4_GRANT_USER_MEMBERSHIP_MISMATCH"), strings.Contains(message, "S4_GRANT_GROUP_TENANT_MISMATCH"), strings.Contains(message, "S4_GRANT_SUBJECT"):
		return ErrTenantGrantSubjectInvalid
	case strings.Contains(message, "S4_GRANT_VERSION_INVALID"):
		return ErrTenantGrantVersionConflict
	case strings.Contains(message, "S4_GRANT_STATUS_TRANSITION_INVALID"), strings.Contains(message, "S4_GRANT_IDENTITY_IMMUTABLE"):
		return ErrTenantGrantStateTransition
	case strings.Contains(message, "S4_GRANT_RESOURCE_TENANT_MISMATCH"):
		return ErrTenantResourceNotFound
	case isDatabaseConstraintError(err):
		return ErrTenantGrantInvalidInput
	default:
		return err
	}
}
