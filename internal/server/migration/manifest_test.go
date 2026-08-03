package migration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildDraftAndFinalizeManifest(t *testing.T) {
	report := manifestTestReport()
	draft, err := BuildDraft(report)
	require.NoError(t, err)
	require.False(t, draft.Finalized)
	require.EqualValues(t, 4, draft.Totals.SourceCount)
	require.EqualValues(t, 1, draft.Totals.MigrateCount)
	require.EqualValues(t, 1, draft.Totals.PendingCount)
	require.EqualValues(t, 1, draft.Totals.CompatibilityCount)
	require.EqualValues(t, 1, draft.Totals.RejectedCount)
	require.Len(t, draft.ManifestHash, 64)
	require.NoError(t, Validate(draft, false))

	second, err := BuildDraft(report)
	require.NoError(t, err)
	require.Equal(t, draft.ManifestHash, second.ManifestHash)

	_, err = Finalize(draft)
	require.ErrorContains(t, err, "pending")
	edited := draft
	edited.Entries = append([]ManifestEntry(nil), draft.Entries...)
	for index := range edited.Entries {
		if edited.Entries[index].Classification == "manual" {
			edited.Entries[index].Decision = "migrate"
			edited.Entries[index].TargetType = "tenant_management_membership"
			edited.Entries[index].TargetID = "membership-confirmed"
		}
	}
	finalized, err := FinalizeAgainstBaseline(edited, draft)
	require.NoError(t, err)
	require.True(t, finalized.Finalized)
	require.Zero(t, finalized.Totals.PendingCount)
	require.NoError(t, Validate(finalized, true))

	tampered := finalized
	tampered.Entries = append([]ManifestEntry(nil), finalized.Entries...)
	tampered.Entries[0].ReasonCodes = append(tampered.Entries[0].ReasonCodes, "tampered")
	require.ErrorContains(t, Validate(tampered, true), "evidence hash mismatch")
	baselineTamper := edited
	baselineTamper.Entries = append([]ManifestEntry(nil), edited.Entries...)
	baselineTamper.Entries[0].EvidenceRefs = append(baselineTamper.Entries[0].EvidenceRefs, "forged")
	_, err = FinalizeAgainstBaseline(baselineTamper, draft)
	require.ErrorContains(t, err, "immutable evidence changed")
}

func TestManifestRejectsUnconservedOrUnsafeInput(t *testing.T) {
	report := manifestTestReport()
	report.Totals.SourceCount++
	_, err := BuildDraft(report)
	require.ErrorContains(t, err, "not conserved")

	report = manifestTestReport()
	report.Sections[0].Candidates[0].TargetCandidates = []string{"one", "two"}
	_, err = BuildDraft(report)
	require.ErrorContains(t, err, "ambiguous target")
}

func manifestTestReport() CompatReport {
	candidates := []CompatCandidate{
		{SourceType: "user", SourceID: "10", SourceRevision: "1", Classification: "automatic", TargetType: "user", TargetCandidates: []string{"10"}, ReasonCodes: []string{"stable_identity"}, EvidenceRefs: []string{"user:10"}},
		{SourceType: "admin_tenant_membership", SourceID: "20", SourceRevision: "2", Classification: "manual", TargetType: "tenant_management_membership", ReasonCodes: []string{"legacy_tenant_admin"}, EvidenceRefs: []string{"admin:1", "tenant:a"}},
		{SourceType: "acl", SourceID: "30", SourceRevision: "1", Classification: "compatibility", ReasonCodes: []string{"legacy_acl"}, EvidenceRefs: []string{"acl:30"}},
		{SourceType: "resource", SourceID: "40", SourceRevision: "1", Classification: "invalid", ReasonCodes: []string{"missing_tenant"}, EvidenceRefs: []string{"resource:40"}},
	}
	return CompatReport{
		SchemaVersion: compatSchemaVersion, SourceFingerprint: strings.Repeat("a", 64), ContentHash: strings.Repeat("b", 64),
		Sections: []CompatSection{{SourceType: "fixture", SourceCount: 4, ClassifiedCount: 4, Conserved: true, Candidates: candidates}},
		Totals:   CompatTotals{SourceCount: 4, ClassifiedCount: 4, Conserved: true},
	}
}
