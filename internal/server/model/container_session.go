package model

import "time"

// ContainerSessionStatus describes the control-plane lifecycle of a
// ContainerSSH session. The Agent Broker will use the revoked state to stop
// an active stream once the data plane is connected.
type ContainerSessionStatus string

const (
	ContainerSessionActive   ContainerSessionStatus = "active"
	ContainerSessionEnded    ContainerSessionStatus = "ended"
	ContainerSessionRevoked  ContainerSessionStatus = "revoked"
	ContainerSessionRejected ContainerSessionStatus = "rejected"
)

// ContainerSession records connection context, target identity and the final
// outcome. Terminal input/output and Kubernetes credentials are never stored.
type ContainerSession struct {
	ID                       string                 `gorm:"primaryKey;size:36" json:"id"`
	TenantID                 string                 `gorm:"size:36;not null;index" json:"tenant_id"`
	UserID                   uint64                 `gorm:"not null;index" json:"user_id"`
	DeviceID                 uint64                 `gorm:"index" json:"device_id,omitempty"`
	ResourceID               string                 `gorm:"size:36;not null;index" json:"resource_id"`
	WorkspaceID              string                 `gorm:"size:200;index" json:"workspace_id,omitempty"`
	GrantRevision            int64                  `gorm:"not null;default:0" json:"grant_revision"`
	TargetRevision           int64                  `gorm:"not null;default:0" json:"target_revision"`
	PodUID                   string                 `gorm:"size:100" json:"pod_uid,omitempty"`
	ContainerName            string                 `gorm:"size:200" json:"container_name,omitempty"`
	AgentNodeID              uint64                 `gorm:"index" json:"agent_node_id,omitempty"`
	RequestID                string                 `gorm:"size:100;index" json:"request_id,omitempty"`
	TraceID                  string                 `gorm:"size:100;index" json:"trace_id,omitempty"`
	Status                   ContainerSessionStatus `gorm:"size:20;not null;default:'active';index" json:"status"`
	StartedAt                time.Time              `gorm:"not null;index" json:"started_at"`
	EndedAt                  *time.Time             `json:"ended_at,omitempty"`
	Result                   string                 `gorm:"size:30" json:"result,omitempty"`
	CloseReason              string                 `gorm:"size:500" json:"close_reason,omitempty"`
	DisconnectAcknowledgedAt *time.Time             `json:"disconnect_acknowledged_at,omitempty"`
	CreatedAt                time.Time              `json:"created_at"`
	UpdatedAt                time.Time              `json:"updated_at"`
}

func (ContainerSession) TableName() string { return "container_session" }
