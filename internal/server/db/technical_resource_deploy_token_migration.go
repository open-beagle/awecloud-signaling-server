package db

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func migrateAgentDeployTokensToTechnicalResources(database *gorm.DB) error {
	if database == nil || !database.Migrator().HasTable(&model.DeployToken{}) {
		return nil
	}
	return database.Transaction(func(tx *gorm.DB) error {
		var tokens []model.DeployToken
		if err := tx.Joins("JOIN user ON user.id = deploy_tokens.user_id").
			Where("user.role = ?", model.UserRoleAgent).Order("deploy_tokens.id").Find(&tokens).Error; err != nil {
			return fmt.Errorf("load legacy Agent deploy tokens: %w", err)
		}
		if len(tokens) == 0 {
			return nil
		}
		var providerID string
		for i := range tokens {
			legacy := &tokens[i]
			var existing model.TechnicalResourceDeployToken
			err := tx.Where("token = ?", legacy.Token).First(&existing).Error
			if err == nil {
				if err := validateMigratedDeployToken(tx, legacy, &existing); err != nil {
					return fmt.Errorf("validate already migrated Agent deploy token %d: %w", legacy.ID, err)
				}
				if err := tx.Delete(&model.DeployToken{}, legacy.ID).Error; err != nil {
					return err
				}
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("find migrated Agent deploy token %d: %w", legacy.ID, err)
			}
			if providerID == "" {
				providerID, err = deploymentTokenMigrationProvider(tx)
				if err != nil {
					return err
				}
			}
			resource, err := deploymentTokenTechnicalResource(tx, providerID, legacy)
			if err != nil {
				return fmt.Errorf("map Agent deploy token %d: %w", legacy.ID, err)
			}
			status, consumedAt, revokedAt := migratedDeployTokenStatus(legacy)
			migrated := model.TechnicalResourceDeployToken{
				ID: uuid.NewString(), TechnicalResourceID: resource.ID, Token: legacy.Token, Name: legacy.Name, RuntimeUserID: legacy.UserID,
				Status: status, DeviceFingerprint: legacy.DeviceFingerprint, ExpiresAt: legacy.ExpiresAt,
				ConsumedAt: consumedAt, RevokedAt: revokedAt, CreatedByUserID: legacy.UserID, CreatedAt: legacy.CreatedAt,
			}
			if err := tx.Create(&migrated).Error; err != nil {
				return err
			}
			if err := tx.Delete(&model.DeployToken{}, legacy.ID).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func validateMigratedDeployToken(tx *gorm.DB, legacy *model.DeployToken, existing *model.TechnicalResourceDeployToken) error {
	if existing.RuntimeUserID != legacy.UserID {
		return fmt.Errorf("runtime user differs: legacy=%d migrated=%d", legacy.UserID, existing.RuntimeUserID)
	}
	var resource model.TechnicalResource
	if err := tx.Where("id = ?", existing.TechnicalResourceID).First(&resource).Error; err != nil {
		return fmt.Errorf("load technical resource %s: %w", existing.TechnicalResourceID, err)
	}
	if resource.Type != model.TechnicalResourceAgent || resource.RuntimeUserID != legacy.UserID {
		return fmt.Errorf("technical resource ownership differs: type=%s runtime_user_id=%d", resource.Type, resource.RuntimeUserID)
	}
	expectedStatus, _, _ := migratedDeployTokenStatus(legacy)
	if existing.Name != legacy.Name || existing.Status != expectedStatus || existing.DeviceFingerprint != legacy.DeviceFingerprint || !sameOptionalTime(existing.ExpiresAt, legacy.ExpiresAt) {
		return fmt.Errorf("credential attributes differ for migrated token %s", existing.ID)
	}
	if expectedStatus == model.TechnicalResourceDeployTokenConsumed && existing.ConsumedAt == nil {
		return fmt.Errorf("consumed token %s has no consumed_at", existing.ID)
	}
	if expectedStatus == model.TechnicalResourceDeployTokenRevoked && existing.RevokedAt == nil {
		return fmt.Errorf("revoked token %s has no revoked_at", existing.ID)
	}
	return nil
}

func migratedDeployTokenStatus(legacy *model.DeployToken) (model.TechnicalResourceDeployTokenStatus, *time.Time, *time.Time) {
	status := model.TechnicalResourceDeployTokenPending
	var consumedAt, revokedAt *time.Time
	switch legacy.Status {
	case model.DeployTokenStatusBound:
		status = model.TechnicalResourceDeployTokenConsumed
		consumedAt = legacy.BoundAt
		if consumedAt == nil {
			value := legacy.CreatedAt
			consumedAt = &value
		}
	case model.DeployTokenStatusRevoked:
		status = model.TechnicalResourceDeployTokenRevoked
		value := legacy.CreatedAt
		revokedAt = &value
	}
	return status, consumedAt, revokedAt
}

func deploymentTokenMigrationProvider(tx *gorm.DB) (string, error) {
	var provider model.ResourceProvider
	err := tx.Where("key = ? AND status = ?", "beagle", model.ProviderStatusActive).First(&provider).Error
	if err == nil {
		return provider.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	var providers []model.ResourceProvider
	if err := tx.Where("status = ?", model.ProviderStatusActive).Limit(2).Find(&providers).Error; err != nil {
		return "", err
	}
	if len(providers) == 0 {
		provider = model.ResourceProvider{ID: uuid.NewString(), Key: "beagle", DisplayName: "Beagle", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
		if err := tx.Create(&provider).Error; err != nil {
			return "", err
		}
		return provider.ID, nil
	}
	if len(providers) != 1 {
		return "", fmt.Errorf("cannot assign pending Agent deploy tokens: expected beagle or exactly one active Provider")
	}
	return providers[0].ID, nil
}

func deploymentTokenTechnicalResource(tx *gorm.DB, providerID string, token *model.DeployToken) (*model.TechnicalResource, error) {
	var node model.Node
	query := tx.Where("user_id = ? AND type = ?", token.UserID, model.NodeTypeAgent)
	if token.NodeID != nil {
		err := tx.Where("id = ? AND user_id = ? AND type = ?", *token.NodeID, token.UserID, model.NodeTypeAgent).First(&node).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if err == nil {
			query = nil
		}
	}
	var nodeErr error
	if query != nil {
		nodeErr = query.Order("id").First(&node).Error
	}
	if nodeErr == nil {
		var binding model.TechnicalResourceBinding
		if err := tx.Where("source_type = ? AND source_id = ?", model.TechnicalResourceBindingLegacyNode, strconv.FormatUint(node.ID, 10)).First(&binding).Error; err == nil {
			var resource model.TechnicalResource
			if err := tx.Where("id = ?", binding.TechnicalResourceID).First(&resource).Error; err != nil {
				return nil, err
			}
			if resource.RuntimeUserID == 0 {
				if err := tx.Model(&resource).Update("runtime_user_id", token.UserID).Error; err != nil {
					return nil, err
				}
				resource.RuntimeUserID = token.UserID
			}
			return &resource, nil
		}
	} else if !errors.Is(nodeErr, gorm.ErrRecordNotFound) {
		return nil, nodeErr
	}
	resource := &model.TechnicalResource{
		ID: uuid.NewString(), ProviderID: providerID, Type: model.TechnicalResourceAgent,
		StableKey: "deploy-token:" + strconv.FormatUint(token.ID, 10), LifecycleState: model.TechnicalResourcePending,
		HealthState: model.ResourceHealthUnknown, CredentialRevision: 1, RuntimeUserID: token.UserID,
		ConfigRevision: 1, RowVersion: 1,
	}
	if err := tx.Create(resource).Error; err != nil {
		return nil, err
	}
	return resource, nil
}
