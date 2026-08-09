package db

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestEnsureUpdaterReleaseSchemaRemovesObsoleteVersionConstraint(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Exec(`
		CREATE TABLE release (
			id text PRIMARY KEY,
			component text NOT NULL,
			version text NOT NULL,
			commit_id text NOT NULL DEFAULT ''
		);
		CREATE UNIQUE INDEX uk_release_component_version ON release(component, version);
	`).Error)

	require.NoError(t, ensureUpdaterReleaseSchema(database))
	require.NoError(t, database.AutoMigrate(&model.Release{}))

	first := model.Release{ID: "first", Component: model.ComponentAgent, Version: "v1.0.2", CommitID: "0123456789abcdef0123456789abcdef01234567"}
	second := model.Release{ID: "second", Component: model.ComponentAgent, Version: "v1.0.2", CommitID: "abcdef0123456789abcdef0123456789abcdef01"}
	require.NoError(t, database.Create(&first).Error)
	require.NoError(t, database.Create(&second).Error)
	require.False(t, database.Migrator().HasIndex(&model.Release{}, "uk_release_component_version"))
	require.True(t, database.Migrator().HasIndex(&model.Release{}, "uk_release_component_version_commit"))
}
