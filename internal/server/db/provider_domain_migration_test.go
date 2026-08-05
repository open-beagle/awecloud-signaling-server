package db

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestEnsureProviderDomainLabelSchemaEnforcesExplicitGlobalUniqueness(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.ResourceProvider{}, &model.TechnicalResource{}, &model.DomainRegistry{},
	))
	require.NoError(t, database.Create(&[]model.ResourceProvider{
		{ID: "provider-root", Key: "beagle", DisplayName: "Root Provider", DomainScope: model.ProviderDomainRoot, Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1},
		{ID: "provider-a", Key: "beagle-bj", DisplayName: "Provider A", DomainScope: model.ProviderDomainNamed, DomainLabel: "beagle-bj", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1},
		{ID: "provider-b", Key: "zhiyi-sz", DisplayName: "Provider B", DomainScope: model.ProviderDomainNamed, DomainLabel: "zhiyi-sz", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1},
	}).Error)
	require.NoError(t, ensureProviderDomainLabelSchema(database))
	require.Error(t, database.Model(&model.ResourceProvider{}).Where("id = ?", "provider-b").Update("domain_label", "BEAGLE-BJ").Error)
	require.Error(t, database.Create(&model.ResourceProvider{
		ID: "provider-root-b", Key: "root-b", DisplayName: "Second Root", DomainScope: model.ProviderDomainRoot,
		Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1,
	}).Error)
}
