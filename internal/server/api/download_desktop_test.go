package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestGetDesktopLaunchersReturnsPublishedLauncherArtifacts(t *testing.T) {
	database := setupTestDB(t)
	now := time.Now().UTC()
	release := model.Release{
		ID: uuid.NewString(), Component: model.ComponentDesktop, Version: "v1.2.3",
		CommitID: "0123456789abcdef0123456789abcdef01234567", Channel: "stable",
		Status: model.ReleaseStatusPublished, PublishedAt: &now,
	}
	require.NoError(t, database.Create(&release).Error)

	launcher := model.Artifact{
		ID: uuid.NewString(), ReleaseID: release.ID, OS: "windows", Arch: "amd64", Role: "launcher",
		PackageType: "binary", Filename: "beagle-signal.launcher-v1.2.3-windows-amd64.exe",
		DownloadURL: "https://artifacts.example/launcher.exe", Size: 1024,
		SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Status: model.ArtifactStatusAvailable,
	}
	require.NoError(t, database.Create(&launcher).Error)
	require.NoError(t, database.Create(&model.Artifact{
		ID: uuid.NewString(), ReleaseID: release.ID, OS: "windows", Arch: "amd64", Role: "app",
		PackageType: "binary", Filename: "beagle-signal-v1.2.3.app.exe",
		DownloadURL: "https://artifacts.example/app.exe", Size: 2048,
		SHA256: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		Status: model.ArtifactStatusAvailable,
	}).Error)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/public/download/desktop", NewDownloadAPI().GetDesktopLaunchers)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/public/download/desktop", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload DesktopLauncherDownloadsResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, "v1.2.3", payload.Version)
	require.Len(t, payload.Downloads, 1)
	require.Equal(t, launcher.Filename, payload.Downloads[0].Filename)
	require.Equal(t, "windows", payload.Downloads[0].OS)
}

func TestGetDesktopLaunchersRejectsReleaseWithoutLauncher(t *testing.T) {
	database := setupTestDB(t)
	now := time.Now().UTC()
	release := model.Release{
		ID: uuid.NewString(), Component: model.ComponentDesktop, Version: "v1.2.3",
		CommitID: "0123456789abcdef0123456789abcdef01234567", Channel: "stable",
		Status: model.ReleaseStatusPublished, PublishedAt: &now,
	}
	require.NoError(t, database.Create(&release).Error)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/public/download/desktop", nil)
	NewDownloadAPI().GetDesktopLaunchers(context)

	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Contains(t, recorder.Body.String(), "尚未发布 Launcher")
}
