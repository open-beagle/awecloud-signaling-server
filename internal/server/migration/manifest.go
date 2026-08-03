package migration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	ManifestSchemaVersion = "resource-business-mapping/v1"
	compatSchemaVersion   = "resource-business-compat/v1"
)

type CompatReport struct {
	SchemaVersion     string          `json:"schema_version"`
	SourceFingerprint string          `json:"source_fingerprint"`
	ContentHash       string          `json:"content_hash"`
	Sections          []CompatSection `json:"sections"`
	Totals            CompatTotals    `json:"totals"`
}

type CompatSection struct {
	SourceType      string            `json:"source_type"`
	SourceCount     int64             `json:"source_count"`
	ClassifiedCount int64             `json:"classified_count"`
	Conserved       bool              `json:"conserved"`
	Candidates      []CompatCandidate `json:"candidates"`
}

type CompatTotals struct {
	SourceCount     int64 `json:"source_count"`
	ClassifiedCount int64 `json:"classified_count"`
	Conserved       bool  `json:"conserved"`
}

type CompatCandidate struct {
	SourceType       string   `json:"source_type"`
	SourceID         string   `json:"source_id"`
	SourceRevision   string   `json:"source_revision"`
	Classification   string   `json:"classification"`
	TargetType       string   `json:"target_type,omitempty"`
	TargetCandidates []string `json:"target_candidates"`
	ReasonCodes      []string `json:"reason_codes"`
	EvidenceRefs     []string `json:"evidence_refs"`
}

type Manifest struct {
	SchemaVersion     string          `json:"schema_version"`
	SourceSchema      string          `json:"source_schema"`
	SourceFingerprint string          `json:"source_fingerprint"`
	SourceContentHash string          `json:"source_content_hash"`
	Finalized         bool            `json:"finalized"`
	ManifestHash      string          `json:"manifest_hash,omitempty"`
	Totals            ManifestTotals  `json:"totals"`
	Entries           []ManifestEntry `json:"entries"`
}

type ManifestTotals struct {
	SourceCount        int64 `json:"source_count"`
	MigrateCount       int64 `json:"migrate_count"`
	PendingCount       int64 `json:"pending_count"`
	CompatibilityCount int64 `json:"compatibility_count"`
	RejectedCount      int64 `json:"rejected_count"`
}

type ManifestEntry struct {
	SourceType     string   `json:"source_type"`
	SourceID       string   `json:"source_id"`
	SourceRevision string   `json:"source_revision"`
	Classification string   `json:"classification"`
	Decision       string   `json:"decision"`
	TargetType     string   `json:"target_type,omitempty"`
	TargetID       string   `json:"target_id,omitempty"`
	ReasonCodes    []string `json:"reason_codes"`
	EvidenceRefs   []string `json:"evidence_refs"`
	EvidenceHash   string   `json:"evidence_hash"`
}

func BuildDraft(report CompatReport) (Manifest, error) {
	if err := validateReport(report); err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{
		SchemaVersion: ManifestSchemaVersion, SourceSchema: report.SchemaVersion,
		SourceFingerprint: strings.ToLower(report.SourceFingerprint), SourceContentHash: strings.ToLower(report.ContentHash),
		Entries: make([]ManifestEntry, 0, report.Totals.SourceCount),
	}
	for _, section := range report.Sections {
		for _, candidate := range section.Candidates {
			entry := ManifestEntry{
				SourceType: strings.TrimSpace(candidate.SourceType), SourceID: strings.TrimSpace(candidate.SourceID), SourceRevision: strings.TrimSpace(candidate.SourceRevision),
				Classification: strings.TrimSpace(candidate.Classification), ReasonCodes: normalizedStrings(candidate.ReasonCodes), EvidenceRefs: normalizedStrings(candidate.EvidenceRefs),
			}
			switch entry.Classification {
			case "automatic":
				if candidate.TargetType == "" || len(candidate.TargetCandidates) > 1 {
					return Manifest{}, fmt.Errorf("automatic source %s/%s has an ambiguous target", entry.SourceType, entry.SourceID)
				}
				entry.Decision, entry.TargetType, entry.TargetID = "migrate", candidate.TargetType, entry.SourceID
				if len(candidate.TargetCandidates) == 1 {
					entry.TargetID = strings.TrimSpace(candidate.TargetCandidates[0])
				}
			case "manual":
				entry.Decision = "pending"
				entry.TargetType = strings.TrimSpace(candidate.TargetType)
			case "compatibility":
				entry.Decision = "preserve_compatibility"
			case "invalid":
				entry.Decision = "reject"
			default:
				return Manifest{}, fmt.Errorf("unsupported classification %q", entry.Classification)
			}
			entry.EvidenceHash = evidenceHash(entry)
			manifest.Entries = append(manifest.Entries, entry)
		}
	}
	sortEntries(manifest.Entries)
	manifest.Totals = calculateTotals(manifest.Entries)
	if manifest.Totals.SourceCount != report.Totals.SourceCount {
		return Manifest{}, errors.New("manifest source count is not conserved")
	}
	manifest.ManifestHash = manifestHash(manifest)
	return manifest, nil
}

func Finalize(manifest Manifest) (Manifest, error) {
	manifest.Finalized = true
	manifest.Totals = calculateTotals(manifest.Entries)
	manifest.ManifestHash = ""
	if err := Validate(manifest, true); err != nil {
		return Manifest{}, err
	}
	manifest.ManifestHash = manifestHash(manifest)
	return manifest, nil
}

func FinalizeAgainstBaseline(edited, baseline Manifest) (Manifest, error) {
	if err := Validate(baseline, false); err != nil {
		return Manifest{}, fmt.Errorf("invalid baseline manifest: %w", err)
	}
	if baseline.Finalized || baseline.ManifestHash == "" {
		return Manifest{}, errors.New("baseline must be a hashed draft manifest")
	}
	if edited.SchemaVersion != baseline.SchemaVersion || edited.SourceSchema != baseline.SourceSchema ||
		strings.ToLower(edited.SourceFingerprint) != strings.ToLower(baseline.SourceFingerprint) ||
		strings.ToLower(edited.SourceContentHash) != strings.ToLower(baseline.SourceContentHash) || len(edited.Entries) != len(baseline.Entries) {
		return Manifest{}, errors.New("edited manifest does not match its baseline identity")
	}
	baselineEntries := append([]ManifestEntry(nil), baseline.Entries...)
	editedEntries := append([]ManifestEntry(nil), edited.Entries...)
	sortEntries(baselineEntries)
	sortEntries(editedEntries)
	for index := range baselineEntries {
		base, candidate := baselineEntries[index], editedEntries[index]
		if base.SourceType != candidate.SourceType || base.SourceID != candidate.SourceID || base.SourceRevision != candidate.SourceRevision ||
			base.Classification != candidate.Classification || base.EvidenceHash != candidate.EvidenceHash ||
			strings.Join(base.ReasonCodes, "\x00") != strings.Join(candidate.ReasonCodes, "\x00") ||
			strings.Join(base.EvidenceRefs, "\x00") != strings.Join(candidate.EvidenceRefs, "\x00") {
			return Manifest{}, fmt.Errorf("immutable evidence changed for %s/%s", base.SourceType, base.SourceID)
		}
	}
	edited.Entries = editedEntries
	return Finalize(edited)
}

func Validate(manifest Manifest, requireFinal bool) error {
	if manifest.SchemaVersion != ManifestSchemaVersion || manifest.SourceSchema != compatSchemaVersion {
		return errors.New("unsupported manifest or source schema")
	}
	if !validSHA256(manifest.SourceFingerprint) || !validSHA256(manifest.SourceContentHash) {
		return errors.New("invalid source fingerprint or content hash")
	}
	seen := make(map[string]struct{}, len(manifest.Entries))
	for i := range manifest.Entries {
		entry := &manifest.Entries[i]
		if entry.SourceType == "" || entry.SourceID == "" || entry.SourceRevision == "" {
			return errors.New("manifest source identity is incomplete")
		}
		key := entry.SourceType + "\x00" + entry.SourceID + "\x00" + entry.SourceRevision
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate manifest source %s/%s", entry.SourceType, entry.SourceID)
		}
		seen[key] = struct{}{}
		if evidenceHash(*entry) != strings.ToLower(entry.EvidenceHash) {
			return fmt.Errorf("evidence hash mismatch for %s/%s", entry.SourceType, entry.SourceID)
		}
		if err := validateDecision(*entry, requireFinal); err != nil {
			return fmt.Errorf("%s/%s: %w", entry.SourceType, entry.SourceID, err)
		}
	}
	totals := calculateTotals(manifest.Entries)
	if totals != manifest.Totals {
		return errors.New("manifest totals do not match entries")
	}
	if requireFinal && (!manifest.Finalized || totals.PendingCount != 0) {
		return errors.New("final manifest contains pending decisions")
	}
	if manifest.ManifestHash != "" && manifestHash(manifest) != strings.ToLower(manifest.ManifestHash) {
		return errors.New("manifest hash mismatch")
	}
	return nil
}

func validateReport(report CompatReport) error {
	if report.SchemaVersion != compatSchemaVersion || !validSHA256(report.SourceFingerprint) || !validSHA256(report.ContentHash) {
		return errors.New("invalid compatibility report identity")
	}
	if !report.Totals.Conserved || report.Totals.SourceCount != report.Totals.ClassifiedCount {
		return errors.New("compatibility report totals are not conserved")
	}
	var sourceCount int64
	for _, section := range report.Sections {
		if !section.Conserved || section.SourceCount != section.ClassifiedCount || int64(len(section.Candidates)) != section.ClassifiedCount {
			return fmt.Errorf("compatibility section %q is not conserved", section.SourceType)
		}
		sourceCount += section.SourceCount
	}
	if sourceCount != report.Totals.SourceCount {
		return errors.New("compatibility section totals do not match report")
	}
	return nil
}

func validateDecision(entry ManifestEntry, requireFinal bool) error {
	switch entry.Decision {
	case "migrate":
		if entry.TargetType == "" || entry.TargetID == "" {
			return errors.New("migrate decision requires an explicit target")
		}
	case "pending":
		if requireFinal || entry.Classification != "manual" {
			return errors.New("pending is only allowed for manual draft entries")
		}
	case "preserve_compatibility":
		if entry.Classification != "compatibility" && entry.Classification != "manual" {
			return errors.New("compatibility decision does not match classification")
		}
	case "reject":
		if entry.Classification == "automatic" {
			return errors.New("automatic source cannot be rejected without reclassification")
		}
	default:
		return fmt.Errorf("unsupported decision %q", entry.Decision)
	}
	return nil
}

func calculateTotals(entries []ManifestEntry) ManifestTotals {
	totals := ManifestTotals{SourceCount: int64(len(entries))}
	for _, entry := range entries {
		switch entry.Decision {
		case "migrate":
			totals.MigrateCount++
		case "pending":
			totals.PendingCount++
		case "preserve_compatibility":
			totals.CompatibilityCount++
		case "reject":
			totals.RejectedCount++
		}
	}
	return totals
}

func evidenceHash(entry ManifestEntry) string {
	payload := struct {
		SourceType, SourceID, SourceRevision, Classification string
		ReasonCodes, EvidenceRefs                            []string
	}{entry.SourceType, entry.SourceID, entry.SourceRevision, entry.Classification, normalizedStrings(entry.ReasonCodes), normalizedStrings(entry.EvidenceRefs)}
	content, _ := json.Marshal(payload)
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func manifestHash(manifest Manifest) string {
	manifest.ManifestHash = ""
	content, _ := json.Marshal(manifest)
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func normalizedStrings(values []string) []string {
	result := append([]string(nil), values...)
	for i := range result {
		result[i] = strings.TrimSpace(result[i])
	}
	sort.Strings(result)
	return result
}

func sortEntries(entries []ManifestEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].SourceType != entries[j].SourceType {
			return entries[i].SourceType < entries[j].SourceType
		}
		if entries[i].SourceID != entries[j].SourceID {
			return entries[i].SourceID < entries[j].SourceID
		}
		return entries[i].SourceRevision < entries[j].SourceRevision
	})
}
func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
