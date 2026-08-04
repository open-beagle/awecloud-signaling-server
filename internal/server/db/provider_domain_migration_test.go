package db

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestEnsureProviderDomainLabelSchemaBackfillsAndEnforcesGlobalUniqueness(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Exec(`CREATE TABLE resource_provider (
		id TEXT PRIMARY KEY, key TEXT NOT NULL UNIQUE, display_name TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'active', revision INTEGER NOT NULL DEFAULT 1,
		row_version INTEGER NOT NULL DEFAULT 1, created_at DATETIME, updated_at DATETIME
	)`).Error)
	require.NoError(t, database.Exec(`INSERT INTO resource_provider (id, key, display_name) VALUES
		('provider-a', 'beagle-bj', 'Provider A'), ('provider-b', 'zhiyi-sz', 'Provider B')`).Error)
	require.NoError(t, database.AutoMigrate(&model.ResourceProvider{}))
	require.NoError(t, ensureProviderDomainLabelSchema(database))

	var providers []model.ResourceProvider
	require.NoError(t, database.Order("id").Find(&providers).Error)
	require.Equal(t, "beagle-bj", providers[0].DomainLabel)
	require.Equal(t, "zhiyi-sz", providers[1].DomainLabel)
	require.Error(t, database.Model(&model.ResourceProvider{}).Where("id = ?", providers[1].ID).Update("domain_label", "BEAGLE-BJ").Error)
}
