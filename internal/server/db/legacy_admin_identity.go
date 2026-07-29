package db

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

const defaultLegacyAdminIdentityReason = "legacy Admin identity synchronization"

// SyncLegacyAdminIdentity maintains the explicit Admin-ID to User adapter used
// by management context v2. It never associates an Admin with a User by name.
func SyncLegacyAdminIdentity(database *gorm.DB, adminID int64, reason string) (uint64, error) {
	if database == nil || adminID <= 0 {
		return 0, fmt.Errorf("invalid legacy admin identity input")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = defaultLegacyAdminIdentityReason
	}
	if len(reason) > 500 {
		return 0, fmt.Errorf("legacy admin identity reason exceeds 500 characters")
	}

	var userID uint64
	err := database.Transaction(func(tx *gorm.DB) error {
		var admin model.Admin
		if err := tx.First(&admin, adminID).Error; err != nil {
			return fmt.Errorf("load legacy admin: %w", err)
		}

		user, link, err := ensureLegacyAdminAuthenticationLink(tx, admin)
		if err != nil {
			return err
		}
		userID = user.ID
		if err := syncLegacyAdminProfile(tx, admin, user, link); err != nil {
			return err
		}
		if err := syncLegacyAdminPlatformRole(tx, admin, user.ID, reason); err != nil {
			return err
		}
		if err := syncLegacyAdminTenantMemberships(tx, admin, user.ID, reason); err != nil {
			return err
		}
		return nil
	})
	return userID, err
}

// EnsureDefaultAdminIdentity bootstraps the configured administrator after the
// default Tenant and its explicit legacy management membership exist.
func EnsureDefaultAdminIdentity(adminUsername string) error {
	var admin model.Admin
	err := DB.WithContext(context.Background()).Where("username = ?", adminUsername).First(&admin).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load default administrator for unified identity: %w", err)
	}
	if _, err := SyncLegacyAdminIdentity(DB.WithContext(context.Background()), admin.ID, "default administrator bootstrap"); err != nil {
		return fmt.Errorf("bootstrap default administrator unified identity: %w", err)
	}
	return nil
}

func ensureLegacyAdminAuthenticationLink(tx *gorm.DB, admin model.Admin) (*model.User, *model.UserAuthenticationLink, error) {
	providerSubject := strconv.FormatInt(admin.ID, 10)
	var link model.UserAuthenticationLink
	err := tx.Where("provider_type = ? AND provider_subject = ?", model.AuthenticationProviderLegacyAdmin, providerSubject).First(&link).Error
	if err == nil {
		var user model.User
		if err := tx.First(&user, link.UserID).Error; err != nil {
			return nil, nil, fmt.Errorf("load explicitly linked management user: %w", err)
		}
		return &user, &link, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, fmt.Errorf("load legacy admin authentication link: %w", err)
	}

	userName, err := availableLegacyAdminUserName(tx, admin.ID)
	if err != nil {
		return nil, nil, err
	}
	user := model.User{
		Name:       userName,
		Alias:      admin.Username,
		Role:       model.UserRoleClient,
		SecretHash: "management-only-" + uuid.NewString(),
		Enabled:    true,
		Source:     model.UserSourceManual,
	}
	if err := tx.Create(&user).Error; err != nil {
		return nil, nil, fmt.Errorf("create dedicated management user: %w", err)
	}
	if !admin.Enabled {
		if err := tx.Model(&user).Update("enabled", false).Error; err != nil {
			return nil, nil, fmt.Errorf("disable dedicated management user: %w", err)
		}
		user.Enabled = false
	}

	profileUsername, err := availableLegacyAdminProfileUsername(tx, admin.Username, admin.ID, user.ID, "")
	if err != nil {
		return nil, nil, err
	}
	profile := model.UserIdentityProfile{
		UserID: user.ID, Username: profileUsername, DisplayName: admin.Username,
		Enabled: admin.Enabled, AuthRevision: 1, RowVersion: 1,
	}
	if err := tx.Create(&profile).Error; err != nil {
		return nil, nil, fmt.Errorf("create management identity profile: %w", err)
	}
	link = model.UserAuthenticationLink{
		ID: uuid.NewString(), UserID: user.ID, ProviderType: model.AuthenticationProviderLegacyAdmin,
		ProviderSubject: providerSubject, CredentialRevision: 1, Enabled: admin.Enabled, RowVersion: 1,
	}
	if err := tx.Create(&link).Error; err != nil {
		return nil, nil, fmt.Errorf("create legacy admin authentication link: %w", err)
	}
	return &user, &link, nil
}

func syncLegacyAdminProfile(tx *gorm.DB, admin model.Admin, user *model.User, link *model.UserAuthenticationLink) error {
	var profile model.UserIdentityProfile
	if err := tx.First(&profile, "user_id = ?", user.ID).Error; err != nil {
		return fmt.Errorf("load management identity profile: %w", err)
	}
	profileUsername, err := availableLegacyAdminProfileUsername(tx, admin.Username, admin.ID, user.ID, profile.Username)
	if err != nil {
		return err
	}
	identityChanged := user.Enabled != admin.Enabled || user.Alias != admin.Username ||
		profile.Username != profileUsername || profile.DisplayName != admin.Username || profile.Enabled != admin.Enabled
	if user.Enabled != admin.Enabled || user.Alias != admin.Username {
		if err := tx.Model(user).Updates(map[string]any{"enabled": admin.Enabled, "alias": admin.Username}).Error; err != nil {
			return fmt.Errorf("synchronize management user status: %w", err)
		}
	}
	if identityChanged {
		if err := tx.Model(&profile).Updates(map[string]any{
			"username": profileUsername, "display_name": admin.Username, "enabled": admin.Enabled,
			"auth_revision": gorm.Expr("auth_revision + 1"), "row_version": gorm.Expr("row_version + 1"),
		}).Error; err != nil {
			return fmt.Errorf("synchronize management identity profile: %w", err)
		}
	}
	if link.Enabled != admin.Enabled {
		if err := tx.Model(link).Updates(map[string]any{
			"enabled": admin.Enabled, "credential_revision": gorm.Expr("credential_revision + 1"),
			"row_version": gorm.Expr("row_version + 1"),
		}).Error; err != nil {
			return fmt.Errorf("synchronize legacy admin authentication status: %w", err)
		}
	}
	return nil
}

func syncLegacyAdminPlatformRole(tx *gorm.DB, admin model.Admin, userID uint64, reason string) error {
	desiredRole := model.NormalizePlatformRole(admin.Role)
	var membership model.PlatformRoleMembership
	err := tx.Where("user_id = ?", userID).First(&membership).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if desiredRole == model.PlatformRoleNone {
			return nil
		}
		membership = model.PlatformRoleMembership{
			ID: uuid.NewString(), UserID: userID, Role: desiredRole, Enabled: admin.Enabled,
			ValidFrom: time.Now(), PermissionRevision: 1, CreatedByUserID: userID,
			Reason: reason, RowVersion: 1,
		}
		if err := tx.Create(&membership).Error; err != nil {
			return fmt.Errorf("create mirrored platform role: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("load mirrored platform role: %w", err)
	}
	desiredEnabled := admin.Enabled && desiredRole != model.PlatformRoleNone
	roleChanged := desiredRole != model.PlatformRoleNone && membership.Role != desiredRole
	if membership.Enabled == desiredEnabled && !roleChanged {
		return nil
	}
	updates := map[string]any{
		"enabled": desiredEnabled, "permission_revision": gorm.Expr("permission_revision + 1"),
		"row_version": gorm.Expr("row_version + 1"), "reason": reason,
	}
	if desiredRole != model.PlatformRoleNone {
		updates["role"] = desiredRole
	}
	if err := tx.Model(&membership).Updates(updates).Error; err != nil {
		return fmt.Errorf("synchronize mirrored platform role: %w", err)
	}
	return nil
}

func syncLegacyAdminTenantMemberships(tx *gorm.DB, admin model.Admin, userID uint64, reason string) error {
	var legacyMemberships []model.AdminTenantMembership
	if err := tx.Where("admin_id = ?", admin.ID).Find(&legacyMemberships).Error; err != nil {
		return fmt.Errorf("load legacy tenant management memberships: %w", err)
	}
	legacyByTenant := make(map[string]model.AdminTenantMembership, len(legacyMemberships))
	for _, membership := range legacyMemberships {
		legacyByTenant[membership.TenantID] = membership
	}

	var unifiedMemberships []model.UserTenantManagementMembership
	if err := tx.Where("user_id = ?", userID).Find(&unifiedMemberships).Error; err != nil {
		return fmt.Errorf("load unified tenant management memberships: %w", err)
	}
	unifiedByTenant := make(map[string]*model.UserTenantManagementMembership, len(unifiedMemberships))
	for i := range unifiedMemberships {
		unifiedByTenant[unifiedMemberships[i].TenantID] = &unifiedMemberships[i]
	}

	for tenantID, legacy := range legacyByTenant {
		role := model.NormalizeTenantManagementRole(legacy.Role)
		desiredEnabled := admin.Enabled && legacy.Enabled && role != ""
		current := unifiedByTenant[tenantID]
		if current == nil {
			if role == "" {
				continue
			}
			permissionRevision := legacy.PermissionRevision
			if permissionRevision <= 0 {
				permissionRevision = 1
			}
			validFrom := legacy.CreatedAt
			if validFrom.IsZero() {
				validFrom = time.Now()
			}
			membership := model.UserTenantManagementMembership{
				ID: uuid.NewString(), UserID: userID, TenantID: tenantID, Role: role,
				Enabled: desiredEnabled, ValidFrom: validFrom, ExpiresAt: legacy.ExpiresAt,
				PermissionRevision: permissionRevision, CreatedByUserID: userID, Reason: reason, RowVersion: 1,
			}
			if err := tx.Create(&membership).Error; err != nil {
				return fmt.Errorf("create mirrored tenant management membership: %w", err)
			}
			continue
		}
		roleChanged := role != "" && current.Role != role
		if current.Enabled == desiredEnabled && !roleChanged && sameOptionalTime(current.ExpiresAt, legacy.ExpiresAt) {
			continue
		}
		updates := map[string]any{
			"enabled": desiredEnabled, "expires_at": legacy.ExpiresAt, "reason": reason,
			"permission_revision": gorm.Expr("permission_revision + 1"), "row_version": gorm.Expr("row_version + 1"),
		}
		if role != "" {
			updates["role"] = role
		}
		if err := tx.Model(current).Updates(updates).Error; err != nil {
			return fmt.Errorf("synchronize mirrored tenant management membership: %w", err)
		}
	}

	for tenantID, current := range unifiedByTenant {
		if _, exists := legacyByTenant[tenantID]; exists || !current.Enabled {
			continue
		}
		if err := tx.Model(current).Updates(map[string]any{
			"enabled": false, "reason": reason, "permission_revision": gorm.Expr("permission_revision + 1"),
			"row_version": gorm.Expr("row_version + 1"),
		}).Error; err != nil {
			return fmt.Errorf("disable stale mirrored tenant management membership: %w", err)
		}
	}
	return nil
}

func availableLegacyAdminUserName(tx *gorm.DB, adminID int64) (string, error) {
	base := fmt.Sprintf("legacy-admin-%d", adminID)
	return availableUniqueValue(tx, &model.User{}, "name", base)
}

func availableLegacyAdminProfileUsername(tx *gorm.DB, adminUsername string, adminID int64, userID uint64, current string) (string, error) {
	preferred := truncateString(strings.TrimSpace(adminUsername), 100)
	if preferred != "" {
		var count int64
		if err := tx.Model(&model.UserIdentityProfile{}).
			Where("username = ? AND user_id <> ?", preferred, userID).Count(&count).Error; err != nil {
			return "", fmt.Errorf("check unique profile username: %w", err)
		}
		if count == 0 {
			return preferred, nil
		}
	}
	if current != "" {
		return current, nil
	}
	base := fmt.Sprintf("legacy-admin-%d", adminID)
	return availableUniqueValue(tx, &model.UserIdentityProfile{}, "username", base)
}

func availableUniqueValue(tx *gorm.DB, table any, column, preferred string) (string, error) {
	preferred = truncateString(preferred, 100)
	for attempt := 0; attempt < 4; attempt++ {
		candidate := preferred
		if attempt > 0 {
			suffix := "-" + uuid.NewString()[:8]
			candidate = truncateString(preferred, 100-len(suffix)) + suffix
		}
		query := tx.Model(table).Where(column+" = ?", candidate)
		var count int64
		if err := query.Count(&count).Error; err != nil {
			return "", fmt.Errorf("check unique %s: %w", column, err)
		}
		if count == 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not allocate unique %s", column)
}

func truncateString(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
