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

func (a *AgentAPI) List(c *gin.Context) {
	var agents []model.Agent
	if err := db.DB.Order("created_at DESC").Find(&agents).Error; err != nil {
		c.JSON(http.StatusInternalServerError, AgentResponse{
			Success: false,
			Message: "查询失败",
		})
		return
	}

	// 实时计算在线状态（基于心跳时间，60秒内有心跳认为在线）
	now := time.Now()
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
	}

	c.JSON(http.StatusOK, AgentResponse{
		Success: true,
		Data:    agents,
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
