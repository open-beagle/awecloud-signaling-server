package model

import "time"

// ActionType 操作类型常量
const (
	ActionCreateAgent        = "create_agent"
	ActionDeleteAgent        = "delete_agent"
	ActionCreateService      = "create_service"
	ActionDeleteService      = "delete_service"
	ActionGrantDesktop       = "grant_desktop"
	ActionRevokeDesktop      = "revoke_desktop"
	ActionGrantAgent         = "grant_agent"
	ActionRevokeAgent        = "revoke_agent"
	ActionCreatePortForward  = "create_port_forward"
	ActionDeletePortForward  = "delete_port_forward"
	ActionCreateClientGroup  = "create_client_group"
	ActionDeleteClientGroup  = "delete_client_group"
	ActionCreateAgentGroup   = "create_agent_group"
	ActionDeleteAgentGroup   = "delete_agent_group"
	ActionUpdateAgent        = "update_agent"
	ActionUpdateClient       = "update_client"
	ActionUpdateService      = "update_service"
	ActionUpdatePortForward  = "update_port_forward"
	ActionUpdateClientGroup  = "update_client_group"
	ActionUpdateAgentGroup   = "update_agent_group"
	ActionResetAgentSecret   = "reset_agent_secret"
	ActionResetClientSecret  = "reset_client_secret"
	ActionCreateClient       = "create_client"
	ActionDeleteClient       = "delete_client"
	ActionDeleteDesktop      = "delete_desktop"
	ActionToggleService      = "toggle_service"
	ActionTogglePortForward  = "toggle_port_forward"
	ActionAddGroupMember     = "add_group_member"
	ActionRemoveGroupMember  = "remove_group_member"
	ActionUpdateSystemConfig = "update_system_config"

	// K8S ACL 操作
	ActionGrantK8SACL         = "grant_k8s_acl"
	ActionRevokeK8SACL        = "revoke_k8s_acl"
	ActionGrantK8SServiceACL  = "grant_k8s_service_acl"
	ActionRevokeK8SServiceACL = "revoke_k8s_service_acl"

	// Endpoint 操作
	ActionCreateEndpoint = "create_endpoint"
	ActionUpdateEndpoint = "update_endpoint"
	ActionDeleteEndpoint = "delete_endpoint"

	// 隧道管理操作
	ActionUpdateTunnelUser = "update_tunnel_user"
	ActionDeleteTunnelUser = "delete_tunnel_user"
	ActionUpdateTunnelNode = "update_tunnel_node"
	ActionUpdateTunnelTags = "update_tunnel_tags"
	ActionDeleteTunnelNode = "delete_tunnel_node"
	ActionUpdateTunnelACL  = "update_tunnel_acl"
	ActionSyncTunnelACL    = "sync_tunnel_acl"
)

// AuditLog 审计日志模型
// 记录所有管理操作的审计信息
type AuditLog struct {
	ID                  int64     `gorm:"primaryKey" json:"id"`
	UserID              int64     `gorm:"index:idx_audit_user" json:"user_id"` // 兼容字段，等同于 ActorAdminID
	UserType            string    `gorm:"size:20;default:admin" json:"user_type"`
	ActorAdminID        int64     `gorm:"index:idx_audit_actor_admin" json:"actor_admin_id"`
	ActorUsername       string    `gorm:"size:100" json:"actor_username"`
	ActorUserID         uint64    `gorm:"index:idx_audit_actor_user" json:"actor_user_id"`
	EffectiveUserID     uint64    `gorm:"index:idx_audit_effective_user" json:"effective_user_id"`
	SimulationSessionID string    `gorm:"size:36;index:idx_audit_simulation_session" json:"simulation_session_id"`
	ScopeType           string    `gorm:"size:20;index:idx_audit_scope,priority:1" json:"scope_type"`
	ScopeID             string    `gorm:"size:64;index:idx_audit_scope,priority:2" json:"scope_id"`
	PlatformRole        string    `gorm:"size:30" json:"platform_role"`
	TenantID            string    `gorm:"size:64;index:idx_audit_tenant" json:"tenant_id"`
	TenantRole          string    `gorm:"size:30" json:"tenant_role"`
	RequiredPermission  string    `gorm:"size:80" json:"required_permission"`
	PermissionRevision  int64     `json:"permission_revision"`
	RequestID           string    `gorm:"size:64;index:idx_audit_request" json:"request_id"`
	SourceIP            string    `gorm:"size:64" json:"source_ip"`
	UserAgent           string    `gorm:"size:512" json:"user_agent"`
	ActionType          string    `gorm:"size:50;index;not null" json:"action_type"`
	TargetType          string    `gorm:"size:50;not null" json:"target_type"`
	TargetID            string    `gorm:"size:100;not null" json:"target_id"`
	TargetName          string    `gorm:"size:200;not null" json:"target_name"`
	Detail              string    `gorm:"type:text" json:"detail"`
	CreatedAt           time.Time `gorm:"autoCreateTime;index:idx_audit_logs_created_at" json:"created_at"`
}

// TableName 指定表名
func (AuditLog) TableName() string {
	return "audit_log"
}

// AuditLogDetail 审计日志详情结构（用于 JSON 解析）
type AuditLogDetail struct {
	Before interface{} `json:"before,omitempty"` // 操作前的状态
	After  interface{} `json:"after,omitempty"`  // 操作后的状态
	Reason string      `json:"reason,omitempty"` // 操作原因
	Extra  interface{} `json:"extra,omitempty"`  // 额外信息
}
