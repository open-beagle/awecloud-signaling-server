package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"

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
}

type AgentResponse struct {
	Success bool          `json:"success"`
	Message string        `json:"message,omitempty"`
	Agent   *model.Agent  `json:"agent,omitempty"`
	Agents  []model.Agent `json:"agents,omitempty"`
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

	// 不返回token
	for i := range agents {
		agents[i].AgentToken = ""
	}

	c.JSON(http.StatusOK, AgentResponse{
		Success: true,
		Agents:  agents,
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
		Agent:   agent,
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
		Agent:   &agent,
	})
}

func generateToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
