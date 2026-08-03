package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/migration"
)

func TestRunBuildsAndFinalizesManifest(t *testing.T) {
	directory := t.TempDir()
	reportPath := filepath.Join(directory, "compat.json")
	draftPath := filepath.Join(directory, "mapping.draft.json")
	editedPath := filepath.Join(directory, "mapping.edited.json")
	finalPath := filepath.Join(directory, "mapping.final.json")
	report := migration.CompatReport{
		SchemaVersion: "resource-business-compat/v1", SourceFingerprint: strings.Repeat("a", 64), ContentHash: strings.Repeat("b", 64),
		Sections: []migration.CompatSection{{SourceType: "admin", SourceCount: 1, ClassifiedCount: 1, Conserved: true, Candidates: []migration.CompatCandidate{{SourceType: "admin", SourceID: "1", SourceRevision: "1", Classification: "manual", TargetType: "user", ReasonCodes: []string{"manual_identity"}}}}},
		Totals:   migration.CompatTotals{SourceCount: 1, ClassifiedCount: 1, Conserved: true},
	}
	content, err := json.Marshal(report)
	require.NoError(t, err)
	content = append(content[:len(content)-1], []byte(`,"audit_sources":[]}`)...)
	require.NoError(t, os.WriteFile(reportPath, content, 0o600))
	require.NoError(t, run(reportPath, "", "", draftPath, false))

	var draft migration.Manifest
	draftContent, err := os.ReadFile(draftPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(draftContent, &draft))
	require.EqualValues(t, 1, draft.Totals.PendingCount)
	draft.Entries[0].Decision = "migrate"
	draft.Entries[0].TargetType = "user"
	draft.Entries[0].TargetID = "100"
	draftContent, err = json.Marshal(draft)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(editedPath, draftContent, 0o600))
	require.NoError(t, run("", editedPath, draftPath, finalPath, true))

	var finalized migration.Manifest
	finalContent, err := os.ReadFile(finalPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(finalContent, &finalized))
	require.True(t, finalized.Finalized)
	require.Len(t, finalized.ManifestHash, 64)
	require.NoError(t, migration.Validate(finalized, true))
	require.Error(t, run("", finalPath, "", finalPath, false))
}
