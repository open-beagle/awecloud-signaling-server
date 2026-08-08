package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

const (
	platformAllocationCreateRoute   = "/api/v1/management/platform/allocations"
	platformAllocationUpdateRoute   = "/api/v1/management/platform/allocations/:id"
	platformAllocationScheduleRoute = "/api/v1/management/platform/allocations/:id/schedule"
	platformAllocationActivateRoute = "/api/v1/management/platform/allocations/:id/activate"
	platformAllocationSuspendRoute  = "/api/v1/management/platform/allocations/:id/suspend"
	platformAllocationResumeRoute   = "/api/v1/management/platform/allocations/:id/resume"
	platformAllocationRevokeRoute   = "/api/v1/management/platform/allocations/:id/revoke"
	platformAllocationRenewRoute    = "/api/v1/management/platform/allocations/:id/renew"

	ErrorCodePlatformAllocationNotFound    = "ALLOCATION_NOT_FOUND"
	ErrorCodePlatformAllocationVersion     = "ALLOCATION_VERSION_CONFLICT"
	ErrorCodePlatformAllocationState       = "ALLOCATION_STATE_TRANSITION_INVALID"
	ErrorCodePlatformAllocationMode        = "ALLOCATION_MODE_UNSUPPORTED"
	ErrorCodePlatformAllocationTime        = "ALLOCATION_TIME_INVALID"
	ErrorCodePlatformAllocationTenant      = "ALLOCATION_TENANT_NOT_ACTIVE"
	ErrorCodePlatformAllocationScope       = "ALLOCATION_SCOPE_NOT_ALLOCATABLE"
	ErrorCodePlatformAllocationItem        = "ALLOCATION_ITEM_POLICY_VIOLATION"
	ErrorCodePlatformAllocationConflict    = "ALLOCATION_SCOPE_CONFLICT"
	ErrorCodePlatformAllocationHierarchy   = "ALLOCATION_SCOPE_HIERARCHY_CONFLICT"
	ErrorCodePlatformAllocationReason      = "ALLOCATION_REASON_REQUIRED"
	ErrorCodePlatformAllocationWriteFailed = "ALLOCATION_WRITE_FAILED"
	ErrorCodePlatformAllocationQueryFailed = "ALLOCATION_QUERY_FAILED"
	ErrorCodePlatformAllocationAuditFailed = "ALLOCATION_AUDIT_FAILED"
)

var platformAllocationIdempotencyPolicies = map[string]service.JSONFieldPolicy{
	http.MethodPost + " " + platformAllocationCreateRoute:   service.NewJSONFieldPolicy("success", "data"),
	http.MethodPatch + " " + platformAllocationUpdateRoute:  service.NewJSONFieldPolicy("success", "data"),
	http.MethodPost + " " + platformAllocationScheduleRoute: service.NewJSONFieldPolicy("success", "data"),
	http.MethodPost + " " + platformAllocationActivateRoute: service.NewJSONFieldPolicy("success", "data"),
	http.MethodPost + " " + platformAllocationSuspendRoute:  service.NewJSONFieldPolicy("success", "data"),
	http.MethodPost + " " + platformAllocationResumeRoute:   service.NewJSONFieldPolicy("success", "data"),
	http.MethodPost + " " + platformAllocationRevokeRoute:   service.NewJSONFieldPolicy("success", "data"),
	http.MethodPost + " " + platformAllocationRenewRoute:    service.NewJSONFieldPolicy("success", "data"),
}

type PlatformAllocationAPI struct{}

func NewPlatformAllocationAPI() *PlatformAllocationAPI { return &PlatformAllocationAPI{} }

type platformAllocationDraftRequest struct {
	TenantID    string                       `json:"tenant_id"`
	Mode        model.ResourceAllocationMode `json:"mode"`
	ScopeID     string                       `json:"scope_id"`
	ValidFrom   time.Time                    `json:"valid_from"`
	ExpiresAt   *time.Time                   `json:"expires_at"`
	ContractRef string                       `json:"contract_ref"`
}

type platformAllocationReasonRequest struct {
	Reason string `json:"reason"`
}

type platformAllocationRenewRequest struct {
	ValidFrom   time.Time  `json:"valid_from"`
	ExpiresAt   *time.Time `json:"expires_at"`
	ContractRef *string    `json:"contract_ref"`
	Reason      string     `json:"reason"`
}

type platformAllocationMutation func(*service.PlatformAllocationService) (*model.ResourceAllocation, error)

func (a *PlatformAllocationAPI) List(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return
	}
	input, ok := platformAllocationListInput(c)
	if !ok {
		return
	}
	result, err := service.NewPlatformAllocationService(db.DB).List(c.Request.Context(), authorization, input)
	if err != nil {
		writePlatformAllocationError(c, err, false)
		return
	}
	c.JSON(http.StatusOK, NewPagedResponse(result.Items, result.Total, input.Page, input.PageSize))
}

func (a *PlatformAllocationAPI) Get(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return
	}
	allocation, err := service.NewPlatformAllocationService(db.DB).Get(c.Request.Context(), authorization, c.Param("id"))
	if err != nil {
		writePlatformAllocationError(c, err, false)
		return
	}
	SetRevisionETag(c, allocation.RowVersion)
	c.JSON(http.StatusOK, NewSuccessResponse(allocation))
}

func (a *PlatformAllocationAPI) Create(c *gin.Context) {
	authorization, body, request, ok := platformAllocationDraftRequestBody(c)
	if !ok {
		return
	}
	executePlatformAllocationMutation(c, authorization, body, http.StatusCreated, "create_resource_allocation", "resource_allocation.created", "",
		func(allocationService *service.PlatformAllocationService) (*model.ResourceAllocation, error) {
			return allocationService.CreateDraft(c.Request.Context(), authorization, service.CreatePlatformAllocationInput{
				TenantID: request.TenantID, Mode: request.Mode, ScopeID: request.ScopeID,
				ValidFrom: request.ValidFrom, ExpiresAt: request.ExpiresAt, ContractRef: request.ContractRef,
			})
		})
}

func (a *PlatformAllocationAPI) Update(c *gin.Context) {
	authorization, body, request, ok := platformAllocationDraftRequestBody(c)
	if !ok {
		return
	}
	rowVersion, ok := requiredRevision(c)
	if !ok {
		codedError(c, http.StatusPreconditionRequired, ErrorCodePreconditionRequired, "必须提供 If-Match revision")
		return
	}
	executePlatformAllocationMutation(c, authorization, body, http.StatusOK, "update_resource_allocation", "resource_allocation.updated", "",
		func(allocationService *service.PlatformAllocationService) (*model.ResourceAllocation, error) {
			return allocationService.UpdateDraft(c.Request.Context(), authorization, service.UpdatePlatformAllocationDraftInput{
				AllocationID: c.Param("id"), ExpectedRowVersion: rowVersion, TenantID: request.TenantID,
				Mode: request.Mode, ScopeID: request.ScopeID, ValidFrom: request.ValidFrom,
				ExpiresAt: request.ExpiresAt, ContractRef: request.ContractRef,
			})
		})
}

type platformAllocationTransition func(*service.PlatformAllocationService, *service.ManagementAuthorizationContext, service.PlatformAllocationActionInput) (*model.ResourceAllocation, error)

func (a *PlatformAllocationAPI) Schedule(c *gin.Context) {
	a.action(c, "schedule_resource_allocation", "resource_allocation.scheduled", func(s *service.PlatformAllocationService, authorization *service.ManagementAuthorizationContext, input service.PlatformAllocationActionInput) (*model.ResourceAllocation, error) {
		return s.Schedule(c.Request.Context(), authorization, input)
	})
}

func (a *PlatformAllocationAPI) Activate(c *gin.Context) {
	a.action(c, "activate_resource_allocation", "resource_allocation.activated", func(s *service.PlatformAllocationService, authorization *service.ManagementAuthorizationContext, input service.PlatformAllocationActionInput) (*model.ResourceAllocation, error) {
		return s.Activate(c.Request.Context(), authorization, input)
	})
}

func (a *PlatformAllocationAPI) Suspend(c *gin.Context) {
	a.action(c, "suspend_resource_allocation", "resource_allocation.suspended", func(s *service.PlatformAllocationService, authorization *service.ManagementAuthorizationContext, input service.PlatformAllocationActionInput) (*model.ResourceAllocation, error) {
		return s.Suspend(c.Request.Context(), authorization, input)
	})
}

func (a *PlatformAllocationAPI) Resume(c *gin.Context) {
	a.action(c, "resume_resource_allocation", "resource_allocation.resumed", func(s *service.PlatformAllocationService, authorization *service.ManagementAuthorizationContext, input service.PlatformAllocationActionInput) (*model.ResourceAllocation, error) {
		return s.Resume(c.Request.Context(), authorization, input)
	})
}

func (a *PlatformAllocationAPI) Revoke(c *gin.Context) {
	a.action(c, "revoke_resource_allocation", "resource_allocation.revoked", func(s *service.PlatformAllocationService, authorization *service.ManagementAuthorizationContext, input service.PlatformAllocationActionInput) (*model.ResourceAllocation, error) {
		return s.Revoke(c.Request.Context(), authorization, input)
	})
}

func (a *PlatformAllocationAPI) action(c *gin.Context, action, eventType string, transition platformAllocationTransition) {
	authorization, rowVersion, body, request, ok := platformAllocationReasonRequestBody(c)
	if !ok {
		return
	}
	input := service.PlatformAllocationActionInput{
		AllocationID: c.Param("id"), ExpectedRowVersion: rowVersion, Reason: request.Reason,
	}
	executePlatformAllocationMutation(c, authorization, body, http.StatusOK, action, eventType, request.Reason,
		func(allocationService *service.PlatformAllocationService) (*model.ResourceAllocation, error) {
			return transition(allocationService, authorization, input)
		})
}

func (a *PlatformAllocationAPI) Renew(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return
	}
	rowVersion, ok := requiredRevision(c)
	if !ok {
		codedError(c, http.StatusPreconditionRequired, ErrorCodePreconditionRequired, "必须提供 If-Match revision")
		return
	}
	var request platformAllocationRenewRequest
	body, ok := decodePlatformAllocationRequest(c, &request)
	if !ok {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len(request.Reason) > 500 {
		writePlatformAllocationError(c, service.ErrPlatformAllocationReasonRequired, true)
		return
	}
	executePlatformAllocationMutation(c, authorization, body, http.StatusCreated, "renew_resource_allocation", "resource_allocation.renewed", request.Reason,
		func(allocationService *service.PlatformAllocationService) (*model.ResourceAllocation, error) {
			return allocationService.Renew(c.Request.Context(), authorization, service.RenewPlatformAllocationInput{
				AllocationID: c.Param("id"), ExpectedRowVersion: rowVersion, ValidFrom: request.ValidFrom,
				ExpiresAt: request.ExpiresAt, ContractRef: request.ContractRef, Reason: request.Reason,
			})
		})
}

func platformAllocationDraftRequestBody(c *gin.Context) (*service.ManagementAuthorizationContext, []byte, platformAllocationDraftRequest, bool) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return nil, nil, platformAllocationDraftRequest{}, false
	}
	var request platformAllocationDraftRequest
	body, ok := decodePlatformAllocationRequest(c, &request)
	if !ok {
		return nil, nil, request, false
	}
	request.TenantID = strings.TrimSpace(request.TenantID)
	request.ScopeID = strings.TrimSpace(request.ScopeID)
	request.ContractRef = strings.TrimSpace(request.ContractRef)
	return authorization, body, request, true
}

func platformAllocationReasonRequestBody(c *gin.Context) (*service.ManagementAuthorizationContext, int64, []byte, platformAllocationReasonRequest, bool) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return nil, 0, nil, platformAllocationReasonRequest{}, false
	}
	rowVersion, ok := requiredRevision(c)
	if !ok {
		codedError(c, http.StatusPreconditionRequired, ErrorCodePreconditionRequired, "必须提供 If-Match revision")
		return nil, 0, nil, platformAllocationReasonRequest{}, false
	}
	var request platformAllocationReasonRequest
	body, ok := decodePlatformAllocationRequest(c, &request)
	if !ok {
		return nil, 0, nil, request, false
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len(request.Reason) > 500 {
		writePlatformAllocationError(c, service.ErrPlatformAllocationReasonRequired, true)
		return nil, 0, nil, request, false
	}
	return authorization, rowVersion, body, request, true
}

func decodePlatformAllocationRequest(c *gin.Context, target any) ([]byte, bool) {
	body, ok := readSimulationJSON(c)
	if !ok {
		return nil, false
	}
	if !decodeSimulationJSON(c, body, target) {
		return nil, false
	}
	return body, true
}

func platformAllocationListInput(c *gin.Context) (service.PlatformAllocationListInput, bool) {
	input := service.PlatformAllocationListInput{
		TenantID: strings.TrimSpace(c.Query("tenant_id")), ProviderID: strings.TrimSpace(c.Query("provider_id")),
		ResourceID: strings.TrimSpace(c.Query("resource_id")), ScopeID: strings.TrimSpace(c.Query("scope_id")),
		Mode: strings.TrimSpace(c.Query("mode")), State: strings.TrimSpace(c.Query("state")), Search: strings.TrimSpace(c.Query("search")),
		Page: 1, PageSize: 20,
	}
	for key := range c.Request.URL.Query() {
		switch key {
		case "tenant_id", "provider_id", "resource_id", "scope_id", "mode", "state", "search", "valid_at", "page", "size":
		default:
			codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "资源分配查询参数无效")
			return input, false
		}
	}
	var err error
	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		input.Page, err = strconv.Atoi(raw)
		if err != nil {
			codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "分页参数无效")
			return input, false
		}
	}
	if raw := strings.TrimSpace(c.Query("size")); raw != "" {
		input.PageSize, err = strconv.Atoi(raw)
		if err != nil {
			codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "分页参数无效")
			return input, false
		}
	}
	if raw := strings.TrimSpace(c.Query("valid_at")); raw != "" {
		value, parseErr := time.Parse(time.RFC3339, raw)
		if parseErr != nil {
			codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "有效时间参数无效")
			return input, false
		}
		input.ValidAt = &value
	}
	if input.Page < 1 || input.PageSize < 1 || input.PageSize > 100 {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "分页参数无效")
		return input, false
	}
	return input, true
}

func executePlatformAllocationMutation(c *gin.Context, authorization *service.ManagementAuthorizationContext, body []byte, status int, action, _ string, reason string, mutation platformAllocationMutation) {
	idempotencyBody, err := platformAllocationIdempotencyBody(c, body)
	if err != nil {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "幂等请求参数无效")
		return
	}
	idempotency := service.NewAPIIdempotencyService(db.DB, platformAllocationIdempotencyPolicies, 5*time.Minute, 24*time.Hour)
	begin, err := idempotency.Begin(c.Request.Context(), service.BeginIdempotencyInput{
		ActorType: "management_user", ActorID: providerIdempotencyActorID(authorization), ScopeType: string(model.ManagementScopePlatform),
		ScopeID: "platform", Method: c.Request.Method, Route: c.FullPath(), Key: singleSafeHeader(c, HeaderIdempotencyKey, 128), Body: idempotencyBody,
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

	var responseBody []byte
	var allocation *model.ResourceAllocation
	err = db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var mutationErr error
		allocation, mutationErr = mutation(service.NewPlatformAllocationService(tx))
		if mutationErr != nil {
			return mutationErr
		}
		responseBody, mutationErr = json.Marshal(NewSuccessResponse(gin.H{"result": allocation, "row_version": allocation.RowVersion}))
		if mutationErr != nil {
			return mutationErr
		}
		if err := recordPlatformAllocationAudit(tx, c, allocation, action, reason); err != nil {
			return fmt.Errorf("%w: %v", errPlatformAllocationAudit, err)
		}
		_, completeErr := idempotency.Complete(tx, service.CompleteIdempotencyInput{
			RecordID: begin.Record.ID, RequestHash: begin.Record.RequestHash, Status: model.APIIdempotencyCompleted,
			ResponseStatus: status, ResponseBody: responseBody,
		})
		return completeErr
	})
	if err != nil {
		writePlatformAllocationError(c, err, true)
		return
	}
	SetRevisionETag(c, allocation.RowVersion)
	c.Data(status, "application/json; charset=utf-8", responseBody)
}

var (
	errPlatformAllocationAudit = errors.New("Platform allocation audit write failed")
)

func recordPlatformAllocationAudit(tx *gorm.DB, c *gin.Context, allocation *model.ResourceAllocation, action, reason string) error {
	scopeIDs := make([]string, 0, len(allocation.Items))
	for _, item := range allocation.Items {
		scopeIDs = append(scopeIDs, item.ScopeID)
	}
	detail := gin.H{
		"reason": reason, "tenant_id": allocation.TenantID, "scope_ids": scopeIDs,
		"state": allocation.State, "row_version": allocation.RowVersion,
	}
	if allocation.RenewedFromID != nil {
		detail["renewed_from_id"] = *allocation.RenewedFromID
	}
	return recordAuditLogStrictWithDB(c.Request.Context(), tx, c, action, "resource_allocation", allocation.ID, allocation.ID, detail)
}

func platformAllocationIdempotencyBody(c *gin.Context, body []byte) ([]byte, error) {
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil || value == nil {
		return nil, fmt.Errorf("invalid Platform allocation idempotency request")
	}
	value["_request_path"] = c.Request.URL.Path
	if revision, ok := requiredRevision(c); ok {
		value["_if_match_revision"] = revision
	}
	return json.Marshal(value)
}

func writePlatformAllocationError(c *gin.Context, err error, write bool) {
	switch {
	case errors.Is(err, service.ErrPlatformAllocationInvalidInput):
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "资源分配请求参数无效")
	case errors.Is(err, service.ErrPlatformAllocationObjectNotFound):
		codedError(c, http.StatusNotFound, ErrorCodePlatformAllocationNotFound, "资源分配不存在或不可见")
	case errors.Is(err, service.ErrManagementPermissionDenied):
		codedError(c, http.StatusForbidden, ErrorCodeManagementPermission, "Platform 资源分配权限已失效")
	case errors.Is(err, service.ErrPlatformAllocationVersionConflict):
		codedError(c, http.StatusConflict, ErrorCodePlatformAllocationVersion, "资源分配版本已变化")
	case errors.Is(err, service.ErrPlatformAllocationStateTransition):
		codedError(c, http.StatusConflict, ErrorCodePlatformAllocationState, "资源分配状态转换无效")
	case errors.Is(err, service.ErrPlatformAllocationModeUnsupported):
		codedError(c, http.StatusUnprocessableEntity, ErrorCodePlatformAllocationMode, "当前版本不支持该分配方式")
	case errors.Is(err, service.ErrPlatformAllocationTimeInvalid):
		codedError(c, http.StatusUnprocessableEntity, ErrorCodePlatformAllocationTime, "资源分配时间窗口无效")
	case errors.Is(err, service.ErrPlatformAllocationTenantNotActive):
		codedError(c, http.StatusUnprocessableEntity, ErrorCodePlatformAllocationTenant, "目标租户当前不可分配")
	case errors.Is(err, service.ErrPlatformAllocationScopeUnavailable):
		codedError(c, http.StatusUnprocessableEntity, ErrorCodePlatformAllocationScope, "Scope 当前不满足分配条件")
	case errors.Is(err, service.ErrPlatformAllocationItemPolicy):
		codedError(c, http.StatusUnprocessableEntity, ErrorCodePlatformAllocationItem, "当前版本只允许一个 Namespace Scope")
	case errors.Is(err, service.ErrPlatformAllocationScopeConflict):
		codedError(c, http.StatusConflict, ErrorCodePlatformAllocationConflict, "Scope 在该时间窗口已被占用")
	case errors.Is(err, service.ErrPlatformAllocationHierarchyConflict):
		codedError(c, http.StatusConflict, ErrorCodePlatformAllocationHierarchy, "Scope 与现有 Cluster/Namespace 分配冲突")
	case errors.Is(err, service.ErrPlatformAllocationReasonRequired):
		codedError(c, http.StatusUnprocessableEntity, ErrorCodePlatformAllocationReason, "必须提供有效操作原因")
	case errors.Is(err, errPlatformAllocationAudit):
		codedError(c, http.StatusServiceUnavailable, ErrorCodePlatformAllocationAuditFailed, "资源分配审计写入失败")
	default:
		code := ErrorCodePlatformAllocationQueryFailed
		message := "查询资源分配失败"
		if write {
			code = ErrorCodePlatformAllocationWriteFailed
			message = "写入资源分配失败"
		}
		codedError(c, http.StatusInternalServerError, code, message)
	}
}
