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
