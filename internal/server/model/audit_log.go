package model

import "time"

// ActionType 操作类型常量
const (
	ActionCreateAgent        = "create_agent"
	ActionDeleteAgent        = "delete_agent"
	ActionCreateService      = "create_service"
	ActionDeleteService      = "delete_service"
	ActionDesktopAuthGrant   = "desktop_auth_grant"
	ActionDesktopAuthRevoke  = "desktop_auth_revoke"
	ActionAgentAuthGrant     = "agent_auth_grant"
	ActionAgentAuthRevoke    = "agent_auth_revoke"
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
)

// AuditLog 审计日志模型
// 记录所有管理操作的审计信息
type AuditLog struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	AdminID    int64     `gorm:"index" json:"admin_id"`                     // 执行操作的管理员ID
	ActionType string    `gorm:"size:50;index;not null" json:"action_type"` // 操作类型
	TargetType string    `gorm:"size:50;not null" json:"target_type"`       // 目标类型 (agent, client, service, etc.)
	TargetID   string    `gorm:"size:100;not null" json:"target_id"`        // 目标ID
	TargetName string    `gorm:"size:200;not null" json:"target_name"`      // 目标名称
	Detail     string    `gorm:"type:text" json:"detail"`                   // 操作详情 (JSON格式)
	CreatedAt  time.Time `gorm:"autoCreateTime;index:idx_audit_logs_created_at" json:"created_at"`
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
