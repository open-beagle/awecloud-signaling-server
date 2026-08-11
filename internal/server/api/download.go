package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type DownloadAPI struct{}

func NewDownloadAPI() *DownloadAPI {
	return &DownloadAPI{}
}

// VersionInfo version.json 的结构
type VersionInfo struct {
	Version   string            `json:"version"`
	CommitID  string            `json:"commit_id,omitempty"`
	BuildDate string            `json:"build_date"`
	Files     map[string]string `json:"files,omitempty"`
	SHA256    map[string]string `json:"sha256,omitempty"`
}

type DesktopLauncherDownload struct {
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	PackageType string `json:"package_type"`
	Filename    string `json:"filename"`
	DownloadURL string `json:"download_url"`
	Size        int64  `json:"size"`
}

type DesktopLauncherDownloadsResponse struct {
	Version     string                    `json:"version"`
	CommitID    string                    `json:"commit_id"`
	PublishedAt time.Time                 `json:"published_at"`
	Downloads   []DesktopLauncherDownload `json:"downloads"`
}

const versionCacheTTL = time.Minute

// GetDesktopLaunchers returns only the published Launcher artifacts used for
// the first Desktop installation. Desktop App artifacts are never exposed here.
func (a *DownloadAPI) GetDesktopLaunchers(c *gin.Context) {
	ctx := c.Request.Context()
	var release model.Release
	err := db.DB.WithContext(ctx).
		Where("component = ? AND channel = ? AND status = ?", model.ComponentDesktop, "stable", model.ReleaseStatusPublished).
		Order("published_at DESC, created_at DESC").
		First(&release).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, NewErrorResponse("未找到已发布的 Desktop 版本"))
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("读取 Desktop 版本失败"))
		return
	}
	if release.CommitID == "" || release.PublishedAt == nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("Desktop 发布信息不完整"))
		return
	}

	var artifacts []model.Artifact
	if err := db.DB.WithContext(ctx).
		Where("release_id = ? AND role = ? AND status = ?", release.ID, "launcher", model.ArtifactStatusAvailable).
		Order("os ASC, arch ASC").
		Find(&artifacts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("读取 Desktop Launcher 制品失败"))
		return
	}
	if len(artifacts) == 0 {
		c.JSON(http.StatusNotFound, NewErrorResponse("当前 Desktop 版本尚未发布 Launcher"))
		return
	}

	downloads := make([]DesktopLauncherDownload, 0, len(artifacts))
	for _, artifact := range artifacts {
		downloads = append(downloads, DesktopLauncherDownload{
			OS:          artifact.OS,
			Arch:        artifact.Arch,
			PackageType: artifact.PackageType,
			Filename:    artifact.Filename,
			DownloadURL: artifact.DownloadURL,
			Size:        artifact.Size,
		})
	}

	c.JSON(http.StatusOK, DesktopLauncherDownloadsResponse{
		Version:     release.Version,
		CommitID:    release.CommitID,
		PublishedAt: *release.PublishedAt,
		Downloads:   downloads,
	})
}

// ============================================
// Agent 下载相关 API
// ============================================

// GetAgentInstallScript 获取 Agent 安装脚本（公开接口）
// GET /api/v1/download/install_agent.sh
// 重定向到 S3 上的安装脚本
func (a *DownloadAPI) GetAgentInstallScript(c *gin.Context) {
	// 获取系统配置中的 Agent 下载地址
	baseURL, err := getAgentDownloadURL(c)
	if err != nil || baseURL == "" {
		logger.Warnf("[Download] 获取 Agent 下载地址失败: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Agent 下载服务未配置",
		})
		return
	}

	// 重定向到 S3 上的安装脚本
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	c.Redirect(http.StatusFound, baseURL+"install_agent.sh")
}

// GetSignalInstallScript 获取统一安装脚本（公开接口）
// GET /api/v1/download/install_signal.sh
// 重定向到 S3 上的统一安装脚本（支持 Agent 和 Desktop）
func (a *DownloadAPI) GetSignalInstallScript(c *gin.Context) {
	// 获取系统配置中的 Agent 下载地址
	baseURL, err := getAgentDownloadURL(c)
	if err != nil || baseURL == "" {
		logger.Warnf("[Download] 获取下载地址失败: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "下载服务未配置",
		})
		return
	}

	// 重定向到 S3 上的统一安装脚本
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	c.Redirect(http.StatusFound, baseURL+"install_signal.sh")
}

// GetAgentDownload 获取 Agent 二进制下载（公开接口）
// GET /api/v1/download/agent?os=linux&arch=amd64
func (a *DownloadAPI) GetAgentDownload(c *gin.Context) {
	osType := c.DefaultQuery("os", "linux")
	arch := c.DefaultQuery("arch", "amd64")

	// 获取系统配置中的 Agent 下载地址
	baseURL, err := getAgentDownloadURL(c)
	if err != nil || baseURL == "" {
		logger.Warnf("[Download] 获取 Agent 下载地址失败: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Agent 下载服务未配置",
		})
		return
	}

	versionInfo, err := getAgentVersionInfo(baseURL)
	if err != nil {
		logger.Warnf("[Download] 获取 Agent Manifest 失败: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "Agent 下载清单不可用"})
		return
	}
	platform := strings.ToLower(osType) + "-" + strings.ToLower(arch)
	downloadURL := strings.TrimSpace(versionInfo.Files[platform])
	parsedURL, parseErr := url.Parse(downloadURL)
	if parseErr != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Agent 平台制品不存在"})
		return
	}

	c.Redirect(http.StatusFound, downloadURL)
}

// GetAgentVersion 获取 Agent 版本信息（公开接口）
// GET /api/v1/download/agent/version
func (a *DownloadAPI) GetAgentVersion(c *gin.Context) {
	// 获取系统配置中的 Agent 下载地址
	baseURL, err := getAgentDownloadURL(c)
	if err != nil || baseURL == "" {
		logger.Warnf("[Download] 获取 Agent 下载地址失败: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Agent 下载服务未配置",
		})
		return
	}

	// 获取版本信息
	versionInfo, err := getAgentVersionInfo(baseURL)
	if err != nil {
		logger.Warnf("[Download] 获取 Agent Manifest 失败: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "Agent 下载清单不可用"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"version":    versionInfo.Version,
		"commit_id":  versionInfo.CommitID,
		"build_date": versionInfo.BuildDate,
		"sha256":     versionInfo.SHA256,
	})
}

// Agent 版本信息缓存
var (
	cachedAgentVersionInfo *VersionInfo
	agentVersionCacheMutex sync.RWMutex
	agentVersionCacheTime  time.Time
)

var (
	cachedEndpointVersionInfo *VersionInfo
	endpointVersionCacheMutex sync.RWMutex
	endpointVersionCacheTime  time.Time
)

// getAgentVersionInfo 获取 Agent 版本信息
func getAgentVersionInfo(baseURL string) (*VersionInfo, error) {
	// 检查缓存
	agentVersionCacheMutex.RLock()
	if cachedAgentVersionInfo != nil && time.Since(agentVersionCacheTime) < versionCacheTTL {
		info := cachedAgentVersionInfo
		agentVersionCacheMutex.RUnlock()
		return info, nil
	}
	agentVersionCacheMutex.RUnlock()

	// 构建 version.json URL
	versionURL := baseURL
	if !strings.HasSuffix(versionURL, "/") {
		versionURL += "/"
	}
	versionURL += "signal_agent-version.json"

	// 发起 HTTP 请求
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	resp, err := client.Get(versionURL)
	if err != nil {
		return nil, fmt.Errorf("获取 Agent 版本信息失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取 Agent 版本信息失败: HTTP %d", resp.StatusCode)
	}

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 Agent 版本信息失败: %w", err)
	}

	// 解析 JSON
	var versionInfo VersionInfo
	if err := json.Unmarshal(body, &versionInfo); err != nil {
		return nil, fmt.Errorf("解析 Agent 版本信息失败: %w", err)
	}
	if versionInfo.Version == "" || !validAgentCommitID(versionInfo.CommitID) || len(versionInfo.Files) == 0 || len(versionInfo.SHA256) == 0 {
		return nil, fmt.Errorf("Agent 版本信息缺少构建身份")
	}
	for platform, digest := range versionInfo.SHA256 {
		if platform == "" || len(digest) != 64 || digest != strings.ToLower(digest) || strings.Trim(digest, "0123456789abcdef") != "" {
			return nil, fmt.Errorf("Agent 版本信息包含非法 SHA256")
		}
	}

	// 更新缓存
	agentVersionCacheMutex.Lock()
	cachedAgentVersionInfo = &versionInfo
	agentVersionCacheTime = time.Now()
	agentVersionCacheMutex.Unlock()

	return &versionInfo, nil
}

// getAgentDownloadURL 从系统配置获取 Agent 下载地址。
func getAgentDownloadURL(c *gin.Context) (string, error) {
	ctx := c.Request.Context()
	var config model.SystemConfig
	if err := db.DB.WithContext(ctx).Where("key = ?", model.ConfigClientDownloadURL).First(&config).Error; err != nil {
		return "", err
	}
	return config.Value, nil
}

// GetEndpointVersion 获取 Endpoint 版本信息（公开接口）
// GET /api/v1/download/endpoint/version
func (a *DownloadAPI) GetEndpointVersion(c *gin.Context) {
	baseURL, err := getAgentDownloadURL(c)
	if err != nil || baseURL == "" {
		logger.Warnf("[Download] 获取 Endpoint 下载地址失败: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Endpoint 下载服务未配置",
		})
		return
	}

	versionInfo, err := getEndpointVersionInfo(baseURL)
	if err != nil {
		logger.Warnf("[Download] 获取 Endpoint Manifest 失败: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "Endpoint 下载清单不可用"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"version":    versionInfo.Version,
		"commit_id":  versionInfo.CommitID,
		"build_date": versionInfo.BuildDate,
		"sha256":     versionInfo.SHA256,
	})
}

func getEndpointVersionInfo(baseURL string) (*VersionInfo, error) {
	endpointVersionCacheMutex.RLock()
	if cachedEndpointVersionInfo != nil && time.Since(endpointVersionCacheTime) < versionCacheTTL {
		info := cachedEndpointVersionInfo
		endpointVersionCacheMutex.RUnlock()
		return info, nil
	}
	endpointVersionCacheMutex.RUnlock()

	versionURL := strings.TrimSuffix(baseURL, "/") + "/signal_endpoint-version.json"
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(versionURL)
	if err != nil {
		return nil, fmt.Errorf("获取 Endpoint 版本信息失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取 Endpoint 版本信息失败: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 Endpoint 版本信息失败: %w", err)
	}
	var versionInfo VersionInfo
	if err := json.Unmarshal(body, &versionInfo); err != nil {
		return nil, fmt.Errorf("解析 Endpoint 版本信息失败: %w", err)
	}
	if versionInfo.Version == "" || !validAgentCommitID(versionInfo.CommitID) || len(versionInfo.Files) == 0 || len(versionInfo.SHA256) == 0 {
		return nil, fmt.Errorf("Endpoint 版本信息缺少构建身份")
	}
	for platform, digest := range versionInfo.SHA256 {
		if platform == "" || len(digest) != 64 || digest != strings.ToLower(digest) || strings.Trim(digest, "0123456789abcdef") != "" {
			return nil, fmt.Errorf("Endpoint 版本信息包含非法 SHA256")
		}
	}

	endpointVersionCacheMutex.Lock()
	cachedEndpointVersionInfo = &versionInfo
	endpointVersionCacheTime = time.Now()
	endpointVersionCacheMutex.Unlock()
	return &versionInfo, nil
}

// ============================================
// Endpoint 下载相关 API
// ============================================

// GetEndpointInstallScript 获取 Endpoint 安装脚本（公开接口）
// GET /api/v1/download/install_endpoint.sh
func (a *DownloadAPI) GetEndpointInstallScript(c *gin.Context) {
	baseURL, err := getAgentDownloadURL(c)
	if err != nil || baseURL == "" {
		logger.Warnf("[Download] 获取下载地址失败: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "下载服务未配置",
		})
		return
	}

	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	c.Redirect(http.StatusFound, baseURL+"install_endpoint.sh")
}

// GetEndpointDownload 获取 Endpoint 二进制下载（公开接口）
// GET /api/v1/download/endpoint?os=linux&arch=amd64&version=v0.2.3
func (a *DownloadAPI) GetEndpointDownload(c *gin.Context) {
	osType := c.DefaultQuery("os", "linux")
	arch := c.DefaultQuery("arch", "amd64")
	baseURL, err := getAgentDownloadURL(c)
	if err != nil || baseURL == "" {
		logger.Warnf("[Download] 获取下载地址失败: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "下载服务未配置",
		})
		return
	}

	versionInfo, err := getEndpointVersionInfo(baseURL)
	if err != nil {
		logger.Warnf("[Download] 获取 Endpoint Manifest 失败: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "Endpoint 下载清单不可用"})
		return
	}
	platform := strings.ToLower(osType) + "-" + strings.ToLower(arch)
	downloadURL := strings.TrimSpace(versionInfo.Files[platform])
	parsedURL, parseErr := url.Parse(downloadURL)
	if parseErr != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Endpoint 平台制品不存在"})
		return
	}
	c.Redirect(http.StatusFound, downloadURL)
}
