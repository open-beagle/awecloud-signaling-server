package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	serverdb "github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var safeIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type migrationReport struct {
	SchemaVersion        string           `json:"schema_version"`
	SourceFingerprint    string           `json:"source_fingerprint"`
	TargetFingerprint    string           `json:"target_baseline_fingerprint"`
	MigratedFingerprint  string           `json:"migrated_fingerprint"`
	BeagleTenantID       string           `json:"beagle_tenant_id"`
	Integrity            string           `json:"integrity"`
	ScopedTables         []string         `json:"scoped_tables"`
	NonBeagleScopedRows  int64            `json:"non_beagle_scoped_rows"`
	ClientUsers          int64            `json:"client_users"`
	TenantMemberships    int64            `json:"tenant_memberships"`
	DistinctMemberUsers  int64            `json:"distinct_member_users"`
	Admins               int64            `json:"admins"`
	AdminMemberships     int64            `json:"admin_memberships"`
	LegacyAdminLinks     int64            `json:"legacy_admin_links"`
	Nodes                int64            `json:"nodes"`
	AgentNodes           int64            `json:"agent_nodes"`
	Endpoints            int64            `json:"endpoints"`
	LegacyResourceClaims int64            `json:"legacy_resource_claims"`
	Resources            int64            `json:"resources"`
	AccessGrants         int64            `json:"access_grants"`
	Groups               int64            `json:"groups"`
	GroupMembers         int64            `json:"group_members"`
	LegacyACLPermissions int64            `json:"legacy_acl_permissions"`
	LegacyACLHashBefore  string           `json:"legacy_acl_hash_before"`
	LegacyACLHashAfter   string           `json:"legacy_acl_hash_after"`
	Before               map[string]int64 `json:"before"`
	After                map[string]int64 `json:"after"`
}

func main() {
	databaseCopy := flag.String("database-copy", "", "writable copy of the legacy SQLite database")
	targetBaseline := flag.String("target-baseline", "", "read-only current target SQLite backup used to preserve the administrator credential")
	adminUsername := flag.String("admin-username", "admin", "administrator whose target credential is preserved")
	output := flag.String("output", "", "migration report JSON")
	apply := flag.Bool("apply", false, "required acknowledgement that the database copy may be modified")
	flag.Parse()
	if err := run(*databaseCopy, *targetBaseline, *adminUsername, *output, *apply); err != nil {
		fmt.Fprintln(os.Stderr, "legacy-beagle-migrate:", err)
		os.Exit(1)
	}
}

func run(databasePath, targetPath, adminUsername, outputPath string, apply bool) error {
	if !apply {
		return errors.New("-apply is required")
	}
	if databasePath == "" || targetPath == "" || outputPath == "" || strings.TrimSpace(adminUsername) == "" {
		return errors.New("-database-copy, -target-baseline, -admin-username and -output are required")
	}
	databasePath, targetPath, outputPath, err := absoluteDistinct(databasePath, targetPath, outputPath)
	if err != nil {
		return err
	}
	sourceFingerprint, err := fileSHA256(databasePath)
	if err != nil {
		return err
	}
	targetFingerprint, err := fileSHA256(targetPath)
	if err != nil {
		return err
	}
	if integrity, err := integrityCheck(databasePath); err != nil || integrity != "ok" {
		return fmt.Errorf("legacy database integrity check failed: %s: %w", integrity, err)
	}
	before, err := legacyCounts(databasePath)
	if err != nil {
		return err
	}
	aclHashBefore, err := tablesContentHash(databasePath, legacyACLTables())
	if err != nil {
		return err
	}
	clientIDs, err := legacyClientIDs(databasePath)
	if err != nil {
		return err
	}
	credential, err := adminCredential(targetPath, adminUsername)
	if err != nil {
		return err
	}

	if err := serverdb.InitDB(config.DatabaseSection{Type: "sqlite", Path: databasePath}); err != nil {
		return err
	}
	defer func() {
		if database, closeErr := serverdb.DB.DB(); closeErr == nil {
			_ = database.Close()
		}
	}()

	var tenant model.Tenant
	if err := serverdb.DB.Where("key = ?", "beagle").First(&tenant).Error; err != nil {
		return fmt.Errorf("load Beagle tenant: %w", err)
	}
	scopedTables, err := normalizeToBeagle(serverdb.DB, tenant, clientIDs, adminUsername, credential)
	if err != nil {
		return err
	}
	var admins []model.Admin
	if err := serverdb.DB.Order("id").Find(&admins).Error; err != nil {
		return err
	}
	for _, admin := range admins {
		if _, err := serverdb.SyncLegacyAdminIdentity(serverdb.DB, admin.ID, "legacy signal.wodcloud.com migration to Beagle tenant"); err != nil {
			return fmt.Errorf("sync administrator %d: %w", admin.ID, err)
		}
	}

	report, err := verifyMigration(databasePath, sourceFingerprint, targetFingerprint, tenant.ID, scopedTables, before, aclHashBefore)
	if err != nil {
		return err
	}
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, append(content, '\n'), 0o600)
}

func normalizeToBeagle(database *gorm.DB, tenant model.Tenant, clientIDs []uint64, adminUsername, credential string) ([]string, error) {
	scopedTables, err := tenantScopedTables(database)
	if err != nil {
		return nil, err
	}
	err = database.Transaction(func(tx *gorm.DB) error {
		for _, table := range scopedTables {
			if table == "tenant_membership" || table == "admin_tenant_membership" {
				continue
			}
			if err := tx.Exec(`UPDATE "`+table+`" SET tenant_id = ? WHERE tenant_id IS NULL OR tenant_id <> ?`, tenant.ID, tenant.ID).Error; err != nil {
				return fmt.Errorf("scope %s to Beagle: %w", table, err)
			}
		}
		now := time.Now().UTC()
		if len(clientIDs) == 0 {
			return errors.New("legacy database has no business users")
		}
		if err := tx.Where("user_id NOT IN ?", clientIDs).Delete(&model.TenantMembership{}).Error; err != nil {
			return err
		}
		for _, userID := range clientIDs {
			var memberships []model.TenantMembership
			if err := tx.Where("user_id = ?", userID).Order("id").Find(&memberships).Error; err != nil {
				return err
			}
			if len(memberships) == 0 {
				membership := model.TenantMembership{TenantID: tenant.ID, UserID: userID, Role: "member", Enabled: true, CreatedAt: now, UpdatedAt: now}
				if err := tx.Create(&membership).Error; err != nil {
					return fmt.Errorf("create Beagle membership for user %d: %w", userID, err)
				}
				continue
			}
			keep := memberships[0]
			for _, membership := range memberships {
				if membership.TenantID == tenant.ID {
					keep = membership
					break
				}
			}
			if keep.TenantID != tenant.ID || keep.Role != "member" || !keep.Enabled || keep.ExpiresAt != nil {
				if err := tx.Model(&model.TenantMembership{}).Where("id = ?", keep.ID).Updates(map[string]any{"tenant_id": tenant.ID, "role": "member", "enabled": true, "expires_at": nil, "updated_at": now}).Error; err != nil {
					return err
				}
			}
			for _, membership := range memberships {
				if membership.ID != keep.ID {
					if err := tx.Delete(&model.TenantMembership{}, membership.ID).Error; err != nil {
						return err
					}
				}
			}
		}
		var admins []model.Admin
		if err := tx.Order("id").Find(&admins).Error; err != nil {
			return err
		}
		for _, admin := range admins {
			role := string(model.TenantManagementRoleViewer)
			if admin.Role == "admin" || admin.Role == "tenant_admin" {
				role = string(model.TenantManagementRoleAdmin)
			}
			var memberships []model.AdminTenantMembership
			if err := tx.Where("admin_id = ?", admin.ID).Order("id").Find(&memberships).Error; err != nil {
				return err
			}
			if len(memberships) == 0 {
				membership := model.AdminTenantMembership{AdminID: admin.ID, TenantID: tenant.ID, Role: role, Enabled: admin.Enabled, PermissionRevision: 1}
				if err := tx.Create(&membership).Error; err != nil {
					return fmt.Errorf("create Beagle administrator membership for %d: %w", admin.ID, err)
				}
				continue
			}
			keep := memberships[0]
			for _, membership := range memberships {
				if membership.TenantID == tenant.ID {
					keep = membership
					break
				}
			}
			if keep.TenantID != tenant.ID || keep.Role != role || keep.Enabled != admin.Enabled {
				if err := tx.Model(&model.AdminTenantMembership{}).Where("id = ?", keep.ID).Updates(map[string]any{"tenant_id": tenant.ID, "role": role, "enabled": admin.Enabled, "permission_revision": gorm.Expr("permission_revision + 1")}).Error; err != nil {
					return err
				}
			}
			for _, membership := range memberships {
				if membership.ID != keep.ID {
					if err := tx.Delete(&model.AdminTenantMembership{}, membership.ID).Error; err != nil {
						return err
					}
				}
			}
		}
		if result := tx.Model(&model.Admin{}).Where("username = ?", adminUsername).Update("password_hash", credential); result.Error != nil {
			return result.Error
		} else if result.RowsAffected != 1 {
			return fmt.Errorf("administrator %q not found in legacy database", adminUsername)
		}
		if err := ensureLegacyClaims(tx, tenant.ID, admins); err != nil {
			return err
		}
		return nil
	})
	return scopedTables, err
}

func ensureLegacyClaims(tx *gorm.DB, tenantID string, admins []model.Admin) error {
	claimedBy := int64(0)
	if len(admins) > 0 {
		claimedBy = admins[0].ID
	}
	type source struct{ sourceType, sourceID string }
	var sources []source
	var nodeIDs []string
	if err := tx.Table("node").Where("type = ?", "agent").Pluck("CAST(id AS TEXT)", &nodeIDs).Error; err != nil {
		return err
	}
	for _, id := range nodeIDs {
		sources = append(sources, source{model.LegacySourceAgentNode, id})
	}
	var endpointIDs []string
	if err := tx.Table("endpoint").Pluck("CAST(id AS TEXT)", &endpointIDs).Error; err != nil {
		return err
	}
	for _, id := range endpointIDs {
		sources = append(sources, source{model.LegacySourceEndpoint, id})
	}
	for _, item := range sources {
		var claim model.LegacyResourceClaim
		err := tx.Where("source_type = ? AND source_id = ?", item.sourceType, item.sourceID).First(&claim).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			claim = model.LegacyResourceClaim{ID: uuid.NewString(), SourceType: item.sourceType, SourceID: item.sourceID}
		} else if err != nil {
			return err
		}
		reason := "legacy signal.wodcloud.com migration to Beagle tenant"
		if claim.CreatedAt.IsZero() {
			claim.TenantID = tenantID
			claim.Status = "active"
			claim.ClaimedBy = claimedBy
			claim.ClaimReason = reason
			if err := tx.Create(&claim).Error; err != nil {
				return err
			}
		} else if claim.TenantID != tenantID || claim.Status != "active" || claim.ClaimedBy != claimedBy || claim.ClaimReason != reason {
			if err := tx.Model(&claim).Updates(map[string]any{"tenant_id": tenantID, "status": "active", "claimed_by": claimedBy, "claim_reason": reason}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func verifyMigration(databasePath, sourceFingerprint, targetFingerprint, tenantID string, scopedTables []string, before map[string]int64, aclHashBefore string) (*migrationReport, error) {
	after, err := legacyCounts(databasePath)
	if err != nil {
		return nil, err
	}
	for _, table := range []string{"node", "endpoint", "resource", "access_grant", "group", "group_member", "legacy_acl_permissions"} {
		if before[table] != after[table] {
			return nil, fmt.Errorf("%s count changed from %d to %d", table, before[table], after[table])
		}
	}
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	report := &migrationReport{SchemaVersion: "legacy-beagle-migration/v1", SourceFingerprint: sourceFingerprint, TargetFingerprint: targetFingerprint, BeagleTenantID: tenantID, ScopedTables: scopedTables, Before: before, After: after, LegacyACLHashBefore: aclHashBefore}
	for _, table := range scopedTables {
		var count int64
		if err := database.QueryRow(`SELECT COUNT(*) FROM "`+table+`" WHERE tenant_id IS NULL OR tenant_id <> ?`, tenantID).Scan(&count); err != nil {
			return nil, err
		}
		report.NonBeagleScopedRows += count
	}
	queries := []struct {
		value *int64
		query string
		args  []any
	}{
		{&report.ClientUsers, "SELECT COUNT(*) FROM user u WHERE role='client' AND NOT EXISTS (SELECT 1 FROM user_authentication_link l WHERE l.user_id=u.id AND l.provider_type='legacy_admin')", nil},
		{&report.TenantMemberships, "SELECT COUNT(*) FROM tenant_membership WHERE tenant_id=?", []any{tenantID}},
		{&report.DistinctMemberUsers, "SELECT COUNT(DISTINCT user_id) FROM tenant_membership WHERE tenant_id=?", []any{tenantID}},
		{&report.Admins, "SELECT COUNT(*) FROM admin", nil},
		{&report.AdminMemberships, "SELECT COUNT(*) FROM admin_tenant_membership WHERE tenant_id=?", []any{tenantID}},
		{&report.LegacyAdminLinks, "SELECT COUNT(*) FROM user_authentication_link WHERE provider_type='legacy_admin'", nil},
		{&report.Nodes, "SELECT COUNT(*) FROM node", nil},
		{&report.AgentNodes, "SELECT COUNT(*) FROM node WHERE type='agent'", nil},
		{&report.Endpoints, "SELECT COUNT(*) FROM endpoint", nil},
		{&report.LegacyResourceClaims, "SELECT COUNT(*) FROM legacy_resource_claim WHERE tenant_id=? AND status='active'", []any{tenantID}},
		{&report.Resources, "SELECT COUNT(*) FROM resource", nil},
		{&report.AccessGrants, "SELECT COUNT(*) FROM access_grant", nil},
		{&report.Groups, `SELECT COUNT(*) FROM "group"`, nil},
		{&report.GroupMembers, "SELECT COUNT(*) FROM group_member", nil},
		{&report.LegacyACLPermissions, legacyACLCountSQL(), nil},
	}
	for _, item := range queries {
		if err := database.QueryRow(item.query, item.args...).Scan(item.value); err != nil {
			return nil, err
		}
	}
	if report.NonBeagleScopedRows != 0 || report.TenantMemberships != int64(lenMust(before, "client_users")) || report.TenantMemberships != report.DistinctMemberUsers || report.AdminMemberships != report.Admins || report.LegacyAdminLinks != report.Admins || report.LegacyResourceClaims < report.AgentNodes+report.Endpoints {
		return nil, fmt.Errorf("migration invariants failed: non_beagle=%d memberships=%d distinct=%d admins=%d admin_memberships=%d admin_links=%d claims=%d expected_claims=%d", report.NonBeagleScopedRows, report.TenantMemberships, report.DistinctMemberUsers, report.Admins, report.AdminMemberships, report.LegacyAdminLinks, report.LegacyResourceClaims, report.AgentNodes+report.Endpoints)
	}
	report.LegacyACLHashAfter, err = tablesContentHash(databasePath, legacyACLTables())
	if err != nil {
		return nil, err
	}
	if report.LegacyACLHashBefore != report.LegacyACLHashAfter {
		return nil, errors.New("legacy ACL permission content changed during migration")
	}
	report.Integrity, err = integrityCheck(databasePath)
	if err != nil || report.Integrity != "ok" {
		return nil, fmt.Errorf("migrated database integrity failed: %s: %w", report.Integrity, err)
	}
	report.MigratedFingerprint, err = fileSHA256(databasePath)
	return report, err
}

func lenMust(counts map[string]int64, key string) int { return int(counts[key]) }

func tenantScopedTables(database *gorm.DB) ([]string, error) {
	var tables []string
	err := database.Raw("SELECT DISTINCT m.name FROM sqlite_master m, pragma_table_info(m.name) p WHERE m.type='table' AND p.name='tenant_id' ORDER BY m.name").Scan(&tables).Error
	if err != nil {
		return nil, err
	}
	for _, table := range tables {
		if !safeIdentifier.MatchString(table) {
			return nil, fmt.Errorf("unsafe table name %q", table)
		}
	}
	return tables, nil
}

func legacyClientIDs(databasePath string) ([]uint64, error) {
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databasePath)+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer database.Close()
	query := "SELECT id FROM user WHERE role='client' ORDER BY id"
	var hasLinks int
	if err := database.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='user_authentication_link'").Scan(&hasLinks); err != nil {
		return nil, err
	}
	if hasLinks > 0 {
		query = "SELECT u.id FROM user u WHERE u.role='client' AND NOT EXISTS (SELECT 1 FROM user_authentication_link l WHERE l.user_id=u.id AND l.provider_type='legacy_admin') ORDER BY u.id"
	}
	rows, err := database.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uint64
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func adminCredential(databasePath, username string) (string, error) {
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databasePath)+"?mode=ro")
	if err != nil {
		return "", err
	}
	defer database.Close()
	var credential string
	if err := database.QueryRow("SELECT password_hash FROM admin WHERE username=? AND enabled=1", username).Scan(&credential); err != nil {
		return "", fmt.Errorf("load target administrator credential: %w", err)
	}
	if credential == "" {
		return "", errors.New("target administrator credential is empty")
	}
	return credential, nil
}

func legacyCounts(databasePath string) (map[string]int64, error) {
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databasePath)+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer database.Close()
	clientQuery := "SELECT COUNT(*) FROM user WHERE role='client'"
	var hasLinks int
	if err := database.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='user_authentication_link'").Scan(&hasLinks); err != nil {
		return nil, err
	}
	if hasLinks > 0 {
		clientQuery = "SELECT COUNT(*) FROM user u WHERE u.role='client' AND NOT EXISTS (SELECT 1 FROM user_authentication_link l WHERE l.user_id=u.id AND l.provider_type='legacy_admin')"
	}
	queries := map[string]string{
		"client_users": clientQuery, "node": "SELECT COUNT(*) FROM node", "endpoint": "SELECT COUNT(*) FROM endpoint",
		"resource": "SELECT COUNT(*) FROM resource", "access_grant": "SELECT COUNT(*) FROM access_grant", "group": `SELECT COUNT(*) FROM "group"`,
		"group_member": "SELECT COUNT(*) FROM group_member", "legacy_acl_permissions": legacyACLCountSQL(),
	}
	counts := make(map[string]int64, len(queries))
	for name, query := range queries {
		var count int64
		if err := database.QueryRow(query).Scan(&count); err != nil {
			return nil, fmt.Errorf("count %s: %w", name, err)
		}
		counts[name] = count
	}
	return counts, nil
}

func legacyACLCountSQL() string {
	tables := legacyACLTables()
	parts := make([]string, 0, len(tables))
	for _, table := range tables {
		parts = append(parts, `SELECT COUNT(*) c FROM "`+table+`"`)
	}
	return "SELECT SUM(c) FROM (" + strings.Join(parts, " UNION ALL ") + ")"
}

func legacyACLTables() []string {
	return []string{"acl_user_user_permission", "acl_user_group_permission", "acl_group_user_permission", "acl_group_group_permission", "acl_ssh_user_permission", "acl_ssh_group_permission", "acl_service_user_permission", "acl_service_group_permission", "acl_k8s_user_permission", "acl_k8s_group_permission", "acl_k8s_service_user_permission", "acl_k8s_service_group_permission"}
}

func tablesContentHash(databasePath string, tables []string) (string, error) {
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databasePath)+"?mode=ro")
	if err != nil {
		return "", err
	}
	defer database.Close()
	combined := sha256.New()
	for _, table := range tables {
		if !safeIdentifier.MatchString(table) {
			return "", fmt.Errorf("unsafe table name %q", table)
		}
		columnRows, err := database.Query(`PRAGMA table_info("` + table + `")`)
		if err != nil {
			return "", err
		}
		var columns []string
		for columnRows.Next() {
			var cid, notNull, primaryKey int
			var name, fieldType string
			var defaultValue any
			if err := columnRows.Scan(&cid, &name, &fieldType, &notNull, &defaultValue, &primaryKey); err != nil {
				columnRows.Close()
				return "", err
			}
			columns = append(columns, name)
		}
		columnRows.Close()
		quoted := make([]string, len(columns))
		for index, column := range columns {
			quoted[index] = `"` + strings.ReplaceAll(column, `"`, `""`) + `"`
		}
		rows, err := database.Query(`SELECT ` + strings.Join(quoted, ",") + ` FROM "` + table + `"`)
		if err != nil {
			return "", err
		}
		var rowHashes []string
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for index := range values {
				pointers[index] = &values[index]
			}
			if err := rows.Scan(pointers...); err != nil {
				rows.Close()
				return "", err
			}
			rowHash := sha256.New()
			for _, value := range values {
				fmt.Fprintf(rowHash, "%T:%v\x00", value, value)
			}
			rowHashes = append(rowHashes, hex.EncodeToString(rowHash.Sum(nil)))
		}
		rows.Close()
		sort.Strings(rowHashes)
		fmt.Fprintf(combined, "%s\x00%s\x00", table, strings.Join(rowHashes, "\n"))
	}
	return hex.EncodeToString(combined.Sum(nil)), nil
}

func absoluteDistinct(paths ...string) (string, string, string, error) {
	abs := make([]string, len(paths))
	seen := map[string]bool{}
	for index, path := range paths {
		value, err := filepath.Abs(path)
		if err != nil {
			return "", "", "", err
		}
		key := strings.ToLower(value)
		if seen[key] {
			return "", "", "", errors.New("database, target baseline and report paths must be distinct")
		}
		seen[key], abs[index] = true, value
	}
	return abs[0], abs[1], abs[2], nil
}

func integrityCheck(databasePath string) (string, error) {
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databasePath)+"?mode=ro")
	if err != nil {
		return "", err
	}
	defer database.Close()
	var integrity string
	err = database.QueryRow("PRAGMA integrity_check").Scan(&integrity)
	return integrity, err
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
