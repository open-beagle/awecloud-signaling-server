package model

import "time"

// 操作审计类型常量
const (
	OperationTypeSSHSession        = "ssh_session"
	OperationTypeK8SAPIRequest     = "k8s_api_request"
	OperationTypeK8SServiceConnect = "k8s_service_connect"
)

// OperationAuditLog 操作级审计日志模型
// 记录 Agent 上报的实际操作审计（SSH 会话、K8S API 请求、K8S Service 连接等）
type OperationAuditLog struct {
	ID            int64     `gorm:"primaryKey" json:"id"`
	AgentUserID   uint64    `gorm:"not null;index" json:"agent_user_id"`                       // Agent 的 User ID
	AgentName     string    `gorm:"size:100" json:"agent_name"`                                // Agent 名称
	ClientUserID  uint64    `gorm:"index" json:"client_user_id"`                               // Client 用户 ID
	ClientName    string    `gorm:"size:100" json:"client_name"`                               // Client 用户名
	EndpointID    string    `gorm:"size:36;index" json:"endpoint_id"`                          // Endpoint ID（Endpoint 跳跃时）
	EndpointName  string    `gorm:"size:100" json:"endpoint_name"`                             // Endpoint 名称
	OperationType string    `gorm:"size:50;not null;index:idx_oal_type" json:"operation_type"` // 操作类型
	Target        string    `gorm:"size:255;not null" json:"target"`                           // 操作目标
	Detail        string    `gorm:"type:text" json:"detail"`                                   // 操作详情（JSON）
	StartedAt     time.Time `gorm:"not null;index:idx_oal_started" json:"started_at"`          // 开始时间
	EndedAt       time.Time `json:"ended_at"`                                                  // 结束时间
	DurationMs    int64     `json:"duration_ms"`                                               // 持续时间（毫秒）
	CreatedAt     time.Time `gorm:"autoCreateTime;index:idx_oal_created" json:"created_at"`    // 记录创建时间
}

func (OperationAuditLog) TableName() string {
	return "operation_audit_log"
}
