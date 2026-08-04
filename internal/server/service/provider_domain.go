package service

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var (
	ErrProviderDomainLabelInvalid  = errors.New("provider domain label is invalid")
	ErrProviderDomainLabelExists   = errors.New("provider domain label already exists")
	ErrProviderDomainConfirmation  = errors.New("provider domain label confirmation does not match")
	providerDomainLabelPattern     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	providerDomainLabelReservedSet = map[string]struct{}{
		"admin": {}, "api": {}, "container": {}, "kubernetes": {}, "service": {}, "system": {},
	}
)

type ProviderDomainChangeResult struct {
	DomainCount int64
	Examples    []string
}

func NormalizeProviderDomainLabel(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if !providerDomainLabelPattern.MatchString(value) {
		return "", ErrProviderDomainLabelInvalid
	}
	if _, reserved := providerDomainLabelReservedSet[value]; reserved {
		return "", ErrProviderDomainLabelInvalid
	}
	return value, nil
}

func EffectiveProviderDomainLabel(ctx context.Context, database *gorm.DB, userID uint64, fallback string) string {
	if database == nil || userID == 0 {
		return fallback
	}
	var label string
	err := database.WithContext(ctx).Table("technical_resource AS resource").
		Select("provider.domain_label").
		Joins("JOIN resource_provider AS provider ON provider.id = resource.provider_id").
		Where("resource.runtime_user_id = ? AND resource.deleted_at IS NULL AND provider.domain_label <> ''", userID).
		Order("resource.type ASC, resource.id ASC").Limit(1).Scan(&label).Error
	if err != nil || label == "" {
		return fallback
	}
	return label
}

func NormalizeReportedProviderDomain(ctx context.Context, database *gorm.DB, userID uint64, fallback, domain string) string {
	if fallback == "" && database != nil && userID > 0 {
		var user model.User
		if err := database.WithContext(ctx).Select("name").First(&user, userID).Error; err == nil {
			fallback = user.Name
		}
	}
	label := EffectiveProviderDomainLabel(ctx, database, userID, fallback)
	if label == "" || label == fallback {
		return domain
	}
	return replaceDomainNamespace(domain, fallback, label, domainSuffix(ctx, database))
}

func ChangeProviderDomainLabel(ctx context.Context, tx *gorm.DB, providerID, oldLabel, newLabel string) (ProviderDomainChangeResult, error) {
	result := ProviderDomainChangeResult{}
	if tx == nil || providerID == "" || oldLabel == "" || newLabel == "" {
		return result, ErrProviderDomainLabelInvalid
	}
	var userIDs []uint64
	if err := tx.WithContext(ctx).Model(&model.TechnicalResource{}).
		Where("provider_id = ? AND runtime_user_id > 0 AND deleted_at IS NULL", providerID).
		Distinct().Pluck("runtime_user_id", &userIDs).Error; err != nil {
		return result, err
	}
	if len(userIDs) == 0 {
		return result, nil
	}
	var users []model.User
	if err := tx.WithContext(ctx).Where("id IN ?", userIDs).Find(&users).Error; err != nil {
		return result, err
	}
	userNames := make(map[uint64]string, len(users))
	for i := range users {
		userNames[users[i].ID] = users[i].Name
	}
	var records []model.DomainRegistry
	if err := tx.WithContext(ctx).Where("user_id IN ? AND status = ?", userIDs, model.DomainStatusOnline).Find(&records).Error; err != nil {
		return result, err
	}
	suffix := domainSuffix(ctx, tx)
	for i := range records {
		newDomain := replaceDomainNamespace(records[i].Domain, oldLabel, newLabel, suffix)
		if newDomain == records[i].Domain {
			newDomain = replaceDomainNamespace(records[i].Domain, userNames[records[i].UserID], newLabel, suffix)
		}
		if newDomain == records[i].Domain {
			continue
		}
		clone := records[i]
		clone.ID = 0
		clone.Domain = newDomain
		clone.Status = model.DomainStatusOnline
		clone.CreatedAt = records[i].CreatedAt
		clone.UpdatedAt = records[i].UpdatedAt
		var existing model.DomainRegistry
		query := tx.WithContext(ctx).Where("domain = ? AND node_id = ? AND endpoint_id = ?", newDomain, clone.NodeID, clone.EndpointID)
		if err := query.First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.WithContext(ctx).Create(&clone).Error; err != nil {
				return result, err
			}
		} else if err != nil {
			return result, err
		} else if err := tx.WithContext(ctx).Model(&existing).Updates(map[string]any{
			"type": clone.Type, "user_id": clone.UserID, "target_ip": clone.TargetIP, "target_port": clone.TargetPort,
			"namespace": clone.Namespace, "service_name": clone.ServiceName, "service_ports": clone.ServicePorts,
			"ssh_users": clone.SshUsers, "status": model.DomainStatusOnline,
		}).Error; err != nil {
			return result, err
		}
		if err := tx.WithContext(ctx).Model(&records[i]).Update("status", model.DomainStatusOffline).Error; err != nil {
			return result, err
		}
		result.DomainCount++
		if len(result.Examples) < 3 {
			result.Examples = append(result.Examples, records[i].Domain+" -> "+newDomain)
		}
	}
	return result, nil
}

func replaceDomainNamespace(domain, oldLabel, newLabel, suffix string) string {
	if domain == "" || oldLabel == "" || newLabel == "" || oldLabel == newLabel {
		return domain
	}
	oldSuffix := "." + strings.ToLower(oldLabel) + suffix
	if !strings.HasSuffix(strings.ToLower(domain), oldSuffix) {
		return domain
	}
	return domain[:len(domain)-len(oldSuffix)] + "." + newLabel + suffix
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
