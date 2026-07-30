package service

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

const PlatformAllocationOutboxConsumer = "resource_authorization_projection"

var platformAllocationEventTypes = []string{
	"resource_allocation.created",
	"resource_allocation.updated",
	"resource_allocation.scheduled",
	"resource_allocation.activated",
	"resource_allocation.suspended",
	"resource_allocation.resumed",
	"resource_allocation.revoked",
	"resource_allocation.expired",
	"resource_allocation.renewed",
}

func PlatformAllocationOutboxPolicies() map[string]JSONFieldPolicy {
	result := make(map[string]JSONFieldPolicy, len(platformAllocationEventTypes))
	for _, eventType := range platformAllocationEventTypes {
		result[eventType] = NewJSONFieldPolicy(
			"allocation_id", "tenant_id", "state", "mode", "valid_from", "expires_at",
			"scope_ids", "row_version", "occurred_at", "request_id",
		)
	}
	return result
}

func AppendPlatformAllocationOutbox(tx *gorm.DB, database *gorm.DB, allocation *model.ResourceAllocation, eventType, requestID string, occurredAt time.Time) error {
	if tx == nil || database == nil || allocation == nil || occurredAt.IsZero() {
		return ErrInvalidDeliveryInput
	}
	scopeIDs := make([]string, 0, len(allocation.Items))
	for _, item := range allocation.Items {
		scopeIDs = append(scopeIDs, item.ScopeID)
	}
	payload, err := json.Marshal(map[string]any{
		"allocation_id": allocation.ID,
		"tenant_id":     allocation.TenantID,
		"state":         allocation.State,
		"mode":          allocation.Mode,
		"valid_from":    allocation.ValidFrom,
		"expires_at":    allocation.ExpiresAt,
		"scope_ids":     scopeIDs,
		"row_version":   allocation.RowVersion,
		"occurred_at":   occurredAt.UTC(),
		"request_id":    requestID,
	})
	if err != nil {
		return err
	}
	outbox := NewResourceOutboxService(database, PlatformAllocationOutboxPolicies())
	_, err = outbox.Append(tx, AppendOutboxEventInput{
		Consumer: PlatformAllocationOutboxConsumer, EventType: eventType, AggregateType: "resource_allocation",
		AggregateID: allocation.ID, AggregateRevision: allocation.RowVersion,
		EventKey: fmt.Sprintf("resource_allocation:%s:%d", allocation.ID, allocation.RowVersion),
		Payload:  payload, RequestID: requestID,
	})
	return err
}
