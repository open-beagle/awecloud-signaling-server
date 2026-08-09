package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func newUpdateServiceForTest(t *testing.T) (*UpdateService, *gorm.DB) {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.Node{},
		&model.Endpoint{},
		&model.Release{},
		&model.Artifact{},
		&model.UpdateTask{},
		&model.UpdateEvent{},
	))
	return NewUpdateService(database), database
}

func TestCreateTaskAndDeliverDirective(t *testing.T) {
	updates, database := newUpdateServiceForTest(t)
	ctx := context.Background()
	platform, err := json.Marshal(model.NodeSystemInfo{OS: "linux", Arch: "amd64"})
	require.NoError(t, err)
	node := model.Node{UserID: 1, Name: "beijing", Type: model.NodeTypeAgent, SystemInfo: string(platform), UpdaterProtocol: "v2"}
	require.NoError(t, database.Create(&node).Error)

	release := model.Release{ID: uuid.NewString(), Component: model.ComponentAgent, Version: "v1.2.3", CommitID: "1111111111111111111111111111111111111111", Channel: "stable", Status: model.ReleaseStatusPublished}
	require.NoError(t, database.Create(&release).Error)
	artifact := model.Artifact{
		ID:          uuid.NewString(),
		ReleaseID:   release.ID,
		OS:          "linux",
		Arch:        "amd64",
		PackageType: "binary",
		Filename:    "signal_agent-v1.2.3-linux-amd64",
		DownloadURL: "https://download.example.com/signal_agent-v1.2.3-linux-amd64",
		Size:        128,
		SHA256:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Status:      model.ArtifactStatusAvailable,
	}
	require.NoError(t, database.Create(&artifact).Error)

	task, err := updates.CreateTask(ctx, CreateUpdateTaskInput{Component: model.ComponentAgent, TargetType: model.UpdateTargetNode, TargetID: "1", ReleaseID: release.ID})
	require.NoError(t, err)
	directives, err := updates.DirectivesForNode(ctx, node.ID, model.ComponentAgent)
	require.NoError(t, err)
	require.Len(t, directives, 1)
	require.Equal(t, task.ID, directives[0].TaskID)
	require.Equal(t, release.CommitID, directives[0].CommitID)
	require.Equal(t, artifact.SHA256, directives[0].SHA256)

	var persisted model.UpdateTask
	require.NoError(t, database.First(&persisted, "id = ?", task.ID).Error)
	require.Equal(t, model.UpdateTaskDelivered, persisted.Status)
	require.Equal(t, artifact.ID, persisted.ArtifactID)
	require.Equal(t, release.CommitID, persisted.DesiredCommitID)
	require.Equal(t, artifact.SHA256, persisted.DesiredSHA256)
	_, err = updates.CreateTask(ctx, CreateUpdateTaskInput{Component: model.ComponentAgent, TargetType: model.UpdateTargetNode, TargetID: "1", ReleaseID: release.ID})
	require.ErrorIs(t, err, ErrActiveTaskExists)
}

func TestCreateTaskRequiresUpdaterProtocolV2(t *testing.T) {
	updates, database := newUpdateServiceForTest(t)
	platform, err := json.Marshal(model.NodeSystemInfo{OS: "linux", Arch: "amd64"})
	require.NoError(t, err)
	node := model.Node{UserID: 1, Name: "legacy", Type: model.NodeTypeAgent, SystemInfo: string(platform)}
	require.NoError(t, database.Create(&node).Error)
	release := model.Release{ID: uuid.NewString(), Component: model.ComponentAgent, Version: "v1.2.3", CommitID: "1111111111111111111111111111111111111111", Status: model.ReleaseStatusPublished}
	require.NoError(t, database.Create(&release).Error)

	_, err = updates.CreateTask(context.Background(), CreateUpdateTaskInput{Component: model.ComponentAgent, TargetType: model.UpdateTargetNode, TargetID: "1", ReleaseID: release.ID})
	require.ErrorIs(t, err, ErrUpdaterUnsupported)
}

func TestCreateTaskSupportsDesktop(t *testing.T) {
	updates, database := newUpdateServiceForTest(t)
	ctx := context.Background()
	desktopNode := model.Node{UserID: 1, Name: "desktop-1", Type: model.NodeTypeDesktop, SystemInfo: `{"os":"linux","arch":"amd64"}`, UpdaterProtocol: "v2"}
	require.NoError(t, database.Create(&desktopNode).Error)

	release := model.Release{ID: uuid.NewString(), Component: model.ComponentDesktop, Version: "v1.2.3", CommitID: "1111111111111111111111111111111111111111", Status: model.ReleaseStatusPublished}
	require.NoError(t, database.Create(&release).Error)

	artifact := model.Artifact{
		ID:          uuid.NewString(),
		ReleaseID:   release.ID,
		Role:        "app",
		OS:          "linux",
		Arch:        "amd64",
		PackageType: "binary",
		Filename:    "awecloud-signaling-desktop",
		DownloadURL: "https://download.example.com/awecloud-signaling-desktop",
		Size:        128,
		SHA256:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Status:      model.ArtifactStatusAvailable,
	}
	require.NoError(t, database.Create(&artifact).Error)

	task, err := updates.CreateTask(ctx, CreateUpdateTaskInput{Component: model.ComponentDesktop, TargetType: model.UpdateTargetNode, TargetID: "1", ReleaseID: release.ID})
	require.NoError(t, err)
	require.Equal(t, model.ComponentDesktop, task.Component)
}

func TestUpdateStatusSequenceIsMonotonic(t *testing.T) {
	updates, database := newUpdateServiceForTest(t)
	task := model.UpdateTask{ID: uuid.NewString(), Component: model.ComponentAgent, TargetType: model.UpdateTargetNode, TargetID: "1", ReleaseID: uuid.NewString(), DesiredVersion: "v1.2.3", Status: model.UpdateTaskDelivered, MaxAttempts: 3}
	require.NoError(t, database.Create(&task).Error)
	reporter := UpdateStatusReporter{Source: "agent", Component: model.ComponentAgent, TargetType: model.UpdateTargetNode, TargetID: "1"}
	require.NoError(t, updates.Report(context.Background(), task.ID, reporter, UpdateStatusReport{Phase: string(model.UpdateTaskDownloading), Progress: 40, Sequence: 2}))
	require.NoError(t, updates.Report(context.Background(), task.ID, reporter, UpdateStatusReport{Phase: string(model.UpdateTaskAccepted), Sequence: 1}))

	var persisted model.UpdateTask
	require.NoError(t, database.First(&persisted, "id = ?", task.ID).Error)
	require.Equal(t, model.UpdateTaskDownloading, persisted.Status)
	var events []model.UpdateEvent
	require.NoError(t, database.Where("task_id = ?", task.ID).Find(&events).Error)
	require.Len(t, events, 1)
	require.EqualValues(t, 2, events[0].Sequence)
}

func TestSucceededStatusRequiresTargetVersion(t *testing.T) {
	updates, database := newUpdateServiceForTest(t)
	task := model.UpdateTask{ID: uuid.NewString(), Component: model.ComponentAgent, TargetType: model.UpdateTargetNode, TargetID: "1", ReleaseID: uuid.NewString(), DesiredVersion: "v1.2.3", Status: model.UpdateTaskRestarting, MaxAttempts: 3}
	require.NoError(t, database.Create(&task).Error)
	reporter := UpdateStatusReporter{Source: "agent", Component: model.ComponentAgent, TargetType: model.UpdateTargetNode, TargetID: "1"}
	require.NoError(t, updates.Report(context.Background(), task.ID, reporter, UpdateStatusReport{Phase: string(model.UpdateTaskSucceeded), CurrentVersion: "v1.2.2", Sequence: 1}))

	var persisted model.UpdateTask
	require.NoError(t, database.First(&persisted, "id = ?", task.ID).Error)
	require.Equal(t, model.UpdateTaskFailed, persisted.Status)
	require.Equal(t, "version_mismatch", persisted.LastErrorCode)
}

func TestSucceededStatusRequiresCommitAndArtifact(t *testing.T) {
	updates, database := newUpdateServiceForTest(t)
	task := model.UpdateTask{
		ID: uuid.NewString(), Component: model.ComponentAgent, TargetType: model.UpdateTargetNode, TargetID: "1",
		ReleaseID: uuid.NewString(), DesiredVersion: "v1.2.3", DesiredCommitID: "1111111111111111111111111111111111111111",
		DesiredSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Status:        model.UpdateTaskRestarting, MaxAttempts: 3,
	}
	require.NoError(t, database.Create(&task).Error)
	reporter := UpdateStatusReporter{Source: "agent", Component: model.ComponentAgent, TargetType: model.UpdateTargetNode, TargetID: "1"}
	require.NoError(t, updates.Report(context.Background(), task.ID, reporter, UpdateStatusReport{
		Phase: string(model.UpdateTaskSucceeded), CurrentVersion: task.DesiredVersion,
		CurrentCommitID: "2222222222222222222222222222222222222222", CurrentSHA256: task.DesiredSHA256, Sequence: 1,
	}))

	var persisted model.UpdateTask
	require.NoError(t, database.First(&persisted, "id = ?", task.ID).Error)
	require.Equal(t, model.UpdateTaskFailed, persisted.Status)
	require.Equal(t, "commit_mismatch", persisted.LastErrorCode)
}

func TestReportRejectsMismatchedReporter(t *testing.T) {
	updates, database := newUpdateServiceForTest(t)
	task := model.UpdateTask{ID: uuid.NewString(), Component: model.ComponentAgent, TargetType: model.UpdateTargetNode, TargetID: "1", ReleaseID: uuid.NewString(), DesiredVersion: "v1.2.3", Status: model.UpdateTaskDelivered, MaxAttempts: 3}
	require.NoError(t, database.Create(&task).Error)

	err := updates.Report(context.Background(), task.ID, UpdateStatusReporter{Source: "agent", Component: model.ComponentAgent, TargetType: model.UpdateTargetNode, TargetID: "2"}, UpdateStatusReport{Phase: string(model.UpdateTaskSucceeded), CurrentVersion: "v1.2.3", Sequence: 1})
	require.ErrorIs(t, err, ErrUpdateReporterMismatch)
}

func TestReportRejectsBackwardTransition(t *testing.T) {
	updates, database := newUpdateServiceForTest(t)
	task := model.UpdateTask{ID: uuid.NewString(), Component: model.ComponentAgent, TargetType: model.UpdateTargetNode, TargetID: "1", ReleaseID: uuid.NewString(), DesiredVersion: "v1.2.3", Status: model.UpdateTaskDownloading, MaxAttempts: 3}
	require.NoError(t, database.Create(&task).Error)

	reporter := UpdateStatusReporter{Source: "agent", Component: model.ComponentAgent, TargetType: model.UpdateTargetNode, TargetID: "1"}
	err := updates.Report(context.Background(), task.ID, reporter, UpdateStatusReport{Phase: string(model.UpdateTaskAccepted), Sequence: 1})
	require.ErrorIs(t, err, ErrInvalidUpdateTransition)
}

func TestRetryCreatesNewTaskID(t *testing.T) {
	updates, database := newUpdateServiceForTest(t)
	release := model.Release{ID: uuid.NewString(), Component: model.ComponentAgent, Version: "v1.2.3", Status: model.ReleaseStatusPublished}
	require.NoError(t, database.Create(&release).Error)
	original := model.UpdateTask{ID: uuid.NewString(), Component: model.ComponentAgent, TargetType: model.UpdateTargetNode, TargetID: "1", TargetName: "beijing", ReleaseID: release.ID, DesiredVersion: "v1.2.3", Status: model.UpdateTaskFailed, Attempt: 1, MaxAttempts: 3, CreatedBy: 42}
	require.NoError(t, database.Create(&original).Error)

	retry, err := updates.Retry(context.Background(), original.ID)
	require.NoError(t, err)
	require.NotEqual(t, original.ID, retry.ID)
	require.Equal(t, original.ID, retry.RetryOfTaskID)
	require.Equal(t, model.UpdateTaskPending, retry.Status)
	require.Equal(t, original.Attempt, retry.Attempt)
	require.Nil(t, retry.ScheduledAt)
	require.Nil(t, retry.DeadlineAt)

	var persistedOriginal model.UpdateTask
	require.NoError(t, database.First(&persistedOriginal, "id = ?", original.ID).Error)
	require.Equal(t, model.UpdateTaskFailed, persistedOriginal.Status)
}

func TestRetryRejectsUnpublishedRelease(t *testing.T) {
	updates, database := newUpdateServiceForTest(t)
	release := model.Release{ID: uuid.NewString(), Component: model.ComponentAgent, Version: "v1.2.3", Status: model.ReleaseStatusRevoked}
	require.NoError(t, database.Create(&release).Error)
	task := model.UpdateTask{ID: uuid.NewString(), Component: model.ComponentAgent, TargetType: model.UpdateTargetNode, TargetID: "1", ReleaseID: release.ID, DesiredVersion: release.Version, Status: model.UpdateTaskFailed, MaxAttempts: 3}
	require.NoError(t, database.Create(&task).Error)

	_, err := updates.Retry(context.Background(), task.ID)
	require.ErrorIs(t, err, ErrReleaseNotPublished)
}
