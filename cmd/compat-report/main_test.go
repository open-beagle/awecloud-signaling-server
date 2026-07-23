package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/require"
)

func TestBuildReportReadsFixtureWithoutChangingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compat.db")
	database, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	statements := []string{
		`CREATE TABLE admin (id INTEGER PRIMARY KEY, role TEXT NOT NULL)`,
		`INSERT INTO admin (role) VALUES ('admin'), ('platform_viewer')`,
		`CREATE TABLE tenant_membership (id INTEGER PRIMARY KEY, tenant_id TEXT, user_id INTEGER, role TEXT, enabled INTEGER)`,
		`INSERT INTO tenant_membership (tenant_id, user_id, role, enabled) VALUES ('tenant-a', 42, 'tenant_admin', 1), ('tenant-a', 43, 'member', 1)`,
		`CREATE TABLE audit_log (id INTEGER PRIMARY KEY, action_type TEXT, target_id TEXT, created_at TEXT)`,
		`INSERT INTO audit_log (action_type, target_id, created_at) VALUES ('create', 'one', '2026-07-19T00:00:00Z')`,
		`CREATE TABLE operation_audit_log (id INTEGER PRIMARY KEY, operation_type TEXT, target TEXT, started_at TEXT, created_at TEXT)`,
		`INSERT INTO operation_audit_log (operation_type, target, started_at, created_at) VALUES ('ssh_session', 'host', '2026-07-20T00:00:00Z', '2026-07-20T00:00:01Z')`,
	}
	for _, statement := range statements {
		_, err = database.Exec(statement)
		require.NoError(t, err)
	}
	require.NoError(t, database.Close())

	report, err := buildReport(path)
	require.NoError(t, err)
	require.Len(t, report.AdminRoles, 2)
	require.Len(t, report.LegacyTenantAdminMemberships, 1)
	require.Equal(t, uint64(42), report.LegacyTenantAdminMemberships[0].UserID)
	require.Equal(t, int64(1), report.ManagementAudit.Count)
	require.Equal(t, "2026-07-20T00:00:00Z", report.OperationAudit.MinTime)
	require.Equal(t, "keep-separate-until-human-review", report.AuditDisposition)

	verify, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	require.NoError(t, err)
	defer verify.Close()
	var count int
	require.NoError(t, verify.QueryRow("SELECT COUNT(*) FROM tenant_membership").Scan(&count))
	require.Equal(t, 2, count)
}
