// compat-report inspects legacy management roles and audit tables without
// modifying the source SQLite database.
package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type roleCount struct {
	Role  string `json:"role"`
	Count int64  `json:"count"`
}

type legacyTenantAdminMembership struct {
	ID       int64  `json:"id"`
	TenantID string `json:"tenant_id"`
	UserID   uint64 `json:"user_id"`
	Enabled  bool   `json:"enabled"`
}

type auditTableReport struct {
	Table     string   `json:"table"`
	Exists    bool     `json:"exists"`
	Count     int64    `json:"count"`
	TimeField string   `json:"time_field,omitempty"`
	MinTime   string   `json:"min_time,omitempty"`
	MaxTime   string   `json:"max_time,omitempty"`
	Columns   []string `json:"columns,omitempty"`
}

type compatibilityReport struct {
	GeneratedAt                  time.Time                     `json:"generated_at"`
	Database                     string                        `json:"database"`
	AdminRoles                   []roleCount                   `json:"admin_roles"`
	TenantMembershipRoles        []roleCount                   `json:"tenant_membership_roles"`
	LegacyTenantAdminMemberships []legacyTenantAdminMembership `json:"legacy_tenant_admin_memberships"`
	ManagementAudit              auditTableReport              `json:"management_audit"`
	OperationAudit               auditTableReport              `json:"operation_audit"`
	AuditDisposition             string                        `json:"audit_disposition"`
}

func main() {
	databasePath := flag.String("database", "", "SQLite database file to inspect (required; opened read-only)")
	format := flag.String("format", "markdown", "output format: markdown or json")
	output := flag.String("output", "", "optional report file; stdout when omitted")
	flag.Parse()

	if *databasePath == "" {
		fatal(errors.New("-database is required"))
	}
	report, err := buildReport(*databasePath)
	if err != nil {
		fatal(err)
	}
	var content []byte
	switch *format {
	case "json":
		content, err = json.MarshalIndent(report, "", "  ")
	case "markdown":
		content = []byte(renderMarkdown(report))
	default:
		err = fmt.Errorf("unsupported format %q", *format)
	}
	if err != nil {
		fatal(err)
	}
	content = append(content, '\n')
	if *output == "" {
		_, err = os.Stdout.Write(content)
	} else {
		err = os.WriteFile(*output, content, 0o600)
	}
	if err != nil {
		fatal(err)
	}
}

func buildReport(databasePath string) (compatibilityReport, error) {
	absPath, err := filepath.Abs(databasePath)
	if err != nil {
		return compatibilityReport{}, fmt.Errorf("resolve database path: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return compatibilityReport{}, fmt.Errorf("stat database: %w", err)
	}
	if info.IsDir() {
		return compatibilityReport{}, errors.New("database path is a directory")
	}

	dsn := "file:" + filepath.ToSlash(absPath) + "?mode=ro"
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return compatibilityReport{}, fmt.Errorf("open database read-only: %w", err)
	}
	defer database.Close()
	if _, err = database.Exec("PRAGMA query_only = ON"); err != nil {
		return compatibilityReport{}, fmt.Errorf("enable query-only mode: %w", err)
	}
	if err = database.Ping(); err != nil {
		return compatibilityReport{}, fmt.Errorf("ping database: %w", err)
	}

	report := compatibilityReport{
		GeneratedAt:      time.Now().UTC(),
		Database:         filepath.Base(absPath),
		AuditDisposition: "keep-separate-until-human-review",
	}
	if report.AdminRoles, err = readRoleCounts(database, "admin"); err != nil {
		return compatibilityReport{}, err
	}
	if report.TenantMembershipRoles, err = readRoleCounts(database, "tenant_membership"); err != nil {
		return compatibilityReport{}, err
	}
	if report.LegacyTenantAdminMemberships, err = readLegacyTenantAdmins(database); err != nil {
		return compatibilityReport{}, err
	}
	if report.ManagementAudit, err = readAuditTable(database, "audit_log", "created_at"); err != nil {
		return compatibilityReport{}, err
	}
	if report.OperationAudit, err = readAuditTable(database, "operation_audit_log", "started_at"); err != nil {
		return compatibilityReport{}, err
	}
	return report, nil
}

func tableExists(database *sql.DB, table string) (bool, error) {
	var count int
	err := database.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count)
	return count > 0, err
}

func readRoleCounts(database *sql.DB, table string) ([]roleCount, error) {
	exists, err := tableExists(database, table)
	if err != nil || !exists {
		return nil, err
	}
	rows, err := database.Query(fmt.Sprintf(`SELECT role, COUNT(*) FROM %q GROUP BY role ORDER BY role`, table))
	if err != nil {
		return nil, fmt.Errorf("read %s roles: %w", table, err)
	}
	defer rows.Close()
	var result []roleCount
	for rows.Next() {
		var item roleCount
		if err = rows.Scan(&item.Role, &item.Count); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func readLegacyTenantAdmins(database *sql.DB) ([]legacyTenantAdminMembership, error) {
	exists, err := tableExists(database, "tenant_membership")
	if err != nil || !exists {
		return nil, err
	}
	rows, err := database.Query(`SELECT id, tenant_id, user_id, enabled FROM tenant_membership WHERE role = 'tenant_admin' ORDER BY tenant_id, user_id`)
	if err != nil {
		return nil, fmt.Errorf("read legacy tenant_admin memberships: %w", err)
	}
	defer rows.Close()
	var result []legacyTenantAdminMembership
	for rows.Next() {
		var item legacyTenantAdminMembership
		if err = rows.Scan(&item.ID, &item.TenantID, &item.UserID, &item.Enabled); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func readAuditTable(database *sql.DB, table, timeField string) (auditTableReport, error) {
	report := auditTableReport{Table: table, TimeField: timeField}
	var err error
	if report.Exists, err = tableExists(database, table); err != nil || !report.Exists {
		return report, err
	}
	rows, err := database.Query(fmt.Sprintf(`PRAGMA table_info(%q)`, table))
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err = rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return report, err
		}
		report.Columns = append(report.Columns, name)
	}
	if err = rows.Close(); err != nil {
		return report, err
	}
	sort.Strings(report.Columns)
	query := fmt.Sprintf(`SELECT COUNT(*), COALESCE(CAST(MIN(%q) AS TEXT), ''), COALESCE(CAST(MAX(%q) AS TEXT), '') FROM %q`, timeField, timeField, table)
	if err = database.QueryRow(query).Scan(&report.Count, &report.MinTime, &report.MaxTime); err != nil {
		return report, fmt.Errorf("summarize %s: %w", table, err)
	}
	return report, nil
}

func renderMarkdown(report compatibilityReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 兼容数据只读报告\n\n- 生成时间：%s\n- 数据库：`%s`\n- 审计处置：`%s`\n", report.GeneratedAt.Format(time.RFC3339), report.Database, report.AuditDisposition)
	b.WriteString("\n## 管理角色分布\n\n| 表 | 角色 | 数量 |\n| --- | --- | ---: |\n")
	for _, item := range report.AdminRoles {
		fmt.Fprintf(&b, "| admin | %s | %d |\n", item.Role, item.Count)
	}
	for _, item := range report.TenantMembershipRoles {
		fmt.Fprintf(&b, "| tenant_membership | %s | %d |\n", item.Role, item.Count)
	}
	fmt.Fprintf(&b, "\n旧 `TenantMembership.role = tenant_admin` 明细：%d 条（仅 ID，不推断迁移目标）。\n", len(report.LegacyTenantAdminMemberships))
	if len(report.LegacyTenantAdminMemberships) > 0 {
		b.WriteString("\n| ID | Tenant ID | User ID | Enabled |\n| ---: | --- | ---: | --- |\n")
		for _, item := range report.LegacyTenantAdminMemberships {
			fmt.Fprintf(&b, "| %d | %s | %d | %t |\n", item.ID, item.TenantID, item.UserID, item.Enabled)
		}
	}
	b.WriteString("\n## 审计来源差异\n\n| 来源 | 存在 | 数量 | 时间字段 | 最早 | 最晚 | 字段 |\n| --- | --- | ---: | --- | --- | --- | --- |\n")
	for _, item := range []auditTableReport{report.ManagementAudit, report.OperationAudit} {
		fmt.Fprintf(&b, "| %s | %t | %d | %s | %s | %s | %s |\n", item.Table, item.Exists, item.Count, item.TimeField, item.MinTime, item.MaxTime, strings.Join(item.Columns, ", "))
	}
	b.WriteString("\n> 两张审计表记录的事件语义不同。报告不会自动合并、迁移或建议删除；入口重定向需人工确认统一查询契约后再执行。\n")
	return b.String()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "compat-report:", err)
	os.Exit(1)
}
