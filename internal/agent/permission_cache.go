package agent

import (
	"sync"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// K8SUserPermission 单个用户的 K8S API 权限
type K8SUserPermission struct {
	K8SGroups  []string // Impersonation 分组
	Namespaces []string // 允许的命名空间（空表示全部）
}

// K8SServiceUserPermission 单个用户的 K8S Service 权限
type K8SServiceUserPermission struct {
	Namespaces   []string // 允许的命名空间（空表示全部）
	ServiceNames []string // 允许的 Service 名称（空表示全部）
}

// EndpointSSHUserPermission 单个用户对某 Endpoint 的 SSH 权限
type EndpointSSHUserPermission struct {
	EndpointID   string   // Endpoint ID
	EndpointName string   // Endpoint 名称
	SSHUsers     []string // 允许的 SSH 登录用户名
}

// EndpointK8SAPIUserPermission 单个用户对某 Endpoint 的 K8SAPI 权限
type EndpointK8SAPIUserPermission struct {
	EndpointID   string   // Endpoint ID
	EndpointName string   // Endpoint 名称
	K8SGroups    []string // Impersonation 分组
	Namespaces   []string // 允许的命名空间（空表示全部）
}

// EndpointK8SServiceUserPermission 单个用户对某 Endpoint 的 K8SService 权限
// 已废弃（P10 重构）：统一使用 K8SServiceUserPermission
type EndpointK8SServiceUserPermission struct {
	EndpointID   string   // Endpoint ID
	EndpointName string   // Endpoint 名称
	Namespaces   []string // 允许的命名空间（空表示全部）
	ServiceNames []string // 允许的 Service 名称（空表示全部）
}

// PermissionCache Agent 本地权限缓存
// 从心跳响应中同步，供 K8SAPI 代理和 SVCProxy 鉴权使用
type PermissionCache struct {
	// K8S API 权限：key = user_name
	k8sPermissions map[string]*K8SUserPermission
	k8sMutex       sync.RWMutex

	// K8S Service 权限：key = user_name
	k8sSvcPermissions map[string]*K8SServiceUserPermission
	k8sSvcMutex       sync.RWMutex

	// Endpoint SSH 权限：key = user_name, value = 按 endpoint_name 索引的权限列表
	epSSHPermissions map[string][]*EndpointSSHUserPermission
	epSSHMutex       sync.RWMutex

	// Endpoint K8SAPI 权限：key = user_name
	epK8SAPIPermissions map[string][]*EndpointK8SAPIUserPermission
	epK8SAPIMutex       sync.RWMutex

	// Endpoint K8SService 权限（已废弃，P10 重构）
	// 保留字段以兼容旧代码，但不再使用
	epK8SSvcPermissions map[string][]*EndpointK8SServiceUserPermission
	epK8SSvcMutex       sync.RWMutex
}

// NewPermissionCache 创建权限缓存
func NewPermissionCache() *PermissionCache {
	return &PermissionCache{
		k8sPermissions:      make(map[string]*K8SUserPermission),
		k8sSvcPermissions:   make(map[string]*K8SServiceUserPermission),
		epSSHPermissions:    make(map[string][]*EndpointSSHUserPermission),
		epK8SAPIPermissions: make(map[string][]*EndpointK8SAPIUserPermission),
		epK8SSvcPermissions: make(map[string][]*EndpointK8SServiceUserPermission),
	}
}

// UpdateK8SPermissions 从心跳响应更新 K8S API 权限
func (c *PermissionCache) UpdateK8SPermissions(perms []*pb.K8SPermission) {
	c.k8sMutex.Lock()
	defer c.k8sMutex.Unlock()

	// 全量替换
	newPerms := make(map[string]*K8SUserPermission)
	for _, p := range perms {
		existing, ok := newPerms[p.UserName]
		if ok {
			// 合并同一用户的多条权限（来自不同分组）
			existing.K8SGroups = mergeStringSlice(existing.K8SGroups, p.K8SGroups)
			existing.Namespaces = mergeStringSlice(existing.Namespaces, p.Namespaces)
		} else {
			newPerms[p.UserName] = &K8SUserPermission{
				K8SGroups:  p.K8SGroups,
				Namespaces: p.Namespaces,
			}
		}
	}

	c.k8sPermissions = newPerms
	logger.Debugf("K8S 权限缓存已更新: %d 个用户", len(newPerms))
}

// CheckK8SAccess 检查用户的 K8S API 访问权限
// 返回: k8sGroups（Impersonation 分组）, allowed（是否允许）
func (c *PermissionCache) CheckK8SAccess(userName, namespace string) ([]string, bool) {
	c.k8sMutex.RLock()
	defer c.k8sMutex.RUnlock()

	perm, ok := c.k8sPermissions[userName]
	if !ok {
		return nil, false
	}

	// 检查命名空间权限
	if len(perm.Namespaces) > 0 && namespace != "" {
		allowed := false
		for _, ns := range perm.Namespaces {
			if ns == "*" || ns == namespace {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, false
		}
	}

	return perm.K8SGroups, true
}

// UpdateK8SServicePermissions 从心跳响应更新 K8S Service 权限
func (c *PermissionCache) UpdateK8SServicePermissions(perms []*pb.K8SServicePermission) {
	c.k8sSvcMutex.Lock()
	defer c.k8sSvcMutex.Unlock()

	newPerms := make(map[string]*K8SServiceUserPermission)
	for _, p := range perms {
		existing, ok := newPerms[p.UserName]
		if ok {
			existing.Namespaces = mergeStringSlice(existing.Namespaces, p.Namespaces)
			existing.ServiceNames = mergeStringSlice(existing.ServiceNames, p.ServiceNames)
		} else {
			newPerms[p.UserName] = &K8SServiceUserPermission{
				Namespaces:   p.Namespaces,
				ServiceNames: p.ServiceNames,
			}
		}
	}

	c.k8sSvcPermissions = newPerms
	logger.Debugf("K8SService 权限缓存已更新: %d 个用户", len(newPerms))
}

// CheckK8SServiceAccess 检查用户的 K8S Service 访问权限
func (c *PermissionCache) CheckK8SServiceAccess(userName, namespace, serviceName string) bool {
	c.k8sSvcMutex.RLock()
	defer c.k8sSvcMutex.RUnlock()

	perm, ok := c.k8sSvcPermissions[userName]
	if !ok {
		return false
	}

	// 检查命名空间
	if len(perm.Namespaces) > 0 {
		nsAllowed := false
		for _, ns := range perm.Namespaces {
			if ns == "*" || ns == namespace {
				nsAllowed = true
				break
			}
		}
		if !nsAllowed {
			return false
		}
	}

	// 检查 Service 名称
	if len(perm.ServiceNames) > 0 {
		svcAllowed := false
		for _, sn := range perm.ServiceNames {
			if sn == "*" || sn == serviceName {
				svcAllowed = true
				break
			}
		}
		if !svcAllowed {
			return false
		}
	}

	return true
}

// UpdateEndpointSSHPermissions 从心跳响应更新 Endpoint SSH 权限
func (c *PermissionCache) UpdateEndpointSSHPermissions(perms []*pb.EndpointSSHPermission) {
	c.epSSHMutex.Lock()
	defer c.epSSHMutex.Unlock()

	// 全量替换：key = user_name
	newPerms := make(map[string][]*EndpointSSHUserPermission)
	for _, p := range perms {
		newPerms[p.UserName] = append(newPerms[p.UserName], &EndpointSSHUserPermission{
			EndpointID:   p.EndpointId,
			EndpointName: p.EndpointName,
			SSHUsers:     p.SshUsers,
		})
	}

	c.epSSHPermissions = newPerms
	logger.Debugf("Endpoint SSH 权限缓存已更新: %d 个用户", len(newPerms))
}

// CheckEndpointSSHAccess 检查用户对某 Endpoint 的 SSH 访问权限
// 返回: sshUsers（允许的登录用户名列表）, allowed（是否允许）
func (c *PermissionCache) CheckEndpointSSHAccess(userName, endpointName string) ([]string, bool) {
	c.epSSHMutex.RLock()
	defer c.epSSHMutex.RUnlock()

	perms, ok := c.epSSHPermissions[userName]
	if !ok {
		return nil, false
	}

	// 合并该用户对该 Endpoint 的所有权限（可能来自多条授权记录）
	var allSSHUsers []string
	found := false
	for _, p := range perms {
		if p.EndpointName == endpointName {
			found = true
			allSSHUsers = mergeStringSlice(allSSHUsers, p.SSHUsers)
		}
	}

	if !found {
		return nil, false
	}

	return allSSHUsers, true
}

// UpdateEndpointK8SAPIPermissions 从心跳响应更新 Endpoint K8SAPI 权限
func (c *PermissionCache) UpdateEndpointK8SAPIPermissions(perms []*pb.EndpointK8SAPIPermission) {
	c.epK8SAPIMutex.Lock()
	defer c.epK8SAPIMutex.Unlock()

	newPerms := make(map[string][]*EndpointK8SAPIUserPermission)
	for _, p := range perms {
		newPerms[p.UserName] = append(newPerms[p.UserName], &EndpointK8SAPIUserPermission{
			EndpointID:   p.EndpointId,
			EndpointName: p.EndpointName,
			K8SGroups:    p.K8SGroups,
			Namespaces:   p.Namespaces,
		})
	}

	c.epK8SAPIPermissions = newPerms
	logger.Debugf("Endpoint K8SAPI 权限缓存已更新: %d 个用户", len(newPerms))
}

// CheckEndpointK8SAPIAccess 检查用户对某 Endpoint 的 K8SAPI 访问权限
// 返回: k8sGroups（Impersonation 分组）, allowed（是否允许）
func (c *PermissionCache) CheckEndpointK8SAPIAccess(userName, endpointName string) ([]string, bool) {
	c.epK8SAPIMutex.RLock()
	defer c.epK8SAPIMutex.RUnlock()

	perms, ok := c.epK8SAPIPermissions[userName]
	if !ok {
		return nil, false
	}

	var allK8SGroups []string
	found := false
	for _, p := range perms {
		if p.EndpointName == endpointName {
			found = true
			allK8SGroups = mergeStringSlice(allK8SGroups, p.K8SGroups)
		}
	}

	if !found {
		return nil, false
	}

	return allK8SGroups, true
}

// UpdateEndpointK8SServicePermissions 从心跳响应更新 Endpoint K8SService 权限
// 已废弃（P10 重构）：不再使用 Endpoint 级别权限，统一使用 Agent 级别权限
func (c *PermissionCache) UpdateEndpointK8SServicePermissions(perms []*pb.EndpointK8SServicePermission) {
	// 空实现，保留以兼容旧代码
	logger.Debugf("UpdateEndpointK8SServicePermissions 已废弃（P10 重构），忽略 %d 条权限", len(perms))
}

// CheckEndpointK8SServiceAccess 检查用户对某 Endpoint 的 K8SService 访问权限
// 已废弃（P10 重构）：不再使用，统一使用 CheckK8SServiceAccess
func (c *PermissionCache) CheckEndpointK8SServiceAccess(userName, endpointName, namespace, serviceName string) bool {
	// 兼容旧代码：直接调用 Agent 级别权限检查
	logger.Debugf("CheckEndpointK8SServiceAccess 已废弃（P10 重构），转发到 CheckK8SServiceAccess")
	return c.CheckK8SServiceAccess(userName, namespace, serviceName)
}

// mergeStringSlice 合并两个字符串切片，去重
func mergeStringSlice(a, b []string) []string {
	seen := make(map[string]bool)
	for _, s := range a {
		seen[s] = true
	}
	for _, s := range b {
		seen[s] = true
	}
	result := make([]string, 0, len(seen))
	for s := range seen {
		result = append(result, s)
	}
	return result
}
