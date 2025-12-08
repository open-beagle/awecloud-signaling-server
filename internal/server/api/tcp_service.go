package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	grpcserver "github.com/open-beagle/awecloud-signaling-server/internal/server/grpc"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// TCPServiceAPI TCP实例API
type TCPServiceAPI struct {
	agentService *grpcserver.AgentServiceServer
}

// NewTCPServiceAPI 创建TCP服务API
func NewTCPServiceAPI() *TCPServiceAPI {
	return &TCPServiceAPI{}
}

// SetAgentService 设置AgentService
func (t *TCPServiceAPI) SetAgentService(agentService *grpcserver.AgentServiceServer) {
	t.agentService = agentService
}

// 全局实例（用于向后兼容）
var globalTCPServiceAPI = NewTCPServiceAPI()

// GetTCPServices 获取TCP服务列表
func GetTCPServices(c *gin.Context) {
	agentIDStr := c.Query("agent_id")
	enabledStr := c.Query("enabled")

	query := db.DB.Model(&model.TCPService{})

	if agentIDStr != "" {
		agentID, _ := strconv.ParseUint(agentIDStr, 10, 32)
		query = query.Where("agent_id = ?", agentID)
	}

	if enabledStr != "" {
		enabled := enabledStr == "true" || enabledStr == "1"
		query = query.Where("enabled = ?", enabled)
	}

	var services []model.TCPService
	if err := query.Order("created_at DESC").Find(&services).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "查询TCP服务失败: " + err.Error(),
		})
		return
	}

	// 获取Agent名称
	type ServiceWithAgent struct {
		model.TCPService
		AgentName string `json:"agent_name"`
	}

	var result []ServiceWithAgent
	for _, svc := range services {
		var agent model.Agent
		db.DB.First(&agent, svc.AgentID)
		result = append(result, ServiceWithAgent{
			TCPService: svc,
			AgentName:  agent.AgentName,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// CreateTCPService 创建TCP服务
func CreateTCPService(c *gin.Context) {
	var req struct {
		ServiceName   string `json:"service_name" binding:"required"`
		AgentID       int64  `json:"agent_id" binding:"required"`
		LocalIP       string `json:"local_ip" binding:"required"`
		LocalPort     int    `json:"local_port" binding:"required"`
		Description   string `json:"description"`
		AccessControl string `json:"access_control"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "请求参数错误: " + err.Error(),
		})
		return
	}

	// 检查服务名称在该Agent下是否已存在
	var count int64
	if err := db.DB.Model(&model.TCPService{}).
		Where("service_name = ? AND agent_id = ?", req.ServiceName, req.AgentID).
		Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "检查服务名称失败: " + err.Error(),
		})
		return
	}
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"error":   "该Agent下已存在同名服务",
		})
		return
	}

	// 检查Agent是否存在
	var agent model.Agent
	if err := db.DB.First(&agent, req.AgentID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Agent不存在",
		})
		return
	}

	// 检查Agent的TCP服务数量是否超限
	var maxPerAgentCfg model.SystemSettings
	maxPerAgent := 50 // 默认值
	if err := db.DB.Where("setting_key = ?", "tcp_service_max_per_agent").First(&maxPerAgentCfg).Error; err == nil {
		maxPerAgent, _ = strconv.Atoi(maxPerAgentCfg.SettingValue)
	}

	var agentServiceCount int64
	db.DB.Model(&model.TCPService{}).Where("agent_id = ?", req.AgentID).Count(&agentServiceCount)
	if agentServiceCount >= int64(maxPerAgent) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   fmt.Sprintf("Agent已达到最大TCP服务数量限制(%d)", maxPerAgent),
		})
		return
	}

	// 自动分配端口
	remotePort, err := allocatePort()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// 设置默认值
	if req.AccessControl == "" {
		req.AccessControl = "public"
	}

	// 创建TCP服务
	service := model.TCPService{
		ServiceName:   req.ServiceName,
		AgentID:       req.AgentID,
		LocalIP:       req.LocalIP,
		LocalPort:     req.LocalPort,
		RemotePort:    remotePort,
		Description:   req.Description,
		Enabled:       false,
		AccessControl: req.AccessControl,
	}

	if err := db.DB.Create(&service).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "创建TCP服务失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    service,
		"message": fmt.Sprintf("TCP实例创建成功，端口%d已分配。请启用服务以开始使用。", remotePort),
	})
}

// allocatePort 自动分配端口
func allocatePort() (int, error) {
	// 获取起始端口
	var portStartCfg model.SystemSettings
	portStart := 9000 // 默认值
	if err := db.DB.Where("setting_key = ?", "tcp_service_port_start").First(&portStartCfg).Error; err == nil {
		portStart, _ = strconv.Atoi(portStartCfg.SettingValue)
	}

	// 查询最大端口（使用 sql.NullInt64 处理 NULL 值）
	var maxPort sql.NullInt64
	if err := db.DB.Model(&model.TCPService{}).Select("MAX(remote_port)").Scan(&maxPort).Error; err != nil {
		return 0, fmt.Errorf("查询最大端口失败: %w", err)
	}

	// 如果没有记录或最大端口为 NULL，返回起始端口
	if !maxPort.Valid || maxPort.Int64 == 0 {
		return portStart, nil
	}

	nextPort := int(maxPort.Int64) + 1
	if nextPort > 65535 {
		return 0, fmt.Errorf("无可用端口，请删除不使用的TCP服务")
	}

	return nextPort, nil
}

// UpdateTCPService 更新TCP服务
func UpdateTCPService(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Description   string `json:"description"`
		AccessControl string `json:"access_control"`
		IPWhitelist   string `json:"ip_whitelist"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "请求参数错误: " + err.Error(),
		})
		return
	}

	updates := map[string]interface{}{
		"description":    req.Description,
		"access_control": req.AccessControl,
		"ip_whitelist":   req.IPWhitelist,
	}

	if err := db.DB.Model(&model.TCPService{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "更新TCP服务失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "TCP实例已更新",
	})
}

// DeleteTCPService 删除TCP服务
func DeleteTCPService(c *gin.Context) {
	id := c.Param("id")

	// 查询服务信息
	var service model.TCPService
	if err := db.DB.First(&service, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "TCP实例不存在",
		})
		return
	}

	// 如果服务已启用，需要先禁用
	if service.Enabled {
		// TODO: 发送gRPC命令给Agent禁用服务
		// 这里暂时只更新数据库状态
	}

	// 删除服务
	if err := db.DB.Delete(&service).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "删除TCP服务失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "TCP实例删除成功",
	})
}

// EnableTCPService 启用TCP服务
func EnableTCPService(c *gin.Context) {
	globalTCPServiceAPI.EnableTCPService(c)
}

// EnableTCPService 启用TCP服务（实例方法）
func (t *TCPServiceAPI) EnableTCPService(c *gin.Context) {
	id := c.Param("id")

	// 查询服务信息
	var service model.TCPService
	if err := db.DB.First(&service, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "TCP实例不存在",
		})
		return
	}

	if service.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "TCP实例已启用",
		})
		return
	}

	// 发送gRPC命令给Agent创建TCP代理（如果Agent在线）
	if t.agentService != nil && t.agentService.IsAgentOnline(int64(service.AgentID)) {
		cmd := &pb.Command{
			CommandId:   fmt.Sprintf("tcp-enable-%d-%d", service.ID, time.Now().Unix()),
			Type:        pb.Command_CREATE_TCP,
			ServiceName: service.ServiceName,
			LocalIp:     service.LocalIP,
			LocalPort:   int32(service.LocalPort),
			RemotePort:  int32(service.RemotePort),
		}

		if err := t.agentService.SendCommand(int64(service.AgentID), cmd); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "发送命令失败: " + err.Error(),
			})
			return
		}
	}
	// 如果Agent离线，只更新数据库状态，Agent上线后会自动同步

	// 更新状态
	if err := db.DB.Model(&service).Update("enabled", true).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "启用TCP服务失败: " + err.Error(),
		})
		return
	}

	// 根据Agent在线状态返回不同消息
	message := "TCP实例已启用"
	if t.agentService != nil && !t.agentService.IsAgentOnline(int64(service.AgentID)) {
		message = "TCP实例已启用（Agent离线，将在Agent上线后自动同步）"
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": message,
	})
}

// DisableTCPService 禁用TCP服务
func DisableTCPService(c *gin.Context) {
	globalTCPServiceAPI.DisableTCPService(c)
}

// DisableTCPService 禁用TCP服务（实例方法）
func (t *TCPServiceAPI) DisableTCPService(c *gin.Context) {
	id := c.Param("id")

	// 查询服务信息
	var service model.TCPService
	if err := db.DB.First(&service, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "TCP实例不存在",
		})
		return
	}

	if !service.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "TCP实例已禁用",
		})
		return
	}

	// 发送gRPC命令给Agent删除TCP代理
	if t.agentService != nil {
		cmd := &pb.Command{
			CommandId:   fmt.Sprintf("tcp-disable-%d-%d", service.ID, time.Now().Unix()),
			Type:        pb.Command_DELETE_TCP,
			ServiceName: service.ServiceName,
		}

		// 如果Agent在线，发送命令；如果离线，只更新数据库状态
		if t.agentService.IsAgentOnline(int64(service.AgentID)) {
			if err := t.agentService.SendCommand(int64(service.AgentID), cmd); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"error":   "发送命令失败: " + err.Error(),
				})
				return
			}
		}
	}

	// 更新状态
	if err := db.DB.Model(&service).Update("enabled", false).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "禁用TCP服务失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "TCP实例已禁用",
	})
}

// GetTCPServiceConfig 获取TCP服务配置
func GetTCPServiceConfig(c *gin.Context) {
	var portStartCfg, maxPerAgentCfg model.SystemSettings
	portStart := "9000"
	maxPerAgent := "50"

	// 获取配置
	if err := db.DB.Where("setting_key = ?", "tcp_service_port_start").First(&portStartCfg).Error; err == nil {
		portStart = portStartCfg.SettingValue
	}
	if err := db.DB.Where("setting_key = ?", "tcp_service_max_per_agent").First(&maxPerAgentCfg).Error; err == nil {
		maxPerAgent = maxPerAgentCfg.SettingValue
	}

	// 获取下一个可用端口
	var maxPort int
	db.DB.Model(&model.TCPService{}).Select("MAX(remote_port)").Scan(&maxPort)

	nextPort := maxPort + 1
	if nextPort <= maxPort || maxPort == 0 {
		portStartInt, _ := strconv.Atoi(portStart)
		if portStartInt == 0 {
			portStartInt = 9000
		}
		nextPort = portStartInt
	}

	// 获取已分配端口总数
	var totalPorts int64
	db.DB.Model(&model.TCPService{}).Count(&totalPorts)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"tcp_service_port_start":    portStart,
			"tcp_service_max_per_agent": maxPerAgent,
			"next_available_port":       nextPort,
			"total_allocated_ports":     totalPorts,
		},
	})
}

// UpdateTCPServiceConfig 更新TCP服务配置
func UpdateTCPServiceConfig(c *gin.Context) {
	var req struct {
		PortStart   int `json:"tcp_service_port_start"`
		MaxPerAgent int `json:"tcp_service_max_per_agent"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "请求参数错误: " + err.Error(),
		})
		return
	}

	// 更新配置
	if req.PortStart > 0 {
		if err := db.DB.Model(&model.SystemSettings{}).
			Where("setting_key = ?", "tcp_service_port_start").
			Update("setting_value", strconv.Itoa(req.PortStart)).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "更新端口起始值失败: " + err.Error(),
			})
			return
		}
	}

	if req.MaxPerAgent > 0 {
		if err := db.DB.Model(&model.SystemSettings{}).
			Where("setting_key = ?", "tcp_service_max_per_agent").
			Update("setting_value", strconv.Itoa(req.MaxPerAgent)).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "更新最大服务数失败: " + err.Error(),
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "TCP实例配置已更新",
	})
}
