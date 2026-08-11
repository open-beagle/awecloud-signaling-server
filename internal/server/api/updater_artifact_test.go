package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

func TestBuildArtifactUsesSizeAndSHA256(t *testing.T) {
	artifact, err := buildArtifact(ArtifactInput{
		OS: "linux", Arch: "amd64", Role: "app", PackageType: "binary",
		Filename: "signal_app", DownloadURL: "https://artifacts.example/signal_app", Size: 128,
		SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	})
	require.NoError(t, err)
	require.Equal(t, int64(128), artifact.Size)
	require.Equal(t, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", artifact.SHA256)
}

func TestPublishReleaseDoesNotRequireSigningKey(t *testing.T) {
	database := setupTestDB(t)

	release := model.Release{
		ID: uuid.NewString(), Component: model.ComponentDesktop, Version: "v1.0.0",
		CommitID: "0123456789abcdef0123456789abcdef01234567", Channel: "stable", Status: model.ReleaseStatusDraft,
	}
	require.NoError(t, database.Create(&release).Error)
	require.NoError(t, database.Create(&model.Artifact{
		ID: uuid.NewString(), ReleaseID: release.ID, OS: "windows", Arch: "amd64", Role: "app",
		PackageType: "binary", Filename: "signal_desktop.exe", DownloadURL: "https://artifacts.example/signal_desktop.exe",
		Size: 128, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Status: model.ArtifactStatusAvailable,
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	ctx.Params = gin.Params{{Key: "id", Value: release.ID}}
	NewUpdaterAPI().PublishRelease(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NoError(t, database.First(&release, "id = ?", release.ID).Error)
	require.Equal(t, model.ReleaseStatusPublished, release.Status)
}

func TestListReleasesIncludesArtifactCount(t *testing.T) {
	database := setupTestDB(t)
	release := model.Release{
		ID: uuid.NewString(), Component: model.ComponentAgent, Version: "v1.0.2",
		CommitID: "0123456789abcdef0123456789abcdef01234567", Channel: "stable", Status: model.ReleaseStatusPublished,
	}
	require.NoError(t, database.Create(&release).Error)
	for _, arch := range []string{"amd64", "arm64"} {
		require.NoError(t, database.Create(&model.Artifact{
			ID: uuid.NewString(), ReleaseID: release.ID, OS: "linux", Arch: arch, Role: "app",
			PackageType: "binary", Filename: "signal_agent_" + arch, DownloadURL: "https://artifacts.example/signal_agent_" + arch,
			Size: 128, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Status: model.ArtifactStatusAvailable,
		}).Error)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/updater/releases?component=agent", nil)
	NewUpdaterAPI().ListReleases(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"artifact_count":2`)
}

func TestSyncCatalogRequiresConfiguration(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/updater/sync", nil)

	NewUpdaterAPI().SyncCatalog(ctx)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}

func TestSyncCatalogReturnsCreatedReleaseCount(t *testing.T) {
	database := setupTestDB(t)
	manifest, err := json.Marshal(service.UpdaterCatalogManifest{
		SchemaVersion: 1,
		PublishedAt:   time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC),
		Release: service.UpdaterCatalogRelease{
			Component: model.ComponentAgent,
			Version:   "v1.0.0",
			CommitID:  "0123456789abcdef0123456789abcdef01234567",
			Channel:   "stable",
		},
		Artifacts: []service.UpdaterCatalogArtifact{{
			OS: "linux", Arch: "amd64", Role: "app", PackageType: "binary", Filename: "signal_agent",
			DownloadURL: "https://minio.example/vscode/awecloud-signaling/agent/artifacts/linux/amd64/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef/signal_agent",
			Size:        128,
			SHA256:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		}},
	})
	require.NoError(t, err)
	catalog, err := service.NewUpdaterCatalogServiceWithStore(database, updaterCatalogAPIStore{manifest: manifest}, "https://minio.example/vscode/awecloud-signaling")
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/updater/sync", nil)
	updaterAPI := NewUpdaterAPI()
	updaterAPI.SetCatalog(catalog)
	updaterAPI.SyncCatalog(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"created":1`)
}

type updaterCatalogAPIStore struct {
	manifest []byte
}

func (s updaterCatalogAPIStore) ListManifestKeys(context.Context) ([]string, error) {
	return []string{"https://minio.example/vscode/awecloud-signaling/updater/releases/agent/release.json"}, nil
}

func (s updaterCatalogAPIStore) ReadManifest(context.Context, string) ([]byte, error) {
	return s.manifest, nil
}
