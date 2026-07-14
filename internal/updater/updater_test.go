package updater

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDownloadAndVerifyStagesSignedArtifact(t *testing.T) {
	payload := []byte("signed signal artifact")
	digest := sha256.Sum256(payload)
	digestText := hex.EncodeToString(digest[:])
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(digestText)))

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	manager, err := NewManager(Config{
		Component:       "agent",
		CurrentVersion:  "v1.0.0",
		StateDir:        t.TempDir(),
		CurrentLink:     "/tmp/signal_agent",
		ServiceName:     "signal-agent",
		PublicKeyBase64: base64.StdEncoding.EncodeToString(publicKey),
	})
	require.NoError(t, err)
	manager.client = server.Client()

	staged, err := manager.downloadAndVerify(Directive{
		TaskID:      "task-1",
		Component:   "agent",
		Version:     "v1.2.3",
		DownloadURL: server.URL,
		Filename:    "signal_agent-v1.2.3-linux-amd64",
		Size:        int64(len(payload)),
		SHA256:      digestText,
		Signature:   signature,
	})
	require.NoError(t, err)
	content, err := os.ReadFile(staged)
	require.NoError(t, err)
	require.Equal(t, payload, content)
}

func TestDownloadAndVerifyRejectsInvalidChecksum(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("x"))
	}))
	defer server.Close()
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	manager, err := NewManager(Config{
		Component:       "agent",
		CurrentVersion:  "v1.0.0",
		StateDir:        t.TempDir(),
		CurrentLink:     "/tmp/signal_agent",
		ServiceName:     "signal-agent",
		PublicKeyBase64: base64.StdEncoding.EncodeToString(publicKey),
	})
	require.NoError(t, err)
	manager.client = server.Client()
	_, err = manager.downloadAndVerify(Directive{
		TaskID:      "task-1",
		Component:   "agent",
		Version:     "v1.2.3",
		DownloadURL: server.URL,
		Filename:    "signal_agent",
		Size:        1,
		SHA256:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Signature:   base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)),
	})
	require.EqualError(t, err, "checksum_mismatch")
}

func TestSwitchSymlinkIsAtomicReplacement(t *testing.T) {
	directory := t.TempDir()
	linkPath := filepath.Join(directory, "signal_agent")
	require.NoError(t, os.Symlink("old", linkPath))
	require.NoError(t, switchSymlink(linkPath, "new"))
	target, err := os.Readlink(linkPath)
	require.NoError(t, err)
	require.Equal(t, "new", target)
}
