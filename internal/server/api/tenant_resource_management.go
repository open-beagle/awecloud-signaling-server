package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

type TenantResourceManagementAPI struct{}

func NewTenantResourceManagementAPI() *TenantResourceManagementAPI {
	return &TenantResourceManagementAPI{}
}

type tenantResourceReviewRequest struct {
	ObservationRevision int64  `json:"observation_revision"`
	Reason              string `json:"reason"`
}

type tenantResourceUpdateRequest struct {
	DisplayName *string `json:"display_name"`
	Description *string `json:"description"`
}

type tenantResourceActionRequest struct {
	Reason string `json:"reason"`
}

func (a *TenantResourceManagementAPI) ListCandidates(c *gin.Context) {
	a.list(c, true)
}

func (a *TenantResourceManagementAPI) ListResources(c *gin.Context) {
	a.list(c, false)
}

func (a *TenantResourceManagementAPI) list(c *gin.Context, candidates bool) {
	authorization, tenantID, ok := currentTenantAuthorization(c)
	if !ok {
		return
	}
	input, ok := tenantResourceListInput(c, candidates)
	if !ok {
		return
	}
	result, err := service.NewTenantResourceService(db.DB).List(c.Request.Context(), authorization, tenantID, input)
	if err != nil {
		writeTenantManagementError(c, err)
		return
	}
	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

func (a *TenantResourceManagementAPI) GetCandidate(c *gin.Context) {
	a.get(c, true)
}

func (a *TenantResourceManagementAPI) GetResource(c *gin.Context) {
	a.get(c, false)
}

func (a *TenantResourceManagementAPI) get(c *gin.Context, candidate bool) {
	authorization, tenantID, ok := currentTenantAuthorization(c)
	if !ok {
		return
	}
	view, err := service.NewTenantResourceService(db.DB).Get(c.Request.Context(), authorization, tenantID, tenantResourceIDParam(c), candidate)
	if err != nil {
		writeTenantManagementError(c, err)
		return
	}
	SetRevisionETag(c, view.RowVersion)
	c.JSON(http.StatusOK, NewSuccessResponse(view))
}

func (a *TenantResourceManagementAPI) PublishCandidate(c *gin.Context) {
	authorization, tenantID, ok := currentTenantAuthorization(c)
	if !ok {
		return
	}
	rowVersion, ok := requiredRevision(c)
	if !ok {
		codedError(c, http.StatusPreconditionRequired, ErrorCodePreconditionRequired, "必须提供 If-Match revision")
		return
	}
	var request tenantResourceReviewRequest
	body, ok := decodeTenantManagementRequest(c, &request)
	if !ok {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	executeTenantIdempotentMutation(c, authorization, tenantID, body, http.StatusOK, "publish_tenant_resource", request.Reason,
		func(tx *gorm.DB) (*tenantMutationResult, error) {
			resourceService := service.NewTenantResourceService(tx)
			resource, err := resourceService.Review(c.Request.Context(), authorization, service.ReviewTenantResourceInput{
				TenantID: tenantID, ResourceID: tenantResourceIDParam(c), ExpectedRowVersion: rowVersion,
				ObservationRevision: request.ObservationRevision, Reason: request.Reason, Publish: true,
			})
			if err != nil {
				return nil, err
			}
			view, err := resourceService.Get(c.Request.Context(), authorization, tenantID, resource.ID, false)
			if err != nil {
				return nil, err
			}
			return tenantResourceMutationResult(view, "tenant_resource.published"), nil
		})
}

func (a *TenantResourceManagementAPI) RejectCandidate(c *gin.Context) {
	authorization, tenantID, ok := currentTenantAuthorization(c)
	if !ok {
		return
	}
	var request tenantResourceReviewRequest
	if _, ok := decodeTenantManagementRequest(c, &request); !ok {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	executeTenantMutation(c, tenantID, "reject_tenant_resource", request.Reason, http.StatusOK,
		func(tx *gorm.DB) (*tenantMutationResult, error) {
			resourceService := service.NewTenantResourceService(tx)
			resource, err := resourceService.Review(c.Request.Context(), authorization, service.ReviewTenantResourceInput{
				TenantID: tenantID, ResourceID: tenantResourceIDParam(c), ObservationRevision: request.ObservationRevision,
				Reason: request.Reason, Publish: false,
			})
			if err != nil {
				return nil, err
			}
			view, err := resourceService.Get(c.Request.Context(), authorization, tenantID, resource.ID, true)
			if err != nil {
				return nil, err
			}
			return tenantResourceMutationResult(view, "tenant_resource.rejected"), nil
		})
}

func (a *TenantResourceManagementAPI) UpdateResource(c *gin.Context) {
	authorization, tenantID, ok := currentTenantAuthorization(c)
	if !ok {
		return
	}
	rowVersion, ok := requiredRevision(c)
	if !ok {
		codedError(c, http.StatusPreconditionRequired, ErrorCodePreconditionRequired, "必须提供 If-Match revision")
		return
	}
	var request tenantResourceUpdateRequest
	if _, ok := decodeTenantManagementRequest(c, &request); !ok {
		return
	}
	executeTenantMutation(c, tenantID, "update_tenant_resource", "", http.StatusOK,
		func(tx *gorm.DB) (*tenantMutationResult, error) {
			resourceService := service.NewTenantResourceService(tx)
			resource, err := resourceService.Update(c.Request.Context(), authorization, service.UpdateTenantResourceInput{
				TenantID: tenantID, ResourceID: tenantResourceIDParam(c), ExpectedRowVersion: rowVersion,
				DisplayName: request.DisplayName, Description: request.Description,
			})
			if err != nil {
				return nil, err
			}
			view, err := resourceService.Get(c.Request.Context(), authorization, tenantID, resource.ID, false)
			if err != nil {
				return nil, err
			}
			return tenantResourceMutationResult(view, "tenant_resource.updated"), nil
		})
}

func (a *TenantResourceManagementAPI) HideResource(c *gin.Context) {
	a.setVisibility(c, model.TenantResourceHidden, "hide_tenant_resource")
}

func (a *TenantResourceManagementAPI) ShowResource(c *gin.Context) {
	a.setVisibility(c, model.TenantResourceVisible, "show_tenant_resource")
}

func (a *TenantResourceManagementAPI) setVisibility(c *gin.Context, target model.TenantResourceVisibilityState, action string) {
	authorization, tenantID, ok := currentTenantAuthorization(c)
	if !ok {
		return
	}
	rowVersion, ok := requiredRevision(c)
	if !ok {
		codedError(c, http.StatusPreconditionRequired, ErrorCodePreconditionRequired, "必须提供 If-Match revision")
		return
	}
	var request tenantResourceActionRequest
	if _, ok := decodeOptionalTenantManagementRequest(c, &request); !ok {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if len(request.Reason) > 500 {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "操作原因无效")
		return
	}
	executeTenantMutation(c, tenantID, action, request.Reason, http.StatusOK,
		func(tx *gorm.DB) (*tenantMutationResult, error) {
			resourceService := service.NewTenantResourceService(tx)
			resource, err := resourceService.SetVisibility(c.Request.Context(), authorization, tenantID, tenantResourceIDParam(c), rowVersion, target)
			if err != nil {
				return nil, err
			}
			view, err := resourceService.Get(c.Request.Context(), authorization, tenantID, resource.ID, false)
			if err != nil {
				return nil, err
			}
			return tenantResourceMutationResult(view, "tenant_resource.visibility_changed"), nil
		})
}

func tenantResourceMutationResult(view *service.TenantResourceView, eventType string) *tenantMutationResult {
	return &tenantMutationResult{
		Data: view, AggregateType: "tenant_resource", AggregateID: view.ResourceID,
		AggregateRevision: view.Revision, RowVersion: view.RowVersion, EventType: eventType,
		ResourceID: view.ResourceID, VisibilityState: string(view.VisibilityState), Status: string(view.AvailabilityState),
		TargetName: view.DisplayName,
	}
}

func tenantResourceListInput(c *gin.Context, candidates bool) (service.TenantResourceListInput, bool) {
	input := service.TenantResourceListInput{
		Type: strings.TrimSpace(c.Query("type")), Visibility: strings.TrimSpace(c.Query("visibility")),
		Availability: strings.TrimSpace(c.Query("availability")), Namespace: strings.TrimSpace(c.Query("namespace")),
		Query: strings.TrimSpace(c.Query("query")), Cursor: strings.TrimSpace(c.Query("cursor")), Limit: 20, Candidates: candidates,
	}
	for key := range c.Request.URL.Query() {
		switch key {
		case "type", "availability", "namespace", "query", "cursor", "limit":
		case "visibility":
			if candidates {
				codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "Candidate 查询参数无效")
				return input, false
			}
		default:
			codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "Tenant Resource 查询参数无效")
			return input, false
		}
	}
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "分页参数无效")
			return input, false
		}
		input.Limit = value
	}
	return input, true
}

func decodeTenantManagementRequest(c *gin.Context, target any) ([]byte, bool) {
	body, ok := readSimulationJSON(c)
	if !ok {
		return nil, false
	}
	if !decodeSimulationJSON(c, body, target) {
		return nil, false
	}
	return body, true
}

func decodeOptionalTenantManagementRequest(c *gin.Context, target any) ([]byte, bool) {
	body, ok := readSimulationJSON(c)
	if !ok {
		return nil, false
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		body = []byte("{}")
	}
	if !decodeSimulationJSON(c, body, target) {
		return nil, false
	}
	return body, true
}

func tenantResourceIDParam(c *gin.Context) string {
	if value := strings.TrimSpace(c.Param("resource_id")); value != "" {
		return value
	}
	return strings.TrimSpace(c.Param("id"))
}
