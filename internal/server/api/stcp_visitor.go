package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	grpcserver "github.com/open-beagle/awecloud-signaling-server/internal/server/grpc"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// STCPVisitorAPI STCP访问API
type STCPVisitorAPI struct {
	agentService *grpcserver.AgentServiceServer
}

// NewSTCPVisitorAPI 创建STCP访问API
func NewSTCPVisitorAPI() *STCPVisitorAPI {
	return &STCPVisitorAPI{}
}

// SetAgentService 设置AgentService
func (s *STCPVisitorAPI) SetAgentService(agentService *grpcserver.AgentServiceServer) {
	s.agentService = agentService
}

// 全局实例
var globalSTCPVisitorAPI = NewSTCPVisitorAPI()

// GetSTCPVisitors 获取STCP访问列表
func GetSTCPVisitors(c *gin.Context) {
	agentName := c.Query("agent_name")
	enabledStr := c.Query("enabled")

	query := db.DB.Model(&model.STCPVisitor{})

	if agentName != "" {
		query = query.Where("agent_name = ?", agentName)
	}

	if enabledStr != "" {
		enabled := enabledStr == "true" || enabledStr == "1"
		query = query.Where("enabled = ?", enabled)
	}

	var visitors []model.STCPVisitor
	if err := query.Order("created_at DESC").Find(&visitors).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "查询STCP访问失败: " + err.Error(),
		})
		return
	}

	// 直接返回，AgentName已经在模型中
	var result []model.STCPVisitorWithAgent
	for _, visitor := range visitors {
		result = append(result, model.STCPVisitorWithAgent{
			STCPVisitor: visitor,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// CreateSTCPVisitor 创建STCP访问
func CreateSTCPVisitor(c *gin.Context) {
	var req struct {
		VisitorName string `json:"visitor_name" binding:"required"`
		AgentName   string `json:"agent_name" binding:"required"`
		ServerName  string `json:"server_name" binding:"required"`
		SecretKey   string `json:"secret_key" binding:"required"`
		BindAddr    string `json:"bind_addr"`
		BindPort    int    `json:"bind_port" binding:"required"`
		Description string `json:"description"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "请求参数错误: " + err.Error(),
		})
		return
	}

	// 检查visitor名称在该Agent下是否已存在
	var count int64
	if err := db.DB.Model(&model.STCPVisitor{}).
		Where("visitor_name = ? AND agent_name = ?", req.VisitorName, req.AgentName).
		Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "检查visitor名称失败: " + err.Error(),
		})
		return
	}
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"error":   "该Agent下已存在同名STCP访问",
		})
		return
	}

	// 检查Agent是否存在
	var agent model.Agent
	if err := db.DB.Where("agent_name = ?", req.AgentName).First(&agent).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Agent不存在",
		})
		return
	}

	// 设置默认值
	if req.BindAddr == "" {
		req.BindAddr = "127.0.0.1"
	}

	// 创建STCP访问
	visitor := model.STCPVisitor{
		VisitorName: req.VisitorName,
		AgentName:   req.AgentName,
		ServerName:  req.ServerName,
		SecretKey:   req.SecretKey,
		BindAddr:    req.BindAddr,
		BindPort:    req.BindPort,
		Description: req.Description,
		Enabled:     false,
	}

	if err := db.DB.Create(&visitor).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "创建STCP访问失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    visitor,
		"message": "STCP访问创建成功",
	})
}

// UpdateSTCPVisitor 更新STCP访问
func UpdateSTCPVisitor(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Description string `json:"description"`
		BindAddr    string `json:"bind_addr"`
		BindPort    int    `json:"bind_port"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "请求参数错误: " + err.Error(),
		})
		return
	}

	updates := map[string]interface{}{
		"description": req.Description,
	}

	if req.BindAddr != "" {
		updates["bind_addr"] = req.BindAddr
	}
	if req.BindPort > 0 {
		updates["bind_port"] = req.BindPort
	}

	if err := db.DB.Model(&model.STCPVisitor{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "更新STCP访问失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "STCP访问已更新",
	})
}

// DeleteSTCPVisitor 删除STCP访问
func DeleteSTCPVisitor(c *gin.Context) {
	id := c.Param("id")

	var visitor model.STCPVisitor
	if err := db.DB.First(&visitor, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "STCP访问不存在",
		})
		return
	}

	if err := db.DB.Delete(&visitor).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "删除STCP访问失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "STCP访问删除成功",
	})
}

// EnableSTCPVisitor 启用STCP访问
func EnableSTCPVisitor(c *gin.Context) {
	globalSTCPVisitorAPI.EnableSTCPVisitor(c)
}

// EnableSTCPVisitor 启用STCP访问（实例方法）
func (s *STCPVisitorAPI) EnableSTCPVisitor(c *gin.Context) {
	id := c.Param("id")

	var visitor model.STCPVisitor
	if err := db.DB.First(&visitor, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "STCP访问不存在",
		})
		return
	}

	if visitor.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "STCP访问已启用",
		})
		return
	}

	// 获取Agent ID用于在线检查和发送命令
	var agent model.Agent
	if err := db.DB.Where("agent_name = ?", visitor.AgentName).First(&agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "查询Agent失败: " + err.Error(),
		})
		return
	}

	// 发送gRPC命令给Agent创建STCP visitor（如果Agent在线）
	if s.agentService != nil && s.agentService.IsAgentOnline(agent.ID) {
		cmd := &pb.Command{
			CommandId:   fmt.Sprintf("stcp-visitor-enable-%d-%d", visitor.ID, time.Now().Unix()),
			Type:        pb.Command_CREATE_STCP_VISITOR,
			VisitorName: visitor.VisitorName,
			ServerName:  visitor.ServerName,
			SecretKey:   visitor.SecretKey,
			BindAddr:    visitor.BindAddr,
			BindPort:    int32(visitor.BindPort),
		}

		if err := s.agentService.SendCommand(agent.ID, cmd); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "发送命令失败: " + err.Error(),
			})
			return
		}
	}
	// 如果Agent离线，只更新数据库状态，Agent上线后会自动同步

	// 更新状态
	if err := db.DB.Model(&visitor).Update("enabled", true).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "启用STCP访问失败: " + err.Error(),
		})
		return
	}

	message := "STCP访问已启用"
	if s.agentService != nil && !s.agentService.IsAgentOnline(agent.ID) {
		message = "STCP访问已启用（Agent离线，将在Agent上线后自动同步）"
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": message,
	})
}

// DisableSTCPVisitor 禁用STCP访问
func DisableSTCPVisitor(c *gin.Context) {
	globalSTCPVisitorAPI.DisableSTCPVisitor(c)
}

// DisableSTCPVisitor 禁用STCP访问（实例方法）
func (s *STCPVisitorAPI) DisableSTCPVisitor(c *gin.Context) {
	id := c.Param("id")

	var visitor model.STCPVisitor
	if err := db.DB.First(&visitor, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "STCP访问不存在",
		})
		return
	}

	if !visitor.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "STCP访问已禁用",
		})
		return
	}

	// 获取Agent ID用于在线检查和发送命令
	var agent model.Agent
	if err := db.DB.Where("agent_name = ?", visitor.AgentName).First(&agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "查询Agent失败: " + err.Error(),
		})
		return
	}

	// 发送gRPC命令给Agent删除STCP visitor
	if s.agentService != nil {
		cmd := &pb.Command{
			CommandId:   fmt.Sprintf("stcp-visitor-disable-%d-%d", visitor.ID, time.Now().Unix()),
			Type:        pb.Command_DELETE_STCP_VISITOR,
			VisitorName: visitor.VisitorName,
		}

		if s.agentService.IsAgentOnline(agent.ID) {
			if err := s.agentService.SendCommand(agent.ID, cmd); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"error":   "发送命令失败: " + err.Error(),
				})
				return
			}
		}
	}

	// 更新状态
	if err := db.DB.Model(&visitor).Update("enabled", false).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "禁用STCP访问失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "STCP访问已禁用",
	})
}
