package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

type Directive struct {
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
	CommitID      string
	Action        string
}

type Status struct {
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

type Config struct {
	Component       string
	CurrentVersion  string
	CurrentCommitID string
	CurrentSHA256   string
	StateDir        string
	CurrentLink     string
	ServiceName     string
	Executable      string
}

type taskState struct {
	Status
	Component      string    `json:"component"`
	TargetVersion  string    `json:"target_version"`
	TargetCommitID string    `json:"target_commit_id"`
	TargetSHA256   string    `json:"target_sha256"`
	Force          bool      `json:"force"`
	StagedBinary   string    `json:"staged_binary"`
	PreviousTarget string    `json:"previous_target"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type HealthFile struct {
	SchemaVersion      int       `json:"schema_version"`
	TaskID             string    `json:"task_id"`
	Version            string    `json:"version"`
	CommitID           string    `json:"commit_id"`
	BinarySHA256       string    `json:"binary_sha256"`
	RegisteredAt       time.Time `json:"registered_at"`
	HeartbeatConfirmed time.Time `json:"heartbeat_confirmed_at"`
}

var errTaskAlreadyTerminal = errors.New("updater task already reached a terminal state")

type Manager struct {
	cfg    Config
	client *http.Client

	mu          sync.Mutex
	states      map[string]taskState
	processing  map[string]bool
	startHelper func(Directive, string) error
}

func NewManager(cfg Config) (*Manager, error) {
	if cfg.Component == "" || cfg.StateDir == "" || cfg.CurrentLink == "" || cfg.ServiceName == "" {
		return nil, errors.New("updater configuration is incomplete")
	}
	if (cfg.Component == "agent" || cfg.Component == "endpoint") &&
		(!validCommitID(cfg.CurrentCommitID) || !validSHA256(cfg.CurrentSHA256)) {
		return nil, errors.New("updater current build identity is invalid")
	}
	if cfg.Executable == "" {
		executable, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve updater executable: %w", err)
		}
		cfg.Executable = executable
	}
	manager := &Manager{
		cfg:        cfg,
		client:     secureHTTPClient(),
		states:     make(map[string]taskState),
		processing: make(map[string]bool),
	}
	if err := os.MkdirAll(cfg.StateDir, 0700); err != nil {
		return nil, fmt.Errorf("create updater state directory: %w", err)
	}
	for _, name := range []string{"tasks", "downloads", "artifacts", "health"} {
		if err := os.MkdirAll(filepath.Join(cfg.StateDir, name), 0700); err != nil {
			return nil, fmt.Errorf("create updater %s directory: %w", name, err)
		}
	}

	manager.loadStates()
	if err := manager.cleanupLocalState(); err != nil {
		log.Printf("updater: cleanup local state: %v", err)
	}
	manager.startHelper = manager.startApplyHelper
	return manager, nil
}

func (m *Manager) cleanupLocalState() error {
	if err := clearDirectory(filepath.Join(m.cfg.StateDir, "downloads")); err != nil {
		return fmt.Errorf("clear incomplete downloads: %w", err)
	}

	protected := make(map[string]struct{})
	for _, link := range []string{m.cfg.CurrentLink, m.cfg.CurrentLink + ".previous"} {
		if target, err := resolvedLinkTarget(link); err == nil {
			protected[target] = struct{}{}
		}
	}
	m.mu.Lock()
	for _, state := range m.states {
		if !terminal(state.Phase) && state.StagedBinary != "" {
			protected[filepath.Clean(state.StagedBinary)] = struct{}{}
		}
	}
	m.mu.Unlock()

	artifactsDir := filepath.Join(m.cfg.StateDir, "artifacts")
	entries, err := os.ReadDir(artifactsDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !validSHA256(entry.Name()) {
			continue
		}
		directory := filepath.Join(artifactsDir, entry.Name())
		if pathProtected(directory, protected) {
			continue
		}
		if err := os.RemoveAll(directory); err != nil {
			return fmt.Errorf("remove unreferenced artifact %s: %w", entry.Name(), err)
		}
	}
	return syncDirectory(artifactsDir)
}

func clearDirectory(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return syncDirectory(path)
}

func resolvedLinkTarget(link string) (string, error) {
	target, err := os.Readlink(link)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	return filepath.Clean(target), nil
}

func pathProtected(directory string, protected map[string]struct{}) bool {
	directory = filepath.Clean(directory)
	prefix := directory + string(os.PathSeparator)
	for path := range protected {
		path = filepath.Clean(path)
		if path == directory || strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func (m *Manager) Handle(directive Directive) {
	if !validTaskID(directive.TaskID) || directive.Component != m.cfg.Component || directive.Version == "" {
		return
	}
	m.mu.Lock()
	state, exists := m.states[directive.TaskID]
	if m.processing[directive.TaskID] {
		m.mu.Unlock()
		return
	}
	if exists && (terminal(state.Phase) || !stateMatchesDirective(state, directive)) {
		m.mu.Unlock()
		return
	}
	m.processing[directive.TaskID] = true
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.processing, directive.TaskID)
			m.mu.Unlock()
		}()
		if exists {
			m.resume(directive, state)
		} else {
			m.execute(directive)
		}
	}()
}

func (m *Manager) resume(directive Directive, state taskState) {
	if err := m.validateDirective(directive); err != nil {
		_ = m.setStatusWithPath(directive, "failed", state.Progress, state.StagedBinary, "invalid_update_directive", err.Error())
		return
	}
	if latest, err := readTaskStateConsistent(m.cfg.StateDir, directive.TaskID); err == nil {
		state = latest
		m.mu.Lock()
		m.states[directive.TaskID] = latest
		m.mu.Unlock()
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Printf("updater: read latest task %s before resume: %v", directive.TaskID, err)
		return
	}
	if terminal(state.Phase) {
		return
	}
	switch state.Phase {
	case "staged", "installing", "restarting":
		if state.StagedBinary != "" {
			if err := m.startHelper(directive, state.StagedBinary); err != nil {
				_ = m.setStatusWithPath(directive, "failed", state.Progress, state.StagedBinary, "helper_start_failed", err.Error())
			}
			return
		}
	}
	m.downloadAndStart(directive)
}

func (m *Manager) HandleHealthConfirmations(confirmations []*pb.UpdateHealthConfirmation) {
	if len(confirmations) == 0 {
		return
	}
	healthDir := filepath.Join(m.cfg.StateDir, "health")
	_ = os.MkdirAll(healthDir, 0700)

	for _, conf := range confirmations {
		if conf == nil || conf.TaskId == "" {
			continue
		}
		m.mu.Lock()
		state, ok := m.states[conf.TaskId]
		m.mu.Unlock()
		if !ok || terminal(state.Phase) || conf.Version != state.TargetVersion ||
			conf.CommitId != state.TargetCommitID || !strings.EqualFold(conf.ArtifactSha256, state.TargetSHA256) {
			continue
		}
		hf := HealthFile{
			SchemaVersion:      2,
			TaskID:             conf.TaskId,
			Version:            conf.Version,
			CommitID:           conf.CommitId,
			BinarySHA256:       conf.ArtifactSha256,
			RegisteredAt:       time.Now(),
			HeartbeatConfirmed: time.Unix(conf.ConfirmedAtUnix, 0),
		}
		if err := writeHealthFile(healthDir, hf); err != nil {
			log.Printf("updater: write health confirmation for task %s: %v", conf.TaskId, err)
		}
	}
}

func (m *Manager) Statuses() []Status {
	m.loadStates()
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]Status, 0, len(m.states))
	for _, state := range m.states {
		state.CurrentVersion = m.cfg.CurrentVersion
		state.CurrentCommitID = m.cfg.CurrentCommitID
		state.CurrentSHA256 = m.cfg.CurrentSHA256
		result = append(result, state.Status)
	}
	return result
}

func (m *Manager) execute(directive Directive) {
	if err := m.validateDirective(directive); err != nil {
		_ = m.setStatus(directive, "failed", 0, "invalid_update_directive", err.Error())
		return
	}
	if directive.NotBeforeUnix > 0 && time.Now().Before(time.Unix(directive.NotBeforeUnix, 0)) {
		return
	}
	if directive.DeadlineUnix > 0 && time.Now().After(time.Unix(directive.DeadlineUnix, 0)) {
		_ = m.setStatus(directive, "failed", 0, "task_expired", "更新任务已超过截止时间")
		return
	}
	if err := m.setStatus(directive, "accepted", 0, "", ""); err != nil {
		return
	}
	m.downloadAndStart(directive)
}

func (m *Manager) downloadAndStart(directive Directive) {
	if err := m.setStatus(directive, "downloading", 0, "", ""); err != nil {
		return
	}
	stagedBinary, err := m.downloadAndVerify(directive)
	if err != nil {
		_ = m.setStatus(directive, "failed", 0, errorCode(err), err.Error())
		return
	}
	if err := m.setStatusWithPath(directive, "staged", 100, stagedBinary, "", ""); err != nil {
		return
	}

	if err := m.startHelper(directive, stagedBinary); err != nil {
		_ = m.setStatusWithPath(directive, "failed", 100, stagedBinary, "helper_start_failed", err.Error())
	}
}

func (m *Manager) validateDirective(directive Directive) error {
	if !validTaskID(directive.TaskID) || directive.ArtifactID == "" || directive.Version == "" ||
		!validCommitID(directive.CommitID) || !validSHA256(directive.SHA256) {
		return errors.New("update directive build identity is incomplete")
	}
	if directive.OS != runtime.GOOS || directive.Arch != runtime.GOARCH {
		return fmt.Errorf("update artifact platform mismatch: expected %s/%s, got %s/%s", runtime.GOOS, runtime.GOARCH, directive.OS, directive.Arch)
	}
	if directive.Action != "install" {
		return fmt.Errorf("unsupported update action %q", directive.Action)
	}
	return nil
}

func (m *Manager) downloadAndVerify(directive Directive) (string, error) {
	if directive.DownloadURL == "" || directive.Filename == "" || directive.Size <= 0 || len(directive.SHA256) != sha256.Size*2 {
		return "", errors.New("invalid update artifact metadata")
	}
	if filepath.Base(directive.Filename) != directive.Filename || strings.Contains(directive.Filename, "\\") {
		return "", errors.New("invalid update artifact filename")
	}
	parsedURL, err := url.Parse(directive.DownloadURL)
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
		return "", errors.New("update artifact URL must use HTTPS")
	}

	// Content-addressed artifact path: artifacts/<sha256>/<artifact filename>
	artifactDir := filepath.Join(m.cfg.StateDir, "artifacts", strings.ToLower(directive.SHA256))
	finalPath := filepath.Join(artifactDir, directive.Filename)

	// If already present, verify existing file SHA256
	if fileInfo, err := os.Stat(finalPath); err == nil && !fileInfo.IsDir() {
		existingFile, openErr := os.Open(finalPath)
		if openErr == nil {
			h := sha256.New()
			_, _ = io.Copy(h, existingFile)
			existingFile.Close()
			if strings.EqualFold(hex.EncodeToString(h.Sum(nil)), directive.SHA256) {
				return finalPath, nil
			}
			return "", errors.New("artifact_identity_conflict")
		}
	}

	downloadDir := filepath.Join(m.cfg.StateDir, "downloads")
	if err := os.MkdirAll(downloadDir, 0700); err != nil {
		return "", fmt.Errorf("create download directory: %w", err)
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, directive.DownloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("create download request: %w", err)
	}
	response, err := m.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download artifact: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download artifact: unexpected HTTP status %d", response.StatusCode)
	}

	temporary, err := os.CreateTemp(downloadDir, ".download-*")
	if err != nil {
		return "", fmt.Errorf("create temporary artifact: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		temporary.Close()
		_ = os.Remove(temporaryPath)
	}()

	_ = os.Chmod(temporaryPath, 0600)

	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(response.Body, directive.Size+1))
	if copyErr != nil {
		return "", fmt.Errorf("write artifact: %w", copyErr)
	}
	if written != directive.Size {
		return "", fmt.Errorf("artifact_size_mismatch: expected %d bytes, got %d", directive.Size, written)
	}
	if err := temporary.Sync(); err != nil {
		return "", fmt.Errorf("sync artifact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close artifact: %w", err)
	}

	digest := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(digest, directive.SHA256) {
		return "", errors.New("checksum_mismatch")
	}

	if err := os.MkdirAll(artifactDir, 0700); err != nil {
		return "", fmt.Errorf("create artifact directory: %w", err)
	}
	if err := os.Chmod(temporaryPath, 0755); err != nil {
		return "", fmt.Errorf("make artifact executable: %w", err)
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return "", fmt.Errorf("stage artifact: %w", err)
	}
	if err := syncDirectory(artifactDir); err != nil {
		return "", fmt.Errorf("sync artifact directory: %w", err)
	}
	return finalPath, nil
}

func (m *Manager) startApplyHelper(directive Directive, stagedBinary string) error {
	unit := fmt.Sprintf("signal-update-%s", directive.TaskID[:min(12, len(directive.TaskID))])
	if output, _ := exec.Command("systemctl", "is-active", unit).CombinedOutput(); helperUnitRunning(strings.TrimSpace(string(output))) {
		return nil
	}
	previousLink := m.cfg.CurrentLink + ".previous"
	command := exec.Command(
		"systemd-run",
		"--unit", unit,
		"--collect",
		m.cfg.Executable,
		"updater-apply",
		"--task-id", directive.TaskID,
		"--state-dir", m.cfg.StateDir,
		"--binary", stagedBinary,
		"--current-link", m.cfg.CurrentLink,
		"--previous-link", previousLink,
		"--service", m.cfg.ServiceName,
	)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("start updater helper: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func helperUnitRunning(state string) bool {
	switch state {
	case "active", "activating", "reloading", "deactivating":
		return true
	default:
		return false
	}
}

func (m *Manager) setStatus(directive Directive, phase string, progress int, code, message string) error {
	return m.setStatusWithPath(directive, phase, progress, "", code, message)
}

func (m *Manager) setStatusWithPath(directive Directive, phase string, progress int, stagedPath, code, message string) error {
	m.mu.Lock()
	base := m.states[directive.TaskID]
	m.mu.Unlock()

	state, updated, err := updateTaskState(m.cfg.StateDir, directive.TaskID, base, func(state taskState) taskState {
		state.TaskID = directive.TaskID
		state.Component = directive.Component
		state.TargetVersion = directive.Version
		state.TargetCommitID = directive.CommitID
		state.TargetSHA256 = directive.SHA256
		state.Force = directive.Force
		state.Phase = phase
		state.Progress = progress
		state.CurrentVersion = m.cfg.CurrentVersion
		state.CurrentCommitID = m.cfg.CurrentCommitID
		state.CurrentSHA256 = m.cfg.CurrentSHA256
		state.Sequence++
		state.ErrorCode = code
		state.ErrorMessage = message
		if stagedPath != "" {
			state.StagedBinary = stagedPath
		}
		state.UpdatedAt = time.Now()
		return state
	})
	if err != nil {
		log.Printf("updater: persist task %s phase %s: %v", directive.TaskID, phase, err)
		return err
	}
	m.mu.Lock()
	if current, ok := m.states[directive.TaskID]; !ok || current.Sequence < state.Sequence || terminal(state.Phase) {
		m.states[directive.TaskID] = state
	}
	m.mu.Unlock()
	if !updated {
		return errTaskAlreadyTerminal
	}
	return nil
}

func (m *Manager) loadStates() {
	tasksDir := filepath.Join(m.cfg.StateDir, "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return
	}
	loaded := make(map[string]taskState)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		state, err := readTaskState(filepath.Join(tasksDir, entry.Name()))
		if err == nil && state.TaskID != "" {
			loaded[state.TaskID] = state
		}
	}
	m.mu.Lock()
	for taskID, state := range loaded {
		if current, ok := m.states[taskID]; !ok || current.Sequence < state.Sequence {
			m.states[taskID] = state
		}
	}
	m.mu.Unlock()
}

type ApplyConfig struct {
	TaskID        string
	StateDir      string
	BinaryPath    string
	CurrentLink   string
	PreviousLink  string
	ServiceName   string
	restart       func(context.Context, string) error
	healthTimeout time.Duration
	healthPoll    time.Duration
}

func Apply(ctx context.Context, cfg ApplyConfig) error {
	if cfg.TaskID == "" || cfg.StateDir == "" || cfg.BinaryPath == "" || cfg.CurrentLink == "" || cfg.ServiceName == "" {
		return errors.New("updater apply configuration is incomplete")
	}
	if cfg.PreviousLink == "" {
		cfg.PreviousLink = cfg.CurrentLink + ".previous"
	}
	if cfg.restart == nil {
		cfg.restart = restartAndCheck
	}
	if cfg.healthTimeout <= 0 {
		cfg.healthTimeout = 90 * time.Second
	}
	if cfg.healthPoll <= 0 {
		cfg.healthPoll = 2 * time.Second
	}
	releaseApplyLock, err := acquireTaskFileLock(cfg.StateDir, cfg.TaskID, "apply")
	if err != nil {
		return fmt.Errorf("lock updater apply task: %w", err)
	}
	defer releaseApplyLock()

	statePath := taskStatePath(cfg.StateDir, cfg.TaskID)
	state, err := readTaskState(statePath)
	if err != nil {
		return fmt.Errorf("read updater task state: %w", err)
	}
	if terminal(state.Phase) {
		return nil
	}
	if err := verifyFileSHA256(cfg.BinaryPath, state.TargetSHA256); err != nil {
		return failApply(cfg, state, "artifact_mismatch", err)
	}

	previous, err := os.Readlink(cfg.CurrentLink)
	if err != nil {
		return failApply(cfg, state, "current_link_invalid", err)
	}

	// A persisted installing/restarting state means a previous helper was
	// interrupted. Re-enter the health window without changing previous.
	sameArtifact := previous == cfg.BinaryPath
	recovering := sameArtifact && (state.Phase == "installing" || state.Phase == "restarting") && state.PreviousTarget != ""
	if sameArtifact && !state.Force && !recovering {
		state.Phase = "succeeded"
		state.Progress = 100
		state.Sequence++
		state.ErrorCode = ""
		state.ErrorMessage = "already_current"
		state.UpdatedAt = time.Now()
		return writeTaskState(cfg.StateDir, state)
	}

	if !sameArtifact {
		state.PreviousTarget = previous
	}
	state.Phase = "installing"
	state.Sequence++
	state.UpdatedAt = time.Now()
	if err := writeTaskState(cfg.StateDir, state); err != nil {
		return err
	}

	if !sameArtifact {
		if err := switchSymlink(cfg.PreviousLink, previous); err != nil {
			return failApply(cfg, state, "install_failed", err)
		}
		if err := switchSymlink(cfg.CurrentLink, cfg.BinaryPath); err != nil {
			return failApply(cfg, state, "install_failed", err)
		}
	}

	state.Phase = "restarting"
	state.Sequence++
	state.UpdatedAt = time.Now()
	if err := writeTaskState(cfg.StateDir, state); err != nil {
		return err
	}

	healthPath := filepath.Join(cfg.StateDir, "health", cfg.TaskID+".json")
	if err := os.Remove(healthPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return failApply(cfg, state, "health_file_cleanup_failed", err)
	}
	if err := cfg.restart(ctx, cfg.ServiceName); err != nil {
		return rollback(ctx, cfg, state, "restart_failed", err)
	}

	deadline := time.Now().Add(cfg.healthTimeout)
	healthConfirmed := false

	for time.Now().Before(deadline) {
		if validHealthFile(healthPath, state) {
			healthConfirmed = true
			break
		}
		select {
		case <-ctx.Done():
			return rollback(ctx, cfg, state, "heartbeat_timeout", ctx.Err())
		case <-time.After(cfg.healthPoll):
		}
	}

	if !healthConfirmed {
		return rollback(ctx, cfg, state, "heartbeat_timeout", errors.New("90s certified heartbeat health confirmation timeout"))
	}

	state.Phase = "succeeded"
	state.Progress = 100
	state.Sequence++
	state.ErrorCode = ""
	state.ErrorMessage = ""
	state.UpdatedAt = time.Now()
	return writeTaskState(cfg.StateDir, state)
}

func writeHealthFile(healthDir string, health HealthFile) error {
	if err := os.MkdirAll(healthDir, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(health)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(healthDir, ".health-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, filepath.Join(healthDir, health.TaskID+".json")); err != nil {
		return err
	}
	return syncDirectory(healthDir)
}

func validHealthFile(path string, state taskState) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || !secureTaskFileMode(info) {
		return false
	}
	if !ownedByCurrentUser(info) {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var health HealthFile
	if json.Unmarshal(data, &health) != nil {
		return false
	}
	return health.SchemaVersion == 2 && health.TaskID == state.TaskID &&
		health.Version == state.TargetVersion && health.CommitID == state.TargetCommitID &&
		strings.EqualFold(health.BinarySHA256, state.TargetSHA256) && !health.HeartbeatConfirmed.IsZero()
}

func validCommitID(value string) bool {
	if len(value) != 40 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func rollback(ctx context.Context, cfg ApplyConfig, state taskState, errCode string, origErr error) error {
	rollbackErr := switchSymlink(cfg.CurrentLink, state.PreviousTarget)
	if rollbackErr == nil {
		rollbackErr = cfg.restart(ctx, cfg.ServiceName)
	}
	state.Phase = "rolled_back"
	state.Sequence++
	state.ErrorCode = errCode
	state.ErrorMessage = origErr.Error()
	if rollbackErr != nil {
		state.ErrorCode = "rollback_failed"
		state.ErrorMessage = fmt.Sprintf("original error: %v; rollback error: %v", origErr, rollbackErr)
	}
	state.UpdatedAt = time.Now()
	return writeTaskState(cfg.StateDir, state)
}

func RunApplyCLI(args []string) error {
	values := make(map[string]string)
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			values[args[i]] = args[i+1]
		}
	}
	currentLink := values["--current-link"]
	previousLink := values["--previous-link"]
	if previousLink == "" && currentLink != "" {
		previousLink = currentLink + ".previous"
	}
	return Apply(context.Background(), ApplyConfig{
		TaskID:       values["--task-id"],
		StateDir:     values["--state-dir"],
		BinaryPath:   values["--binary"],
		CurrentLink:  currentLink,
		PreviousLink: previousLink,
		ServiceName:  values["--service"],
	})
}

func secureHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Minute,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			if request.URL.Scheme != "https" {
				return errors.New("redirect to non-HTTPS URL is not allowed")
			}
			return nil
		},
	}
}

func restartAndCheck(ctx context.Context, serviceName string) error {
	if output, err := exec.CommandContext(ctx, "systemctl", "reset-failed", serviceName).CombinedOutput(); err != nil {
		return fmt.Errorf("reset failed state for %s: %w: %s", serviceName, err, strings.TrimSpace(string(output)))
	}
	if output, err := exec.CommandContext(ctx, "systemctl", "restart", serviceName).CombinedOutput(); err != nil {
		return fmt.Errorf("restart %s: %w: %s", serviceName, err, strings.TrimSpace(string(output)))
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", serviceName).Run(); err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("service %s did not become active", serviceName)
}

func switchSymlink(linkPath, target string) error {
	directory := filepath.Dir(linkPath)
	temporaryDir, err := os.MkdirTemp(directory, "."+filepath.Base(linkPath)+".new-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporaryDir)
	temporary := filepath.Join(temporaryDir, filepath.Base(linkPath))
	if err := os.Symlink(target, temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, linkPath); err != nil {
		return err
	}
	return syncDirectory(directory)
}

func verifyFileSHA256(path, expected string) error {
	if !validSHA256(expected) {
		return errors.New("invalid expected artifact SHA256")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open staged artifact: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash staged artifact: %w", err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("staged artifact SHA256 mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

func failApply(cfg ApplyConfig, state taskState, code string, err error) error {
	state.Phase = "failed"
	state.Sequence++
	state.ErrorCode = code
	state.ErrorMessage = err.Error()
	state.UpdatedAt = time.Now()
	return writeTaskState(cfg.StateDir, state)
}

func writeTaskState(stateDir string, state taskState) error {
	release, err := acquireTaskFileLock(stateDir, state.TaskID, "state")
	if err != nil {
		return err
	}
	defer release()

	path := taskStatePath(stateDir, state.TaskID)
	current, err := readTaskState(path)
	if err == nil {
		if terminal(current.Phase) || state.Sequence <= current.Sequence {
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return replaceTaskState(stateDir, state)
}

func updateTaskState(stateDir, taskID string, fallback taskState, mutate func(taskState) taskState) (taskState, bool, error) {
	release, err := acquireTaskFileLock(stateDir, taskID, "state")
	if err != nil {
		return taskState{}, false, err
	}
	defer release()

	current, err := readTaskState(taskStatePath(stateDir, taskID))
	if errors.Is(err, os.ErrNotExist) {
		current = fallback
	} else if err != nil {
		return taskState{}, false, err
	}
	if terminal(current.Phase) {
		return current, false, nil
	}
	next := mutate(current)
	if err := replaceTaskState(stateDir, next); err != nil {
		return taskState{}, false, err
	}
	return next, true, nil
}

func readTaskStateConsistent(stateDir, taskID string) (taskState, error) {
	release, err := acquireTaskFileLock(stateDir, taskID, "state")
	if err != nil {
		return taskState{}, err
	}
	defer release()
	return readTaskState(taskStatePath(stateDir, taskID))
}

func replaceTaskState(stateDir string, state taskState) error {
	tasksDir := filepath.Join(stateDir, "tasks")
	if err := os.MkdirAll(tasksDir, 0700); err != nil {
		tasksDir = stateDir
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(tasksDir, ".task-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	_ = os.Chmod(temporaryPath, 0600)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, taskStatePath(stateDir, state.TaskID)); err != nil {
		return err
	}
	return syncDirectory(tasksDir)
}

func readTaskState(path string) (taskState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return taskState{}, err
	}
	var state taskState
	if err := json.Unmarshal(data, &state); err != nil {
		return taskState{}, err
	}
	return state, nil
}

func taskStatePath(stateDir, taskID string) string {
	tasksDir := filepath.Join(stateDir, "tasks")
	if _, err := os.Stat(tasksDir); err == nil {
		return filepath.Join(tasksDir, taskID+".json")
	}
	return filepath.Join(stateDir, taskID+".json")
}

func terminal(phase string) bool {
	switch phase {
	case "succeeded", "failed", "rolled_back", "cancelled", "expired":
		return true
	default:
		return false
	}
}

func stateMatchesDirective(state taskState, directive Directive) bool {
	return state.Component == directive.Component && state.TargetVersion == directive.Version &&
		state.TargetCommitID == directive.CommitID && strings.EqualFold(state.TargetSHA256, directive.SHA256)
}

func validTaskID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}

func errorCode(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "checksum_mismatch"):
		return "checksum_mismatch"
	case strings.Contains(message, "artifact_size_mismatch"):
		return "artifact_size_mismatch"
	default:
		return "download_failed"
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
