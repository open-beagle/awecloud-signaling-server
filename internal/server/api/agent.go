package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type AgentAPI struct{}

func NewAgentAPI() *AgentAPI {
	return &AgentAPI{}
}

type CreateAgentRequest struct {
	AgentName string `json:"agent_name" binding:"required"`
	GroupName string `json:"group_name"` // 分组名称（可选）
}

type UpdateAgentRequest struct {
	GroupName   *string `json:"group_name"`  // 分组名称
	Description *string `json:"description"` // 描述
}

type AgentResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// AgentListItem 用于列表响应，包含服务数量
type AgentListItem struct {
	model.Agent
	ServiceCount int64 `json:"service_count"` // 端口映射服务数量
}

func (a *AgentAPI) List(c *gin.Context) {
	var agents []model.Agent
	if err := db.DB.Order("created_at DESC").Find(&agents).Error; err != nil {
		c.JSON(http.StatusInternalServerError, AgentResponse{
			Success: false,
			Message: "查询失败",
		})
		return
	}

	// 查询每个 Agent 的服务数量
	var serviceCounts []struct {
		AgentID int64 `gorm:"column:agent_id"`
		Count   int64 `gorm:"column:count"`
	}
	db.DB.Model(&model.ProxyService{}).
		Select("agent_id, COUNT(*) as count").
		Group("agent_id").
		Find(&serviceCounts)

	// 构建 Agent ID 到服务数量的映射
	serviceCountMap := make(map[int64]int64)
	for _, sc := range serviceCounts {
		serviceCountMap[sc.AgentID] = sc.Count
	}

	// 实时计算在线状态（基于心跳时间，60秒内有心跳认为在线）
	now := time.Now()
	result := make([]AgentListItem, len(agents))
	for i := range agents {
		agents[i].AgentToken = "" // 不返回token
		if agents[i].LastHeartbeat != nil {
			heartbeatAge := now.Sub(*agents[i].LastHeartbeat)
			if heartbeatAge < 60*time.Second {
				agents[i].Status = "online"
			} else {
				agents[i].Status = "offline"
			}
		} else {
			agents[i].Status = "offline"
		}

		result[i] = AgentListItem{
			Agent:        agents[i],
			ServiceCount: serviceCountMap[agents[i].ID],
		}
	}

	c.JSON(http.StatusOK, AgentResponse{
		Success: true,
		Data:    result,
	})
}

// AgentDetailItem 用于详情响应，包含服务列表
type AgentDetailItem struct {
	model.Agent
	ServiceCount int64                `json:"service_count"` // 端口映射服务数量
	Services     []model.ProxyService `json:"services"`      // 端口映射服务列表
}

// Get 获取单个 Agent 详情
func (a *AgentAPI) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, AgentResponse{
			Success: false,
			Message: "无效的ID",
		})
		return
	}

	var agent model.Agent
	if err := db.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, AgentResponse{
			Success: false,
			Message: "Agent不存在",
		})
		return
	}

	// 不返回 token
	agent.AgentToken = ""

	// 实时计算在线状态
	now := time.Now()
	if agent.LastHeartbeat != nil {
		heartbeatAge := now.Sub(*agent.LastHeartbeat)
		if heartbeatAge < 60*time.Second {
			agent.Status = "online"
		} else {
			agent.Status = "offline"
		}
	} else {
		agent.Status = "offline"
	}

	// 查询该 Agent 的服务列表
	var services []model.ProxyService
	db.DB.Where("agent_id = ?", id).Order("created_at DESC").Find(&services)

	result := AgentDetailItem{
		Agent:        agent,
		ServiceCount: int64(len(services)),
		Services:     services,
	}

	c.JSON(http.StatusOK, AgentResponse{
		Success: true,
		Data:    result,
	})
}

func (a *AgentAPI) Create(c *gin.Context) {
	var req CreateAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, AgentResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 生成随机token
	token, err := generateToken(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, AgentResponse{
			Success: false,
			Message: "生成Token失败",
		})
		return
	}

	// 创建Agent
	agent := &model.Agent{
		AgentName:  req.AgentName,
		AgentToken: token,
		Status:     "offline",
		GroupName:  req.GroupName, // 设置分组
	}

	if err := db.DB.Create(agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, AgentResponse{
			Success: false,
			Message: "创建失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, AgentResponse{
		Success: true,
		Message: "创建成功",
		Data:    agent,
	})
}

// Update 更新 Agent 信息（分组、描述等）
func (a *AgentAPI) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, AgentResponse{
			Success: false,
			Message: "无效的ID",
		})
		return
	}

	var req UpdateAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, AgentResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 查询 Agent
	var agent model.Agent
	if err := db.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, AgentResponse{
			Success: false,
			Message: "Agent不存在",
		})
		return
	}

	// 更新字段
	if req.GroupName != nil {
		agent.GroupName = *req.GroupName
	}
	if req.Description != nil {
		agent.Description = *req.Description
	}

	if err := db.DB.Save(&agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, AgentResponse{
			Success: false,
			Message: "更新失败",
		})
		return
	}

	c.JSON(http.StatusOK, AgentResponse{
		Success: true,
		Message: "更新成功",
		Data:    agent,
	})
}

func (a *AgentAPI) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, AgentResponse{
			Success: false,
			Message: "无效的ID",
		})
		return
	}

	// 删除Agent
	if err := db.DB.Delete(&model.Agent{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, AgentResponse{
			Success: false,
			Message: "删除失败",
		})
		return
	}

	c.JSON(http.StatusOK, AgentResponse{
		Success: true,
		Message: "删除成功",
	})
}

func (a *AgentAPI) RegenerateToken(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, AgentResponse{
			Success: false,
			Message: "无效的ID",
		})
		return
	}

	// 生成新token
	token, err := generateToken(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, AgentResponse{
			Success: false,
			Message: "生成Token失败",
		})
		return
	}

	// 更新Agent
	var agent model.Agent
	if err := db.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, AgentResponse{
			Success: false,
			Message: "Agent不存在",
		})
		return
	}

	agent.AgentToken = token
	if err := db.DB.Save(&agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, AgentResponse{
			Success: false,
			Message: "更新失败",
		})
		return
	}

	c.JSON(http.StatusOK, AgentResponse{
		Success: true,
		Message: "Token重新生成成功",
		Data:    map[string]string{"agent_token": agent.AgentToken},
	})
}

func generateToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
