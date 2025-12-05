package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"runtime"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// DownloadInfo 下载信息
type DownloadInfo struct {
	Version     string `json:"version"`
	DownloadURL string `json:"download_url"`
	Filename    string `json:"filename"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	BuildDate   string `json:"build_date,omitempty"`
}

// VersionInfo S3 上的版本信息
type VersionInfo struct {
	Version   string `json:"version"`
	BuildDate string `json:"build_date"`
}

// 不再硬编码 S3 地址，完全依赖系统配置

// DownloadAPI 下载API
type DownloadAPI struct{}

// NewDownloadAPI 创建下载API实例
func NewDownloadAPI() *DownloadAPI {
	return &DownloadAPI{}
}

// GetDesktopDownload 获取桌面客户端下载链接（智能识别操作系统）
func (d *DownloadAPI) GetDesktopDownload(c *gin.Context) {
	r := c.Request
	// 从 User-Agent 或查询参数获取操作系统信息
	osType := r.URL.Query().Get("os")
	if osType == "" {
		osType = detectOSFromUserAgent(r.UserAgent())
	}

	// 获取最新版本信息
	versionInfo, err := getLatestVersion()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("获取版本信息失败: %v", err)})
		return
	}

	// 根据操作系统生成下载信息
	downloadInfo := generateDownloadInfo(osType, versionInfo)

	c.JSON(http.StatusOK, downloadInfo)
}

// GetDesktopDownloadDirect 直接重定向到下载链接
func (d *DownloadAPI) GetDesktopDownloadDirect(c *gin.Context) {
	r := c.Request
	// 从 User-Agent 或查询参数获取操作系统信息
	osType := r.URL.Query().Get("os")
	if osType == "" {
		osType = detectOSFromUserAgent(r.UserAgent())
	}

	// 获取最新版本信息
	versionInfo, err := getLatestVersion()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("获取版本信息失败: %v", err)})
		return
	}

	// 根据操作系统生成下载信息
	downloadInfo := generateDownloadInfo(osType, versionInfo)

	// 重定向到下载链接
	c.Redirect(http.StatusFound, downloadInfo.DownloadURL)
}

// ListDesktopVersions 列出所有可用版本
func (d *DownloadAPI) ListDesktopVersions(c *gin.Context) {
	// 获取最新版本信息
	versionInfo, err := getLatestVersion()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("获取版本信息失败: %v", err)})
		return
	}

	// 生成所有平台的下载信息
	downloads := map[string]*DownloadInfo{
		"windows": generateDownloadInfo("windows", versionInfo),
		"linux":   generateDownloadInfo("linux", versionInfo),
		"darwin":  generateDownloadInfo("darwin", versionInfo),
		"macos":   generateDownloadInfo("darwin", versionInfo), // 别名
	}

	response := gin.H{
		"version":    versionInfo.Version,
		"build_date": versionInfo.BuildDate,
		"downloads":  downloads,
	}

	c.JSON(http.StatusOK, response)
}

// detectOSFromUserAgent 从 User-Agent 检测操作系统
func detectOSFromUserAgent(userAgent string) string {
	ua := strings.ToLower(userAgent)

	if strings.Contains(ua, "windows") || strings.Contains(ua, "win64") || strings.Contains(ua, "win32") {
		return "windows"
	}
	if strings.Contains(ua, "macintosh") || strings.Contains(ua, "mac os x") || strings.Contains(ua, "darwin") {
		return "darwin"
	}
	if strings.Contains(ua, "linux") || strings.Contains(ua, "x11") {
		return "linux"
	}

	// 默认返回服务器运行的操作系统
	return runtime.GOOS
}

// getLatestVersion 从配置的地址获取最新版本信息
func getLatestVersion() (*VersionInfo, error) {
	// 从系统配置获取基础地址
	var config model.SystemConfig
	var baseURL string

	if err := db.DB.First(&config).Error; err == nil && config.ClientDownloadURL != "" {
		configURL := strings.TrimSpace(config.ClientDownloadURL)

		// 提取基础路径
		// 如果是完整文件 URL，提取目录部分
		if strings.Contains(configURL, "awecloud-signaling-") &&
			(strings.HasSuffix(configURL, ".exe") ||
				strings.HasSuffix(configURL, ".zip") ||
				!strings.Contains(path.Base(configURL), ".")) {
			baseURL = path.Dir(configURL)
		} else {
			// 如果是目录 URL，去掉尾部斜杠
			baseURL = strings.TrimRight(configURL, "/")
		}
	}

	// 如果没有配置，返回默认版本
	if baseURL == "" {
		return &VersionInfo{
			Version:   "v0.1.0",
			BuildDate: "",
		}, nil
	}

	// 从配置的地址获取 version.json
	versionURL := fmt.Sprintf("%s/version.json", baseURL)

	resp, err := http.Get(versionURL)
	if err != nil {
		// 如果获取失败，返回默认版本
		return &VersionInfo{
			Version:   "v0.1.0",
			BuildDate: "",
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// 如果文件不存在，返回默认版本
		return &VersionInfo{
			Version:   "v0.1.0",
			BuildDate: "",
		}, nil
	}

	var versionInfo VersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&versionInfo); err != nil {
		return nil, fmt.Errorf("解析版本信息失败: %w", err)
	}

	return &versionInfo, nil
}

// generateDownloadInfo 生成下载信息
func generateDownloadInfo(osType string, versionInfo *VersionInfo) *DownloadInfo {
	var filename string
	var arch string

	// 使用带版本号的文件名（兼容现有 Server 存储的 URL）
	switch osType {
	case "windows":
		filename = fmt.Sprintf("awecloud-signaling-%s-windows-amd64.exe", versionInfo.Version)
		arch = "amd64"
	case "linux":
		filename = fmt.Sprintf("awecloud-signaling-%s-linux-amd64", versionInfo.Version)
		arch = "amd64"
	case "darwin", "macos":
		filename = fmt.Sprintf("awecloud-signaling-%s-darwin-universal.zip", versionInfo.Version)
		arch = "universal"
		osType = "darwin"
	default:
		// 默认 Linux
		filename = fmt.Sprintf("awecloud-signaling-%s-linux-amd64", versionInfo.Version)
		arch = "amd64"
		osType = "linux"
	}

	// 从系统配置获取下载地址
	downloadURL := getDownloadURL(filename)

	// 如果没有配置下载地址，返回空 URL（前端需要提示管理员配置）
	if downloadURL == "" {
		downloadURL = "" // 保持为空，让前端显示错误提示
	}

	return &DownloadInfo{
		Version:     versionInfo.Version,
		DownloadURL: downloadURL,
		Filename:    filename,
		OS:          osType,
		Arch:        arch,
		BuildDate:   versionInfo.BuildDate,
	}
}

// getDownloadURL 获取下载 URL
// 从系统配置获取基础地址，拼接文件名
// 支持三种配置格式：
// 1. https://xxx/path/ （带尾部斜杠）
// 2. https://xxx/path （不带尾部斜杠）
// 3. https://xxx/path/awecloud-signaling-v0.1.0-windows-amd64.exe （完整文件 URL）
func getDownloadURL(filename string) string {
	// 从数据库获取系统配置
	var config model.SystemConfig
	if err := db.DB.First(&config).Error; err == nil && config.ClientDownloadURL != "" {
		configURL := strings.TrimSpace(config.ClientDownloadURL)

		// 判断配置的 URL 是否是完整的文件 URL（包含文件名）
		// 检查是否包含 awecloud-signaling- 且以文件扩展名结尾
		if strings.Contains(configURL, "awecloud-signaling-") &&
			(strings.HasSuffix(configURL, ".exe") ||
				strings.HasSuffix(configURL, ".zip") ||
				!strings.Contains(path.Base(configURL), ".")) {
			// 提取基础路径（去掉文件名）
			baseURL := path.Dir(configURL)
			return fmt.Sprintf("%s/%s", baseURL, filename)
		}

		// 如果配置的是目录 URL
		// 去掉尾部斜杠，统一处理
		baseURL := strings.TrimRight(configURL, "/")
		return fmt.Sprintf("%s/%s", baseURL, filename)
	}

	// 如果没有配置，返回空字符串（调用方需要处理）
	return ""
}
