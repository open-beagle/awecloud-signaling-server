package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// SystemConfigResponse 系统配置响应
type SystemConfigResponse struct {
	ClientDownloadURL  string `json:"client_download_url"`
	DesktopMinVersion  string `json:"desktop_min_version"`
	HeadscalePublicURL string `json:"headscale_public_url"`
	StunPort           int    `json:"stun_port"`
	IPPrefix           string `json:"ip_prefix"`
	AuthKeyExpiryHours int    `json:"auth_key_expiry_hours"`
	DomainSuffix       string `json:"domain_suffix"`
}

// GetSystemConfig 获取系统配置
func GetSystemConfig(c *gin.Context) {
	ctx := c.Request.Context()
	config := SystemConfigResponse{
		StunPort:           3478,
		AuthKeyExpiryHours: 24,
		DomainSuffix:       model.DefaultDomainSuffix,
	}

	// 从数据库读取配置
	var configs []model.SystemConfig
	db.DB.WithContext(ctx).Find(&configs)

	for _, cfg := range configs {
		switch cfg.Key {
		case model.ConfigClientDownloadURL:
			config.ClientDownloadURL = cfg.Value
		case model.ConfigDesktopMinVersion:
			config.DesktopMinVersion = cfg.Value
		case model.ConfigHeadscalePublicURL:
			config.HeadscalePublicURL = cfg.Value
		case model.ConfigStunPort:
			if v, err := strconv.Atoi(cfg.Value); err == nil {
				config.StunPort = v
			}
		case model.ConfigIPPrefix:
			config.IPPrefix = cfg.Value
		case model.ConfigAuthKeyExpiryHours:
			if v, err := strconv.Atoi(cfg.Value); err == nil {
				config.AuthKeyExpiryHours = v
			}
		case model.ConfigDomainSuffix:
			config.DomainSuffix = cfg.Value
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(config))
}

// UpdateSystemConfigRequest 更新系统配置请求
type UpdateSystemConfigRequest struct {
	ClientDownloadURL  *string `json:"client_download_url"`
	DesktopMinVersion  *string `json:"desktop_min_version"`
	HeadscalePublicURL *string `json:"headscale_public_url"`
	StunPort           *int    `json:"stun_port"`
	IPPrefix           *string `json:"ip_prefix"`
	AuthKeyExpiryHours *int    `json:"auth_key_expiry_hours"`
	DomainSuffix       *string `json:"domain_suffix"`
}

// UpdateSystemConfig 更新系统配置
func UpdateSystemConfig(c *gin.Context) {
	ctx := c.Request.Context()
	var req UpdateSystemConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 更新配置项
	updates := make(map[string]string)

	if req.ClientDownloadURL != nil {
		updates[model.ConfigClientDownloadURL] = *req.ClientDownloadURL
	}
	if req.DesktopMinVersion != nil {
		updates[model.ConfigDesktopMinVersion] = *req.DesktopMinVersion
	}
	if req.HeadscalePublicURL != nil {
		updates[model.ConfigHeadscalePublicURL] = *req.HeadscalePublicURL
	}
	if req.StunPort != nil {
		updates[model.ConfigStunPort] = strconv.Itoa(*req.StunPort)
	}
	if req.IPPrefix != nil {
		updates[model.ConfigIPPrefix] = *req.IPPrefix
	}
	if req.AuthKeyExpiryHours != nil {
		updates[model.ConfigAuthKeyExpiryHours] = strconv.Itoa(*req.AuthKeyExpiryHours)
	}
	if req.DomainSuffix != nil {
		updates[model.ConfigDomainSuffix] = *req.DomainSuffix
	}

	// 保存到数据库
	for key, value := range updates {
		var config model.SystemConfig
		result := db.DB.WithContext(ctx).Where("key = ?", key).First(&config)
		if result.Error != nil {
			// 不存在则创建
			config = model.SystemConfig{Key: key, Value: value}
			db.DB.WithContext(ctx).Create(&config)
		} else {
			// 存在则更新
			config.Value = value
			db.DB.WithContext(ctx).Save(&config)
		}
	}

	logger.Infof("更新系统配置: %v", updates)
	recordAuditLog(ctx, c, model.ActionUpdateSystemConfig, "system", "config", "系统配置", updates)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新成功", nil))
}

// GetPublicSystemConfig 获取公开的系统配置（无需认证）
func GetPublicSystemConfig(c *gin.Context) {
	ctx := c.Request.Context()
	config := struct {
		HeadscalePublicURL string `json:"headscale_public_url"`
		DesktopMinVersion  string `json:"desktop_min_version"`
		ClientDownloadURL  string `json:"client_download_url"`
	}{}

	var configs []model.SystemConfig
	db.DB.WithContext(ctx).Where("key IN ?", []string{
		model.ConfigHeadscalePublicURL,
		model.ConfigDesktopMinVersion,
		model.ConfigClientDownloadURL,
	}).Find(&configs)

	for _, cfg := range configs {
		switch cfg.Key {
		case model.ConfigHeadscalePublicURL:
			config.HeadscalePublicURL = cfg.Value
		case model.ConfigDesktopMinVersion:
			config.DesktopMinVersion = cfg.Value
		case model.ConfigClientDownloadURL:
			config.ClientDownloadURL = cfg.Value
		}
	}

	c.JSON(http.StatusOK, NewSuccessResponse(config))
}
