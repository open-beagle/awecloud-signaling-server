package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type updaterCatalogMemoryStore struct {
	objects map[string][]byte
}

func (s *updaterCatalogMemoryStore) ListManifestKeys(context.Context) ([]string, error) {
	keys := make([]string, 0, len(s.objects))
	for key := range s.objects {
		keys = append(keys, key)
	}
	return keys, nil
}

func (s *updaterCatalogMemoryStore) ReadManifest(_ context.Context, key string) ([]byte, error) {
	return s.objects[key], nil
}

func TestUpdaterCatalogSyncCreatesPublishedReleaseAndIsIdempotent(t *testing.T) {
	database := updaterCatalogTestDB(t)
	store := &updaterCatalogMemoryStore{objects: map[string][]byte{
		"updater/releases/agent/0123456789abcdef0123456789abcdef01234567.json": updaterCatalogTestManifest(t, updaterCatalogArtifactSHA),
	}}
	syncer, err := NewUpdaterCatalogServiceWithStore(database, store, "https://minio.example/vscode/awecloud-signaling")
	require.NoError(t, err)

	result, err := syncer.Sync(context.Background())
	require.NoError(t, err)
	require.Equal(t, UpdaterCatalogSyncResult{Scanned: 1, Created: 1}, result)

	var release model.Release
	require.NoError(t, database.First(&release).Error)
	require.Equal(t, model.ReleaseStatusPublished, release.Status)
	require.Equal(t, "0123456789abcdef0123456789abcdef01234567", release.CommitID)
	var artifacts []model.Artifact
	require.NoError(t, database.Where("release_id = ?", release.ID).Find(&artifacts).Error)
	require.Len(t, artifacts, 1)
	require.Equal(t, updaterCatalogArtifactSHA, artifacts[0].SHA256)

	result, err = syncer.Sync(context.Background())
	require.NoError(t, err)
	require.Equal(t, UpdaterCatalogSyncResult{Scanned: 1, Existing: 1}, result)
}

func TestUpdaterCatalogSyncOverwritesSameVersionWithDifferentCommit(t *testing.T) {
	database := updaterCatalogTestDB(t)
	firstCommit := "0123456789abcdef0123456789abcdef01234567"
	secondCommit := "abcdef0123456789abcdef0123456789abcdef01"
	store := &updaterCatalogMemoryStore{objects: map[string][]byte{
		"updater/releases/agent/latest.json": updaterCatalogTestManifestForCommit(t, updaterCatalogArtifactSHA, firstCommit),
	}}
	syncer, err := NewUpdaterCatalogServiceWithStore(database, store, "https://minio.example/vscode/awecloud-signaling")
	require.NoError(t, err)
	result, err := syncer.Sync(context.Background())
	require.NoError(t, err)
	require.Equal(t, UpdaterCatalogSyncResult{Scanned: 1, Created: 1}, result)

	store.objects["updater/releases/agent/latest.json"] = updaterCatalogTestManifestForCommit(t, strings.Repeat("a", 64), secondCommit)
	result, err = syncer.Sync(context.Background())
	require.NoError(t, err)
	require.Equal(t, UpdaterCatalogSyncResult{Scanned: 1, Updated: 1}, result)

	var releases []model.Release
	require.NoError(t, database.Where("component = ? AND version = ?", model.ComponentAgent, "v1.0.0").Find(&releases).Error)
	require.Len(t, releases, 1)
	require.Equal(t, secondCommit, releases[0].CommitID)
	var artifact model.Artifact
	require.NoError(t, database.Where("release_id = ?", releases[0].ID).First(&artifact).Error)
	require.Equal(t, strings.Repeat("a", 64), artifact.SHA256)
}

func TestUpdaterCatalogSyncOverwritesArtifactMutation(t *testing.T) {
	database := updaterCatalogTestDB(t)
	store := &updaterCatalogMemoryStore{objects: map[string][]byte{
		"updater/releases/agent/release.json": updaterCatalogTestManifest(t, updaterCatalogArtifactSHA),
	}}
	syncer, err := NewUpdaterCatalogServiceWithStore(database, store, "https://minio.example/vscode/awecloud-signaling")
	require.NoError(t, err)
	_, err = syncer.Sync(context.Background())
	require.NoError(t, err)

	store.objects["updater/releases/agent/release.json"] = updaterCatalogTestManifest(t, "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
	result, err := syncer.Sync(context.Background())
	require.NoError(t, err)
	require.Equal(t, UpdaterCatalogSyncResult{Scanned: 1, Updated: 1}, result)

	var artifact model.Artifact
	require.NoError(t, database.First(&artifact).Error)
	require.Equal(t, "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", artifact.SHA256)
}

func TestUpdaterCatalogSyncCancelsActiveTaskWhenVersionIsRepublished(t *testing.T) {
	database := updaterCatalogTestDB(t)
	require.NoError(t, database.AutoMigrate(&model.Node{}, &model.UpdateTask{}, &model.UpdateEvent{}))
	firstCommit := "0123456789abcdef0123456789abcdef01234567"
	secondCommit := "abcdef0123456789abcdef0123456789abcdef01"
	store := &updaterCatalogMemoryStore{objects: map[string][]byte{
		"updater/releases/agent/latest.json": updaterCatalogTestManifestForCommit(t, updaterCatalogArtifactSHA, firstCommit),
	}}
	syncer, err := NewUpdaterCatalogServiceWithStore(database, store, "https://minio.example/vscode/awecloud-signaling")
	require.NoError(t, err)
	_, err = syncer.Sync(context.Background())
	require.NoError(t, err)

	var firstRelease model.Release
	require.NoError(t, database.Where("component = ? AND commit_id = ?", model.ComponentAgent, firstCommit).First(&firstRelease).Error)
	node := model.Node{
		ID: 1, UserID: 1, Name: "agent-1", Type: model.NodeTypeAgent,
		UpdaterProtocol: "v2", SystemInfo: `{"os":"linux","arch":"amd64"}`,
	}
	require.NoError(t, database.Create(&node).Error)
	updateService := NewUpdateService(database)
	task, err := updateService.CreateTask(context.Background(), CreateUpdateTaskInput{
		Component: model.ComponentAgent, TargetType: model.UpdateTargetNode,
		TargetID: "1", ReleaseID: firstRelease.ID,
	})
	require.NoError(t, err)

	store.objects["updater/releases/agent/latest.json"] = updaterCatalogTestManifestForCommit(t, strings.Repeat("a", 64), secondCommit)
	result, err := syncer.Sync(context.Background())
	require.NoError(t, err)
	require.Equal(t, UpdaterCatalogSyncResult{Scanned: 1, Updated: 1}, result)

	directives, err := updateService.DirectivesForNode(context.Background(), node.ID, model.ComponentAgent)
	require.NoError(t, err)
	require.Empty(t, directives)
	var updatedTask model.UpdateTask
	require.NoError(t, database.First(&updatedTask, "id = ?", task.ID).Error)
	require.Equal(t, model.UpdateTaskCancelled, updatedTask.Status)
	require.Equal(t, "release_republished", updatedTask.LastErrorCode)
	var updatedRelease model.Release
	require.NoError(t, database.First(&updatedRelease, "id = ?", firstRelease.ID).Error)
	require.Equal(t, secondCommit, updatedRelease.CommitID)
}

func TestHTTPUpdaterCatalogStoreReadsPublicHTTPSCatalog(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/vscode/awecloud-signaling/updater/catalog.json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"schema_version":1,"generated_at":"2026-08-09T08:00:00Z","manifests":[%q]}`,
				server.URL+"/vscode/awecloud-signaling/updater/releases/agent/0123456789abcdef0123456789abcdef01234567.json")
		case "/vscode/awecloud-signaling/updater/releases/agent/0123456789abcdef0123456789abcdef01234567.json":
			_, _ = w.Write(updaterCatalogTestManifest(t, updaterCatalogArtifactSHA))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	store, _, err := newHTTPUpdaterCatalogStore(config.UpdaterSection{
		CatalogURL:      server.URL + "/vscode/awecloud-signaling/updater/catalog.json",
		ArtifactBaseURL: server.URL + "/vscode/awecloud-signaling",
	})
	require.NoError(t, err)
	httpStore := store.(*httpUpdaterCatalogStore)
	testClient := server.Client()
	testClient.CheckRedirect = httpStore.client.CheckRedirect
	httpStore.client = testClient

	keys, err := httpStore.ListManifestKeys(context.Background())
	require.NoError(t, err)
	require.Len(t, keys, 1)
	manifest, err := httpStore.ReadManifest(context.Background(), keys[0])
	require.NoError(t, err)
	require.NotEmpty(t, manifest)
}

func TestHTTPUpdaterCatalogStoreRejectsUnsafeManifestEntries(t *testing.T) {
	tests := map[string]string{
		"cross origin":     `{"schema_version":1,"generated_at":"2026-08-09T08:00:00Z","manifests":["https://other.example/updater/releases/agent/release.json"]}`,
		"outside releases": `{"schema_version":1,"generated_at":"2026-08-09T08:00:00Z","manifests":["https://minio.example/vscode/awecloud-signaling/updater/catalog-copy.json"]}`,
		"duplicate":        `{"schema_version":1,"generated_at":"2026-08-09T08:00:00Z","manifests":["https://minio.example/vscode/awecloud-signaling/updater/releases/agent/release.json","https://minio.example/vscode/awecloud-signaling/updater/releases/agent/release.json"]}`,
		"trailing JSON":    `{"schema_version":1,"generated_at":"2026-08-09T08:00:00Z","manifests":[]} {}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			store := &httpUpdaterCatalogStore{
				client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
				})},
				catalogURL:      mustParseUpdaterCatalogURL(t, "https://minio.example/vscode/awecloud-signaling/updater/catalog.json"),
				artifactBaseURL: mustParseUpdaterCatalogURL(t, "https://minio.example/vscode/awecloud-signaling"),
			}
			_, err := store.ListManifestKeys(context.Background())
			require.Error(t, err)
		})
	}
}

func TestHTTPUpdaterCatalogStoreRejectsCrossOriginRedirect(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://other.example/updater/catalog.json", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	store, _, err := newHTTPUpdaterCatalogStore(config.UpdaterSection{
		CatalogURL:      server.URL + "/vscode/awecloud-signaling/updater/catalog.json",
		ArtifactBaseURL: server.URL + "/vscode/awecloud-signaling",
	})
	require.NoError(t, err)
	httpStore := store.(*httpUpdaterCatalogStore)
	testClient := server.Client()
	testClient.CheckRedirect = httpStore.client.CheckRedirect
	httpStore.client = testClient

	_, err = httpStore.ListManifestKeys(context.Background())
	require.ErrorContains(t, err, "redirect left artifact_base_url")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func mustParseUpdaterCatalogURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	return parsed
}

const updaterCatalogArtifactSHA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func updaterCatalogTestManifest(t *testing.T, sha256 string) []byte {
	return updaterCatalogTestManifestForCommit(t, sha256, "0123456789abcdef0123456789abcdef01234567")
}

func updaterCatalogTestManifestForCommit(t *testing.T, sha256, commitID string) []byte {
	t.Helper()
	manifest := UpdaterCatalogManifest{
		SchemaVersion: 1,
		PublishedAt:   time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC),
		Release: UpdaterCatalogRelease{
			Component: model.ComponentAgent, Version: "v1.0.0",
			CommitID: commitID, Channel: "stable",
			ReleaseNotes: "Agent build",
		},
		Artifacts: []UpdaterCatalogArtifact{{
			OS: "linux", Arch: "amd64", Role: "app", PackageType: "binary", Filename: "signal_agent",
			DownloadURL: "https://minio.example/vscode/awecloud-signaling/agent/artifacts/linux/amd64/sha/signal_agent",
			Size:        128, SHA256: sha256,
		}},
	}
	data, err := json.Marshal(manifest)
	require.NoError(t, err)
	return data
}

func updaterCatalogTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.Release{}, &model.Artifact{}, &model.UpdateTask{}, &model.UpdateEvent{}))
	return database
}
