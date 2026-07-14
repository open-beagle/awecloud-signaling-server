package api

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestVerifyArtifactSignature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	artifact := model.Artifact{SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	artifact.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(artifact.SHA256)))

	require.NoError(t, verifyArtifactSignature(publicKey, artifact))
	artifact.Signature = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	require.Error(t, verifyArtifactSignature(publicKey, artifact))
}
