package cache

import (
	"sync"
	"time"
)

// EndpointStatus Endpoint 在线状态
type EndpointStatus struct {
	Online        bool      `json:"online"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	AgentUserID   uint64    `json:"agent_user_id"`
}

var (
	// Endpoint 状态缓存 map[endpoint_id]*EndpointStatus
	endpointStatusCache = make(map[string]*EndpointStatus)
	endpointStatusMutex sync.RWMutex
)

// UpdateEndpointStatus 更新 Endpoint 状态
func UpdateEndpointStatus(endpointID string, agentUserID uint64, online bool) {
	endpointStatusMutex.Lock()
	defer endpointStatusMutex.Unlock()

	endpointStatusCache[endpointID] = &EndpointStatus{
		Online:        online,
		LastHeartbeat: time.Now(),
		AgentUserID:   agentUserID,
	}
}

// GetEndpointStatus 获取 Endpoint 状态
func GetEndpointStatus(endpointID string) *EndpointStatus {
	endpointStatusMutex.RLock()
	defer endpointStatusMutex.RUnlock()

	return endpointStatusCache[endpointID]
}

// SetAgentEndpointsOffline 将指定 Agent 的所有 Endpoint 设为离线
func SetAgentEndpointsOffline(agentUserID uint64) {
	endpointStatusMutex.Lock()
	defer endpointStatusMutex.Unlock()

	for id, status := range endpointStatusCache {
		if status.AgentUserID == agentUserID {
			endpointStatusCache[id] = &EndpointStatus{
				Online:        false,
				LastHeartbeat: status.LastHeartbeat,
				AgentUserID:   agentUserID,
			}
		}
	}
}
