package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// VersionCheckRequest 版本检查请求
type VersionCheckRequest struct {
	ClientVersion string `json:"client_version" binding:"required"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
}

// VersionCheckResponse 版本检查响应
type VersionCheckResponse struct {
	Success      bool   `json:"success"`
	VersionValid bool   `json:"version_valid"`
	MinVersion   string `json:"min_version"`
	DownloadURL  string `json:"download_url"`
	Message      string `json:"message"`
}

// CheckVersion Desktop 客户端版本检查
func CheckVersion(c *gin.Context) {
	var req VersionCheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}

	// 获取系统配置
	var config model.SystemConfig
	if err := db.DB.First(&config, 1).Error; err != nil {
		// 如果配置不存在，默认允许所有版本
		c.JSON(http.StatusOK, VersionCheckResponse{
			Success:      true,
			VersionValid: true,
			MinVersion:   "1.0.0",
			DownloadURL:  "",
			Message:      "版本检查通过",
		})
		return
	}

	// 比较版本号
	minVersion := config.DesktopMinVersion
	if minVersion == "" {
		minVersion = "1.0.0"
	}

	versionValid := compareVersion(req.ClientVersion, minVersion) >= 0

	// 构建下载地址
	downloadURL := config.ClientDownloadURL
	if downloadURL == "" {
		// 使用当前服务器地址 + /download
		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
		downloadURL = scheme + "://" + c.Request.Host + "/download"
	}

	message := "版本检查通过"
	if !versionValid {
		message = "您的客户端版本过低，请升级到最新版本"
	}

	c.JSON(http.StatusOK, VersionCheckResponse{
		Success:      true,
		VersionValid: versionValid,
		MinVersion:   minVersion,
		DownloadURL:  downloadURL,
		Message:      message,
	})
}

// compareVersion 比较两个版本号
// 返回值：-1 表示 v1 < v2，0 表示 v1 == v2，1 表示 v1 > v2
func compareVersion(v1, v2 string) int {
	// 移除 "v" 或 "V" 前缀
	v1 = strings.TrimPrefix(strings.TrimPrefix(v1, "v"), "V")
	v2 = strings.TrimPrefix(strings.TrimPrefix(v2, "v"), "V")

	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	// 补齐长度
	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	for len(parts1) < maxLen {
		parts1 = append(parts1, "0")
	}
	for len(parts2) < maxLen {
		parts2 = append(parts2, "0")
	}

	// 逐段比较
	for i := 0; i < maxLen; i++ {
		n1, err1 := strconv.Atoi(parts1[i])
		n2, err2 := strconv.Atoi(parts2[i])

		// 如果解析失败，按字符串比较
		if err1 != nil || err2 != nil {
			if parts1[i] < parts2[i] {
				return -1
			} else if parts1[i] > parts2[i] {
				return 1
			}
			continue
		}

		if n1 < n2 {
			return -1
		} else if n1 > n2 {
			return 1
		}
	}

	return 0
}
