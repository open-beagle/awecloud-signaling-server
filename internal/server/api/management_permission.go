package api

import "github.com/open-beagle/awecloud-signaling-server/internal/server/model"

const (
	PermissionTenantOverviewRead       = "tenant.overview.read"
	PermissionTenantMembersRead        = "tenant.members.read"
	PermissionTenantMembersWrite       = "tenant.members.write"
	PermissionTenantGroupsRead         = "tenant.groups.read"
	PermissionTenantGroupsWrite        = "tenant.groups.write"
	PermissionTenantDevicesRead        = "tenant.devices.read"
	PermissionTenantResourcesRead      = "tenant.resources.read"
	PermissionTenantResourcesWrite     = "tenant.resources.write"
	PermissionTenantGrantsRead         = "tenant.grants.read"
	PermissionTenantGrantsWrite        = "tenant.grants.write"
	PermissionTenantSessionsRead       = "tenant.sessions.read"
	PermissionTenantSessionsDisconnect = "tenant.sessions.disconnect"
	PermissionTenantAuditRead          = "tenant.audit.read"
	PermissionTenantSettingsRead       = "tenant.settings.read"
	PermissionTenantSettingsWrite      = "tenant.settings.write"
)

var tenantAdminPermissions = []string{
	PermissionTenantOverviewRead,
	PermissionTenantMembersRead,
	PermissionTenantMembersWrite,
	PermissionTenantGroupsRead,
	PermissionTenantGroupsWrite,
	PermissionTenantDevicesRead,
	PermissionTenantResourcesRead,
	PermissionTenantResourcesWrite,
	PermissionTenantGrantsRead,
	PermissionTenantGrantsWrite,
	PermissionTenantSessionsRead,
	PermissionTenantSessionsDisconnect,
	PermissionTenantAuditRead,
	PermissionTenantSettingsRead,
	PermissionTenantSettingsWrite,
}

var tenantSecurityAuditorPermissions = []string{
	PermissionTenantOverviewRead,
	PermissionTenantResourcesRead,
	PermissionTenantSessionsRead,
	PermissionTenantAuditRead,
}

var tenantViewerPermissions = []string{
	PermissionTenantOverviewRead,
	PermissionTenantMembersRead,
	PermissionTenantGroupsRead,
	PermissionTenantDevicesRead,
	PermissionTenantResourcesRead,
	PermissionTenantGrantsRead,
	PermissionTenantSessionsRead,
	PermissionTenantAuditRead,
	PermissionTenantSettingsRead,
}

func permissionsForTenantRole(role string) ([]string, model.TenantManagementRole, bool) {
	normalized := model.NormalizeTenantManagementRole(role)
	var source []string
	switch normalized {
	case model.TenantManagementRoleAdmin:
		source = tenantAdminPermissions
	case model.TenantManagementRoleSecurityAuditor:
		source = tenantSecurityAuditorPermissions
	case model.TenantManagementRoleViewer:
		source = tenantViewerPermissions
	default:
		return nil, "", false
	}
	return append([]string(nil), source...), normalized, true
}

func tenantRoleHasPermission(role, permission string) (model.TenantManagementRole, bool) {
	permissions, normalized, valid := permissionsForTenantRole(role)
	if !valid {
		return "", false
	}
	for _, candidate := range permissions {
		if candidate == permission {
			return normalized, true
		}
	}
	return normalized, false
}

func tenantPermissionIsWrite(permission string) bool {
	switch permission {
	case PermissionTenantMembersWrite,
		PermissionTenantGroupsWrite,
		PermissionTenantResourcesWrite,
		PermissionTenantGrantsWrite,
		PermissionTenantSessionsDisconnect,
		PermissionTenantSettingsWrite:
		return true
	default:
		return false
	}
}
