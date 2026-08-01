package resourcebusiness

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	_ "modernc.org/sqlite"
)

var compatibilitySchema = []string{
	`CREATE TABLE admin (id INTEGER PRIMARY KEY, username TEXT, role TEXT, enabled INTEGER)`,
	`CREATE TABLE user (id INTEGER PRIMARY KEY, name TEXT, role TEXT, enabled INTEGER, source TEXT)`,
	`CREATE TABLE admin_tenant_membership (id INTEGER PRIMARY KEY, admin_id INTEGER, tenant_id TEXT, role TEXT, enabled INTEGER, expires_at TEXT, permission_revision INTEGER)`,
	`CREATE TABLE tenant (id TEXT PRIMARY KEY, key TEXT, status TEXT)`,
	`CREATE TABLE tenant_membership (id INTEGER PRIMARY KEY, tenant_id TEXT, user_id INTEGER, role TEXT, enabled INTEGER, expires_at TEXT)`,
	`CREATE TABLE "group" (id INTEGER PRIMARY KEY, tenant_id TEXT, name TEXT)`,
	`CREATE TABLE group_member (id INTEGER PRIMARY KEY, group_id INTEGER, user_id INTEGER)`,
	`CREATE TABLE node (id INTEGER PRIMARY KEY, user_id INTEGER, name TEXT, type TEXT, headscale_node_id INTEGER)`,
	`CREATE TABLE endpoint (id TEXT PRIMARY KEY, user_id INTEGER, name TEXT, status TEXT, revoked INTEGER)`,
	`CREATE TABLE resource (id TEXT PRIMARY KEY, tenant_id TEXT, type TEXT, state TEXT, provider_id TEXT, external_workspace_id TEXT, owner_user_id INTEGER, owner_group_id INTEGER, agent_node_id INTEGER, target_revision INTEGER)`,
	`CREATE TABLE resource_target (id INTEGER PRIMARY KEY, resource_id TEXT, revision INTEGER, agent_node_id INTEGER, cluster_id TEXT, namespace TEXT, pod_uid TEXT, container_name TEXT)`,
	`CREATE TABLE workspace_binding (id TEXT PRIMARY KEY, provider_id TEXT, external_tenant_id TEXT, external_workspace_id TEXT, tenant_id TEXT, owner_user_id INTEGER, resource_id TEXT, generation INTEGER, status TEXT)`,
	`CREATE TABLE discovery_candidate (id TEXT PRIMARY KEY, agent_node_id INTEGER, provider_hint TEXT, cluster_id TEXT, namespace TEXT, pod_uid TEXT, container_name TEXT, generation_hint INTEGER, status TEXT, resource_id TEXT)`,
	`CREATE TABLE access_grant (id TEXT PRIMARY KEY, tenant_id TEXT, resource_id TEXT, subject_type TEXT, subject_user_id INTEGER, subject_group_id INTEGER, actions TEXT, status TEXT, revision INTEGER)`,
	`CREATE TABLE acl_group_user_permission (id INTEGER PRIMARY KEY, target_group_id INTEGER, user_id INTEGER)`,
	`CREATE TABLE acl_group_group_permission (id INTEGER PRIMARY KEY, target_group_id INTEGER, group_id INTEGER)`,
	`CREATE TABLE acl_k8s_user_permission (id INTEGER PRIMARY KEY, target_user_id INTEGER, user_id INTEGER, k8s_groups TEXT, namespaces TEXT, enabled INTEGER)`,
	`CREATE TABLE acl_k8s_group_permission (id INTEGER PRIMARY KEY, target_user_id INTEGER, group_id INTEGER, k8s_groups TEXT, namespaces TEXT, enabled INTEGER)`,
	`CREATE TABLE acl_k8s_service_user_permission (id INTEGER PRIMARY KEY, target_user_id INTEGER, user_id INTEGER, namespaces TEXT, service_names TEXT, enabled INTEGER)`,
	`CREATE TABLE acl_k8s_service_group_permission (id INTEGER PRIMARY KEY, target_user_id INTEGER, group_id INTEGER, namespaces TEXT, service_names TEXT, enabled INTEGER)`,
	`CREATE TABLE acl_service_user_permission (id INTEGER PRIMARY KEY, service_id INTEGER, user_id INTEGER)`,
	`CREATE TABLE acl_service_group_permission (id INTEGER PRIMARY KEY, service_id INTEGER, group_id INTEGER)`,
	`CREATE TABLE acl_ssh_user_permission (id INTEGER PRIMARY KEY, target_user_id INTEGER, user_id INTEGER, ssh_users TEXT, enabled INTEGER)`,
	`CREATE TABLE acl_ssh_group_permission (id INTEGER PRIMARY KEY, target_user_id INTEGER, group_id INTEGER, ssh_users TEXT, enabled INTEGER)`,
	`CREATE TABLE acl_user_user_permission (id INTEGER PRIMARY KEY, target_user_id INTEGER, granted_user_id INTEGER)`,
	`CREATE TABLE acl_user_group_permission (id INTEGER PRIMARY KEY, target_user_id INTEGER, group_id INTEGER)`,
	`CREATE TABLE audit_log (id INTEGER PRIMARY KEY, user_id INTEGER, actor_admin_id INTEGER, tenant_id TEXT, request_id TEXT, target_id TEXT, created_at TEXT)`,
	`CREATE TABLE operation_audit_log (id INTEGER PRIMARY KEY, agent_user_id INTEGER, client_user_id INTEGER, endpoint_id TEXT, created_at TEXT, started_at TEXT)`,
	`CREATE TABLE proxy_service (id INTEGER PRIMARY KEY, user_id INTEGER)`,
}

func CreateCompatibilityDatabase(path string, scenario Scenario) (err error) {
	if err := Validate(scenario); err != nil {
		return fmt.Errorf("validate fixture: %w", err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve fixture path: %w", err)
	}
	if _, err := os.Lstat(absPath); err == nil {
		return fmt.Errorf("fixture path already exists: %s", absPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect fixture path: %w", err)
	}
	database, err := sql.Open("sqlite", absPath)
	if err != nil {
		return fmt.Errorf("open fixture database: %w", err)
	}
	succeeded := false
	defer func() {
		closeErr := database.Close()
		if !succeeded {
			_ = os.Remove(absPath)
		}
		if err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	for _, statement := range compatibilitySchema {
		if _, err = database.Exec(statement); err != nil {
			return fmt.Errorf("create compatibility schema: %w", err)
		}
	}
	if err = seedCompatibilityDatabase(database, scenario); err != nil {
		return err
	}
	succeeded = true
	return nil
}

func seedCompatibilityDatabase(database *sql.DB, scenario Scenario) error {
	for _, tenant := range scenario.Tenants {
		if _, err := database.Exec(`INSERT INTO tenant (id, key, status) VALUES (?, ?, 'active')`, tenant.ID, tenant.Key); err != nil {
			return fmt.Errorf("seed tenant: %w", err)
		}
	}
	for _, identity := range scenario.Identities {
		switch identity.Kind {
		case "admin":
			if _, err := database.Exec(`INSERT INTO admin (id, username, role, enabled) VALUES (?, ?, ?, 1)`, identity.ID, identity.NameToken, identity.Role); err != nil {
				return fmt.Errorf("seed admin: %w", err)
			}
		case "agent", "user":
			role := "client"
			if identity.Kind == "agent" {
				role = "agent"
			}
			if _, err := database.Exec(`INSERT INTO user (id, name, role, enabled, source) VALUES (?, ?, ?, 1, 'manual')`, identity.ID, identity.NameToken, role); err != nil {
				return fmt.Errorf("seed user: %w", err)
			}
			if identity.TenantID != "" {
				if _, err := database.Exec(`INSERT INTO tenant_membership (id, tenant_id, user_id, role, enabled, expires_at) VALUES (?, ?, ?, ?, 1, '')`, identity.ID, identity.TenantID, identity.ID, identity.Role); err != nil {
					return fmt.Errorf("seed tenant membership: %w", err)
				}
			}
		}
	}
	agentID, nodeID := firstAgent(scenario), uint64(0)
	if agentID != 0 {
		nodeID = agentID + 10000
		if _, err := database.Exec(`INSERT INTO node (id, user_id, name, type, headscale_node_id) VALUES (?, ?, ?, 'agent', ?)`, nodeID, agentID, "fixture-agent-node", nodeID); err != nil {
			return fmt.Errorf("seed agent node: %w", err)
		}
	}
	resources := map[string]Grant{}
	resourceOrder := []string{}
	for _, grant := range scenario.Grants {
		if grant.Traceable {
			if _, exists := resources[grant.ResourceID]; !exists {
				resourceOrder = append(resourceOrder, grant.ResourceID)
			}
			resources[grant.ResourceID] = grant
		}
	}
	for _, resourceID := range resourceOrder {
		grant := resources[resourceID]
		workload, hasWorkload := firstWorkload(scenario)
		resourceType := "host_ssh"
		if hasWorkload {
			resourceType = "container_ssh"
		}
		if _, err := database.Exec(`INSERT INTO resource (id, tenant_id, type, state, provider_id, external_workspace_id, owner_user_id, agent_node_id, target_revision) VALUES (?, ?, ?, 'available', ?, ?, ?, ?, 1)`,
			resourceID, grant.TenantID, resourceType, firstProviderID(scenario), "workspace-"+resourceID, grant.SubjectID, nodeID); err != nil {
			return fmt.Errorf("seed resource: %w", err)
		}
		if hasWorkload && nodeID != 0 {
			if _, err := database.Exec(`INSERT INTO resource_target (resource_id, revision, agent_node_id, cluster_id, namespace, pod_uid, container_name) VALUES (?, 1, ?, ?, ?, ?, ?)`,
				resourceID, nodeID, workload.ClusterID, workload.Namespace, workload.PodUID, workload.ContainerName); err != nil {
				return fmt.Errorf("seed resource target: %w", err)
			}
		}
		if firstProviderID(scenario) != "" {
			if _, err := database.Exec(`INSERT INTO workspace_binding (id, provider_id, external_tenant_id, external_workspace_id, tenant_id, owner_user_id, resource_id, generation, status) VALUES (?, ?, ?, ?, ?, ?, ?, 1, 'active')`,
				"binding-"+resourceID, firstProviderID(scenario), "external-"+grant.TenantID, "workspace-"+resourceID, grant.TenantID, grant.SubjectID, resourceID); err != nil {
				return fmt.Errorf("seed workspace binding: %w", err)
			}
		}
	}
	for _, grant := range scenario.Grants {
		actions, _ := json.Marshal(grant.Actions)
		if _, err := database.Exec(`INSERT INTO access_grant (id, tenant_id, resource_id, subject_type, subject_user_id, subject_group_id, actions, status, revision) VALUES (?, ?, ?, 'user', ?, 0, ?, 'enabled', 1)`,
			grant.ID, grant.TenantID, grant.ResourceID, grant.SubjectID, string(actions)); err != nil {
			return fmt.Errorf("seed access grant: %w", err)
		}
	}
	for index, workload := range scenario.Workloads {
		if nodeID == 0 {
			break
		}
		resourceID := ""
		if len(resourceOrder) > 0 {
			resourceID = resourceOrder[0]
		}
		if _, err := database.Exec(`INSERT INTO discovery_candidate (id, agent_node_id, provider_hint, cluster_id, namespace, pod_uid, container_name, generation_hint, status, resource_id) VALUES (?, ?, ?, ?, ?, ?, ?, 1, 'observed', ?)`,
			"candidate-"+strconv.Itoa(index+1), nodeID, workload.ProviderID, workload.ClusterID, workload.Namespace, workload.PodUID, workload.ContainerName, resourceID); err != nil {
			return fmt.Errorf("seed discovery candidate: %w", err)
		}
	}
	if scenario.Name == ScenarioLegacyAmbiguity && agentID != 0 && len(scenario.Grants) > 0 {
		if _, err := database.Exec(`INSERT INTO acl_ssh_user_permission (target_user_id, user_id, ssh_users, enabled) VALUES (?, ?, '["root"]', 1)`, agentID, scenario.Grants[0].SubjectID); err != nil {
			return fmt.Errorf("seed legacy ACL: %w", err)
		}
	}
	return nil
}

func firstAgent(scenario Scenario) uint64 {
	for _, identity := range scenario.Identities {
		if identity.Kind == "agent" {
			return identity.ID
		}
	}
	return 0
}

func firstProviderID(scenario Scenario) string {
	if len(scenario.Providers) == 0 {
		return ""
	}
	return scenario.Providers[0].ID
}

func firstWorkload(scenario Scenario) (Workload, bool) {
	if len(scenario.Workloads) == 0 {
		return Workload{}, false
	}
	return scenario.Workloads[0], true
}
