package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type TechnicalResourceDeploymentCredential struct {
	ID                  string    `json:"id"`
	TechnicalResourceID string    `json:"technical_resource_id"`
	Token               string    `json:"token"`
	ExpiresAt           time.Time `json:"expires_at"`
}

func (s *ProviderSupplyService) CreateTechnicalResourceDeploymentCredential(ctx context.Context, authorization *ManagementAuthorizationContext, resourceID, name string, ttl time.Duration) (*TechnicalResourceDeploymentCredential, error) {
	if ttl <= 0 || ttl > 24*time.Hour {
		return nil, ErrProviderSupplyInvalidInput
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return nil, ErrProviderSupplyInvalidInput
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	rawToken := "tr_" + hex.EncodeToString(random)
	now, expiresAt := s.now().UTC(), s.now().UTC().Add(ttl)
	result := &TechnicalResourceDeploymentCredential{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		providerID, err := reauthorizeProviderPermission(tx, authorization, PermissionProviderTechnicalResourcesWrite, now)
		if err != nil {
			return err
		}
		var resource model.TechnicalResource
		if err := tx.Where("id = ? AND provider_id = ? AND lifecycle_state IN ?", strings.TrimSpace(resourceID), providerID,
			[]model.TechnicalResourceLifecycleState{model.TechnicalResourcePending, model.TechnicalResourceRegistered}).First(&resource).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProviderSupplyObjectNotFound
			}
			return err
		}
		if resource.Type != model.TechnicalResourceAgent || resource.RuntimeUserID == 0 {
			return ErrProviderSupplyConflict
		}
		if err := tx.Model(&model.TechnicalResourceDeployToken{}).
			Where("technical_resource_id = ? AND status = ?", resource.ID, model.TechnicalResourceDeployTokenPending).
			Updates(map[string]any{"status": model.TechnicalResourceDeployTokenRevoked, "revoked_at": now}).Error; err != nil {
			return err
		}
		token := model.TechnicalResourceDeployToken{
			ID: uuid.NewString(), TechnicalResourceID: resource.ID, Token: rawToken, Name: name,
			RuntimeUserID: resource.RuntimeUserID, Status: model.TechnicalResourceDeployTokenPending,
			ExpiresAt: &expiresAt, CreatedByUserID: authorization.EffectiveUserID,
		}
		if err := tx.Create(&token).Error; err != nil {
			return err
		}
		updated := tx.Model(&model.TechnicalResource{}).
			Where("id = ? AND provider_id = ? AND row_version = ?", resource.ID, providerID, resource.RowVersion).
			Update("row_version", gorm.Expr("row_version + 1"))
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrProviderSupplyVersionConflict
		}
		result = &TechnicalResourceDeploymentCredential{ID: token.ID, TechnicalResourceID: resource.ID, Token: rawToken, ExpiresAt: expiresAt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type TechnicalResourceCapabilities struct {
	SSHEnabled            bool     `json:"ssh_enabled"`
	SSHUsers              []string `json:"ssh_users,omitempty"`
	K8SEnabled            bool     `json:"k8s_enabled"`
	K8SAPIAddress         string   `json:"k8s_api_address,omitempty"`
	SVCEnabled            bool     `json:"svc_enabled"`
	SVCLabelSelector      string   `json:"svc_label_selector,omitempty"`
	SVCNamespaces         []string `json:"svc_namespaces,omitempty"`
	EndpointAccessEnabled bool     `json:"endpoint_access_enabled"`
	K8SListenPort         *int     `json:"k8s_listen_port,omitempty"`
	SVCListenPortBase     *int     `json:"svc_listen_port_base,omitempty"`
	EndpointListenPort    *int     `json:"endpoint_listen_port,omitempty"`
}

type UpdateTechnicalResourceCapabilitiesInput struct {
	TechnicalResourceID string
	ExpectedRowVersion  int64
	Capabilities        TechnicalResourceCapabilities
}

type TechnicalResourceDeleteBlocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Count   int64  `json:"count"`
}

type TechnicalResourceDeleteCheck struct {
	Allowed  bool                             `json:"allowed"`
	Blockers []TechnicalResourceDeleteBlocker `json:"blockers"`
}

var ErrTechnicalResourceDeleteBlocked = errors.New("TECHNICAL_RESOURCE_DELETE_BLOCKED")

func (s *ProviderSupplyService) DeleteTechnicalResource(ctx context.Context, authorization *ManagementAuthorizationContext, resourceID string, expectedRowVersion int64, reason string) (*model.TechnicalResource, error) {
	resourceID, reason = strings.TrimSpace(resourceID), strings.TrimSpace(reason)
	if s == nil || s.db == nil || resourceID == "" || expectedRowVersion <= 0 || reason == "" || len(reason) > 500 {
		return nil, ErrProviderSupplyInvalidInput
	}
	now := s.now().UTC()
	var resource model.TechnicalResource
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		providerID, err := reauthorizeProviderPermission(tx, authorization, PermissionProviderTechnicalResourcesWrite, now)
		if err != nil {
			return err
		}
		if err := tx.Where("provider_id = ? AND id = ?", providerID, resourceID).First(&resource).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProviderSupplyObjectNotFound
			}
			return err
		}
		if resource.RowVersion != expectedRowVersion {
			return ErrProviderSupplyVersionConflict
		}
		check, err := checkTechnicalResourceDelete(tx, providerID, &resource)
		if err != nil {
			return err
		}
		if !check.Allowed {
			return ErrTechnicalResourceDeleteBlocked
		}
		if err := tx.Model(&model.TechnicalResourceDeployToken{}).
			Where("technical_resource_id = ? AND status = ?", resource.ID, model.TechnicalResourceDeployTokenPending).
			Updates(map[string]any{"status": model.TechnicalResourceDeployTokenRevoked, "revoked_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.TechnicalResourceBinding{}).Where("technical_resource_id = ? AND enabled = ?", resource.ID, true).
			Updates(map[string]any{"enabled": false, "row_version": gorm.Expr("row_version + 1")}).Error; err != nil {
			return err
		}
		var bindings []model.TechnicalResourceBinding
		if err := tx.Where("technical_resource_id = ?", resource.ID).Find(&bindings).Error; err != nil {
			return err
		}
		domains := NewDomainService(tx)
		for _, binding := range bindings {
			switch binding.SourceType {
			case model.TechnicalResourceBindingLegacyNode:
				nodeID, parseErr := strconv.ParseUint(binding.SourceID, 10, 64)
				if parseErr != nil {
					return ErrProviderSupplyConflict
				}
				if err := domains.DeleteNodeAllDomains(ctx, nodeID); err != nil {
					return err
				}
			case model.TechnicalResourceBindingLegacyEndpoint:
				if err := domains.DeleteEndpointAllDomains(ctx, binding.SourceID); err != nil {
					return err
				}
			}
		}
		if resource.Type == model.TechnicalResourceAgent {
			if err := tx.Where("provider_id = ? AND agent_resource_id = ?", providerID, resource.ID).Delete(&model.DomainRegistry{}).Error; err != nil {
				return err
			}
		}
		updated := tx.Model(&model.TechnicalResource{}).
			Where("provider_id = ? AND id = ? AND row_version = ?", providerID, resource.ID, resource.RowVersion).
			Where("lifecycle_state IN ? OR health_state = ?", []model.TechnicalResourceLifecycleState{model.TechnicalResourcePending, model.TechnicalResourceRetired}, model.ResourceHealthOffline).
			Updates(map[string]any{
				"health_state": model.ResourceHealthOffline, "lease_expires_at": nil, "deleted_at": now,
				"credential_revision": gorm.Expr("credential_revision + 1"),
				"config_revision":     gorm.Expr("config_revision + 1"), "row_version": gorm.Expr("row_version + 1"),
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrProviderSupplyVersionConflict
		}
		if err := tx.First(&resource, "provider_id = ? AND id = ?", providerID, resource.ID).Error; err != nil {
			return err
		}
		resource.LifecycleState = model.TechnicalResourceDeleted
		return nil
	})
	return &resource, err
}

func (s *ProviderSupplyService) activeTechnicalResourceBinding(ctx context.Context, authorization *ManagementAuthorizationContext, resourceID, permission string) (*model.TechnicalResource, *model.TechnicalResourceBinding, error) {
	providerID, err := reauthorizeProviderPermission(s.db.WithContext(ctx), authorization, permission, s.now().UTC())
	if err != nil {
		return nil, nil, err
	}
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return nil, nil, ErrProviderSupplyInvalidInput
	}
	var resource model.TechnicalResource
	if err := s.db.WithContext(ctx).Where("id = ? AND provider_id = ?", resourceID, providerID).First(&resource).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrProviderSupplyObjectNotFound
		}
		return nil, nil, err
	}
	var binding model.TechnicalResourceBinding
	if err := s.db.WithContext(ctx).Where("technical_resource_id = ? AND enabled = ?", resource.ID, true).First(&binding).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrTechnicalResourceUnbound
		}
		return nil, nil, err
	}
	return &resource, &binding, nil
}

func (s *ProviderSupplyService) GetTechnicalResourceCapabilities(ctx context.Context, authorization *ManagementAuthorizationContext, resourceID string) (*TechnicalResourceCapabilities, error) {
	resource, binding, err := s.activeTechnicalResourceBinding(ctx, authorization, resourceID, PermissionProviderTechnicalResourcesRead)
	if err != nil {
		return nil, err
	}
	result := &TechnicalResourceCapabilities{}
	switch binding.SourceType {
	case model.TechnicalResourceBindingLegacyNode:
		if resource.Type != model.TechnicalResourceAgent {
			return nil, ErrProviderSupplyConflict
		}
		id, err := strconv.ParseUint(binding.SourceID, 10, 64)
		if err != nil {
			return nil, ErrProviderSupplyConflict
		}
		var node model.Node
		if err := s.db.WithContext(ctx).Preload("User").First(&node, id).Error; err != nil {
			return nil, ErrProviderSupplyObjectNotFound
		}
		result.SSHEnabled = node.User != nil && node.User.SSHEnabled
		result.K8SEnabled = node.K8SEnabled != nil && *node.K8SEnabled
		result.K8SAPIAddress = node.K8SApiServer
		result.SVCEnabled = node.SVCEnabled != nil && *node.SVCEnabled
		result.SVCLabelSelector = node.SVCLabelSelector
		result.SVCNamespaces = decodeStringList(node.SVCNamespaces)
		result.EndpointAccessEnabled = node.EndpointEnabled != nil && *node.EndpointEnabled
		result.K8SListenPort = node.K8SListenPort
		result.SVCListenPortBase = node.SVCListenPortBase
		result.EndpointListenPort = node.EndpointListenPort
	case model.TechnicalResourceBindingLegacyEndpoint:
		if resource.Type != model.TechnicalResourceEndpoint {
			return nil, ErrProviderSupplyConflict
		}
		var endpoint model.Endpoint
		if err := s.db.WithContext(ctx).First(&endpoint, "id = ?", binding.SourceID).Error; err != nil {
			return nil, ErrProviderSupplyObjectNotFound
		}
		result.SSHEnabled = endpoint.SSHEnabled
		result.SSHUsers = decodeStringList(endpoint.SSHUsers)
		result.K8SEnabled = endpoint.K8SAPIEnabled
		result.K8SAPIAddress = endpoint.K8SAPIApiServer
		result.SVCEnabled = endpoint.K8SServiceEnabled
		result.SVCLabelSelector = endpoint.K8SServiceLabelSelector
		result.SVCNamespaces = decodeStringList(endpoint.K8SServiceNamespaces)
	default:
		return nil, ErrProviderSupplyConflict
	}
	return result, nil
}

func (s *ProviderSupplyService) UpdateTechnicalResourceCapabilities(ctx context.Context, authorization *ManagementAuthorizationContext, input UpdateTechnicalResourceCapabilitiesInput) (*model.TechnicalResource, error) {
	resource, binding, err := s.activeTechnicalResourceBinding(ctx, authorization, input.TechnicalResourceID, PermissionProviderTechnicalResourcesWrite)
	if err != nil {
		return nil, err
	}
	if input.ExpectedRowVersion <= 0 || resource.RowVersion != input.ExpectedRowVersion || resource.LifecycleState == model.TechnicalResourceRetired {
		if resource.RowVersion != input.ExpectedRowVersion {
			return nil, ErrProviderSupplyVersionConflict
		}
		return nil, ErrTechnicalResourceStateTransition
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		capability := input.Capabilities
		bindings := []model.TechnicalResourceBinding{*binding}
		if resource.Type == model.TechnicalResourceAgent {
			if err := tx.Where("technical_resource_id = ? AND enabled = ?", resource.ID, true).
				Order("created_at ASC, id ASC").Find(&bindings).Error; err != nil {
				return err
			}
		}
		for i := range bindings {
			binding = &bindings[i]
			switch binding.SourceType {
			case model.TechnicalResourceBindingLegacyNode:
				id, parseErr := strconv.ParseUint(binding.SourceID, 10, 64)
				if parseErr != nil {
					return ErrProviderSupplyConflict
				}
				var node model.Node
				if err := tx.First(&node, id).Error; err != nil {
					return ErrProviderSupplyObjectNotFound
				}
				if err := tx.Model(&model.User{}).Where("id = ?", node.UserID).Update("ssh_enabled", capability.SSHEnabled).Error; err != nil {
					return err
				}
				updates := map[string]any{
					"k8s_enabled": capability.K8SEnabled, "k8s_api_server": strings.TrimSpace(capability.K8SAPIAddress),
					"svc_enabled": capability.SVCEnabled, "svc_label_selector": strings.TrimSpace(capability.SVCLabelSelector),
					"svc_namespaces": encodeStringList(capability.SVCNamespaces), "endpoint_enabled": capability.EndpointAccessEnabled,
				}
				if capability.K8SListenPort != nil {
					updates["k8s_listen_port"] = *capability.K8SListenPort
				}
				if capability.SVCListenPortBase != nil {
					updates["svc_listen_port_base"] = *capability.SVCListenPortBase
				}
				if capability.EndpointListenPort != nil {
					updates["endpoint_listen_port"] = *capability.EndpointListenPort
				}
				if err := tx.Model(&node).Updates(updates).Error; err != nil {
					return err
				}
				var user model.User
				if err := tx.First(&user, node.UserID).Error; err != nil {
					return err
				}
				if err := tx.First(&node, node.ID).Error; err != nil {
					return err
				}
				domains := NewDomainService(tx)
				if capability.SSHEnabled {
					if err := domains.CreateNodeSSHDomain(ctx, &node, &user); err != nil {
						return err
					}
				} else if err := domains.DeleteNodeSSHDomain(ctx, &node, &user); err != nil {
					return err
				}
				if capability.K8SEnabled {
					if err := domains.CreateNodeK8SAPIDomain(ctx, &node, &user); err != nil {
						return err
					}
				} else if err := domains.DeleteNodeK8SAPIDomain(ctx, &node, &user); err != nil {
					return err
				}
			case model.TechnicalResourceBindingLegacyEndpoint:
				updates := map[string]any{
					"ssh_enabled": capability.SSHEnabled, "ssh_users": encodeStringList(capability.SSHUsers),
					"k8sapi_enabled": capability.K8SEnabled, "k8sapi_api_server": strings.TrimSpace(capability.K8SAPIAddress),
					"k8sservice_enabled": capability.SVCEnabled, "k8sservice_label_selector": strings.TrimSpace(capability.SVCLabelSelector),
					"k8sservice_namespaces": encodeStringList(capability.SVCNamespaces),
				}
				if err := tx.Model(&model.Endpoint{}).Where("id = ?", binding.SourceID).Updates(updates).Error; err != nil {
					return err
				}
				var endpoint model.Endpoint
				if err := tx.First(&endpoint, "id = ?", binding.SourceID).Error; err != nil {
					return err
				}
				var agent model.Node
				if err := tx.Where("user_id = ? AND type = ?", endpoint.UserID, model.NodeTypeAgent).First(&agent).Error; err != nil {
					return err
				}
				var user model.User
				if err := tx.First(&user, endpoint.UserID).Error; err != nil {
					return err
				}
				domains := NewDomainService(tx)
				if capability.SSHEnabled {
					if err := domains.CreateEndpointSSHDomain(ctx, &endpoint, &agent, &user); err != nil {
						return err
					}
				} else if err := domains.DeleteEndpointSSHDomain(ctx, endpoint.ID, &user); err != nil {
					return err
				}
				if capability.K8SEnabled {
					if err := domains.CreateEndpointK8SAPIDomain(ctx, &endpoint, &agent, &user); err != nil {
						return err
					}
				} else if err := domains.DeleteEndpointK8SAPIDomain(ctx, endpoint.ID, &user); err != nil {
					return err
				}
			default:
				return ErrProviderSupplyConflict
			}
		}
		result := tx.Model(&model.TechnicalResource{}).Where("id = ? AND provider_id = ? AND row_version = ?", resource.ID, authorization.ScopeID, input.ExpectedRowVersion).
			Updates(map[string]any{"config_revision": gorm.Expr("config_revision + 1"), "row_version": gorm.Expr("row_version + 1")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrProviderSupplyVersionConflict
		}
		return tx.First(resource, "id = ?", resource.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return resource, nil
}

func (s *ProviderSupplyService) ListTechnicalResourceReleases(ctx context.Context, authorization *ManagementAuthorizationContext, resourceID string) ([]model.Release, error) {
	resource, _, err := s.activeTechnicalResourceBinding(ctx, authorization, resourceID, PermissionProviderTechnicalResourcesRead)
	if err != nil {
		return nil, err
	}
	component := model.ComponentAgent
	if resource.Type == model.TechnicalResourceEndpoint {
		component = model.ComponentEndpoint
	}
	var releases []model.Release
	err = s.db.WithContext(ctx).Where("component = ? AND status = ?", component, model.ReleaseStatusPublished).Order("published_at DESC").Find(&releases).Error
	return releases, err
}

func (s *ProviderSupplyService) CreateTechnicalResourceUpdateTask(ctx context.Context, authorization *ManagementAuthorizationContext, resourceID, releaseID string, force bool) (*model.UpdateTask, error) {
	resource, binding, err := s.activeTechnicalResourceBinding(ctx, authorization, resourceID, PermissionProviderTechnicalResourcesWrite)
	if err != nil {
		return nil, err
	}
	if resource.LifecycleState != model.TechnicalResourceRegistered && resource.LifecycleState != model.TechnicalResourceDisabled {
		return nil, ErrTechnicalResourceStateTransition
	}
	targetType := model.UpdateTargetNode
	component := model.ComponentAgent
	if resource.Type == model.TechnicalResourceEndpoint {
		targetType, component = model.UpdateTargetEndpoint, model.ComponentEndpoint
	}
	return NewUpdateService(s.db).CreateTask(ctx, CreateUpdateTaskInput{
		Component: component, TargetType: targetType, TargetID: binding.SourceID, ReleaseID: strings.TrimSpace(releaseID), Force: force, CreatedBy: authorization.EffectiveUserID,
	})
}

func (s *ProviderSupplyService) ListTechnicalResourceUpdateTasks(ctx context.Context, authorization *ManagementAuthorizationContext, resourceID string) ([]model.UpdateTask, error) {
	_, binding, err := s.activeTechnicalResourceBinding(ctx, authorization, resourceID, PermissionProviderTechnicalResourcesRead)
	if err != nil {
		return nil, err
	}
	targetType := model.UpdateTargetNode
	if binding.SourceType == model.TechnicalResourceBindingLegacyEndpoint {
		targetType = model.UpdateTargetEndpoint
	}
	var tasks []model.UpdateTask
	err = s.db.WithContext(ctx).Where("target_type = ? AND target_id = ?", targetType, binding.SourceID).Order("created_at DESC").Find(&tasks).Error
	return tasks, err
}

func (s *ProviderSupplyService) CheckTechnicalResourceDelete(ctx context.Context, authorization *ManagementAuthorizationContext, resourceID string) (*TechnicalResourceDeleteCheck, error) {
	providerID, err := reauthorizeProviderPermission(s.db.WithContext(ctx), authorization, PermissionProviderTechnicalResourcesRead, s.now().UTC())
	if err != nil {
		return nil, err
	}
	var resource model.TechnicalResource
	if err := s.db.WithContext(ctx).Where("id = ? AND provider_id = ?", strings.TrimSpace(resourceID), providerID).First(&resource).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProviderSupplyObjectNotFound
		}
		return nil, err
	}
	return checkTechnicalResourceDelete(s.db.WithContext(ctx), providerID, &resource)
}

func checkTechnicalResourceDelete(database *gorm.DB, providerID string, resource *model.TechnicalResource) (*TechnicalResourceDeleteCheck, error) {
	result := &TechnicalResourceDeleteCheck{Blockers: []TechnicalResourceDeleteBlocker{}}
	add := func(code, message string, count int64) {
		if count > 0 {
			result.Blockers = append(result.Blockers, TechnicalResourceDeleteBlocker{Code: code, Message: message, Count: count})
		}
	}
	var bindings []model.TechnicalResourceBinding
	if err := database.Where("technical_resource_id = ?", resource.ID).Find(&bindings).Error; err != nil {
		return nil, err
	}
	pendingUnbound := resource.LifecycleState == model.TechnicalResourcePending && len(bindings) == 0
	if resource.LifecycleState != model.TechnicalResourceRetired && resource.HealthState != model.ResourceHealthOffline && !pendingUnbound {
		add("RESOURCE_NOT_DELETABLE", "仅已退役、离线或未部署资源可以删除", 1)
	}
	if resource.DeletedAt != nil {
		add("RESOURCE_ALREADY_DELETED", "资源已经删除", 1)
	}
	var count int64
	if resource.Type == model.TechnicalResourceAgent {
		if err := database.Model(&model.TechnicalResource{}).Where(
			"provider_id = ? AND parent_id = ? AND type = ? AND deleted_at IS NULL AND lifecycle_state <> ?",
			providerID,
			resource.ID,
			model.TechnicalResourceEndpoint,
			model.TechnicalResourceRetired,
		).Count(&count).Error; err != nil {
			return nil, err
		}
		add("ACTIVE_CHILD_ENDPOINTS", "仍有未退役的子 Endpoint", count)
	}
	count = 0
	if err := database.Model(&model.ResourceSession{}).Where("access_technical_resource_id = ? AND status IN ?", resource.ID, []model.ResourceSessionStatus{model.ResourceSessionAuthorizing, model.ResourceSessionActive, model.ResourceSessionEnding}).Count(&count).Error; err != nil {
		return nil, err
	}
	add("ACTIVE_SESSIONS", "仍有活动会话", count)
	for _, binding := range bindings {
		count = 0
		targetType := model.UpdateTargetNode
		if binding.SourceType == model.TechnicalResourceBindingLegacyEndpoint {
			targetType = model.UpdateTargetEndpoint
		}
		if err := database.Model(&model.UpdateTask{}).Where("target_type = ? AND target_id = ? AND status NOT IN ?", targetType, binding.SourceID,
			[]model.UpdateTaskStatus{model.UpdateTaskSucceeded, model.UpdateTaskFailed, model.UpdateTaskRolledBack, model.UpdateTaskCancelled, model.UpdateTaskExpired}).Count(&count).Error; err != nil {
			return nil, err
		}
		add("ACTIVE_UPDATE_TASKS", "仍有未结束的更新任务", count)
	}
	result.Allowed = len(result.Blockers) == 0
	return result, nil
}

func decodeStringList(raw string) []string {
	var result []string
	if json.Unmarshal([]byte(raw), &result) != nil {
		return []string{}
	}
	return result
}

func encodeStringList(values []string) string {
	if values == nil {
		values = []string{}
	}
	data, _ := json.Marshal(values)
	return string(data)
}
