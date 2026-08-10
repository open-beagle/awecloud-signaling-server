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

func TestEnsureUpdaterReleaseSchemaRebuildsArtifactRoleConstraint(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Exec(`
		CREATE TABLE artifact (
			id text PRIMARY KEY,
			release_id text NOT NULL,
			os text NOT NULL,
			arch text NOT NULL,
			role text NOT NULL DEFAULT 'app',
			package_type text NOT NULL DEFAULT 'binary',
			filename text NOT NULL,
			download_url text NOT NULL,
			size integer NOT NULL,
			sha256 text NOT NULL,
			status text NOT NULL DEFAULT 'available',
			created_at datetime,
			updated_at datetime
		);
		CREATE UNIQUE INDEX uk_artifact_release_platform ON artifact(release_id, os, arch);
	`).Error)

	require.NoError(t, ensureUpdaterReleaseSchema(database))
	require.NoError(t, database.AutoMigrate(&model.Artifact{}))

	app := model.Artifact{
		ID: "app", ReleaseID: "release", OS: "windows", Arch: "amd64", Role: "app",
		Filename: "desktop.exe", DownloadURL: "https://artifacts.example/desktop.exe", Size: 1, SHA256: "app",
	}
	launcher := model.Artifact{
		ID: "launcher", ReleaseID: "release", OS: "windows", Arch: "amd64", Role: "launcher",
		Filename: "launcher.exe", DownloadURL: "https://artifacts.example/launcher.exe", Size: 1, SHA256: "launcher",
	}
	require.NoError(t, database.Create(&app).Error)
	require.NoError(t, database.Create(&launcher).Error)

	duplicateLauncher := launcher
	duplicateLauncher.ID = "duplicate-launcher"
	require.Error(t, database.Create(&duplicateLauncher).Error)
}
