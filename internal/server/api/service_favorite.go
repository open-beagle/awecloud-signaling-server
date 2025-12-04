package api

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type ServiceFavoriteAPI struct{}

func NewServiceFavoriteAPI() *ServiceFavoriteAPI {
	return &ServiceFavoriteAPI{}
}

// FavoriteInfo 收藏信息
type FavoriteInfo struct {
	STCPInstanceID int64 `json:"stcp_instance_id"`
	LocalPort      int   `json:"local_port"`
}

// GetServiceFavoritesResponse 获取服务收藏响应
type GetServiceFavoritesResponse struct {
	Success   bool           `json:"success"`
	Favorites []FavoriteInfo `json:"favorites,omitempty"` // 收藏列表（包含端口）
	Message   string         `json:"message,omitempty"`
}

// ToggleFavoriteRequest 切换收藏请求
type ToggleFavoriteRequest struct {
	STCPInstanceID int64 `json:"stcp_instance_id" binding:"required"`
	LocalPort      int   `json:"local_port,omitempty"` // 可选的本地端口
}

// ToggleFavoriteResponse 切换收藏响应
type ToggleFavoriteResponse struct {
	Success    bool   `json:"success"`
	IsFavorite bool   `json:"is_favorite"` // 切换后的状态
	Message    string `json:"message,omitempty"`
}

// GetServiceFavorites 获取用户的服务收藏列表
func (a *ServiceFavoriteAPI) GetServiceFavorites(c *gin.Context) {
	// 从JWT获取client_id
	clientID, exists := c.Get("client_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, GetServiceFavoritesResponse{
			Success: false,
			Message: "未认证",
		})
		return
	}

	// 查询用户的所有收藏
	var favorites []model.ServiceFavorite
	if err := db.DB.Where("client_id = ?", int64(clientID.(float64))).
		Find(&favorites).Error; err != nil {
		log.Printf("查询服务收藏失败: %v", err)
		c.JSON(http.StatusInternalServerError, GetServiceFavoritesResponse{
			Success: false,
			Message: "查询服务收藏失败",
		})
		return
	}

	// 构建响应（包含端口信息）
	favoriteInfos := make([]FavoriteInfo, 0, len(favorites))
	for _, fav := range favorites {
		favoriteInfos = append(favoriteInfos, FavoriteInfo{
			STCPInstanceID: fav.STCPInstanceID,
			LocalPort:      fav.LocalPort,
		})
	}

	c.JSON(http.StatusOK, GetServiceFavoritesResponse{
		Success:   true,
		Favorites: favoriteInfos,
	})
}

// ToggleFavorite 切换服务收藏状态（收藏/取消收藏）
func (a *ServiceFavoriteAPI) ToggleFavorite(c *gin.Context) {
	var req ToggleFavoriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ToggleFavoriteResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	log.Printf("[ServiceFavorite] ToggleFavorite: instance_id=%d, local_port=%d", req.STCPInstanceID, req.LocalPort)

	// 从JWT获取client_id
	clientID, exists := c.Get("client_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, ToggleFavoriteResponse{
			Success: false,
			Message: "未认证",
		})
		return
	}

	// 检查STCP实例是否存在
	var instance model.STCPInstance
	if err := db.DB.First(&instance, req.STCPInstanceID).Error; err != nil {
		c.JSON(http.StatusNotFound, ToggleFavoriteResponse{
			Success: false,
			Message: "服务实例不存在",
		})
		return
	}

	// 查找收藏记录
	var favorite model.ServiceFavorite
	result := db.DB.Where("client_id = ? AND stcp_instance_id = ?",
		int64(clientID.(float64)), req.STCPInstanceID).
		First(&favorite)

	isFavorite := false

	if result.Error != nil {
		// 记录不存在，创建收藏
		favorite = model.ServiceFavorite{
			ClientID:       int64(clientID.(float64)),
			STCPInstanceID: req.STCPInstanceID,
			LocalPort:      req.LocalPort,
		}
		log.Printf("[ServiceFavorite] Creating favorite: client_id=%d, instance_id=%d, local_port=%d",
			favorite.ClientID, favorite.STCPInstanceID, favorite.LocalPort)
		if err := db.DB.Create(&favorite).Error; err != nil {
			log.Printf("创建服务收藏失败: %v", err)
			c.JSON(http.StatusInternalServerError, ToggleFavoriteResponse{
				Success: false,
				Message: "收藏失败",
			})
			return
		}
		isFavorite = true
	} else {
		// 记录存在，删除收藏
		if err := db.DB.Delete(&favorite).Error; err != nil {
			log.Printf("删除服务收藏失败: %v", err)
			c.JSON(http.StatusInternalServerError, ToggleFavoriteResponse{
				Success: false,
				Message: "取消收藏失败",
			})
			return
		}
		isFavorite = false
	}

	c.JSON(http.StatusOK, ToggleFavoriteResponse{
		Success:    true,
		IsFavorite: isFavorite,
		Message:    map[bool]string{true: "已收藏", false: "已取消收藏"}[isFavorite],
	})
}

// UpdateFavoritePortRequest 更新收藏端口请求
type UpdateFavoritePortRequest struct {
	STCPInstanceID int64 `json:"stcp_instance_id" binding:"required"`
	LocalPort      int   `json:"local_port" binding:"required,min=1,max=65535"`
}

// UpdateFavoritePort 更新收藏服务的端口
func (a *ServiceFavoriteAPI) UpdateFavoritePort(c *gin.Context) {
	var req UpdateFavoritePortRequest
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

	// 查找收藏记录
	var favorite model.ServiceFavorite
	result := db.DB.Where("client_id = ? AND stcp_instance_id = ?",
		int64(clientID.(float64)), req.STCPInstanceID).
		First(&favorite)

	if result.Error != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "收藏记录不存在",
		})
		return
	}

	// 更新端口
	favorite.LocalPort = req.LocalPort
	if err := db.DB.Save(&favorite).Error; err != nil {
		log.Printf("更新收藏端口失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "更新端口失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "端口已更新",
	})
}

// FavoriteListItem 收藏列表项（管理员视图）
type FavoriteListItem struct {
	ID             int64  `json:"id"`
	ClientID       int64  `json:"client_id"`
	ClientName     string `json:"client_name"`
	STCPInstanceID int64  `json:"stcp_instance_id"`
	InstanceName   string `json:"instance_name"`
	AgentName      string `json:"agent_name"`
	LocalPort      int    `json:"local_port"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// GetAllFavoritesResponse 获取所有收藏响应（管理员）
type GetAllFavoritesResponse struct {
	Success    bool               `json:"success"`
	Favorites  []FavoriteListItem `json:"favorites"`
	TotalCount int64              `json:"total_count"`
	Message    string             `json:"message,omitempty"`
}

// GetAllFavorites 获取所有用户的收藏列表（管理员）
func (a *ServiceFavoriteAPI) GetAllFavorites(c *gin.Context) {
	log.Printf("[ServiceFavorite] GetAllFavorites called")

	// 查询所有收藏记录，关联用户和服务信息
	var favorites []model.ServiceFavorite
	if err := db.DB.Preload("Client").Preload("STCPInstance").Preload("STCPInstance.Agent").
		Order("created_at DESC").
		Find(&favorites).Error; err != nil {
		log.Printf("查询所有收藏失败: %v", err)
		c.JSON(http.StatusInternalServerError, GetAllFavoritesResponse{
			Success: false,
			Message: "查询收藏列表失败",
		})
		return
	}

	log.Printf("[ServiceFavorite] Found %d favorites", len(favorites))

	// 构建响应
	items := make([]FavoriteListItem, 0, len(favorites))
	for _, fav := range favorites {
		item := FavoriteListItem{
			ID:             fav.ID,
			ClientID:       fav.ClientID,
			STCPInstanceID: fav.STCPInstanceID,
			LocalPort:      fav.LocalPort,
			CreatedAt:      fav.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt:      fav.UpdatedAt.Format("2006-01-02 15:04:05"),
		}

		// 获取Client名称
		if fav.Client.ID > 0 {
			item.ClientName = fav.Client.ClientID
		}

		// 获取STCP实例名称和Agent名称
		if fav.STCPInstance.ID > 0 {
			item.InstanceName = fav.STCPInstance.InstanceName
			if fav.STCPInstance.Agent.ID > 0 {
				item.AgentName = fav.STCPInstance.Agent.AgentName
			}
		}

		items = append(items, item)
	}

	log.Printf("[ServiceFavorite] Returning %d items", len(items))

	c.JSON(http.StatusOK, GetAllFavoritesResponse{
		Success:    true,
		Favorites:  items,
		TotalCount: int64(len(items)),
	})
}
