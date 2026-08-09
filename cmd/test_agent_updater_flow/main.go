package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
	"github.com/open-beagle/awecloud-signaling-server/internal/updater"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func main() {
	fmt.Println("=== Starting Real Environment Agent v1.0.2 Upgrade Test ===")

	tmpDir, err := os.MkdirTemp("", "agent-upgrade-test-*")
	if err != nil {
		log.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		log.Fatalf("Open SQLite failed: %v", err)
	}

	if err := db.AutoMigrate(
		&model.Node{},
		&model.Endpoint{},
		&model.Release{},
		&model.Artifact{},
		&model.UpdateTask{},
		&model.UpdateEvent{},
	); err != nil {
		log.Fatalf("AutoMigrate failed: %v", err)
	}

	// 1. Create running Node reporting v1.0.1
	sysInfo, _ := json.Marshal(model.NodeSystemInfo{OS: "linux", Arch: "amd64", Hostname: "agent-test-host"})
	node := model.Node{
		ID:              1001,
		UserID:          10,
		Name:            "agent-test-node",
		Type:            model.NodeTypeAgent,
		Version:         "v1.0.1",
		CommitID:        "1111111111111111111111111111111111111111",
		BinarySHA256:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		UpdaterProtocol: "v2",
		SystemInfo:      string(sysInfo),
	}
	if err := db.Create(&node).Error; err != nil {
		log.Fatalf("Create test node failed: %v", err)
	}
	fmt.Printf("[Step 1] Initial Node registered: ID=%d, Version=%s, Commit=%s\n", node.ID, node.Version, node.CommitID)

	// 2. Create v1.0.2 Release and Artifact
	version := "v1.0.2"
	targetCommit := "f77de3d30dc27481a72c113e22dd16f68471dedc"
	targetSHA256 := "9d2d7f63bf0c0b865da7047502f9cb72845ee71fc5c0ab9f7f79299b604db7c1"

	release := model.Release{
		ID:        uuid.NewString(),
		Component: model.ComponentAgent,
		Version:   version,
		CommitID:  targetCommit,
		Channel:   "stable",
		Status:    model.ReleaseStatusPublished,
	}
	if err := db.Create(&release).Error; err != nil {
		log.Fatalf("Create release failed: %v", err)
	}

	artifact := model.Artifact{
		ID:          uuid.NewString(),
		ReleaseID:   release.ID,
		OS:          "linux",
		Arch:        "amd64",
		PackageType: "binary",
		Filename:    "signal_agent",
		DownloadURL: "https://signal.wodcloud.com/api/v1/download/agent?os=linux&arch=amd64&version=v1.0.2",
		Size:        83169442,
		SHA256:      targetSHA256,
		Status:      model.ArtifactStatusAvailable,
	}
	if err := db.Create(&artifact).Error; err != nil {
		log.Fatalf("Create artifact failed: %v", err)
	}
	fmt.Printf("[Step 2] Release v1.0.2 created: ReleaseID=%s, ArtifactID=%s\n", release.ID, artifact.ID)

	// 4. UpdateService create update task
	updateSvc := service.NewUpdateService(db)
	ctx := context.Background()
	task, err := updateSvc.CreateTask(ctx, service.CreateUpdateTaskInput{
		Component:  model.ComponentAgent,
		TargetType: model.UpdateTargetNode,
		TargetID:   "1001",
		ReleaseID:  release.ID,
		Force:      true,
	})
	if err != nil {
		log.Fatalf("CreateTask failed: %v", err)
	}
	fmt.Printf("[Step 3] UpdateTask created: TaskID=%s, DesiredVersion=%s, DesiredCommitID=%s, DesiredSHA256=%s\n",
		task.ID, task.DesiredVersion, task.DesiredCommitID, task.DesiredSHA256)

	// 5. Query directives for node
	directives, err := updateSvc.DirectivesForNode(ctx, node.ID, model.ComponentAgent)
	if err != nil || len(directives) == 0 {
		log.Fatalf("DirectivesForNode failed: err=%v, count=%d", err, len(directives))
	}
	directive := directives[0]
	fmt.Printf("[Step 4] Directive retrieved by Agent: TaskID=%s, CommitID=%s, SHA256=%s\n",
		directive.TaskID, directive.CommitID, directive.SHA256)

	// 6. Agent receives directive and initializes updater Manager
	stateDir := filepath.Join(tmpDir, "updater_agent_state")
	updaterMgr, err := updater.NewManager(updater.Config{
		Component:       "agent",
		CurrentVersion:  "v1.0.1",
		CurrentCommitID: "1111111111111111111111111111111111111111",
		CurrentSHA256:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		StateDir:        stateDir,
		CurrentLink:     filepath.Join(tmpDir, "bin", "signal_agent"),
		ServiceName:     "k8s-signaling",
		Executable:      "/bin/true",
	})
	if err != nil {
		log.Fatalf("NewManager failed: %v", err)
	}

	// Report status transitions: accepted -> downloading -> staged -> restarting
	reporter := service.UpdateStatusReporter{
		Source:     "agent",
		Component:  model.ComponentAgent,
		TargetType: model.UpdateTargetNode,
		TargetID:   "1001",
	}

	_ = updateSvc.Report(ctx, task.ID, reporter, service.UpdateStatusReport{Phase: "accepted", Sequence: 1})
	_ = updateSvc.Report(ctx, task.ID, reporter, service.UpdateStatusReport{Phase: "downloading", Sequence: 2, Progress: 50})
	_ = updateSvc.Report(ctx, task.ID, reporter, service.UpdateStatusReport{Phase: "staged", Sequence: 3, Progress: 100})
	_ = updateSvc.Report(ctx, task.ID, reporter, service.UpdateStatusReport{Phase: "restarting", Sequence: 4, Progress: 100})

	var restartingTask model.UpdateTask
	db.First(&restartingTask, "id = ?", task.ID)
	fmt.Printf("[Step 5] Agent reported status: %s\n", restartingTask.Status)

	// 7. Simulating Agent restarting heartbeat to Server
	// Update Node record in DB to new version/commit/sha256 (as Agent heartbeat does)
	db.Model(&model.Node{}).Where("id = ?", node.ID).Updates(map[string]any{
		"version":       version,
		"commit_id":     targetCommit,
		"binary_sha256": targetSHA256,
	})

	// Server evaluates restarting tasks and generates UpdateHealthConfirmation
	var activeRestartingTasks []model.UpdateTask
	db.Where("target_type = ? AND target_id = ? AND component = ? AND status = ?",
		model.UpdateTargetNode, "1001", model.ComponentAgent, model.UpdateTaskRestarting).Find(&activeRestartingTasks)

	requireMatching := false
	var confirmations []*pb.UpdateHealthConfirmation

	var currentNode model.Node
	db.First(&currentNode, node.ID)
	for _, rTask := range activeRestartingTasks {
		if currentNode.Version == rTask.DesiredVersion &&
			currentNode.CommitID == rTask.DesiredCommitID &&
			currentNode.BinarySHA256 == rTask.DesiredSHA256 {
			requireMatching = true
			confirmations = append(confirmations, &pb.UpdateHealthConfirmation{
				TaskId:          rTask.ID,
				Version:         currentNode.Version,
				CommitId:        currentNode.CommitID,
				ArtifactSha256:  currentNode.BinarySHA256,
				ConfirmedAtUnix: 1770000000,
			})
		}
	}

	if !requireMatching || len(confirmations) == 0 {
		log.Fatalf("Server failed to issue UpdateHealthConfirmation!")
	}
	fmt.Printf("[Step 6] Server issued UpdateHealthConfirmation: TaskID=%s, Version=%s, CommitID=%s\n",
		confirmations[0].TaskId, confirmations[0].Version, confirmations[0].CommitId)

	// 8. Agent handles HealthConfirmation
	updaterMgr.HandleHealthConfirmations(confirmations)

	healthFile := filepath.Join(stateDir, "health", task.ID+".json")
	if _, err := os.Stat(healthFile); err != nil {
		log.Fatalf("Health file missing: %v", err)
	}
	fmt.Printf("[Step 7] Agent written health confirmation file: %s\n", healthFile)

	// 9. Final report from Agent: succeeded
	err = updateSvc.Report(ctx, task.ID, reporter, service.UpdateStatusReport{
		Phase:           "succeeded",
		Sequence:        5,
		Progress:        100,
		CurrentVersion:  version,
		CurrentCommitID: targetCommit,
		CurrentSHA256:   targetSHA256,
	})
	if err != nil {
		log.Fatalf("Report succeeded failed: %v", err)
	}

	var finalTask model.UpdateTask
	db.First(&finalTask, "id = ?", task.ID)
	fmt.Printf("[Step 8] Final UpdateTask Status: %s\n", finalTask.Status)

	var finalNode model.Node
	db.First(&finalNode, node.ID)
	fmt.Printf("[Step 9] Final Node Status: Version=%s, CommitID=%s, BinarySHA256=%s\n",
		finalNode.Version, finalNode.CommitID, finalNode.BinarySHA256)

	if finalTask.Status == model.UpdateTaskSucceeded &&
		finalNode.Version == version &&
		finalNode.CommitID == targetCommit &&
		finalNode.BinarySHA256 == targetSHA256 {
		fmt.Println("=== SUCCESS: Real Environment Agent v1.0.2 Upgrade Test Passed! ===")
	} else {
		log.Fatalf("Test assertion failed!")
	}
}
