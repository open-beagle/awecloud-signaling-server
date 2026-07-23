package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// GetLatestVersions 获取最新版本信息
// @Summary 获取最新版本
// @Description 获取 Agent/Desktop/Endpoint 的最新已发布版本号
// @Tags 版本管理
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/admin/version/latest [get]
func GetLatestVersions(c *gin.Context) {
	versions := map[string]string{
		"agent":    "",
		"desktop":  "",
		"endpoint": "",
	}
	for _, component := range []model.Component{model.ComponentAgent, model.ComponentDesktop, model.ComponentEndpoint} {
		var release model.Release
		err := db.DB.Where("component = ? AND status = ?", component, model.ReleaseStatusPublished).
			Order("published_at DESC, created_at DESC").First(&release).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "查询已发布版本失败",
			})
			return
		}
		if err == nil {
			versions[string(component)] = release.Version
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    versions,
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
