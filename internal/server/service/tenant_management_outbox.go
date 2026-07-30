package service

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

const TenantAuthorizationOutboxConsumer = "tenant_authorization_projection"

var tenantManagementOutboxPolicies = func() map[string]JSONFieldPolicy {
	policy := NewJSONFieldPolicy(
		"tenant_id", "resource_id", "grant_id", "session_id", "visibility_state", "status",
		"revision", "row_version", "reason_code",
	)
	events := []string{
		"tenant_resource.published", "tenant_resource.rejected", "tenant_resource.updated", "tenant_resource.visibility_changed",
		"tenant_access_grant.created", "tenant_access_grant.updated", "tenant_access_grant.suspended", "tenant_access_grant.resumed", "tenant_access_grant.revoked", "tenant_access_grant.expired",
		"resource_session.authorized", "resource_session.ending",
	}
	result := make(map[string]JSONFieldPolicy, len(events))
	for _, event := range events {
		result[event] = policy
	}
	return result
}()

type TenantManagementOutboxInput struct {
	EventType         string
	AggregateType     string
	AggregateID       string
	AggregateRevision int64
	TenantID          string
	ResourceID        string
	GrantID           string
	SessionID         string
	VisibilityState   string
	Status            string
	RowVersion        int64
	ReasonCode        string
	RequestID         string
	AvailableAt       time.Time
}

func AppendTenantManagementOutbox(tx *gorm.DB, input TenantManagementOutboxInput) error {
	payload := map[string]any{
		"tenant_id": input.TenantID,
		"revision":  input.AggregateRevision,
	}
	if input.ResourceID != "" {
		payload["resource_id"] = input.ResourceID
	}
	if input.GrantID != "" {
		payload["grant_id"] = input.GrantID
	}
	if input.SessionID != "" {
		payload["session_id"] = input.SessionID
	}
	if input.VisibilityState != "" {
		payload["visibility_state"] = input.VisibilityState
	}
	if input.Status != "" {
		payload["status"] = input.Status
	}
	if input.RowVersion > 0 {
		payload["row_version"] = input.RowVersion
	}
	if input.ReasonCode != "" {
		payload["reason_code"] = input.ReasonCode
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	outbox := NewResourceOutboxService(tx, tenantManagementOutboxPolicies)
	_, err = outbox.Append(tx, AppendOutboxEventInput{
		Consumer: TenantAuthorizationOutboxConsumer, EventType: input.EventType,
		AggregateType: input.AggregateType, AggregateID: input.AggregateID, AggregateRevision: input.AggregateRevision,
		EventKey: fmt.Sprintf("%s:%s:%d", input.AggregateType, input.AggregateID, input.AggregateRevision),
		Payload:  data, RequestID: input.RequestID, AvailableAt: input.AvailableAt,
	})
	return err
}
