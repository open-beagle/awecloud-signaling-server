package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/require"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/migration"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/testfixture/resourcebusiness"
)

func TestBuildReportClassifiesFullAnonymousFixture(t *testing.T) {
	path := createFullFixture(t)
	beforeHash, err := fileSHA256(path)
	require.NoError(t, err)
	beforeSchemaVersion := sqliteInteger(t, path, "PRAGMA schema_version")
	beforeRows := sqliteInteger(t, path, "SELECT COUNT(*) FROM access_grant")

	report, err := buildReport(path)
	require.NoError(t, err)
	require.Equal(t, compatibilitySchemaVersion, report.SchemaVersion)
	require.Equal(t, beforeHash, report.SourceFingerprint)
	require.True(t, report.Totals.Conserved)
	require.Equal(t, report.Totals.SourceCount, report.Totals.ClassifiedCount)
	require.NotEmpty(t, report.ContentHash)
	for _, section := range report.Sections {
		require.Truef(t, section.Conserved, "section %s must conserve rows", section.SourceType)
		require.Equal(t, section.SourceCount, section.ClassifiedCount)
	}

	requireCandidate(t, report, "admin", "admin:1", classificationManual, "ADMIN_USER_EXACT_NAME_ONLY")
	requireCandidate(t, report, "admin", "admin:2", classificationManual, "ADMIN_USER_NAME_CONFLICT")
	requireCandidate(t, report, "user", "user:10", classificationAutomatic, "CLIENT_USER_STABLE_IDENTITY")
	requireCandidate(t, report, "user", "user:20", classificationCompatibility, "PROVIDER_OWNERSHIP_UNPROVEN")
	requireCandidate(t, report, "node", "node:n1", classificationManual, "PROVIDER_OWNERSHIP_REQUIRES_CONFIRMATION")
	requireCandidate(t, report, "endpoint", "endpoint:e1", classificationManual, "ENDPOINT_PROVIDER_OWNERSHIP_REQUIRES_CONFIRMATION")
	requireCandidate(t, report, "node", "node:<empty>", classificationInvalid, "SOURCE_PRIMARY_KEY_EMPTY")
	requireCandidate(t, report, "resource", "resource:r1", classificationManual, "RESOURCE_SOURCE_REQUIRES_CONFIRMATION")
	requireCandidate(t, report, "resource_target", "resource_target:rt1", classificationManual, "RESOURCE_TARGET_SOURCE_REQUIRES_CONFIRMATION")
	requireCandidate(t, report, "resource_target", "resource_target:rt2", classificationManual, "RESOURCE_TARGET_SOURCE_REQUIRES_CONFIRMATION")
	requireCandidate(t, report, "resource_target", "resource_target:rt3", classificationCompatibility, "RESOURCE_TARGET_RUNTIME_MAPPING_UNPROVEN")
	requireCandidate(t, report, "workspace_binding", "workspace_binding:w1", classificationManual, "WORKSPACE_BINDING_NOT_ALLOCATION")
	requireCandidate(t, report, "discovery_candidate", "discovery_candidate:d1", classificationCompatibility, "DISCOVERY_OBSERVATION_REMAINS_COMPATIBILITY")
	requireCandidate(t, report, "access_grant", "access_grant:a1", classificationManual, "ACCESS_GRANT_REQUIRES_TARGET_MODEL_CONFIRMATION")
	requireCandidate(t, report, "access_grant", "access_grant:a2", classificationInvalid, "ACCESS_GRANT_RESOURCE_OR_TENANT_INVALID")
	requireCandidate(t, report, "access_grant", "access_grant:a3", classificationInvalid, "ACCESS_GRANT_ACTIONS_INVALID")

	aclCount := 0
	for _, section := range report.Sections {
		if strings.HasPrefix(section.SourceType, "acl_") {
			aclCount++
			require.True(t, section.TablePresent)
			require.Equal(t, int64(1), section.SourceCount)
		}
	}
	require.Equal(t, 12, aclCount)
	requireCandidate(t, report, "acl_ssh_user_permission", "acl_ssh_user_permission:1", classificationInvalid, "ACL_CONSTRAINT_JSON_INVALID")

	require.Equal(t, int64(1), report.ManagementAudit.Count)
	require.Equal(t, int64(1), report.OperationAudit.Count)
	require.Equal(t, int64(1), report.ManagementAudit.CorrelatedCount)
	require.Equal(t, int64(1), report.OperationAudit.CorrelatedCount)

	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	markdown := renderMarkdown(report)
	for _, secret := range []string{"private-admin", "sensitive-audit-detail", "203.0.113.9", "secret-user-agent", "raw-target-value", "private-label", "invalid-json-secret"} {
		require.NotContains(t, string(encoded), secret)
		require.NotContains(t, markdown, secret)
	}
	require.Contains(t, markdown, `admin\|review`)
	require.Contains(t, string(encoded), `"target_candidates":[]`)
	require.Contains(t, string(encoded), `"evidence_refs":[]`)
	require.Contains(t, markdown, "| **总计**")
	require.Contains(t, markdown, report.ContentHash)

	afterHash, err := fileSHA256(path)
	require.NoError(t, err)
	require.Equal(t, beforeHash, afterHash)
	require.Equal(t, beforeSchemaVersion, sqliteInteger(t, path, "PRAGMA schema_version"))
	require.Equal(t, beforeRows, sqliteInteger(t, path, "SELECT COUNT(*) FROM access_grant"))
}

func TestAnonymousCompatibilityReportBuildsDeterministicMappingDraft(t *testing.T) {
	report, err := buildReport(createFullFixture(t))
	require.NoError(t, err)
	content, err := json.Marshal(report)
	require.NoError(t, err)
	var input migration.CompatReport
	require.NoError(t, json.Unmarshal(content, &input))
	draft, err := migration.BuildDraft(input)
	require.NoError(t, err)
	require.Equal(t, report.SourceFingerprint, draft.SourceFingerprint)
	require.Equal(t, report.ContentHash, draft.SourceContentHash)
	require.Equal(t, report.Totals.SourceCount, draft.Totals.SourceCount)
	require.Greater(t, draft.Totals.PendingCount, int64(0))
	require.Len(t, draft.ManifestHash, 64)

	second, err := migration.BuildDraft(input)
	require.NoError(t, err)
	require.Equal(t, draft.ManifestHash, second.ManifestHash)
}

func TestContentHashIsIndependentOfPathAndGenerationTime(t *testing.T) {
	source := createFullFixture(t)
	copyPath := filepath.Join(t.TempDir(), "renamed.db")
	content, err := os.ReadFile(source)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(copyPath, content, 0o600))

	first, err := buildReport(source)
	require.NoError(t, err)
	time.Sleep(time.Millisecond)
	second, err := buildReport(copyPath)
	require.NoError(t, err)
	require.NotEqual(t, first.Database, second.Database)
	require.NotEqual(t, first.GeneratedAt, second.GeneratedAt)
	require.Equal(t, first.SourceFingerprint, second.SourceFingerprint)
	require.Equal(t, first.ContentHash, second.ContentHash)

	second.GeneratedAt = second.GeneratedAt.Add(24 * time.Hour)
	second.Database = "another-local-name.db"
	hash, err := compatibilityContentHash(second)
	require.NoError(t, err)
	require.Equal(t, first.ContentHash, hash)
}

func TestSchemaWarningsAndFailClosedColumns(t *testing.T) {
	t.Run("missing tables and optional columns are stable warnings", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "old.db")
		database := openFixture(t, path)
		execStatements(t, database,
			`CREATE TABLE admin (id TEXT)`,
			`INSERT INTO admin (id) VALUES ('1')`,
			`CREATE TABLE tenant_membership (id INTEGER PRIMARY KEY)`,
		)
		require.NoError(t, database.Close())

		report, err := buildReport(path)
		require.NoError(t, err)
		require.Empty(t, report.AdminRoles)
		require.Empty(t, report.LegacyTenantAdminMemberships)
		require.True(t, report.Totals.Conserved)
		require.Contains(t, report.SchemaWarnings, schemaWarning{Code: "OPTIONAL_COLUMN_MISSING", Table: "admin", Column: "role"})
		require.Contains(t, report.SchemaWarnings, schemaWarning{Code: "TABLE_MISSING", Table: "resource"})
	})

	t.Run("missing required primary key fails closed", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing-id.db")
		database := openFixture(t, path)
		execStatements(t, database, `CREATE TABLE admin (username TEXT, role TEXT)`)
		require.NoError(t, database.Close())
		_, err := buildReport(path)
		require.ErrorContains(t, err, "table admin is missing required column id")
	})

	t.Run("missing audit time fails closed", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing-time.db")
		database := openFixture(t, path)
		execStatements(t, database, `CREATE TABLE audit_log (id INTEGER PRIMARY KEY)`)
		require.NoError(t, database.Close())
		_, err := buildReport(path)
		require.ErrorContains(t, err, "table audit_log is missing required column created_at")
	})
}

func TestValidateOutputPathRejectsInputAndSymlink(t *testing.T) {
	path := createFullFixture(t)
	require.Error(t, validateOutputPath(path, path))
	symlink := filepath.Join(t.TempDir(), "database-link")
	require.NoError(t, os.Symlink(path, symlink))
	require.Error(t, validateOutputPath(path, symlink))
	require.NoError(t, validateOutputPath(path, filepath.Join(t.TempDir(), "report.json")))
}

func TestM0DScenariosProduceConservedReadOnlyReports(t *testing.T) {
	for _, name := range resourcebusiness.Names() {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "scenario.db")
			require.NoError(t, resourcebusiness.CreateCompatibilityDatabase(path, resourcebusiness.MustLoad(name)))
			before, err := fileSHA256(path)
			require.NoError(t, err)

			report, err := buildReport(path)
			require.NoError(t, err)
			require.True(t, report.Totals.Conserved)
			for _, section := range report.Sections {
				require.Truef(t, section.Conserved, "section %s must conserve rows", section.SourceType)
			}

			after, err := fileSHA256(path)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func createFullFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "compat.db")
	database := openFixture(t, path)
	execStatements(t, database,
		`CREATE TABLE admin (id TEXT, username TEXT, role TEXT, enabled INTEGER)`,
		`INSERT INTO admin VALUES ('1', 'exact-user', 'admin|review', 1), ('2', 'duplicate-user', 'platform_viewer', 1), ('3', 'private-admin', 'viewer', 1)`,
		`CREATE TABLE user (id TEXT, name TEXT, role TEXT, enabled INTEGER, source TEXT)`,
		`INSERT INTO user VALUES ('10', 'exact-user', 'client', 1, 'local'), ('11', 'duplicate-user', 'client', 1, 'local'), ('12', 'duplicate-user', 'client', 1, 'local'), ('20', 'agent-source', 'agent', 1, 'agent')`,
		`CREATE TABLE tenant (id TEXT, key TEXT, status TEXT)`,
		`INSERT INTO tenant VALUES ('t1', 'tenant-one', 'active'), ('t2', 'tenant-two', 'active')`,
		`CREATE TABLE admin_tenant_membership (id TEXT, admin_id TEXT, tenant_id TEXT, role TEXT, enabled INTEGER, expires_at TEXT, permission_revision INTEGER)`,
		`INSERT INTO admin_tenant_membership VALUES ('am1', '1', 't1', 'tenant_admin', 1, '', 1)`,
		`CREATE TABLE tenant_membership (id INTEGER PRIMARY KEY, tenant_id TEXT, user_id TEXT, role TEXT, enabled INTEGER, expires_at TEXT)`,
		`INSERT INTO tenant_membership VALUES (1, 't1', '10', 'member', 1, ''), (2, 't1', '11', 'tenant_admin', 1, '')`,
		`CREATE TABLE "group" (id TEXT, tenant_id TEXT, name TEXT)`,
		`INSERT INTO "group" VALUES ('g1', 't1', 'operators'), ('g2', 't2', 'other')`,
		`CREATE TABLE group_member (id TEXT, group_id TEXT, user_id TEXT)`,
		`INSERT INTO group_member VALUES ('gm1', 'g1', '10')`,
		`CREATE TABLE node (id TEXT, user_id TEXT, name TEXT, type TEXT, headscale_node_id TEXT)`,
		`INSERT INTO node VALUES ('n1', '20', 'agent-node', 'agent', '100'), ('n2', '10', 'desktop-node', 'desktop', '101'), ('', '20', 'empty-id-node', 'agent', '102')`,
		`CREATE TABLE endpoint (id TEXT, user_id TEXT, name TEXT, status TEXT, revoked INTEGER)`,
		`INSERT INTO endpoint VALUES ('e1', '20', 'endpoint', 'online', 0)`,
		`CREATE TABLE resource (id TEXT, tenant_id TEXT, type TEXT, state TEXT, provider_id TEXT, external_workspace_id TEXT, owner_user_id TEXT, owner_group_id TEXT, agent_node_id TEXT, target_revision INTEGER)`,
		`INSERT INTO resource VALUES ('r1', 't1', 'container_ssh', 'available', 'provider-opaque', 'workspace-opaque', '10', '', 'n1', 1), ('r2', 't2', 'host_ssh', 'available', '', '', '', '', 'n1', 1), ('r3', 't1', 'tcp_service', 'available', '', '', '', '', 'n1', 1)`,
		`CREATE TABLE resource_target (id TEXT, resource_id TEXT, revision INTEGER, agent_node_id TEXT, cluster_id TEXT, namespace TEXT, pod_uid TEXT, container_name TEXT)`,
		`INSERT INTO resource_target VALUES ('rt1', 'r1', 1, 'n1', 'cluster', 'namespace', 'pod-uid', 'container'), ('rt2', 'r2', 1, 'n1', '', '', '', ''), ('rt3', 'r3', 1, 'n1', '', '', '', '')`,
		`CREATE TABLE workspace_binding (id TEXT, provider_id TEXT, external_tenant_id TEXT, external_workspace_id TEXT, tenant_id TEXT, owner_user_id TEXT, resource_id TEXT, generation INTEGER, status TEXT)`,
		`INSERT INTO workspace_binding VALUES ('w1', 'provider-opaque', 'external-tenant', 'workspace-opaque', 't1', '10', 'r1', 1, 'active')`,
		`CREATE TABLE discovery_candidate (id TEXT, agent_node_id TEXT, provider_hint TEXT, cluster_id TEXT, namespace TEXT, pod_uid TEXT, container_name TEXT, generation_hint INTEGER, status TEXT, resource_id TEXT)`,
		`INSERT INTO discovery_candidate VALUES ('d1', 'n1', 'untrusted-provider-hint', 'cluster', 'namespace', 'pod-uid', 'container', 1, 'observed', '')`,
		`CREATE TABLE access_grant (id TEXT, tenant_id TEXT, resource_id TEXT, subject_type TEXT, subject_user_id TEXT, subject_group_id TEXT, actions TEXT, status TEXT, revision INTEGER)`,
		`INSERT INTO access_grant VALUES ('a1', 't1', 'r1', 'user', '10', '', '["shell"]', 'active', 1), ('a2', 't1', 'r2', 'user', '10', '', '["shell"]', 'active', 1), ('a3', 't1', 'r1', 'user', '10', '', 'invalid-json-secret', 'active', 1)`,
		`CREATE TABLE proxy_service (id TEXT, user_id TEXT)`,
		`INSERT INTO proxy_service VALUES ('s1', '20')`,
	)
	createACLFixture(t, database)
	execStatements(t, database,
		`CREATE TABLE audit_log (id TEXT, user_id TEXT, actor_admin_id TEXT, tenant_id TEXT, request_id TEXT, target_id TEXT, action_type TEXT, target TEXT, detail TEXT, source_ip TEXT, user_agent TEXT, created_at TEXT)`,
		`INSERT INTO audit_log VALUES ('au1', '10', '1', 't1', 'request-1', 'r1', 'read', 'raw-target-value', 'sensitive-audit-detail', '203.0.113.9', 'secret-user-agent', '2026-07-19T00:00:00Z')`,
		`CREATE TABLE operation_audit_log (id TEXT, agent_user_id TEXT, client_user_id TEXT, endpoint_id TEXT, operation_type TEXT, target TEXT, detail TEXT, started_at TEXT, ended_at TEXT, created_at TEXT)`,
		`INSERT INTO operation_audit_log VALUES ('oa1', '20', '10', 'e1', 'ssh', 'raw-target-value', 'private-label', '2026-07-20T00:00:00Z', '2026-07-20T00:01:00Z', '2026-07-20T00:00:00Z')`,
	)
	require.NoError(t, database.Close())
	return path
}

func createACLFixture(t *testing.T, database *sql.DB) {
	t.Helper()
	definitions := []struct {
		name    string
		columns string
		values  string
	}{
		{"acl_group_user_permission", "target_group_id TEXT, user_id TEXT", "'g1', '10'"},
		{"acl_group_group_permission", "target_group_id TEXT, group_id TEXT", "'g1', 'g2'"},
		{"acl_k8s_user_permission", "target_user_id TEXT, user_id TEXT, k8s_groups TEXT, namespaces TEXT, enabled INTEGER", "'20', '10', '[\"dev\"]', '[\"ns\"]', 1"},
		{"acl_k8s_group_permission", "target_user_id TEXT, group_id TEXT, k8s_groups TEXT, namespaces TEXT, enabled INTEGER", "'20', 'g1', '[\"dev\"]', '[\"ns\"]', 1"},
		{"acl_k8s_service_user_permission", "target_user_id TEXT, user_id TEXT, namespaces TEXT, service_names TEXT, enabled INTEGER", "'20', '10', '[\"ns\"]', '[\"svc\"]', 1"},
		{"acl_k8s_service_group_permission", "target_user_id TEXT, group_id TEXT, namespaces TEXT, service_names TEXT, enabled INTEGER", "'20', 'g1', '[\"ns\"]', '[\"svc\"]', 1"},
		{"acl_service_user_permission", "service_id TEXT, user_id TEXT", "'s1', '10'"},
		{"acl_service_group_permission", "service_id TEXT, group_id TEXT", "'s1', 'g1'"},
		{"acl_ssh_user_permission", "target_user_id TEXT, user_id TEXT, ssh_users TEXT, enabled INTEGER", "'20', '10', 'not-json', 1"},
		{"acl_ssh_group_permission", "target_user_id TEXT, group_id TEXT, ssh_users TEXT, enabled INTEGER", "'20', 'g1', '[\"root\"]', 1"},
		{"acl_user_user_permission", "target_user_id TEXT, granted_user_id TEXT", "'20', '10'"},
		{"acl_user_group_permission", "target_user_id TEXT, group_id TEXT", "'20', 'g1'"},
	}
	for _, definition := range definitions {
		execStatements(t, database,
			"CREATE TABLE "+quoteIdentifier(definition.name)+" (id INTEGER PRIMARY KEY, "+definition.columns+")",
			"INSERT INTO "+quoteIdentifier(definition.name)+" VALUES (1, "+definition.values+")",
		)
	}
}

func requireCandidate(t *testing.T, report compatibilityReport, sourceType, sourceID string, category classification, reason string) compatibilityCandidate {
	t.Helper()
	for _, section := range report.Sections {
		if section.SourceType != sourceType {
			continue
		}
		for _, candidate := range section.Candidates {
			if candidate.SourceID == sourceID {
				require.Equal(t, category, candidate.Classification)
				require.Contains(t, candidate.ReasonCodes, reason)
				return candidate
			}
		}
	}
	t.Fatalf("candidate %s not found", sourceID)
	return compatibilityCandidate{}
}

func openFixture(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	return database
}

func execStatements(t *testing.T, database *sql.DB, statements ...string) {
	t.Helper()
	for _, statement := range statements {
		_, err := database.Exec(statement)
		require.NoError(t, err, statement)
	}
}

func sqliteInteger(t *testing.T, path, query string) int64 {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	require.NoError(t, err)
	defer database.Close()
	var value int64
	require.NoError(t, database.QueryRow(query).Scan(&value))
	return value
}
