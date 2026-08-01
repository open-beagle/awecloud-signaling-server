package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// ContainerSessionAPI exposes the control-plane session view. The future
// Agent Broker will create active sessions and observe revoked sessions.
type ContainerSessionAPI struct{}

func NewContainerSessionAPI() *ContainerSessionAPI { return &ContainerSessionAPI{} }

type containerSessionListItem struct {
	model.ContainerSession
	ResourceName string `json:"resource_name,omitempty"`
	UserName     string `json:"user_name,omitempty"`
}

type containerSessionDetail struct {
	Session  model.ContainerSession `json:"session"`
	Resource *model.Resource        `json:"resource,omitempty"`
	Tenant   *model.Tenant          `json:"tenant,omitempty"`
}

func (a *ContainerSessionAPI) List(c *gin.Context) {
	ctx := c.Request.Context()
	page, size := pageParams(c)
	query := db.DB.WithContext(ctx).Model(&model.ContainerSession{})
	tenantIDs, unrestricted, ok := tenantReadScope(c, PermissionTenantSessionsRead)
	if !ok {
		return
	}
	if !unrestricted {
		query = query.Where("tenant_id IN ?", tenantIDs)
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	if tenantID := strings.TrimSpace(c.Query("tenant_id")); tenantID != "" {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if resourceID := strings.TrimSpace(c.Query("resource_id")); resourceID != "" {
		query = query.Where("resource_id = ?", resourceID)
	}
	if userID := strings.TrimSpace(c.Query("user_id")); userID != "" {
		if _, err := strconv.ParseUint(userID, 10, 64); err != nil {
			c.JSON(http.StatusBadRequest, NewErrorResponse("用户 ID 无效"))
			return
		}
		query = query.Where("user_id = ?", userID)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询 ContainerSSH 会话失败"))
		return
	}
	var sessions []model.ContainerSession
	if err := query.Order("started_at DESC").Offset((page - 1) * size).Limit(size).Find(&sessions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询 ContainerSSH 会话失败"))
		return
	}
	items := make([]containerSessionListItem, 0, len(sessions))
	for _, session := range sessions {
		item := containerSessionListItem{ContainerSession: session}
		var resource model.Resource
		if err := db.DB.WithContext(ctx).First(&resource, "id = ? AND tenant_id = ?", session.ResourceID, session.TenantID).Error; err == nil {
			item.ResourceName = resource.DisplayName
		}
		var user model.User
		if err := db.DB.WithContext(ctx).
			Joins("JOIN tenant_membership ON tenant_membership.user_id = user.id AND tenant_membership.tenant_id = ?", session.TenantID).
			First(&user, "user.id = ?", session.UserID).Error; err == nil {
			item.UserName = user.Alias
			if item.UserName == "" {
				item.UserName = user.Name
			}
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}

func (a *ContainerSessionAPI) Get(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID, unrestricted, ok := tenantObjectScope(c, PermissionTenantSessionsRead)
	if !ok {
		return
	}
	var session model.ContainerSession
	query := db.DB.WithContext(ctx).Where("id = ?", c.Param("id"))
	if !unrestricted {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if err := query.First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			codedError(c, http.StatusNotFound, ErrorCodeTenantObjectNotFound, "当前租户范围内对象不存在")
			return
		}
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询 ContainerSSH 会话失败"))
		return
	}
	detail := containerSessionDetail{Session: session}
	var resource model.Resource
	if err := db.DB.WithContext(ctx).First(&resource, "id = ? AND tenant_id = ?", session.ResourceID, session.TenantID).Error; err == nil {
		detail.Resource = &resource
	}
	var tenant model.Tenant
	if err := db.DB.WithContext(ctx).First(&tenant, "id = ?", session.TenantID).Error; err == nil {
		detail.Tenant = &tenant
	}
	c.JSON(http.StatusOK, NewSuccessResponse(detail))
}

func (a *ContainerSessionAPI) Revoke(c *gin.Context) {
	a.close(c, "revoke_container_session", "管理员撤销会话")
}

func (a *ContainerSessionAPI) ForceDisconnect(c *gin.Context) {
	a.close(c, "force_disconnect_container_session", "管理员强制断开会话")
}

func (a *ContainerSessionAPI) close(c *gin.Context, action, defaultReason string) {
	ctx := c.Request.Context()
	tenantID, unrestricted, ok := tenantObjectScope(c, PermissionTenantSessionsDisconnect)
	if !ok {
		return
	}
	var session model.ContainerSession
	query := db.DB.WithContext(ctx).Where("id = ?", c.Param("id"))
	if !unrestricted {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if err := query.First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			codedError(c, http.StatusNotFound, ErrorCodeTenantObjectNotFound, "当前租户范围内对象不存在")
			return
		}
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询 ContainerSSH 会话失败"))
		return
	}
	if session.Status != model.ContainerSessionActive {
		c.JSON(http.StatusConflict, NewErrorResponse("会话已结束，不能重复关闭"))
		return
	}
	var agentNode model.Node
	if session.AgentNodeID == 0 || db.DB.WithContext(ctx).First(&agentNode, session.AgentNodeID).Error != nil || agentNode.ContainerSSHProtocol != "v1" {
		c.JSON(http.StatusConflict, NewErrorResponse("当前 Agent 不支持 ContainerSSH 远程断开"))
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, NewErrorResponse("关闭原因格式无效"))
			return
		}
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		reason = defaultReason
	}
	now := time.Now()
	session.Status = model.ContainerSessionRevoked
	session.EndedAt = &now
	session.Result = "revoked"
	session.CloseReason = reason
	if err := db.DB.WithContext(ctx).Save(&session).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("关闭 ContainerSSH 会话失败"))
		return
	}
	recordAuditLog(ctx, c, action, "container_session", session.ID, session.ResourceID, map[string]interface{}{
		"resource_id": session.ResourceID,
		"user_id":     session.UserID,
		"reason":      reason,
	})
	c.JSON(http.StatusOK, NewSuccessResponse(session))
}
