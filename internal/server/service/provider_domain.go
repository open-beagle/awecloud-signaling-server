package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var (
	ErrProviderDomainLabelInvalid  = errors.New("provider domain label is invalid")
	ErrProviderDomainLabelExists   = errors.New("provider domain label already exists")
	ErrProviderDomainConfirmation  = errors.New("provider domain label confirmation does not match")
	ErrAgentDomainLabelInvalid     = errors.New("agent domain label is invalid")
	ErrAgentDomainLabelExists      = errors.New("agent domain label already exists")
	ErrHostDomainLabelInvalid      = errors.New("host domain label is invalid")
	ErrHostDomainLabelExists       = errors.New("host domain label already exists")
	ErrDomainOwnershipMissing      = errors.New("domain ownership is missing")
	providerDomainLabelPattern     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	providerDomainLabelReservedSet = map[string]struct{}{
		"admin": {}, "api": {}, "container": {}, "kubernetes": {}, "service": {}, "system": {},
	}
)

type AgentDomainIdentity struct {
	ProviderID      string
	AgentResourceID string
	AgentLabel      string
	ProviderScope   model.ProviderDomainScope
	ProviderLabel   string
}

func (i AgentDomainIdentity) Namespace() string {
	if i.ProviderScope == model.ProviderDomainRoot {
		return i.AgentLabel
	}
	return i.AgentLabel + "." + i.ProviderLabel
}

type ProviderDomainChangeResult struct {
	DomainCount int64
	Examples    []string
}

func NormalizeProviderDomainLabel(value string) (string, error) {
	return normalizeDomainLabel(value, ErrProviderDomainLabelInvalid)
}

func NormalizeAgentDomainLabel(value string) (string, error) {
	return normalizeDomainLabel(value, ErrAgentDomainLabelInvalid)
}

func NormalizeHostDomainLabel(value string) (string, error) {
	return normalizeDomainLabel(value, ErrHostDomainLabelInvalid)
}

func SuggestedHostDomainLabel(ctx context.Context, database *gorm.DB, runtimeUserID uint64, value string) string {
	label, err := NormalizeHostDomainLabel(value)
	if err != nil || database == nil || runtimeUserID == 0 {
		return ""
	}
	var nodeCount, endpointCount int64
	if err := database.WithContext(ctx).Model(&model.Node{}).
		Where("user_id = ? AND type = ? AND lower(host_domain_label) = ?", runtimeUserID, model.NodeTypeAgent, label).
		Count(&nodeCount).Error; err != nil {
		return ""
	}
	if err := database.WithContext(ctx).Model(&model.Endpoint{}).
		Where("user_id = ? AND revoked = ? AND lower(host_domain_label) = ?", runtimeUserID, false, label).
		Count(&endpointCount).Error; err != nil {
		return ""
	}
	if nodeCount+endpointCount > 0 {
		return ""
	}
	return label
}

func normalizeDomainLabel(value string, invalid error) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if !providerDomainLabelPattern.MatchString(value) {
		return "", invalid
	}
	if _, reserved := providerDomainLabelReservedSet[value]; reserved {
		return "", invalid
	}
	return value, nil
}

func ResolveAgentDomainForNode(ctx context.Context, database *gorm.DB, nodeID uint64) (AgentDomainIdentity, error) {
	var identity AgentDomainIdentity
	err := database.WithContext(ctx).Table("technical_resource_binding AS binding").
		Select("agent.provider_id, agent.id AS agent_resource_id, agent.domain_label AS agent_label, provider.domain_scope AS provider_scope, provider.domain_label AS provider_label").
		Joins("JOIN technical_resource AS agent ON agent.id = binding.technical_resource_id AND agent.type = ? AND agent.deleted_at IS NULL", model.TechnicalResourceAgent).
		Joins("JOIN resource_provider AS provider ON provider.id = agent.provider_id").
		Where("binding.source_type = ? AND binding.source_id = ? AND binding.enabled = ?", model.TechnicalResourceBindingLegacyNode, fmt.Sprint(nodeID), true).
		Take(&identity).Error
	if err != nil || identity.AgentLabel == "" || (identity.ProviderScope == model.ProviderDomainNamed && identity.ProviderLabel == "") {
		return AgentDomainIdentity{}, ErrDomainOwnershipMissing
	}
	return identity, nil
}

func ResolveAgentDomainForEndpoint(ctx context.Context, database *gorm.DB, endpointID string) (AgentDomainIdentity, error) {
	var identity AgentDomainIdentity
	err := database.WithContext(ctx).Table("technical_resource_binding AS binding").
		Select("agent.provider_id, agent.id AS agent_resource_id, agent.domain_label AS agent_label, provider.domain_scope AS provider_scope, provider.domain_label AS provider_label").
		Joins("JOIN technical_resource AS endpoint_resource ON endpoint_resource.id = binding.technical_resource_id AND endpoint_resource.type = ? AND endpoint_resource.deleted_at IS NULL", model.TechnicalResourceEndpoint).
		Joins("JOIN technical_resource AS agent ON agent.id = endpoint_resource.parent_id AND agent.type = ? AND agent.deleted_at IS NULL", model.TechnicalResourceAgent).
		Joins("JOIN resource_provider AS provider ON provider.id = agent.provider_id").
		Where("binding.source_type = ? AND binding.source_id = ? AND binding.enabled = ?", model.TechnicalResourceBindingLegacyEndpoint, endpointID, true).
		Take(&identity).Error
	if err != nil || identity.AgentLabel == "" || (identity.ProviderScope == model.ProviderDomainNamed && identity.ProviderLabel == "") {
		return AgentDomainIdentity{}, ErrDomainOwnershipMissing
	}
	return identity, nil
}

func ChangeProviderDomainLabel(ctx context.Context, tx *gorm.DB, providerID, oldLabel, newLabel string) (ProviderDomainChangeResult, error) {
	result := ProviderDomainChangeResult{}
	if tx == nil || providerID == "" || oldLabel == "" || newLabel == "" {
		return result, ErrProviderDomainLabelInvalid
	}
	var records []model.DomainRegistry
	if err := tx.WithContext(ctx).Where("provider_id = ?", providerID).Find(&records).Error; err != nil {
		return result, err
	}
	suffix := domainSuffix(ctx, tx)
	oldNamespaceSuffix := "." + strings.ToLower(oldLabel) + suffix
	newNamespaceSuffix := "." + strings.ToLower(newLabel) + suffix
	for i := range records {
		if records[i].AgentResourceID == "" || !strings.HasSuffix(strings.ToLower(records[i].Domain), oldNamespaceSuffix) {
			return result, ErrDomainOwnershipMissing
		}
		newDomain := records[i].Domain[:len(records[i].Domain)-len(oldNamespaceSuffix)] + newNamespaceSuffix
		var conflictCount int64
		if err := tx.WithContext(ctx).Model(&model.DomainRegistry{}).
			Where("domain = ? AND provider_id <> ?", newDomain, providerID).Count(&conflictCount).Error; err != nil {
			return result, err
		}
		if conflictCount > 0 {
			return result, ErrProviderDomainLabelExists
		}
		if err := tx.WithContext(ctx).Model(&records[i]).Update("domain", newDomain).Error; err != nil {
			return result, err
		}
		result.DomainCount++
		if len(result.Examples) < 3 {
			result.Examples = append(result.Examples, records[i].Domain+" -> "+newDomain)
		}
	}
	return result, nil
}

func domainSuffix(ctx context.Context, database *gorm.DB) string {
	suffix := model.DefaultDomainSuffix
	if database != nil {
		var config model.SystemConfig
		if err := database.WithContext(ctx).Where("key = ?", model.ConfigDomainSuffix).First(&config).Error; err == nil && strings.TrimSpace(config.Value) != "" {
			suffix = strings.TrimSpace(config.Value)
		}
	}
	if !strings.HasPrefix(suffix, ".") {
		suffix = "." + suffix
	}
	return strings.ToLower(suffix)
}
