package db

import (
	"database/sql"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type releaseWithoutCommitDate struct {
	ID                  string              `gorm:"primaryKey;size:36"`
	Component           model.Component     `gorm:"size:20;not null;uniqueIndex:uk_release_component_version,priority:1;index"`
	Version             string              `gorm:"size:64;not null;uniqueIndex:uk_release_component_version,priority:2"`
	CommitID            string              `gorm:"size:40;not null;default:''"`
	Channel             string              `gorm:"size:32;not null;default:'stable';index"`
	Status              model.ReleaseStatus `gorm:"size:20;not null;default:'draft';index"`
	ReleaseNotes        string              `gorm:"type:text"`
	MinSupportedVersion string              `gorm:"size:64"`
	PublishedAt         *time.Time
	CreatedBy           uint64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (releaseWithoutCommitDate) TableName() string {
	return "release"
}

func requireFinalCommitDateColumn(t *testing.T, database *gorm.DB) {
	t.Helper()
	var notNull int
	var defaultValue sql.NullString
	require.NoError(t, database.Raw(`
		SELECT "notnull", dflt_value
		FROM pragma_table_info('release')
		WHERE name = 'commit_date'
	`).Row().Scan(&notNull, &defaultValue))
	require.Equal(t, 1, notNull)
	require.False(t, defaultValue.Valid)
}

func TestEnsureUpdaterReleaseSchemaPreservesReleaseHistory(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Exec(`
		CREATE TABLE release (
			id text PRIMARY KEY,
			component text NOT NULL,
			version text NOT NULL,
			commit_id text NOT NULL DEFAULT '',
			channel text NOT NULL DEFAULT 'stable',
			status text NOT NULL DEFAULT 'draft',
			release_notes text,
			min_supported_version text,
			published_at datetime,
			created_by integer,
			created_at datetime,
			updated_at datetime
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
	require.NoError(t, database.Exec(`CREATE UNIQUE INDEX uk_release_component_version ON release(component, version)`).Error)

	var releases []model.Release
	require.NoError(t, database.Find(&releases).Error)
	require.Len(t, releases, 2)
	var artifactCount, taskCount, eventCount int64
	require.NoError(t, database.Table("artifact").Count(&artifactCount).Error)
	require.NoError(t, database.Table("update_task").Count(&taskCount).Error)
	require.NoError(t, database.Table("update_event").Count(&eventCount).Error)
	require.Equal(t, int64(2), artifactCount)
	require.Equal(t, int64(2), taskCount)
	require.Equal(t, int64(2), eventCount)
	require.False(t, database.Migrator().HasIndex(&model.Release{}, "uk_release_component"))
	require.False(t, database.Migrator().HasIndex(&model.Release{}, "uk_release_component_version_commit"))
	require.True(t, database.Migrator().HasIndex(&model.Release{}, "uk_release_component_version"))

	require.NoError(t, database.Exec(`
		INSERT INTO release (id, component, version, commit_id)
		VALUES ('duplicate', 'agent', 'v1.0.3', '1111111111111111111111111111111111111111')
	`).Error)
	require.Error(t, database.Exec(`
		INSERT INTO release (id, component, version, commit_id)
		VALUES ('same-version', 'agent', 'v1.0.3', '2222222222222222222222222222222222222222')
	`).Error)
}

func TestEnsureUpdaterReleaseSchemaBackfillsCommitDateForExistingReleases(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&releaseWithoutCommitDate{}))
	publishedAt := time.Date(2026, 8, 13, 19, 24, 35, 0, time.UTC)
	require.NoError(t, database.Create(&releaseWithoutCommitDate{
		ID:          "agent-v1.0.2",
		Component:   model.ComponentAgent,
		Version:     "v1.0.2",
		CommitID:    "0123456789abcdef0123456789abcdef01234567",
		PublishedAt: &publishedAt,
	}).Error)

	require.NoError(t, ensureUpdaterReleaseSchema(database))
	require.NoError(t, database.AutoMigrate(&model.Release{}))
	require.NoError(t, ensureUpdaterReleaseSchema(database))
	require.NoError(t, database.AutoMigrate(&model.Release{}))

	var release model.Release
	require.NoError(t, database.First(&release, "id = ?", "agent-v1.0.2").Error)
	require.NotNil(t, release.PublishedAt)
	require.Equal(t, release.PublishedAt.UTC(), release.CommitDate.UTC())
	requireFinalCommitDateColumn(t, database)
}

func TestEnsureUpdaterReleaseSchemaResumesCommitDateBackfill(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&releaseWithoutCommitDate{}))
	publishedAt := time.Date(2026, 8, 13, 19, 24, 35, 0, time.UTC)
	require.NoError(t, database.Create(&releaseWithoutCommitDate{
		ID:          "agent-v1.0.2",
		Component:   model.ComponentAgent,
		Version:     "v1.0.2",
		CommitID:    "0123456789abcdef0123456789abcdef01234567",
		PublishedAt: &publishedAt,
	}).Error)
	require.NoError(t, database.Exec(`
		ALTER TABLE release ADD COLUMN commit_date datetime NOT NULL DEFAULT 0
	`).Error)

	require.NoError(t, ensureUpdaterReleaseSchema(database))
	require.NoError(t, database.AutoMigrate(&model.Release{}))

	var release model.Release
	require.NoError(t, database.First(&release, "id = ?", "agent-v1.0.2").Error)
	require.Equal(t, publishedAt, release.CommitDate.UTC())
	requireFinalCommitDateColumn(t, database)
}

func TestEnsureUpdaterReleaseSchemaReplacesSingleComponentConstraint(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.Release{}))
	require.NoError(t, database.Exec(`DROP INDEX uk_release_component_version`).Error)
	require.NoError(t, database.Exec(`CREATE UNIQUE INDEX uk_release_component ON release(component)`).Error)
	require.NoError(t, database.Create(&model.Release{
		ID: "first", Component: model.ComponentAgent, Version: "v1.0.0",
		CommitID: "0123456789abcdef0123456789abcdef01234567",
	}).Error)

	require.NoError(t, ensureUpdaterReleaseSchema(database))
	require.NoError(t, database.AutoMigrate(&model.Release{}))
	require.False(t, database.Migrator().HasIndex(&model.Release{}, "uk_release_component"))
	require.True(t, database.Migrator().HasIndex(&model.Release{}, "uk_release_component_version"))
	require.Error(t, database.Create(&model.Release{
		ID: "second", Component: model.ComponentAgent, Version: "v1.0.0",
		CommitID: "abcdef0123456789abcdef0123456789abcdef01",
	}).Error)
	require.NoError(t, database.Create(&model.Release{
		ID: "third", Component: model.ComponentAgent, Version: "v1.0.1",
		CommitID: "abcdef0123456789abcdef0123456789abcdef01",
	}).Error)
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
