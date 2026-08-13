package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type ManifestReleaseInfo struct {
	ID                  string `json:"id"`
	Version             string `json:"version"`
	CommitID            string `json:"commit_id"`
	Channel             string `json:"channel"`
	ReleaseNotes        string `json:"release_notes"`
	MinSupportedVersion string `json:"min_supported_version"`
	PublishedAt         string `json:"published_at"`
}

type ManifestArtifactInfo struct {
	ID          string `json:"id"`
	Role        string `json:"role"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	PackageType string `json:"package_type"`
	Filename    string `json:"filename"`
	DownloadURL string `json:"download_url"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

type ManifestArtifactsMap struct {
	App      *ManifestArtifactInfo `json:"app"`
	Launcher *ManifestArtifactInfo `json:"launcher,omitempty"`
}

type ManifestPayload struct {
	SchemaVersion int                  `json:"schema_version"`
	GeneratedAt   string               `json:"generated_at"`
	ExpiresAt     string               `json:"expires_at"`
	Release       ManifestReleaseInfo  `json:"release"`
	Artifacts     ManifestArtifactsMap `json:"artifacts"`
}

func (a *UpdaterAPI) GetPublicManifest(c *gin.Context) {
	componentStr := strings.ToLower(strings.TrimSpace(c.Query("component")))
	osStr := strings.ToLower(strings.TrimSpace(c.Query("os")))
	archStr := strings.ToLower(strings.TrimSpace(c.Query("arch")))
	channelStr := strings.ToLower(strings.TrimSpace(c.Query("channel")))

	if componentStr != "desktop" {
		c.JSON(http.StatusBadRequest, NewErrorResponse("component 必须为 desktop"))
		return
	}
	if osStr == "macos" {
		osStr = "darwin"
	}
	if osStr != "windows" && osStr != "linux" && osStr != "darwin" {
		c.JSON(http.StatusBadRequest, NewErrorResponse("不支持的 os 类型"))
		return
	}
	if archStr != "amd64" && archStr != "arm64" {
		c.JSON(http.StatusBadRequest, NewErrorResponse("不支持的 arch 类型"))
		return
	}
	if channelStr == "" {
		channelStr = "stable"
	}

	ctx := c.Request.Context()
	var release model.Release
	err := db.DB.WithContext(ctx).
		Where("component = ? AND channel = ? AND status = ?", model.ComponentDesktop, channelStr, model.ReleaseStatusPublished).
		Order("published_at DESC, created_at DESC").
		First(&release).Error
	if err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("未找到发布的 Desktop 版本"))
		return
	}

	var artifacts []model.Artifact
	if err := db.DB.WithContext(ctx).
		Where("release_id = ? AND os = ? AND arch = ? AND status = ?", release.ID, osStr, archStr, model.ArtifactStatusAvailable).
		Find(&artifacts).Error; err != nil || len(artifacts) == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("未找到匹配的 Desktop 制品"))
		return
	}

	var appArtifact *ManifestArtifactInfo
	var launcherArtifact *ManifestArtifactInfo

	for _, art := range artifacts {
		info := &ManifestArtifactInfo{
			ID:          art.ID,
			Role:        art.Role,
			OS:          art.OS,
			Arch:        art.Arch,
			PackageType: art.PackageType,
			Filename:    art.Filename,
			DownloadURL: art.DownloadURL,
			Size:        art.Size,
			SHA256:      art.SHA256,
		}
		if art.Role == "app" {
			appArtifact = info
		} else if art.Role == "launcher" {
			launcherArtifact = info
		}
	}

	if appArtifact == nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("缺少 role=app 制品"))
		return
	}

	now := time.Now().UTC()
	expiresAt := now.Add(10 * time.Minute)
	publishedAt := release.CreatedAt.UTC()
	if release.PublishedAt != nil {
		publishedAt = release.PublishedAt.UTC()
	}

	payload := ManifestPayload{
		SchemaVersion: 1,
		GeneratedAt:   now.Format(time.RFC3339),
		ExpiresAt:     expiresAt.Format(time.RFC3339),
		Release: ManifestReleaseInfo{
			ID:                  release.ID,
			Version:             strings.TrimPrefix(release.Version, "v"),
			CommitID:            release.CommitID,
			Channel:             release.Channel,
			ReleaseNotes:        release.ReleaseNotes,
			MinSupportedVersion: strings.TrimPrefix(release.MinSupportedVersion, "v"),
			PublishedAt:         publishedAt.Format(time.RFC3339),
		},
		Artifacts: ManifestArtifactsMap{
			App:      appArtifact,
			Launcher: launcherArtifact,
		},
	}

	c.JSON(http.StatusOK, payload)
}
