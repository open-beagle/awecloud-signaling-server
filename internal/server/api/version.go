package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// GetLatestVersions 获取最新版本信息
// @Summary 获取最新版本
// @Description 获取 Agent/Desktop/Endpoint 的最新版本号（从已连接客户端统计）
// @Tags 版本管理
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/admin/version/latest [get]
func GetLatestVersions(c *gin.Context) {
	// 统计 Agent 最新版本（从 nodes 表）
	var agentLatest string
	err := db.DB.Model(&model.Node{}).
		Where("version IS NOT NULL AND version != ''").
		Order("version DESC").
		Limit(1).
		Pluck("version", &agentLatest).Error

	if err != nil && err != gorm.ErrRecordNotFound {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "查询 Agent 版本失败",
		})
		return
	}

	// 统计 Endpoint 最新版本（从 endpoints 表）
	var endpointLatest string
	err = db.DB.Model(&model.Endpoint{}).
		Where("version IS NOT NULL AND version != ''").
		Order("version DESC").
		Limit(1).
		Pluck("version", &endpointLatest).Error

	if err != nil && err != gorm.ErrRecordNotFound {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "查询 Endpoint 版本失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"agent":    agentLatest,
			"desktop":  "", // Desktop 暂不实现
			"endpoint": endpointLatest,
		},
	})
}

// VersionAPI 版本管理 API
type VersionAPI struct {
	config *config.ServerConfig
}

// NewVersionAPI 创建版本管理 API
func NewVersionAPI(cfg *config.ServerConfig) *VersionAPI {
	return &VersionAPI{
		config: cfg,
	}
}

// GetLatest 获取最新版本信息
func (api *VersionAPI) GetLatest(c *gin.Context) {
	GetLatestVersions(c)
}
