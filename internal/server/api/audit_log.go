package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/auth"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type AuditLogAPI struct{}

func NewAuditLogAPI() *AuditLogAPI {
	return &AuditLogAPI{}
}

// RecordConnectionRequest 记录连接审计日志请求
type RecordConnectionRequest struct {
	STCPInstanceID    int64           `json:"stcp_instance_id" binding:"required"`
	Action            string          `json:"action" binding:"required,oneof=connect disconnect"`
	LocalPort         int             `json:"local_port"`
	DeviceFingerprint string          `json:"device_fingerprint"`
	DeviceInfo        auth.DeviceInfo `json:"device_info"`
	Success           bool            `json:"success"`
	ErrorMessage      string          `json:"error_message"`
	ServerAddress     string          `json:"server_address"` // Desktop连接的Server地址
}

// QueryAuditLogsResponse 查询审计日志响应
type QueryAuditLogsResponse struct {
	Success    bool           `json:"success"`
	Logs       []AuditLogInfo `json:"logs,omitempty"`
	Total      int64          `json:"total"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	TotalPages int            `json:"total_pages"`
	Message    string         `json:"message,omitempty"`
}

// AuditLogInfo 审计日志信息
type AuditLogInfo struct {
	ID                int64           `json:"id"`
	ClientID          int64           `json:"client_id"`
	ClientName        string          `json:"client_name"`
	STCPInstanceID    int64           `json:"stcp_instance_id"`
	STCPInstanceName  string          `json:"stcp_instance_name"`
	ServerAddress     string          `json:"server_address"`
	Action            string          `json:"action"`
	LocalPort         int             `json:"local_port"`
	DeviceInfo        auth.DeviceInfo `json:"device_info"`
	DeviceFingerprint string          `json:"device_fingerprint"`
	IPAddress         string          `json:"ip_address"`
	Success           bool            `json:"success"`
	ErrorMessage      string          `json:"error_message"`
	CreatedAt         string          `json:"created_at"`
}

// RecordConnection 记录连接审计日志
func (a *AuditLogAPI) RecordConnection(c *gin.Context) {
	var req RecordConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请求参数错误",
		})
		return
	}

	// 从JWT获取client_id
	clientID, exists := c.Get("client_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "未认证",
		})
		return
	}

	// 获取客户端IP地址
	ipAddress := c.ClientIP()

	// 将设备信息转换为JSON
	deviceInfoJSON, err := auth.DeviceInfoToJSON(req.DeviceInfo)
	if err != nil {
		logger.Warnf("序列化设备信息失败: %v", err)
		deviceInfoJSON = "{}"
	}

	// 创建审计日志记录
	auditLog := &model.ConnectionAuditLog{
		ClientPKID:        int64(clientID.(float64)),
		STCPInstancePKID:  req.STCPInstanceID,
		Action:            req.Action,
		LocalPort:         req.LocalPort,
		DeviceFingerprint: req.DeviceFingerprint,
		DeviceInfo:        deviceInfoJSON,
		IPAddress:         ipAddress,
		ServerAddress:     req.ServerAddress,
		Success:           req.Success,
		ErrorMessage:      req.ErrorMessage,
	}

	if err := db.DB.Create(auditLog).Error; err != nil {
		logger.Warnf("创建审计日志失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "记录审计日志失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"audit_id": auditLog.ID,
	})
}

// QueryAuditLogs 查询审计日志（管理员）
func (a *AuditLogAPI) QueryAuditLogs(c *gin.Context) {
	// 验证管理员权限（从JWT中获取）
	_, exists := c.Get("admin_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, QueryAuditLogsResponse{
			Success: false,
			Message: "需要管理员权限",
		})
		return
	}

	// 解析查询参数
	clientIDStr := c.Query("client_id")
	instanceIDStr := c.Query("stcp_instance_id")
	action := c.Query("action")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	pageStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("page_size", "50")

	page, _ := strconv.Atoi(pageStr)
	pageSize, _ := strconv.Atoi(pageSizeStr)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}

	// 构建查询
	query := db.DB.Model(&model.ConnectionAuditLog{})

	// 过滤条件
	if clientIDStr != "" {
		clientID, _ := strconv.ParseInt(clientIDStr, 10, 64)
		query = query.Where("client_id = ?", clientID)
	}
	if instanceIDStr != "" {
		instanceID, _ := strconv.ParseInt(instanceIDStr, 10, 64)
		query = query.Where("stcp_instance_id = ?", instanceID)
	}
	if action != "" {
		query = query.Where("action = ?", action)
	}
	if startDate != "" {
		query = query.Where("created_at >= ?", startDate)
	}
	if endDate != "" {
		query = query.Where("created_at <= ?", endDate)
	}

	// 查询总数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		logger.Warnf("查询审计日志总数失败: %v", err)
		c.JSON(http.StatusInternalServerError, QueryAuditLogsResponse{
			Success: false,
			Message: "查询审计日志失败",
		})
		return
	}

	// 分页查询
	var logs []model.ConnectionAuditLog
	offset := (page - 1) * pageSize
	if err := query.Preload("Client").
		Order("created_at DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&logs).Error; err != nil {
		logger.Warnf("查询审计日志失败: %v", err)
		c.JSON(http.StatusInternalServerError, QueryAuditLogsResponse{
			Success: false,
			Message: "查询审计日志失败",
		})
		return
	}

	// 构建响应
	logInfos := make([]AuditLogInfo, 0, len(logs))
	for _, auditLog := range logs {
		deviceInfo, _ := auth.DeviceInfoFromJSON(auditLog.DeviceInfo)

		clientName := ""
		if auditLog.Client.ClientID != "" {
			clientName = auditLog.Client.ClientID
		} else {
			logger.Warnf("Warning: Client not loaded for audit log %d, client_pk_id=%d", auditLog.ID, auditLog.ClientPKID)
		}

		// 实例名称（废弃 STCPInstance，使用 ID）
		instanceName := fmt.Sprintf("instance-%d", auditLog.STCPInstancePKID)

		logInfos = append(logInfos, AuditLogInfo{
			ID:                auditLog.ID,
			ClientID:          auditLog.ClientPKID,
			ClientName:        clientName,
			STCPInstanceID:    auditLog.STCPInstancePKID,
			STCPInstanceName:  instanceName,
			ServerAddress:     auditLog.ServerAddress,
			Action:            auditLog.Action,
			LocalPort:         auditLog.LocalPort,
			DeviceInfo:        deviceInfo,
			DeviceFingerprint: auditLog.DeviceFingerprint,
			IPAddress:         auditLog.IPAddress,
			Success:           auditLog.Success,
			ErrorMessage:      auditLog.ErrorMessage,
			CreatedAt:         auditLog.CreatedAt.Format(time.RFC3339),
		})
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	c.JSON(http.StatusOK, QueryAuditLogsResponse{
		Success:    true,
		Logs:       logInfos,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

// ExportAuditLogs 导出审计日志（管理员）
func (a *AuditLogAPI) ExportAuditLogs(c *gin.Context) {
	// 验证管理员权限
	_, exists := c.Get("admin_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "需要管理员权限",
		})
		return
	}

	// 解析查询参数（与QueryAuditLogs相同的过滤条件）
	clientIDStr := c.Query("client_id")
	instanceIDStr := c.Query("stcp_instance_id")
	action := c.Query("action")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	format := c.DefaultQuery("format", "csv")

	// 构建查询
	query := db.DB.Model(&model.ConnectionAuditLog{})

	// 过滤条件
	if clientIDStr != "" {
		clientID, _ := strconv.ParseInt(clientIDStr, 10, 64)
		query = query.Where("client_id = ?", clientID)
	}
	if instanceIDStr != "" {
		instanceID, _ := strconv.ParseInt(instanceIDStr, 10, 64)
		query = query.Where("stcp_instance_id = ?", instanceID)
	}
	if action != "" {
		query = query.Where("action = ?", action)
	}
	if startDate != "" {
		query = query.Where("created_at >= ?", startDate)
	}
	if endDate != "" {
		query = query.Where("created_at <= ?", endDate)
	}

	// 查询所有记录（不分页）
	var logs []model.ConnectionAuditLog
	if err := query.Preload("Client").
		Order("created_at DESC").
		Find(&logs).Error; err != nil {
		logger.Warnf("查询审计日志失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "查询审计日志失败",
		})
		return
	}

	// 导出为CSV
	if format == "csv" {
		c.Header("Content-Type", "text/csv")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=audit_logs_%s.csv",
			time.Now().Format("20060102_150405")))

		writer := csv.NewWriter(c.Writer)
		defer writer.Flush()

		// 写入表头
		writer.Write([]string{
			"ID", "Client ID", "Client Name", "STCP Instance ID", "STCP Instance Name",
			"Action", "Local Port", "IP Address", "Success", "Error Message",
			"Device OS", "Device Hostname", "Created At",
		})

		// 写入数据
		for _, logEntry := range logs {
			deviceInfo, _ := auth.DeviceInfoFromJSON(logEntry.DeviceInfo)

			clientName := ""
			if logEntry.Client.ClientID != "" {
				clientName = logEntry.Client.ClientID
			}

			// 实例名称（废弃 STCPInstance，使用 ID）
			instanceName := fmt.Sprintf("instance-%d", logEntry.STCPInstancePKID)

			writer.Write([]string{
				strconv.FormatInt(logEntry.ID, 10),
				strconv.FormatInt(logEntry.ClientPKID, 10),
				clientName,
				strconv.FormatInt(logEntry.STCPInstancePKID, 10),
				instanceName,
				logEntry.Action,
				strconv.Itoa(logEntry.LocalPort),
				logEntry.IPAddress,
				strconv.FormatBool(logEntry.Success),
				logEntry.ErrorMessage,
				deviceInfo.OS,
				deviceInfo.Hostname,
				logEntry.CreatedAt.Format(time.RFC3339),
			})
		}
		return
	}

	// 不支持的格式
	c.JSON(http.StatusBadRequest, gin.H{
		"success": false,
		"message": "不支持的导出格式",
	})
}
