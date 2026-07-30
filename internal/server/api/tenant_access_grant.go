package api

import (
	"bytes"
	"encoding/json"
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

type TenantAccessGrantAPI struct{}

func NewTenantAccessGrantAPI() *TenantAccessGrantAPI { return &TenantAccessGrantAPI{} }

type tenantGrantSubjectRequest struct {
	Type    model.TenantAccessGrantSubjectType `json:"type"`
	UserID  *uint64                            `json:"user_id"`
	GroupID *int64                             `json:"group_id"`
}

type tenantGrantCreateRequest struct {
	ResourceID        string                    `json:"resource_id"`
	Subject           tenantGrantSubjectRequest `json:"subject"`
	Actions           []string                  `json:"actions"`
	ValidFrom         *time.Time                `json:"valid_from"`
	ExpiresAt         *time.Time                `json:"expires_at"`
	MaxSessionSeconds int                       `json:"max_session_seconds"`
}

type optionalNullableTime struct {
	Set   bool
	Value *time.Time
}

func (o *optionalNullableTime) UnmarshalJSON(data []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		o.Value = nil
		return nil
	}
	var value time.Time
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	o.Value = &value
	return nil
}

type tenantGrantUpdateRequest struct {
	Actions           *[]string            `json:"actions"`
	ValidFrom         *time.Time           `json:"valid_from"`
	ExpiresAt         optionalNullableTime `json:"expires_at"`
	MaxSessionSeconds *int                 `json:"max_session_seconds"`
}

type tenantGrantActionRequest struct {
	Reason string `json:"reason"`
}

func (a *TenantAccessGrantAPI) List(c *gin.Context) {
	authorization, tenantID, ok := currentTenantAuthorization(c)
	if !ok {
		return
	}
	input, ok := tenantGrantListInput(c)
	if !ok {
		return
	}
	result, err := service.NewTenantAccessGrantService(db.DB).List(c.Request.Context(), authorization, tenantID, input)
	if err != nil {
		writeTenantManagementError(c, err)
		return
	}
	c.JSON(http.StatusOK, NewPagedResponse(result.Items, result.Total, input.Page, input.PageSize))
}

func (a *TenantAccessGrantAPI) Get(c *gin.Context) {
	authorization, tenantID, ok := currentTenantAuthorization(c)
	if !ok {
		return
	}
	view, err := service.NewTenantAccessGrantService(db.DB).Get(c.Request.Context(), authorization, tenantID, c.Param("id"))
	if err != nil {
		writeTenantManagementError(c, err)
		return
	}
	SetRevisionETag(c, view.RowVersion)
	c.JSON(http.StatusOK, NewSuccessResponse(view))
}

func (a *TenantAccessGrantAPI) Create(c *gin.Context) {
	authorization, tenantID, ok := currentTenantAuthorization(c)
	if !ok {
		return
	}
	var request tenantGrantCreateRequest
	body, ok := decodeTenantManagementRequest(c, &request)
	if !ok {
		return
	}
	request.ResourceID = strings.TrimSpace(request.ResourceID)
	validFrom := time.Time{}
	if request.ValidFrom != nil {
		validFrom = *request.ValidFrom
	}
	executeTenantIdempotentMutation(c, authorization, tenantID, body, http.StatusCreated, "create_tenant_access_grant", "",
		func(tx *gorm.DB) (*tenantMutationResult, error) {
			grantService := service.NewTenantAccessGrantService(tx)
			grant, err := grantService.Create(c.Request.Context(), authorization, service.CreateTenantGrantInput{
				TenantID: tenantID, ResourceID: request.ResourceID, SubjectType: request.Subject.Type,
				SubjectUserID: request.Subject.UserID, SubjectGroupID: request.Subject.GroupID, Actions: request.Actions,
				ValidFrom: validFrom, ExpiresAt: request.ExpiresAt, MaxSessionSeconds: request.MaxSessionSeconds, RequestID: requestID(c),
			})
			if err != nil {
				return nil, err
			}
			view, err := grantService.View(grant)
			if err != nil {
				return nil, err
			}
			return tenantGrantMutationResult(view, "tenant_access_grant.created"), nil
		})
}

func (a *TenantAccessGrantAPI) Update(c *gin.Context) {
	authorization, tenantID, rowVersion, ok := tenantGrantMutationContext(c)
	if !ok {
		return
	}
	var request tenantGrantUpdateRequest
	if _, ok := decodeTenantManagementRequest(c, &request); !ok {
		return
	}
	executeTenantMutation(c, tenantID, "update_tenant_access_grant", "", http.StatusOK,
		func(tx *gorm.DB) (*tenantMutationResult, error) {
			grantService := service.NewTenantAccessGrantService(tx)
			grant, err := grantService.Update(c.Request.Context(), authorization, service.UpdateTenantGrantInput{
				TenantID: tenantID, GrantID: c.Param("id"), ExpectedRowVersion: rowVersion,
				Actions: request.Actions, ValidFrom: request.ValidFrom, ExpiresAt: request.ExpiresAt.Value,
				SetExpiresAt: request.ExpiresAt.Set, MaxSessionSeconds: request.MaxSessionSeconds, RequestID: requestID(c),
			})
			if err != nil {
				return nil, err
			}
			view, err := grantService.View(grant)
			if err != nil {
				return nil, err
			}
			return tenantGrantMutationResult(view, "tenant_access_grant.updated"), nil
		})
}

func (a *TenantAccessGrantAPI) Suspend(c *gin.Context) {
	a.action(c, model.TenantAccessGrantSuspended, "suspend_tenant_access_grant", "tenant_access_grant.suspended")
}

func (a *TenantAccessGrantAPI) Resume(c *gin.Context) {
	a.action(c, model.TenantAccessGrantEnabled, "resume_tenant_access_grant", "tenant_access_grant.resumed")
}

func (a *TenantAccessGrantAPI) Revoke(c *gin.Context) {
	a.action(c, model.TenantAccessGrantRevoked, "revoke_tenant_access_grant", "tenant_access_grant.revoked")
}

func (a *TenantAccessGrantAPI) action(c *gin.Context, target model.TenantAccessGrantStatus, action, eventType string) {
	authorization, tenantID, rowVersion, ok := tenantGrantMutationContext(c)
	if !ok {
		return
	}
	var request tenantGrantActionRequest
	decode := decodeOptionalTenantManagementRequest
	if target == model.TenantAccessGrantRevoked {
		decode = decodeTenantManagementRequest
	}
	body, ok := decode(c, &request)
	if !ok {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	executeTenantIdempotentMutation(c, authorization, tenantID, body, http.StatusOK, action, request.Reason,
		func(tx *gorm.DB) (*tenantMutationResult, error) {
			grantService := service.NewTenantAccessGrantService(tx)
			input := service.TenantGrantActionInput{
				TenantID: tenantID, GrantID: c.Param("id"), ExpectedRowVersion: rowVersion,
				Reason: request.Reason, RequestID: requestID(c),
			}
			var grant *model.TenantAccessGrant
			var err error
			switch target {
			case model.TenantAccessGrantSuspended:
				grant, err = grantService.Suspend(c.Request.Context(), authorization, input)
			case model.TenantAccessGrantEnabled:
				grant, err = grantService.Resume(c.Request.Context(), authorization, input)
			case model.TenantAccessGrantRevoked:
				grant, err = grantService.Revoke(c.Request.Context(), authorization, input)
			}
			if err != nil {
				return nil, err
			}
			view, err := grantService.View(grant)
			if err != nil {
				return nil, err
			}
			return tenantGrantMutationResult(view, eventType), nil
		})
}

func tenantGrantMutationContext(c *gin.Context) (*service.ManagementAuthorizationContext, string, int64, bool) {
	authorization, tenantID, ok := currentTenantAuthorization(c)
	if !ok {
		return nil, "", 0, false
	}
	rowVersion, ok := requiredRevision(c)
	if !ok {
		codedError(c, http.StatusPreconditionRequired, ErrorCodePreconditionRequired, "必须提供 If-Match revision")
		return nil, "", 0, false
	}
	return authorization, tenantID, rowVersion, true
}

func tenantGrantMutationResult(view *service.TenantGrantView, eventType string) *tenantMutationResult {
	return &tenantMutationResult{
		Data: view, AggregateType: "tenant_access_grant", AggregateID: view.ID,
		AggregateRevision: view.Revision, RowVersion: view.RowVersion, EventType: eventType,
		ResourceID: view.ResourceID, GrantID: view.ID, Status: string(view.Status), TargetName: view.ID,
	}
}

func tenantGrantListInput(c *gin.Context) (service.TenantGrantListInput, bool) {
	input := service.TenantGrantListInput{
		ResourceID: strings.TrimSpace(c.Query("resource_id")), SubjectType: strings.TrimSpace(c.Query("subject_type")),
		Status: strings.TrimSpace(c.Query("status")), Page: 1, PageSize: 20,
	}
	for key := range c.Request.URL.Query() {
		switch key {
		case "resource_id", "subject_type", "status", "page", "size":
		default:
			codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "Grant 查询参数无效")
			return input, false
		}
	}
	var err error
	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		input.Page, err = strconv.Atoi(raw)
	}
	if err == nil {
		if raw := strings.TrimSpace(c.Query("size")); raw != "" {
			input.PageSize, err = strconv.Atoi(raw)
		}
	}
	if err != nil {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "分页参数无效")
		return input, false
	}
	return input, true
}
