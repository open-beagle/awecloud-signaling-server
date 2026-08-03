package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	serverdb "github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/migration"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

var safeTableName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type tableSnapshot struct {
	Table   string   `json:"table"`
	Columns []string `json:"columns"`
	Count   int64    `json:"count"`
	Hash    string   `json:"hash"`
}

type rehearsalReport struct {
	SchemaVersion     string          `json:"schema_version"`
	SourceFingerprint string          `json:"source_fingerprint"`
	ManifestHash      string          `json:"manifest_hash"`
	BeforeFingerprint string          `json:"before_fingerprint"`
	AfterFingerprint  string          `json:"after_fingerprint"`
	Integrity         string          `json:"integrity"`
	CountsConserved   bool            `json:"counts_conserved"`
	ContentPreserved  bool            `json:"content_preserved"`
	Replay            bool            `json:"replay"`
	BatchID           string          `json:"batch_id"`
	BatchStatus       string          `json:"batch_status"`
	SourceCount       int64           `json:"source_count"`
	MigratedCount     int64           `json:"migrated_count"`
	SkippedCount      int64           `json:"skipped_count"`
	FailedCount       int64           `json:"failed_count"`
	Tables            []tableSnapshot `json:"tables"`
}

func main() {
	databasePath := flag.String("database-copy", "", "local SQLite copy to migrate in place")
	manifestPath := flag.String("manifest", "", "finalized mapping manifest JSON")
	outputPath := flag.String("output", "", "rehearsal report JSON")
	flag.Parse()
	if err := run(*databasePath, *manifestPath, *outputPath); err != nil {
		fmt.Fprintln(os.Stderr, "migration-rehearsal:", err)
		os.Exit(1)
	}
}

func run(databasePath, manifestPath, outputPath string) error {
	if databasePath == "" || manifestPath == "" || outputPath == "" {
		return errors.New("-database-copy, -manifest and -output are required")
	}
	absDatabase, err := filepath.Abs(databasePath)
	if err != nil {
		return err
	}
	absManifest, err := filepath.Abs(manifestPath)
	if err != nil {
		return err
	}
	absOutput, err := filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	if absDatabase == absManifest || absDatabase == absOutput || absManifest == absOutput {
		return errors.New("database, manifest and output must be different files")
	}
	manifest, err := loadManifest(absManifest)
	if err != nil {
		return err
	}
	if err := migration.Validate(manifest, true); err != nil {
		return err
	}

	replayBatch, replay, err := completedBatch(absDatabase, manifest.ManifestHash)
	if err != nil {
		return err
	}
	beforeFingerprint, err := fileSHA256(absDatabase)
	if err != nil {
		return err
	}
	if !replay && beforeFingerprint != manifest.SourceFingerprint {
		return fmt.Errorf("database fingerprint %s does not match manifest source %s", beforeFingerprint, manifest.SourceFingerprint)
	}
	if replay {
		migrated, skipped, failed, countErr := mappingCountsReadOnly(absDatabase, replayBatch.ID)
		if countErr != nil {
			return countErr
		}
		integrity, integrityErr := integrityCheckReadOnly(absDatabase)
		if integrityErr != nil {
			return integrityErr
		}
		afterFingerprint, fingerprintErr := fileSHA256(absDatabase)
		if fingerprintErr != nil {
			return fingerprintErr
		}
		report := rehearsalReport{
			SchemaVersion: "resource-business-rehearsal/v1", SourceFingerprint: manifest.SourceFingerprint,
			ManifestHash: manifest.ManifestHash, BeforeFingerprint: beforeFingerprint, AfterFingerprint: afterFingerprint,
			Integrity: integrity, CountsConserved: true, ContentPreserved: beforeFingerprint == afterFingerprint,
			Replay: true, BatchID: replayBatch.ID, BatchStatus: string(replayBatch.Status), SourceCount: manifest.Totals.SourceCount,
			MigratedCount: migrated, SkippedCount: skipped, FailedCount: failed,
		}
		if report.Integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed: %s", report.Integrity)
		}
		if !report.ContentPreserved {
			return errors.New("completed-batch replay modified the database")
		}
		return writeReport(absOutput, report)
	}

	var before []tableSnapshot
	before, err = snapshotTables(absDatabase, manifest.Entries, nil)
	if err != nil {
		return err
	}
	if err := serverdb.InitDB(config.DatabaseSection{Path: absDatabase}); err != nil {
		return err
	}

	report := rehearsalReport{
		SchemaVersion: "resource-business-rehearsal/v1", SourceFingerprint: manifest.SourceFingerprint,
		ManifestHash: manifest.ManifestHash, BeforeFingerprint: beforeFingerprint, Replay: replay, SourceCount: manifest.Totals.SourceCount,
	}
	post, err := snapshotTables(absDatabase, manifest.Entries, snapshotColumns(before))
	if err != nil {
		return err
	}
	report.Tables = post
	report.CountsConserved, report.ContentPreserved = compareSnapshots(before, post)
	if !report.CountsConserved || !report.ContentPreserved {
		return errors.New("source table content changed during schema migration")
	}
	batch, migrated, skipped, failed, err := applyManifest(context.Background(), manifest)
	if err != nil {
		return err
	}
	report.BatchID, report.BatchStatus = batch.ID, string(batch.Status)
	report.MigratedCount, report.SkippedCount, report.FailedCount = migrated, skipped, failed
	if err := serverdb.DB.Raw("PRAGMA integrity_check").Scan(&report.Integrity).Error; err != nil {
		return err
	}
	if report.Integrity != "ok" {
		return fmt.Errorf("SQLite integrity check failed: %s", report.Integrity)
	}
	report.AfterFingerprint, err = fileSHA256(absDatabase)
	if err != nil {
		return err
	}
	return writeReport(absOutput, report)
}

func writeReport(outputPath string, report rehearsalReport) error {
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, append(content, '\n'), 0o600)
}

func applyManifest(ctx context.Context, manifest migration.Manifest) (*model.MigrationBatch, int64, int64, int64, error) {
	migrationService := service.NewResourceMigrationService(serverdb.DB)
	var batch model.MigrationBatch
	err := serverdb.DB.Where("manifest_hash = ?", manifest.ManifestHash).Order("created_at ASC").First(&batch).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		created, createErr := migrationService.CreateBatch(ctx, service.CreateMigrationBatchInput{
			Kind: "s7_mapping_manifest", SourceFingerprint: manifest.SourceFingerprint, ManifestHash: manifest.ManifestHash,
			RequestID: "s7-" + manifest.ManifestHash[:32], TotalCount: manifest.Totals.SourceCount,
		})
		if createErr != nil {
			return nil, 0, 0, 0, createErr
		}
		batch = *created
	} else if err != nil {
		return nil, 0, 0, 0, err
	}
	if batch.Status == model.MigrationBatchCompleted {
		migrated, skipped, failed, countErr := mappingCounts(batch.ID)
		return &batch, migrated, skipped, failed, countErr
	}
	if batch.Status == model.MigrationBatchDraft {
		running, transitionErr := migrationService.TransitionBatch(ctx, batch.ID, batch.RowVersion, model.MigrationBatchRunning, service.MigrationBatchTransition{ManifestHash: manifest.ManifestHash})
		if transitionErr != nil {
			return nil, 0, 0, 0, transitionErr
		}
		batch = *running
	}
	if batch.Status != model.MigrationBatchRunning {
		return nil, 0, 0, 0, fmt.Errorf("migration batch is not resumable from %s", batch.Status)
	}
	for _, entry := range manifest.Entries {
		if err := applyManifestEntry(ctx, migrationService, batch.ID, entry); err != nil {
			return nil, 0, 0, 0, err
		}
	}
	migrated, skipped, failed, err := mappingCounts(batch.ID)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	processed := migrated + skipped + failed
	completed, err := migrationService.TransitionBatch(ctx, batch.ID, batch.RowVersion, model.MigrationBatchCompleted, service.MigrationBatchTransition{
		ProcessedCount: processed, SucceededCount: migrated + skipped, FailedCount: failed, ManifestHash: manifest.ManifestHash,
	})
	return completed, migrated, skipped, failed, err
}

func applyManifestEntry(ctx context.Context, migrationService *service.ResourceMigrationService, batchID string, entry migration.ManifestEntry) error {
	var mapping model.MigrationSourceMapping
	err := serverdb.DB.Where("batch_id = ? AND source_type = ? AND source_id = ? AND source_revision = ?", batchID, entry.SourceType, entry.SourceID, entry.SourceRevision).First(&mapping).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		created, createErr := migrationService.UpsertSource(ctx, service.UpsertMigrationSourceInput{
			BatchID: batchID, SourceType: entry.SourceType, SourceID: entry.SourceID, SourceRevision: entry.SourceRevision,
			TargetType: entry.TargetType, TargetID: entry.TargetID, Classification: model.MigrationClassification(entry.Classification),
			EvidenceHash: entry.EvidenceHash, EvidenceSummary: strings.Join(entry.ReasonCodes, ","),
		})
		if createErr != nil {
			return createErr
		}
		mapping = *created
	} else if err != nil {
		return err
	}
	if mapping.Status == model.MigrationSourceMigrated || mapping.Status == model.MigrationSourceSkipped || mapping.Status == model.MigrationSourceFailed {
		return nil
	}
	if entry.Decision == "migrate" {
		if mapping.Classification == model.MigrationClassificationManual && mapping.Status == model.MigrationSourceCandidate {
			confirmed, confirmErr := migrationService.TransitionSource(ctx, mapping.ID, mapping.RowVersion, model.MigrationSourceConfirmed, entry.TargetType, entry.TargetID, "", "")
			if confirmErr != nil {
				return confirmErr
			}
			mapping = *confirmed
		}
		_, err = migrationService.TransitionSource(ctx, mapping.ID, mapping.RowVersion, model.MigrationSourceMigrated, entry.TargetType, entry.TargetID, "", "")
		return err
	}
	_, err = migrationService.TransitionSource(ctx, mapping.ID, mapping.RowVersion, model.MigrationSourceSkipped, "", "", strings.ToUpper(entry.Decision), "manifest decision")
	return err
}

func mappingCounts(batchID string) (migrated, skipped, failed int64, err error) {
	for status, target := range map[model.MigrationSourceStatus]*int64{model.MigrationSourceMigrated: &migrated, model.MigrationSourceSkipped: &skipped, model.MigrationSourceFailed: &failed} {
		if countErr := serverdb.DB.Model(&model.MigrationSourceMapping{}).Where("batch_id = ? AND status = ?", batchID, status).Count(target).Error; countErr != nil {
			return 0, 0, 0, countErr
		}
	}
	return migrated, skipped, failed, nil
}

func completedBatch(databasePath, manifestHash string) (model.MigrationBatch, bool, error) {
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databasePath)+"?mode=ro")
	if err != nil {
		return model.MigrationBatch{}, false, err
	}
	defer database.Close()
	var exists int
	if err := database.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='migration_batch'").Scan(&exists); err != nil || exists == 0 {
		return model.MigrationBatch{}, false, err
	}
	var batch model.MigrationBatch
	err = database.QueryRow("SELECT id, status FROM migration_batch WHERE manifest_hash = ? AND status = 'completed' ORDER BY created_at LIMIT 1", manifestHash).Scan(&batch.ID, &batch.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return model.MigrationBatch{}, false, nil
	}
	return batch, err == nil, err
}

func mappingCountsReadOnly(databasePath, batchID string) (migrated, skipped, failed int64, err error) {
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databasePath)+"?mode=ro")
	if err != nil {
		return 0, 0, 0, err
	}
	defer database.Close()
	for status, target := range map[string]*int64{"migrated": &migrated, "skipped": &skipped, "failed": &failed} {
		if countErr := database.QueryRow("SELECT COUNT(*) FROM migration_source_mapping WHERE batch_id = ? AND status = ?", batchID, status).Scan(target); countErr != nil {
			return 0, 0, 0, countErr
		}
	}
	return migrated, skipped, failed, nil
}

func integrityCheckReadOnly(databasePath string) (string, error) {
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databasePath)+"?mode=ro")
	if err != nil {
		return "", err
	}
	defer database.Close()
	var integrity string
	err = database.QueryRow("PRAGMA integrity_check").Scan(&integrity)
	return integrity, err
}

func snapshotTables(databasePath string, entries []migration.ManifestEntry, fixedColumns map[string][]string) ([]tableSnapshot, error) {
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	tableSet := map[string]struct{}{}
	for _, entry := range entries {
		if !safeTableName.MatchString(entry.SourceType) {
			return nil, fmt.Errorf("unsafe source table %q", entry.SourceType)
		}
		tableSet[entry.SourceType] = struct{}{}
	}
	tables := make([]string, 0, len(tableSet))
	for table := range tableSet {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	result := make([]tableSnapshot, 0, len(tables))
	for _, table := range tables {
		columns := fixedColumns[table]
		if len(columns) == 0 {
			rows, queryErr := database.Query(`PRAGMA table_info("` + table + `")`)
			if queryErr != nil {
				return nil, queryErr
			}
			for rows.Next() {
				var cid, notNull, primaryKey int
				var name, fieldType string
				var defaultValue any
				if scanErr := rows.Scan(&cid, &name, &fieldType, &notNull, &defaultValue, &primaryKey); scanErr != nil {
					rows.Close()
					return nil, scanErr
				}
				columns = append(columns, name)
			}
			rows.Close()
		}
		if len(columns) == 0 {
			continue
		}
		quotedColumns := make([]string, len(columns))
		for index, column := range columns {
			quotedColumns[index] = `"` + strings.ReplaceAll(column, `"`, `""`) + `"`
		}
		rows, queryErr := database.Query(`SELECT ` + strings.Join(quotedColumns, ",") + ` FROM "` + table + `"`)
		if queryErr != nil {
			return nil, queryErr
		}
		rowHashes := []string{}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for index := range values {
				pointers[index] = &values[index]
			}
			if scanErr := rows.Scan(pointers...); scanErr != nil {
				rows.Close()
				return nil, scanErr
			}
			hash := sha256.New()
			for _, value := range values {
				fmt.Fprintf(hash, "%T:%v\x00", value, value)
			}
			rowHashes = append(rowHashes, hex.EncodeToString(hash.Sum(nil)))
		}
		rows.Close()
		sort.Strings(rowHashes)
		hash := sha256.Sum256([]byte(strings.Join(rowHashes, "\n")))
		result = append(result, tableSnapshot{Table: table, Columns: columns, Count: int64(len(rowHashes)), Hash: hex.EncodeToString(hash[:])})
	}
	return result, nil
}

func snapshotColumns(snapshots []tableSnapshot) map[string][]string {
	result := make(map[string][]string, len(snapshots))
	for _, snapshot := range snapshots {
		result[snapshot.Table] = snapshot.Columns
	}
	return result
}

func compareSnapshots(before, after []tableSnapshot) (bool, bool) {
	beforeByTable := make(map[string]tableSnapshot, len(before))
	for _, snapshot := range before {
		beforeByTable[snapshot.Table] = snapshot
	}
	counts, content := len(before) == len(after), len(before) == len(after)
	for _, snapshot := range after {
		previous, exists := beforeByTable[snapshot.Table]
		counts = counts && exists && previous.Count == snapshot.Count
		content = content && exists && previous.Hash == snapshot.Hash
	}
	return counts, content
}

func loadManifest(path string) (migration.Manifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return migration.Manifest{}, err
	}
	var manifest migration.Manifest
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return migration.Manifest{}, err
	}
	return manifest, nil
}

func fileSHA256(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:]), nil
}
