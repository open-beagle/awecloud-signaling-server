package api

import (
	"net/http"
	"strconv"
	"strings"

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
	UserID          int64  `json:"user_id" binding:"required"`
	ListenPort      int    `json:"listen_port" binding:"required"`
	TargetServiceID int64  `json:"target_service_id" binding:"required"`
}

// List 获取 Visitor 列表
func (v *VisitorAPI) List(c *gin.Context) {
	ctx := c.Request.Context()
	var visitors []model.Visitor

	query := db.DB.WithContext(ctx).Order("created_at DESC")

	// 支持按 User 筛选
	if userID := c.Query("user_id"); userID != "" {
		query = query.Where("user_id = ?", userID)
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
		if err := db.DB.WithContext(ctx).Preload("User").First(&targetService, visitor.TargetServiceID).Error; err == nil {
			result[i].TargetServiceName = targetService.Name
			if targetService.User != nil {
				result[i].TargetUserName = targetService.User.Name
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
	ctx := c.Request.Context()
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, VisitorResponse{
			Success: false,
			Message: "无效的ID",
		})
		return
	}

	var visitor model.Visitor
	if err := db.DB.WithContext(ctx).First(&visitor, id).Error; err != nil {
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
	if err := db.DB.WithContext(ctx).Preload("User").First(&targetService, visitor.TargetServiceID).Error; err == nil {
		result.TargetServiceName = targetService.Name
		if targetService.User != nil {
			result.TargetUserName = targetService.User.Name
		}
	}

	c.JSON(http.StatusOK, VisitorResponse{
		Success: true,
		Data:    result,
	})
}

// Create 创建 Visitor
func (v *VisitorAPI) Create(c *gin.Context) {
	ctx := c.Request.Context()
	var req CreateVisitorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, VisitorResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 验证 User 存在且为 Agent 角色
	var user model.User
	if err := db.DB.WithContext(ctx).Where("id = ? AND role = ?", req.UserID, model.UserRoleAgent).First(&user).Error; err != nil {
		c.JSON(http.StatusBadRequest, VisitorResponse{
			Success: false,
			Message: "Agent 用户不存在",
		})
		return
	}

	// 验证目标服务存在
	var targetService model.ProxyService
	if err := db.DB.WithContext(ctx).First(&targetService, req.TargetServiceID).Error; err != nil {
		c.JSON(http.StatusBadRequest, VisitorResponse{
			Success: false,
			Message: "目标服务不存在",
		})
		return
	}

	// 检查端口是否已被占用
	var existingVisitor model.Visitor
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND listen_port = ?", req.UserID, req.ListenPort).First(&existingVisitor).Error; err == nil {
		c.JSON(http.StatusBadRequest, VisitorResponse{
			Success: false,
			Message: "端口已被占用",
		})
		return
	}

	// 获取目标服务所属 Agent 的 Node IP
	var targetNode model.Node
	db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", targetService.UserID, model.NodeTypeAgent).First(&targetNode)
	targetAddr := targetNode.IP + ":" + strings.Split(targetService.SourceAddr, ":")[1]

	// 创建 Visitor
	visitor := &model.Visitor{
		Name:            req.Name,
		UserID:          req.UserID,
		ListenPort:      req.ListenPort,
		TargetServiceID: req.TargetServiceID,
		TargetAddr:      targetAddr,
		Status:          model.VisitorStatusStopped,
	}

	if err := db.DB.WithContext(ctx).Create(visitor).Error; err != nil {
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
	ctx := c.Request.Context()
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
	if err := db.DB.WithContext(ctx).First(&visitor, id).Error; err != nil {
		c.JSON(http.StatusNotFound, VisitorResponse{
			Success: false,
			Message: "Visitor不存在",
		})
		return
	}

	// TODO: 如果正在运行，先发送停止命令给 Agent

	// 删除 Visitor
	if err := db.DB.WithContext(ctx).Delete(&visitor).Error; err != nil {
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
	ctx := c.Request.Context()
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
	if err := db.DB.WithContext(ctx).First(&visitor, id).Error; err != nil {
		c.JSON(http.StatusNotFound, VisitorResponse{
			Success: false,
			Message: "Visitor不存在",
		})
		return
	}

	// 检查 User 的 Agent Node 是否在线
	var node model.Node
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", visitor.UserID, model.NodeTypeAgent).First(&node).Error; err != nil {
		c.JSON(http.StatusBadRequest, VisitorResponse{
			Success: false,
			Message: "Agent 设备不存在",
		})
		return
	}

	// 检查 Node 是否有 Tailscale IP（表示在线）
	if node.IP == "" {
		c.JSON(http.StatusBadRequest, VisitorResponse{
			Success: false,
			Message: "Agent离线，无法启动",
		})
		return
	}

	// TODO: 发送 START_VISITOR 命令给 Agent

	// 更新状态
	visitor.Status = model.VisitorStatusRunning
	db.DB.WithContext(ctx).Save(&visitor)

	c.JSON(http.StatusOK, VisitorResponse{
		Success: true,
		Message: "启动成功",
	})
}

// Stop 停止 Visitor
func (v *VisitorAPI) Stop(c *gin.Context) {
	ctx := c.Request.Context()
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
	if err := db.DB.WithContext(ctx).First(&visitor, id).Error; err != nil {
		c.JSON(http.StatusNotFound, VisitorResponse{
			Success: false,
			Message: "Visitor不存在",
		})
		return
	}

	// TODO: 发送 STOP_VISITOR 命令给 Agent

	// 更新状态
	visitor.Status = model.VisitorStatusStopped
	db.DB.WithContext(ctx).Save(&visitor)

	c.JSON(http.StatusOK, VisitorResponse{
		Success: true,
		Message: "停止成功",
	})
}
