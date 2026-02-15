package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// OperationAuditAPI 操作审计 API
type OperationAuditAPI struct{}

// NewOperationAuditAPI 创建 OperationAuditAPI
func NewOperationAuditAPI() *OperationAuditAPI {
	return &OperationAuditAPI{}
}

// OperationAuditItem 操作审计列表项
type OperationAuditItem struct {
	ID            int64     `json:"id"`
	AgentName     string    `json:"agent_name"`
	ClientName    string    `json:"client_name"`
	EndpointName  string    `json:"endpoint_name"`
	OperationType string    `json:"operation_type"`
	Target        string    `json:"target"`
	Detail        string    `json:"detail"`
	StartedAt     time.Time `json:"started_at"`
	EndedAt       time.Time `json:"ended_at"`
	DurationMs    int64     `json:"duration_ms"`
	CreatedAt     time.Time `json:"created_at"`
}

// ListOperationAudit 查询操作审计日志
func (a *OperationAuditAPI) ListOperationAudit(c *gin.Context) {
	ctx := c.Request.Context()
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	operationType := c.Query("operation_type")
	agentUserID, _ := strconv.ParseUint(c.Query("agent_user_id"), 10, 64)
	endpointName := c.Query("endpoint_name")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	query := db.DB.WithContext(ctx).Model(&model.OperationAuditLog{})

	// 按操作类型筛选
	if operationType != "" {
		query = query.Where("operation_type = ?", operationType)
	}

	// 按 Agent 筛选
	if agentUserID > 0 {
		query = query.Where("agent_user_id = ?", agentUserID)
	}

	// 按 Endpoint 名称筛选
	if endpointName != "" {
		query = query.Where("endpoint_name LIKE ?", "%"+endpointName+"%")
	}

	// 按日期范围筛选
	if startDate != "" {
		t, err := time.Parse("2006-01-02", startDate)
		if err == nil {
			query = query.Where("started_at >= ?", t)
		}
	}
	if endDate != "" {
		t, err := time.Parse("2006-01-02", endDate)
		if err == nil {
			query = query.Where("started_at < ?", t.AddDate(0, 0, 1))
		}
	}

	var total int64
	query.Count(&total)

	var logs []model.OperationAuditLog
	offset := (page - 1) * size
	if err := query.Order("started_at DESC").Offset(offset).Limit(size).Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	result := make([]OperationAuditItem, len(logs))
	for i, log := range logs {
		result[i] = OperationAuditItem{
			ID:            log.ID,
			AgentName:     log.AgentName,
			ClientName:    log.ClientName,
			EndpointName:  log.EndpointName,
			OperationType: log.OperationType,
			Target:        log.Target,
			Detail:        log.Detail,
			StartedAt:     log.StartedAt,
			EndedAt:       log.EndedAt,
			DurationMs:    log.DurationMs,
			CreatedAt:     log.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(result, total, page, size))
}

// GetOperationTypes 获取操作审计类型列表
func (a *OperationAuditAPI) GetOperationTypes(c *gin.Context) {
	types := []map[string]string{
		{"value": model.OperationTypeSSHSession, "label": "SSH 会话"},
		{"value": model.OperationTypeK8SAPIRequest, "label": "K8S API 请求"},
		{"value": model.OperationTypeK8SServiceConnect, "label": "K8S Service 连接"},
	}

	c.JSON(http.StatusOK, NewSuccessResponse(types))
}
