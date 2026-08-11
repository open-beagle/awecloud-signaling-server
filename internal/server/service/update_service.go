package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var (
	ErrReleaseNotPublished     = errors.New("release is not published")
	ErrArtifactNotFound        = errors.New("matching artifact not found")
	ErrActiveTaskExists        = errors.New("an active update task already exists for this target")
	ErrInvalidUpdatePhase      = errors.New("invalid update phase")
	ErrComponentNotSupported   = errors.New("component updater is not implemented")
	ErrUpdateReporterMismatch  = errors.New("update status reporter does not match task target")
	ErrInvalidUpdateTransition = errors.New("invalid update status transition")
	ErrInvalidBuildIdentity    = errors.New("release or artifact build identity is invalid")
)

type UpdateDirective struct {
	TaskID        string
	Component     string
	Version       string
	ArtifactID    string
	DownloadURL   string
	Filename      string
	OS            string
	Arch          string
	Size          int64
	SHA256        string
	Force         bool
	NotBeforeUnix int64
	DeadlineUnix  int64
	Action        string
	TargetName    string
	CommitID      string
}

type UpdateStatusReport struct {
	TaskID          string
	Phase           string
	Progress        int
	CurrentVersion  string
	Sequence        int64
	ErrorCode       string
	ErrorMessage    string
	CurrentCommitID string
	CurrentSHA256   string
}

type UpdateStatusReporter struct {
	Source     string
	Component  model.Component
	TargetType model.UpdateTargetType
	TargetID   string
}

type CreateUpdateTaskInput struct {
	Component   model.Component
	TargetType  model.UpdateTargetType
	TargetID    string
	ReleaseID   string
	Force       bool
	ScheduledAt *time.Time
	DeadlineAt  *time.Time
	MaxAttempts int
	CreatedBy   uint64
}

type UpdateService struct {
	db *gorm.DB
}

func NewUpdateService(database *gorm.DB) *UpdateService {
	return &UpdateService{db: database}
}

func (s *UpdateService) CreateTask(ctx context.Context, input CreateUpdateTaskInput) (*model.UpdateTask, error) {
	if !input.Component.Valid() {
		return nil, fmt.Errorf("unsupported component %q", input.Component)
	}
	if input.TargetType != model.UpdateTargetNode && input.TargetType != model.UpdateTargetEndpoint {
		return nil, fmt.Errorf("unsupported target type %q", input.TargetType)
	}
	if input.TargetType == model.UpdateTargetEndpoint && input.Component != model.ComponentEndpoint {
		return nil, fmt.Errorf("endpoint targets only support the endpoint component")
	}
	if input.TargetType == model.UpdateTargetNode && input.Component == model.ComponentEndpoint {
		return nil, fmt.Errorf("endpoint component requires an endpoint target")
	}
	if input.MaxAttempts <= 0 {
		input.MaxAttempts = 3
	}

	var result model.UpdateTask
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var release model.Release
		if err := tx.First(&release, "id = ?", input.ReleaseID).Error; err != nil {
			return err
		}
		if release.Component != input.Component || release.Status != model.ReleaseStatusPublished {
			return ErrReleaseNotPublished
		}
		requiresBuildIdentity := input.Component == model.ComponentAgent || input.Component == model.ComponentEndpoint
		if requiresBuildIdentity && !validAgentCommitID(release.CommitID) {
			return ErrInvalidBuildIdentity
		}

		name, osName, arch, err := s.targetPlatform(tx, input.TargetType, input.TargetID)
		if err != nil {
			return err
		}
		artifact, err := s.findArtifactRole(tx, release.ID, osName, arch, input.Component)
		if err != nil {
			return err
		}
		if requiresBuildIdentity && !validAgentSHA256(artifact.SHA256) {
			return ErrInvalidBuildIdentity
		}

		var activeCount int64
		if err := tx.Model(&model.UpdateTask{}).
			Where("component = ? AND target_type = ? AND target_id = ? AND status NOT IN ?", input.Component, input.TargetType, input.TargetID,
				[]model.UpdateTaskStatus{model.UpdateTaskSucceeded, model.UpdateTaskFailed, model.UpdateTaskRolledBack, model.UpdateTaskCancelled, model.UpdateTaskExpired}).
			Count(&activeCount).Error; err != nil {
			return err
		}
		if activeCount > 0 {
			return ErrActiveTaskExists
		}

		result = model.UpdateTask{
			ID:              uuid.NewString(),
			Component:       input.Component,
			TargetType:      input.TargetType,
			TargetID:        input.TargetID,
			TargetName:      name,
			ReleaseID:       release.ID,
			ArtifactID:      artifact.ID,
			DesiredVersion:  release.Version,
			DesiredCommitID: release.CommitID,
			DesiredSHA256:   artifact.SHA256,
			Force:           input.Force,
			ScheduledAt:     input.ScheduledAt,
			DeadlineAt:      input.DeadlineAt,
			Status:          model.UpdateTaskPending,
			MaxAttempts:     input.MaxAttempts,
			CreatedBy:       input.CreatedBy,
		}
		if err := tx.Create(&result).Error; err != nil {
			return err
		}
		return s.recordEvent(tx, result.ID, 0, string(model.UpdateTaskPending), 0, "", "", "", "", "", "server")
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *UpdateService) DirectivesForNode(ctx context.Context, nodeID uint64, component model.Component) ([]UpdateDirective, error) {
	return s.directivesForTarget(ctx, model.UpdateTargetNode, strconv.FormatUint(nodeID, 10), component)
}

func (s *UpdateService) DirectivesForEndpoint(ctx context.Context, endpointID string) ([]UpdateDirective, error) {
	return s.directivesForTarget(ctx, model.UpdateTargetEndpoint, endpointID, model.ComponentEndpoint)
}

func (s *UpdateService) DirectivesForAgentEndpoints(ctx context.Context, agentID uint64) ([]UpdateDirective, error) {
	var endpoints []model.Endpoint
	if err := s.db.WithContext(ctx).Where("user_id = ? AND revoked = ?", agentID, false).Find(&endpoints).Error; err != nil {
		return nil, err
	}
	var result []UpdateDirective
	for _, endpoint := range endpoints {
		directives, err := s.DirectivesForEndpoint(ctx, endpoint.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, directives...)
	}
	return result, nil
}

func (s *UpdateService) directivesForTarget(ctx context.Context, targetType model.UpdateTargetType, targetID string, component model.Component) ([]UpdateDirective, error) {
	var tasks []model.UpdateTask
	now := time.Now()
	if err := s.db.WithContext(ctx).Preload("Release").
		Where("target_type = ? AND target_id = ? AND component = ? AND status NOT IN ?", targetType, targetID, component,
			[]model.UpdateTaskStatus{model.UpdateTaskSucceeded, model.UpdateTaskFailed, model.UpdateTaskRolledBack, model.UpdateTaskCancelled, model.UpdateTaskExpired}).
		Order("created_at ASC").Find(&tasks).Error; err != nil {
		return nil, err
	}

	result := make([]UpdateDirective, 0, len(tasks))
	for i := range tasks {
		task := &tasks[i]
		if task.DeadlineAt != nil && now.After(*task.DeadlineAt) {
			_ = s.expireTask(ctx, task.ID)
			continue
		}
		if task.ScheduledAt != nil && now.Before(*task.ScheduledAt) {
			continue
		}
		if task.Release == nil || task.Release.Status != model.ReleaseStatusPublished {
			continue
		}

		_, osName, arch, err := s.targetPlatform(s.db.WithContext(ctx), task.TargetType, task.TargetID)
		if err != nil {
			continue
		}
		var artifact model.Artifact
		if err := s.db.WithContext(ctx).First(&artifact, "id = ? AND status = ?", task.ArtifactID, model.ArtifactStatusAvailable).Error; err != nil {
			continue
		}
		if artifact.ReleaseID != task.ReleaseID || artifact.OS != osName || artifact.Arch != arch ||
			artifact.SHA256 != task.DesiredSHA256 {
			_ = s.setTaskStatus(ctx, task.ID, model.UpdateTaskFailed, "artifact_identity_conflict", "任务快照与固定 Artifact 不一致")
			continue
		}
		if err := s.markDelivered(ctx, task.ID); err != nil {
			return nil, err
		}

		directive := UpdateDirective{
			TaskID:      task.ID,
			Component:   string(task.Component),
			Version:     task.DesiredVersion,
			ArtifactID:  artifact.ID,
			DownloadURL: artifact.DownloadURL,
			Filename:    artifact.Filename,
			OS:          artifact.OS,
			Arch:        artifact.Arch,
			Size:        artifact.Size,
			SHA256:      artifact.SHA256,
			Force:       task.Force,
			Action:      "install",
			TargetName:  task.TargetName,
			CommitID:    task.DesiredCommitID,
		}
		if task.ScheduledAt != nil {
			directive.NotBeforeUnix = task.ScheduledAt.Unix()
		}
		if task.DeadlineAt != nil {
			directive.DeadlineUnix = task.DeadlineAt.Unix()
		}
		result = append(result, directive)
	}
	return result, nil
}

func (s *UpdateService) Report(ctx context.Context, taskID string, reporter UpdateStatusReporter, report UpdateStatusReport) error {
	statusValue := model.UpdateTaskStatus(report.Phase)
	if !validUpdateStatus(statusValue) {
		return ErrInvalidUpdatePhase
	}
	if report.Progress < 0 {
		report.Progress = 0
	}
	if report.Progress > 100 {
		report.Progress = 100
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.UpdateTask
		if err := tx.First(&task, "id = ?", taskID).Error; err != nil {
			return err
		}
		if task.Component != reporter.Component || task.TargetType != reporter.TargetType || task.TargetID != reporter.TargetID {
			return ErrUpdateReporterMismatch
		}
		if task.Status.Terminal() {
			return nil
		}
		if statusValue == model.UpdateTaskSucceeded {
			if report.CurrentVersion != task.DesiredVersion {
				statusValue = model.UpdateTaskFailed
				report.Phase = string(model.UpdateTaskFailed)
				report.ErrorCode = "version_mismatch"
				report.ErrorMessage = fmt.Sprintf("expected version %s, got %s", task.DesiredVersion, report.CurrentVersion)
			} else if report.CurrentCommitID != task.DesiredCommitID {
				statusValue = model.UpdateTaskFailed
				report.Phase = string(model.UpdateTaskFailed)
				report.ErrorCode = "commit_mismatch"
				report.ErrorMessage = fmt.Sprintf("expected commit %s, got %s", task.DesiredCommitID, report.CurrentCommitID)
			} else if report.CurrentSHA256 != task.DesiredSHA256 {
				statusValue = model.UpdateTaskFailed
				report.Phase = string(model.UpdateTaskFailed)
				report.ErrorCode = "artifact_mismatch"
				report.ErrorMessage = fmt.Sprintf("expected sha256 %s, got %s", task.DesiredSHA256, report.CurrentSHA256)
			}
		}

		var latest model.UpdateEvent
		err := tx.Where("task_id = ?", taskID).Order("sequence DESC, id DESC").First(&latest).Error
		if err == nil && report.Sequence > 0 && report.Sequence <= latest.Sequence {
			return nil
		}
		if !canReportTransition(task.Status, statusValue) {
			return ErrInvalidUpdateTransition
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if report.Sequence == 0 {
			report.Sequence = latest.Sequence + 1
		}

		now := time.Now()
		updates := map[string]any{
			"status":             statusValue,
			"last_reported_at":   now,
			"last_error_code":    report.ErrorCode,
			"last_error_message": report.ErrorMessage,
		}
		if statusValue == model.UpdateTaskAccepted {
			updates["attempt"] = task.Attempt + 1
		}
		if err := tx.Model(&task).Updates(updates).Error; err != nil {
			return err
		}
		return s.recordEvent(tx, taskID, report.Sequence, report.Phase, report.Progress, report.CurrentVersion, report.CurrentCommitID, report.CurrentSHA256, report.ErrorCode, report.ErrorMessage, reporter.Source)
	})
}

func validAgentCommitID(value string) bool {
	if len(value) != 40 || value != strings.ToLower(value) {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func validAgentSHA256(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func (s *UpdateService) Cancel(ctx context.Context, taskID string) error {
	return s.setTaskStatus(ctx, taskID, model.UpdateTaskCancelled, "task_cancelled", "任务已取消")
}

func (s *UpdateService) Retry(ctx context.Context, taskID string) (*model.UpdateTask, error) {
	var retry model.UpdateTask
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.UpdateTask
		if err := tx.First(&task, "id = ?", taskID).Error; err != nil {
			return err
		}
		if task.Status != model.UpdateTaskFailed && task.Status != model.UpdateTaskRolledBack && task.Status != model.UpdateTaskExpired {
			return fmt.Errorf("task %s cannot be retried from status %s", taskID, task.Status)
		}
		if task.Attempt >= task.MaxAttempts {
			return fmt.Errorf("task %s exceeded max attempts", taskID)
		}
		var release model.Release
		if err := tx.First(&release, "id = ?", task.ReleaseID).Error; err != nil {
			return err
		}
		if release.Component != task.Component || release.Status != model.ReleaseStatusPublished {
			return ErrReleaseNotPublished
		}

		var activeCount int64
		if err := tx.Model(&model.UpdateTask{}).
			Where("component = ? AND target_type = ? AND target_id = ? AND status NOT IN ?", task.Component, task.TargetType, task.TargetID,
				[]model.UpdateTaskStatus{model.UpdateTaskSucceeded, model.UpdateTaskFailed, model.UpdateTaskRolledBack, model.UpdateTaskCancelled, model.UpdateTaskExpired}).
			Count(&activeCount).Error; err != nil {
			return err
		}
		if activeCount > 0 {
			return ErrActiveTaskExists
		}

		retry = model.UpdateTask{
			ID:              uuid.NewString(),
			Component:       task.Component,
			TargetType:      task.TargetType,
			TargetID:        task.TargetID,
			TargetName:      task.TargetName,
			ReleaseID:       task.ReleaseID,
			ArtifactID:      task.ArtifactID,
			DesiredVersion:  task.DesiredVersion,
			DesiredCommitID: task.DesiredCommitID,
			DesiredSHA256:   task.DesiredSHA256,
			Force:           task.Force,
			Status:          model.UpdateTaskPending,
			Attempt:         task.Attempt,
			MaxAttempts:     task.MaxAttempts,
			CreatedBy:       task.CreatedBy,
			RetryOfTaskID:   task.ID,
		}
		if err := tx.Create(&retry).Error; err != nil {
			return err
		}
		return s.recordEvent(tx, retry.ID, 0, string(model.UpdateTaskPending), 0, "", "", "", "", "retry of "+task.ID, "server")
	})
	if err != nil {
		return nil, err
	}
	return &retry, nil
}

func (s *UpdateService) markDelivered(ctx context.Context, taskID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.UpdateTask
		if err := tx.First(&task, "id = ?", taskID).Error; err != nil {
			return err
		}
		if task.Status != model.UpdateTaskPending {
			return nil
		}
		now := time.Now()
		if err := tx.Model(&task).Updates(map[string]any{"status": model.UpdateTaskDelivered, "last_delivered_at": now}).Error; err != nil {
			return err
		}
		return s.recordEvent(tx, taskID, 0, string(model.UpdateTaskDelivered), 0, "", "", "", "", "", "server")
	})
}

func (s *UpdateService) expireTask(ctx context.Context, taskID string) error {
	return s.setTaskStatus(ctx, taskID, model.UpdateTaskExpired, "task_expired", "任务已超过截止时间")
}

func (s *UpdateService) setTaskStatus(ctx context.Context, taskID string, next model.UpdateTaskStatus, errorCode, errorMessage string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.UpdateTask
		if err := tx.First(&task, "id = ?", taskID).Error; err != nil {
			return err
		}
		if task.Status.Terminal() {
			return nil
		}
		if err := tx.Model(&task).Updates(map[string]any{
			"status":             next,
			"last_error_code":    errorCode,
			"last_error_message": errorMessage,
		}).Error; err != nil {
			return err
		}
		return s.recordEvent(tx, task.ID, 0, string(next), 0, "", "", "", errorCode, errorMessage, "server")
	})
}

func (s *UpdateService) targetPlatform(tx *gorm.DB, targetType model.UpdateTargetType, targetID string) (name, osName, arch string, err error) {
	switch targetType {
	case model.UpdateTargetNode:
		id, parseErr := strconv.ParseUint(targetID, 10, 64)
		if parseErr != nil {
			return "", "", "", fmt.Errorf("invalid node target id: %w", parseErr)
		}
		var node model.Node
		if err := tx.First(&node, id).Error; err != nil {
			return "", "", "", err
		}
		if node.Type != model.NodeTypeAgent && node.Type != model.NodeTypeDesktop {
			return "", "", "", fmt.Errorf("node %d is neither agent nor desktop target", id)
		}
		var info model.NodeSystemInfo
		if node.SystemInfo != "" {
			_ = json.Unmarshal([]byte(node.SystemInfo), &info)
		}
		if info.OS == "" || info.Arch == "" {
			return "", "", "", fmt.Errorf("node %d has no reported os/arch", id)
		}
		return node.Name, strings.ToLower(info.OS), strings.ToLower(info.Arch), nil
	case model.UpdateTargetEndpoint:
		var endpoint model.Endpoint
		if err := tx.First(&endpoint, "id = ?", targetID).Error; err != nil {
			return "", "", "", err
		}
		if endpoint.OS == "" || endpoint.Arch == "" {
			return "", "", "", fmt.Errorf("endpoint %s has no reported os/arch", targetID)
		}
		return endpoint.Name, strings.ToLower(endpoint.OS), strings.ToLower(endpoint.Arch), nil
	default:
		return "", "", "", fmt.Errorf("unsupported target type %q", targetType)
	}
}

func (s *UpdateService) findArtifact(tx *gorm.DB, releaseID, osName, arch string) (*model.Artifact, error) {
	return s.findArtifactRole(tx, releaseID, osName, arch, "")
}

func (s *UpdateService) findArtifactRole(tx *gorm.DB, releaseID, osName, arch string, comp model.Component) (*model.Artifact, error) {
	var artifact model.Artifact
	query := tx.Where("release_id = ? AND os = ? AND arch = ? AND status = ?", releaseID, osName, arch, model.ArtifactStatusAvailable)
	if comp == model.ComponentDesktop {
		query = query.Where("role = ?", "app")
	}
	if err := query.First(&artifact).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrArtifactNotFound
		}
		return nil, err
	}
	return &artifact, nil
}

func (s *UpdateService) recordEvent(tx *gorm.DB, taskID string, sequence int64, phase string, progress int, version, commitID, sha256, errorCode, errorMessage, source string) error {
	return tx.Create(&model.UpdateEvent{
		TaskID:          taskID,
		Sequence:        sequence,
		Phase:           phase,
		Progress:        progress,
		RunningVersion:  version,
		RunningCommitID: commitID,
		RunningSHA256:   sha256,
		ErrorCode:       errorCode,
		ErrorMessage:    errorMessage,
		Source:          source,
	}).Error
}

func validUpdateStatus(status model.UpdateTaskStatus) bool {
	switch status {
	case model.UpdateTaskAccepted, model.UpdateTaskDownloading, model.UpdateTaskVerifying,
		model.UpdateTaskStaged, model.UpdateTaskInstalling, model.UpdateTaskRestarting,
		model.UpdateTaskSucceeded, model.UpdateTaskFailed, model.UpdateTaskRolledBack:
		return true
	default:
		return false
	}
}

func canReportTransition(current, next model.UpdateTaskStatus) bool {
	if current == next || next == model.UpdateTaskFailed || next == model.UpdateTaskRolledBack {
		return true
	}
	return updateStatusRank(next) >= updateStatusRank(current)
}

func updateStatusRank(status model.UpdateTaskStatus) int {
	switch status {
	case model.UpdateTaskPending:
		return 0
	case model.UpdateTaskDelivered:
		return 1
	case model.UpdateTaskAccepted:
		return 2
	case model.UpdateTaskDownloading:
		return 3
	case model.UpdateTaskVerifying:
		return 4
	case model.UpdateTaskStaged:
		return 5
	case model.UpdateTaskInstalling:
		return 6
	case model.UpdateTaskRestarting:
		return 7
	case model.UpdateTaskSucceeded:
		return 8
	default:
		return -1
	}
}
