package api

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	grpcserver "github.com/open-beagle/awecloud-signaling-server/internal/server/grpc"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

type STCPAPI struct {
	agentService *grpcserver.AgentServiceServer
}

func NewSTCPAPI() *STCPAPI {
	return &STCPAPI{}
}

// SetAgentService 设置AgentService（用于发送命令）
func (a *STCPAPI) SetAgentService(service *grpcserver.AgentServiceServer) {
	a.agentService = service
}

type CreateSTCPRequest struct {
	InstanceName string `json:"instance_name" binding:"required"`
	AgentID      int64  `json:"agent_id" binding:"required"`
	LocalIP      string `json:"local_ip" binding:"required"`
	LocalPort    int    `json:"local_port" binding:"required"`
	Description  string `json:"description"`
}

type GrantAccessRequest struct {
	ClientID int64 `json:"client_id" binding:"required"`
}

type STCPResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
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
		Success: true,
		Data:    instances,
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

	// 创建STCP实例
	instance := &model.STCPInstance{
		InstanceName: req.InstanceName,
		AgentID:      req.AgentID,
		SecretKey:    secretKey,
		LocalIP:      req.LocalIP,
		LocalPort:    req.LocalPort,
		Description:  req.Description,
	}

	if err := db.DB.Create(instance).Error; err != nil {
		c.JSON(http.StatusInternalServerError, STCPResponse{
			Success: false,
			Message: "创建失败: " + err.Error(),
		})
		return
	}

	// 通知Agent创建STCP代理
	if a.agentService != nil && a.agentService.IsAgentOnline(req.AgentID) {
		cmd := &pb.Command{
			CommandId:    fmt.Sprintf("create-%d-%d", instance.ID, instance.CreatedAt.Unix()),
			Type:         pb.Command_CREATE_STCP,
			InstanceName: instance.InstanceName,
			SecretKey:    instance.SecretKey,
			LocalIp:      instance.LocalIP,
			LocalPort:    int32(instance.LocalPort),
		}

		if err := a.agentService.SendCommand(req.AgentID, cmd); err != nil {
			log.Printf("发送创建STCP命令失败: %v", err)
			// 不影响API响应，Agent重连后会同步
		} else {
			log.Printf("已发送创建STCP命令: instance=%s, agent_id=%d", instance.InstanceName, req.AgentID)
		}
	} else {
		log.Printf("Agent离线，STCP实例已保存，等待Agent上线后同步: agent_id=%d", req.AgentID)
	}

	c.JSON(http.StatusOK, STCPResponse{
		Success: true,
		Message: "创建成功",
		Data:    instance,
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

	// 查询实例信息
	var instance model.STCPInstance
	if err := db.DB.First(&instance, id).Error; err != nil {
		c.JSON(http.StatusNotFound, STCPResponse{
			Success: false,
			Message: "实例不存在",
		})
		return
	}

	// 通知Agent删除STCP代理
	if a.agentService != nil && a.agentService.IsAgentOnline(instance.AgentID) {
		cmd := &pb.Command{
			CommandId:    fmt.Sprintf("delete-%d-%d", instance.ID, instance.UpdatedAt.Unix()),
			Type:         pb.Command_DELETE_STCP,
			InstanceName: instance.InstanceName,
		}

		if err := a.agentService.SendCommand(instance.AgentID, cmd); err != nil {
			log.Printf("发送删除STCP命令失败: %v", err)
			// 继续删除数据库记录
		} else {
			log.Printf("已发送删除STCP命令: instance=%s, agent_id=%d", instance.InstanceName, instance.AgentID)
		}
	}

	// 删除数据库记录
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

	// 创建访问权限
	access := &model.STCPAccess{
		STCPInstanceID: id,
		ClientID:       req.ClientID,
	}

	if err := db.DB.Create(access).Error; err != nil {
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

	// 删除访问权限
	if err := db.DB.Where("stcp_instance_id = ? AND client_id = ?", id, req.ClientID).
		Delete(&model.STCPAccess{}).Error; err != nil {
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
