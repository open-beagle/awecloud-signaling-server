package service

import (
	"context"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

const (
	// HeartbeatTimeout 心跳超时时间（60秒）
	HeartbeatTimeout = 60 * time.Second
)

// DomainStatusService 域名状态判断服务
type DomainStatusService struct {
	headscaleClient *headscale.Client
}

// NewDomainStatusService 创建域名状态判断服务
func NewDomainStatusService(headscaleClient *headscale.Client) *DomainStatusService {
	return &DomainStatusService{
		headscaleClient: headscaleClient,
	}
}

// GetDomainStatus 获取域名状态
// 根据 node_id 或 endpoint_id 从内存缓存判断状态
func (s *DomainStatusService) GetDomainStatus(ctx context.Context, domain *model.DomainRegistry) model.DomainStatus {
	// 场景 1：Node 域名（node_id > 0 且 endpoint_id 为空）
	if domain.NodeID > 0 && domain.EndpointID == "" {
		return s.getNodeDomainStatus(ctx, domain)
	}

	// 场景 2：Endpoint 域名（endpoint_id 不为空）
	if domain.EndpointID != "" {
		return s.getEndpointDomainStatus(domain)
	}

	// 场景 3：无法判断（node_id=0 且 endpoint_id 为空）
	logger.Warnf("域名无法判断状态: domain=%s, node_id=%d, endpoint_id=%s",
		domain.Domain, domain.NodeID, domain.EndpointID)
	return model.DomainStatusOffline
}

// getNodeDomainStatus 获取 Node 域名状态
func (s *DomainStatusService) getNodeDomainStatus(ctx context.Context, domain *model.DomainRegistry) model.DomainStatus {
	// 查询 NodeStatusCache
	nodeStatus, exists := cache.GetNodeStatus(domain.NodeID)
	if !exists {
		// 缓存不存在 → offline（已断连）
		logger.Debugf("Node 缓存不存在，判断为 offline: domain=%s, node_id=%d",
			domain.Domain, domain.NodeID)
		return model.DomainStatusOffline
	}

	// 计算心跳时间差
	timeSinceHeartbeat := time.Since(nodeStatus.LastHeartbeat)

	// 心跳正常（< 60秒）→ online
	if timeSinceHeartbeat < HeartbeatTimeout {
		logger.Debugf("Node 心跳正常，判断为 online: domain=%s, node_id=%d, last_heartbeat=%v",
			domain.Domain, domain.NodeID, nodeStatus.LastHeartbeat)
		return model.DomainStatusOnline
	}

	// 心跳超时（>= 60秒）→ 查询 Headscale 验证
	if s.headscaleClient != nil && nodeStatus.TunnelIP != "" {
		logger.Infof("Node 心跳超时，查询 Headscale 验证: domain=%s, node_id=%d, tunnel_ip=%s, time_since=%v",
			domain.Domain, domain.NodeID, nodeStatus.TunnelIP, timeSinceHeartbeat)

		hsNode, err := s.headscaleClient.GetNodeByIP(ctx, nodeStatus.TunnelIP)
		if err != nil {
			logger.Warnf("查询 Headscale 失败，降级为 offline: domain=%s, node_id=%d, err=%v",
				domain.Domain, domain.NodeID, err)
			// 查询失败，清理缓存
			cache.DeleteNodeStatus(domain.NodeID)
			return model.DomainStatusOffline
		}

		if hsNode != nil && hsNode.Online {
			// Headscale 显示在线，更新缓存
			logger.Infof("Headscale 显示在线，更新缓存: domain=%s, node_id=%d",
				domain.Domain, domain.NodeID)
			cache.SetNodeStatus(domain.NodeID, cache.NodeStatus{
				NodeID:        domain.NodeID,
				UserID:        nodeStatus.UserID,
				TunnelIP:      nodeStatus.TunnelIP,
				LastHeartbeat: time.Now(),
			})
			return model.DomainStatusOnline
		}

		// Headscale 显示离线，清理缓存
		logger.Infof("Headscale 显示离线，清理缓存: domain=%s, node_id=%d",
			domain.Domain, domain.NodeID)
		cache.DeleteNodeStatus(domain.NodeID)
		return model.DomainStatusOffline
	}

	// 无法查询 Headscale，降级为 offline
	logger.Warnf("无法查询 Headscale，降级为 offline: domain=%s, node_id=%d",
		domain.Domain, domain.NodeID)
	return model.DomainStatusOffline
}

// getEndpointDomainStatus 获取 Endpoint 域名状态
func (s *DomainStatusService) getEndpointDomainStatus(domain *model.DomainRegistry) model.DomainStatus {
	// 查询 EndpointStatusCache
	epStatus, exists := cache.GetEndpointStatus(domain.EndpointID)
	if !exists {
		// 缓存不存在 → offline（已断连）
		logger.Debugf("Endpoint 缓存不存在，判断为 offline: domain=%s, endpoint_id=%s",
			domain.Domain, domain.EndpointID)
		return model.DomainStatusOffline
	}

	// 计算心跳时间差
	timeSinceHeartbeat := time.Since(epStatus.LastHeartbeat)

	// 心跳正常（< 60秒）→ online
	if timeSinceHeartbeat < HeartbeatTimeout {
		logger.Debugf("Endpoint 心跳正常，判断为 online: domain=%s, endpoint_id=%s, last_heartbeat=%v",
			domain.Domain, domain.EndpointID, epStatus.LastHeartbeat)
		return model.DomainStatusOnline
	}

	// 心跳超时（>= 60秒）→ offline
	// 注意：Endpoint 不在 Tailscale 网络中，无法通过 Headscale 查询
	logger.Infof("Endpoint 心跳超时，判断为 offline: domain=%s, endpoint_id=%s, time_since=%v",
		domain.Domain, domain.EndpointID, timeSinceHeartbeat)
	return model.DomainStatusOffline
}
