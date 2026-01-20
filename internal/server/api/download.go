package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type DownloadAPI struct{}

func NewDownloadAPI() *DownloadAPI {
	return &DownloadAPI{}
}

// DownloadInfo 单个平台的下载信息
type DownloadInfo struct {
	Version     string `json:"version"`
	DownloadURL string `json:"download_url"`
	Filename    string `json:"filename"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	BuildDate   string `json:"build_date,omitempty"`
}

// VersionInfo version.json 的结构
type VersionInfo struct {
	Version   string `json:"version"`
	BuildDate string `json:"build_date"`
}

// AllDownloadsResponse 所有平台的下载信息
type AllDownloadsResponse struct {
	Success   bool                    `json:"success"`
	Version   string                  `json:"version,omitempty"`
	BuildDate string                  `json:"build_date,omitempty"`
	Downloads map[string]DownloadInfo `json:"downloads,omitempty"`
	Message   string                  `json:"message,omitempty"`
}

// 版本信息缓存
var (
	cachedVersionInfo *VersionInfo
	versionCacheMutex sync.RWMutex
	versionCacheTime  time.Time
	versionCacheTTL   = 1 * time.Minute // 缓存1分钟
)

// getClientDownloadURL 从系统配置获取客户端下载地址
func getClientDownloadURL(c *gin.Context) (string, error) {
	ctx := c.Request.Context()
	var config model.SystemConfig
	if err := db.DB.WithContext(ctx).Where("key = ?", model.ConfigClientDownloadURL).First(&config).Error; err != nil {
		return "", err
	}
	return config.Value, nil
}

// fetchVersionInfo 从远程获取版本信息
func fetchVersionInfo(baseURL string) (*VersionInfo, error) {
	// 检查缓存
	versionCacheMutex.RLock()
	if cachedVersionInfo != nil && time.Since(versionCacheTime) < versionCacheTTL {
		info := cachedVersionInfo
		versionCacheMutex.RUnlock()
		return info, nil
	}
	versionCacheMutex.RUnlock()

	// 构建 version.json URL
	versionURL := baseURL
	if !strings.HasSuffix(versionURL, "/") {
		versionURL += "/"
	}
	versionURL += "version.json"

	// 发起 HTTP 请求
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	resp, err := client.Get(versionURL)
	if err != nil {
		return nil, fmt.Errorf("获取版本信息失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取版本信息失败: HTTP %d", resp.StatusCode)
	}

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取版本信息失败: %w", err)
	}

	// 解析 JSON
	var versionInfo VersionInfo
	if err := json.Unmarshal(body, &versionInfo); err != nil {
		return nil, fmt.Errorf("解析版本信息失败: %w", err)
	}

	// 更新缓存
	versionCacheMutex.Lock()
	cachedVersionInfo = &versionInfo
	versionCacheTime = time.Now()
	versionCacheMutex.Unlock()

	return &versionInfo, nil
}

// getVersionInfo 获取版本信息（带降级处理）
func getVersionInfo(baseURL string) *VersionInfo {
	versionInfo, err := fetchVersionInfo(baseURL)
	if err != nil {
		logger.Warnf("[Download] 获取版本信息失败，使用默认版本: %v", err)
		// 降级：使用默认版本
		return &VersionInfo{
			Version:   "v0.1.0",
			BuildDate: time.Now().Format(time.RFC3339),
		}
	}
	return versionInfo
}

// detectOS 从 User-Agent 或查询参数检测操作系统和架构
func detectOS(c *gin.Context) (string, string) {
	osType := ""
	arch := "amd64" // 默认架构

	// 优先使用查询参数
	if osParam := c.Query("os"); osParam != "" {
		osParam = strings.ToLower(osParam)
		// 标准化 macOS 别名
		if osParam == "macos" || osParam == "mac" {
			osType = "darwin"
		} else {
			osType = osParam
		}
	}

	// 检查架构参数
	if archParam := c.Query("arch"); archParam != "" {
		arch = strings.ToLower(archParam)
	}

	// 如果没有指定 OS，从 User-Agent 检测
	if osType == "" {
		userAgent := strings.ToLower(c.GetHeader("User-Agent"))

		if strings.Contains(userAgent, "windows") || strings.Contains(userAgent, "win64") || strings.Contains(userAgent, "win32") {
			osType = "windows"
		} else if strings.Contains(userAgent, "macintosh") || strings.Contains(userAgent, "mac os x") || strings.Contains(userAgent, "darwin") {
			osType = "darwin"
			// 检测 macOS 架构
			if strings.Contains(userAgent, "arm64") || strings.Contains(userAgent, "aarch64") {
				arch = "arm64"
			}
		} else if strings.Contains(userAgent, "linux") || strings.Contains(userAgent, "x11") {
			osType = "linux"
		} else {
			// 默认返回 windows
			osType = "windows"
		}
	}

	return osType, arch
}

// buildDownloadInfo 构建下载信息
func buildDownloadInfo(baseURL, osType, arch string, versionInfo *VersionInfo) DownloadInfo {
	version := versionInfo.Version
	buildDate := versionInfo.BuildDate

	// 默认架构
	if arch == "" {
		arch = "amd64"
	}

	var filename string
	switch osType {
	case "windows":
		arch = "amd64" // Windows 只支持 amd64
		filename = "awecloud-signaling-" + version + "-windows-" + arch + ".exe"
	case "linux":
		arch = "amd64" // Linux 只支持 amd64
		filename = "awecloud-signaling-" + version + "-linux-" + arch
	case "darwin":
		// macOS 支持 arm64 和 amd64
		if arch != "arm64" {
			arch = "amd64"
		}
		filename = "awecloud-signaling-" + version + "-darwin-" + arch + ".zip"
	default:
		filename = "awecloud-signaling-" + version + "-windows-amd64.exe"
		osType = "windows"
		arch = "amd64"
	}

	// 确保 baseURL 以斜杠结尾
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	return DownloadInfo{
		Version:     version,
		DownloadURL: baseURL + filename,
		Filename:    filename,
		OS:          osType,
		Arch:        arch,
		BuildDate:   buildDate,
	}
}

// buildAllDownloads 构建所有平台的下载信息
func buildAllDownloads(baseURL string, versionInfo *VersionInfo) map[string]DownloadInfo {
	downloads := make(map[string]DownloadInfo)

	// Windows
	downloads["windows"] = buildDownloadInfo(baseURL, "windows", "amd64", versionInfo)

	// Linux
	downloads["linux"] = buildDownloadInfo(baseURL, "linux", "amd64", versionInfo)

	// macOS (darwin) - arm64 (Apple Silicon)
	downloads["darwin-arm64"] = buildDownloadInfo(baseURL, "darwin", "arm64", versionInfo)
	downloads["macos-arm64"] = buildDownloadInfo(baseURL, "darwin", "arm64", versionInfo) // 别名

	// macOS (darwin) - amd64 (Intel)
	downloads["darwin-amd64"] = buildDownloadInfo(baseURL, "darwin", "amd64", versionInfo)
	downloads["macos-amd64"] = buildDownloadInfo(baseURL, "darwin", "amd64", versionInfo) // 别名

	// 兼容旧的 darwin/macos 键（默认返回 arm64）
	downloads["darwin"] = buildDownloadInfo(baseURL, "darwin", "arm64", versionInfo)
	downloads["macos"] = buildDownloadInfo(baseURL, "darwin", "arm64", versionInfo)

	return downloads
}

// GetDesktopDownload 获取桌面客户端下载信息（公开接口，无需认证）
// 根据 User-Agent 或 os 参数返回适合的下载信息
func (a *DownloadAPI) GetDesktopDownload(c *gin.Context) {
	// 获取系统配置中的下载地址
	baseURL, err := getClientDownloadURL(c)
	if err != nil || baseURL == "" {
		logger.Warnf("[Download] 获取下载地址失败: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "下载服务未配置",
		})
		return
	}

	// 获取版本信息
	versionInfo := getVersionInfo(baseURL)

	// 检测操作系统和架构
	osType, arch := detectOS(c)
	logger.Warnf("[Download] 检测到操作系统: %s, 架构: %s, User-Agent: %s", osType, arch, c.GetHeader("User-Agent"))

	// 构建下载信息
	downloadInfo := buildDownloadInfo(baseURL, osType, arch, versionInfo)

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"version":      downloadInfo.Version,
		"download_url": downloadInfo.DownloadURL,
		"filename":     downloadInfo.Filename,
		"os":           downloadInfo.OS,
		"arch":         downloadInfo.Arch,
		"build_date":   downloadInfo.BuildDate,
	})
}

// GetDesktopDownloadDirect 直接重定向到最新版本下载（公开接口）
func (a *DownloadAPI) GetDesktopDownloadDirect(c *gin.Context) {
	// 获取系统配置中的下载地址
	baseURL, err := getClientDownloadURL(c)
	if err != nil || baseURL == "" {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "下载服务未配置",
		})
		return
	}

	// 获取版本信息
	versionInfo := getVersionInfo(baseURL)

	// 检测操作系统和架构
	osType, arch := detectOS(c)

	// 构建下载信息
	downloadInfo := buildDownloadInfo(baseURL, osType, arch, versionInfo)

	// 重定向到下载链接
	c.Redirect(http.StatusFound, downloadInfo.DownloadURL)
}

// ListDesktopVersions 列出所有可用版本（公开接口）
func (a *DownloadAPI) ListDesktopVersions(c *gin.Context) {
	// 获取系统配置中的下载地址
	baseURL, err := getClientDownloadURL(c)
	if err != nil || baseURL == "" {
		c.JSON(http.StatusOK, AllDownloadsResponse{
			Success: false,
			Message: "下载服务未配置",
		})
		return
	}

	// 获取版本信息
	versionInfo := getVersionInfo(baseURL)

	// 构建所有平台的下载信息
	downloads := buildAllDownloads(baseURL, versionInfo)

	c.JSON(http.StatusOK, AllDownloadsResponse{
		Success:   true,
		Version:   versionInfo.Version,
		BuildDate: versionInfo.BuildDate,
		Downloads: downloads,
	})
}
