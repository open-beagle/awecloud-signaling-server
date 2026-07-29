package service

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var (
	ErrResourceIdentityInvalid   = errors.New("invalid resource identity input")
	ErrResourceIdentityReference = errors.New("resource identity reference not found")
	ErrUserSimulationNotAllowed  = errors.New("user simulation not allowed")
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
		session.EndedAt != nil || session.PermissionRevision <= 0 || session.RowVersion <= 0 {
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
