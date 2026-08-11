package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fatal("usage: updater-sign <lowercase-sha256>")
	}
	signature, err := signDigest(os.Args[1], os.Getenv("SIGNAL_UPDATER_PRIVATE_KEY"))
	if err != nil {
		fatal(err.Error())
	}
	fmt.Print(signature)
}

func signDigest(rawDigest, encodedKey string) (string, error) {
	digest := strings.TrimSpace(rawDigest)
	if len(digest) != 64 || digest != strings.ToLower(digest) || strings.Trim(digest, "0123456789abcdef") != "" {
		return "", errors.New("digest must be a 64-character lowercase SHA256")
	}
	privateKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedKey))
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		return "", errors.New("SIGNAL_UPDATER_PRIVATE_KEY must be a base64-encoded Ed25519 private key")
	}
	signature := ed25519.Sign(ed25519.PrivateKey(privateKey), []byte(digest))
	return base64.StdEncoding.EncodeToString(signature), nil
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
