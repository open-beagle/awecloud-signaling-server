package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var (
	ErrResourceIdentityInvalid   = errors.New("invalid resource identity input")
	ErrResourceIdentityReference = errors.New("resource identity reference not found")
	ErrUserSimulationNotAllowed  = errors.New("user simulation not allowed")
	ErrUserSimulationInactive    = errors.New("user simulation session is inactive")
	ErrUserSimulationVersion     = errors.New("user simulation row version conflict")
)

func CreateUserIdentityProfile(database *gorm.DB, profile *model.UserIdentityProfile) error {
	if profile == nil || profile.UserID == 0 || validateRequired("username", profile.Username, 100) != nil ||
		validateRequired("display_name", profile.DisplayName, 200) != nil || profile.AuthRevision <= 0 || profile.RowVersion <= 0 {
		return ErrResourceIdentityInvalid
	}
	return database.Transaction(func(tx *gorm.DB) error {
		if err := requireRecord(tx, &model.User{}, "id = ?", profile.UserID); err != nil {
			return err
		}
		return tx.Create(profile).Error
	})
}

func CreateUserAuthenticationLink(database *gorm.DB, link *model.UserAuthenticationLink) error {
	if link == nil || link.ID == "" || link.UserID == 0 || !validAuthenticationProvider(link.ProviderType) ||
		validateRequired("provider_subject", link.ProviderSubject, 200) != nil || link.CredentialRevision <= 0 || link.RowVersion <= 0 {
		return ErrResourceIdentityInvalid
	}
	return database.Transaction(func(tx *gorm.DB) error {
		if err := requireProfile(tx, link.UserID); err != nil {
			return err
		}
		return tx.Create(link).Error
	})
}

func CreatePlatformRoleMembership(database *gorm.DB, membership *model.PlatformRoleMembership) error {
	if membership == nil || membership.ID == "" || membership.UserID == 0 || membership.CreatedByUserID == 0 ||
		!validPlatformRole(membership.Role) || !validMembershipWindow(membership.ValidFrom, membership.ExpiresAt) ||
		validateRequired("reason", membership.Reason, 500) != nil || membership.PermissionRevision <= 0 || membership.RowVersion <= 0 {
		return ErrResourceIdentityInvalid
	}
	return database.Transaction(func(tx *gorm.DB) error {
		if err := requireProfiles(tx, membership.UserID, membership.CreatedByUserID); err != nil {
			return err
		}
		return tx.Create(membership).Error
	})
}

func CreateResourceProvider(database *gorm.DB, provider *model.ResourceProvider) error {
	if provider == nil || provider.ID == "" || validateRequired("key", provider.Key, 100) != nil ||
		validateRequired("display_name", provider.DisplayName, 200) != nil || !validProviderStatus(provider.Status) ||
		provider.Revision <= 0 || provider.RowVersion <= 0 {
		return ErrResourceIdentityInvalid
	}
	return database.Create(provider).Error
}

func CreateAdminProviderMembership(database *gorm.DB, membership *model.AdminProviderMembership) error {
	if membership == nil || membership.ID == "" || membership.UserID == 0 || membership.ProviderID == "" || membership.CreatedByUserID == 0 ||
		!validProviderRole(membership.Role) || !validMembershipWindow(membership.ValidFrom, membership.ExpiresAt) ||
		validateRequired("reason", membership.Reason, 500) != nil || membership.PermissionRevision <= 0 || membership.RowVersion <= 0 {
		return ErrResourceIdentityInvalid
	}
	return database.Transaction(func(tx *gorm.DB) error {
		if err := requireProfiles(tx, membership.UserID, membership.CreatedByUserID); err != nil {
			return err
		}
		if err := requireRecord(tx, &model.ResourceProvider{}, "id = ?", membership.ProviderID); err != nil {
			return err
		}
		return tx.Create(membership).Error
	})
}

func CreateUserTenantManagementMembership(database *gorm.DB, membership *model.UserTenantManagementMembership) error {
	if membership == nil || membership.ID == "" || membership.UserID == 0 || membership.TenantID == "" || membership.CreatedByUserID == 0 ||
		!validTenantManagementRole(membership.Role) || !validMembershipWindow(membership.ValidFrom, membership.ExpiresAt) ||
		validateRequired("reason", membership.Reason, 500) != nil || membership.PermissionRevision <= 0 || membership.RowVersion <= 0 {
		return ErrResourceIdentityInvalid
	}
	return database.Transaction(func(tx *gorm.DB) error {
		if err := requireProfiles(tx, membership.UserID, membership.CreatedByUserID); err != nil {
			return err
		}
		if err := requireRecord(tx, &model.Tenant{}, "id = ?", membership.TenantID); err != nil {
			return err
		}
		return tx.Create(membership).Error
	})
}

func CreateUserSimulationSession(database *gorm.DB, session *model.UserSimulationSession) error {
	if session == nil || session.ID == "" || session.ActorUserID == 0 || session.EffectiveUserID == 0 || session.ScopeID == "" ||
		validateRequired("reason", session.Reason, 500) != nil || validateRequired("created_request_id", session.CreatedRequestID, 64) != nil ||
		session.Status != model.UserSimulationSessionActive || session.StartedAt.IsZero() || !session.StartedAt.Before(session.ExpiresAt) ||
		session.EndedAt != nil || session.EndReason != "" || session.PermissionRevision <= 0 || session.RowVersion <= 0 {
		return ErrResourceIdentityInvalid
	}
	return database.Transaction(func(tx *gorm.DB) error {
		if err := requireEnabledSimulationUser(tx, session.ActorUserID); err != nil {
			return err
		}
		if err := requireEnabledSimulationUser(tx, session.EffectiveUserID); err != nil {
			return err
		}
		if err := requireActivePlatformAdmin(tx, session.ActorUserID, session.StartedAt); err != nil {
			return err
		}
		switch session.ScopeType {
		case model.UserSimulationScopeProvider:
			if err := requireActiveSimulationScope(tx, &model.ResourceProvider{}, "id = ? AND status = ?", session.ScopeID, model.ProviderStatusActive); err != nil {
				return err
			}
			if err := requireActiveProviderSimulationMembership(tx, session.EffectiveUserID, session.ScopeID, session.StartedAt); err != nil {
				return err
			}
		case model.UserSimulationScopeTenant:
			if err := requireActiveSimulationScope(tx, &model.Tenant{}, "id = ? AND status = ?", session.ScopeID, model.TenantStatusActive); err != nil {
				return err
			}
			if err := requireActiveTenantSimulationMembership(tx, session.EffectiveUserID, session.ScopeID, session.StartedAt); err != nil {
				return err
			}
		default:
			return ErrResourceIdentityInvalid
		}
		return tx.Create(session).Error
	})
}

func ResolveUserSimulationSession(database *gorm.DB, sessionID string, actorUserID uint64, at time.Time) (*model.UserSimulationSession, *ManagementAuthorizationContext, error) {
	if database == nil || strings.TrimSpace(sessionID) == "" || actorUserID == 0 || at.IsZero() {
		return nil, nil, ErrResourceIdentityInvalid
	}
	var session model.UserSimulationSession
	var context *ManagementAuthorizationContext
	var resolutionErr error
	err := database.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&session, "id = ?", strings.TrimSpace(sessionID)).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				resolutionErr = ErrUserSimulationNotAllowed
				return nil
			}
			return err
		}
		if session.ActorUserID != actorUserID {
			resolutionErr = ErrUserSimulationNotAllowed
			return nil
		}
		if session.Status != model.UserSimulationSessionActive || session.EndedAt != nil {
			resolutionErr = ErrUserSimulationInactive
			return nil
		}
		if !at.Before(session.ExpiresAt) {
			if err := endUserSimulationSession(tx, &session, model.UserSimulationSessionExpired, "expired", at); err != nil {
				return err
			}
			resolutionErr = ErrUserSimulationInactive
			return nil
		}

		actorContext, err := ResolveManagementContext(tx, actorUserID, model.ManagementScopePlatform, "", at, false)
		if err == nil {
			err = AuthorizeManagementPermission(actorContext, PermissionPlatformUserSimulationsWrite)
		}
		if err != nil {
			if endErr := endUserSimulationSession(tx, &session, model.UserSimulationSessionRevoked, "actor_permission_invalid", at); endErr != nil {
				return endErr
			}
			resolutionErr = ErrUserSimulationNotAllowed
			return nil
		}

		scopeType := model.ManagementScopeType(session.ScopeType)
		context, err = ResolveManagementContext(tx, session.EffectiveUserID, scopeType, session.ScopeID, at, true)
		if err != nil || context.ScopeStatus != "active" {
			if endErr := endUserSimulationSession(tx, &session, model.UserSimulationSessionRevoked, "effective_context_invalid", at); endErr != nil {
				return endErr
			}
			resolutionErr = ErrUserSimulationNotAllowed
			return nil
		}
		context.ActorUserID = actorUserID
		context.EffectiveUserID = session.EffectiveUserID
		context.SimulationSessionID = session.ID
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if resolutionErr != nil {
		return &session, nil, resolutionErr
	}
	return &session, context, nil
}

func ListUserSimulationSessions(database *gorm.DB, at time.Time) ([]model.UserSimulationSession, error) {
	if database == nil || at.IsZero() {
		return nil, ErrResourceIdentityInvalid
	}
	var sessions []model.UserSimulationSession
	err := database.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.UserSimulationSession{}).
			Where("status = ? AND expires_at <= ?", model.UserSimulationSessionActive, at).
			Updates(map[string]any{
				"status":      model.UserSimulationSessionExpired,
				"ended_at":    at,
				"end_reason":  "expired",
				"row_version": gorm.Expr("row_version + 1"),
			}).Error; err != nil {
			return err
		}
		return tx.Order("started_at DESC, id ASC").Find(&sessions).Error
	})
	if err != nil {
		return nil, err
	}
	return sessions, nil
}

func RevokeUserSimulationSession(database *gorm.DB, sessionID string, actorUserID uint64, expectedRowVersion int64, reason string, at time.Time) (*model.UserSimulationSession, error) {
	if database == nil || strings.TrimSpace(sessionID) == "" || actorUserID == 0 || expectedRowVersion <= 0 ||
		validateRequired("reason", reason, 100) != nil || at.IsZero() {
		return nil, ErrResourceIdentityInvalid
	}
	var session model.UserSimulationSession
	var lifecycleErr error
	err := database.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&session, "id = ?", strings.TrimSpace(sessionID)).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserSimulationNotAllowed
			}
			return err
		}
		if session.ActorUserID != actorUserID {
			return ErrUserSimulationNotAllowed
		}
		if session.Status != model.UserSimulationSessionActive || session.EndedAt != nil {
			return ErrUserSimulationInactive
		}
		if at.Before(session.StartedAt) {
			return ErrResourceIdentityInvalid
		}
		if !at.Before(session.ExpiresAt) {
			if err := endUserSimulationSession(tx, &session, model.UserSimulationSessionExpired, "expired", at); err != nil {
				return err
			}
			lifecycleErr = ErrUserSimulationInactive
			return nil
		}
		if session.RowVersion != expectedRowVersion {
			return ErrUserSimulationVersion
		}
		return endUserSimulationSession(tx, &session, model.UserSimulationSessionRevoked, strings.TrimSpace(reason), at)
	})
	if err != nil {
		return nil, err
	}
	if lifecycleErr != nil {
		return &session, lifecycleErr
	}
	return &session, nil
}

func endUserSimulationSession(database *gorm.DB, session *model.UserSimulationSession, status model.UserSimulationSessionStatus, reason string, at time.Time) error {
	result := database.Model(&model.UserSimulationSession{}).
		Where("id = ? AND status = ? AND row_version = ?", session.ID, model.UserSimulationSessionActive, session.RowVersion).
		Updates(map[string]any{
			"status": status, "ended_at": at, "end_reason": reason, "row_version": gorm.Expr("row_version + 1"),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrUserSimulationVersion
	}
	session.Status = status
	session.EndedAt = &at
	session.EndReason = reason
	session.RowVersion++
	return nil
}

func requireEnabledSimulationUser(database *gorm.DB, userID uint64) error {
	if err := requireRecord(database, &model.User{}, "id = ?", userID); err != nil {
		return err
	}
	if err := requireRecord(database, &model.UserIdentityProfile{}, "user_id = ?", userID); err != nil {
		return err
	}
	var count int64
	if err := database.Model(&model.User{}).Where("id = ? AND enabled = ?", userID, true).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: user is disabled", ErrUserSimulationNotAllowed)
	}
	if err := database.Model(&model.UserIdentityProfile{}).Where("user_id = ? AND enabled = ?", userID, true).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: identity profile is disabled", ErrUserSimulationNotAllowed)
	}
	return nil
}

func requireActivePlatformAdmin(database *gorm.DB, userID uint64, at time.Time) error {
	var count int64
	err := database.Model(&model.PlatformRoleMembership{}).
		Where("user_id = ? AND role = ? AND enabled = ? AND valid_from <= ? AND (expires_at IS NULL OR expires_at > ?)",
			userID, model.PlatformRoleAdmin, true, at, at).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: actor is not an active platform administrator", ErrUserSimulationNotAllowed)
	}
	return nil
}

func requireActiveSimulationScope(database *gorm.DB, value any, query string, args ...any) error {
	var count int64
	if err := database.Model(value).Where(query, args...).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: simulation scope is missing or inactive", ErrUserSimulationNotAllowed)
	}
	return nil
}

func requireActiveProviderSimulationMembership(database *gorm.DB, userID uint64, providerID string, at time.Time) error {
	var count int64
	err := database.Model(&model.AdminProviderMembership{}).
		Where("user_id = ? AND provider_id = ? AND enabled = ? AND valid_from <= ? AND (expires_at IS NULL OR expires_at > ?)",
			userID, providerID, true, at, at).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: effective user has no active provider membership", ErrUserSimulationNotAllowed)
	}
	return nil
}

func requireActiveTenantSimulationMembership(database *gorm.DB, userID uint64, tenantID string, at time.Time) error {
	var managementCount int64
	err := database.Model(&model.UserTenantManagementMembership{}).
		Where("user_id = ? AND tenant_id = ? AND enabled = ? AND valid_from <= ? AND (expires_at IS NULL OR expires_at > ?)",
			userID, tenantID, true, at, at).
		Count(&managementCount).Error
	if err != nil {
		return err
	}
	var memberCount int64
	err = database.Model(&model.TenantMembership{}).
		Where("user_id = ? AND tenant_id = ? AND enabled = ? AND (expires_at IS NULL OR expires_at > ?)",
			userID, tenantID, true, at).
		Count(&memberCount).Error
	if err != nil {
		return err
	}
	if managementCount == 0 && memberCount == 0 {
		return fmt.Errorf("%w: effective user has no active tenant membership", ErrUserSimulationNotAllowed)
	}
	return nil
}

func requireProfiles(database *gorm.DB, userIDs ...uint64) error {
	seen := map[uint64]bool{}
	for _, userID := range userIDs {
		if seen[userID] {
			continue
		}
		seen[userID] = true
		if err := requireProfile(database, userID); err != nil {
			return err
		}
	}
	return nil
}

func requireProfile(database *gorm.DB, userID uint64) error {
	return requireRecord(database, &model.UserIdentityProfile{}, "user_id = ?", userID)
}

func requireRecord(database *gorm.DB, value any, query string, args ...any) error {
	var count int64
	if err := database.Model(value).Where(query, args...).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: %s", ErrResourceIdentityReference, query)
	}
	return nil
}

func validMembershipWindow(validFrom time.Time, expiresAt *time.Time) bool {
	return !validFrom.IsZero() && (expiresAt == nil || expiresAt.After(validFrom))
}

func validAuthenticationProvider(value model.AuthenticationProviderType) bool {
	return value == model.AuthenticationProviderLegacyUser || value == model.AuthenticationProviderLegacyAdmin || value == model.AuthenticationProviderOIDC
}

func validPlatformRole(value model.PlatformRole) bool {
	return value == model.PlatformRoleAdmin || value == model.PlatformRoleViewer
}

func validProviderStatus(value model.ProviderStatus) bool {
	return value == model.ProviderStatusActive || value == model.ProviderStatusSuspended || value == model.ProviderStatusRetired
}

func validProviderRole(value model.ProviderManagementRole) bool {
	return value == model.ProviderManagementRoleAdmin || value == model.ProviderManagementRoleOperator || value == model.ProviderManagementRoleViewer
}

func validTenantManagementRole(value model.TenantManagementRole) bool {
	return value == model.TenantManagementRoleAdmin || value == model.TenantManagementRoleSecurityAuditor || value == model.TenantManagementRoleViewer
}
