package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/auth"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func openLogtoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := db.DB
	t.Cleanup(func() {
		db.DB = originalDB
	})

	dsn := fmt.Sprintf("file:logto_auth_%d?mode=memory&cache=shared", time.Now().UnixNano())
	testDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := testDB.AutoMigrate(
		&model.User{},
		&model.UserIdentityProfile{},
		&model.UserAuthenticationLink{},
		&model.Tenant{},
		&model.TenantMembership{},
	); err != nil {
		t.Fatalf("migrate test tables: %v", err)
	}
	if err := testDB.Create(&model.Tenant{
		ID:     "tenant-beagle-default",
		Key:    "beagle",
		Name:   "Beagle",
		Status: model.TenantStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed beagle tenant: %v", err)
	}
	db.DB = testDB
	return testDB
}

func TestFindOrCreateUserAutoApprovesAndBindsOIDCIdentity(t *testing.T) {
	testDB := openLogtoTestDB(t)
	api := &DesktopAuthAPI{}

	info := &auth.LogtoUserInfo{
		Sub:      "logto-sub-001",
		Username: "MengkZhaoyun",
		Name:     "Display User",
	}

	created, disabled, err := api.findOrCreateUser(context.Background(), info)
	if err != nil {
		t.Fatalf("create Logto user: %v", err)
	}
	if disabled {
		t.Fatal("authorized Logto user must be enabled without manual approval")
	}
	if !created.Enabled {
		t.Fatal("created Logto user returned disabled")
	}
	if created.Name != "mengkzhaoyun" {
		t.Fatalf("expected normalized lowercase username mengkzhaoyun, got %q", created.Name)
	}

	var stored model.User
	if err := testDB.First(&stored, created.ID).Error; err != nil {
		t.Fatalf("reload Logto user: %v", err)
	}
	if !stored.Enabled {
		t.Fatal("Logto user persisted as disabled")
	}
	if stored.Source != model.UserSourceLogto {
		t.Fatalf("unexpected source: %q", stored.Source)
	}

	var profile model.UserIdentityProfile
	if err := testDB.Where("user_id = ?", created.ID).First(&profile).Error; err != nil {
		t.Fatalf("expected UserIdentityProfile to be created: %v", err)
	}
	if profile.Username != "mengkzhaoyun" || !profile.Enabled {
		t.Fatalf("unexpected identity profile: %+v", profile)
	}

	var link model.UserAuthenticationLink
	if err := testDB.Where("provider_type = ? AND provider_subject = ?", model.AuthenticationProviderOIDC, "logto-sub-001").First(&link).Error; err != nil {
		t.Fatalf("expected OIDC UserAuthenticationLink to be created: %v", err)
	}
	if link.UserID != created.ID {
		t.Fatalf("auth link user_id mismatch: got %d, want %d", link.UserID, created.ID)
	}

	var membership model.TenantMembership
	if err := testDB.Where("tenant_id = ? AND user_id = ?", "tenant-beagle-default", created.ID).First(&membership).Error; err != nil {
		t.Fatalf("expected new Logto user to join default beagle tenant: %v", err)
	}

	// Second login resolves by OIDC sub even if username field changes
	second, disabled, err := api.findOrCreateUser(context.Background(), &auth.LogtoUserInfo{
		Sub:      "logto-sub-001",
		Username: "AnotherHandle",
		Name:     "Display User Updated",
	})
	if err != nil {
		t.Fatalf("second login failed: %v", err)
	}
	if disabled || second.ID != created.ID {
		t.Fatalf("expected same enabled user %d, got id=%d disabled=%v", created.ID, second.ID, disabled)
	}

	// Explicit administrator disable after OIDC binding must still block login
	if err := testDB.Model(&model.User{}).Where("id = ?", created.ID).Update("enabled", false).Error; err != nil {
		t.Fatalf("admin disable user: %v", err)
	}
	_, disabledAfterAdminBlock, err := api.findOrCreateUser(context.Background(), info)
	if err != nil {
		t.Fatalf("login after admin disable failed: %v", err)
	}
	if !disabledAfterAdminBlock {
		t.Fatal("explicitly admin-disabled bound user must remain blocked")
	}
}

func TestFindOrCreateUserPromotesLegacyUnlinkedDisabledLogtoUser(t *testing.T) {
	testDB := openLogtoTestDB(t)
	api := &DesktopAuthAPI{}

	legacy := model.User{
		Name:       "LegacyUpperUser",
		Alias:      "Legacy Pending",
		Role:       model.UserRoleClient,
		SecretHash: "placeholder",
		Enabled:    false,
		Source:     model.UserSourceLogto,
	}
	if err := testDB.Create(&legacy).Error; err != nil {
		t.Fatalf("insert legacy user: %v", err)
	}
	if err := testDB.Model(&legacy).UpdateColumn("enabled", false).Error; err != nil {
		t.Fatalf("force legacy user disabled: %v", err)
	}

	promoted, disabled, err := api.findOrCreateUser(context.Background(), &auth.LogtoUserInfo{
		Sub:      "logto-sub-legacy",
		Username: "LegacyUpperUser",
		Name:     "Legacy Promoted",
	})
	if err != nil {
		t.Fatalf("promote legacy Logto user: %v", err)
	}
	if disabled || !promoted.Enabled {
		t.Fatalf("legacy unlinked Logto user should be auto-promoted to enabled, got disabled=%v enabled=%v", disabled, promoted.Enabled)
	}
	if promoted.ID != legacy.ID {
		t.Fatalf("expected existing user ID %d to be promoted, got %d", legacy.ID, promoted.ID)
	}
	if promoted.Name != "legacyupperuser" {
		t.Fatalf("expected legacy username to normalize to lowercase, got %q", promoted.Name)
	}

	var link model.UserAuthenticationLink
	if err := testDB.Where("provider_type = ? AND provider_subject = ?", model.AuthenticationProviderOIDC, "logto-sub-legacy").First(&link).Error; err != nil {
		t.Fatalf("expected OIDC link after promotion: %v", err)
	}
}

func TestResolveLogtoSignInPromptDefaultsToSessionReuse(t *testing.T) {
	if got := resolveLogtoSignInPrompt(""); got != "" {
		t.Fatalf("default prompt should be empty to reuse browser SSO session, got %q", got)
	}
	if got := resolveLogtoSignInPrompt("login"); got != "login" {
		t.Fatalf("explicit login prompt should be preserved for account switch, got %q", got)
	}
	if got := resolveLogtoSignInPrompt("unexpected"); got != "" {
		t.Fatalf("unsupported prompt value should be ignored, got %q", got)
	}
}
