package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var (
	ErrContainerSSHBusinessDomainInvalid  = errors.New("ContainerSSH business domain is invalid")
	ErrContainerSSHBusinessDomainConflict = errors.New("ContainerSSH business domain already belongs to another resource")
)

type containerSSHBusinessTarget struct {
	NamespaceName string `json:"namespace_name"`
	WorkloadName  string `json:"workload_name"`
	PodName       string `json:"pod_name"`
}

type containerServiceBusinessTarget struct {
	NamespaceName string `json:"namespace_name"`
	ServiceName   string `json:"service_name"`
}

// ContainerServiceBusinessDomain returns the stable user-facing address for a
// Kubernetes Service. Service UID and Tenant resource IDs never enter it.
func ContainerServiceBusinessDomain(ctx context.Context, database *gorm.DB, accessTechnicalResourceID, targetSnapshot string) (string, error) {
	var target containerServiceBusinessTarget
	if json.Unmarshal([]byte(targetSnapshot), &target) != nil {
		return "", ErrContainerSSHBusinessDomainInvalid
	}
	return containerBusinessDomain(ctx, database, accessTechnicalResourceID, target.ServiceName, target.NamespaceName)
}

// ContainerSSHRuntimeDomain identifies one current Pod route while the
// TenantResource and its AccessGrant remain bound to Workload + Container.
func ContainerSSHRuntimeDomain(ctx context.Context, database *gorm.DB, accessTechnicalResourceID, targetSnapshot string) (string, error) {
	var target containerSSHBusinessTarget
	if json.Unmarshal([]byte(targetSnapshot), &target) != nil {
		return "", ErrContainerSSHBusinessDomainInvalid
	}
	podName := strings.ToLower(strings.TrimSpace(target.PodName))
	if validation.IsDNS1123Subdomain(podName) != nil {
		return "", ErrContainerSSHBusinessDomainInvalid
	}
	domain, err := ContainerSSHBusinessDomain(ctx, database, accessTechnicalResourceID, targetSnapshot)
	if err != nil {
		return "", err
	}
	domain = podName + "." + domain
	if len(domain) > 253 || validation.IsDNS1123Subdomain(domain) != nil {
		return "", ErrContainerSSHBusinessDomainInvalid
	}
	return domain, nil
}

// ContainerSSHBusinessDomain returns the stable user-facing address for a
// workload. Runtime Pod identity and Tenant resource IDs never enter it.
func ContainerSSHBusinessDomain(ctx context.Context, database *gorm.DB, accessTechnicalResourceID, targetSnapshot string) (string, error) {
	var target containerSSHBusinessTarget
	if json.Unmarshal([]byte(targetSnapshot), &target) != nil {
		return "", ErrContainerSSHBusinessDomainInvalid
	}
	return containerBusinessDomain(ctx, database, accessTechnicalResourceID, target.WorkloadName, target.NamespaceName)
}

func containerBusinessDomain(ctx context.Context, database *gorm.DB, accessTechnicalResourceID, resourceName, namespaceName string) (string, error) {
	if database == nil || strings.TrimSpace(accessTechnicalResourceID) == "" {
		return "", ErrContainerSSHBusinessDomainInvalid
	}
	resourceName = strings.ToLower(strings.TrimSpace(resourceName))
	namespaceName = strings.ToLower(strings.TrimSpace(namespaceName))
	if validation.IsDNS1123Subdomain(resourceName) != nil || validation.IsDNS1123Label(namespaceName) != nil {
		return "", ErrContainerSSHBusinessDomainInvalid
	}

	var technical model.TechnicalResource
	if err := database.WithContext(ctx).Where("id = ?", strings.TrimSpace(accessTechnicalResourceID)).First(&technical).Error; err != nil {
		return "", ErrContainerSSHBusinessDomainInvalid
	}
	if technical.Type == model.TechnicalResourceEndpoint {
		if technical.ParentID == nil || strings.TrimSpace(*technical.ParentID) == "" {
			return "", ErrContainerSSHBusinessDomainInvalid
		}
		if err := database.WithContext(ctx).Where("id = ? AND type = ?", *technical.ParentID, model.TechnicalResourceAgent).First(&technical).Error; err != nil {
			return "", ErrContainerSSHBusinessDomainInvalid
		}
	}
	if technical.Type != model.TechnicalResourceAgent || technical.LifecycleState != model.TechnicalResourceRegistered {
		return "", ErrContainerSSHBusinessDomainInvalid
	}
	agentLabel := strings.ToLower(strings.TrimSpace(technical.DomainLabel))
	if validation.IsDNS1123Label(agentLabel) != nil {
		return "", ErrContainerSSHBusinessDomainInvalid
	}

	var provider model.ResourceProvider
	if err := database.WithContext(ctx).Where("id = ? AND status = ?", technical.ProviderID, model.ProviderStatusActive).First(&provider).Error; err != nil {
		return "", ErrContainerSSHBusinessDomainInvalid
	}
	if provider.DomainScope != model.ProviderDomainNamed && provider.DomainScope != model.ProviderDomainRoot {
		return "", ErrContainerSSHBusinessDomainInvalid
	}
	labels := []string{resourceName, namespaceName, agentLabel}

	suffix := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(resourceDomainSuffix(database.WithContext(ctx)))), ".")
	if validation.IsDNS1123Subdomain(suffix) != nil {
		return "", ErrContainerSSHBusinessDomainInvalid
	}
	domain := strings.Join(append(labels, suffix), ".")
	if len(domain) > 253 || validation.IsDNS1123Subdomain(domain) != nil {
		return "", ErrContainerSSHBusinessDomainInvalid
	}
	return domain, nil
}

func resourceDomainSuffix(database *gorm.DB) string {
	suffix := model.DefaultDomainSuffix
	var config model.SystemConfig
	if database != nil && database.Where("key = ?", model.ConfigDomainSuffix).First(&config).Error == nil && strings.TrimSpace(config.Value) != "" {
		suffix = config.Value
	}
	if !strings.HasPrefix(suffix, ".") {
		suffix = "." + suffix
	}
	return suffix
}
