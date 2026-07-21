package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadServerConfigAdminCredentialsFromEnvironment(t *testing.T) {
	t.Setenv("ADMIN_USERNAME", "env-admin")
	t.Setenv("ADMIN_PASSWORD", "env-password")

	path := filepath.Join(t.TempDir(), "server.toml")
	if err := os.WriteFile(path, []byte("[web]\nlisten_port = 8080\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Web.DefaultAdminUsername != "env-admin" {
		t.Fatalf("username = %q, want env-admin", cfg.Web.DefaultAdminUsername)
	}
	if cfg.Web.DefaultAdminPassword != "env-password" {
		t.Fatalf("password = %q, want env-password", cfg.Web.DefaultAdminPassword)
	}
}

func TestLoadServerConfigRequiresAdminCredentials(t *testing.T) {
	t.Setenv("ADMIN_USERNAME", "")
	t.Setenv("ADMIN_PASSWORD", "")

	path := filepath.Join(t.TempDir(), "server.toml")
	if err := os.WriteFile(path, []byte("[web]\nlisten_port = 8080\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := LoadServerConfig(path); err == nil {
		t.Fatal("expected missing admin credentials to fail")
	}
}
