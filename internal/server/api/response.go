package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// Response 统一响应格式
type Response struct {
	Success   bool        `json:"success"`
	Code      string      `json:"code,omitempty"`
	Message   string      `json:"message,omitempty"`
	RequestID string      `json:"request_id,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
}

// PagedResponse 分页响应格式
type PagedResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Total   int64       `json:"total"`
	Page    int         `json:"page"`
	Size    int         `json:"size"`
}

// NewSuccessResponse 创建成功响应
func NewSuccessResponse(data interface{}) Response {
	return Response{
		Success: true,
		Data:    data,
	}
}

// NewSuccessMessageResponse 创建带消息的成功响应
func NewSuccessMessageResponse(message string, data interface{}) Response {
	return Response{
		Success: true,
		Message: message,
		Data:    data,
	}
}

// NewErrorResponse 创建错误响应
func NewErrorResponse(message string) Response {
	return Response{
		Success: false,
		Message: message,
	}
}

func NewCodedErrorResponse(code, message, requestID string) Response {
	return Response{
		Success:   false,
		Code:      code,
		Message:   message,
		RequestID: requestID,
	}
}

// NewPagedResponse 创建分页响应
func NewPagedResponse(data interface{}, total int64, page, size int) PagedResponse {
	return PagedResponse{
		Success: true,
		Data:    data,
		Total:   total,
		Page:    page,
		Size:    size,
	}
}

// recordAuditLog 记录审计日志
func recordAuditLog(ctx context.Context, c *gin.Context, actionType, targetType, targetID, targetName string, detail interface{}) {
	_ = recordAuditLogStrict(ctx, c, actionType, targetType, targetID, targetName, detail)
}

func recordAuditLogStrict(ctx context.Context, c *gin.Context, actionType, targetType, targetID, targetName string, detail interface{}) error {
	if db.DB == nil {
		return fmt.Errorf("audit database is not initialized")
	}
	userID := getAdminIDFromContext(c)
	actorUsername := ""
	platformRole := ""
	if userID > 0 {
		var admin model.Admin
		if err := db.DB.WithContext(ctx).Select("username", "role").First(&admin, userID).Error; err == nil {
			actorUsername = admin.Username
			platformRole = string(model.NormalizePlatformRole(admin.Role))
		}
	}
	tenantID, _ := c.Get("audit_tenant_id")
	tenantRole, _ := c.Get("audit_tenant_role")
	requiredPermission, _ := c.Get("audit_required_permission")
	permissionRevision, _ := c.Get("audit_permission_revision")

	var detailStr string
	if detail != nil {
		data, err := json.Marshal(detail)
		if err != nil {
			return fmt.Errorf("marshal audit detail: %w", err)
		}
		detailStr = string(data)
	}

	log := &model.AuditLog{
		UserID:             userID,
		UserType:           "admin",
		ActorAdminID:       userID,
		ActorUsername:      actorUsername,
		PlatformRole:       platformRole,
		TenantID:           stringContextValue(tenantID),
		TenantRole:         stringContextValue(tenantRole),
		RequiredPermission: stringContextValue(requiredPermission),
		PermissionRevision: int64ContextValue(permissionRevision),
		RequestID:          requestID(c),
		SourceIP:           c.ClientIP(),
		UserAgent:          c.Request.UserAgent(),
		ActionType:         actionType,
		TargetType:         targetType,
		TargetID:           targetID,
		TargetName:         targetName,
		Detail:             detailStr,
	}

	return db.DB.WithContext(ctx).Create(log).Error
}

func requestID(c *gin.Context) string {
	if value, exists := c.Get(contextRequestID); exists {
		if result, ok := value.(string); ok && result != "" {
			return result
		}
	}
	value := singleSafeHeader(c, HeaderRequestID, 64)
	if value == "" {
		value = uuid.NewString()
	}
	c.Set(contextRequestID, value)
	c.Header(HeaderRequestID, value)
	return value
}

func stringContextValue(value interface{}) string {
	result, _ := value.(string)
	return result
}

func int64ContextValue(value interface{}) int64 {
	switch result := value.(type) {
	case int64:
		return result
	case int:
		return int64(result)
	default:
		return 0
	}
}
