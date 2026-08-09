package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/mod/semver"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type ManifestReleaseInfo struct {
	ID                  string `json:"id"`
	Version             string `json:"version"`
	Channel             string `json:"channel"`
	ReleaseNotes        string `json:"release_notes"`
	MinSupportedVersion string `json:"min_supported_version"`
}

type ManifestUpdateFlags struct {
	Available bool `json:"available"`
	Required  bool `json:"required"`
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
	Update        ManifestUpdateFlags  `json:"update"`
	Artifacts     ManifestArtifactsMap `json:"artifacts"`
}

func (a *UpdaterAPI) GetPublicManifest(c *gin.Context) {
	componentStr := strings.ToLower(strings.TrimSpace(c.Query("component")))
	osStr := strings.ToLower(strings.TrimSpace(c.Query("os")))
	archStr := strings.ToLower(strings.TrimSpace(c.Query("arch")))
	channelStr := strings.ToLower(strings.TrimSpace(c.Query("channel")))
	currentVersionStr := strings.TrimSpace(c.Query("current_version"))

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

	available := true
	required := false

	if currentVersionStr != "" {
		if !strings.HasPrefix(currentVersionStr, "v") {
			currentVersionStr = "v" + currentVersionStr
		}
		relVer := release.Version
		if !strings.HasPrefix(relVer, "v") {
			relVer = "v" + relVer
		}
		if semver.IsValid(currentVersionStr) && semver.IsValid(relVer) {
			available = semver.Compare(relVer, currentVersionStr) > 0
		}
		if release.MinSupportedVersion != "" {
			minVer := release.MinSupportedVersion
			if !strings.HasPrefix(minVer, "v") {
				minVer = "v" + minVer
			}
			if semver.IsValid(currentVersionStr) && semver.IsValid(minVer) {
				required = semver.Compare(currentVersionStr, minVer) < 0
			}
		}
	}

	now := time.Now().UTC()
	expiresAt := now.Add(10 * time.Minute)

	payload := ManifestPayload{
		SchemaVersion: 1,
		GeneratedAt:   now.Format(time.RFC3339),
		ExpiresAt:     expiresAt.Format(time.RFC3339),
		Release: ManifestReleaseInfo{
			ID:                  release.ID,
			Version:             strings.TrimPrefix(release.Version, "v"),
			Channel:             release.Channel,
			ReleaseNotes:        release.ReleaseNotes,
			MinSupportedVersion: strings.TrimPrefix(release.MinSupportedVersion, "v"),
		},
		Update: ManifestUpdateFlags{
			Available: available,
			Required:  required,
		},
		Artifacts: ManifestArtifactsMap{
			App:      appArtifact,
			Launcher: launcherArtifact,
		},
	}

	c.JSON(http.StatusOK, payload)
}
