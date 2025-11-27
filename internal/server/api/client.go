package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type ClientAPI struct{}

func NewClientAPI() *ClientAPI {
	return &ClientAPI{}
}

type ClientResponse struct {
	Success      bool           `json:"success"`
	Message      string         `json:"message,omitempty"`
	Client       *model.Client  `json:"client,omitempty"`
	Clients      []model.Client `json:"clients,omitempty"`
	ClientSecret string         `json:"client_secret,omitempty"` // 用于返回新生成的secret
}

func (a *ClientAPI) List(c *gin.Context) {
	var clients []model.Client
	if err := db.DB.Order("created_at DESC").Find(&clients).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ClientResponse{
			Success: false,
			Message: "查询失败",
		})
		return
	}

	// 不返回secret
	for i := range clients {
		clients[i].ClientSecret = ""
	}

	c.JSON(http.StatusOK, ClientResponse{
		Success: true,
		Clients: clients,
	})
}

type CreateClientRequest struct {
	ClientID string `json:"client_id" binding:"required"` // 用户名或邮箱
}

func (a *ClientAPI) Create(c *gin.Context) {
	var req CreateClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ClientResponse{
			Success: false,
			Message: "请求参数错误：需要提供client_id",
		})
		return
	}

	// 检查client_id是否已存在
	var existingClient model.Client
	if err := db.DB.Where("client_id = ?", req.ClientID).First(&existingClient).Error; err == nil {
		c.JSON(http.StatusConflict, ClientResponse{
			Success: false,
			Message: "Client ID已存在",
		})
		return
	}

	// 生成client_secret
	clientSecret, err := generateToken(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ClientResponse{
			Success: false,
			Message: "生成Client Secret失败",
		})
		return
	}

	// 创建Client
	client := &model.Client{
		ClientID:     req.ClientID, // 使用指定的client_id
		ClientSecret: clientSecret,
		Enabled:      true,
	}

	if err := db.DB.Create(client).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ClientResponse{
			Success: false,
			Message: "创建失败: " + err.Error(),
		})
		return
	}

	// 返回时包含secret（只在创建时返回一次）
	c.JSON(http.StatusOK, ClientResponse{
		Success:      true,
		Message:      "创建成功",
		Client:       client,
		ClientSecret: clientSecret, // 明文返回secret
	})
}

// RegenerateSecret 重新生成Client Secret
func (a *ClientAPI) RegenerateSecret(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, ClientResponse{
			Success: false,
			Message: "无效的ID",
		})
		return
	}

	// 生成新secret
	clientSecret, err := generateToken(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ClientResponse{
			Success: false,
			Message: "生成Client Secret失败",
		})
		return
	}

	// 更新Client
	var client model.Client
	if err := db.DB.First(&client, id).Error; err != nil {
		c.JSON(http.StatusNotFound, ClientResponse{
			Success: false,
			Message: "Client不存在",
		})
		return
	}

	client.ClientSecret = clientSecret
	if err := db.DB.Save(&client).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ClientResponse{
			Success: false,
			Message: "更新失败",
		})
		return
	}

	// 返回时包含新的secret
	c.JSON(http.StatusOK, ClientResponse{
		Success:      true,
		Message:      "Secret重新生成成功",
		Client:       &client,
		ClientSecret: clientSecret, // 明文返回新secret
	})
}

func (a *ClientAPI) Disable(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, ClientResponse{
			Success: false,
			Message: "无效的ID",
		})
		return
	}

	if err := db.DB.Model(&model.Client{}).Where("id = ?", id).Update("enabled", false).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ClientResponse{
			Success: false,
			Message: "禁用失败",
		})
		return
	}

	c.JSON(http.StatusOK, ClientResponse{
		Success: true,
		Message: "禁用成功",
	})
}

func (a *ClientAPI) Enable(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, ClientResponse{
			Success: false,
			Message: "无效的ID",
		})
		return
	}

	if err := db.DB.Model(&model.Client{}).Where("id = ?", id).Update("enabled", true).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ClientResponse{
			Success: false,
			Message: "启用失败",
		})
		return
	}

	c.JSON(http.StatusOK, ClientResponse{
		Success: true,
		Message: "启用成功",
	})
}

func (a *ClientAPI) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, ClientResponse{
			Success: false,
			Message: "无效的ID",
		})
		return
	}

	if err := db.DB.Delete(&model.Client{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ClientResponse{
			Success: false,
			Message: "删除失败",
		})
		return
	}

	c.JSON(http.StatusOK, ClientResponse{
		Success: true,
		Message: "删除成功",
	})
}
