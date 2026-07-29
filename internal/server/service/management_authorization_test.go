package service

import (
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type managementAuthorizationFixture struct {
	database      *gorm.DB
	now           time.Time
	admin         model.Admin
	otherAdmin    model.Admin
	actor         model.User
	provider      model.ResourceProvider
	otherProvider model.ResourceProvider
	tenant        model.Tenant
}

func newManagementAuthorizationFixture(t *testing.T) managementAuthorizationFixture {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=foreign_keys(1)", uuid.NewString())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.Admin{}, &model.User{}, &model.UserIdentityProfile{}, &model.UserAuthenticationLink{},
		&model.PlatformRoleMembership{}, &model.ResourceProvider{}, &model.AdminProviderMembership{},
		&model.Tenant{}, &model.TenantMembership{}, &model.UserTenantManagementMembership{}, &model.ProviderTenantBinding{},
	))
	now := time.Date(2026, 7, 29, 9, 0, 0, 0, time.UTC)
	admin := model.Admin{Username: "legacy-admin", PasswordHash: "fixture", Role: "admin", Enabled: true}
	otherAdmin := model.Admin{Username: "explicit-mapping-required", PasswordHash: "fixture", Role: "admin", Enabled: true}
	require.NoError(t, database.Create(&admin).Error)
	require.NoError(t, database.Create(&otherAdmin).Error)
	actor := model.User{Name: "unified-actor", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	require.NoError(t, database.Create(&actor).Error)
	require.NoError(t, database.Create(&model.UserIdentityProfile{
		UserID: actor.ID, Username: otherAdmin.Username, DisplayName: "Unified Actor", Enabled: true, AuthRevision: 4, RowVersion: 1,
	}).Error)
	require.NoError(t, database.Create(&model.UserAuthenticationLink{
		ID: uuid.NewString(), UserID: actor.ID, ProviderType: model.AuthenticationProviderLegacyAdmin,
		ProviderSubject: strconv.FormatInt(admin.ID, 10), CredentialRevision: 3, Enabled: true, RowVersion: 1,
	}).Error)
	require.NoError(t, database.Create(&model.PlatformRoleMembership{
		ID: uuid.NewString(), UserID: actor.ID, Role: model.PlatformRoleAdmin, Enabled: true, ValidFrom: now.Add(-time.Hour),
		PermissionRevision: 7, CreatedByUserID: actor.ID, Reason: "fixture", RowVersion: 1,
	}).Error)
	provider := model.ResourceProvider{ID: uuid.NewString(), Key: "provider-a", DisplayName: "Provider A", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	otherProvider := model.ResourceProvider{ID: uuid.NewString(), Key: "provider-b", DisplayName: "Provider B", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&provider).Error)
	require.NoError(t, database.Create(&otherProvider).Error)
	require.NoError(t, database.Create(&model.AdminProviderMembership{
		ID: uuid.NewString(), UserID: actor.ID, ProviderID: provider.ID, Role: model.ProviderManagementRoleOperator,
		Enabled: true, ValidFrom: now.Add(-time.Hour), PermissionRevision: 5, CreatedByUserID: actor.ID, Reason: "fixture", RowVersion: 1,
	}).Error)
	tenant := model.Tenant{ID: uuid.NewString(), Key: "tenant-a", Name: "Tenant A", Status: model.TenantStatusActive}
	require.NoError(t, database.Create(&tenant).Error)
	require.NoError(t, database.Create(&model.UserTenantManagementMembership{
		ID: uuid.NewString(), UserID: actor.ID, TenantID: tenant.ID, Role: model.TenantManagementRoleViewer,
		Enabled: true, ValidFrom: now.Add(-time.Hour), PermissionRevision: 2, CreatedByUserID: actor.ID, Reason: "fixture", RowVersion: 1,
	}).Error)
	return managementAuthorizationFixture{
		database: database, now: now, admin: admin, otherAdmin: otherAdmin, actor: actor,
		provider: provider, otherProvider: otherProvider, tenant: tenant,
	}
}

func TestLegacyAdminIdentityRequiresExplicitStableMapping(t *testing.T) {
	fixture := newManagementAuthorizationFixture(t)

	identity, err := LoadLegacyAdminIdentity(fixture.database, fixture.admin.ID)
	require.NoError(t, err)
	require.Equal(t, fixture.actor.ID, identity.UserID)
	require.Equal(t, int64(4), identity.AuthRevision)
	require.Equal(t, int64(3), identity.CredentialRevision)

	// Matching a profile username is deliberately insufficient. The adapter
	// only accepts the explicit legacy_admin + immutable Admin ID link.
	_, err = LoadLegacyAdminIdentity(fixture.database, fixture.otherAdmin.ID)
	require.ErrorIs(t, err, ErrManagementIdentityNotMapped)

	_, err = ResolveLegacyAdminIdentity(fixture.database, fixture.admin.ID, fixture.actor.ID, 3, 3)
	require.ErrorIs(t, err, ErrManagementIdentityStale)
	_, err = ResolveLegacyAdminIdentity(fixture.database, fixture.admin.ID, fixture.actor.ID, 4, 2)
	require.ErrorIs(t, err, ErrManagementIdentityStale)
	require.NoError(t, fixture.database.Model(&model.UserIdentityProfile{}).Where("user_id = ?", fixture.actor.ID).Update("enabled", false).Error)
	_, err = LoadLegacyAdminIdentity(fixture.database, fixture.admin.ID)
	require.ErrorIs(t, err, ErrManagementUserDisabled)
}

func TestManagementContextsUseIndependentMemberships(t *testing.T) {
	fixture := newManagementAuthorizationFixture(t)

	contexts, err := ListManagementContexts(fixture.database, fixture.actor.ID, fixture.now)
	require.NoError(t, err)
	require.Len(t, contexts, 3)
	require.Equal(t, model.ManagementScopePlatform, contexts[0].ScopeType)
	require.Equal(t, model.ManagementScopeProvider, contexts[1].ScopeType)
	require.Equal(t, fixture.provider.ID, contexts[1].ScopeID)
	require.Equal(t, model.ManagementScopeTenant, contexts[2].ScopeType)
	require.Equal(t, fixture.tenant.ID, contexts[2].ScopeID)

	providerContext, err := ResolveManagementContext(fixture.database, fixture.actor.ID, model.ManagementScopeProvider, fixture.provider.ID, fixture.now, false)
	require.NoError(t, err)
	require.NoError(t, AuthorizeManagementPermission(providerContext, PermissionProviderResourcesWrite))
	require.ErrorIs(t, AuthorizeManagementPermission(providerContext, PermissionProviderMembershipsWrite), ErrManagementPermissionDenied)
	require.ErrorIs(t, AuthorizeManagementPermission(providerContext, PermissionPlatformOrganizationsRead), ErrManagementPermissionDenied)

	_, err = ResolveManagementContext(fixture.database, fixture.actor.ID, model.ManagementScopeProvider, fixture.otherProvider.ID, fixture.now, false)
	require.ErrorIs(t, err, ErrManagementMembershipMissing)
	_, err = ResolveManagementContext(fixture.database, fixture.actor.ID, model.ManagementScopeProvider, fixture.tenant.ID, fixture.now, false)
	require.True(t, errors.Is(err, ErrManagementScopeInvalid) || errors.Is(err, ErrManagementMembershipMissing))

	tenantContext, err := ResolveManagementContext(fixture.database, fixture.actor.ID, model.ManagementScopeTenant, fixture.tenant.ID, fixture.now, false)
	require.NoError(t, err)
	require.NoError(t, AuthorizeManagementPermission(tenantContext, PermissionTenantResourcesRead))
	require.ErrorIs(t, AuthorizeManagementPermission(tenantContext, PermissionTenantResourcesWrite), ErrManagementPermissionDenied)
}

func TestProviderContextRejectsLegacyIntegrationProviderID(t *testing.T) {
	fixture := newManagementAuthorizationFixture(t)
	legacyProviderID := fixture.provider.Key
	require.NotEqual(t, fixture.provider.ID, legacyProviderID)
	require.NoError(t, fixture.database.Create(&model.ProviderTenantBinding{
		ID: uuid.NewString(), ProviderID: legacyProviderID, ExternalTenantID: "external-tenant-a",
		TenantID: fixture.tenant.ID, Status: model.ProviderBindingActive,
	}).Error)

	_, err := ResolveManagementContext(
		fixture.database, fixture.actor.ID, model.ManagementScopeProvider, legacyProviderID, fixture.now, false,
	)
	require.ErrorIs(t, err, ErrManagementScopeInvalid)
}

func TestTenantMemberDoesNotGainManagementPermissions(t *testing.T) {
	fixture := newManagementAuthorizationFixture(t)
	member := model.User{Name: "tenant-member", Role: model.UserRoleClient, SecretHash: "fixture", Enabled: true}
	require.NoError(t, fixture.database.Create(&member).Error)
	require.NoError(t, fixture.database.Create(&model.UserIdentityProfile{
		UserID: member.ID, Username: "tenant-member", DisplayName: "Tenant Member", Enabled: true, AuthRevision: 1, RowVersion: 1,
	}).Error)
	require.NoError(t, fixture.database.Create(&model.TenantMembership{TenantID: fixture.tenant.ID, UserID: member.ID, Role: "member", Enabled: true}).Error)

	_, err := ResolveManagementContext(fixture.database, member.ID, model.ManagementScopeTenant, fixture.tenant.ID, fixture.now, false)
	require.ErrorIs(t, err, ErrManagementMembershipMissing)
	context, err := ResolveManagementContext(fixture.database, member.ID, model.ManagementScopeTenant, fixture.tenant.ID, fixture.now, true)
	require.NoError(t, err)
	require.Equal(t, "member", context.Role)
	require.Empty(t, context.Permissions)
	require.ErrorIs(t, AuthorizeManagementPermission(context, PermissionTenantResourcesRead), ErrManagementPermissionDenied)
}

func TestSuspendedScopeKeepsReadAndRejectsMutation(t *testing.T) {
	fixture := newManagementAuthorizationFixture(t)
	require.NoError(t, fixture.database.Model(&model.ResourceProvider{}).Where("id = ?", fixture.provider.ID).Update("status", model.ProviderStatusSuspended).Error)
	context, err := ResolveManagementContext(fixture.database, fixture.actor.ID, model.ManagementScopeProvider, fixture.provider.ID, fixture.now, false)
	require.NoError(t, err)
	require.NoError(t, AuthorizeManagementPermission(context, PermissionProviderResourcesRead))
	require.ErrorIs(t, AuthorizeManagementPermission(context, PermissionProviderResourcesWrite), ErrManagementPermissionDenied)
}
