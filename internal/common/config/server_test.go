package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadServerConfigAdminCredentialsFromEnvironment(t *testing.T) {
	t.Setenv("ADMIN_USERNAME", "env-admin")
	t.Setenv("ADMIN_PASSWORD", "env-password")
	t.Setenv("SIGNAL_INTERNAL_TOKEN", "test-internal-token")

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
	if cfg.Security.UserSimulationMaxHours != 8 {
		t.Fatalf("user simulation max hours = %d, want 8", cfg.Security.UserSimulationMaxHours)
	}
	if !cfg.Tailscale.HeadscaleAutoSync {
		t.Fatal("headscale auto sync must default to enabled for compatibility")
	}
}

func TestLoadServerConfigHeadscaleAutoSyncOverride(t *testing.T) {
	t.Setenv("ADMIN_USERNAME", "env-admin")
	t.Setenv("ADMIN_PASSWORD", "env-password")
	t.Setenv("HEADSCALE_AUTO_SYNC", "false")
	t.Setenv("SIGNAL_INTERNAL_TOKEN", "test-internal-token")

	path := filepath.Join(t.TempDir(), "server.toml")
	if err := os.WriteFile(path, []byte("[web]\nlisten_port = 8080\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Tailscale.HeadscaleAutoSync {
		t.Fatal("HEADSCALE_AUTO_SYNC=false must disable automatic synchronization")
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

func TestResourceFeatureFlagsDefaultOffAndEnvironmentOverrides(t *testing.T) {
	for _, key := range []string{
		"SIGNAL_FEATURE_RESOURCE_MODEL_WRITE",
		"SIGNAL_FEATURE_RESOURCE_RECONCILIATION",
		"SIGNAL_FEATURE_RESOURCE_ALLOCATION",
		"SIGNAL_FEATURE_MANAGEMENT_CONTEXT_V2",
		"SIGNAL_FEATURE_MANAGEMENT_WEB_V2",
		"SIGNAL_FEATURE_TENANT_RESOURCE_READ_V2",
		"SIGNAL_FEATURE_LEGACY_WRITE_FREEZE",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("ADMIN_USERNAME", "feature-admin")
	t.Setenv("ADMIN_PASSWORD", "feature-password")
	t.Setenv("SIGNAL_INTERNAL_TOKEN", "test-internal-token")

	path := filepath.Join(t.TempDir(), "server.toml")
	data := []byte("[web]\nlisten_port = 8080\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	for _, flag := range []FeatureFlag{
		FeatureResourceModelWrite,
		FeatureResourceReconciliation,
		FeatureResourceAllocation,
		FeatureManagementContextV2,
		FeatureManagementWebV2,
		FeatureTenantResourceReadV2,
		FeatureLegacyWriteFreeze,
	} {
		if cfg.FeatureFlags.Enabled(flag) {
			t.Fatalf("feature %q must default to disabled", flag)
		}
	}

	data = []byte("[web]\nlisten_port = 8080\n[feature_flags]\nresource_model_write = true\nmanagement_web_v2 = true\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	t.Setenv("SIGNAL_FEATURE_RESOURCE_MODEL_WRITE", "false")
	t.Setenv("SIGNAL_FEATURE_RESOURCE_ALLOCATION", "true")
	t.Setenv("SIGNAL_FEATURE_TENANT_RESOURCE_READ_V2", "yes")
	cfg, err = LoadServerConfig(path)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.FeatureFlags.ResourceModelWrite {
		t.Fatal("environment must be able to disable a TOML feature flag")
	}
	if !cfg.FeatureFlags.ManagementWebV2 {
		t.Fatal("TOML feature flag was not loaded")
	}
	if !cfg.FeatureFlags.TenantResourceReadV2 {
		t.Fatal("environment must be able to enable a feature flag")
	}
	if !cfg.FeatureFlags.ResourceAllocation {
		t.Fatal("environment must be able to enable the allocation feature flag")
	}
	if cfg.FeatureFlags.Enabled(FeatureFlag("unknown")) {
		t.Fatal("unknown feature flag must fail closed")
	}
}
