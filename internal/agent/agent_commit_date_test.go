package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewAgentRejectsInvalidCommitDate(t *testing.T) {
	_, err := NewAgent(nil, "v1.0.0", strings.Repeat("a", 40), "2026-08-12 18:42:06", "2026-08-15_10:00:00")
	require.ErrorContains(t, err, "Commit Date")
}
