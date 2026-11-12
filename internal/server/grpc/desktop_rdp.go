package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	v1 "github.com/juanfont/headscale/gen/go/headscale/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

const rdpPolicyLeaseDuration = 5 * time.Minute

func (s *DesktopServiceServer) allowLegacyUnauthStream() bool {
	return s.config != nil && s.config.Security.AllowLegacyUnauthenticatedDesktopStream
}

// authenticateDesktopChannel 校验 Desktop 长连接与设备查询凭证：
// 默认严格要求非空且有效的 secret；仅当运维显式开启灰度开关且客户端未携带 secret 时允许降级只读兼容（仍拒绝任何非空错误凭证）。
func (s *DesktopServiceServer) authenticateDesktopChannel(ctx context.Context, desktopID uint64, secret string) (model.Node, model.User, bool, error) {
	if desktopID == 0 {
		return model.Node{}, model.User{}, false, status.Error(codes.InvalidArgument, "缺少设备标识")
	}

	if secret != "" || !s.allowLegacyUnauthStream() {
		node, user, msg, ok := s.VerifyCredential(ctx, desktopID, secret)
		if !ok {
			if msg == "您的账号已被禁用" {
				return model.Node{}, model.User{}, false, status.Error(codes.PermissionDenied, msg)
			}
			// 统一返回 Unauthenticated，防止枚举自增 desktop_id 探测设备是否存在
			return model.Node{}, model.User{}, false, status.Error(codes.Unauthenticated, "设备凭证无效")
		}
		return node, user, true, nil
	}

	// 灰度兼容路径：仅允许旧客户端空 secret 只读接入，不返回任何远程桌面策略与跨账号授权设备
	var node model.Node
	if err := db.DB.WithContext(ctx).First(&node, desktopID).Error; err != nil || node.Type != model.NodeTypeDesktop {
		return model.Node{}, model.User{}, false, status.Error(codes.Unauthenticated, "设备凭证无效")
	}
	var user model.User
	if err := db.DB.WithContext(ctx).First(&user, node.UserID).Error; err != nil {
		return model.Node{}, model.User{}, false, status.Error(codes.Unauthenticated, "设备凭证无效")
	}
	if !user.Enabled {
		return model.Node{}, model.User{}, false, status.Error(codes.PermissionDenied, "您的账号已被禁用")
	}
	return node, user, false, nil
}

func (s *DesktopServiceServer) clearDesktopRuntimeTunnelIP(nodeID uint64) {
	if s.runtimeStore != nil {
		s.runtimeStore.ClearTunnelIP(nodeID)
	}
}

// verifyAndBindDesktopTunnelIP 交叉核验客户端心跳上报的 tunnel_ip 是否与 Headscale 快照中该用户节点真实 IP 一致，并绑定 HeadscaleNodeID。
func (s *DesktopServiceServer) verifyAndBindDesktopTunnelIP(ctx context.Context, nodeID uint64, user model.User, rawTunnelIP string, tunnelConnected bool, now time.Time) (string, bool) {
	reportedIP := strings.TrimSpace(rawTunnelIP)
	if !tunnelConnected {
		reportedIP = ""
	}
	if s.snapshotRefresher == nil {
		return reportedIP, false
	}

	snapshot := s.snapshotRefresher.LoadSnapshot()
	if snapshot == nil || len(snapshot.ByID) == 0 || snapshot.IsExpired(now) {
		return reportedIP, false
	}

	hsUserName := fmt.Sprintf("client-%s", user.Name)
	var nodeName string
	var boundHSID uint64
	if s.runtimeStore != nil {
		if rn, ok := s.runtimeStore.GetNode(nodeID); ok {
			nodeName = rn.Name
			boundHSID = rn.HeadscaleNodeID
		}
	}
	if nodeName == "" {
		var dbNode model.Node
		if err := db.DB.WithContext(ctx).Select("name", "headscale_node_id").First(&dbNode, nodeID).Error; err == nil {
			nodeName = dbNode.Name
			if boundHSID == 0 {
				boundHSID = dbNode.HeadscaleNodeID
			}
		}
	}

	bindID := func(hsID uint64) bool {
		if hsID == 0 || s.runtimeStore == nil {
			return false
		}
		changed := s.runtimeStore.UpdateHeadscaleNodeID(nodeID, hsID)
		if changed && s.runtimePersister != nil {
			s.runtimePersister.NotifyHighPriority()
		}
		return changed
	}

	if tunnelConnected && reportedIP != "" {
		if hsView, found := snapshot.GetByIP(reportedIP); found {
			candidate := &v1.Node{
				Id:          hsView.ID,
				IpAddresses: hsView.IPAddresses,
				User:        &v1.User{Name: hsView.User},
			}
			if validDesktopHeadscaleNode(candidate, hsUserName, reportedIP) {
				return reportedIP, bindID(hsView.ID)
			}
		}

		// 客户端上报的 IP 与 Headscale 快照中该用户名下节点不匹配，拒绝盲信自报 IP，尝试按已绑定 ID 或设备名回退真实 IP
		if boundHSID != 0 {
			if hsView, found := snapshot.GetByID(boundHSID); found && hsView.User == hsUserName && len(hsView.IPAddresses) > 0 {
				logger.Warnf("Desktop 心跳 IP 与 Headscale 快照不符，已回退为已绑定节点真实 IP: nodeID=%d, reported=%s, actual=%s", nodeID, reportedIP, hsView.IPAddresses[0])
				return hsView.IPAddresses[0], false
			}
		}
		if nodeName != "" {
			if hsView, found := snapshot.GetByUserNameAndNodeName(hsUserName, nodeName); found && len(hsView.IPAddresses) > 0 {
				logger.Warnf("Desktop 心跳 IP 与 Headscale 快照不符，已回退为同名节点真实 IP: nodeID=%d, reported=%s, actual=%s", nodeID, reportedIP, hsView.IPAddresses[0])
				return hsView.IPAddresses[0], bindID(hsView.ID)
			}
		}
		logger.Warnf("Desktop 心跳上报未在 Headscale 快照核验通过的 IP，已丢弃: nodeID=%d, user=%s, reported=%s", nodeID, hsUserName, reportedIP)
		s.clearDesktopRuntimeTunnelIP(nodeID)
		return "", false
	}

	if boundHSID == 0 && nodeName != "" {
		if hsView, found := snapshot.GetByUserNameAndNodeName(hsUserName, nodeName); found {
			return "", bindID(hsView.ID)
		}
	}
	return "", false
}

func (s *DesktopServiceServer) loadUserHeadscaleIPIndex(ctx context.Context, userName string) headscaleDeviceIPIndex {
	hsUserName := fmt.Sprintf("client-%s", userName)
	now := time.Now()
	if s.snapshotRefresher != nil {
		snap := s.snapshotRefresher.LoadSnapshot()
		if snap != nil && !snap.IsExpired(now) {
			views := snap.GetByUser(hsUserName)
			if len(views) > 0 {
				raw := make([]*v1.Node, 0, len(views))
				for _, v := range views {
					raw = append(raw, &v1.Node{
						Id:          v.ID,
						GivenName:   v.GivenName,
						Online:      v.Online,
						IpAddresses: v.IPAddresses,
						User:        &v1.User{Name: v.User},
					})
				}
				return buildHeadscaleDeviceIPIndex(raw)
			}
		}
	}
	if s.headscaleClient != nil {
		if hsNodes, err := s.headscaleClient.ListNodesByUser(ctx, hsUserName); err == nil {
			return buildHeadscaleDeviceIPIndex(hsNodes)
		}
	}
	return headscaleDeviceIPIndex{}
}

func (s *DesktopServiceServer) resolveNodeHeadscaleID(ctx context.Context, node model.Node, ownerUserName string, snap *headscale.HeadscaleNodeSnapshot) uint64 {
	hsUserName := fmt.Sprintf("client-%s", ownerUserName)
	var candidateID uint64
	if s.runtimeStore != nil {
		if rn, ok := s.runtimeStore.GetNode(node.ID); ok && rn.HeadscaleNodeID != 0 {
			candidateID = rn.HeadscaleNodeID
		}
	}
	if candidateID == 0 {
		candidateID = node.HeadscaleNodeID
	}
	if snap != nil && len(snap.ByID) > 0 {
		if candidateID != 0 {
			if view, ok := snap.GetByID(candidateID); ok && view.User == hsUserName {
				return view.ID
			}
			// 快照中该 ID 已不属于当前用户，拒绝沿用失效 ID
			candidateID = 0
		}
		if view, ok := snap.GetByUserNameAndNodeName(hsUserName, node.Name); ok && view.User == hsUserName {
			if s.runtimeStore != nil && s.runtimeStore.UpdateHeadscaleNodeID(node.ID, view.ID) && s.runtimePersister != nil {
				s.runtimePersister.NotifyHighPriority()
			}
			return view.ID
		}
		return 0
	}
	if candidateID != 0 {
		return candidateID
	}
	if s.headscaleClient != nil {
		if hsNodes, err := s.headscaleClient.ListNodesByUser(ctx, hsUserName); err == nil {
			index := buildHeadscaleDeviceIPIndex(hsNodes)
			if hsNode := index.byName[node.Name]; hsNode != nil {
				return hsNode.Id
			}
		}
	}
	return 0
}

func (s *DesktopServiceServer) nodeLiveSnapshot(node model.Node) (online bool, lastHeartbeat *time.Time, ip string, rdpLocalReady bool, rdpLocalPort int, rdpTunnelExposed bool) {
	ip = node.IP
	lastHeartbeat = node.LastHeartbeat

	if s.runtimeStore != nil {
		if rn, ok := s.runtimeStore.GetNode(node.ID); ok {
			if !rn.LastHeartbeat.IsZero() && (lastHeartbeat == nil || rn.LastHeartbeat.After(*lastHeartbeat)) {
				hb := rn.LastHeartbeat
				lastHeartbeat = &hb
			}
			if rn.IP != "" {
				ip = rn.IP
			}
			rdpLocalReady = rn.RDPLocalReady
			rdpLocalPort = rn.RDPLocalPort
			rdpTunnelExposed = rn.RDPTunnelExposed
		}
	}

	if s.IsDesktopOnline(node.ID) {
		online = true
	} else if lastHeartbeat != nil && time.Since(*lastHeartbeat) < 90*time.Second {
		online = true
	}
	if !online {
		rdpLocalReady = false
		rdpTunnelExposed = false
	}
	return
}

func (s *DesktopServiceServer) buildSingleDeviceInfo(ctx context.Context, node model.Node, owner model.User, currentNodeID uint64, ipIndex headscaleDeviceIPIndex, isShared bool, grantExpiresUnix int64) *pb.DeviceInfo {
	var sysInfo model.NodeSystemInfo
	osName := "未知"
	arch := "未知"
	hostname := node.Hostname
	if node.SystemInfo != "" {
		if err := json.Unmarshal([]byte(node.SystemInfo), &sysInfo); err == nil {
			if sysInfo.OS != "" {
				osName = sysInfo.OS
			}
			if sysInfo.OSVersion != "" {
				osName = sysInfo.OSVersion
			}
			if sysInfo.Arch != "" {
				arch = sysInfo.Arch
			}
			if sysInfo.Hostname != "" {
				hostname = sysInfo.Hostname
			}
		}
	}

	hostLabel := service.EnsureDesktopNodeHostDomainLabel(ctx, db.DB, &node)
	ownerDomainLabel := service.EffectiveUserDomainLabel(owner)
	deviceDomain := service.BuildPersonalDeviceDomain(ctx, db.DB, node, owner)

	online, lastHb, runtimeIP, rdpLocalReady, rdpLocalPort, rdpTunnelExposed := s.nodeLiveSnapshot(node)
	deviceStatus := "offline"
	if online {
		deviceStatus = "online"
	}

	lastUsedAt := ""
	if lastHb != nil && !lastHb.IsZero() {
		lastUsedAt = lastHb.Format(time.RFC3339)
	}
	createdAt := ""
	if !node.CreatedAt.IsZero() {
		createdAt = node.CreatedAt.Format(time.RFC3339)
	}

	nodeCopy := node
	if runtimeIP != "" {
		nodeCopy.IP = runtimeIP
	}
	ip := resolveDeviceIP(nodeCopy, ipIndex)
	mode := model.NormalizeDesktopRDPMode(node.RDPMode)
	isCurrent := !isShared && node.ID == currentNodeID
	connectable := !isCurrent &&
		deviceStatus == "online" &&
		ip != "" &&
		!node.RDPAdminDisabled &&
		mode != model.DesktopRDPModeDisabled &&
		rdpLocalReady &&
		rdpTunnelExposed

	token := fmt.Sprintf("%d:***", node.ID)
	if isShared {
		token = fmt.Sprintf("shared:%d", node.ID)
	}

	return &pb.DeviceInfo{
		DeviceToken:        token,
		DeviceName:         node.Name,
		Os:                 osName,
		Arch:               arch,
		Hostname:           hostname,
		Status:             deviceStatus,
		LastUsedAt:         lastUsedAt,
		CreatedAt:          createdAt,
		IsCurrent:          isCurrent,
		Ip:                 ip,
		NodeId:             node.ID,
		RdpMode:            mode,
		RdpAllowShare:      node.RDPAllowShare,
		RdpAdminDisabled:   node.RDPAdminDisabled,
		RdpLocalReady:      rdpLocalReady,
		RdpLocalPort:       int32(rdpLocalPort),
		RdpTunnelExposed:   rdpTunnelExposed,
		RdpConnectable:     connectable,
		OwnerUsername:      owner.Name,
		IsShared:           isShared,
		GrantExpiresAtUnix: grantExpiresUnix,
		HostDomainLabel:    hostLabel,
		DeviceDomain:       deviceDomain,
		OwnerDomainLabel:   ownerDomainLabel,
	}
}

// buildDevicesDataWithOptions 构建本人设备列表，并在已认证会话下附加他人跨账号临时授权给我的设备列表
func (s *DesktopServiceServer) buildDevicesDataWithOptions(ctx context.Context, userID, currentNodeID uint64, includeShared bool) []*pb.DeviceInfo {
	var owner model.User
	if err := db.DB.WithContext(ctx).First(&owner, userID).Error; err != nil {
		return nil
	}

	var ownNodes []model.Node
	if err := db.DB.WithContext(ctx).Where("user_id = ? AND type = ?", userID, model.NodeTypeDesktop).Order("id ASC").Find(&ownNodes).Error; err != nil {
		return nil
	}

	ownIPIndex := s.loadUserHeadscaleIPIndex(ctx, owner.Name)
	devices := make([]*pb.DeviceInfo, 0, len(ownNodes))
	for _, n := range ownNodes {
		info := s.buildSingleDeviceInfo(ctx, n, owner, currentNodeID, ownIPIndex, false, 0)
		if !includeShared {
			// 未认证灰度路径隐藏全部远程桌面元数据，防止敏感信息泄露
			info.RdpMode = ""
			info.RdpAllowShare = false
			info.RdpAdminDisabled = false
			info.RdpLocalReady = false
			info.RdpLocalPort = 0
			info.RdpTunnelExposed = false
			info.RdpConnectable = false
		}
		devices = append(devices, info)
	}

	if !includeShared {
		return devices
	}

	// 追加他人显式临时授权给当前用户的 Desktop 设备
	now := time.Now()
	var grants []model.AclDesktopRDPPermission
	if err := db.DB.WithContext(ctx).
		Where("grantee_user_id = ? AND enabled = ? AND revoked_at IS NULL AND expires_at > ?", userID, true, now).
		Order("expires_at DESC").
		Find(&grants).Error; err != nil || len(grants) == 0 {
		return devices
	}

	seenNodes := make(map[uint64]bool, len(grants))
	ipIndexCache := make(map[string]headscaleDeviceIPIndex)

	for _, grant := range grants {
		if seenNodes[grant.TargetNodeID] || grant.OwnerUserID == userID {
			continue
		}
		if !s.areBothActiveTenantMembers(ctx, grant.TenantID, grant.OwnerUserID, userID) {
			continue
		}
		var targetNode model.Node
		if err := db.DB.WithContext(ctx).First(&targetNode, grant.TargetNodeID).Error; err != nil {
			continue
		}
		if targetNode.Type != model.NodeTypeDesktop || targetNode.UserID != grant.OwnerUserID {
			continue
		}
		if !targetNode.RDPAllowShare || targetNode.RDPAdminDisabled || model.NormalizeDesktopRDPMode(targetNode.RDPMode) == model.DesktopRDPModeDisabled {
			continue
		}
		var targetOwner model.User
		if err := db.DB.WithContext(ctx).First(&targetOwner, targetNode.UserID).Error; err != nil || !targetOwner.Enabled {
			continue
		}

		idx, cached := ipIndexCache[targetOwner.Name]
		if !cached {
			idx = s.loadUserHeadscaleIPIndex(ctx, targetOwner.Name)
			ipIndexCache[targetOwner.Name] = idx
		}

		seenNodes[targetNode.ID] = true
		info := s.buildSingleDeviceInfo(ctx, targetNode, targetOwner, currentNodeID, idx, true, grant.ExpiresAt.Unix())
		// 共享视图不向被授权人暴露主人的开关控制状态细节
		info.RdpAllowShare = false
		devices = append(devices, info)
	}

	return devices
}

func (s *DesktopServiceServer) areBothActiveTenantMembers(ctx context.Context, tenantID string, userA, userB uint64) bool {
	if strings.TrimSpace(tenantID) == "" || userA == 0 || userB == 0 {
		return false
	}
	var tenant model.Tenant
	if err := db.DB.WithContext(ctx).Where("id = ? AND status = ?", tenantID, model.TenantStatusActive).First(&tenant).Error; err != nil {
		return false
	}
	now := time.Now().UTC()
	var count int64
	if err := db.DB.WithContext(ctx).Model(&model.TenantMembership{}).
		Where("tenant_id = ? AND user_id IN ? AND enabled = ? AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", tenantID, []uint64{userA, userB}, true, now).
		Count(&count).Error; err != nil {
		return false
	}
	if userA == userB {
		return count >= 1
	}
	return count >= 2
}

// buildInboundRDPPolicy 计算指定被控端 Desktop 的精确入站门禁白名单策略快照
// 核心不变式：仅收录 type=desktop 的实名桌面节点 HeadscaleNodeID，天然排除同账号 CloudIDE 容器客户端与同分组未授权同事。
func (s *DesktopServiceServer) buildInboundRDPPolicy(ctx context.Context, ownerUserID, targetNodeID uint64) *pb.DesktopInboundRDPPolicy {
	now := time.Now()
	policy := &pb.DesktopInboundRDPPolicy{
		EffectiveEnabled: false,
		RdpMode:          model.DesktopRDPModeAuto,
		TargetPort:       3389,
		IssuedAtUnix:     now.Unix(),
		ValidUntilUnix:   now.Add(rdpPolicyLeaseDuration).Unix(),
	}

	var targetNode model.Node
	if err := db.DB.WithContext(ctx).First(&targetNode, targetNodeID).Error; err != nil {
		return policy
	}
	if targetNode.Type != model.NodeTypeDesktop || targetNode.UserID != ownerUserID {
		return policy
	}

	var owner model.User
	if err := db.DB.WithContext(ctx).First(&owner, ownerUserID).Error; err != nil || !owner.Enabled {
		return policy
	}

	mode := model.NormalizeDesktopRDPMode(targetNode.RDPMode)
	policy.RdpMode = mode
	policy.AllowShare = targetNode.RDPAllowShare
	policy.AdminDisabled = targetNode.RDPAdminDisabled

	_, _, _, localReady, localPort, _ := s.nodeLiveSnapshot(targetNode)
	if localPort > 0 {
		policy.TargetPort = int32(localPort)
	}

	if targetNode.RDPAdminDisabled || mode == model.DesktopRDPModeDisabled {
		return policy
	}
	if mode == model.DesktopRDPModeAuto && !localReady {
		return policy
	}

	policy.EffectiveEnabled = true

	var snap *headscale.HeadscaleNodeSnapshot
	if s.snapshotRefresher != nil {
		loaded := s.snapshotRefresher.LoadSnapshot()
		if loaded != nil && !loaded.IsExpired(now) {
			snap = loaded
		}
	}

	seenHSNodeID := make(map[uint64]bool)
	var peers []*pb.AllowedRDPPeer

	// 1. 收录本人名下其他 type=desktop 节点（严格排除 type=agent 或未注册为 desktop 的 CloudIDE 节点）
	var siblingNodes []model.Node
	if err := db.DB.WithContext(ctx).
		Where("user_id = ? AND type = ? AND id <> ?", owner.ID, model.NodeTypeDesktop, targetNode.ID).
		Find(&siblingNodes).Error; err == nil {
		ownerHSUser := fmt.Sprintf("client-%s", owner.Name)
		for _, peerNode := range siblingNodes {
			hsID := s.resolveNodeHeadscaleID(ctx, peerNode, owner.Name, snap)
			if hsID == 0 || seenHSNodeID[hsID] {
				continue
			}
			seenHSNodeID[hsID] = true
			peers = append(peers, &pb.AllowedRDPPeer{
				HeadscaleNodeId:   hsID,
				SourceNodeId:      peerNode.ID,
				SourceUserName:    owner.Name,
				SourceDeviceName:  peerNode.Name,
				GrantId:           0,
				ExpiresAtUnix:     0,
				HeadscaleUserName: ownerHSUser,
			})
		}
	}

	// 2. 收录满足“归属人同意共享 + 同租户有效成员 + 未过期未撤销”的跨账号显式授权 Desktop 节点
	if targetNode.RDPAllowShare {
		var grants []model.AclDesktopRDPPermission
		if err := db.DB.WithContext(ctx).
			Where("target_node_id = ? AND enabled = ? AND revoked_at IS NULL AND expires_at > ?", targetNode.ID, true, now).
			Order("expires_at DESC").
			Find(&grants).Error; err == nil {
			for _, grant := range grants {
				if !grant.IsActiveAt(now) || grant.GranteeUserID == owner.ID {
					continue
				}
				if !s.areBothActiveTenantMembers(ctx, grant.TenantID, owner.ID, grant.GranteeUserID) {
					continue
				}
				var grantee model.User
				if err := db.DB.WithContext(ctx).First(&grantee, grant.GranteeUserID).Error; err != nil || !grantee.Enabled || grantee.Role != model.UserRoleClient {
					continue
				}
				var granteeNodes []model.Node
				if err := db.DB.WithContext(ctx).
					Where("user_id = ? AND type = ?", grantee.ID, model.NodeTypeDesktop).
					Find(&granteeNodes).Error; err != nil {
					continue
				}
				granteeHSUser := fmt.Sprintf("client-%s", grantee.Name)
				for _, gNode := range granteeNodes {
					hsID := s.resolveNodeHeadscaleID(ctx, gNode, grantee.Name, snap)
					if hsID == 0 || seenHSNodeID[hsID] {
						continue
					}
					seenHSNodeID[hsID] = true
					peers = append(peers, &pb.AllowedRDPPeer{
						HeadscaleNodeId:   hsID,
						SourceNodeId:      gNode.ID,
						SourceUserName:    grantee.Name,
						SourceDeviceName:  gNode.Name,
						GrantId:           grant.ID,
						ExpiresAtUnix:     grant.ExpiresAt.Unix(),
						HeadscaleUserName: granteeHSUser,
					})
				}
			}
		}
	}

	policy.AllowedPeers = peers
	return policy
}

// UpdateDeviceRDPConfig 允许已认证 Desktop 修改本人名下任一 Desktop 设备的远程桌面开放模式与共享许可
func (s *DesktopServiceServer) UpdateDeviceRDPConfig(ctx context.Context, req *pb.UpdateDeviceRDPConfigRequest) (*pb.UpdateDeviceRDPConfigResponse, error) {
	currentNode, owner, _, ok := s.VerifyCredential(ctx, req.DesktopId, req.Secret)
	if !ok {
		return &pb.UpdateDeviceRDPConfigResponse{Success: false, Message: "当前设备认证失败"}, nil
	}

	targetNodeID := req.TargetNodeId
	if targetNodeID == 0 && req.DeviceToken != "" {
		if _, err := fmt.Sscanf(req.DeviceToken, "%d:", &targetNodeID); err != nil {
			return &pb.UpdateDeviceRDPConfigResponse{Success: false, Message: "设备令牌格式错误"}, nil
		}
	}
	if targetNodeID == 0 {
		targetNodeID = currentNode.ID
	}

	var targetNode model.Node
	if err := db.DB.WithContext(ctx).First(&targetNode, targetNodeID).Error; err != nil || targetNode.Type != model.NodeTypeDesktop {
		return &pb.UpdateDeviceRDPConfigResponse{Success: false, Message: "目标设备不存在"}, nil
	}
	if targetNode.UserID != currentNode.UserID {
		return &pb.UpdateDeviceRDPConfigResponse{Success: false, Message: "仅允许修改本人归属设备的远程桌面配置"}, nil
	}

	updates := make(map[string]any)
	domainUpdated := false
	if req.UpdateUserDomainLabel {
		if _, err := service.UpdateUserDomainLabel(ctx, db.DB, owner.ID, req.UserDomainLabel); err != nil {
			msg := "更新个人域名标识失败"
			switch err {
			case service.ErrUserDomainLabelInvalid:
				msg = "个人域名标识格式无效（仅支持小写字母、数字与连字符，不能使用系统保留词）"
			case service.ErrUserDomainLabelExists:
				msg = "该个人域名标识已被其他账号或站点占用，请更换"
			}
			return &pb.UpdateDeviceRDPConfigResponse{Success: false, Message: msg}, nil
		}
		_ = db.DB.WithContext(ctx).First(&owner, owner.ID).Error
		domainUpdated = true
	}

	if req.UpdateHostDomainLabel {
		updatedHostLabel, err := service.UpdateDesktopNodeHostDomainLabel(ctx, db.DB, owner.ID, targetNode.ID, req.HostDomainLabel)
		if err != nil {
			msg := "更新主机域名前缀失败"
			switch err {
			case service.ErrHostDomainLabelInvalid:
				msg = "主机域名前缀格式无效（仅支持小写字母、数字与连字符，如 home、work）"
			case service.ErrHostDomainLabelExists:
				msg = "您名下已有另一台设备使用该主机域名前缀，请更换"
			}
			return &pb.UpdateDeviceRDPConfigResponse{Success: false, Message: msg}, nil
		}
		targetNode.HostDomainLabel = updatedHostLabel
		domainUpdated = true
	}

	if rawMode := strings.TrimSpace(req.RdpMode); rawMode != "" {
		switch rawMode {
		case model.DesktopRDPModeAuto, model.DesktopRDPModeEnabled, model.DesktopRDPModeDisabled:
			updates["rdp_mode"] = rawMode
			targetNode.RDPMode = rawMode
		default:
			return &pb.UpdateDeviceRDPConfigResponse{Success: false, Message: "无效的远程桌面开放模式"}, nil
		}
	}

	revokedAnyShare := false
	var affectedGrantees []uint64
	if req.UpdateAllowShare {
		updates["rdp_allow_share"] = req.RdpAllowShare
		targetNode.RDPAllowShare = req.RdpAllowShare
		if !req.RdpAllowShare {
			affectedGrantees = s.collectNodeRDPGranteeUserIDs(ctx, targetNode.ID)
			now := time.Now()
			res := db.DB.WithContext(ctx).Model(&model.AclDesktopRDPPermission{}).
				Where("target_node_id = ? AND enabled = ?", targetNode.ID, true).
				Updates(map[string]any{
					"enabled":       false,
					"revoked_at":    &now,
					"revoke_reason": "设备归属人已关闭共享许可",
				})
			revokedAnyShare = res.RowsAffected > 0
		}
	}

	if len(updates) == 0 && !domainUpdated {
		return &pb.UpdateDeviceRDPConfigResponse{Success: false, Message: "未提供需要更新的配置项"}, nil
	}

	if len(updates) > 0 {
		if err := db.DB.WithContext(ctx).Model(&model.Node{}).Where("id = ?", targetNode.ID).Updates(updates).Error; err != nil {
			return &pb.UpdateDeviceRDPConfigResponse{Success: false, Message: "保存配置失败"}, nil
		}
	}
	if s.runtimeStore != nil {
		s.runtimeStore.UpsertNode(&targetNode)
	}

	if req.UpdateUserDomainLabel {
		var ownerActiveGrantees []uint64
		_ = db.DB.WithContext(ctx).Model(&model.AclDesktopRDPPermission{}).
			Where("owner_user_id = ? AND enabled = ?", owner.ID, true).
			Distinct("grantee_user_id").
			Pluck("grantee_user_id", &ownerActiveGrantees).Error
		affectedGrantees = append(affectedGrantees, ownerActiveGrantees...)
	}

	ipIndex := s.loadUserHeadscaleIPIndex(ctx, owner.Name)
	deviceInfo := s.buildSingleDeviceInfo(ctx, targetNode, owner, currentNode.ID, ipIndex, false, 0)

	go func() {
		bgCtx := context.Background()
		s.NotifyDesktopDataChange(targetNode.ID, pb.DesktopDataType_DESKTOP_DATA_TYPE_RDP_POLICY)
		s.NotifyRelatedDesktopDevicesChange(bgCtx, owner.ID, targetNode.ID)
		seenGrantee := make(map[uint64]bool)
		for _, granteeID := range affectedGrantees {
			if granteeID == 0 || granteeID == owner.ID || seenGrantee[granteeID] {
				continue
			}
			seenGrantee[granteeID] = true
			s.NotifyUserDesktopsDataChange(granteeID, pb.DesktopDataType_DESKTOP_DATA_TYPE_DEVICES)
		}
		if revokedAnyShare && s.aclSyncTrigger != nil {
			s.aclSyncTrigger(bgCtx)
		}
	}()

	logger.Infof("Desktop %d 更新设备 %d 配置成功: mode=%s, allow_share=%v, host_label=%s, user_label=%s",
		currentNode.ID, targetNode.ID, targetNode.RDPMode, targetNode.RDPAllowShare, targetNode.HostDomainLabel, owner.DomainLabel)
	return &pb.UpdateDeviceRDPConfigResponse{
		Success:         true,
		Message:         "设置已更新",
		Device:          deviceInfo,
		DomainSuffix:    service.EffectiveDomainSuffix(ctx, db.DB),
		UserDomainLabel: service.EffectiveUserDomainLabel(owner),
	}, nil
}

// ReportRDPSessionAudit 接收被控端上报的远程桌面入站连接建立、结束与拒绝审计记录
func (s *DesktopServiceServer) ReportRDPSessionAudit(ctx context.Context, req *pb.ReportRDPSessionAuditRequest) (*pb.ReportRDPSessionAuditResponse, error) {
	targetNode, _, _, ok := s.VerifyCredential(ctx, req.DesktopId, req.Secret)
	if !ok {
		return &pb.ReportRDPSessionAuditResponse{Success: false, Message: "设备认证失败"}, nil
	}

	eventType := strings.TrimSpace(req.EventType)
	switch eventType {
	case model.RDPSessionEventStarted, model.RDPSessionEventEnded, model.RDPSessionEventRejected:
	default:
		return &pb.ReportRDPSessionAuditResponse{Success: false, Message: "无效的审计事件类型"}, nil
	}

	sessionUUID := strings.TrimSpace(req.SessionUuid)
	if sessionUUID == "" {
		return &pb.ReportRDPSessionAuditResponse{Success: false, Message: "缺少会话标识"}, nil
	}

	occurredAt := time.Now()
	if req.TimestampUnix > 0 {
		occurredAt = time.Unix(req.TimestampUnix, 0)
	}

	record := model.DesktopRDPSessionAudit{
		SessionUUID:           sessionUUID,
		TargetNodeID:          targetNode.ID,
		OwnerUserID:           targetNode.UserID,
		EventType:             eventType,
		SourceHeadscaleNodeID: req.SourceHeadscaleNodeId,
		SourceNodeID:          req.SourceNodeId,
		SourceUserName:        strings.TrimSpace(req.SourceUserName),
		SourceDeviceName:      strings.TrimSpace(req.SourceDeviceName),
		SourceIP:              strings.TrimSpace(req.SourceIp),
		GrantID:               req.GrantId,
		CloseReason:           strings.TrimSpace(req.CloseReason),
		DurationMs:            req.DurationMs,
		OccurredAt:            occurredAt,
	}
	if err := db.DB.WithContext(ctx).Create(&record).Error; err != nil {
		logger.Warnf("写入 Desktop RDP 会话审计失败: targetNode=%d, err=%v", targetNode.ID, err)
		return &pb.ReportRDPSessionAuditResponse{Success: false, Message: "写入审计记录失败"}, nil
	}

	return &pb.ReportRDPSessionAuditResponse{Success: true, Message: "已记录"}, nil
}

// NotifyUserDesktopsDataChange 向指定用户名下所有在线 Desktop 推送指定类型快照
func (s *DesktopServiceServer) NotifyUserDesktopsDataChange(userID uint64, dataType pb.DesktopDataType) {
	if userID == 0 {
		return
	}
	s.dataStreamMutex.RLock()
	streams := make([]*DesktopDataStream, 0)
	for _, ds := range s.dataStreams {
		if ds.UserID == userID {
			streams = append(streams, ds)
		}
	}
	s.dataStreamMutex.RUnlock()

	ctx := context.Background()
	for _, ds := range streams {
		if err := s.sendDataSnapshotToStream(ctx, ds, dataType); err != nil {
			logger.Warnf("推送用户 %d Desktop %d 数据变更(%v)失败: %v", userID, ds.NodeID, dataType, err)
		}
	}
}

func (s *DesktopServiceServer) collectNodeRDPGranteeUserIDs(ctx context.Context, targetNodeID uint64) []uint64 {
	if targetNodeID == 0 {
		return nil
	}
	var granteeIDs []uint64
	_ = db.DB.WithContext(ctx).Model(&model.AclDesktopRDPPermission{}).
		Where("target_node_id = ? AND enabled = ?", targetNodeID, true).
		Distinct("grantee_user_id").
		Pluck("grantee_user_id", &granteeIDs).Error
	return granteeIDs
}

// NotifyRelatedDesktopDevicesChange 当某台 Desktop 在线态、HeadscaleNodeID 或 RDP 状态变化时，
// 同步刷新本人所有在线 Desktop（设备列表 + 被控策略）以及跨账号被授权人或授权目标的实时视图。
func (s *DesktopServiceServer) NotifyRelatedDesktopDevicesChange(ctx context.Context, userID, changedNodeID uint64) {
	// 该函数多以 fire-and-forget 协程调用：任何异常都不得拖垮进程（进程崩溃会连带断开全部 Agent）
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("推送 Desktop 关联设备变更异常已恢复: userID=%d, nodeID=%d, err=%v", userID, changedNodeID, r)
		}
	}()
	if db.DB == nil {
		return
	}
	s.NotifyUserDesktopsDataChange(userID, pb.DesktopDataType_DESKTOP_DATA_TYPE_DEVICES)
	s.NotifyUserDesktopsDataChange(userID, pb.DesktopDataType_DESKTOP_DATA_TYPE_RDP_POLICY)

	// 若当前设备作为被控端存在有效跨账号授权，刷新被授权人的设备列表
	for _, granteeID := range s.collectNodeRDPGranteeUserIDs(ctx, changedNodeID) {
		if granteeID != userID {
			s.NotifyUserDesktopsDataChange(granteeID, pb.DesktopDataType_DESKTOP_DATA_TYPE_DEVICES)
		}
	}

	// 若当前用户作为被授权人新上线并绑定了 HeadscaleNodeID，同步刷新其获授权的目标被控端白名单
	now := time.Now()
	var targetNodeIDs []uint64
	if err := db.DB.WithContext(ctx).Model(&model.AclDesktopRDPPermission{}).
		Where("grantee_user_id = ? AND enabled = ? AND revoked_at IS NULL AND expires_at > ?", userID, true, now).
		Distinct("target_node_id").
		Pluck("target_node_id", &targetNodeIDs).Error; err == nil {
		for _, targetID := range targetNodeIDs {
			s.NotifyDesktopDataChange(targetID, pb.DesktopDataType_DESKTOP_DATA_TYPE_RDP_POLICY)
		}
	}
}
