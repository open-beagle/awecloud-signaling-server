package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// GetSystemConfig 获取系统配置
func GetSystemConfig(c *gin.Context) {
	var config model.SystemConfig

	// 查找配置（ID=1 是唯一的系统配置）
	if err := db.DB.First(&config, 1).Error; err != nil {
		// 如果不存在，返回默认配置
		config = model.SystemConfig{
			ID:                 1,
			ClientDownloadURL:  "",
			DesktopMinVersion:  "1.0.0",
			StunPort:           3479,
			IPPrefix:           "100.64.0.0/10",
			AuthKeyExpiryHours: 24,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    config,
	})
}

// UpdateSystemConfig 更新系统配置
func UpdateSystemConfig(c *gin.Context) {
	var req struct {
		ClientDownloadURL  string `json:"client_download_url"`
		DesktopMinVersion  string `json:"desktop_min_version"`
		HeadscalePublicURL string `json:"headscale_public_url"`
		DerpURL            string `json:"derp_url"`
		StunPort           int    `json:"stun_port"`
		IPPrefix           string `json:"ip_prefix"`
		AuthKeyExpiryHours int    `json:"auth_key_expiry_hours"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}

	var config model.SystemConfig

	// 查找或创建配置
	if err := db.DB.First(&config, 1).Error; err != nil {
		// 不存在则创建
		config = model.SystemConfig{
			ID:                 1,
			ClientDownloadURL:  req.ClientDownloadURL,
			DesktopMinVersion:  req.DesktopMinVersion,
			HeadscalePublicURL: req.HeadscalePublicURL,
			DerpURL:            req.DerpURL,
			StunPort:           req.StunPort,
			IPPrefix:           req.IPPrefix,
			AuthKeyExpiryHours: req.AuthKeyExpiryHours,
		}
		if config.DesktopMinVersion == "" {
			config.DesktopMinVersion = "1.0.0"
		}
		if config.StunPort == 0 {
			config.StunPort = 3479
		}
		if config.IPPrefix == "" {
			config.IPPrefix = "100.64.0.0/10"
		}
		if config.AuthKeyExpiryHours == 0 {
			config.AuthKeyExpiryHours = 24
		}
		if err := db.DB.Create(&config).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "创建配置失败",
			})
			return
		}
	} else {
		// 存在则更新
		config.ClientDownloadURL = req.ClientDownloadURL
		if req.DesktopMinVersion != "" {
			config.DesktopMinVersion = req.DesktopMinVersion
		}
		config.HeadscalePublicURL = req.HeadscalePublicURL
		config.DerpURL = req.DerpURL
		if req.StunPort > 0 {
			config.StunPort = req.StunPort
		}
		if req.IPPrefix != "" {
			config.IPPrefix = req.IPPrefix
		}
		if req.AuthKeyExpiryHours > 0 {
			config.AuthKeyExpiryHours = req.AuthKeyExpiryHours
		}
		if err := db.DB.Save(&config).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "更新配置失败",
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "更新成功",
		"data":    config,
	})
}

// GetPublicSystemConfig 获取公开的系统配置（不需要认证）
func GetPublicSystemConfig(c *gin.Context) {
	var config model.SystemConfig

	// 查找配置
	if err := db.DB.First(&config, 1).Error; err != nil {
		// 如果不存在，返回默认配置
		config = model.SystemConfig{
			ClientDownloadURL: "",
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"client_download_url": config.ClientDownloadURL,
		},
	})
}
