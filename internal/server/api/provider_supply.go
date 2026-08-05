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
	providerTechnicalResourceCreateRoute     = "/api/v1/management/provider/technical-resources"
	providerTechnicalResourceBindRoute       = "/api/v1/management/provider/technical-resources/:id/bind"
	providerTechnicalResourceUpdateRoute     = "/api/v1/management/provider/technical-resources/:id/update-tasks"
	providerTechnicalResourceCredentialRoute = "/api/v1/management/provider/technical-resources/:id/deployment-credentials"
	providerTechnicalResourceDeleteRoute     = "/api/v1/management/provider/technical-resources/:id"
	providerCandidateAcceptRoute             = "/api/v1/management/provider/supply-candidates/:id/accept"
	providerSupplyOutboxEventType            = "provider_supply.changed"
	providerSupplyOutboxConsumer             = "provider_supply_projection"

	ErrorCodeProviderSupplyConflict      = "PROVIDER_SUPPLY_CONFLICT"
	ErrorCodeProviderSupplyVersion       = "PROVIDER_SUPPLY_VERSION_CONFLICT"
	ErrorCodeProviderSupplyState         = "PROVIDER_SUPPLY_STATE_CONFLICT"
	ErrorCodeProviderScopeNotAllocatable = "RESOURCE_SCOPE_NOT_ALLOCATABLE"
	ErrorCodeProviderSupplyWriteFailed   = "PROVIDER_SUPPLY_WRITE_FAILED"
	ErrorCodeProviderSupplyQueryFailed   = "PROVIDER_SUPPLY_QUERY_FAILED"
	ErrorCodeProviderSupplyOutboxFailed  = "PROVIDER_SUPPLY_OUTBOX_FAILED"
	ErrorCodeProviderSupplyAuditFailed   = "PROVIDER_SUPPLY_AUDIT_FAILED"
)

var providerSupplyIdempotencyPolicies = map[string]service.JSONFieldPolicy{
	http.MethodPost + " " + providerTechnicalResourceCreateRoute:   service.NewJSONFieldPolicy("success", "data"),
	http.MethodPost + " " + providerTechnicalResourceBindRoute:     service.NewJSONFieldPolicy("success", "data"),
	http.MethodPost + " " + providerTechnicalResourceUpdateRoute:   service.NewJSONFieldPolicy("success", "data"),
	http.MethodDelete + " " + providerTechnicalResourceDeleteRoute: service.NewJSONFieldPolicy("success", "data"),
	http.MethodPost + " " + providerCandidateAcceptRoute:           service.NewJSONFieldPolicy("success", "data"),
}

var providerSupplyOutboxPolicies = map[string]service.JSONFieldPolicy{
	providerSupplyOutboxEventType: service.NewJSONFieldPolicy("provider_id", "aggregate_type", "aggregate_id", "row_version", "action"),
}

type ProviderSupplyAPI struct{}

func NewProviderSupplyAPI() *ProviderSupplyAPI { return &ProviderSupplyAPI{} }

type providerTechnicalResourceCreateRequest struct {
	Type               model.TechnicalResourceType `json:"type"`
	StableKey          string                      `json:"stable_key"`
	ParentID           string                      `json:"parent_id"`
	CredentialRevision int64                       `json:"credential_revision"`
	RuntimeName        string                      `json:"runtime_name"`
	DomainLabel        string                      `json:"domain_label"`
	Reason             string                      `json:"reason"`
}

type providerDeploymentCredentialRequest struct {
	Name       string `json:"name"`
	TTLMinutes int    `json:"ttl_minutes"`
}

type providerTechnicalResourceBindRequest struct {
	SourceType model.TechnicalResourceBindingSourceType `json:"source_type"`
	SourceID   string                                   `json:"source_id"`
	Reason     string                                   `json:"reason"`
}

type providerReasonRequest struct {
	Reason string `json:"reason"`
}

type providerTechnicalResourceUpdateRequest struct {
	ReleaseID string `json:"release_id"`
	Force     bool   `json:"force"`
	Reason    string `json:"reason"`
}

type providerAgentDomainLabelRequest struct {
	DomainLabel string `json:"domain_label"`
	Reason      string `json:"reason"`
}

type providerAgentHostDomainLabelRequest struct {
	HostDomainLabel string `json:"host_domain_label"`
}

type providerCandidateAcceptRequest struct {
	DisplayName string `json:"display_name"`
	Reason      string `json:"reason"`
}

type providerIdempotentMutation func(*service.ProviderSupplyService) (data any, aggregateType, aggregateID string, rowVersion int64, err error)
type providerMutation func(*service.ProviderSupplyService) (data any, aggregateType, aggregateID string, rowVersion int64, err error)

func (a *ProviderSupplyAPI) ListTechnicalResources(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return
	}
	input, ok := providerSupplyListInput(c)
	if !ok {
		return
	}
	result, err := service.NewProviderSupplyService(db.DB).ListTechnicalResources(c.Request.Context(), authorization, input)
	if err != nil {
		writeProviderSupplyError(c, err, false)
		return
	}
	c.JSON(http.StatusOK, NewPagedResponse(result.Items, result.Total, input.Page, input.PageSize))
}

func (a *ProviderSupplyAPI) GetTechnicalResource(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return
	}
	result, err := service.NewProviderSupplyService(db.DB).GetTechnicalResource(c.Request.Context(), authorization, c.Param("id"))
	if err != nil {
		writeProviderSupplyError(c, err, false)
		return
	}
	SetRevisionETag(c, result.Resource.RowVersion)
	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

func (a *ProviderSupplyAPI) CreateTechnicalResource(c *gin.Context) {
	authorization, body, request, ok := providerSupplyCreateRequest(c)
	if !ok {
		return
	}
	executeProviderIdempotentMutation(c, authorization, body, http.StatusCreated, "create_technical_resource", request.Reason,
		func(supply *service.ProviderSupplyService) (any, string, string, int64, error) {
			resource, err := supply.CreateTechnicalResource(c.Request.Context(), authorization, service.CreateTechnicalResourceInput{
				Type: request.Type, StableKey: request.StableKey, ParentID: request.ParentID, CredentialRevision: request.CredentialRevision, RuntimeName: request.RuntimeName, DomainLabel: request.DomainLabel,
			})
			if err != nil {
				return nil, "", "", 0, err
			}
			return resource, "technical_resource", resource.ID, resource.RowVersion, nil
		})
}

func (a *ProviderSupplyAPI) UpdateAgentDomainLabel(c *gin.Context) {
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
	var request providerAgentDomainLabelRequest
	if _, ok := decodeProviderSupplyRequest(c, &request); !ok {
		return
	}
	request.DomainLabel = strings.TrimSpace(request.DomainLabel)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len(request.Reason) > 500 {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "变更原因无效")
		return
	}
	resource, err := service.NewProviderSupplyService(db.DB).ChangeAgentDomainLabel(c.Request.Context(), authorization, c.Param("id"), request.DomainLabel, rowVersion)
	if err != nil {
		writeProviderSupplyError(c, err, true)
		return
	}
	recordAuditLog(c.Request.Context(), c, model.ActionUpdateAgent, "technical_resource", resource.ID, resource.DomainLabel, gin.H{"domain_label": resource.DomainLabel, "reason": request.Reason})
	SetRevisionETag(c, resource.RowVersion)
	c.JSON(http.StatusOK, NewSuccessResponse(resource))
}

func (a *ProviderSupplyAPI) UpdateAgentHostDomainLabel(c *gin.Context) {
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
	var request providerAgentHostDomainLabelRequest
	if _, ok := decodeProviderSupplyRequest(c, &request); !ok {
		return
	}
	request.HostDomainLabel = strings.TrimSpace(request.HostDomainLabel)
	resource, err := service.NewProviderSupplyService(db.DB).ChangeAgentHostDomainLabel(c.Request.Context(), authorization, c.Param("id"), request.HostDomainLabel, rowVersion)
	if err != nil {
		writeProviderSupplyError(c, err, true)
		return
	}
	recordAuditLog(c.Request.Context(), c, model.ActionUpdateAgent, "technical_resource", resource.ID, request.HostDomainLabel, gin.H{"host_domain_label": request.HostDomainLabel})
	SetRevisionETag(c, resource.RowVersion)
	c.JSON(http.StatusOK, NewSuccessResponse(resource))
}

func (a *ProviderSupplyAPI) CreateDeploymentCredential(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return
	}
	var request providerDeploymentCredentialRequest
	_, ok = decodeProviderSupplyRequest(c, &request)
	if !ok {
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.TTLMinutes == 0 {
		request.TTLMinutes = 30
	}
	if request.Name == "" || request.TTLMinutes < 1 || request.TTLMinutes > 1440 {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "部署凭据参数无效")
		return
	}
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	serverAddr := scheme + "://" + c.Request.Host
	executeProviderMutationWithStatus(c, authorization, http.StatusCreated, "create_technical_resource_deployment_credential", "generate deployment credential",
		func(supply *service.ProviderSupplyService) (any, string, string, int64, error) {
			credential, err := supply.CreateTechnicalResourceDeploymentCredential(c.Request.Context(), authorization, c.Param("id"), request.Name, time.Duration(request.TTLMinutes)*time.Minute)
			if err != nil {
				return nil, "", "", 0, err
			}
			detail, err := supply.GetTechnicalResource(c.Request.Context(), authorization, c.Param("id"))
			if err != nil {
				return nil, "", "", 0, err
			}
			data := gin.H{"credential": credential, "install_command": fmt.Sprintf("curl -fsSL %s/api/v1/download/install_agent.sh | sudo bash -s -- --deploy -t %s -s %s", serverAddr, credential.Token, serverAddr)}
			return data, "technical_resource", c.Param("id"), detail.Resource.RowVersion, nil
		})
}

func (a *ProviderSupplyAPI) BindTechnicalResource(c *gin.Context) {
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
	var request providerTechnicalResourceBindRequest
	body, ok := decodeProviderSupplyRequest(c, &request)
	if !ok {
		return
	}
	request.SourceID = strings.TrimSpace(request.SourceID)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.SourceID == "" || request.Reason == "" || len(request.SourceID) > 100 || len(request.Reason) > 500 {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "绑定参数无效")
		return
	}
	executeProviderIdempotentMutation(c, authorization, body, http.StatusCreated, "bind_technical_resource", request.Reason,
		func(supply *service.ProviderSupplyService) (any, string, string, int64, error) {
			result, err := supply.BindTechnicalResource(c.Request.Context(), authorization, service.BindTechnicalResourceInput{
				TechnicalResourceID: c.Param("id"), SourceType: request.SourceType, SourceID: request.SourceID,
				ExpectedResourceVersion: rowVersion, Reason: request.Reason,
			})
			if err != nil {
				return nil, "", "", 0, err
			}
			data := gin.H{"technical_resource": result.TechnicalResource, "binding": result.Binding}
			return data, "technical_resource", result.TechnicalResource.ID, result.TechnicalResource.RowVersion, nil
		})
}

func (a *ProviderSupplyAPI) SetTechnicalResourceLifecycle(target model.TechnicalResourceLifecycleState, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authorization, rowVersion, request, ok := providerSupplyReasonAction(c)
		if !ok {
			return
		}
		executeProviderMutation(c, authorization, action, request.Reason,
			func(supply *service.ProviderSupplyService) (any, string, string, int64, error) {
				resource, err := supply.SetTechnicalResourceLifecycle(c.Request.Context(), authorization, service.SetTechnicalResourceLifecycleInput{
					TechnicalResourceID: c.Param("id"), TargetState: target, ExpectedRowVersion: rowVersion, Reason: request.Reason,
				})
				if err != nil {
					return nil, "", "", 0, err
				}
				return resource, "technical_resource", resource.ID, resource.RowVersion, nil
			})
	}
}

func (a *ProviderSupplyAPI) GetTechnicalResourceCapabilities(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return
	}
	result, err := service.NewProviderSupplyService(db.DB).GetTechnicalResourceCapabilities(c.Request.Context(), authorization, c.Param("id"))
	if err != nil {
		writeProviderSupplyError(c, err, false)
		return
	}
	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

func (a *ProviderSupplyAPI) UpdateTechnicalResourceCapabilities(c *gin.Context) {
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
	var request service.TechnicalResourceCapabilities
	if _, ok := decodeProviderSupplyRequest(c, &request); !ok {
		return
	}
	resource, err := service.NewProviderSupplyService(db.DB).UpdateTechnicalResourceCapabilities(c.Request.Context(), authorization, service.UpdateTechnicalResourceCapabilitiesInput{
		TechnicalResourceID: c.Param("id"), ExpectedRowVersion: rowVersion, Capabilities: request,
	})
	if err != nil {
		writeProviderSupplyError(c, err, true)
		return
	}
	SetRevisionETag(c, resource.RowVersion)
	c.JSON(http.StatusOK, NewSuccessResponse(resource))
}

func (a *ProviderSupplyAPI) ListTechnicalResourceReleases(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return
	}
	result, err := service.NewProviderSupplyService(db.DB).ListTechnicalResourceReleases(c.Request.Context(), authorization, c.Param("id"))
	if err != nil {
		writeProviderSupplyError(c, err, false)
		return
	}
	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

func (a *ProviderSupplyAPI) ListTechnicalResourceUpdateTasks(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return
	}
	result, err := service.NewProviderSupplyService(db.DB).ListTechnicalResourceUpdateTasks(c.Request.Context(), authorization, c.Param("id"))
	if err != nil {
		writeProviderSupplyError(c, err, false)
		return
	}
	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

func (a *ProviderSupplyAPI) CreateTechnicalResourceUpdateTask(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return
	}
	var request providerTechnicalResourceUpdateRequest
	body, ok := decodeProviderSupplyRequest(c, &request)
	if !ok {
		return
	}
	request.ReleaseID, request.Reason = strings.TrimSpace(request.ReleaseID), strings.TrimSpace(request.Reason)
	if request.ReleaseID == "" || request.Reason == "" || len(request.ReleaseID) > 36 || len(request.Reason) > 500 {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "更新任务参数无效")
		return
	}
	executeProviderIdempotentMutation(c, authorization, body, http.StatusCreated, "create_technical_resource_update_task", request.Reason,
		func(supply *service.ProviderSupplyService) (any, string, string, int64, error) {
			task, err := supply.CreateTechnicalResourceUpdateTask(c.Request.Context(), authorization, c.Param("id"), request.ReleaseID, request.Force)
			if err != nil {
				return nil, "", "", 0, err
			}
			detail, err := supply.GetTechnicalResource(c.Request.Context(), authorization, c.Param("id"))
			if err != nil {
				return nil, "", "", 0, err
			}
			return task, "technical_resource", c.Param("id"), detail.Resource.RowVersion, nil
		})
}

func (a *ProviderSupplyAPI) CheckTechnicalResourceDelete(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return
	}
	result, err := service.NewProviderSupplyService(db.DB).CheckTechnicalResourceDelete(c.Request.Context(), authorization, c.Param("id"))
	if err != nil {
		writeProviderSupplyError(c, err, false)
		return
	}
	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

func (a *ProviderSupplyAPI) DeleteTechnicalResource(c *gin.Context) {
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
	var request providerReasonRequest
	body, ok := decodeProviderSupplyRequest(c, &request)
	if !ok {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len(request.Reason) > 500 {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "删除原因无效")
		return
	}
	executeProviderIdempotentMutation(c, authorization, body, http.StatusOK, "delete_technical_resource", request.Reason,
		func(supply *service.ProviderSupplyService) (any, string, string, int64, error) {
			resource, err := supply.DeleteTechnicalResource(c.Request.Context(), authorization, c.Param("id"), rowVersion, request.Reason)
			if err != nil {
				return nil, "", "", 0, err
			}
			return resource, "technical_resource", resource.ID, resource.RowVersion, nil
		})
}

func (a *ProviderSupplyAPI) ListSupplyCandidates(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return
	}
	input, ok := providerSupplyListInput(c)
	if !ok {
		return
	}
	result, err := service.NewProviderSupplyService(db.DB).ListSupplyCandidates(c.Request.Context(), authorization, input)
	if err != nil {
		writeProviderSupplyError(c, err, false)
		return
	}
	c.JSON(http.StatusOK, NewPagedResponse(result.Items, result.Total, input.Page, input.PageSize))
}

func (a *ProviderSupplyAPI) GetSupplyCandidate(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return
	}
	result, err := service.NewProviderSupplyService(db.DB).GetSupplyCandidate(c.Request.Context(), authorization, c.Param("id"))
	if err != nil {
		writeProviderSupplyError(c, err, false)
		return
	}
	SetRevisionETag(c, result.RowVersion)
	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

func (a *ProviderSupplyAPI) RejectSupplyCandidate(c *gin.Context) {
	authorization, rowVersion, request, ok := providerSupplyReasonAction(c)
	if !ok {
		return
	}
	executeProviderMutation(c, authorization, "reject_supply_candidate", request.Reason,
		func(supply *service.ProviderSupplyService) (any, string, string, int64, error) {
			candidate, err := supply.RejectSupplyCandidate(c.Request.Context(), authorization, service.RejectSupplyCandidateInput{
				CandidateID: c.Param("id"), ExpectedRowVersion: rowVersion, Reason: request.Reason,
			})
			if err != nil {
				return nil, "", "", 0, err
			}
			return candidate, "supply_candidate", candidate.ID, candidate.RowVersion, nil
		})
}

func (a *ProviderSupplyAPI) AcceptSupplyCandidate(c *gin.Context) {
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
	var request providerCandidateAcceptRequest
	body, ok := decodeProviderSupplyRequest(c, &request)
	if !ok {
		return
	}
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len(request.Reason) > 500 || len(request.DisplayName) > 200 {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "接受候选参数无效")
		return
	}
	executeProviderIdempotentMutation(c, authorization, body, http.StatusOK, "accept_supply_candidate", request.Reason,
		func(supply *service.ProviderSupplyService) (any, string, string, int64, error) {
			result, err := supply.AcceptSupplyCandidate(c.Request.Context(), authorization, service.AcceptSupplyCandidateInput{
				CandidateID: c.Param("id"), ExpectedRowVersion: rowVersion, DisplayName: request.DisplayName, Reason: request.Reason,
			})
			if err != nil {
				return nil, "", "", 0, err
			}
			data := gin.H{
				"candidate": result.Candidate, "resource": result.Resource, "source": result.Source,
				"cluster_scope": result.ClusterScope, "namespace_scopes": result.NamespaceScopes,
			}
			return data, "supply_candidate", result.Candidate.ID, result.Candidate.RowVersion, nil
		})
}

func (a *ProviderSupplyAPI) ListPlatformResources(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return
	}
	input, ok := providerSupplyListInput(c)
	if !ok {
		return
	}
	result, err := service.NewProviderSupplyService(db.DB).ListPlatformResources(c.Request.Context(), authorization, input)
	if err != nil {
		writeProviderSupplyError(c, err, false)
		return
	}
	c.JSON(http.StatusOK, NewPagedResponse(result.Items, result.Total, input.Page, input.PageSize))
}

func (a *ProviderSupplyAPI) GetPlatformResource(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return
	}
	result, err := service.NewProviderSupplyService(db.DB).GetPlatformResource(c.Request.Context(), authorization, c.Param("id"))
	if err != nil {
		writeProviderSupplyError(c, err, false)
		return
	}
	SetRevisionETag(c, result.Resource.RowVersion)
	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

func (a *ProviderSupplyAPI) SetPlatformResourceLifecycle(target model.PlatformResourceLifecycleState, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authorization, rowVersion, request, ok := providerSupplyReasonAction(c)
		if !ok {
			return
		}
		executeProviderMutation(c, authorization, action, request.Reason,
			func(supply *service.ProviderSupplyService) (any, string, string, int64, error) {
				resource, err := supply.SetPlatformResourceLifecycle(c.Request.Context(), authorization, service.SetPlatformResourceLifecycleInput{
					ResourceID: c.Param("id"), TargetState: target, ExpectedRowVersion: rowVersion, Reason: request.Reason,
				})
				if err != nil {
					return nil, "", "", 0, err
				}
				return resource, "platform_resource", resource.ID, resource.RowVersion, nil
			})
	}
}

func (a *ProviderSupplyAPI) ListResourceScopes(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return
	}
	input, ok := providerSupplyListInput(c)
	if !ok {
		return
	}
	result, err := service.NewProviderSupplyService(db.DB).ListResourceScopes(c.Request.Context(), authorization, service.ResourceScopeListInput{
		ProviderSupplyListInput: input, PlatformResourceID: c.Param("id"),
	})
	if err != nil {
		writeProviderSupplyError(c, err, false)
		return
	}
	c.JSON(http.StatusOK, NewPagedResponse(result.Items, result.Total, input.Page, input.PageSize))
}

func (a *ProviderSupplyAPI) GetResourceScope(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return
	}
	result, err := service.NewProviderSupplyService(db.DB).GetResourceScope(c.Request.Context(), authorization, c.Param("id"))
	if err != nil {
		writeProviderSupplyError(c, err, false)
		return
	}
	SetRevisionETag(c, result.Scope.RowVersion)
	c.JSON(http.StatusOK, NewSuccessResponse(result))
}

func (a *ProviderSupplyAPI) SetResourceScopeLifecycle(target model.ResourceScopeLifecycleState, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authorization, rowVersion, request, ok := providerSupplyReasonAction(c)
		if !ok {
			return
		}
		executeProviderMutation(c, authorization, action, request.Reason,
			func(supply *service.ProviderSupplyService) (any, string, string, int64, error) {
				scope, err := supply.SetResourceScopeLifecycle(c.Request.Context(), authorization, service.SetResourceScopeLifecycleInput{
					ScopeID: c.Param("id"), TargetState: target, ExpectedRowVersion: rowVersion, Reason: request.Reason,
				})
				if err != nil {
					return nil, "", "", 0, err
				}
				return scope, "resource_scope", scope.ID, scope.RowVersion, nil
			})
	}
}

func (a *ProviderSupplyAPI) MarkResourceScopeAllocatable(c *gin.Context) {
	authorization, rowVersion, request, ok := providerSupplyReasonAction(c)
	if !ok {
		return
	}
	executeProviderMutation(c, authorization, "mark_resource_scope_allocatable", request.Reason,
		func(supply *service.ProviderSupplyService) (any, string, string, int64, error) {
			result, err := supply.MarkResourceScopeAllocatable(c.Request.Context(), authorization, service.MarkResourceScopeAllocatableInput{
				ScopeID: c.Param("id"), ExpectedRowVersion: rowVersion, Reason: request.Reason,
			})
			if err != nil {
				return nil, "", "", 0, err
			}
			data := gin.H{"scope": result.Scope, "resource": result.Resource}
			return data, "resource_scope", result.Scope.ID, result.Scope.RowVersion, nil
		})
}

func providerSupplyCreateRequest(c *gin.Context) (*service.ManagementAuthorizationContext, []byte, providerTechnicalResourceCreateRequest, bool) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return nil, nil, providerTechnicalResourceCreateRequest{}, false
	}
	var request providerTechnicalResourceCreateRequest
	body, ok := decodeProviderSupplyRequest(c, &request)
	if !ok {
		return nil, nil, request, false
	}
	request.StableKey = strings.TrimSpace(request.StableKey)
	request.ParentID = strings.TrimSpace(request.ParentID)
	request.DomainLabel = strings.TrimSpace(request.DomainLabel)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len(request.StableKey) > 128 || len(request.ParentID) > 36 ||
		len(request.Reason) > 500 || request.CredentialRevision < 0 ||
		(request.Type != model.TechnicalResourceAgent && request.Type != model.TechnicalResourceEndpoint) ||
		(request.Type == model.TechnicalResourceAgent && request.ParentID != "") ||
		(request.Type == model.TechnicalResourceEndpoint && request.ParentID == "") {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "技术资源参数无效")
		return nil, nil, request, false
	}
	if (request.Type == model.TechnicalResourceAgent && request.DomainLabel == "") ||
		(request.Type == model.TechnicalResourceEndpoint && request.DomainLabel != "") {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "Agent 域名标识无效")
		return nil, nil, request, false
	}
	if request.CredentialRevision == 0 {
		request.CredentialRevision = 1
	}
	return authorization, body, request, true
}

func providerSupplyReasonAction(c *gin.Context) (*service.ManagementAuthorizationContext, int64, providerReasonRequest, bool) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return nil, 0, providerReasonRequest{}, false
	}
	rowVersion, ok := requiredRevision(c)
	if !ok {
		codedError(c, http.StatusPreconditionRequired, ErrorCodePreconditionRequired, "必须提供 If-Match revision")
		return nil, 0, providerReasonRequest{}, false
	}
	var request providerReasonRequest
	if _, ok := decodeProviderSupplyRequest(c, &request); !ok {
		return nil, 0, request, false
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len(request.Reason) > 500 {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "操作原因无效")
		return nil, 0, request, false
	}
	return authorization, rowVersion, request, true
}

func providerSupplyListInput(c *gin.Context) (service.ProviderSupplyListInput, bool) {
	input := service.ProviderSupplyListInput{
		Search: strings.TrimSpace(c.Query("search")), Type: strings.TrimSpace(c.Query("type")), State: strings.TrimSpace(c.Query("state")),
		Page: 1, PageSize: 20,
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
	if input.Page < 1 || input.PageSize < 1 || input.PageSize > 100 || len(input.Search) > 200 {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "查询参数无效")
		return input, false
	}
	return input, true
}

func decodeProviderSupplyRequest(c *gin.Context, target any) ([]byte, bool) {
	body, ok := readSimulationJSON(c)
	if !ok {
		return nil, false
	}
	if !decodeSimulationJSON(c, body, target) {
		return nil, false
	}
	return body, true
}

func executeProviderIdempotentMutation(c *gin.Context, authorization *service.ManagementAuthorizationContext, body []byte, status int, action, reason string, mutation providerIdempotentMutation) {
	idempotencyBody, err := providerIdempotencyRequestBody(c, body)
	if err != nil {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "幂等请求参数无效")
		return
	}
	idempotency := service.NewAPIIdempotencyService(db.DB, providerSupplyIdempotencyPolicies, 5*time.Minute, 24*time.Hour)
	begin, err := idempotency.Begin(c.Request.Context(), service.BeginIdempotencyInput{
		ActorType: "management_user", ActorID: providerIdempotencyActorID(authorization), ScopeType: string(model.ManagementScopeProvider),
		ScopeID: authorization.ScopeID, Method: c.Request.Method, Route: c.FullPath(),
		Key: singleSafeHeader(c, HeaderIdempotencyKey, 128), Body: idempotencyBody,
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
	var rowVersion int64
	err = db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		data, aggregateType, aggregateID, revision, mutationErr := mutation(service.NewProviderSupplyService(tx))
		if mutationErr != nil {
			return mutationErr
		}
		rowVersion = revision
		var marshalErr error
		responseBody, marshalErr = json.Marshal(NewSuccessResponse(gin.H{"result": data, "row_version": revision}))
		if marshalErr != nil {
			return marshalErr
		}
		if err := appendProviderSupplyOutbox(tx, c, authorization, action, aggregateType, aggregateID, revision); err != nil {
			return fmt.Errorf("%w: %v", errProviderSupplyOutbox, err)
		}
		if err := recordProviderSupplyAudit(tx, c, action, aggregateType, aggregateID, reason, revision); err != nil {
			return fmt.Errorf("%w: %v", errProviderSupplyAudit, err)
		}
		_, completeErr := idempotency.Complete(tx, service.CompleteIdempotencyInput{
			RecordID: begin.Record.ID, RequestHash: begin.Record.RequestHash, Status: model.APIIdempotencyCompleted,
			ResponseStatus: status, ResponseBody: responseBody,
		})
		return completeErr
	})
	if err != nil {
		writeProviderSupplyError(c, err, true)
		return
	}
	SetRevisionETag(c, rowVersion)
	c.Data(status, "application/json; charset=utf-8", responseBody)
}

func executeProviderMutation(c *gin.Context, authorization *service.ManagementAuthorizationContext, action, reason string, mutation providerMutation) {
	executeProviderMutationWithStatus(c, authorization, http.StatusOK, action, reason, mutation)
}

func executeProviderMutationWithStatus(c *gin.Context, authorization *service.ManagementAuthorizationContext, status int, action, reason string, mutation providerMutation) {
	var data any
	var rowVersion int64
	err := db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var aggregateType, aggregateID string
		var mutationErr error
		data, aggregateType, aggregateID, rowVersion, mutationErr = mutation(service.NewProviderSupplyService(tx))
		if mutationErr != nil {
			return mutationErr
		}
		if err := appendProviderSupplyOutbox(tx, c, authorization, action, aggregateType, aggregateID, rowVersion); err != nil {
			return fmt.Errorf("%w: %v", errProviderSupplyOutbox, err)
		}
		if err := recordProviderSupplyAudit(tx, c, action, aggregateType, aggregateID, reason, rowVersion); err != nil {
			return fmt.Errorf("%w: %v", errProviderSupplyAudit, err)
		}
		return nil
	})
	if err != nil {
		writeProviderSupplyError(c, err, true)
		return
	}
	SetRevisionETag(c, rowVersion)
	c.JSON(status, NewSuccessResponse(data))
}

var (
	errProviderSupplyOutbox = errors.New("Provider supply outbox write failed")
	errProviderSupplyAudit  = errors.New("Provider supply audit write failed")
)

func appendProviderSupplyOutbox(tx *gorm.DB, c *gin.Context, authorization *service.ManagementAuthorizationContext, action, aggregateType, aggregateID string, rowVersion int64) error {
	payload, err := json.Marshal(gin.H{
		"provider_id": authorization.ScopeID, "aggregate_type": aggregateType, "aggregate_id": aggregateID,
		"row_version": rowVersion, "action": action,
	})
	if err != nil {
		return err
	}
	outbox := service.NewResourceOutboxService(db.DB, providerSupplyOutboxPolicies)
	_, err = outbox.Append(tx, service.AppendOutboxEventInput{
		Consumer: providerSupplyOutboxConsumer, EventType: providerSupplyOutboxEventType,
		AggregateType: aggregateType, AggregateID: aggregateID, AggregateRevision: rowVersion,
		EventKey: fmt.Sprintf("%s:%s:%d", aggregateType, aggregateID, rowVersion), Payload: payload, RequestID: requestID(c),
	})
	return err
}

func recordProviderSupplyAudit(tx *gorm.DB, c *gin.Context, action, aggregateType, aggregateID, reason string, rowVersion int64) error {
	return recordAuditLogStrictWithDB(c.Request.Context(), tx, c, action, aggregateType, aggregateID, aggregateID, gin.H{
		"reason": reason, "row_version": rowVersion,
	})
}

func providerIdempotencyActorID(authorization *service.ManagementAuthorizationContext) string {
	value := fmt.Sprintf("%d:%d", authorization.ActorUserID, authorization.EffectiveUserID)
	if authorization.SimulationSessionID != "" {
		value += ":" + authorization.SimulationSessionID
	}
	return value
}

func providerIdempotencyRequestBody(c *gin.Context, body []byte) ([]byte, error) {
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil || value == nil {
		return nil, fmt.Errorf("invalid Provider idempotency request")
	}
	value["_request_path"] = c.Request.URL.Path
	if revision, ok := requiredRevision(c); ok {
		value["_if_match_revision"] = revision
	}
	return json.Marshal(value)
}

func setProviderReplayETag(c *gin.Context, responseBody string) {
	var response struct {
		Data struct {
			RowVersion int64 `json:"row_version"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(responseBody), &response) == nil && response.Data.RowVersion > 0 {
		SetRevisionETag(c, response.Data.RowVersion)
	}
}

func writeProviderSupplyError(c *gin.Context, err error, write bool) {
	switch {
	case errors.Is(err, service.ErrProviderSupplyInvalidInput), errors.Is(err, service.ErrHostDomainLabelInvalid):
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "Provider 资源请求参数无效")
	case errors.Is(err, service.ErrProviderSupplyObjectNotFound):
		codedError(c, http.StatusNotFound, ErrorCodeManagementObjectMissing, "当前 Provider 内对象不存在或不可见")
	case errors.Is(err, service.ErrManagementPermissionDenied):
		codedError(c, http.StatusForbidden, ErrorCodeManagementPermission, "当前 Provider 权限已失效")
	case errors.Is(err, service.ErrProviderSupplyVersionConflict):
		codedError(c, http.StatusConflict, ErrorCodeProviderSupplyVersion, "对象版本已变化")
	case errors.Is(err, service.ErrResourceScopeNotAllocatable):
		codedError(c, http.StatusConflict, ErrorCodeProviderScopeNotAllocatable, "Scope 当前不满足可分配条件")
	case errors.Is(err, service.ErrProviderSupplyConflict), errors.Is(err, service.ErrTechnicalResourceUnbound), errors.Is(err, service.ErrHostDomainLabelExists):
		codedError(c, http.StatusConflict, ErrorCodeProviderSupplyConflict, "Provider 资源存在冲突或缺少前置条件")
	case errors.Is(err, service.ErrActiveTaskExists), errors.Is(err, service.ErrReleaseNotPublished),
		errors.Is(err, service.ErrArtifactNotFound), errors.Is(err, service.ErrUpdaterUnsupported):
		codedError(c, http.StatusConflict, ErrorCodeProviderSupplyConflict, "当前资源不满足更新前置条件")
	case errors.Is(err, service.ErrTechnicalResourceDeleteBlocked):
		codedError(c, http.StatusConflict, ErrorCodeProviderSupplyConflict, "资源仍有业务依赖，请重新执行删除检查")
	case errors.Is(err, service.ErrTechnicalResourceStateTransition), errors.Is(err, service.ErrPlatformResourceStateTransition),
		errors.Is(err, service.ErrResourceScopeStateTransition):
		codedError(c, http.StatusConflict, ErrorCodeProviderSupplyState, "对象状态转换无效")
	case errors.Is(err, errProviderSupplyOutbox):
		codedError(c, http.StatusServiceUnavailable, ErrorCodeProviderSupplyOutboxFailed, "Provider 资源事件写入失败")
	case errors.Is(err, errProviderSupplyAudit):
		codedError(c, http.StatusServiceUnavailable, ErrorCodeProviderSupplyAuditFailed, "Provider 资源审计写入失败")
	default:
		code := ErrorCodeProviderSupplyQueryFailed
		message := "查询 Provider 资源失败"
		if write {
			code = ErrorCodeProviderSupplyWriteFailed
			message = "写入 Provider 资源失败"
		}
		codedError(c, http.StatusInternalServerError, code, message)
	}
}
