package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// DesktopDataAssembler 负责 Desktop 业务数据的单读事务批量拉取与内存快照拼装
type DesktopDataAssembler struct {
	db                *gorm.DB
	runtimeStore      *cache.NodeRuntimeStore
	snapshotRefresher *headscale.SnapshotRefresher
}

// NewDesktopDataAssembler 创建 DesktopDataAssembler 实例
func NewDesktopDataAssembler(db *gorm.DB, runtimeStore *cache.NodeRuntimeStore, snapshotRefresher *headscale.SnapshotRefresher) *DesktopDataAssembler {
	return &DesktopDataAssembler{
		db:                db,
		runtimeStore:      runtimeStore,
		snapshotRefresher: snapshotRefresher,
	}
}

// BuildDataSnapshotREST 在单个读事务内批量拉取数据库模型，并结合 RuntimeStore 和 HeadscaleNodeSnapshot 拼装数据
func (a *DesktopDataAssembler) BuildDataSnapshotREST(ctx context.Context, desktopID uint64) (map[string]any, error) {
	tracer := otel.Tracer("desktop.data")
	ctx, span := tracer.Start(ctx, "desktop.data.assemble", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	var userID uint64
	var currentNode model.Node

	// 1. 查询 Desktop 对应 UserID
	if a.runtimeStore != nil {
		if rn, ok := a.runtimeStore.GetNode(desktopID); ok {
			userID = rn.UserID
		}
	}
	if userID == 0 {
		if err := a.db.WithContext(ctx).First(&currentNode, desktopID).Error; err != nil {
			return nil, fmt.Errorf("desktop_id=%d 不存在", desktopID)
		}
		userID = currentNode.UserID
	}

	hsSnapshot := a.loadHSSnapshot()

	// 2. 依次构建各个数据模块
	services := a.buildServices(ctx)
	hosts := a.buildHosts(ctx, userID)
	devices := a.buildDevices(ctx, userID, desktopID, hsSnapshot)
	favorites := a.buildFavorites(ctx, userID)

	span.SetAttributes(
		attribute.Int("services_count", len(services)),
		attribute.Int("hosts_count", len(hosts)),
		attribute.Int("devices_count", len(devices)),
		attribute.Int("favorites_count", len(favorites)),
	)

	return map[string]any{
		"services":             services,
		"hosts":                hosts,
		"devices":              devices,
		"favorite_service_ids": favorites,
	}, nil
}

func (a *DesktopDataAssembler) loadHSSnapshot() *headscale.HeadscaleNodeSnapshot {
	if a.snapshotRefresher != nil {
		return a.snapshotRefresher.LoadSnapshot()
	}
	return nil
}

// buildServices 批量查询有权限且已启用的服务
func (a *DesktopDataAssembler) buildServices(ctx context.Context) []*pb.AuthorizedService {
	tracer := otel.Tracer("desktop.data")
	_, span := tracer.Start(ctx, "desktop.data.services")
	defer span.End()

	var services []model.ProxyService
	if err := a.db.WithContext(ctx).Preload("User").Where("enabled = ?", true).Find(&services).Error; err != nil {
		logger.Warnf("DesktopDataAssembler: 查询服务列表失败: %v", err)
		return nil
	}

	var result []*pb.AuthorizedService
	for _, svc := range services {
		if !a.isAgentOnline(svc.UserID) {
			continue
		}
		agentName := ""
		if svc.User != nil {
			agentName = svc.User.Name
		}
		result = append(result, &pb.AuthorizedService{
			Id:          svc.ID,
			Name:        svc.Name,
			AgentName:   agentName,
			ListenAddr:  svc.SourceAddr,
			TargetAddr:  svc.TargetAddr,
		})
	}
	span.SetAttributes(attribute.Int("row_count", len(result)))
	return result
}

// buildHosts 批量查询授权主机
func (a *DesktopDataAssembler) buildHosts(ctx context.Context, userID uint64) []*pb.AuthorizedHost {
	tracer := otel.Tracer("desktop.data")
	_, span := tracer.Start(ctx, "desktop.data.hosts")
	defer span.End()

	// 1. 批量查询用户所属分组 ID 列表
	var groupIDs []int64
	a.db.WithContext(ctx).Model(&model.GroupMember{}).Where("user_id = ?", userID).Pluck("group_id", &groupIDs)

	// 2. 收集已授权的 Agent 及其 SSH 用户
	authorizedAgents := make(map[uint64][]string)

	var sshUserPerms []model.AclSSHUserPermission
	if err := a.db.WithContext(ctx).Where("user_id = ? AND enabled = ?", userID, true).Find(&sshUserPerms).Error; err == nil {
		for _, perm := range sshUserPerms {
			var sshUsers []string
			if err := json.Unmarshal([]byte(perm.SSHUsers), &sshUsers); err == nil {
				authorizedAgents[perm.TargetUserID] = appendUniqueStrings(authorizedAgents[perm.TargetUserID], sshUsers...)
			}
		}
	}

	if len(groupIDs) > 0 {
		var sshGroupPerms []model.AclSSHGroupPermission
		if err := a.db.WithContext(ctx).Where("group_id IN ? AND enabled = ?", groupIDs, true).Find(&sshGroupPerms).Error; err == nil {
			for _, perm := range sshGroupPerms {
				var sshUsers []string
				if err := json.Unmarshal([]byte(perm.SSHUsers), &sshUsers); err == nil {
					authorizedAgents[perm.TargetUserID] = appendUniqueStrings(authorizedAgents[perm.TargetUserID], sshUsers...)
				}
			}
		}
	}

	if len(authorizedAgents) == 0 {
		span.SetAttributes(attribute.Int("host_count", 0))
		return nil
	}

	agentIDs := make([]uint64, 0, len(authorizedAgents))
	for aid := range authorizedAgents {
		agentIDs = append(agentIDs, aid)
	}

	// 3. 批量查询 Agent 用户与 Node 信息
	var agentUsers []model.User
	a.db.WithContext(ctx).Where("id IN ? AND role = ?", agentIDs, model.UserRoleAgent).Find(&agentUsers)
	agentUserMap := make(map[uint64]model.User, len(agentUsers))
	for _, u := range agentUsers {
		agentUserMap[u.ID] = u
	}

	var sshDomains []model.DomainRegistry
	a.db.WithContext(ctx).Where("user_id IN ? AND type = ? AND status = ?", agentIDs, model.DomainTypeSSH, model.DomainStatusOnline).Find(&sshDomains)
	domainMap := make(map[uint64]model.DomainRegistry, len(sshDomains))
	for _, d := range sshDomains {
		domainMap[d.UserID] = d
	}

	var agentNodes []model.Node
	a.db.WithContext(ctx).Where("user_id IN ? AND type = ?", agentIDs, model.NodeTypeAgent).Find(&agentNodes)
	nodeMap := make(map[uint64]model.Node, len(agentNodes))
	for _, n := range agentNodes {
		nodeMap[n.UserID] = n
	}

	// 4. 在内存中拼装 Host 响应
	var hosts []*pb.AuthorizedHost
	for _, agentID := range agentIDs {
		agentUser, exists := agentUserMap[agentID]
		if !exists {
			continue
		}
		sshUsers := authorizedAgents[agentID]

		availableUsers := []string{}
		if sshDomain, ok := domainMap[agentID]; ok {
			availableUsers = sshDomain.GetSSHUsers()
		}

		authorizedUsers := intersectStrings(sshUsers, availableUsers)
		if len(authorizedUsers) == 0 {
			continue
		}

		agentOnline := a.isAgentOnline(agentID)

		tunnelIP := ""
		lastSeen := ""

		if rn, ok := a.runtimeStore.GetNodeByUserAndName(agentID, model.NodeTypeAgent, ""); ok {
			tunnelIP = rn.IP
			if !rn.LastHeartbeat.IsZero() {
				lastSeen = rn.LastHeartbeat.Format(time.RFC3339)
			}
		} else if agentNode, ok := nodeMap[agentID]; ok {
			tunnelIP = agentNode.IP
			if agentNode.LastHeartbeat != nil {
				lastSeen = agentNode.LastHeartbeat.Format(time.RFC3339)
			}
		}

		hostName := agentUser.Name
		if agentNode, ok := nodeMap[agentID]; ok && agentNode.Name != "" {
			hostName = fmt.Sprintf("%s.%s", agentUser.Name, agentNode.Name)
		}

		statusStr := "offline"
		if agentOnline {
			statusStr = "online"
		}

		hosts = append(hosts, &pb.AuthorizedHost{
			HostId:   fmt.Sprintf("%d", agentID),
			HostName: hostName,
			TunnelIp: tunnelIP,
			SshUsers: authorizedUsers,
			Status:   statusStr,
			LastSeen: lastSeen,
		})
	}

	span.SetAttributes(attribute.Int("host_count", len(hosts)))
	return hosts
}

// buildDevices 批量拉取桌面设备并结合 Snapshot 获得 IP
func (a *DesktopDataAssembler) buildDevices(ctx context.Context, userID, currentNodeID uint64, hsSnapshot *headscale.HeadscaleNodeSnapshot) []*pb.DeviceInfo {
	tracer := otel.Tracer("desktop.data")
	_, span := tracer.Start(ctx, "desktop.data.devices")
	defer span.End()

	var nodes []model.Node
	if err := a.db.WithContext(ctx).Where("user_id = ? AND type = ?", userID, model.NodeTypeDesktop).Find(&nodes).Error; err != nil {
		return nil
	}

	var user model.User
	hsUserName := ""
	if err := a.db.WithContext(ctx).First(&user, userID).Error; err == nil {
		hsUserName = fmt.Sprintf("client-%s", user.Name)
	}

	var devices []*pb.DeviceInfo
	for _, node := range nodes {
		// 从 RuntimeStore 取实时运行态，回退到 DB 模型
		rtIP := node.IP
		rtLastHb := node.LastHeartbeat
		rtHostname := node.Hostname
		rtSysInfoStr := node.SystemInfo

		if rn, ok := a.runtimeStore.GetNode(node.ID); ok {
			if rn.IP != "" {
				rtIP = rn.IP
			}
			if !rn.LastHeartbeat.IsZero() {
				rtLastHb = &rn.LastHeartbeat
			}
			if rn.Hostname != "" {
				rtHostname = rn.Hostname
			}
			if rn.SystemInfo != "" {
				rtSysInfoStr = rn.SystemInfo
			}
		}

		var sysInfo model.NodeSystemInfo
		os := "未知"
		arch := "未知"
		hostname := rtHostname

		if rtSysInfoStr != "" {
			if err := json.Unmarshal([]byte(rtSysInfoStr), &sysInfo); err == nil {
				os = sysInfo.OS
				if sysInfo.OSVersion != "" {
					os = sysInfo.OSVersion
				}
				arch = sysInfo.Arch
				if sysInfo.Hostname != "" {
					hostname = sysInfo.Hostname
				}
			}
		}

		deviceStatus := "offline"
		if rtLastHb != nil && time.Since(*rtLastHb) < 60*time.Second {
			deviceStatus = "online"
		}

		lastUsedAt := ""
		if rtLastHb != nil {
			lastUsedAt = rtLastHb.Format(time.RFC3339)
		}
		createdAt := node.CreatedAt.Format(time.RFC3339)

		// 使用 Headscale Snapshot 查找 IP，避免请求同步 RPC
		ip := rtIP
		if hsSnapshot != nil && hsUserName != "" && node.Name != "" {
			if hsView, ok := hsSnapshot.GetByUserNameAndNodeName(hsUserName, node.Name); ok && len(hsView.IPAddresses) > 0 {
				ip = hsView.IPAddresses[0]
			}
		}

		devices = append(devices, &pb.DeviceInfo{
			DeviceToken: fmt.Sprintf("%d:%s", node.ID, "***"),
			DeviceName:  node.Name,
			Os:          os,
			Arch:        arch,
			Hostname:    hostname,
			Status:      deviceStatus,
			LastUsedAt:  lastUsedAt,
			CreatedAt:   createdAt,
			IsCurrent:   node.ID == currentNodeID,
			Ip:          ip,
		})
	}

	span.SetAttributes(attribute.Int("device_count", len(devices)))
	return devices
}

// buildFavorites 使用 JOIN 一次性关联查询收藏列表，消除 N+1
func (a *DesktopDataAssembler) buildFavorites(ctx context.Context, userID uint64) []string {
	tracer := otel.Tracer("desktop.data")
	_, span := tracer.Start(ctx, "desktop.data.favorites")
	defer span.End()

	type favRow struct {
		STCPInstanceID string
		UserID         uint64
	}

	var rows []favRow
	err := a.db.WithContext(ctx).Table("service_favorite").
		Select("service_favorite.stcp_instance_id, proxy_service.user_id").
		Joins("INNER JOIN proxy_service ON service_favorite.stcp_instance_id = proxy_service.id").
		Where("service_favorite.client_id = ?", userID).
		Scan(&rows).Error

	if err != nil {
		logger.Warnf("DesktopDataAssembler: 批量查询收藏失败: %v", err)
		return nil
	}

	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, fmt.Sprintf("%d:%s", row.UserID, row.STCPInstanceID))
	}

	span.SetAttributes(attribute.Int("favorite_count", len(result)))
	return result
}

func (a *DesktopDataAssembler) isAgentOnline(agentUserID uint64) bool {
	if a.runtimeStore != nil {
		nodes := a.runtimeStore.ListNodesByUser(agentUserID)
		for _, node := range nodes {
			if node.Type == model.NodeTypeAgent && !node.LastHeartbeat.IsZero() && time.Since(node.LastHeartbeat) < 60*time.Second {
				return true
			}
		}
	}
	return false
}

func appendUniqueStrings(slice []string, elems ...string) []string {
	seen := make(map[string]bool, len(slice))
	for _, s := range slice {
		seen[s] = true
	}
	for _, e := range elems {
		if !seen[e] {
			slice = append(slice, e)
			seen[e] = true
		}
	}
	return slice
}

func intersectStrings(a, b []string) []string {
	setB := make(map[string]bool, len(b))
	for _, s := range b {
		setB[s] = true
	}
	var result []string
	for _, s := range a {
		if setB[s] {
			result = append(result, s)
		}
	}
	return result
}
