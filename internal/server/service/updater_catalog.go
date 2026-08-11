package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

const maxUpdaterCatalogManifestSize = 1 << 20

var (
	updaterCatalogVersionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+$`)
	updaterCatalogCommitPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	updaterCatalogSHA256Pattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	updaterCatalogLabelPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
)

type UpdaterCatalogStore interface {
	ListManifestKeys(context.Context) ([]string, error)
	ReadManifest(context.Context, string) ([]byte, error)
}

type UpdaterCatalogManifest struct {
	SchemaVersion int                      `json:"schema_version"`
	PublishedAt   time.Time                `json:"published_at"`
	Release       UpdaterCatalogRelease    `json:"release"`
	Artifacts     []UpdaterCatalogArtifact `json:"artifacts"`
}

type UpdaterCatalogRelease struct {
	Component           model.Component `json:"component"`
	Version             string          `json:"version"`
	CommitID            string          `json:"commit_id"`
	Channel             string          `json:"channel"`
	ReleaseNotes        string          `json:"release_notes"`
	MinSupportedVersion string          `json:"min_supported_version"`
}

type UpdaterCatalogArtifact struct {
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Role        string `json:"role"`
	PackageType string `json:"package_type"`
	Filename    string `json:"filename"`
	DownloadURL string `json:"download_url"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Signature   string `json:"signature"`
	KeyID       string `json:"key_id"`
}

type UpdaterCatalogSyncResult struct {
	Scanned  int `json:"scanned"`
	Created  int `json:"created"`
	Updated  int `json:"updated"`
	Existing int `json:"existing"`
	Revoked  int `json:"revoked"`
	Failed   int `json:"failed"`
}

type UpdaterCatalogService struct {
	db              *gorm.DB
	store           UpdaterCatalogStore
	artifactBaseURL *url.URL
	mu              sync.Mutex
}

type httpUpdaterCatalogStore struct {
	client          *http.Client
	catalogURL      *url.URL
	artifactBaseURL *url.URL
}

type updaterCatalogIndex struct {
	SchemaVersion int       `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
	Manifests     []string  `json:"manifests"`
}

func NewUpdaterCatalogService(database *gorm.DB, cfg config.UpdaterSection) (*UpdaterCatalogService, error) {
	if database == nil {
		return nil, errors.New("updater catalog database is required")
	}
	store, baseURL, err := newHTTPUpdaterCatalogStore(cfg)
	if err != nil {
		return nil, err
	}
	return &UpdaterCatalogService{db: database, store: store, artifactBaseURL: baseURL}, nil
}

func NewUpdaterCatalogServiceWithStore(database *gorm.DB, store UpdaterCatalogStore, artifactBaseURL string) (*UpdaterCatalogService, error) {
	baseURL, err := validateHTTPSBaseURL(artifactBaseURL)
	if err != nil {
		return nil, err
	}
	if database == nil || store == nil {
		return nil, errors.New("updater catalog database and store are required")
	}
	return &UpdaterCatalogService{db: database, store: store, artifactBaseURL: baseURL}, nil
}

func newHTTPUpdaterCatalogStore(cfg config.UpdaterSection) (UpdaterCatalogStore, *url.URL, error) {
	baseURL, err := validateHTTPSBaseURL(cfg.ArtifactBaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid updater artifact base URL: %w", err)
	}
	catalogURL, err := url.Parse(strings.TrimSpace(cfg.CatalogURL))
	if err != nil || !urlWithinBase(catalogURL, baseURL) || !strings.HasSuffix(catalogURL.Path, "/updater/catalog.json") {
		return nil, nil, errors.New("updater catalog URL must be an HTTPS JSON object below artifact_base_url/updater")
	}
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
		},
	}
	client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if !urlWithinBase(req.URL, baseURL) {
			return errors.New("updater catalog redirect left artifact_base_url")
		}
		return nil
	}
	return &httpUpdaterCatalogStore{client: client, catalogURL: catalogURL, artifactBaseURL: baseURL}, baseURL, nil
}

func validateHTTPSBaseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("HTTPS URL without credentials, query or fragment is required")
	}
	return parsed, nil
}

func (s *httpUpdaterCatalogStore) ListManifestKeys(ctx context.Context) ([]string, error) {
	data, err := s.readURL(ctx, s.catalogURL)
	if err != nil {
		return nil, fmt.Errorf("read updater catalog index: %w", err)
	}
	var index updaterCatalogIndex
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&index); err != nil || index.SchemaVersion != 1 || index.GeneratedAt.IsZero() {
		return nil, errors.New("updater catalog index is invalid")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("updater catalog index is invalid")
	}
	keys := make([]string, 0, len(index.Manifests))
	seen := make(map[string]struct{}, len(index.Manifests))
	for _, raw := range index.Manifests {
		manifestURL, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || !isUpdaterReleaseManifestURL(manifestURL, s.artifactBaseURL) {
			return nil, errors.New("updater catalog contains an invalid manifest URL")
		}
		key := manifestURL.String()
		if _, exists := seen[key]; exists {
			return nil, errors.New("updater catalog contains duplicate manifest URLs")
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

func (s *httpUpdaterCatalogStore) ReadManifest(ctx context.Context, key string) ([]byte, error) {
	manifestURL, err := url.Parse(key)
	if err != nil || !isUpdaterReleaseManifestURL(manifestURL, s.artifactBaseURL) {
		return nil, errors.New("updater manifest URL is outside artifact_base_url")
	}
	return s.readURL(ctx, manifestURL)
}

func (s *httpUpdaterCatalogStore) readURL(ctx context.Context, target *url.URL) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxUpdaterCatalogManifestSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxUpdaterCatalogManifestSize {
		return nil, errors.New("updater catalog response exceeds 1 MiB")
	}
	return data, nil
}

func urlWithinBase(target, base *url.URL) bool {
	if target == nil || base == nil || target.Scheme != "https" || target.Scheme != base.Scheme || target.Host != base.Host ||
		target.User != nil || target.RawQuery != "" || target.Fragment != "" {
		return false
	}
	basePath := strings.TrimRight(path.Clean(base.Path), "/")
	targetPath := path.Clean(target.Path)
	return targetPath == basePath || strings.HasPrefix(targetPath, basePath+"/")
}

func isUpdaterReleaseManifestURL(target, base *url.URL) bool {
	if !urlWithinBase(target, base) || !strings.HasSuffix(target.Path, ".json") {
		return false
	}
	basePath := strings.TrimRight(path.Clean(base.Path), "/")
	targetPath := path.Clean(target.Path)
	return strings.HasPrefix(targetPath, basePath+"/updater/releases/")
}

func (s *UpdaterCatalogService) Sync(ctx context.Context) (UpdaterCatalogSyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	keys, err := s.store.ListManifestKeys(ctx)
	if err != nil {
		return UpdaterCatalogSyncResult{}, err
	}
	result := UpdaterCatalogSyncResult{Scanned: len(keys)}
	var syncErrors []error
	for _, key := range keys {
		data, readErr := s.store.ReadManifest(ctx, key)
		if readErr != nil {
			result.Failed++
			syncErrors = append(syncErrors, readErr)
			continue
		}
		manifest, parseErr := s.parseManifest(data)
		if parseErr != nil {
			result.Failed++
			syncErrors = append(syncErrors, fmt.Errorf("invalid updater catalog object %s: %w", key, parseErr))
			continue
		}
		state, syncErr := s.syncManifest(ctx, manifest)
		if syncErr != nil {
			result.Failed++
			syncErrors = append(syncErrors, fmt.Errorf("sync updater catalog object %s: %w", key, syncErr))
			continue
		}
		switch state {
		case "created":
			result.Created++
		case "updated":
			result.Updated++
		case "existing":
			result.Existing++
		case "revoked":
			result.Revoked++
		}
	}
	if len(syncErrors) > 0 {
		return result, errors.Join(syncErrors...)
	}
	return result, nil
}

func (s *UpdaterCatalogService) parseManifest(data []byte) (UpdaterCatalogManifest, error) {
	var manifest UpdaterCatalogManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return manifest, errors.New("manifest must contain one JSON value")
	}
	if err := s.validateManifest(manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func (s *UpdaterCatalogService) validateManifest(manifest UpdaterCatalogManifest) error {
	if manifest.SchemaVersion != 1 || manifest.PublishedAt.IsZero() || !manifest.Release.Component.Valid() {
		return errors.New("manifest identity is incomplete")
	}
	if !updaterCatalogVersionPattern.MatchString(manifest.Release.Version) || !updaterCatalogCommitPattern.MatchString(manifest.Release.CommitID) {
		return errors.New("release version or commit_id is invalid")
	}
	if !updaterCatalogLabelPattern.MatchString(manifest.Release.Channel) || len(manifest.Artifacts) == 0 {
		return errors.New("release channel or artifacts are invalid")
	}
	seen := make(map[string]struct{}, len(manifest.Artifacts))
	for _, artifact := range manifest.Artifacts {
		identity := artifact.OS + "\x00" + artifact.Arch + "\x00" + artifact.Role
		if _, exists := seen[identity]; exists {
			return errors.New("duplicate artifact platform and role")
		}
		seen[identity] = struct{}{}
		if !updaterCatalogLabelPattern.MatchString(artifact.OS) || !updaterCatalogLabelPattern.MatchString(artifact.Arch) ||
			!updaterCatalogLabelPattern.MatchString(artifact.Role) || !updaterCatalogLabelPattern.MatchString(artifact.PackageType) ||
			artifact.Size <= 0 || !updaterCatalogSHA256Pattern.MatchString(artifact.SHA256) ||
			artifact.Filename == "" || path.Base(artifact.Filename) != artifact.Filename {
			return errors.New("artifact metadata is invalid")
		}
		artifactURL, err := url.Parse(artifact.DownloadURL)
		if err != nil || !urlWithinBase(artifactURL, s.artifactBaseURL) || path.Clean(artifactURL.Path) == path.Clean(s.artifactBaseURL.Path) {
			return errors.New("artifact download_url is outside the configured HTTP artifact base URL")
		}
	}
	return nil
}

func (s *UpdaterCatalogService) syncManifest(ctx context.Context, manifest UpdaterCatalogManifest) (string, error) {
	state := ""
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var release model.Release
		err := tx.Where("component = ? AND version = ?", manifest.Release.Component, manifest.Release.Version).First(&release).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			release = model.Release{
				ID: uuid.NewString(), Component: manifest.Release.Component, Version: manifest.Release.Version,
				CommitID: manifest.Release.CommitID, Channel: manifest.Release.Channel, Status: model.ReleaseStatusPublished,
				ReleaseNotes: manifest.Release.ReleaseNotes, MinSupportedVersion: manifest.Release.MinSupportedVersion,
				PublishedAt: &manifest.PublishedAt,
			}
			if err := tx.Create(&release).Error; err != nil {
				return err
			}
			for _, input := range manifest.Artifacts {
				artifact := model.Artifact{
					ID: uuid.NewString(), ReleaseID: release.ID, OS: input.OS, Arch: input.Arch, Role: input.Role,
					PackageType: input.PackageType, Filename: input.Filename, DownloadURL: input.DownloadURL,
					Size: input.Size, SHA256: input.SHA256, Signature: input.Signature, KeyID: input.KeyID,
					Status: model.ArtifactStatusAvailable,
				}
				if err := tx.Create(&artifact).Error; err != nil {
					return err
				}
			}
			state = "created"
			return nil
		}
		if err != nil {
			return err
		}
		if release.Status == model.ReleaseStatusRevoked {
			state = "revoked"
			return nil
		}
		var artifacts []model.Artifact
		if err := tx.Where("release_id = ?", release.ID).Find(&artifacts).Error; err != nil {
			return err
		}
		matches := release.Status == model.ReleaseStatusPublished &&
			release.CommitID == manifest.Release.CommitID &&
			release.Channel == manifest.Release.Channel &&
			release.ReleaseNotes == manifest.Release.ReleaseNotes &&
			release.MinSupportedVersion == manifest.Release.MinSupportedVersion &&
			release.PublishedAt != nil && release.PublishedAt.Equal(manifest.PublishedAt) &&
			catalogArtifactsMatch(artifacts, manifest.Artifacts)
		if !matches {
			if err := replaceCatalogRelease(tx, &release, artifacts, manifest); err != nil {
				return err
			}
			state = "updated"
			return nil
		}
		state = "existing"
		return nil
	})
	return state, err
}

func replaceCatalogRelease(tx *gorm.DB, release *model.Release, existing []model.Artifact, manifest UpdaterCatalogManifest) error {
	terminalStatuses := []model.UpdateTaskStatus{
		model.UpdateTaskSucceeded, model.UpdateTaskFailed, model.UpdateTaskRolledBack,
		model.UpdateTaskCancelled, model.UpdateTaskExpired,
	}
	var tasks []model.UpdateTask
	if err := tx.Where("release_id = ? AND status NOT IN ?", release.ID, terminalStatuses).Find(&tasks).Error; err != nil {
		return err
	}
	for i := range tasks {
		if err := tx.Model(&tasks[i]).Updates(map[string]any{
			"status":             model.UpdateTaskCancelled,
			"last_error_code":    "release_republished",
			"last_error_message": "目标版本已重新发布",
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.UpdateEvent{
			TaskID: tasks[i].ID, Phase: string(model.UpdateTaskCancelled),
			ErrorCode: "release_republished", ErrorMessage: "目标版本已重新发布", Source: "server",
		}).Error; err != nil {
			return err
		}
	}

	if err := tx.Model(release).Updates(map[string]any{
		"commit_id": manifest.Release.CommitID, "channel": manifest.Release.Channel,
		"status": model.ReleaseStatusPublished, "release_notes": manifest.Release.ReleaseNotes,
		"min_supported_version": manifest.Release.MinSupportedVersion, "published_at": manifest.PublishedAt,
	}).Error; err != nil {
		return err
	}

	byPlatform := make(map[string]*model.Artifact, len(existing))
	for i := range existing {
		key := existing[i].OS + "\x00" + existing[i].Arch + "\x00" + existing[i].Role
		byPlatform[key] = &existing[i]
	}
	for _, input := range manifest.Artifacts {
		key := input.OS + "\x00" + input.Arch + "\x00" + input.Role
		if artifact, ok := byPlatform[key]; ok {
			if err := tx.Model(artifact).Updates(map[string]any{
				"package_type": input.PackageType, "filename": input.Filename,
				"download_url": input.DownloadURL, "size": input.Size, "sha256": input.SHA256,
				"signature": input.Signature, "key_id": input.KeyID,
				"status": model.ArtifactStatusAvailable,
			}).Error; err != nil {
				return err
			}
			delete(byPlatform, key)
			continue
		}
		artifact := model.Artifact{
			ID: uuid.NewString(), ReleaseID: release.ID, OS: input.OS, Arch: input.Arch, Role: input.Role,
			PackageType: input.PackageType, Filename: input.Filename, DownloadURL: input.DownloadURL,
			Size: input.Size, SHA256: input.SHA256, Signature: input.Signature, KeyID: input.KeyID,
			Status: model.ArtifactStatusAvailable,
		}
		if err := tx.Create(&artifact).Error; err != nil {
			return err
		}
	}
	for _, artifact := range byPlatform {
		if err := tx.Delete(artifact).Error; err != nil {
			return err
		}
	}
	return nil
}

func catalogArtifactsMatch(existing []model.Artifact, expected []UpdaterCatalogArtifact) bool {
	if len(existing) != len(expected) {
		return false
	}
	byPlatform := make(map[string]model.Artifact, len(existing))
	for _, artifact := range existing {
		key := artifact.OS + "\x00" + artifact.Arch + "\x00" + artifact.Role
		byPlatform[key] = artifact
	}
	for _, input := range expected {
		key := input.OS + "\x00" + input.Arch + "\x00" + input.Role
		artifact, ok := byPlatform[key]
		if !ok || artifact.PackageType != input.PackageType || artifact.Filename != input.Filename ||
			artifact.DownloadURL != input.DownloadURL || artifact.Size != input.Size || artifact.SHA256 != input.SHA256 ||
			artifact.Signature != input.Signature || artifact.KeyID != input.KeyID ||
			artifact.Status != model.ArtifactStatusAvailable {
			return false
		}
	}
	return true
}
