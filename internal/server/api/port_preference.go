package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type PortPreferenceAPI struct{}

func NewPortPreferenceAPI() *PortPreferenceAPI {
	return &PortPreferenceAPI{}
}

// GetPortPreferencesRequest 获取端口偏好请求
type GetPortPreferencesResponse struct {
	Success     bool           `json:"success"`
	Preferences map[string]int `json:"preferences,omitempty"` // key: stcp_instance_id, value: preferred_port
	Message     string         `json:"message,omitempty"`
}

// SavePortPreferenceRequest 保存端口偏好请求
type SavePortPreferenceRequest struct {
	STCPInstanceID int64 `json:"stcp_instance_id" binding:"required"`
	PreferredPort  int   `json:"preferred_port" binding:"required,min=1,max=65535"`
}

// GetPortPreferences 获取用户的端口偏好
func (a *PortPreferenceAPI) GetPortPreferences(c *gin.Context) {
	// 从JWT获取client_id
	clientID, exists := c.Get("client_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, GetPortPreferencesResponse{
			Success: false,
			Message: "未认证",
		})
		return
	}

	// 查询用户的所有端口偏好
	var preferences []model.PortPreference
	if err := db.DB.Where("client_id = ?", int64(clientID.(float64))).
		Find(&preferences).Error; err != nil {
		logger.Warnf("查询端口偏好失败: %v", err)
		c.JSON(http.StatusInternalServerError, GetPortPreferencesResponse{
			Success: false,
			Message: "查询端口偏好失败",
		})
		return
	}

	// 构建响应（map格式）
	prefMap := make(map[string]int)
	for _, pref := range preferences {
		prefMap[string(rune(pref.STCPInstanceID))] = pref.PreferredPort
	}

	c.JSON(http.StatusOK, GetPortPreferencesResponse{
		Success:     true,
		Preferences: prefMap,
	})
}

// SavePortPreference 保存端口偏好
func (a *PortPreferenceAPI) SavePortPreference(c *gin.Context) {
	var req SavePortPreferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请求参数错误",
		})
		return
	}

	// 从JWT获取client_id
	clientID, exists := c.Get("client_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "未认证",
		})
		return
	}

	// 检查端口映射服务是否存在
	var service model.ProxyService
	if err := db.DB.First(&service, req.STCPInstanceID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "服务不存在",
		})
		return
	}

	// 查找或创建端口偏好记录
	var preference model.PortPreference
	result := db.DB.Where("client_id = ? AND stcp_instance_id = ?",
		int64(clientID.(float64)), req.STCPInstanceID).
		First(&preference)

	if result.Error != nil {
		// 记录不存在，创建新记录
		preference = model.PortPreference{
			ClientID:       int64(clientID.(float64)),
			STCPInstanceID: req.STCPInstanceID,
			PreferredPort:  req.PreferredPort,
		}
		if err := db.DB.Create(&preference).Error; err != nil {
			logger.Warnf("创建端口偏好失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "保存端口偏好失败",
			})
			return
		}
	} else {
		// 记录存在，更新
		preference.PreferredPort = req.PreferredPort
		if err := db.DB.Save(&preference).Error; err != nil {
			logger.Warnf("更新端口偏好失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "保存端口偏好失败",
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "端口偏好已保存",
	})
}
