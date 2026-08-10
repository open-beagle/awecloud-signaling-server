package db

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestEnsureUpdaterReleaseSchemaKeepsLatestReleasePerComponent(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Exec(`
		CREATE TABLE release (
			id text PRIMARY KEY,
			component text NOT NULL,
			version text NOT NULL,
			commit_id text NOT NULL DEFAULT '',
			published_at datetime,
			created_at datetime
		);
		CREATE UNIQUE INDEX uk_release_component_version_commit ON release(component, version, commit_id);
		INSERT INTO release (id, component, version, commit_id, published_at, created_at)
		VALUES ('first', 'agent', 'v1.0.1', '0123456789abcdef0123456789abcdef01234567', '2026-08-10 10:00:00', '2026-08-10 10:00:00');
		INSERT INTO release (id, component, version, commit_id, published_at, created_at)
		VALUES ('second', 'agent', 'v1.0.2', 'abcdef0123456789abcdef0123456789abcdef01', '2026-08-10 11:00:00', '2026-08-10 11:00:00');
		CREATE TABLE artifact (id text PRIMARY KEY, release_id text NOT NULL);
		INSERT INTO artifact (id, release_id) VALUES ('first-artifact', 'first'), ('second-artifact', 'second');
		CREATE TABLE update_task (id text PRIMARY KEY, release_id text NOT NULL);
		INSERT INTO update_task (id, release_id) VALUES ('first-task', 'first'), ('second-task', 'second');
		CREATE TABLE update_event (id integer PRIMARY KEY, task_id text NOT NULL);
		INSERT INTO update_event (id, task_id) VALUES (1, 'first-task'), (2, 'second-task');
	`).Error)

	require.NoError(t, ensureUpdaterReleaseSchema(database))
	require.NoError(t, database.Exec(`CREATE UNIQUE INDEX uk_release_component ON release(component)`).Error)

	var releases []model.Release
	require.NoError(t, database.Find(&releases).Error)
	require.Len(t, releases, 1)
	require.Equal(t, "second", releases[0].ID)
	var artifactCount, taskCount, eventCount int64
	require.NoError(t, database.Table("artifact").Count(&artifactCount).Error)
	require.NoError(t, database.Table("update_task").Count(&taskCount).Error)
	require.NoError(t, database.Table("update_event").Count(&eventCount).Error)
	require.Equal(t, int64(1), artifactCount)
	require.Equal(t, int64(1), taskCount)
	require.Equal(t, int64(1), eventCount)
	require.True(t, database.Migrator().HasIndex(&model.Release{}, "uk_release_component"))
	require.False(t, database.Migrator().HasIndex(&model.Release{}, "uk_release_component_version_commit"))

	require.Error(t, database.Exec(`
		INSERT INTO release (id, component, version, commit_id)
		VALUES ('duplicate', 'agent', 'v1.0.3', '1111111111111111111111111111111111111111')
	`).Error)
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
