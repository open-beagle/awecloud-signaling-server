package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const compatibilitySchemaVersion = "resource-business-compat/v1"

type classification string

const (
	classificationAutomatic     classification = "automatic"
	classificationManual        classification = "manual"
	classificationCompatibility classification = "compatibility"
	classificationInvalid       classification = "invalid"
)

type fieldCoverage struct {
	Field         string `json:"field"`
	NonEmptyCount int64  `json:"non_empty_count"`
}

type compatibilityCandidate struct {
	SourceType       string         `json:"source_type"`
	SourceID         string         `json:"source_id"`
	SourceRevision   string         `json:"source_revision"`
	Classification   classification `json:"classification"`
	TargetType       string         `json:"target_type,omitempty"`
	TargetCandidates []string       `json:"target_candidates"`
	ReasonCodes      []string       `json:"reason_codes"`
	EvidenceRefs     []string       `json:"evidence_refs"`
	NameHash         string         `json:"name_hash,omitempty"`
}

type classificationSection struct {
	SourceType         string                   `json:"source_type"`
	TablePresent       bool                     `json:"table_present"`
	SourceCount        int64                    `json:"source_count"`
	ClassifiedCount    int64                    `json:"classified_count"`
	AutomaticCount     int64                    `json:"automatic_count"`
	ManualCount        int64                    `json:"manual_count"`
	CompatibilityCount int64                    `json:"compatibility_count"`
	InvalidCount       int64                    `json:"invalid_count"`
	Conserved          bool                     `json:"conserved"`
	Candidates         []compatibilityCandidate `json:"candidates"`
}

type classificationTotals struct {
	SourceCount        int64 `json:"source_count"`
	ClassifiedCount    int64 `json:"classified_count"`
	AutomaticCount     int64 `json:"automatic_count"`
	ManualCount        int64 `json:"manual_count"`
	CompatibilityCount int64 `json:"compatibility_count"`
	InvalidCount       int64 `json:"invalid_count"`
	Conserved          bool  `json:"conserved"`
}

type schemaWarning struct {
	Code   string `json:"code"`
	Table  string `json:"table"`
	Column string `json:"column,omitempty"`
}

type humanGate struct {
	ReasonCode string `json:"reason_code"`
	Count      int64  `json:"count"`
}

type classificationReport struct {
	Sections       []classificationSection
	Totals         classificationTotals
	SchemaWarnings []schemaWarning
	HumanGates     []humanGate
}

type inventoryField struct {
	Name     string
	Required bool
}

type inventorySpec struct {
	Table    string
	Fields   []inventoryField
	Classify bool
}

type inventoryRow struct {
	Values map[string]string
}

func (r inventoryRow) value(field string) string {
	return r.Values[field]
}

type inventoryTable struct {
	Spec    inventorySpec
	Present bool
	Columns map[string]bool
	Rows    []inventoryRow
}

type classificationEngine struct {
	tables map[string]*inventoryTable
}

var inventorySpecs = []inventorySpec{
	spec("admin", true, required("id"), optional("username", "role", "enabled")),
	spec("user", true, required("id"), optional("name", "role", "enabled", "source")),
	spec("admin_tenant_membership", true, required("id"), optional("admin_id", "tenant_id", "role", "enabled", "expires_at", "permission_revision")),
	spec("tenant", true, required("id"), optional("key", "status")),
	spec("tenant_membership", true, required("id"), optional("tenant_id", "user_id", "role", "enabled", "expires_at")),
	spec("group", true, required("id"), optional("tenant_id", "name")),
	spec("group_member", true, required("id"), optional("group_id", "user_id")),
	spec("node", true, required("id"), optional("user_id", "name", "type", "headscale_node_id")),
	spec("endpoint", true, required("id"), optional("user_id", "name", "status", "revoked")),
	spec("resource", true, required("id"), optional("tenant_id", "type", "state", "provider_id", "external_workspace_id", "owner_user_id", "owner_group_id", "agent_node_id", "target_revision")),
	spec("resource_target", true, required("id"), optional("resource_id", "revision", "agent_node_id", "cluster_id", "namespace", "pod_uid", "container_name")),
	spec("workspace_binding", true, required("id"), optional("provider_id", "external_tenant_id", "external_workspace_id", "tenant_id", "owner_user_id", "resource_id", "generation", "status")),
	spec("discovery_candidate", true, required("id"), optional("agent_node_id", "provider_hint", "cluster_id", "namespace", "pod_uid", "container_name", "generation_hint", "status", "resource_id")),
	spec("access_grant", true, required("id"), optional("tenant_id", "resource_id", "subject_type", "subject_user_id", "subject_group_id", "actions", "status", "revision")),
	spec("acl_group_user_permission", true, required("id"), optional("target_group_id", "user_id")),
	spec("acl_group_group_permission", true, required("id"), optional("target_group_id", "group_id")),
	spec("acl_k8s_user_permission", true, required("id"), optional("target_user_id", "user_id", "k8s_groups", "namespaces", "enabled")),
	spec("acl_k8s_group_permission", true, required("id"), optional("target_user_id", "group_id", "k8s_groups", "namespaces", "enabled")),
	spec("acl_k8s_service_user_permission", true, required("id"), optional("target_user_id", "user_id", "namespaces", "service_names", "enabled")),
	spec("acl_k8s_service_group_permission", true, required("id"), optional("target_user_id", "group_id", "namespaces", "service_names", "enabled")),
	spec("acl_service_user_permission", true, required("id"), optional("service_id", "user_id")),
	spec("acl_service_group_permission", true, required("id"), optional("service_id", "group_id")),
	spec("acl_ssh_user_permission", true, required("id"), optional("target_user_id", "user_id", "ssh_users", "enabled")),
	spec("acl_ssh_group_permission", true, required("id"), optional("target_user_id", "group_id", "ssh_users", "enabled")),
	spec("acl_user_user_permission", true, required("id"), optional("target_user_id", "granted_user_id")),
	spec("acl_user_group_permission", true, required("id"), optional("target_user_id", "group_id")),
	spec("audit_log", true, required("id", "created_at"), optional("user_id", "actor_admin_id", "tenant_id", "request_id", "target_id")),
	spec("operation_audit_log", true, required("id", "started_at"), optional("agent_user_id", "client_user_id", "endpoint_id", "created_at")),
	spec("proxy_service", false, required("id"), optional("user_id")),
}

func required(names ...string) []inventoryField {
	result := make([]inventoryField, 0, len(names))
	for _, name := range names {
		result = append(result, inventoryField{Name: name, Required: true})
	}
	return result
}

func optional(names ...string) []inventoryField {
	result := make([]inventoryField, 0, len(names))
	for _, name := range names {
		result = append(result, inventoryField{Name: name})
	}
	return result
}

func spec(table string, classify bool, groups ...[]inventoryField) inventorySpec {
	result := inventorySpec{Table: table, Classify: classify}
	for _, group := range groups {
		result.Fields = append(result.Fields, group...)
	}
	return result
}

func buildClassificationReport(database *sql.DB) (classificationReport, error) {
	engine := classificationEngine{tables: make(map[string]*inventoryTable, len(inventorySpecs))}
	warnings := []schemaWarning{}
	for _, tableSpec := range inventorySpecs {
		table, tableWarnings, err := loadInventoryTable(database, tableSpec)
		if err != nil {
			return classificationReport{}, err
		}
		engine.tables[tableSpec.Table] = table
		warnings = append(warnings, tableWarnings...)
	}

	result := classificationReport{
		Sections:       make([]classificationSection, 0, len(inventorySpecs)),
		SchemaWarnings: warnings,
		HumanGates:     []humanGate{},
	}
	humanGateCounts := map[string]int64{}
	for _, tableSpec := range inventorySpecs {
		if !tableSpec.Classify {
			continue
		}
		table := engine.tables[tableSpec.Table]
		section := classificationSection{
			SourceType: tableSpec.Table, TablePresent: table.Present,
			Candidates: make([]compatibilityCandidate, 0, len(table.Rows)),
		}
		for _, row := range table.Rows {
			candidate := engine.classify(tableSpec, row)
			section.Candidates = append(section.Candidates, candidate)
			if candidate.Classification == classificationManual {
				for _, reason := range candidate.ReasonCodes {
					humanGateCounts[reason]++
				}
			}
		}
		sortCandidates(section.Candidates)
		finalizeSection(&section)
		result.Sections = append(result.Sections, section)
	}
	result.Totals = totalSections(result.Sections)
	for reason, count := range humanGateCounts {
		result.HumanGates = append(result.HumanGates, humanGate{ReasonCode: reason, Count: count})
	}
	sort.Slice(result.HumanGates, func(i, j int) bool { return result.HumanGates[i].ReasonCode < result.HumanGates[j].ReasonCode })
	sort.Slice(result.SchemaWarnings, func(i, j int) bool {
		if result.SchemaWarnings[i].Table != result.SchemaWarnings[j].Table {
			return result.SchemaWarnings[i].Table < result.SchemaWarnings[j].Table
		}
		if result.SchemaWarnings[i].Column != result.SchemaWarnings[j].Column {
			return result.SchemaWarnings[i].Column < result.SchemaWarnings[j].Column
		}
		return result.SchemaWarnings[i].Code < result.SchemaWarnings[j].Code
	})
	if !result.Totals.Conserved {
		return classificationReport{}, errors.New("compatibility classification counts are not conserved")
	}
	return result, nil
}

func loadInventoryTable(database *sql.DB, tableSpec inventorySpec) (*inventoryTable, []schemaWarning, error) {
	present, err := tableExists(database, tableSpec.Table)
	if err != nil {
		return nil, nil, fmt.Errorf("inspect table %s: %w", tableSpec.Table, err)
	}
	table := &inventoryTable{Spec: tableSpec, Present: present, Columns: map[string]bool{}}
	if !present {
		return table, []schemaWarning{{Code: "TABLE_MISSING", Table: tableSpec.Table}}, nil
	}
	columns, err := readTableColumns(database, tableSpec.Table)
	if err != nil {
		return nil, nil, err
	}
	for _, column := range columns {
		table.Columns[column] = true
	}
	var warnings []schemaWarning
	selects := make([]string, 0, len(tableSpec.Fields))
	for _, field := range tableSpec.Fields {
		if table.Columns[field.Name] {
			selects = append(selects, fmt.Sprintf("COALESCE(CAST(%s AS TEXT), '') AS %s", quoteIdentifier(field.Name), quoteIdentifier(field.Name)))
			continue
		}
		if field.Required {
			return nil, nil, fmt.Errorf("table %s is missing required column %s", tableSpec.Table, field.Name)
		}
		warnings = append(warnings, schemaWarning{Code: "OPTIONAL_COLUMN_MISSING", Table: tableSpec.Table, Column: field.Name})
		selects = append(selects, fmt.Sprintf("'' AS %s", quoteIdentifier(field.Name)))
	}
	query := fmt.Sprintf("SELECT %s FROM %s ORDER BY %s", strings.Join(selects, ", "), quoteIdentifier(tableSpec.Table), quoteIdentifier("id"))
	rows, err := database.Query(query)
	if err != nil {
		return nil, nil, fmt.Errorf("read table %s: %w", tableSpec.Table, err)
	}
	defer rows.Close()
	for rows.Next() {
		values := make([]sql.NullString, len(tableSpec.Fields))
		destinations := make([]any, len(values))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, nil, fmt.Errorf("scan table %s: %w", tableSpec.Table, err)
		}
		row := inventoryRow{Values: make(map[string]string, len(values))}
		for i, field := range tableSpec.Fields {
			row.Values[field.Name] = values[i].String
		}
		table.Rows = append(table.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate table %s: %w", tableSpec.Table, err)
	}
	return table, warnings, nil
}

func readTableColumns(database *sql.DB, table string) ([]string, error) {
	rows, err := database.Query(fmt.Sprintf("PRAGMA table_info(%s)", quoteIdentifier(table)))
	if err != nil {
		return nil, fmt.Errorf("inspect columns for %s: %w", table, err)
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func (e classificationEngine) classify(tableSpec inventorySpec, row inventoryRow) compatibilityCandidate {
	if strings.TrimSpace(row.value("id")) == "" {
		candidate := baseCandidate(tableSpec, row, classificationInvalid, "", "SOURCE_PRIMARY_KEY_EMPTY")
		candidate.SourceID = tableSpec.Table + ":<empty>"
		normalizeCandidate(&candidate)
		return candidate
	}
	var candidate compatibilityCandidate
	switch tableSpec.Table {
	case "admin":
		candidate = e.classifyAdmin(row)
	case "user":
		candidate = e.classifyUser(row)
	case "admin_tenant_membership":
		candidate = e.classifyAdminTenantMembership(row)
	case "tenant":
		candidate = e.classifyTenant(row)
	case "tenant_membership":
		candidate = e.classifyTenantMembership(row)
	case "group":
		candidate = e.classifyGroup(row)
	case "group_member":
		candidate = e.classifyGroupMember(row)
	case "node":
		candidate = e.classifyNode(row)
	case "endpoint":
		candidate = e.classifyEndpoint(row)
	case "resource":
		candidate = e.classifyResource(row)
	case "resource_target":
		candidate = e.classifyResourceTarget(row)
	case "workspace_binding":
		candidate = e.classifyWorkspaceBinding(row)
	case "discovery_candidate":
		candidate = e.classifyDiscoveryCandidate(row)
	case "access_grant":
		candidate = e.classifyAccessGrant(row)
	case "audit_log", "operation_audit_log":
		candidate = baseCandidate(tableSpec, row, classificationCompatibility, "audit_record", "AUDIT_SOURCE_RETAIN_SEPARATE")
	default:
		candidate = e.classifyLegacyACL(tableSpec.Table, row)
	}
	if candidate.SourceType == "" {
		candidate = baseCandidate(tableSpec, row, classificationInvalid, "", "CLASSIFIER_MISSING")
	}
	normalizeCandidate(&candidate)
	return candidate
}

func baseCandidate(tableSpec inventorySpec, row inventoryRow, category classification, targetType string, reasons ...string) compatibilityCandidate {
	return compatibilityCandidate{
		SourceType: tableSpec.Table, SourceID: sourceRef(tableSpec.Table, row.value("id")), SourceRevision: rowRevision(tableSpec, row),
		Classification: category, TargetType: targetType, ReasonCodes: append([]string(nil), reasons...),
	}
}

func (e classificationEngine) classifyAdmin(row inventoryRow) compatibilityCandidate {
	spec := e.tables["admin"].Spec
	name := strings.TrimSpace(row.value("username"))
	if name == "" {
		return baseCandidate(spec, row, classificationInvalid, "user", "ADMIN_USERNAME_MISSING")
	}
	matches := e.findRows("user", "name", name)
	reasons := []string{"ADMIN_USER_IDENTITY_REQUIRES_CONFIRMATION"}
	if len(matches) == 0 {
		reasons = append(reasons, "ADMIN_USER_NAME_NOT_FOUND")
	} else if len(matches) == 1 {
		reasons = append(reasons, "ADMIN_USER_EXACT_NAME_ONLY")
	} else {
		reasons = append(reasons, "ADMIN_USER_NAME_CONFLICT")
	}
	candidate := baseCandidate(spec, row, classificationManual, "user", reasons...)
	candidate.NameHash = hashString(name)
	for _, match := range matches {
		candidate.TargetCandidates = append(candidate.TargetCandidates, sourceRef("user", match.value("id")))
	}
	return candidate
}

func (e classificationEngine) classifyUser(row inventoryRow) compatibilityCandidate {
	spec := e.tables["user"].Spec
	name := strings.TrimSpace(row.value("name"))
	role := row.value("role")
	if name == "" {
		return baseCandidate(spec, row, classificationInvalid, "user", "USER_NAME_MISSING")
	}
	if len(e.findRows("user", "name", name)) != 1 {
		return baseCandidate(spec, row, classificationInvalid, "user", "USER_NAME_NOT_UNIQUE")
	}
	switch role {
	case "client":
		candidate := baseCandidate(spec, row, classificationAutomatic, "user", "CLIENT_USER_STABLE_IDENTITY")
		candidate.TargetCandidates = []string{sourceRef("user", row.value("id"))}
		candidate.NameHash = hashString(name)
		return candidate
	case "agent":
		candidate := baseCandidate(spec, row, classificationCompatibility, "technical_resource", "AGENT_USER_BECOMES_TECHNICAL_SOURCE", "PROVIDER_OWNERSHIP_UNPROVEN")
		candidate.NameHash = hashString(name)
		return candidate
	default:
		return baseCandidate(spec, row, classificationInvalid, "user", "USER_ROLE_INVALID")
	}
}

func (e classificationEngine) classifyAdminTenantMembership(row inventoryRow) compatibilityCandidate {
	spec := e.tables["admin_tenant_membership"].Spec
	adminID, tenantID := row.value("admin_id"), row.value("tenant_id")
	if !e.has("admin", adminID) || !e.has("tenant", tenantID) {
		return baseCandidate(spec, row, classificationInvalid, "admin_tenant_membership", "ADMIN_TENANT_REFERENCE_BROKEN")
	}
	if !inSet(row.value("role"), "tenant_admin", "security_auditor", "tenant_viewer", "viewer") {
		return baseCandidate(spec, row, classificationInvalid, "admin_tenant_membership", "ADMIN_TENANT_ROLE_INVALID")
	}
	admin := e.get("admin", adminID)
	matches := e.findRows("user", "name", admin.value("username"))
	candidate := baseCandidate(spec, row, classificationCompatibility, "admin_tenant_membership", "ADMIN_USER_MAPPING_NOT_CONFIRMED")
	candidate.EvidenceRefs = []string{sourceRef("admin", adminID), sourceRef("tenant", tenantID)}
	if len(matches) == 1 {
		candidate.Classification = classificationManual
		candidate.ReasonCodes = []string{"ADMIN_USER_MAPPING_REQUIRES_CONFIRMATION"}
		candidate.TargetCandidates = []string{sourceRef("user", matches[0].value("id"))}
	}
	return candidate
}

func (e classificationEngine) classifyTenant(row inventoryRow) compatibilityCandidate {
	spec := e.tables["tenant"].Spec
	key := strings.TrimSpace(row.value("key"))
	if key == "" || len(e.findRows("tenant", "key", key)) != 1 {
		return baseCandidate(spec, row, classificationInvalid, "consumer_tenant", "TENANT_KEY_MISSING_OR_DUPLICATE")
	}
	if !inSet(row.value("status"), "active", "suspended") {
		return baseCandidate(spec, row, classificationManual, "consumer_tenant", "TENANT_STATUS_REQUIRES_MAPPING")
	}
	candidate := baseCandidate(spec, row, classificationAutomatic, "consumer_tenant", "TENANT_STABLE_ID_REUSABLE")
	candidate.TargetCandidates = []string{sourceRef("tenant", row.value("id"))}
	return candidate
}

func (e classificationEngine) classifyTenantMembership(row inventoryRow) compatibilityCandidate {
	spec := e.tables["tenant_membership"].Spec
	tenantID, userID := row.value("tenant_id"), row.value("user_id")
	if tenantID == "" && e.has("user", userID) {
		return baseCandidate(spec, row, classificationCompatibility, "tenant_membership", "LEGACY_UNSCOPED_MEMBERSHIP")
	}
	if !e.has("tenant", tenantID) || !e.has("user", userID) {
		return baseCandidate(spec, row, classificationInvalid, "tenant_membership", "TENANT_MEMBERSHIP_REFERENCE_BROKEN")
	}
	candidate := baseCandidate(spec, row, classificationAutomatic, "tenant_membership", "MEMBER_RELATION_REUSABLE")
	candidate.EvidenceRefs = []string{sourceRef("tenant", tenantID), sourceRef("user", userID)}
	switch row.value("role") {
	case "member":
		return candidate
	case "tenant_admin":
		candidate.Classification = classificationManual
		candidate.ReasonCodes = []string{"LEGACY_TENANT_ADMIN_SEMANTICS"}
		return candidate
	default:
		candidate.Classification = classificationManual
		candidate.ReasonCodes = []string{"TENANT_MEMBERSHIP_ROLE_REQUIRES_MAPPING"}
		return candidate
	}
}

func (e classificationEngine) classifyGroup(row inventoryRow) compatibilityCandidate {
	spec := e.tables["group"].Spec
	if strings.TrimSpace(row.value("name")) == "" {
		return baseCandidate(spec, row, classificationInvalid, "group", "GROUP_NAME_MISSING")
	}
	tenantID := row.value("tenant_id")
	if tenantID == "" {
		return baseCandidate(spec, row, classificationManual, "group", "GROUP_TENANT_REQUIRES_MAPPING")
	}
	if !e.has("tenant", tenantID) {
		return baseCandidate(spec, row, classificationInvalid, "group", "GROUP_TENANT_REFERENCE_BROKEN")
	}
	if e.countGroupName(tenantID, row.value("name")) != 1 {
		return baseCandidate(spec, row, classificationInvalid, "group", "GROUP_NAME_NOT_UNIQUE_IN_TENANT")
	}
	candidate := baseCandidate(spec, row, classificationAutomatic, "group", "GROUP_TENANT_RELATION_REUSABLE")
	candidate.EvidenceRefs = []string{sourceRef("tenant", tenantID)}
	return candidate
}

func (e classificationEngine) classifyGroupMember(row inventoryRow) compatibilityCandidate {
	spec := e.tables["group_member"].Spec
	groupID, userID := row.value("group_id"), row.value("user_id")
	if !e.has("group", groupID) || !e.has("user", userID) {
		return baseCandidate(spec, row, classificationInvalid, "group_membership", "GROUP_MEMBER_REFERENCE_BROKEN")
	}
	group := e.get("group", groupID)
	tenantID := group.value("tenant_id")
	if tenantID == "" {
		return baseCandidate(spec, row, classificationCompatibility, "group_membership", "GROUP_TENANT_UNCONFIRMED")
	}
	if !e.hasTenantMembership(tenantID, userID) {
		candidate := baseCandidate(spec, row, classificationManual, "group_membership", "GROUP_MEMBER_TENANT_MEMBERSHIP_MISSING")
		candidate.EvidenceRefs = []string{sourceRef("group", groupID), sourceRef("user", userID), sourceRef("tenant", tenantID)}
		return candidate
	}
	candidate := baseCandidate(spec, row, classificationAutomatic, "group_membership", "GROUP_MEMBER_RELATION_REUSABLE")
	candidate.EvidenceRefs = []string{sourceRef("group", groupID), sourceRef("user", userID), sourceRef("tenant", tenantID)}
	return candidate
}

func (e classificationEngine) classifyNode(row inventoryRow) compatibilityCandidate {
	spec := e.tables["node"].Spec
	userID := row.value("user_id")
	if !e.has("user", userID) {
		return baseCandidate(spec, row, classificationInvalid, "technical_resource", "NODE_USER_REFERENCE_BROKEN")
	}
	userRole := e.get("user", userID).value("role")
	switch row.value("type") {
	case "agent":
		if userRole != "agent" {
			return baseCandidate(spec, row, classificationInvalid, "technical_resource", "AGENT_NODE_USER_ROLE_CONFLICT")
		}
		candidate := baseCandidate(spec, row, classificationManual, "technical_resource", "PROVIDER_OWNERSHIP_REQUIRES_CONFIRMATION")
		candidate.EvidenceRefs = []string{sourceRef("user", userID)}
		return candidate
	case "desktop":
		candidate := baseCandidate(spec, row, classificationCompatibility, "device", "DESKTOP_NODE_REMAINS_COMPATIBILITY")
		candidate.EvidenceRefs = []string{sourceRef("user", userID)}
		return candidate
	default:
		return baseCandidate(spec, row, classificationInvalid, "technical_resource", "NODE_TYPE_INVALID")
	}
}

func (e classificationEngine) classifyEndpoint(row inventoryRow) compatibilityCandidate {
	spec := e.tables["endpoint"].Spec
	userID := row.value("user_id")
	if !e.has("user", userID) || e.get("user", userID).value("role") != "agent" {
		return baseCandidate(spec, row, classificationInvalid, "technical_resource", "ENDPOINT_AGENT_REFERENCE_BROKEN")
	}
	candidate := baseCandidate(spec, row, classificationManual, "technical_resource", "ENDPOINT_PROVIDER_OWNERSHIP_REQUIRES_CONFIRMATION")
	candidate.EvidenceRefs = []string{sourceRef("user", userID)}
	return candidate
}

func (e classificationEngine) classifyResource(row inventoryRow) compatibilityCandidate {
	spec := e.tables["resource"].Spec
	tenantID := row.value("tenant_id")
	if !e.has("tenant", tenantID) {
		return baseCandidate(spec, row, classificationInvalid, "tenant_resource", "RESOURCE_TENANT_REFERENCE_BROKEN")
	}
	if !inSet(row.value("type"), "host_ssh", "container_ssh", "kubernetes_api", "database_service", "tcp_service") ||
		!inSet(row.value("state"), "pending", "available", "degraded", "draining", "stopped", "revoked") {
		return baseCandidate(spec, row, classificationInvalid, "tenant_resource", "RESOURCE_TYPE_OR_STATE_INVALID")
	}
	resourceID := row.value("id")
	bindings := e.validWorkspaceBindings(resourceID)
	targets := e.validResourceTargets(resourceID)
	candidate := baseCandidate(spec, row, classificationCompatibility, "tenant_resource", "RESOURCE_SOURCE_NOT_PROVEN")
	candidate.EvidenceRefs = []string{sourceRef("tenant", tenantID)}
	if len(bindings) > 0 || len(targets) > 0 {
		candidate.Classification = classificationManual
		candidate.ReasonCodes = []string{"RESOURCE_SOURCE_REQUIRES_CONFIRMATION"}
		for _, binding := range bindings {
			candidate.EvidenceRefs = append(candidate.EvidenceRefs, sourceRef("workspace_binding", binding.value("id")))
		}
		for _, target := range targets {
			candidate.EvidenceRefs = append(candidate.EvidenceRefs, sourceRef("resource_target", target.value("id")))
		}
	}
	return candidate
}

func (e classificationEngine) classifyResourceTarget(row inventoryRow) compatibilityCandidate {
	spec := e.tables["resource_target"].Spec
	resourceID, agentNodeID := row.value("resource_id"), row.value("agent_node_id")
	revision, revisionOK := positiveInteger(row.value("revision"))
	if !e.has("resource", resourceID) || !revisionOK || revision <= 0 || !e.validAgentNode(agentNodeID) {
		return baseCandidate(spec, row, classificationInvalid, "resource_target_revision", "RESOURCE_TARGET_REFERENCE_OR_RUNTIME_INVALID")
	}
	resourceType := e.get("resource", resourceID).value("type")
	switch resourceType {
	case "container_ssh":
		if row.value("pod_uid") == "" || row.value("container_name") == "" {
			return baseCandidate(spec, row, classificationInvalid, "resource_target_revision", "RESOURCE_TARGET_REFERENCE_OR_RUNTIME_INVALID")
		}
	case "kubernetes_api":
		if row.value("cluster_id") == "" {
			return baseCandidate(spec, row, classificationInvalid, "resource_target_revision", "RESOURCE_TARGET_REFERENCE_OR_RUNTIME_INVALID")
		}
	case "database_service", "tcp_service":
		candidate := baseCandidate(spec, row, classificationCompatibility, "resource_target_revision", "RESOURCE_TARGET_RUNTIME_MAPPING_UNPROVEN")
		candidate.EvidenceRefs = []string{sourceRef("resource", resourceID), sourceRef("node", agentNodeID)}
		return candidate
	case "host_ssh":
	default:
		return baseCandidate(spec, row, classificationInvalid, "resource_target_revision", "RESOURCE_TARGET_RESOURCE_TYPE_INVALID")
	}
	candidate := baseCandidate(spec, row, classificationManual, "resource_target_revision", "RESOURCE_TARGET_SOURCE_REQUIRES_CONFIRMATION")
	candidate.EvidenceRefs = []string{sourceRef("resource", resourceID), sourceRef("node", agentNodeID)}
	return candidate
}

func (e classificationEngine) classifyWorkspaceBinding(row inventoryRow) compatibilityCandidate {
	spec := e.tables["workspace_binding"].Spec
	tenantID, resourceID, ownerID := row.value("tenant_id"), row.value("resource_id"), row.value("owner_user_id")
	if !e.has("tenant", tenantID) || !e.has("resource", resourceID) || e.get("resource", resourceID).value("tenant_id") != tenantID ||
		(ownerID != "" && ownerID != "0" && !e.has("user", ownerID)) || row.value("provider_id") == "" || row.value("external_workspace_id") == "" {
		return baseCandidate(spec, row, classificationInvalid, "ownership_evidence", "WORKSPACE_BINDING_REFERENCE_BROKEN")
	}
	candidate := baseCandidate(spec, row, classificationManual, "ownership_evidence", "WORKSPACE_BINDING_NOT_ALLOCATION")
	candidate.EvidenceRefs = []string{sourceRef("tenant", tenantID), sourceRef("resource", resourceID)}
	if ownerID != "" && ownerID != "0" {
		candidate.EvidenceRefs = append(candidate.EvidenceRefs, sourceRef("user", ownerID))
	}
	return candidate
}

func (e classificationEngine) classifyDiscoveryCandidate(row inventoryRow) compatibilityCandidate {
	spec := e.tables["discovery_candidate"].Spec
	agentNodeID, resourceID := row.value("agent_node_id"), row.value("resource_id")
	if !e.validAgentNode(agentNodeID) || row.value("namespace") == "" || row.value("pod_uid") == "" || row.value("container_name") == "" ||
		(resourceID != "" && !e.has("resource", resourceID)) || !inSet(row.value("status"), "observed", "pending_claim", "published", "conflict", "stale", "rejected") {
		return baseCandidate(spec, row, classificationInvalid, "workload_observation", "DISCOVERY_RUNTIME_OR_REFERENCE_INVALID")
	}
	candidate := baseCandidate(spec, row, classificationCompatibility, "workload_observation", "DISCOVERY_OBSERVATION_REMAINS_COMPATIBILITY")
	candidate.EvidenceRefs = []string{sourceRef("node", agentNodeID)}
	if resourceID != "" {
		candidate.EvidenceRefs = append(candidate.EvidenceRefs, sourceRef("resource", resourceID))
	}
	if row.value("status") == "published" || row.value("status") == "conflict" {
		candidate.Classification = classificationManual
		candidate.ReasonCodes = []string{"DISCOVERY_RECONCILIATION_REQUIRES_CONFIRMATION"}
	}
	return candidate
}

func (e classificationEngine) classifyAccessGrant(row inventoryRow) compatibilityCandidate {
	spec := e.tables["access_grant"].Spec
	tenantID, resourceID := row.value("tenant_id"), row.value("resource_id")
	if !e.has("tenant", tenantID) || !e.has("resource", resourceID) || e.get("resource", resourceID).value("tenant_id") != tenantID {
		return baseCandidate(spec, row, classificationInvalid, "access_grant", "ACCESS_GRANT_RESOURCE_OR_TENANT_INVALID")
	}
	evidence := []string{sourceRef("tenant", tenantID), sourceRef("resource", resourceID)}
	switch row.value("subject_type") {
	case "user":
		userID := row.value("subject_user_id")
		if !e.has("user", userID) || !e.hasTenantMembership(tenantID, userID) {
			return baseCandidate(spec, row, classificationInvalid, "access_grant", "ACCESS_GRANT_USER_SUBJECT_INVALID")
		}
		evidence = append(evidence, sourceRef("user", userID))
	case "group":
		groupID := row.value("subject_group_id")
		if !e.has("group", groupID) || e.get("group", groupID).value("tenant_id") != tenantID {
			return baseCandidate(spec, row, classificationInvalid, "access_grant", "ACCESS_GRANT_GROUP_SUBJECT_INVALID")
		}
		evidence = append(evidence, sourceRef("group", groupID))
	default:
		return baseCandidate(spec, row, classificationInvalid, "access_grant", "ACCESS_GRANT_SUBJECT_TYPE_INVALID")
	}
	actions, ok := jsonStringArray(row.value("actions"))
	if !ok || len(actions) == 0 {
		return baseCandidate(spec, row, classificationInvalid, "access_grant", "ACCESS_GRANT_ACTIONS_INVALID")
	}
	candidate := baseCandidate(spec, row, classificationManual, "access_grant", "ACCESS_GRANT_REQUIRES_TARGET_MODEL_CONFIRMATION")
	candidate.EvidenceRefs = evidence
	if !actionsCompatible(e.get("resource", resourceID).value("type"), actions) {
		candidate.Classification = classificationCompatibility
		candidate.ReasonCodes = []string{"ACCESS_GRANT_ACTION_MAPPING_UNPROVEN"}
	}
	return candidate
}

type legacyACLRule struct {
	TargetTable   string
	TargetColumn  string
	SubjectTable  string
	SubjectColumn string
	JSONColumns   []string
}

var legacyACLRules = map[string]legacyACLRule{
	"acl_group_user_permission":        {"group", "target_group_id", "user", "user_id", nil},
	"acl_group_group_permission":       {"group", "target_group_id", "group", "group_id", nil},
	"acl_k8s_user_permission":          {"user", "target_user_id", "user", "user_id", []string{"k8s_groups", "namespaces"}},
	"acl_k8s_group_permission":         {"user", "target_user_id", "group", "group_id", []string{"k8s_groups", "namespaces"}},
	"acl_k8s_service_user_permission":  {"user", "target_user_id", "user", "user_id", []string{"namespaces", "service_names"}},
	"acl_k8s_service_group_permission": {"user", "target_user_id", "group", "group_id", []string{"namespaces", "service_names"}},
	"acl_service_user_permission":      {"proxy_service", "service_id", "user", "user_id", nil},
	"acl_service_group_permission":     {"proxy_service", "service_id", "group", "group_id", nil},
	"acl_ssh_user_permission":          {"user", "target_user_id", "user", "user_id", []string{"ssh_users"}},
	"acl_ssh_group_permission":         {"user", "target_user_id", "group", "group_id", []string{"ssh_users"}},
	"acl_user_user_permission":         {"user", "target_user_id", "user", "granted_user_id", nil},
	"acl_user_group_permission":        {"user", "target_user_id", "group", "group_id", nil},
}

func (e classificationEngine) classifyLegacyACL(table string, row inventoryRow) compatibilityCandidate {
	tableSpec := e.tables[table].Spec
	rule, ok := legacyACLRules[table]
	if !ok {
		return baseCandidate(tableSpec, row, classificationInvalid, "access_grant", "ACL_RULE_MISSING")
	}
	targetID, subjectID := row.value(rule.TargetColumn), row.value(rule.SubjectColumn)
	if !e.has(rule.TargetTable, targetID) || !e.has(rule.SubjectTable, subjectID) {
		return baseCandidate(tableSpec, row, classificationInvalid, "access_grant", "ACL_REFERENCE_BROKEN")
	}
	if rule.TargetTable == "user" && e.get("user", targetID).value("role") != "agent" {
		return baseCandidate(tableSpec, row, classificationInvalid, "access_grant", "ACL_TARGET_IS_NOT_AGENT")
	}
	for _, column := range rule.JSONColumns {
		if _, valid := jsonStringArray(row.value(column)); !valid {
			return baseCandidate(tableSpec, row, classificationInvalid, "access_grant", "ACL_CONSTRAINT_JSON_INVALID")
		}
	}
	if rule.TargetTable == "group" && rule.SubjectTable == "group" {
		targetTenant := e.get("group", targetID).value("tenant_id")
		subjectTenant := e.get("group", subjectID).value("tenant_id")
		if targetTenant != "" && subjectTenant != "" && targetTenant != subjectTenant {
			return baseCandidate(tableSpec, row, classificationInvalid, "access_grant", "ACL_GROUP_CROSS_TENANT")
		}
	}
	candidate := baseCandidate(tableSpec, row, classificationCompatibility, "access_grant", "ACL_TENANT_RESOURCE_ACTION_UNPROVEN")
	candidate.EvidenceRefs = []string{sourceRef(rule.TargetTable, targetID), sourceRef(rule.SubjectTable, subjectID)}
	return candidate
}

func (e classificationEngine) has(table, id string) bool {
	if id == "" || id == "0" {
		return false
	}
	_, ok := e.findByID(table, id)
	return ok
}

func (e classificationEngine) get(table, id string) inventoryRow {
	row, _ := e.findByID(table, id)
	return row
}

func (e classificationEngine) findByID(table, id string) (inventoryRow, bool) {
	inventory := e.tables[table]
	if inventory == nil {
		return inventoryRow{}, false
	}
	for _, row := range inventory.Rows {
		if row.value("id") == id {
			return row, true
		}
	}
	return inventoryRow{}, false
}

func (e classificationEngine) findRows(table, field, value string) []inventoryRow {
	inventory := e.tables[table]
	if inventory == nil || value == "" {
		return nil
	}
	var result []inventoryRow
	for _, row := range inventory.Rows {
		if row.value(field) == value {
			result = append(result, row)
		}
	}
	return result
}

func (e classificationEngine) countGroupName(tenantID, name string) int {
	count := 0
	for _, row := range e.tables["group"].Rows {
		if row.value("tenant_id") == tenantID && row.value("name") == name {
			count++
		}
	}
	return count
}

func (e classificationEngine) hasTenantMembership(tenantID, userID string) bool {
	for _, row := range e.tables["tenant_membership"].Rows {
		if row.value("tenant_id") == tenantID && row.value("user_id") == userID {
			return true
		}
	}
	return false
}

func (e classificationEngine) validWorkspaceBindings(resourceID string) []inventoryRow {
	var result []inventoryRow
	for _, row := range e.findRows("workspace_binding", "resource_id", resourceID) {
		if e.classifyWorkspaceBinding(row).Classification == classificationManual {
			result = append(result, row)
		}
	}
	return result
}

func (e classificationEngine) validResourceTargets(resourceID string) []inventoryRow {
	var result []inventoryRow
	for _, row := range e.findRows("resource_target", "resource_id", resourceID) {
		if e.classifyResourceTarget(row).Classification == classificationManual {
			result = append(result, row)
		}
	}
	return result
}

func (e classificationEngine) validAgentNode(nodeID string) bool {
	if !e.has("node", nodeID) {
		return false
	}
	node := e.get("node", nodeID)
	return node.value("type") == "agent" && e.has("user", node.value("user_id")) && e.get("user", node.value("user_id")).value("role") == "agent"
}

func rowRevision(tableSpec inventorySpec, row inventoryRow) string {
	values := make([][2]string, 0, len(tableSpec.Fields))
	for _, field := range tableSpec.Fields {
		values = append(values, [2]string{field.Name, row.value(field.Name)})
	}
	encoded, _ := json.Marshal(values)
	return hashBytes(encoded)
}

func normalizeCandidate(candidate *compatibilityCandidate) {
	sort.Strings(candidate.TargetCandidates)
	sort.Strings(candidate.ReasonCodes)
	sort.Strings(candidate.EvidenceRefs)
	candidate.TargetCandidates = uniqueStrings(candidate.TargetCandidates)
	candidate.ReasonCodes = uniqueStrings(candidate.ReasonCodes)
	candidate.EvidenceRefs = uniqueStrings(candidate.EvidenceRefs)
	if candidate.TargetCandidates == nil {
		candidate.TargetCandidates = []string{}
	}
	if candidate.ReasonCodes == nil {
		candidate.ReasonCodes = []string{}
	}
	if candidate.EvidenceRefs == nil {
		candidate.EvidenceRefs = []string{}
	}
}

func sortCandidates(candidates []compatibilityCandidate) {
	sort.Slice(candidates, func(i, j int) bool {
		return sourceIDLess(candidates[i].SourceID, candidates[j].SourceID)
	})
}

func sourceIDLess(left, right string) bool {
	leftParts, rightParts := strings.SplitN(left, ":", 2), strings.SplitN(right, ":", 2)
	if leftParts[0] != rightParts[0] {
		return leftParts[0] < rightParts[0]
	}
	if len(leftParts) == 2 && len(rightParts) == 2 {
		leftNumber, leftErr := strconv.ParseInt(leftParts[1], 10, 64)
		rightNumber, rightErr := strconv.ParseInt(rightParts[1], 10, 64)
		if leftErr == nil && rightErr == nil {
			return leftNumber < rightNumber
		}
		return leftParts[1] < rightParts[1]
	}
	return left < right
}

func finalizeSection(section *classificationSection) {
	section.SourceCount = int64(len(section.Candidates))
	for _, candidate := range section.Candidates {
		switch candidate.Classification {
		case classificationAutomatic:
			section.AutomaticCount++
		case classificationManual:
			section.ManualCount++
		case classificationCompatibility:
			section.CompatibilityCount++
		case classificationInvalid:
			section.InvalidCount++
		}
	}
	section.ClassifiedCount = section.AutomaticCount + section.ManualCount + section.CompatibilityCount + section.InvalidCount
	section.Conserved = section.SourceCount == section.ClassifiedCount
}

func totalSections(sections []classificationSection) classificationTotals {
	var totals classificationTotals
	for _, section := range sections {
		totals.SourceCount += section.SourceCount
		totals.ClassifiedCount += section.ClassifiedCount
		totals.AutomaticCount += section.AutomaticCount
		totals.ManualCount += section.ManualCount
		totals.CompatibilityCount += section.CompatibilityCount
		totals.InvalidCount += section.InvalidCount
	}
	totals.Conserved = totals.SourceCount == totals.ClassifiedCount
	return totals
}

func compatibilityContentHash(report compatibilityReport) (string, error) {
	payload := struct {
		SchemaVersion     string
		SourceFingerprint string
		Sections          []classificationSection
		Totals            classificationTotals
		SchemaWarnings    []schemaWarning
		HumanGates        []humanGate
		AuditSources      []auditTableReport
	}{
		SchemaVersion: report.SchemaVersion, SourceFingerprint: report.SourceFingerprint, Sections: report.Sections,
		Totals: report.Totals, SchemaWarnings: report.SchemaWarnings, HumanGates: report.HumanGates, AuditSources: report.AuditSources,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal deterministic report content: %w", err)
	}
	return hashBytes(encoded), nil
}

func enrichAuditReports(database *sql.DB, management, operation *auditTableReport) error {
	if err := enrichAuditReport(database, management, []string{"user_id", "actor_admin_id", "tenant_id", "target_id", "request_id", "created_at"}, []string{"user_id", "actor_admin_id", "tenant_id", "target_id", "request_id"}); err != nil {
		return err
	}
	return enrichAuditReport(database, operation, []string{"agent_user_id", "client_user_id", "endpoint_id", "target", "started_at", "ended_at"}, []string{"agent_user_id", "client_user_id", "endpoint_id"})
}

func enrichAuditReport(database *sql.DB, report *auditTableReport, coverageFields, correlationFields []string) error {
	if !report.Exists {
		return nil
	}
	columns := make(map[string]bool, len(report.Columns))
	for _, column := range report.Columns {
		columns[column] = true
	}
	for _, field := range coverageFields {
		coverage := fieldCoverage{Field: field}
		if columns[field] {
			query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s IS NOT NULL AND TRIM(CAST(%s AS TEXT)) <> '' AND CAST(%s AS TEXT) <> '0'", quoteIdentifier(report.Table), quoteIdentifier(field), quoteIdentifier(field), quoteIdentifier(field))
			if err := database.QueryRow(query).Scan(&coverage.NonEmptyCount); err != nil {
				return fmt.Errorf("count %s.%s coverage: %w", report.Table, field, err)
			}
		}
		report.FieldCoverage = append(report.FieldCoverage, coverage)
	}
	var expressions []string
	for _, field := range correlationFields {
		if columns[field] {
			expressions = append(expressions, fmt.Sprintf("(%s IS NOT NULL AND TRIM(CAST(%s AS TEXT)) <> '' AND CAST(%s AS TEXT) <> '0')", quoteIdentifier(field), quoteIdentifier(field), quoteIdentifier(field)))
		}
	}
	if len(expressions) > 0 {
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", quoteIdentifier(report.Table), strings.Join(expressions, " OR "))
		if err := database.QueryRow(query).Scan(&report.CorrelatedCount); err != nil {
			return fmt.Errorf("count %s correlation: %w", report.Table, err)
		}
	}
	return nil
}

func renderClassificationMarkdown(builder *strings.Builder, report compatibilityReport) {
	builder.WriteString("\n## 迁移分类汇总\n\n| 来源 | 存在 | 来源数 | 自动 | 人工 | 兼容 | 无效 | 守恒 |\n| --- | --- | ---: | ---: | ---: | ---: | ---: | --- |\n")
	for _, section := range report.Sections {
		fmt.Fprintf(builder, "| %s | %t | %d | %d | %d | %d | %d | %t |\n", markdownCell(section.SourceType), section.TablePresent, section.SourceCount, section.AutomaticCount, section.ManualCount, section.CompatibilityCount, section.InvalidCount, section.Conserved)
	}
	fmt.Fprintf(builder, "| **总计** | - | **%d** | **%d** | **%d** | **%d** | **%d** | **%t** |\n", report.Totals.SourceCount, report.Totals.AutomaticCount, report.Totals.ManualCount, report.Totals.CompatibilityCount, report.Totals.InvalidCount, report.Totals.Conserved)

	builder.WriteString("\n## 人工确认节点\n\n| 原因码 | 数量 |\n| --- | ---: |\n")
	if len(report.HumanGates) == 0 {
		builder.WriteString("| 无 | 0 |\n")
	}
	for _, gate := range report.HumanGates {
		fmt.Fprintf(builder, "| %s | %d |\n", markdownCell(gate.ReasonCode), gate.Count)
	}

	builder.WriteString("\n## Schema 差异\n\n| 代码 | 表 | 列 |\n| --- | --- | --- |\n")
	if len(report.SchemaWarnings) == 0 {
		builder.WriteString("| 无 | - | - |\n")
	}
	for _, warning := range report.SchemaWarnings {
		fmt.Fprintf(builder, "| %s | %s | %s |\n", markdownCell(warning.Code), markdownCell(warning.Table), markdownCell(warning.Column))
	}

	builder.WriteString("\n## 逐行迁移候选\n\n| 来源 ID | Revision | 分类 | 目标类型 | 目标候选 | 原因码 | 证据引用 |\n| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, section := range report.Sections {
		for _, candidate := range section.Candidates {
			fmt.Fprintf(builder, "| %s | %s | %s | %s | %s | %s | %s |\n",
				markdownCell(candidate.SourceID), markdownCell(candidate.SourceRevision), markdownCell(string(candidate.Classification)), markdownCell(candidate.TargetType),
				markdownCell(strings.Join(candidate.TargetCandidates, ", ")), markdownCell(strings.Join(candidate.ReasonCodes, ", ")), markdownCell(strings.Join(candidate.EvidenceRefs, ", ")))
		}
	}
	builder.WriteString("\n> 分类是只读迁移候选，不等同于人工 mapping manifest 确认；`manual` 与 `compatibility` 记录在确认前不得进入新写路径。\n")
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "|", `\|`)
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

func validateOutputPath(databasePath, outputPath string) error {
	databaseAbs, err := filepath.Abs(databasePath)
	if err != nil {
		return err
	}
	outputAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	if filepath.Clean(databaseAbs) == filepath.Clean(outputAbs) {
		return errors.New("output path must not be the input database")
	}
	databaseInfo, databaseErr := os.Stat(databaseAbs)
	outputInfo, outputErr := os.Stat(outputAbs)
	if databaseErr == nil && outputErr == nil && os.SameFile(databaseInfo, outputInfo) {
		return errors.New("output path resolves to the input database")
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func hashString(value string) string { return hashBytes([]byte(value)) }

func hashBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func sourceRef(table, id string) string { return table + ":" + id }

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func inSet(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func positiveInteger(value string) (int64, bool) {
	number, err := strconv.ParseInt(value, 10, 64)
	return number, err == nil && number > 0
}

func jsonStringArray(value string) ([]string, bool) {
	var result []string
	if err := json.Unmarshal([]byte(value), &result); err != nil || result == nil {
		return nil, false
	}
	for _, item := range result {
		if strings.TrimSpace(item) == "" {
			return nil, false
		}
	}
	return result, true
}

func actionsCompatible(resourceType string, actions []string) bool {
	allowed := map[string]map[string]bool{
		"container_ssh":    {"shell": true},
		"host_ssh":         {"shell": true},
		"kubernetes_api":   {"access": true},
		"database_service": {"connect": true},
		"tcp_service":      {"connect": true},
	}
	typeActions := allowed[resourceType]
	if typeActions == nil {
		return false
	}
	for _, action := range actions {
		if !typeActions[action] {
			return false
		}
	}
	return true
}
