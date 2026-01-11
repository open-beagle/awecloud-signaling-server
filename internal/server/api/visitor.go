package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// VisitorAPI Visitor API
type VisitorAPI struct{}

// NewVisitorAPI 创建 VisitorAPI
func NewVisitorAPI() *VisitorAPI {
	return &VisitorAPI{}
}

// VisitorResponse API 响应
type VisitorResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// CreateVisitorRequest 创建 Visitor 请求
type CreateVisitorRequest struct {
	Name            string `json:"name" binding:"required"`
	AgentID         int64  `json:"agent_id" binding:"required"`
	ListenPort      int    `json:"listen_port" binding:"required"`
	TargetServiceID int64  `json:"target_service_id" binding:"required"`
}

// List 获取 Visitor 列表
func (v *VisitorAPI) List(c *gin.Context) {
	var visitors []model.Visitor

	query := db.DB.Order("created_at DESC")

	// 支持按 Agent 筛选
	if agentID := c.Query("agent_id"); agentID != "" {
		query = query.Where("agent_id = ?", agentID)
	}

	if err := query.Find(&visitors).Error; err != nil {
		c.JSON(http.StatusInternalServerError, VisitorResponse{
			Success: false,
			Message: "查询失败",
		})
		return
	}

	// 构建详情列表
	result := make([]model.VisitorWithDetails, len(visitors))
	for i, visitor := range visitors {
		result[i] = model.VisitorWithDetails{
			Visitor: visitor,
		}
		// 查询目标服务信息
		var targetService model.ProxyService
		if err := db.DB.Preload("Agent").First(&targetService, visitor.TargetServiceID).Error; err == nil {
			result[i].TargetServiceName = targetService.Name
			if targetService.Agent != nil {
				result[i].TargetAgentName = targetService.Agent.AgentName
			}
		}
	}

	c.JSON(http.StatusOK, VisitorResponse{
		Success: true,
		Data:    result,
	})
}

// Get 获取单个 Visitor
func (v *VisitorAPI) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, VisitorResponse{
			Success: false,
			Message: "无效的ID",
		})
		return
	}

	var visitor model.Visitor
	if err := db.DB.First(&visitor, id).Error; err != nil {
		c.JSON(http.StatusNotFound, VisitorResponse{
			Success: false,
			Message: "Visitor不存在",
		})
		return
	}

	// 构建详情
	result := model.VisitorWithDetails{
		Visitor: visitor,
	}
	var targetService model.ProxyService
	if err := db.DB.Preload("Agent").First(&targetService, visitor.TargetServiceID).Error; err == nil {
		result.TargetServiceName = targetService.Name
		if targetService.Agent != nil {
			result.TargetAgentName = targetService.Agent.AgentName
		}
	}

	c.JSON(http.StatusOK, VisitorResponse{
		Success: true,
		Data:    result,
	})
}

// Create 创建 Visitor
func (v *VisitorAPI) Create(c *gin.Context) {
	var req CreateVisitorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, VisitorResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 验证 Agent 存在
	var agent model.Agent
	if err := db.DB.First(&agent, req.AgentID).Error; err != nil {
		c.JSON(http.StatusBadRequest, VisitorResponse{
			Success: false,
			Message: "Agent不存在",
		})
		return
	}

	// 验证目标服务存在
	var targetService model.ProxyService
	if err := db.DB.First(&targetService, req.TargetServiceID).Error; err != nil {
		c.JSON(http.StatusBadRequest, VisitorResponse{
			Success: false,
			Message: "目标服务不存在",
		})
		return
	}

	// 检查端口是否已被占用
	var existingVisitor model.Visitor
	if err := db.DB.Where("agent_id = ? AND listen_port = ?", req.AgentID, req.ListenPort).First(&existingVisitor).Error; err == nil {
		c.JSON(http.StatusBadRequest, VisitorResponse{
			Success: false,
			Message: "端口已被占用",
		})
		return
	}

	// 获取目标服务的 Tailscale 地址
	var targetAgent model.Agent
	db.DB.First(&targetAgent, targetService.AgentID)
	targetAddr := targetAgent.TailscaleIP + ":" + strconv.Itoa(targetService.ListenPort)

	// 创建 Visitor
	visitor := &model.Visitor{
		Name:            req.Name,
		AgentID:         req.AgentID,
		ListenPort:      req.ListenPort,
		TargetServiceID: req.TargetServiceID,
		TargetAddr:      targetAddr,
		Status:          model.VisitorStatusStopped,
	}

	if err := db.DB.Create(visitor).Error; err != nil {
		c.JSON(http.StatusInternalServerError, VisitorResponse{
			Success: false,
			Message: "创建失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, VisitorResponse{
		Success: true,
		Message: "创建成功",
		Data:    visitor,
	})
}

// Delete 删除 Visitor
func (v *VisitorAPI) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, VisitorResponse{
			Success: false,
			Message: "无效的ID",
		})
		return
	}

	// 查询 Visitor
	var visitor model.Visitor
	if err := db.DB.First(&visitor, id).Error; err != nil {
		c.JSON(http.StatusNotFound, VisitorResponse{
			Success: false,
			Message: "Visitor不存在",
		})
		return
	}

	// TODO: 如果正在运行，先发送停止命令给 Agent

	// 删除 Visitor
	if err := db.DB.Delete(&visitor).Error; err != nil {
		c.JSON(http.StatusInternalServerError, VisitorResponse{
			Success: false,
			Message: "删除失败",
		})
		return
	}

	c.JSON(http.StatusOK, VisitorResponse{
		Success: true,
		Message: "删除成功",
	})
}

// Start 启动 Visitor
func (v *VisitorAPI) Start(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, VisitorResponse{
			Success: false,
			Message: "无效的ID",
		})
		return
	}

	// 查询 Visitor
	var visitor model.Visitor
	if err := db.DB.First(&visitor, id).Error; err != nil {
		c.JSON(http.StatusNotFound, VisitorResponse{
			Success: false,
			Message: "Visitor不存在",
		})
		return
	}

	// 检查 Agent 是否在线
	var agent model.Agent
	if err := db.DB.First(&agent, visitor.AgentID).Error; err != nil {
		c.JSON(http.StatusBadRequest, VisitorResponse{
			Success: false,
			Message: "Agent不存在",
		})
		return
	}

	if agent.Status != "online" {
		c.JSON(http.StatusBadRequest, VisitorResponse{
			Success: false,
			Message: "Agent离线，无法启动",
		})
		return
	}

	// TODO: 发送 START_VISITOR 命令给 Agent

	// 更新状态
	visitor.Status = model.VisitorStatusRunning
	db.DB.Save(&visitor)

	c.JSON(http.StatusOK, VisitorResponse{
		Success: true,
		Message: "启动成功",
	})
}

// Stop 停止 Visitor
func (v *VisitorAPI) Stop(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, VisitorResponse{
			Success: false,
			Message: "无效的ID",
		})
		return
	}

	// 查询 Visitor
	var visitor model.Visitor
	if err := db.DB.First(&visitor, id).Error; err != nil {
		c.JSON(http.StatusNotFound, VisitorResponse{
			Success: false,
			Message: "Visitor不存在",
		})
		return
	}

	// TODO: 发送 STOP_VISITOR 命令给 Agent

	// 更新状态
	visitor.Status = model.VisitorStatusStopped
	db.DB.Save(&visitor)

	c.JSON(http.StatusOK, VisitorResponse{
		Success: true,
		Message: "停止成功",
	})
}
