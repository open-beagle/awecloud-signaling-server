package api

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

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
}

// AllDownloadsResponse 所有平台的下载信息
type AllDownloadsResponse struct {
	Success   bool                    `json:"success"`
	Version   string                  `json:"version,omitempty"`
	Downloads map[string]DownloadInfo `json:"downloads,omitempty"`
	Message   string                  `json:"message,omitempty"`
}

// getClientDownloadURL 从系统配置获取客户端下载地址
func getClientDownloadURL() (string, error) {
	var config model.SystemConfig
	if err := db.DB.First(&config).Error; err != nil {
		return "", err
	}
	return config.ClientDownloadURL, nil
}

// detectOS 从 User-Agent 或查询参数检测操作系统
func detectOS(c *gin.Context) string {
	// 优先使用查询参数
	if osParam := c.Query("os"); osParam != "" {
		osParam = strings.ToLower(osParam)
		// 标准化 macOS 别名
		if osParam == "macos" || osParam == "mac" {
			return "darwin"
		}
		return osParam
	}

	// 从 User-Agent 检测
	userAgent := strings.ToLower(c.GetHeader("User-Agent"))

	if strings.Contains(userAgent, "windows") || strings.Contains(userAgent, "win64") || strings.Contains(userAgent, "win32") {
		return "windows"
	}
	if strings.Contains(userAgent, "macintosh") || strings.Contains(userAgent, "mac os x") || strings.Contains(userAgent, "darwin") {
		return "darwin"
	}
	if strings.Contains(userAgent, "linux") || strings.Contains(userAgent, "x11") {
		return "linux"
	}

	// 默认返回 windows
	return "windows"
}

// buildDownloadInfo 构建下载信息
func buildDownloadInfo(baseURL, osType string) DownloadInfo {
	version := "v0.1.0"
	arch := "amd64"

	var filename string
	switch osType {
	case "windows":
		filename = "awecloud-signaling-" + version + "-windows-" + arch + ".exe"
	case "linux":
		filename = "awecloud-signaling-" + version + "-linux-" + arch
	case "darwin":
		arch = "universal"
		filename = "awecloud-signaling-" + version + "-darwin-" + arch + ".zip"
	default:
		filename = "awecloud-signaling-" + version + "-windows-amd64.exe"
		osType = "windows"
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
	}
}

// buildAllDownloads 构建所有平台的下载信息
func buildAllDownloads(baseURL string) map[string]DownloadInfo {
	downloads := make(map[string]DownloadInfo)

	// Windows
	downloads["windows"] = buildDownloadInfo(baseURL, "windows")

	// Linux
	downloads["linux"] = buildDownloadInfo(baseURL, "linux")

	// macOS (darwin)
	downloads["darwin"] = buildDownloadInfo(baseURL, "darwin")
	downloads["macos"] = buildDownloadInfo(baseURL, "darwin") // 别名

	return downloads
}

// GetDesktopDownload 获取桌面客户端下载信息（公开接口，无需认证）
// 根据 User-Agent 或 os 参数返回适合的下载信息
func (a *DownloadAPI) GetDesktopDownload(c *gin.Context) {
	// 获取系统配置中的下载地址
	baseURL, err := getClientDownloadURL()
	if err != nil || baseURL == "" {
		log.Printf("[Download] 获取下载地址失败: %v", err)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "下载服务未配置",
		})
		return
	}

	// 检测操作系统
	osType := detectOS(c)
	log.Printf("[Download] 检测到操作系统: %s, User-Agent: %s", osType, c.GetHeader("User-Agent"))

	// 构建下载信息
	downloadInfo := buildDownloadInfo(baseURL, osType)

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"version":      downloadInfo.Version,
		"download_url": downloadInfo.DownloadURL,
		"filename":     downloadInfo.Filename,
		"os":           downloadInfo.OS,
		"arch":         downloadInfo.Arch,
	})
}

// GetDesktopDownloadDirect 直接重定向到最新版本下载（公开接口）
func (a *DownloadAPI) GetDesktopDownloadDirect(c *gin.Context) {
	// 获取系统配置中的下载地址
	baseURL, err := getClientDownloadURL()
	if err != nil || baseURL == "" {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "下载服务未配置",
		})
		return
	}

	// 检测操作系统
	osType := detectOS(c)

	// 构建下载信息
	downloadInfo := buildDownloadInfo(baseURL, osType)

	// 重定向到下载链接
	c.Redirect(http.StatusFound, downloadInfo.DownloadURL)
}

// ListDesktopVersions 列出所有可用版本（公开接口）
func (a *DownloadAPI) ListDesktopVersions(c *gin.Context) {
	// 获取系统配置中的下载地址
	baseURL, err := getClientDownloadURL()
	if err != nil || baseURL == "" {
		c.JSON(http.StatusOK, AllDownloadsResponse{
			Success: false,
			Message: "下载服务未配置",
		})
		return
	}

	// 构建所有平台的下载信息
	downloads := buildAllDownloads(baseURL)

	c.JSON(http.StatusOK, AllDownloadsResponse{
		Success:   true,
		Version:   "v0.1.0",
		Downloads: downloads,
	})
}
