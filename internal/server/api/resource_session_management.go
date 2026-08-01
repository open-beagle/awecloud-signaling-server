package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

type ResourceSessionManagementAPI struct{}

func NewResourceSessionManagementAPI() *ResourceSessionManagementAPI {
	return &ResourceSessionManagementAPI{}
}

type resourceSessionCreateRequest struct {
	ResourceID       string `json:"resource_id"`
	Action           string `json:"action"`
	DeviceID         uint64 `json:"device_id"`
	ClientCapability string `json:"client_capability"`
}

type resourceSessionTerminateRequest struct {
	Reason string `json:"reason"`
}

func (a *ResourceSessionManagementAPI) List(c *gin.Context) {
	authorization, tenantID, ok := currentTenantAuthorization(c)
	if !ok {
		return
	}
	input, ok := resourceSessionListInput(c)
	if !ok {
		return
	}
	result, err := service.NewResourceSessionService(db.DB).List(c.Request.Context(), authorization, tenantID, input)
	if err != nil {
		writeTenantManagementError(c, err)
		return
	}
	c.JSON(http.StatusOK, NewPagedResponse(result.Items, result.Total, input.Page, input.PageSize))
}

func (a *ResourceSessionManagementAPI) Get(c *gin.Context) {
	authorization, tenantID, ok := currentTenantAuthorization(c)
	if !ok {
		return
	}
	view, err := service.NewResourceSessionService(db.DB).Get(c.Request.Context(), authorization, tenantID, c.Param("id"))
	if err != nil {
		writeTenantManagementError(c, err)
		return
	}
	SetRevisionETag(c, view.RowVersion)
	c.JSON(http.StatusOK, NewSuccessResponse(view))
}

func (a *ResourceSessionManagementAPI) Create(c *gin.Context) {
	authorization, tenantID, ok := currentTenantAuthorization(c)
	if !ok {
		return
	}
	var request resourceSessionCreateRequest
	body, ok := decodeTenantManagementRequest(c, &request)
	if !ok {
		return
	}
	request.ResourceID, request.Action, request.ClientCapability = strings.TrimSpace(request.ResourceID), strings.TrimSpace(request.Action), strings.TrimSpace(request.ClientCapability)
	executeTenantIdempotentMutation(c, authorization, tenantID, body, http.StatusCreated, "authorize_resource_session", "",
		func(tx *gorm.DB) (*tenantMutationResult, error) {
			sessionService := service.NewResourceSessionService(tx)
			session, err := sessionService.Create(c.Request.Context(), authorization, service.CreateResourceSessionInput{
				TenantID: tenantID, ResourceID: request.ResourceID, Action: request.Action, DeviceID: request.DeviceID,
				ClientCapability: request.ClientCapability, RequestID: requestID(c), TraceID: requestTraceID(c),
			})
			if err != nil {
				return nil, err
			}
			view, err := sessionService.View(session)
			if err != nil {
				return nil, err
			}
			return resourceSessionMutationResult(view, "resource_session.authorized", ""), nil
		})
}

func (a *ResourceSessionManagementAPI) Terminate(c *gin.Context) {
	authorization, tenantID, ok := currentTenantAuthorization(c)
	if !ok {
		return
	}
	rowVersion, ok := requiredRevision(c)
	if !ok {
		codedError(c, http.StatusPreconditionRequired, ErrorCodePreconditionRequired, "必须提供 If-Match revision")
		return
	}
	var request resourceSessionTerminateRequest
	body, ok := decodeTenantManagementRequest(c, &request)
	if !ok {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	executeTenantIdempotentMutation(c, authorization, tenantID, body, http.StatusOK, "terminate_resource_session", request.Reason,
		func(tx *gorm.DB) (*tenantMutationResult, error) {
			sessionService := service.NewResourceSessionService(tx)
			session, err := sessionService.Terminate(c.Request.Context(), authorization, service.TerminateResourceSessionInput{
				TenantID: tenantID, SessionID: c.Param("id"), ExpectedRowVersion: rowVersion,
				Reason: request.Reason, RequestID: requestID(c),
			})
			if err != nil {
				return nil, err
			}
			view, err := sessionService.View(session)
			if err != nil {
				return nil, err
			}
			return resourceSessionMutationResult(view, "resource_session.ending", "MANUAL_TERMINATION"), nil
		})
}

func resourceSessionMutationResult(view *service.ResourceSessionView, eventType, reasonCode string) *tenantMutationResult {
	return &tenantMutationResult{
		Data: view, AggregateType: "resource_session", AggregateID: view.ID,
		AggregateRevision: view.RowVersion, RowVersion: view.RowVersion, EventType: eventType,
		ResourceID: view.ResourceID, GrantID: view.GrantID, SessionID: view.ID,
		Status: string(view.Status), ReasonCode: reasonCode, TargetName: view.ID,
	}
}

func resourceSessionListInput(c *gin.Context) (service.ResourceSessionListInput, bool) {
	input := service.ResourceSessionListInput{
		ResourceID: strings.TrimSpace(c.Query("resource_id")), Status: strings.TrimSpace(c.Query("status")), Page: 1, PageSize: 20,
	}
	for key := range c.Request.URL.Query() {
		switch key {
		case "resource_id", "user_id", "status", "page", "size":
		default:
			codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "ResourceSession 查询参数无效")
			return input, false
		}
	}
	var err error
	if raw := strings.TrimSpace(c.Query("user_id")); raw != "" {
		input.UserID, err = strconv.ParseUint(raw, 10, 64)
	}
	if err == nil {
		if raw := strings.TrimSpace(c.Query("page")); raw != "" {
			input.Page, err = strconv.Atoi(raw)
		}
	}
	if err == nil {
		if raw := strings.TrimSpace(c.Query("size")); raw != "" {
			input.PageSize, err = strconv.Atoi(raw)
		}
	}
	if err != nil {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "查询参数无效")
		return input, false
	}
	return input, true
}

func requestTraceID(c *gin.Context) string {
	spanContext := trace.SpanContextFromContext(c.Request.Context())
	if !spanContext.IsValid() {
		return ""
	}
	return spanContext.TraceID().String()
}
