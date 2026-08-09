package model

import "time"

type UpdateTargetType string

const (
	UpdateTargetNode     UpdateTargetType = "node"
	UpdateTargetEndpoint UpdateTargetType = "endpoint"
)

type UpdateTaskStatus string

const (
	UpdateTaskPending     UpdateTaskStatus = "pending"
	UpdateTaskDelivered   UpdateTaskStatus = "delivered"
	UpdateTaskAccepted    UpdateTaskStatus = "accepted"
	UpdateTaskDownloading UpdateTaskStatus = "downloading"
	UpdateTaskVerifying   UpdateTaskStatus = "verifying"
	UpdateTaskStaged      UpdateTaskStatus = "staged"
	UpdateTaskInstalling  UpdateTaskStatus = "installing"
	UpdateTaskRestarting  UpdateTaskStatus = "restarting"
	UpdateTaskSucceeded   UpdateTaskStatus = "succeeded"
	UpdateTaskFailed      UpdateTaskStatus = "failed"
	UpdateTaskRolledBack  UpdateTaskStatus = "rolled_back"
	UpdateTaskCancelled   UpdateTaskStatus = "cancelled"
	UpdateTaskExpired     UpdateTaskStatus = "expired"
)

func (s UpdateTaskStatus) Terminal() bool {
	switch s {
	case UpdateTaskSucceeded, UpdateTaskFailed, UpdateTaskRolledBack, UpdateTaskCancelled, UpdateTaskExpired:
		return true
	default:
		return false
	}
}

type UpdateTask struct {
	ID               string           `gorm:"primaryKey;size:36" json:"id"`
	Component        Component        `gorm:"size:20;not null;index" json:"component"`
	TargetType       UpdateTargetType `gorm:"size:20;not null;index" json:"target_type"`
	TargetID         string           `gorm:"size:100;not null;index" json:"target_id"`
	TargetName       string           `gorm:"size:100" json:"target_name"`
	ReleaseID        string           `gorm:"size:36;not null;index" json:"release_id"`
	ArtifactID       string           `gorm:"size:36;not null;default:'';index" json:"artifact_id"`
	DesiredVersion   string           `gorm:"size:64;not null" json:"desired_version"`
	DesiredCommitID  string           `gorm:"size:40;not null;default:''" json:"desired_commit_id"`
	DesiredSHA256    string           `gorm:"size:64;not null;default:''" json:"desired_sha256"`
	Force            bool             `gorm:"not null;default:false" json:"force"`
	ScheduledAt      *time.Time       `json:"scheduled_at"`
	DeadlineAt       *time.Time       `json:"deadline_at"`
	Status           UpdateTaskStatus `gorm:"size:20;not null;default:'pending';index" json:"status"`
	Attempt          int              `gorm:"not null;default:0" json:"attempt"`
	MaxAttempts      int              `gorm:"not null;default:3" json:"max_attempts"`
	LastDeliveredAt  *time.Time       `json:"last_delivered_at"`
	LastReportedAt   *time.Time       `json:"last_reported_at"`
	LastErrorCode    string           `gorm:"size:100" json:"last_error_code"`
	LastErrorMessage string           `gorm:"type:text" json:"last_error_message"`
	CreatedBy        uint64           `json:"created_by"`
	RetryOfTaskID    string           `gorm:"size:36;index" json:"retry_of_task_id,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`

	Release *Release `gorm:"foreignKey:ReleaseID" json:"release,omitempty"`
}

func (UpdateTask) TableName() string {
	return "update_task"
}

type UpdateEvent struct {
	ID              int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID          string    `gorm:"size:36;not null;index" json:"task_id"`
	Sequence        int64     `gorm:"not null" json:"sequence"`
	Phase           string    `gorm:"size:32;not null" json:"phase"`
	Progress        int       `gorm:"not null;default:0" json:"progress"`
	RunningVersion  string    `gorm:"size:64" json:"running_version"`
	RunningCommitID string    `gorm:"size:40" json:"running_commit_id"`
	RunningSHA256   string    `gorm:"size:64" json:"running_sha256"`
	ErrorCode       string    `gorm:"size:100" json:"error_code"`
	ErrorMessage    string    `gorm:"type:text" json:"error_message"`
	Source          string    `gorm:"size:20;not null" json:"source"`
	CreatedAt       time.Time `json:"created_at"`
}

func (UpdateEvent) TableName() string {
	return "update_event"
}
