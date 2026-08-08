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

// ContainerSSHUserPermission is a fully resolved, Agent-local target. It is
// intentionally keyed by the stable Resource ID so an SSH client can never
// select a namespace, Pod, container, or command itself.
type ContainerSSHUserPermission struct {
	UserID            uint64
	ResourceID        string
	Namespace         string
	PodName           string
	PodUID            string
	ContainerName     string
	SSHUser           string
	TargetRevision    int64
	GrantRevision     int64
	MaxSessionSeconds int
	ListenPort        uint16
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

	// Container SSH permissions: key = user_name, then stable resource ID.
	// This cache is designed for full replacement on every heartbeat, so an
	// empty snapshot immediately removes access.
	containerSSHPermissions map[string]map[string]*ContainerSSHUserPermission
	containerSSHRoutes      map[uint16]string
	containerSSHMutex       sync.RWMutex
}

// NewPermissionCache 创建权限缓存
func NewPermissionCache() *PermissionCache {
	return &PermissionCache{
		k8sPermissions:          make(map[string]*K8SUserPermission),
		k8sSvcPermissions:       make(map[string]*K8SServiceUserPermission),
		epSSHPermissions:        make(map[string][]*EndpointSSHUserPermission),
		containerSSHPermissions: make(map[string]map[string]*ContainerSSHUserPermission),
		containerSSHRoutes:      make(map[uint16]string),
	}
}

// UpdateContainerSSHPermissions replaces the complete ContainerSSH snapshot.
// The server-side heartbeat projection is added independently; keeping this
// API typed lets the broker be tested without accepting untrusted Pod fields.
func (c *PermissionCache) UpdateContainerSSHPermissions(perms map[string][]*ContainerSSHUserPermission) {
	c.containerSSHMutex.Lock()
	defer c.containerSSHMutex.Unlock()

	next := make(map[string]map[string]*ContainerSSHUserPermission, len(perms))
	routes := make(map[uint16]string)
	conflictedPorts := make(map[uint16]bool)
	for userName, userPerms := range perms {
		byResource := make(map[string]*ContainerSSHUserPermission, len(userPerms))
		for _, perm := range userPerms {
			if perm == nil || perm.ResourceID == "" || perm.Namespace == "" || perm.PodName == "" || perm.PodUID == "" || perm.ContainerName == "" || perm.ListenPort == 0 {
				continue
			}
			copy := *perm
			byResource[copy.ResourceID] = &copy
			if existing, ok := routes[copy.ListenPort]; ok && existing != copy.ResourceID {
				conflictedPorts[copy.ListenPort] = true
			} else {
				routes[copy.ListenPort] = copy.ResourceID
			}
		}
		if len(byResource) > 0 {
			next[userName] = byResource
		}
	}
	for port := range conflictedPorts {
		delete(routes, port)
		logger.Warnf("[PermCache] ContainerSSH 端口快照冲突，拒绝路由: port=%d", port)
	}
	c.containerSSHPermissions = next
	c.containerSSHRoutes = routes
}

// UpdateContainerSSHPermissionsFromProto replaces the complete heartbeat
// snapshot. Empty input intentionally clears all ContainerSSH permissions.
func (c *PermissionCache) UpdateContainerSSHPermissionsFromProto(perms []*pb.ContainerSSHPermission) {
	byUser := make(map[string][]*ContainerSSHUserPermission)
	for _, perm := range perms {
		if perm == nil || perm.UserName == "" {
			continue
		}
		byUser[perm.UserName] = append(byUser[perm.UserName], &ContainerSSHUserPermission{
			UserID:            perm.UserId,
			ResourceID:        perm.ResourceId,
			Namespace:         perm.Namespace,
			PodName:           perm.PodName,
			PodUID:            perm.PodUid,
			ContainerName:     perm.ContainerName,
			TargetRevision:    perm.TargetRevision,
			GrantRevision:     perm.GrantRevision,
			MaxSessionSeconds: int(perm.MaxSessionSeconds),
			ListenPort:        uint16(perm.ListenPort),
		})
	}
	c.UpdateContainerSSHPermissions(byUser)
}

// CheckContainerSSHAccess returns an immutable copy of the resolved target.
func (c *PermissionCache) CheckContainerSSHAccess(userName, resourceID string) (*ContainerSSHUserPermission, bool) {
	c.containerSSHMutex.RLock()
	defer c.containerSSHMutex.RUnlock()

	perm, ok := c.containerSSHPermissions[userName][resourceID]
	if !ok {
		return nil, false
	}
	copy := *perm
	return &copy, true
}

// ResolveContainerSSHRoute resolves only trusted Server snapshot metadata.
// User authorization is checked separately by ContainerExecBroker.
func (c *PermissionCache) ResolveContainerSSHRoute(listenPort uint16) (string, bool) {
	c.containerSSHMutex.RLock()
	defer c.containerSSHMutex.RUnlock()
	resourceID, ok := c.containerSSHRoutes[listenPort]
	return resourceID, ok
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
		logger.Debugf("[PermCache] 添加 K8S 权限: user=%s, groups=%v, namespaces=%v",
			p.UserName, p.K8SGroups, p.Namespaces)
	}

	c.k8sPermissions = newPerms
	logger.Infof("K8S 权限缓存已更新: %d 个用户", len(newPerms))

	// 输出所有用户名，用于调试
	if len(newPerms) > 0 {
		userNames := make([]string, 0, len(newPerms))
		for userName := range newPerms {
			userNames = append(userNames, userName)
		}
		logger.Debugf("[PermCache] K8S 权限用户列表: %v", userNames)
	}
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
