package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
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
	Size          int64
	SHA256        string
	Signature     string
	KeyID         string
	Force         bool
	NotBeforeUnix int64
	DeadlineUnix  int64
	CommitID      string
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
	PublicKeyBase64 string
	KeyID           string
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

type Manager struct {
	cfg       Config
	publicKey ed25519.PublicKey
	client    *http.Client

	mu         sync.Mutex
	states     map[string]taskState
	processing map[string]bool
}

func NewManager(cfg Config) (*Manager, error) {
	if cfg.Component == "" || cfg.StateDir == "" || cfg.CurrentLink == "" || cfg.ServiceName == "" {
		return nil, errors.New("updater configuration is incomplete")
	}
	if cfg.Component == "agent" && (!validCommitID(cfg.CurrentCommitID) || !validSHA256(cfg.CurrentSHA256)) {
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
	if cfg.PublicKeyBase64 != "" {
		key, err := decodePublicKey(cfg.PublicKeyBase64)
		if err != nil {
			return nil, err
		}
		manager.publicKey = key
	}
	if err := os.MkdirAll(cfg.StateDir, 0700); err != nil {
		return nil, fmt.Errorf("create updater state directory: %w", err)
	}
	_ = os.MkdirAll(filepath.Join(cfg.StateDir, "tasks"), 0700)
	_ = os.MkdirAll(filepath.Join(cfg.StateDir, "downloads"), 0700)
	_ = os.MkdirAll(filepath.Join(cfg.StateDir, "artifacts"), 0700)
	_ = os.MkdirAll(filepath.Join(cfg.StateDir, "health"), 0700)

	manager.loadStates()
	return manager, nil
}

func (m *Manager) Handle(directive Directive) {
	if directive.TaskID == "" || directive.Component != m.cfg.Component || directive.Version == "" {
		return
	}
	m.mu.Lock()
	if existing, ok := m.states[directive.TaskID]; ok && terminal(existing.Phase) {
		m.mu.Unlock()
		return
	}
	if m.processing[directive.TaskID] {
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
		m.execute(directive)
	}()
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
		_ = writeHealthFile(healthDir, hf)
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
		m.setStatus(directive, "failed", 0, "invalid_update_directive", err.Error())
		return
	}
	if directive.NotBeforeUnix > 0 && time.Now().Before(time.Unix(directive.NotBeforeUnix, 0)) {
		return
	}
	if directive.DeadlineUnix > 0 && time.Now().After(time.Unix(directive.DeadlineUnix, 0)) {
		m.setStatus(directive, "failed", 0, "task_expired", "更新任务已超过截止时间")
		return
	}
	if len(m.publicKey) != ed25519.PublicKeySize {
		m.setStatus(directive, "failed", 0, "signature_key_unavailable", "未配置 updater 签名公钥")
		return
	}

	m.setStatus(directive, "accepted", 0, "", "")
	m.setStatus(directive, "downloading", 0, "", "")
	stagedBinary, err := m.downloadAndVerify(directive)
	if err != nil {
		m.setStatus(directive, "failed", 0, errorCode(err), err.Error())
		return
	}
	m.setStatusWithPath(directive, "staged", 100, stagedBinary, "", "")
	m.setStatusWithPath(directive, "restarting", 100, stagedBinary, "", "")

	if err := m.startApplyHelper(directive, stagedBinary); err != nil {
		m.setStatusWithPath(directive, "failed", 100, stagedBinary, "helper_start_failed", err.Error())
	}
}

func (m *Manager) validateDirective(directive Directive) error {
	if directive.TaskID == "" || directive.ArtifactID == "" || directive.Version == "" ||
		!validCommitID(directive.CommitID) || !validSHA256(directive.SHA256) {
		return errors.New("update directive build identity is incomplete")
	}
	if strings.TrimSpace(m.cfg.KeyID) == "" || directive.KeyID != m.cfg.KeyID {
		return errors.New("update directive key_id does not match configured key")
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

	// Content-addressed artifact path: artifacts/<sha256>/signal_agent
	artifactDir := filepath.Join(m.cfg.StateDir, "artifacts", strings.ToLower(directive.SHA256))
	finalPath := filepath.Join(artifactDir, "signal_agent")

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
	if err := verifySignature(m.publicKey, digest, directive.Signature); err != nil {
		return "", err
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
	return finalPath, nil
}

func (m *Manager) startApplyHelper(directive Directive, stagedBinary string) error {
	unit := fmt.Sprintf("signal-update-%s", directive.TaskID[:min(12, len(directive.TaskID))])
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

func (m *Manager) setStatus(directive Directive, phase string, progress int, code, message string) {
	m.setStatusWithPath(directive, phase, progress, "", code, message)
}

func (m *Manager) setStatusWithPath(directive Directive, phase string, progress int, stagedPath, code, message string) {
	m.mu.Lock()
	state := m.states[directive.TaskID]
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
	m.states[directive.TaskID] = state
	m.mu.Unlock()
	_ = writeTaskState(m.cfg.StateDir, state)
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
		if current, ok := m.states[taskID]; !ok || current.Sequence <= state.Sequence {
			m.states[taskID] = state
		}
	}
	m.mu.Unlock()
}

type ApplyConfig struct {
	TaskID       string
	StateDir     string
	BinaryPath   string
	CurrentLink  string
	PreviousLink string
	ServiceName  string
}

func Apply(ctx context.Context, cfg ApplyConfig) error {
	if cfg.TaskID == "" || cfg.StateDir == "" || cfg.BinaryPath == "" || cfg.CurrentLink == "" || cfg.ServiceName == "" {
		return errors.New("updater apply configuration is incomplete")
	}
	if cfg.PreviousLink == "" {
		cfg.PreviousLink = cfg.CurrentLink + ".previous"
	}

	statePath := taskStatePath(cfg.StateDir, cfg.TaskID)
	state, err := readTaskState(statePath)
	if err != nil {
		return fmt.Errorf("read updater task state: %w", err)
	}

	previous, err := os.Readlink(cfg.CurrentLink)
	if err != nil {
		return failApply(cfg, state, "current_link_invalid", err)
	}

	// Idempotency check: if current already points to binaryPath
	sameArtifact := previous == cfg.BinaryPath
	if sameArtifact && !state.Force {
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
	if err := restartAndCheck(ctx, cfg.ServiceName); err != nil {
		return rollback(ctx, cfg, state, "restart_failed", err)
	}

	// 90-second Health Confirmation Window Check
	deadline := time.Now().Add(90 * time.Second)
	healthConfirmed := false

	for time.Now().Before(deadline) {
		if validHealthFile(healthPath, state) {
			healthConfirmed = true
			break
		}
		time.Sleep(2 * time.Second)
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
	return os.Rename(temporaryPath, filepath.Join(healthDir, health.TaskID+".json"))
}

func validHealthFile(path string, state taskState) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
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
		rollbackErr = restartAndCheck(ctx, cfg.ServiceName)
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

func decodePublicKey(encoded string) (ed25519.PublicKey, error) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(decoded) != ed25519.PublicKeySize {
		return nil, errors.New("invalid updater Ed25519 public key")
	}
	return ed25519.PublicKey(decoded), nil
}

func verifySignature(publicKey ed25519.PublicKey, digest, encodedSignature string) error {
	signature, err := base64.StdEncoding.DecodeString(encodedSignature)
	if err != nil || !ed25519.Verify(publicKey, []byte(digest), signature) {
		return errors.New("signature_invalid")
	}
	return nil
}

func restartAndCheck(ctx context.Context, serviceName string) error {
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
	temporary := filepath.Join(directory, "."+filepath.Base(linkPath)+".new")
	if err := os.Symlink(target, temporary); err != nil {
		return err
	}
	defer os.Remove(temporary)
	return os.Rename(temporary, linkPath)
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
	return os.Rename(temporaryPath, taskStatePath(stateDir, state.TaskID))
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

func errorCode(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "checksum_mismatch"):
		return "checksum_mismatch"
	case strings.Contains(message, "signature_invalid"):
		return "signature_invalid"
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
