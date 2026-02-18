package cache

import (
	"sync"
	"time"
)

// NodeStatus 存储 Node 设备状态
type NodeStatus struct {
	NodeID        uint64    // Node ID
	UserID        uint64    // User ID
	TunnelIP      string    // Tailscale IP
	LastHeartbeat time.Time // 最后心跳时间
}

// EndpointStatus 存储 Endpoint 状态
type EndpointStatus struct {
	EndpointName  string    // Endpoint 名称
	UserID        uint64    // 所属 Agent User ID
	LastHeartbeat time.Time // 最后心跳时间（Agent 转发）
}

// 全局缓存实例
var (
	nodeStatusCache     sync.Map // map[uint64]NodeStatus
	endpointStatusCache sync.Map // map[string]EndpointStatus
)

// SetNodeStatus 设置 Node 状态
func SetNodeStatus(nodeID uint64, status NodeStatus) {
	nodeStatusCache.Store(nodeID, status)
}

// GetNodeStatus 获取 Node 状态
func GetNodeStatus(nodeID uint64) (NodeStatus, bool) {
	value, ok := nodeStatusCache.Load(nodeID)
	if !ok {
		return NodeStatus{}, false
	}
	return value.(NodeStatus), true
}

// DeleteNodeStatus 删除 Node 状态
func DeleteNodeStatus(nodeID uint64) {
	nodeStatusCache.Delete(nodeID)
}

// GetAllNodeStatus 获取所有 Node 状态（用于调试）
func GetAllNodeStatus() map[uint64]NodeStatus {
	result := make(map[uint64]NodeStatus)
	nodeStatusCache.Range(func(key, value interface{}) bool {
		result[key.(uint64)] = value.(NodeStatus)
		return true
	})
	return result
}

// SetEndpointStatus 设置 Endpoint 状态
func SetEndpointStatus(name string, status EndpointStatus) {
	endpointStatusCache.Store(name, status)
}

// GetEndpointStatus 获取 Endpoint 状态
func GetEndpointStatus(name string) (EndpointStatus, bool) {
	value, ok := endpointStatusCache.Load(name)
	if !ok {
		return EndpointStatus{}, false
	}
	return value.(EndpointStatus), true
}

// DeleteEndpointStatus 删除 Endpoint 状态
func DeleteEndpointStatus(name string) {
	endpointStatusCache.Delete(name)
}

// GetAllEndpointStatus 获取所有 Endpoint 状态（用于调试）
func GetAllEndpointStatus() map[string]EndpointStatus {
	result := make(map[string]EndpointStatus)
	endpointStatusCache.Range(func(key, value interface{}) bool {
		result[key.(string)] = value.(EndpointStatus)
		return true
	})
	return result
}

// ClearAllCache 清空所有缓存（用于测试）
func ClearAllCache() {
	nodeStatusCache = sync.Map{}
	endpointStatusCache = sync.Map{}
}
