package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type STCPAPI struct{}

func NewSTCPAPI() *STCPAPI {
	return &STCPAPI{}
}

type CreateSTCPRequest struct {
	AgentID      int64  `json:"agent_id" binding:"required"`
	InstanceName string `json:"instance_name" binding:"required"`
	ServiceType  string `json:"service_type" binding:"required"`
	LocalIP      string `json:"local_ip" binding:"required"`
	LocalPort    int    `json:"local_port" binding:"required"`
}

type GrantAccessRequest struct {
	ClientID int64 `json:"client_id" binding:"required"`
}

type STCPResponse struct {
	Success   bool                 `json:"success"`
	Message   string               `json:"message,omitempty"`
	Instance  *model.STCPInstance  `json:"instance,omitempty"`
	Instances []model.STCPInstance `json:"instances,omitempty"`
}

func (a *STCPAPI) List(c *gin.Context) {
	var instances []model.STCPInstance
	if err := db.DB.Preload("Agent").Order("created_at DESC").Find(&instances).Error; err != nil {
		c.JSON(http.StatusInternalServerError, STCPResponse{
			Success: false,
			Message: "查询失败",
		})
		return
	}

	c.JSON(http.StatusOK, STCPResponse{
		Success:   true,
		Instances: instances,
	})
}

func (a *STCPAPI) Create(c *gin.Context) {
	var req CreateSTCPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, STCPResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 查询Agent
	var agent model.Agent
	if err := db.DB.First(&agent, req.AgentID).Error; err != nil {
		c.JSON(http.StatusNotFound, STCPResponse{
			Success: false,
			Message: "Agent不存在",
		})
		return
	}

	// 生成secret_key
	secretKey, err := generateToken(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, STCPResponse{
			Success: false,
			Message: "生成Secret Key失败",
		})
		return
	}

	// 生成server_name
	serverName := agent.AgentName + "." + req.InstanceName

	// 创建STCP实例
	instance := &model.STCPInstance{
		AgentID:      req.AgentID,
		InstanceName: req.InstanceName,
		ServiceType:  req.ServiceType,
		LocalIP:      req.LocalIP,
		LocalPort:    req.LocalPort,
		SecretKey:    secretKey,
		ServerName:   serverName,
		Status:       "inactive",
	}

	if err := db.DB.Create(instance).Error; err != nil {
		c.JSON(http.StatusInternalServerError, STCPResponse{
			Success: false,
			Message: "创建失败: " + err.Error(),
		})
		return
	}

	// TODO: 通知Agent创建STCP代理

	c.JSON(http.StatusOK, STCPResponse{
		Success:  true,
		Message:  "创建成功",
		Instance: instance,
	})
}

func (a *STCPAPI) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, STCPResponse{
			Success: false,
			Message: "无效的ID",
		})
		return
	}

	// TODO: 通知Agent删除STCP代理

	if err := db.DB.Delete(&model.STCPInstance{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, STCPResponse{
			Success: false,
			Message: "删除失败",
		})
		return
	}

	c.JSON(http.StatusOK, STCPResponse{
		Success: true,
		Message: "删除成功",
	})
}

func (a *STCPAPI) GrantAccess(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, STCPResponse{
			Success: false,
			Message: "无效的ID",
		})
		return
	}

	var req GrantAccessRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, STCPResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 创建权限
	permission := &model.ClientPermission{
		ClientID:       req.ClientID,
		STCPInstanceID: id,
	}

	if err := db.DB.Create(permission).Error; err != nil {
		c.JSON(http.StatusInternalServerError, STCPResponse{
			Success: false,
			Message: "授权失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, STCPResponse{
		Success: true,
		Message: "授权成功",
	})
}

func (a *STCPAPI) RevokeAccess(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, STCPResponse{
			Success: false,
			Message: "无效的ID",
		})
		return
	}

	var req GrantAccessRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, STCPResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 删除权限
	if err := db.DB.Where("client_id = ? AND stcp_instance_id = ?", req.ClientID, id).
		Delete(&model.ClientPermission{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, STCPResponse{
			Success: false,
			Message: "撤销失败",
		})
		return
	}

	c.JSON(http.StatusOK, STCPResponse{
		Success: true,
		Message: "撤销成功",
	})
}
