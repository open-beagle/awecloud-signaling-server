package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

const (
	tenantCandidatePublishRoute = "/api/v1/management/tenants/:tenant_id/resource-candidates/:resource_id/publish"
	tenantGrantCreateRoute      = "/api/v1/management/tenants/:tenant_id/grants"
	tenantGrantSuspendRoute     = "/api/v1/management/tenants/:tenant_id/grants/:id/suspend"
	tenantGrantResumeRoute      = "/api/v1/management/tenants/:tenant_id/grants/:id/resume"
	tenantGrantRevokeRoute      = "/api/v1/management/tenants/:tenant_id/grants/:id/revoke"
	tenantSessionCreateRoute    = "/api/v1/management/tenants/:tenant_id/sessions"
	tenantSessionTerminateRoute = "/api/v1/management/tenants/:tenant_id/sessions/:id/terminate"
)

var tenantManagementIdempotencyPolicies = map[string]service.JSONFieldPolicy{
	http.MethodPost + " " + tenantCandidatePublishRoute: service.NewJSONFieldPolicy("success", "data"),
	http.MethodPost + " " + tenantGrantCreateRoute:      service.NewJSONFieldPolicy("success", "data"),
	http.MethodPost + " " + tenantGrantSuspendRoute:     service.NewJSONFieldPolicy("success", "data"),
	http.MethodPost + " " + tenantGrantResumeRoute:      service.NewJSONFieldPolicy("success", "data"),
	http.MethodPost + " " + tenantGrantRevokeRoute:      service.NewJSONFieldPolicy("success", "data"),
	http.MethodPost + " " + tenantSessionCreateRoute:    service.NewJSONFieldPolicy("success", "data"),
	http.MethodPost + " " + tenantSessionTerminateRoute: service.NewJSONFieldPolicy("success", "data"),
}

type tenantMutationResult struct {
	Data              any
	AggregateType     string
	AggregateID       string
	AggregateRevision int64
	RowVersion        int64
	EventType         string
	ResourceID        string
	GrantID           string
	SessionID         string
	VisibilityState   string
	Status            string
	ReasonCode        string
	TargetName        string
}

type tenantMutation func(*gorm.DB) (*tenantMutationResult, error)

func currentTenantAuthorization(c *gin.Context) (*service.ManagementAuthorizationContext, string, bool) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return nil, "", false
	}
	tenantID := strings.TrimSpace(c.Param("tenant_id"))
	if tenantID == "" || authorization.ScopeType != model.ManagementScopeTenant || authorization.ScopeID != tenantID {
		codedError(c, http.StatusNotFound, ErrorCodeManagementObjectMissing, "当前 Tenant 内对象不存在或不可见")
		return nil, "", false
	}
	return authorization, tenantID, true
}

func executeTenantIdempotentMutation(c *gin.Context, authorization *service.ManagementAuthorizationContext, tenantID string, body []byte, responseStatus int, action, reason string, mutation tenantMutation) {
	idempotencyBody, err := tenantIdempotencyRequestBody(c, body)
	if err != nil {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "幂等请求参数无效")
		return
	}
	idempotency := service.NewAPIIdempotencyService(db.DB, tenantManagementIdempotencyPolicies, 5*time.Minute, 24*time.Hour)
	begin, err := idempotency.Begin(c.Request.Context(), service.BeginIdempotencyInput{
		ActorType: "management_user", ActorID: providerIdempotencyActorID(authorization),
		ScopeType: string(model.ManagementScopeTenant), ScopeID: tenantID,
		Method: c.Request.Method, Route: c.FullPath(), Key: singleSafeHeader(c, HeaderIdempotencyKey, 128), Body: idempotencyBody,
	})
	if err != nil {
		writeSimulationIdempotencyError(c, err)
		return
	}
	if begin.Replay {
		setProviderReplayETag(c, begin.Record.ResponseBody)
		c.Data(begin.Record.ResponseStatus, "application/json; charset=utf-8", []byte(begin.Record.ResponseBody))
		return
	}

	var result *tenantMutationResult
	var responseBody []byte
	err = db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var mutationErr error
		result, mutationErr = mutation(tx)
		if mutationErr != nil {
			return mutationErr
		}
		responseBody, mutationErr = json.Marshal(NewSuccessResponse(gin.H{"result": result.Data, "row_version": result.RowVersion}))
		if mutationErr != nil {
			return mutationErr
		}
		if err := appendTenantMutationOutbox(tx, c, tenantID, result); err != nil {
			return fmt.Errorf("Tenant management outbox write failed: %w", err)
		}
		if err := recordTenantMutationAudit(tx, c, tenantID, action, reason, result); err != nil {
			return fmt.Errorf("Tenant management audit write failed: %w", err)
		}
		_, completeErr := idempotency.Complete(tx, service.CompleteIdempotencyInput{
			RecordID: begin.Record.ID, RequestHash: begin.Record.RequestHash, Status: model.APIIdempotencyCompleted,
			ResponseStatus: responseStatus, ResponseBody: responseBody,
		})
		return completeErr
	})
	if err != nil {
		writeTenantManagementError(c, err)
		return
	}
	SetRevisionETag(c, result.RowVersion)
	c.Data(responseStatus, "application/json; charset=utf-8", responseBody)
}

func executeTenantMutation(c *gin.Context, tenantID, action, reason string, responseStatus int, mutation tenantMutation) {
	var result *tenantMutationResult
	err := db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var mutationErr error
		result, mutationErr = mutation(tx)
		if mutationErr != nil {
			return mutationErr
		}
		if err := appendTenantMutationOutbox(tx, c, tenantID, result); err != nil {
			return fmt.Errorf("Tenant management outbox write failed: %w", err)
		}
		if err := recordTenantMutationAudit(tx, c, tenantID, action, reason, result); err != nil {
			return fmt.Errorf("Tenant management audit write failed: %w", err)
		}
		return nil
	})
	if err != nil {
		writeTenantManagementError(c, err)
		return
	}
	SetRevisionETag(c, result.RowVersion)
	c.JSON(responseStatus, NewSuccessResponse(gin.H{"result": result.Data, "row_version": result.RowVersion}))
}

func appendTenantMutationOutbox(tx *gorm.DB, c *gin.Context, tenantID string, result *tenantMutationResult) error {
	if result == nil || result.EventType == "" {
		return errors.New("Tenant mutation event is missing")
	}
	return service.AppendTenantManagementOutbox(tx, service.TenantManagementOutboxInput{
		EventType: result.EventType, AggregateType: result.AggregateType, AggregateID: result.AggregateID,
		AggregateRevision: result.AggregateRevision, TenantID: tenantID, ResourceID: result.ResourceID,
		GrantID: result.GrantID, SessionID: result.SessionID, VisibilityState: result.VisibilityState,
		Status: result.Status, RowVersion: result.RowVersion, ReasonCode: result.ReasonCode,
		RequestID: requestID(c), AvailableAt: time.Now().UTC(),
	})
}

func recordTenantMutationAudit(tx *gorm.DB, c *gin.Context, tenantID, action, reason string, result *tenantMutationResult) error {
	detail := gin.H{
		"tenant_id": tenantID, "reason": reason, "revision": result.AggregateRevision, "row_version": result.RowVersion,
	}
	if result.ResourceID != "" {
		detail["resource_id"] = result.ResourceID
	}
	if result.GrantID != "" {
		detail["grant_id"] = result.GrantID
	}
	if result.SessionID != "" {
		detail["session_id"] = result.SessionID
	}
	return recordAuditLogStrictWithDB(c.Request.Context(), tx, c, action, result.AggregateType, result.AggregateID, result.TargetName, detail)
}

func tenantIdempotencyRequestBody(c *gin.Context, body []byte) ([]byte, error) {
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil || value == nil {
		return nil, errors.New("invalid Tenant idempotency request")
	}
	value["_request_path"] = c.Request.URL.Path
	if revision, ok := requiredRevision(c); ok {
		value["_if_match_revision"] = revision
	}
	return json.Marshal(value)
}

func writeTenantManagementError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrTenantResourceInvalidInput), errors.Is(err, service.ErrTenantGrantInvalidInput), errors.Is(err, service.ErrResourceSessionInvalidInput):
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "Tenant 请求参数无效")
	case errors.Is(err, service.ErrTenantResourceNotFound), errors.Is(err, service.ErrTenantGrantNotFound), errors.Is(err, service.ErrResourceSessionNotFound):
		codedError(c, http.StatusNotFound, "TENANT_RESOURCE_NOT_FOUND", "当前 Tenant 内对象不存在或不可见")
	case errors.Is(err, service.ErrTenantResourceCrossTenantReference):
		codedError(c, http.StatusNotFound, "TENANT_RESOURCE_CROSS_TENANT_REFERENCE", "关联对象不存在或不属于当前 Tenant")
	case errors.Is(err, service.ErrManagementPermissionDenied):
		codedError(c, http.StatusForbidden, ErrorCodeManagementPermission, "Tenant 权限已失效")
	case errors.Is(err, service.ErrTenantResourceRevisionConflict):
		codedError(c, http.StatusConflict, "TENANT_RESOURCE_REVISION_CONFLICT", "Tenant Resource 版本已变化")
	case errors.Is(err, service.ErrTenantResourceUpstreamUnavailable):
		codedError(c, http.StatusConflict, "TENANT_RESOURCE_UPSTREAM_UNAVAILABLE", "Tenant Resource 上游授权链当前不可用")
	case errors.Is(err, service.ErrTenantResourceReviewStale):
		codedError(c, http.StatusConflict, "TENANT_RESOURCE_REVIEW_STALE", "Candidate 观测版本已变化")
	case errors.Is(err, service.ErrTenantResourceTargetNotTrusted):
		codedError(c, http.StatusConflict, "TENANT_RESOURCE_TARGET_NOT_TRUSTED", "当前没有可信 Target")
	case errors.Is(err, service.ErrTenantResourceServicePortChanged):
		codedError(c, http.StatusConflict, "TENANT_RESOURCE_SERVICE_PORT_CHANGED", "Service Port 或协议已变化")
	case errors.Is(err, service.ErrTenantResourceStateTransition):
		codedError(c, http.StatusConflict, "TENANT_RESOURCE_STATE_TRANSITION_INVALID", "Tenant Resource 状态转换无效")
	case errors.Is(err, service.ErrContainerSSHBusinessDomainInvalid):
		codedError(c, http.StatusUnprocessableEntity, "CONTAINER_SSH_BUSINESS_DOMAIN_INVALID", "ContainerSSH 工作负载、Namespace 或资源域标识无效")
	case errors.Is(err, service.ErrContainerSSHBusinessDomainConflict):
		codedError(c, http.StatusConflict, "CONTAINER_SSH_BUSINESS_DOMAIN_CONFLICT", "ContainerSSH 业务域已被其他资源占用")
	case errors.Is(err, service.ErrTenantGrantActionUnsupported):
		codedError(c, http.StatusUnprocessableEntity, "TENANT_GRANT_ACTION_UNSUPPORTED", "Grant action 不适用于该资源")
	case errors.Is(err, service.ErrTenantGrantSubjectInvalid):
		codedError(c, http.StatusUnprocessableEntity, "TENANT_GRANT_SUBJECT_INVALID", "Grant 主体无效或不属于当前 Tenant")
	case errors.Is(err, service.ErrTenantGrantTimeInvalid):
		codedError(c, http.StatusUnprocessableEntity, "TENANT_GRANT_TIME_INVALID", "Grant 有效期无效")
	case errors.Is(err, service.ErrTenantGrantVersionConflict):
		codedError(c, http.StatusConflict, "TENANT_GRANT_VERSION_CONFLICT", "Grant 版本已变化")
	case errors.Is(err, service.ErrTenantGrantStateTransition):
		codedError(c, http.StatusConflict, "TENANT_GRANT_STATE_TRANSITION_INVALID", "Grant 状态转换无效")
	case errors.Is(err, service.ErrTenantGrantConflict):
		codedError(c, http.StatusConflict, "TENANT_GRANT_CONFLICT", "该主体已存在有效 Grant")
	case errors.Is(err, service.ErrResourceSessionDeviceUnauthorized):
		codedError(c, http.StatusForbidden, "RESOURCE_SESSION_DEVICE_NOT_AUTHORIZED", "Device 无效或不属于当前用户")
	case errors.Is(err, service.ErrResourceSessionAuthorizationDenied):
		codedError(c, http.StatusForbidden, "RESOURCE_SESSION_AUTHORIZATION_DENIED", "资源会话授权链不成立")
	case errors.Is(err, service.ErrResourceSessionTargetUnavailable):
		codedError(c, http.StatusConflict, "RESOURCE_SESSION_TARGET_UNAVAILABLE", "当前 Target 不可连接")
	case errors.Is(err, service.ErrResourceSessionVersionConflict):
		codedError(c, http.StatusConflict, "RESOURCE_SESSION_VERSION_CONFLICT", "ResourceSession 版本已变化")
	case errors.Is(err, service.ErrResourceSessionStateTransition):
		codedError(c, http.StatusConflict, "RESOURCE_SESSION_STATE_TRANSITION_INVALID", "ResourceSession 状态转换无效")
	case errors.Is(err, service.ErrIdempotencyKeyReused), errors.Is(err, service.ErrIdempotencyInProgress), errors.Is(err, service.ErrIdempotencyRecoveryNeeded):
		writeSimulationIdempotencyError(c, err)
	default:
		codedError(c, http.StatusInternalServerError, "TENANT_MANAGEMENT_OPERATION_FAILED", "Tenant 资源操作失败")
	}
}
