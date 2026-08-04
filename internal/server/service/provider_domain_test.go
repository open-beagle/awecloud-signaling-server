package service

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func newProviderDomainTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.ResourceProvider{}, &model.TechnicalResource{}, &model.DomainRegistry{}, &model.SystemConfig{}))
	return database
}

func TestProviderDomainLabelValidation(t *testing.T) {
	label, err := NormalizeProviderDomainLabel(" Beagle-BJ ")
	require.NoError(t, err)
	require.Equal(t, "beagle-bj", label)
	for _, value := range []string{"", "-beagle", "beagle_1", "kubernetes", "a.b", "中文"} {
		_, err := NormalizeProviderDomainLabel(value)
		require.ErrorIs(t, err, ErrProviderDomainLabelInvalid, value)
	}
}

func TestChangeProviderDomainLabelImmediatelyOfflinesOldDomains(t *testing.T) {
	database := newProviderDomainTestDB(t)
	ctx := context.Background()
	user := model.User{Name: "legacy-region", Role: model.UserRoleAgent, SecretHash: "fixture", Enabled: true}
	require.NoError(t, database.Create(&user).Error)
	provider := model.ResourceProvider{ID: "provider-a", Key: "provider-a", DisplayName: "Provider A", DomainLabel: "beagle-bj", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&provider).Error)
	resource := model.TechnicalResource{ID: "agent-a", ProviderID: provider.ID, Type: model.TechnicalResourceAgent, StableKey: "agent-a", LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline, CredentialRevision: 1, RuntimeUserID: user.ID, ConfigRevision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&resource).Error)
	oldDomains := []model.DomainRegistry{
		{Domain: "node-1.beagle-bj.beagle", Type: model.DomainTypeSSH, UserID: user.ID, NodeID: 10, TargetIP: "100.64.0.10", TargetPort: 22, Status: model.DomainStatusOnline},
		{Domain: "api.default.legacy-region.beagle", Type: model.DomainTypeK8SSVC, UserID: user.ID, NodeID: 10, TargetIP: "10.0.0.1", TargetPort: 443, Status: model.DomainStatusOnline},
	}
	require.NoError(t, database.Create(&oldDomains).Error)

	result, err := ChangeProviderDomainLabel(ctx, database, provider.ID, provider.DomainLabel, "beagle-north")
	require.NoError(t, err)
	require.EqualValues(t, 2, result.DomainCount)

	var oldOnline, newOnline int64
	require.NoError(t, database.Model(&model.DomainRegistry{}).Where("domain IN ? AND status = ?", []string{"node-1.beagle-bj.beagle", "api.default.legacy-region.beagle"}, model.DomainStatusOnline).Count(&oldOnline).Error)
	require.Zero(t, oldOnline)
	require.NoError(t, database.Model(&model.DomainRegistry{}).Where("domain IN ? AND status = ?", []string{"node-1.beagle-north.beagle", "api.default.beagle-north.beagle"}, model.DomainStatusOnline).Count(&newOnline).Error)
	require.EqualValues(t, 2, newOnline)
	require.NoError(t, database.Model(&provider).Update("domain_label", "beagle-north").Error)
	require.Equal(t, "node-2.beagle-north.beagle", NormalizeReportedProviderDomain(ctx, database, user.ID, user.Name, "node-2.legacy-region.beagle"))
}

func TestDomainServiceUsesProviderLabelAndConfiguredSuffix(t *testing.T) {
	database := newProviderDomainTestDB(t)
	ctx := context.Background()
	user := model.User{Name: "legacy-region", Role: model.UserRoleAgent, SecretHash: "fixture", Enabled: true}
	require.NoError(t, database.Create(&user).Error)
	provider := model.ResourceProvider{ID: "provider-custom-suffix", Key: "provider-custom-suffix", DisplayName: "Custom Suffix", DomainLabel: "beagle-bj", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&provider).Error)
	resource := model.TechnicalResource{ID: "agent-custom-suffix", ProviderID: provider.ID, Type: model.TechnicalResourceAgent, StableKey: "agent-custom-suffix", LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline, CredentialRevision: 1, RuntimeUserID: user.ID, ConfigRevision: 1, RowVersion: 1}
	require.NoError(t, database.Create(&resource).Error)
	require.NoError(t, database.Create(&model.SystemConfig{Key: model.ConfigDomainSuffix, Value: "corp.example"}).Error)

	node := model.Node{ID: 42, Name: "edge-01", IP: "100.64.0.42"}
	domains := NewDomainService(database)
	require.NoError(t, domains.CreateNodeSSHDomain(ctx, &node, &user))

	var record model.DomainRegistry
	require.NoError(t, database.Where("user_id = ? AND type = ?", user.ID, model.DomainTypeSSH).First(&record).Error)
	require.Equal(t, "edge-01.beagle-bj.corp.example", record.Domain)
	require.NoError(t, domains.DeleteNodeSSHDomain(ctx, &node, &user))
	var count int64
	require.NoError(t, database.Model(&model.DomainRegistry{}).Where("id = ?", record.ID).Count(&count).Error)
	require.Zero(t, count)
}
