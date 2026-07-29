package service

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var (
	ErrManagementIdentityNotMapped = errors.New("management identity is not mapped to a user")
	ErrManagementIdentityStale     = errors.New("management identity revision is stale")
	ErrManagementUserDisabled      = errors.New("management user is disabled")
	ErrManagementScopeInvalid      = errors.New("management scope is invalid")
	ErrManagementMembershipMissing = errors.New("management membership is missing or inactive")
	ErrManagementPermissionDenied  = errors.New("management permission denied")
)

const (
	PermissionPlatformOverviewRead           = "platform.overview.read"
	PermissionPlatformOrganizationsRead      = "platform.organizations.read"
	PermissionPlatformOrganizationsWrite     = "platform.organizations.write"
	PermissionPlatformMembershipsRead        = "platform.memberships.read"
	PermissionPlatformMembershipsWrite       = "platform.memberships.write"
	PermissionPlatformTechnicalResourcesRead = "platform.technical_resources.read"
	PermissionPlatformResourcesRead          = "platform.resources.read"
	PermissionPlatformAllocationsRead        = "platform.allocations.read"
	PermissionPlatformAllocationsWrite       = "platform.allocations.write"
	PermissionPlatformOwnershipRead          = "platform.ownership.read"
	PermissionPlatformOwnershipWrite         = "platform.ownership.write"
	PermissionPlatformWorkloadsRead          = "platform.workloads.read"
	PermissionPlatformUserSimulationsRead    = "platform.user_simulations.read"
	PermissionPlatformUserSimulationsWrite   = "platform.user_simulations.write"
	PermissionPlatformIdentitiesRead         = "platform.identities.read"
	PermissionPlatformIdentitiesWrite        = "platform.identities.write"
	PermissionPlatformAuditRead              = "platform.audit.read"
	PermissionPlatformSettingsRead           = "platform.settings.read"
	PermissionPlatformSettingsWrite          = "platform.settings.write"

	PermissionProviderOverviewRead            = "provider.overview.read"
	PermissionProviderTechnicalResourcesRead  = "provider.technical_resources.read"
	PermissionProviderTechnicalResourcesWrite = "provider.technical_resources.write"
	PermissionProviderResourcesRead           = "provider.resources.read"
	PermissionProviderResourcesWrite          = "provider.resources.write"
	PermissionProviderIsolationEvidenceRead   = "provider.isolation_evidence.read"
	PermissionProviderIsolationEvidenceWrite  = "provider.isolation_evidence.write"
	PermissionProviderMembershipsRead         = "provider.memberships.read"
	PermissionProviderMembershipsWrite        = "provider.memberships.write"
	PermissionProviderAuditRead               = "provider.audit.read"

	PermissionTenantOverviewRead      = "tenant.overview.read"
	PermissionTenantMembersRead       = "tenant.members.read"
	PermissionTenantMembersWrite      = "tenant.members.write"
	PermissionTenantGroupsRead        = "tenant.groups.read"
	PermissionTenantGroupsWrite       = "tenant.groups.write"
	PermissionTenantDevicesRead       = "tenant.devices.read"
	PermissionTenantAdminsRead        = "tenant.admins.read"
	PermissionTenantAdminsWrite       = "tenant.admins.write"
	PermissionTenantResourcesRead     = "tenant.resources.read"
	PermissionTenantResourcesWrite    = "tenant.resources.write"
	PermissionTenantGrantsRead        = "tenant.grants.read"
	PermissionTenantGrantsWrite       = "tenant.grants.write"
	PermissionTenantSessionsRead      = "tenant.sessions.read"
	PermissionTenantSessionsTerminate = "tenant.sessions.terminate"
	PermissionTenantAuditRead         = "tenant.audit.read"
	PermissionTenantSettingsRead      = "tenant.settings.read"
	PermissionTenantSettingsWrite     = "tenant.settings.write"
)

var platformAdminPermissions = []string{
	PermissionPlatformOverviewRead,
	PermissionPlatformOrganizationsRead, PermissionPlatformOrganizationsWrite,
	PermissionPlatformMembershipsRead, PermissionPlatformMembershipsWrite,
	PermissionPlatformTechnicalResourcesRead, PermissionPlatformResourcesRead,
	PermissionPlatformAllocationsRead, PermissionPlatformAllocationsWrite,
	PermissionPlatformOwnershipRead, PermissionPlatformOwnershipWrite,
	PermissionPlatformWorkloadsRead,
	PermissionPlatformUserSimulationsRead, PermissionPlatformUserSimulationsWrite,
	PermissionPlatformIdentitiesRead, PermissionPlatformIdentitiesWrite,
	PermissionPlatformAuditRead,
	PermissionPlatformSettingsRead, PermissionPlatformSettingsWrite,
}

var platformViewerPermissions = []string{
	PermissionPlatformOverviewRead,
	PermissionPlatformOrganizationsRead,
	PermissionPlatformMembershipsRead,
	PermissionPlatformTechnicalResourcesRead,
	PermissionPlatformResourcesRead,
	PermissionPlatformAllocationsRead,
	PermissionPlatformOwnershipRead,
	PermissionPlatformWorkloadsRead,
	PermissionPlatformUserSimulationsRead,
	PermissionPlatformIdentitiesRead,
	PermissionPlatformAuditRead,
	PermissionPlatformSettingsRead,
}

var providerAdminPermissions = []string{
	PermissionProviderOverviewRead,
	PermissionProviderTechnicalResourcesRead, PermissionProviderTechnicalResourcesWrite,
	PermissionProviderResourcesRead, PermissionProviderResourcesWrite,
	PermissionProviderIsolationEvidenceRead, PermissionProviderIsolationEvidenceWrite,
	PermissionProviderMembershipsRead, PermissionProviderMembershipsWrite,
	PermissionProviderAuditRead,
}

var providerOperatorPermissions = []string{
	PermissionProviderOverviewRead,
	PermissionProviderTechnicalResourcesRead, PermissionProviderTechnicalResourcesWrite,
	PermissionProviderResourcesRead, PermissionProviderResourcesWrite,
	PermissionProviderIsolationEvidenceRead,
}

var providerViewerPermissions = []string{
	PermissionProviderOverviewRead,
	PermissionProviderTechnicalResourcesRead,
	PermissionProviderResourcesRead,
	PermissionProviderIsolationEvidenceRead,
	PermissionProviderMembershipsRead,
	PermissionProviderAuditRead,
}

var tenantAdminPermissionsV2 = []string{
	PermissionTenantOverviewRead,
	PermissionTenantMembersRead, PermissionTenantMembersWrite,
	PermissionTenantGroupsRead, PermissionTenantGroupsWrite,
	PermissionTenantDevicesRead,
	PermissionTenantAdminsRead, PermissionTenantAdminsWrite,
	PermissionTenantResourcesRead, PermissionTenantResourcesWrite,
	PermissionTenantGrantsRead, PermissionTenantGrantsWrite,
	PermissionTenantSessionsRead, PermissionTenantSessionsTerminate,
	PermissionTenantAuditRead,
	PermissionTenantSettingsRead, PermissionTenantSettingsWrite,
}

var tenantSecurityAuditorPermissionsV2 = []string{
	PermissionTenantOverviewRead,
	PermissionTenantGrantsRead,
	PermissionTenantSessionsRead, PermissionTenantSessionsTerminate,
	PermissionTenantAuditRead,
	PermissionTenantSettingsRead,
}

var tenantViewerPermissionsV2 = []string{
	PermissionTenantOverviewRead,
	PermissionTenantResourcesRead,
	PermissionTenantSessionsRead,
	PermissionTenantSettingsRead,
}

type UnifiedManagementIdentity struct {
	AdminID            int64
	UserID             uint64
	Username           string
	DisplayName        string
	AuthRevision       int64
	CredentialRevision int64
}

type ManagementAuthorizationContext struct {
	ActorUserID         uint64
	EffectiveUserID     uint64
	ScopeType           model.ManagementScopeType
	ScopeID             string
	ScopeKey            string
	ScopeName           string
	ScopeStatus         string
	Role                string
	Permissions         []string
	PermissionRevision  int64
	ExpiresAt           *time.Time
	SimulationSessionID string
}

func LoadLegacyAdminIdentity(database *gorm.DB, adminID int64) (*UnifiedManagementIdentity, error) {
	if database == nil || adminID <= 0 {
		return nil, ErrManagementIdentityNotMapped
	}
	var admin model.Admin
	if err := database.Where("id = ? AND enabled = ?", adminID, true).First(&admin).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrManagementIdentityNotMapped
		}
		return nil, err
	}

	var link model.UserAuthenticationLink
	err := database.Where("provider_type = ? AND provider_subject = ? AND enabled = ?",
		model.AuthenticationProviderLegacyAdmin, strconv.FormatInt(adminID, 10), true).First(&link).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrManagementIdentityNotMapped
		}
		return nil, err
	}

	var user model.User
	if err := database.Where("id = ? AND enabled = ?", link.UserID, true).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrManagementUserDisabled
		}
		return nil, err
	}
	var profile model.UserIdentityProfile
	if err := database.Where("user_id = ? AND enabled = ?", link.UserID, true).First(&profile).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrManagementUserDisabled
		}
		return nil, err
	}
	return &UnifiedManagementIdentity{
		AdminID: admin.ID, UserID: user.ID, Username: profile.Username, DisplayName: profile.DisplayName,
		AuthRevision: profile.AuthRevision, CredentialRevision: link.CredentialRevision,
	}, nil
}

func ResolveLegacyAdminIdentity(database *gorm.DB, adminID int64, claimedUserID uint64, claimedAuthRevision, claimedCredentialRevision int64) (*UnifiedManagementIdentity, error) {
	identity, err := LoadLegacyAdminIdentity(database, adminID)
	if err != nil {
		return nil, err
	}
	if claimedUserID == 0 || claimedAuthRevision <= 0 || claimedCredentialRevision <= 0 ||
		identity.UserID != claimedUserID || identity.AuthRevision != claimedAuthRevision || identity.CredentialRevision != claimedCredentialRevision {
		return nil, ErrManagementIdentityStale
	}
	return identity, nil
}

func BumpLegacyAdminCredentialRevision(database *gorm.DB, adminID int64) error {
	if database == nil || adminID <= 0 {
		return ErrManagementIdentityNotMapped
	}
	// Legacy deployments may serve Admin authentication before the unified
	// identity tables are migrated. In that case there is no mapped JWT to
	// invalidate, so password changes must remain available.
	if !database.Migrator().HasTable(&model.UserAuthenticationLink{}) {
		return nil
	}
	return database.Model(&model.UserAuthenticationLink{}).
		Where("provider_type = ? AND provider_subject = ?", model.AuthenticationProviderLegacyAdmin, strconv.FormatInt(adminID, 10)).
		Updates(map[string]any{
			"credential_revision": gorm.Expr("credential_revision + 1"),
			"row_version":         gorm.Expr("row_version + 1"),
		}).Error
}

func ListManagementContexts(database *gorm.DB, userID uint64, at time.Time) ([]ManagementAuthorizationContext, error) {
	if err := requireUnifiedManagementUser(database, userID); err != nil {
		return nil, err
	}
	contexts := make([]ManagementAuthorizationContext, 0)

	var platformMembership model.PlatformRoleMembership
	err := activeMembership(database, &platformMembership, "user_id = ?", userID, at)
	if err == nil {
		if permissions, ok := platformPermissions(platformMembership.Role); ok {
			contexts = append(contexts, ManagementAuthorizationContext{
				ActorUserID: userID, EffectiveUserID: userID, ScopeType: model.ManagementScopePlatform,
				Role: string(platformMembership.Role), Permissions: permissions,
				PermissionRevision: platformMembership.PermissionRevision, ExpiresAt: platformMembership.ExpiresAt,
			})
		}
	} else if !errors.Is(err, ErrManagementMembershipMissing) {
		return nil, err
	}

	var providerMemberships []model.AdminProviderMembership
	if err := database.Where("user_id = ? AND enabled = ? AND valid_from <= ? AND (expires_at IS NULL OR expires_at > ?)", userID, true, at, at).
		Order("provider_id ASC").Find(&providerMemberships).Error; err != nil {
		return nil, err
	}
	for _, membership := range providerMemberships {
		context, err := resolveProviderContext(database, userID, membership.ProviderID, at)
		if err == nil {
			contexts = append(contexts, *context)
		} else if !errors.Is(err, ErrManagementScopeInvalid) && !errors.Is(err, ErrManagementMembershipMissing) {
			return nil, err
		}
	}

	var tenantMemberships []model.UserTenantManagementMembership
	if err := database.Where("user_id = ? AND enabled = ? AND valid_from <= ? AND (expires_at IS NULL OR expires_at > ?)", userID, true, at, at).
		Order("tenant_id ASC").Find(&tenantMemberships).Error; err != nil {
		return nil, err
	}
	for _, membership := range tenantMemberships {
		context, err := resolveTenantContext(database, userID, membership.TenantID, at, false)
		if err == nil {
			contexts = append(contexts, *context)
		} else if !errors.Is(err, ErrManagementScopeInvalid) && !errors.Is(err, ErrManagementMembershipMissing) {
			return nil, err
		}
	}

	sort.Slice(contexts, func(i, j int) bool {
		if contexts[i].ScopeType == contexts[j].ScopeType {
			return contexts[i].ScopeID < contexts[j].ScopeID
		}
		return contexts[i].ScopeType < contexts[j].ScopeType
	})
	return contexts, nil
}

func ResolveManagementContext(database *gorm.DB, userID uint64, scopeType model.ManagementScopeType, scopeID string, at time.Time, allowTenantMember bool) (*ManagementAuthorizationContext, error) {
	if err := requireUnifiedManagementUser(database, userID); err != nil {
		return nil, err
	}
	scopeID = strings.TrimSpace(scopeID)
	switch scopeType {
	case model.ManagementScopePlatform:
		if scopeID != "" {
			return nil, ErrManagementScopeInvalid
		}
		var membership model.PlatformRoleMembership
		if err := activeMembership(database, &membership, "user_id = ?", userID, at); err != nil {
			return nil, err
		}
		permissions, ok := platformPermissions(membership.Role)
		if !ok {
			return nil, ErrManagementMembershipMissing
		}
		return &ManagementAuthorizationContext{
			ActorUserID: userID, EffectiveUserID: userID, ScopeType: scopeType, Role: string(membership.Role),
			Permissions: permissions, PermissionRevision: membership.PermissionRevision, ExpiresAt: membership.ExpiresAt,
		}, nil
	case model.ManagementScopeProvider:
		if scopeID == "" {
			return nil, ErrManagementScopeInvalid
		}
		return resolveProviderContext(database, userID, scopeID, at)
	case model.ManagementScopeTenant:
		if scopeID == "" {
			return nil, ErrManagementScopeInvalid
		}
		return resolveTenantContext(database, userID, scopeID, at, allowTenantMember)
	default:
		return nil, ErrManagementScopeInvalid
	}
}

func AuthorizeManagementPermission(context *ManagementAuthorizationContext, permission string) error {
	if context == nil || permission == "" || !strings.HasPrefix(permission, string(context.ScopeType)+".") {
		return ErrManagementPermissionDenied
	}
	if !containsPermission(context.Permissions, permission) {
		return ErrManagementPermissionDenied
	}
	if managementPermissionIsMutation(permission) && context.ScopeStatus != "" && context.ScopeStatus != "active" {
		return ErrManagementPermissionDenied
	}
	return nil
}

func resolveProviderContext(database *gorm.DB, userID uint64, providerID string, at time.Time) (*ManagementAuthorizationContext, error) {
	var provider model.ResourceProvider
	if err := database.Where("id = ?", providerID).First(&provider).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrManagementScopeInvalid
		}
		return nil, err
	}
	if provider.Status == model.ProviderStatusRetired {
		return nil, ErrManagementScopeInvalid
	}
	var membership model.AdminProviderMembership
	if err := activeMembership(database, &membership, "user_id = ? AND provider_id = ?", userID, providerID, at); err != nil {
		return nil, err
	}
	permissions, ok := providerPermissions(membership.Role)
	if !ok {
		return nil, ErrManagementMembershipMissing
	}
	return &ManagementAuthorizationContext{
		ActorUserID: userID, EffectiveUserID: userID, ScopeType: model.ManagementScopeProvider,
		ScopeID: provider.ID, ScopeKey: provider.Key, ScopeName: provider.DisplayName, ScopeStatus: string(provider.Status),
		Role: string(membership.Role), Permissions: permissions, PermissionRevision: membership.PermissionRevision,
		ExpiresAt: membership.ExpiresAt,
	}, nil
}

func resolveTenantContext(database *gorm.DB, userID uint64, tenantID string, at time.Time, allowTenantMember bool) (*ManagementAuthorizationContext, error) {
	var tenant model.Tenant
	if err := database.Where("id = ?", tenantID).First(&tenant).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrManagementScopeInvalid
		}
		return nil, err
	}
	var membership model.UserTenantManagementMembership
	err := activeMembership(database, &membership, "user_id = ? AND tenant_id = ?", userID, tenantID, at)
	if err == nil {
		permissions, ok := tenantPermissions(membership.Role)
		if !ok {
			return nil, ErrManagementMembershipMissing
		}
		return &ManagementAuthorizationContext{
			ActorUserID: userID, EffectiveUserID: userID, ScopeType: model.ManagementScopeTenant,
			ScopeID: tenant.ID, ScopeKey: tenant.Key, ScopeName: tenant.Name, ScopeStatus: string(tenant.Status),
			Role: string(membership.Role), Permissions: permissions, PermissionRevision: membership.PermissionRevision,
			ExpiresAt: membership.ExpiresAt,
		}, nil
	}
	if !errors.Is(err, ErrManagementMembershipMissing) {
		return nil, err
	}
	if !allowTenantMember {
		return nil, err
	}
	var member model.TenantMembership
	err = database.Where("user_id = ? AND tenant_id = ? AND enabled = ? AND (expires_at IS NULL OR expires_at > ?)",
		userID, tenantID, true, at).First(&member).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrManagementMembershipMissing
		}
		return nil, err
	}
	return &ManagementAuthorizationContext{
		ActorUserID: userID, EffectiveUserID: userID, ScopeType: model.ManagementScopeTenant,
		ScopeID: tenant.ID, ScopeKey: tenant.Key, ScopeName: tenant.Name, ScopeStatus: string(tenant.Status),
		Role: "member", Permissions: []string{}, PermissionRevision: 1, ExpiresAt: member.ExpiresAt,
	}, nil
}

func activeMembership(database *gorm.DB, value any, identityQuery string, args ...any) error {
	if len(args) == 0 {
		return ErrManagementMembershipMissing
	}
	at, ok := args[len(args)-1].(time.Time)
	if !ok {
		return fmt.Errorf("active membership check requires time")
	}
	identityArgs := args[:len(args)-1]
	query := identityQuery + " AND enabled = ? AND valid_from <= ? AND (expires_at IS NULL OR expires_at > ?)"
	identityArgs = append(identityArgs, true, at, at)
	err := database.Where(query, identityArgs...).First(value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrManagementMembershipMissing
	}
	return err
}

func requireUnifiedManagementUser(database *gorm.DB, userID uint64) error {
	if database == nil || userID == 0 {
		return ErrManagementIdentityNotMapped
	}
	var userCount int64
	if err := database.Model(&model.User{}).Where("id = ? AND enabled = ?", userID, true).Count(&userCount).Error; err != nil {
		return err
	}
	var profileCount int64
	if err := database.Model(&model.UserIdentityProfile{}).Where("user_id = ? AND enabled = ?", userID, true).Count(&profileCount).Error; err != nil {
		return err
	}
	if userCount == 0 || profileCount == 0 {
		return ErrManagementUserDisabled
	}
	return nil
}

func platformPermissions(role model.PlatformRole) ([]string, bool) {
	switch role {
	case model.PlatformRoleAdmin:
		return clonePermissions(platformAdminPermissions), true
	case model.PlatformRoleViewer:
		return clonePermissions(platformViewerPermissions), true
	default:
		return nil, false
	}
}

func providerPermissions(role model.ProviderManagementRole) ([]string, bool) {
	switch role {
	case model.ProviderManagementRoleAdmin:
		return clonePermissions(providerAdminPermissions), true
	case model.ProviderManagementRoleOperator:
		return clonePermissions(providerOperatorPermissions), true
	case model.ProviderManagementRoleViewer:
		return clonePermissions(providerViewerPermissions), true
	default:
		return nil, false
	}
}

func tenantPermissions(role model.TenantManagementRole) ([]string, bool) {
	switch role {
	case model.TenantManagementRoleAdmin:
		return clonePermissions(tenantAdminPermissionsV2), true
	case model.TenantManagementRoleSecurityAuditor:
		return clonePermissions(tenantSecurityAuditorPermissionsV2), true
	case model.TenantManagementRoleViewer:
		return clonePermissions(tenantViewerPermissionsV2), true
	default:
		return nil, false
	}
}

func clonePermissions(source []string) []string {
	return append([]string(nil), source...)
}

func containsPermission(permissions []string, permission string) bool {
	for _, candidate := range permissions {
		if candidate == permission {
			return true
		}
	}
	return false
}

func managementPermissionIsMutation(permission string) bool {
	return strings.HasSuffix(permission, ".write") || strings.HasSuffix(permission, ".terminate")
}
