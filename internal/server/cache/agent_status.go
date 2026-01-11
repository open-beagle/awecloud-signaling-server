package cache

import (
	"sync"
	"time"
)

// AgentTsStatus Agent 隧道状态（内存缓存，不存数据库）
type AgentTsStatus struct {
	TsConnectedAt *time.Time // 连接时间
}

var (
	agentTsStatusCache = make(map[int64]*AgentTsStatus)
	agentTsStatusMutex sync.RWMutex
)

// UpdateAgentTsConnectedAt 更新 Agent 连接时间
func UpdateAgentTsConnectedAt(agentID int64, connectedAt *time.Time) {
	agentTsStatusMutex.Lock()
	defer agentTsStatusMutex.Unlock()

	if agentTsStatusCache[agentID] == nil {
		agentTsStatusCache[agentID] = &AgentTsStatus{}
	}
	agentTsStatusCache[agentID].TsConnectedAt = connectedAt
}

// GetAgentTsStatus 获取 Agent 隧道状态
func GetAgentTsStatus(agentID int64) *AgentTsStatus {
	agentTsStatusMutex.RLock()
	defer agentTsStatusMutex.RUnlock()

	return agentTsStatusCache[agentID]
}

// ClearAgentTsStatus 清除 Agent 隧道状态（Agent 删除时调用）
func ClearAgentTsStatus(agentID int64) {
	agentTsStatusMutex.Lock()
	defer agentTsStatusMutex.Unlock()

	delete(agentTsStatusCache, agentID)
}
