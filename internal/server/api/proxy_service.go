package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	grpcserver "github.com/open-beagle/awecloud-signaling-server/internal/server/grpc"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// ProxyServiceAPI 端口映射服务 API
type ProxyServiceAPI struct {
	agentService *grpcserver.AgentServiceServer
}

// NewProxyServiceAPI 创建 ProxyServiceAPI
func NewProxyServiceAPI() *ProxyServiceAPI {
	return &ProxyServiceAPI{}
}

// SetAgentService 设置 AgentService（用于发送命令）
func (a *ProxyServiceAPI) SetAgentService(service *grpcserver.AgentServiceServer) {
	a.agentService = service
}

// CreateProxyServiceRequest 创建端口映射请求
type CreateProxyServiceRequest struct {
	Name       string `json:"name" binding:"required"`
	AgentID    int64  `json:"agent_id" binding:"required"`
	ListenPort int    `json:"listen_port" binding:"required"`
	TargetAddr string `json:"target_addr" binding:"required"`
	Remark     string `json:"remark"`
	// 权限控制字段
	AccessType string `json:"access_type"` // public, private, group（默认 public）
	OwnerID    int64  `json:"owner_id"`    // 创建者 Client ID（private 时使用）
	GroupID    *int64 `json:"group_id"`    // 所属组 ID（group 时使用）
}

// UpdateProxyServiceRequest 更新端口映射请求
type UpdateProxyServiceRequest struct {
	Name       string `json:"name"`
	ListenPort int    `json:"listen_port"`
	TargetAddr string `json:"target_addr"`
	Remark     string `json:"remark"`
}

// ProxyServiceResponse API 响应
type ProxyServiceResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// ProxyServiceWithAgent 带 Agent 信息的端口映射服务
type ProxyServiceWithAgent struct {
	model.ProxyService
	AgentName        string `json:"agent_name"`
	AgentStatus      string `json:"agent_status"`
	AgentTsIP        string `json:"agent_ts_ip"`
	AgentTsConnected bool   `json:"agent_ts_connected"`
}

// List 获取端口映射服务列表
func (a *ProxyServiceAPI) List(c *gin.Context) {
	var services []model.ProxyService
	if err := db.DB.Preload("Agent").Order("created_at DESC").Find(&services).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ProxyServiceResponse{
			Success: false,
			Message: "查询失败",
		})
		return
	}

	// 构建带 Agent 信息的响应
	result := make([]ProxyServiceWithAgent, 0, len(services))
	for _, svc := range services {
		item := ProxyServiceWithAgent{
			ProxyService: svc,
		}
		if svc.Agent != nil {
			item.AgentName = svc.Agent.AgentName
			item.AgentStatus = svc.Agent.Status
			item.AgentTsIP = svc.Agent.TailscaleIP
			item.AgentTsConnected = svc.Agent.TsConnected
		}
		result = append(result, item)
	}

	c.JSON(http.StatusOK, ProxyServiceResponse{
		Success: true,
		Data:    result,
	})
}

// Create 创建端口映射服务
func (a *ProxyServiceAPI) Create(c *gin.Context) {
	var req CreateProxyServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ProxyServiceResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 验证 Agent 存在
	var agent model.Agent
	if err := db.DB.First(&agent, req.AgentID).Error; err != nil {
		c.JSON(http.StatusNotFound, ProxyServiceResponse{
			Success: false,
			Message: "Agent 不存在",
		})
		return
	}

	// 检查端口是否已被占用
	var existing model.ProxyService
	if err := db.DB.Where("agent_id = ? AND listen_port = ?", req.AgentID, req.ListenPort).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, ProxyServiceResponse{
			Success: false,
			Message: fmt.Sprintf("端口 %d 已被服务 %s 占用", req.ListenPort, existing.Name),
		})
		return
	}

	// 创建服务
	service := &model.ProxyService{
		Name:       req.Name,
		AgentID:    req.AgentID,
		ListenPort: req.ListenPort,
		TargetAddr: req.TargetAddr,
		Status:     model.ProxyStatusStopped,
		Remark:     req.Remark,
		// 权限控制字段
		AccessType: req.AccessType,
		OwnerID:    req.OwnerID,
		GroupID:    req.GroupID,
	}

	// 设置默认访问类型
	if service.AccessType == "" {
		service.AccessType = model.AccessTypePublic
	}

	if err := db.DB.Create(service).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ProxyServiceResponse{
			Success: false,
			Message: "创建失败: " + err.Error(),
		})
		return
	}

	logger.Infof("创建端口映射服务: name=%s, agent_id=%d, port=%d", req.Name, req.AgentID, req.ListenPort)

	c.JSON(http.StatusOK, ProxyServiceResponse{
		Success: true,
		Message: "创建成功",
		Data:    service,
	})
}

// Update 更新端口映射服务
func (a *ProxyServiceAPI) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, ProxyServiceResponse{
			Success: false,
			Message: "无效的 ID",
		})
		return
	}

	var req UpdateProxyServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ProxyServiceResponse{
			Success: false,
			Message: "请求参数错误",
		})
		return
	}

	// 查询服务
	var service model.ProxyService
	if err := db.DB.First(&service, id).Error; err != nil {
		c.JSON(http.StatusNotFound, ProxyServiceResponse{
			Success: false,
			Message: "服务不存在",
		})
		return
	}

	// 如果服务正在运行，不允许修改端口
	if service.Status == model.ProxyStatusRunning && req.ListenPort != 0 && req.ListenPort != service.ListenPort {
		c.JSON(http.StatusBadRequest, ProxyServiceResponse{
			Success: false,
			Message: "服务运行中，请先停止后再修改端口",
		})
		return
	}

	// 更新字段
	if req.Name != "" {
		service.Name = req.Name
	}
	if req.ListenPort != 0 {
		service.ListenPort = req.ListenPort
	}
	if req.TargetAddr != "" {
		service.TargetAddr = req.TargetAddr
	}
	if req.Remark != "" {
		service.Remark = req.Remark
	}

	if err := db.DB.Save(&service).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ProxyServiceResponse{
			Success: false,
			Message: "更新失败",
		})
		return
	}

	c.JSON(http.StatusOK, ProxyServiceResponse{
		Success: true,
		Message: "更新成功",
		Data:    service,
	})
}

// Delete 删除端口映射服务
func (a *ProxyServiceAPI) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, ProxyServiceResponse{
			Success: false,
			Message: "无效的 ID",
		})
		return
	}

	// 查询服务
	var service model.ProxyService
	if err := db.DB.First(&service, id).Error; err != nil {
		c.JSON(http.StatusNotFound, ProxyServiceResponse{
			Success: false,
			Message: "服务不存在",
		})
		return
	}

	// 如果服务正在运行，先停止
	if service.Status == model.ProxyStatusRunning && a.agentService != nil {
		if err := a.agentService.SendProxyCommand(service.AgentID, "stop", &service); err != nil {
			logger.Warnf("停止服务失败: %v", err)
		}
	}

	// 删除服务
	if err := db.DB.Delete(&model.ProxyService{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ProxyServiceResponse{
			Success: false,
			Message: "删除失败",
		})
		return
	}

	logger.Infof("删除端口映射服务: id=%d, name=%s", id, service.Name)

	c.JSON(http.StatusOK, ProxyServiceResponse{
		Success: true,
		Message: "删除成功",
	})
}

// Start 启动端口映射服务
func (a *ProxyServiceAPI) Start(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, ProxyServiceResponse{
			Success: false,
			Message: "无效的 ID",
		})
		return
	}

	// 查询服务
	var service model.ProxyService
	if err := db.DB.Preload("Agent").First(&service, id).Error; err != nil {
		c.JSON(http.StatusNotFound, ProxyServiceResponse{
			Success: false,
			Message: "服务不存在",
		})
		return
	}

	// 检查 Agent 是否在线
	if a.agentService == nil || !a.agentService.IsAgentOnline(service.AgentID) {
		c.JSON(http.StatusBadRequest, ProxyServiceResponse{
			Success: false,
			Message: "Agent 离线，无法启动服务",
		})
		return
	}

	// 发送启动命令
	if err := a.agentService.SendProxyCommand(service.AgentID, "start", &service); err != nil {
		c.JSON(http.StatusInternalServerError, ProxyServiceResponse{
			Success: false,
			Message: "发送启动命令失败: " + err.Error(),
		})
		return
	}

	// 更新状态
	service.Status = model.ProxyStatusRunning
	if err := db.DB.Save(&service).Error; err != nil {
		logger.Warnf("更新服务状态失败: %v", err)
	}

	logger.Infof("启动端口映射服务: id=%d, name=%s", id, service.Name)

	c.JSON(http.StatusOK, ProxyServiceResponse{
		Success: true,
		Message: "启动成功",
		Data:    service,
	})
}

// Stop 停止端口映射服务
func (a *ProxyServiceAPI) Stop(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, ProxyServiceResponse{
			Success: false,
			Message: "无效的 ID",
		})
		return
	}

	// 查询服务
	var service model.ProxyService
	if err := db.DB.First(&service, id).Error; err != nil {
		c.JSON(http.StatusNotFound, ProxyServiceResponse{
			Success: false,
			Message: "服务不存在",
		})
		return
	}

	// 发送停止命令（即使 Agent 离线也更新状态）
	if a.agentService != nil && a.agentService.IsAgentOnline(service.AgentID) {
		if err := a.agentService.SendProxyCommand(service.AgentID, "stop", &service); err != nil {
			logger.Warnf("发送停止命令失败: %v", err)
		}
	}

	// 更新状态
	service.Status = model.ProxyStatusStopped
	service.Connections = 0
	if err := db.DB.Save(&service).Error; err != nil {
		logger.Warnf("更新服务状态失败: %v", err)
	}

	logger.Infof("停止端口映射服务: id=%d, name=%s", id, service.Name)

	c.JSON(http.StatusOK, ProxyServiceResponse{
		Success: true,
		Message: "停止成功",
		Data:    service,
	})
}

// Stats 获取服务统计信息
func (a *ProxyServiceAPI) Stats(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, ProxyServiceResponse{
			Success: false,
			Message: "无效的 ID",
		})
		return
	}

	// 查询服务
	var service model.ProxyService
	if err := db.DB.First(&service, id).Error; err != nil {
		c.JSON(http.StatusNotFound, ProxyServiceResponse{
			Success: false,
			Message: "服务不存在",
		})
		return
	}

	stats := map[string]interface{}{
		"id":          service.ID,
		"name":        service.Name,
		"status":      service.Status,
		"connections": service.Connections,
		"bytes_in":    service.BytesIn,
		"bytes_out":   service.BytesOut,
	}

	c.JSON(http.StatusOK, ProxyServiceResponse{
		Success: true,
		Data:    stats,
	})
}

// ListByAgent 获取指定 Agent 的端口映射服务列表
func (a *ProxyServiceAPI) ListByAgent(c *gin.Context) {
	agentID, err := strconv.ParseInt(c.Param("agent_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, ProxyServiceResponse{
			Success: false,
			Message: "无效的 Agent ID",
		})
		return
	}

	var services []model.ProxyService
	if err := db.DB.Where("agent_id = ?", agentID).Order("created_at DESC").Find(&services).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ProxyServiceResponse{
			Success: false,
			Message: "查询失败",
		})
		return
	}

	c.JSON(http.StatusOK, ProxyServiceResponse{
		Success: true,
		Data:    services,
	})
}
