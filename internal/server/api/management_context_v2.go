package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

const (
	HeaderManagementScopeType = "X-Management-Scope-Type"
	HeaderManagementScopeID   = "X-Management-Scope-ID"
	HeaderUserSimulationID    = "X-User-Simulation-ID"

	ErrorCodeAuthenticationRequired  = "AUTHENTICATION_REQUIRED"
	ErrorCodeAuthRevisionStale       = "AUTH_REVISION_STALE"
	ErrorCodeUserDisabled            = "USER_DISABLED"
	ErrorCodeManagementScopeConflict = "MANAGEMENT_SCOPE_CONFLICT"
	ErrorCodeManagementObjectMissing = "OBJECT_NOT_FOUND"
	ErrorCodeManagementPermission    = "PERMISSION_DENIED"
	ErrorCodeSimulationInactive      = "USER_SIMULATION_INACTIVE"
	ErrorCodeSimulationForbidden     = "SIMULATION_OPERATION_FORBIDDEN"
)

const contextUnifiedManagementIdentity = "unified_management_identity"

const (
	contextManagementAuthorization  = "management_authorization_context"
	contextAuditActorUserID         = "audit_actor_user_id"
	contextAuditEffectiveUserID     = "audit_effective_user_id"
	contextAuditSimulationSessionID = "audit_simulation_session_id"
	contextAuditScopeType           = "audit_scope_type"
	contextAuditScopeID             = "audit_scope_id"
)

type ManagementContextAPI struct{}

func NewManagementContextAPI() *ManagementContextAPI { return &ManagementContextAPI{} }

type managementContextResponse struct {
	ScopeType          model.ManagementScopeType `json:"scope_type"`
	ScopeID            string                    `json:"scope_id,omitempty"`
	ScopeKey           string                    `json:"scope_key,omitempty"`
	ScopeName          string                    `json:"scope_name,omitempty"`
	ScopeStatus        string                    `json:"scope_status,omitempty"`
	Role               string                    `json:"role"`
	Permissions        []string                  `json:"permissions"`
	PermissionRevision int64                     `json:"permission_revision"`
	ExpiresAt          *time.Time                `json:"expires_at,omitempty"`
}

func UnifiedManagementIdentityMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		adminID := getAdminIDFromContext(c)
		if adminID <= 0 {
			codedError(c, http.StatusUnauthorized, ErrorCodeAuthenticationRequired, "统一管理身份未认证")
			c.Abort()
			return
		}
		identity, err := service.ResolveLegacyAdminIdentity(
			db.DB.WithContext(c.Request.Context()),
			adminID,
			uint64ContextValue(c, "user_id"),
			int64ContextNumber(c, "auth_revision"),
			int64ContextNumber(c, "credential_revision"),
		)
		if err != nil {
			switch {
			case errors.Is(err, service.ErrManagementIdentityNotMapped):
				codedError(c, http.StatusUnauthorized, ErrorCodeAuthenticationRequired, "管理凭证尚未映射到统一 User")
			case errors.Is(err, service.ErrManagementIdentityStale):
				codedError(c, http.StatusUnauthorized, ErrorCodeAuthRevisionStale, "统一身份版本已失效，请重新登录")
			case errors.Is(err, service.ErrManagementUserDisabled):
				codedError(c, http.StatusForbidden, ErrorCodeUserDisabled, "统一 User 已停用")
			default:
				codedError(c, http.StatusInternalServerError, "MANAGEMENT_IDENTITY_QUERY_FAILED", "读取统一身份失败")
			}
			c.Abort()
			return
		}
		c.Set(contextUnifiedManagementIdentity, identity)
		c.Set(contextAuditActorUserID, identity.UserID)
		c.Set(contextAuditEffectiveUserID, identity.UserID)
		c.Next()
	}
}

func RequireManagementPermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, ok := currentUnifiedManagementIdentity(c)
		if !ok {
			c.Abort()
			return
		}
		scopeType, scopeID, ok := managementScopeFromHeaders(c)
		if !ok {
			c.Abort()
			return
		}
		context, err := resolveManagementRequestContext(c, identity, scopeType, scopeID, time.Now())
		if err == nil {
			err = service.AuthorizeManagementPermission(context, permission)
		}
		if context != nil {
			setManagementAuthorizationContext(c, context)
			c.Set("audit_required_permission", permission)
		}
		if context != nil && context.SimulationSessionID != "" {
			result := "allowed"
			if err != nil {
				result = "denied"
			}
			if auditErr := recordAuditLogStrict(c.Request.Context(), c, "authorize_user_simulation_request", "route", c.FullPath(), c.FullPath(), gin.H{
				"method": c.Request.Method, "route": c.FullPath(), "permission": permission, "result": result,
			}); auditErr != nil {
				codedError(c, http.StatusServiceUnavailable, ErrorCodeAuditWriteFailed, "用户模拟审计写入失败，请求已拒绝")
				c.Abort()
				return
			}
		}
		if err != nil {
			writeManagementRequestError(c, err)
			c.Abort()
			return
		}
		c.Next()
	}
}

func (a *ManagementContextAPI) List(c *gin.Context) {
	identity, ok := currentUnifiedManagementIdentity(c)
	if !ok {
		return
	}
	if hasAnyManagementScopeHeader(c) || hasUserSimulationHeader(c) || strings.TrimSpace(c.GetHeader("X-Tenant-ID")) != "" {
		codedError(c, http.StatusBadRequest, ErrorCodeManagementScopeConflict, "上下文目录请求不能携带活动 Scope")
		return
	}
	contexts, err := service.ListManagementContexts(db.DB.WithContext(c.Request.Context()), identity.UserID, time.Now())
	if err != nil {
		writeManagementContextError(c, err)
		return
	}
	items := make([]managementContextResponse, 0, len(contexts))
	for _, context := range contexts {
		items = append(items, managementContextView(&context))
	}
	c.JSON(http.StatusOK, NewSuccessResponse(items))
}

func (a *ManagementContextAPI) Current(c *gin.Context) {
	identity, ok := currentUnifiedManagementIdentity(c)
	if !ok {
		return
	}
	scopeType, scopeID, ok := managementScopeFromHeaders(c)
	if !ok {
		return
	}
	context, err := resolveManagementRequestContext(c, identity, scopeType, scopeID, time.Now())
	if err != nil {
		if context != nil && context.SimulationSessionID != "" {
			setManagementAuthorizationContext(c, context)
			if auditErr := recordAuditLogStrict(c.Request.Context(), c, "resolve_user_simulation_context", "route", c.FullPath(), c.FullPath(), gin.H{
				"method": c.Request.Method, "route": c.FullPath(), "result": "denied",
			}); auditErr != nil {
				codedError(c, http.StatusServiceUnavailable, ErrorCodeAuditWriteFailed, "用户模拟审计写入失败，请求已拒绝")
				return
			}
		}
		writeManagementRequestError(c, err)
		return
	}
	setManagementAuthorizationContext(c, context)
	if context.SimulationSessionID != "" {
		if err := recordAuditLogStrict(c.Request.Context(), c, "resolve_user_simulation_context", "route", c.FullPath(), c.FullPath(), gin.H{
			"method": c.Request.Method, "route": c.FullPath(), "result": "allowed",
		}); err != nil {
			codedError(c, http.StatusServiceUnavailable, ErrorCodeAuditWriteFailed, "用户模拟审计写入失败，请求已拒绝")
			return
		}
	}
	c.JSON(http.StatusOK, NewSuccessResponse(managementContextView(context)))
}

func resolveManagementRequestContext(c *gin.Context, identity *service.UnifiedManagementIdentity, scopeType model.ManagementScopeType, scopeID string, at time.Time) (*service.ManagementAuthorizationContext, error) {
	values := c.Request.Header.Values(HeaderUserSimulationID)
	if len(values) == 0 {
		return service.ResolveManagementContext(db.DB.WithContext(c.Request.Context()), identity.UserID, scopeType, scopeID, at, false)
	}
	sessionID := singleSafeHeader(c, HeaderUserSimulationID, 36)
	if len(values) != 1 || sessionID == "" {
		return nil, service.ErrResourceIdentityInvalid
	}
	session, context, err := service.ResolveUserSimulationSession(db.DB.WithContext(c.Request.Context()), sessionID, identity.UserID, at)
	if err != nil {
		if session != nil && session.ActorUserID == identity.UserID {
			context = &service.ManagementAuthorizationContext{
				ActorUserID: identity.UserID, EffectiveUserID: session.EffectiveUserID,
				ScopeType: model.ManagementScopeType(session.ScopeType), ScopeID: session.ScopeID,
				PermissionRevision: session.PermissionRevision, SimulationSessionID: session.ID,
			}
		}
		return context, err
	}
	if context.ScopeType != scopeType || context.ScopeID != scopeID {
		return context, service.ErrManagementScopeInvalid
	}
	return context, nil
}

func ForbidUserSimulation() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !hasUserSimulationHeader(c) {
			c.Next()
			return
		}
		codedError(c, http.StatusForbidden, ErrorCodeSimulationForbidden, "用户模拟状态不能执行此操作")
		c.Abort()
	}
}

func currentManagementAuthorization(c *gin.Context) (*service.ManagementAuthorizationContext, bool) {
	value, exists := c.Get(contextManagementAuthorization)
	context, ok := value.(*service.ManagementAuthorizationContext)
	return context, exists && ok && context != nil
}

func setManagementAuthorizationContext(c *gin.Context, context *service.ManagementAuthorizationContext) {
	c.Set(contextManagementAuthorization, context)
	c.Set(contextAuditActorUserID, context.ActorUserID)
	c.Set(contextAuditEffectiveUserID, context.EffectiveUserID)
	c.Set(contextAuditSimulationSessionID, context.SimulationSessionID)
	c.Set(contextAuditScopeType, string(context.ScopeType))
	c.Set(contextAuditScopeID, context.ScopeID)
	c.Set("audit_required_permission", "")
	c.Set("audit_permission_revision", context.PermissionRevision)
	if context.ScopeType == model.ManagementScopeTenant {
		c.Set("audit_tenant_id", context.ScopeID)
		c.Set("audit_tenant_role", context.Role)
	}
}

func currentUnifiedManagementIdentity(c *gin.Context) (*service.UnifiedManagementIdentity, bool) {
	value, exists := c.Get(contextUnifiedManagementIdentity)
	identity, ok := value.(*service.UnifiedManagementIdentity)
	if !exists || !ok || identity == nil {
		codedError(c, http.StatusUnauthorized, ErrorCodeAuthenticationRequired, "统一管理身份未认证")
		return nil, false
	}
	return identity, true
}

func managementScopeFromHeaders(c *gin.Context) (model.ManagementScopeType, string, bool) {
	if strings.TrimSpace(c.GetHeader("X-Tenant-ID")) != "" {
		codedError(c, http.StatusBadRequest, ErrorCodeManagementScopeConflict, "新旧上下文 Header 不能同时使用")
		return "", "", false
	}
	rawType := singleSafeHeader(c, HeaderManagementScopeType, 20)
	rawID := singleSafeHeader(c, HeaderManagementScopeID, 64)
	scopeType := model.ManagementScopeType(rawType)
	switch scopeType {
	case model.ManagementScopePlatform:
		if rawID != "" {
			codedError(c, http.StatusBadRequest, ErrorCodeManagementScopeConflict, "platform 上下文不能携带 Scope ID")
			return "", "", false
		}
	case model.ManagementScopeProvider, model.ManagementScopeTenant:
		if rawID == "" {
			codedError(c, http.StatusBadRequest, ErrorCodeManagementScopeConflict, "provider/tenant 上下文必须携带 Scope ID")
			return "", "", false
		}
	default:
		codedError(c, http.StatusBadRequest, ErrorCodeManagementScopeConflict, "管理 Scope 类型无效")
		return "", "", false
	}
	return scopeType, rawID, true
}

func hasAnyManagementScopeHeader(c *gin.Context) bool {
	return len(c.Request.Header.Values(HeaderManagementScopeType)) > 0 || len(c.Request.Header.Values(HeaderManagementScopeID)) > 0
}

func hasUserSimulationHeader(c *gin.Context) bool {
	return len(c.Request.Header.Values(HeaderUserSimulationID)) > 0
}

func managementContextView(context *service.ManagementAuthorizationContext) managementContextResponse {
	permissions := append([]string(nil), context.Permissions...)
	if permissions == nil {
		permissions = []string{}
	}
	return managementContextResponse{
		ScopeType: context.ScopeType, ScopeID: context.ScopeID, ScopeKey: context.ScopeKey,
		ScopeName: context.ScopeName, ScopeStatus: context.ScopeStatus, Role: context.Role,
		Permissions: permissions, PermissionRevision: context.PermissionRevision, ExpiresAt: context.ExpiresAt,
	}
}

func writeManagementContextError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrManagementUserDisabled):
		codedError(c, http.StatusForbidden, ErrorCodeUserDisabled, "统一 User 已停用")
	case errors.Is(err, service.ErrManagementScopeInvalid), errors.Is(err, service.ErrManagementMembershipMissing):
		// Missing and unauthorized scopes intentionally share one response so IDs
		// cannot be used to enumerate another Provider or Tenant.
		codedError(c, http.StatusNotFound, ErrorCodeManagementObjectMissing, "当前工作域内对象不存在或不可见")
	case errors.Is(err, service.ErrManagementPermissionDenied):
		codedError(c, http.StatusForbidden, ErrorCodeManagementPermission, "当前角色不能执行此操作")
	default:
		codedError(c, http.StatusInternalServerError, "MANAGEMENT_CONTEXT_QUERY_FAILED", "读取管理上下文失败")
	}
}

func writeManagementRequestError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrResourceIdentityInvalid):
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "用户模拟 Header 无效")
	case errors.Is(err, service.ErrUserSimulationInactive):
		codedError(c, http.StatusConflict, ErrorCodeSimulationInactive, "用户模拟会话已结束或过期")
	case errors.Is(err, service.ErrUserSimulationNotAllowed):
		codedError(c, http.StatusNotFound, ErrorCodeManagementObjectMissing, "用户模拟会话不存在或不可用")
	default:
		writeManagementContextError(c, err)
	}
}

func uint64ContextValue(c *gin.Context, key string) uint64 {
	value, exists := c.Get(key)
	if !exists {
		return 0
	}
	switch typed := value.(type) {
	case uint64:
		return typed
	case int64:
		if typed > 0 {
			return uint64(typed)
		}
	case float64:
		if typed > 0 {
			return uint64(typed)
		}
	}
	return 0
}

func int64ContextNumber(c *gin.Context, key string) int64 {
	value, exists := c.Get(key)
	if !exists {
		return 0
	}
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	}
	return 0
}
