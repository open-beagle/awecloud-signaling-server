package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.Release{},
		&model.Artifact{},
	))
	db.DB = database
	return database
}

func TestGetPublicManifestPayload(t *testing.T) {
	database := setupTestDB(t)

	updaterAPI := NewUpdaterAPI()

	now := time.Now().UTC()
	release := model.Release{
		ID:                  uuid.NewString(),
		Component:           model.ComponentDesktop,
		Version:             "1.1.1",
		CommitID:            "1122334455667788990011223344556677889900",
		Channel:             "stable",
		Status:              model.ReleaseStatusPublished,
		ReleaseNotes:        "Bug fixes",
		MinSupportedVersion: "1.0.0",
		PublishedAt:         &now,
	}
	require.NoError(t, database.Create(&release).Error)

	appArt := model.Artifact{
		ID:          uuid.NewString(),
		ReleaseID:   release.ID,
		OS:          "windows",
		Arch:        "amd64",
		Role:        "app",
		PackageType: "binary",
		Filename:    "beagle-signal-1.1.1.app.exe",
		DownloadURL: "https://signal.example.com/api/v1/public/updater/artifacts/app",
		Size:        73400320,
		SHA256:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Status:      model.ArtifactStatusAvailable,
	}
	require.NoError(t, database.Create(&appArt).Error)

	launcherArt := model.Artifact{
		ID:          uuid.NewString(),
		ReleaseID:   release.ID,
		OS:          "windows",
		Arch:        "amd64",
		Role:        "launcher",
		PackageType: "binary",
		Filename:    "beagle-signal.launcher.exe",
		DownloadURL: "https://signal.example.com/api/v1/public/updater/artifacts/launcher",
		Size:        15728640,
		SHA256:      "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		Status:      model.ArtifactStatusAvailable,
	}
	require.NoError(t, database.Create(&launcherArt).Error)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/public/updater/manifest", updaterAPI.GetPublicManifest)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/updater/manifest?component=desktop&os=windows&arch=amd64&channel=stable&current_version=1.0.1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var payload ManifestPayload
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))

	require.Equal(t, 1, payload.SchemaVersion)
	require.Equal(t, "1.1.1", payload.Release.Version)
	require.Equal(t, "1.0.0", payload.Release.MinSupportedVersion)
	require.True(t, payload.Update.Available)
	require.False(t, payload.Update.Required)
	require.NotNil(t, payload.Artifacts.App)
	require.Equal(t, "app", payload.Artifacts.App.Role)
	require.Equal(t, appArt.SHA256, payload.Artifacts.App.SHA256)
	require.NotNil(t, payload.Artifacts.Launcher)
	require.Equal(t, "launcher", payload.Artifacts.Launcher.Role)
	require.Equal(t, launcherArt.SHA256, payload.Artifacts.Launcher.SHA256)
}

func TestGetPublicManifestRejectsInvalidComponentOrOS(t *testing.T) {
	setupTestDB(t)
	updaterAPI := &UpdaterAPI{}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/public/updater/manifest", updaterAPI.GetPublicManifest)

	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/public/updater/manifest?component=agent&os=windows&arch=amd64", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	require.Equal(t, http.StatusBadRequest, w1.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/public/updater/manifest?component=desktop&os=invalid&arch=amd64", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusBadRequest, w2.Code)
}
