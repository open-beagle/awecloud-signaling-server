package agent

import "testing"

func TestValidGitCommit(t *testing.T) {
	valid := "0123456789abcdef0123456789abcdef01234567"
	if !validGitCommit(valid) {
		t.Fatalf("expected full lowercase commit to be valid")
	}
	for _, invalid := range []string{"unknown", valid[:8], "0123456789ABCDEF0123456789ABCDEF01234567"} {
		if validGitCommit(invalid) {
			t.Fatalf("expected %q to be invalid", invalid)
		}
	}
}

func TestAgentUpdaterRequiresSigningIdentity(t *testing.T) {
	t.Setenv("SIGNAL_UPDATER_PUBLIC_KEY", "")
	t.Setenv("SIGNAL_UPDATER_KEY_ID", "")
	_, err := newAgentUpdateManager("v1.0.0", "0123456789abcdef0123456789abcdef01234567", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err == nil {
		t.Fatal("expected missing updater signing identity to fail")
	}
}
