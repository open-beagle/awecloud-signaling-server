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
	Success bool           `json:"success"`
	Message string         `json:"message,omitempty"`
	Client  *model.Client  `json:"client,omitempty"`
	Clients []model.Client `json:"clients,omitempty"`
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

func (a *ClientAPI) Create(c *gin.Context) {
	// 生成client_id和client_secret
	clientID, err := generateToken(16)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ClientResponse{
			Success: false,
			Message: "生成Client ID失败",
		})
		return
	}

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
		ClientID:     "client-" + clientID,
		ClientSecret: clientSecret,
		Status:       "active",
		IsOnline:     false,
	}

	if err := db.DB.Create(client).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ClientResponse{
			Success: false,
			Message: "创建失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, ClientResponse{
		Success: true,
		Message: "创建成功",
		Client:  client,
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

	if err := db.DB.Model(&model.Client{}).Where("id = ?", id).Update("status", "disabled").Error; err != nil {
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

	if err := db.DB.Model(&model.Client{}).Where("id = ?", id).Update("status", "active").Error; err != nil {
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
