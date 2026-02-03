package api

import (
	"context"
	"encoding/json"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// Response 统一响应格式
type Response struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
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
	userID := getAdminIDFromContext(c)

	var detailStr string
	if detail != nil {
		if data, err := json.Marshal(detail); err == nil {
			detailStr = string(data)
		}
	}

	log := &model.AuditLog{
		UserID:     userID,
		UserType:   "admin",
		ActionType: actionType,
		TargetType: targetType,
		TargetID:   targetID,
		TargetName: targetName,
		Detail:     detailStr,
	}

	db.DB.WithContext(ctx).Create(log)
}
