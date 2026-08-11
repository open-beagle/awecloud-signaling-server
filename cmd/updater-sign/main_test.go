package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSignDigestProducesVerifiableSignature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	digest := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	encoded, err := signDigest(digest, base64.StdEncoding.EncodeToString(privateKey))
	require.NoError(t, err)
	signature, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	require.True(t, ed25519.Verify(publicKey, []byte(digest), signature))
}

func TestSignDigestRejectsInvalidInput(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	_, err = signDigest("not-a-sha256", base64.StdEncoding.EncodeToString(privateKey))
	require.Error(t, err)
}
