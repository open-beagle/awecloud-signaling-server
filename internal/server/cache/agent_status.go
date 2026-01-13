package cache

import (
	"sync"
	"time"
)

// NetworkInterface 网络接口信息
type NetworkInterface struct {
	Name    string
	IP      string
	Mask    string
	Gateway string
}

// AgentTsStatus Agent 隧道状态（内存缓存，不存数据库）
type AgentTsStatus struct {
	TsConnectedAt *time.Time         // 连接时间
	Hostname      string             // 主机名
	Runtime       string             // 运行环境
	Networks      []NetworkInterface // 网络接口列表
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

// UpdateAgentNetworkInfo 更新 Agent 网络信息
func UpdateAgentNetworkInfo(agentID int64, hostname, runtime string, networks []NetworkInterface) {
	agentTsStatusMutex.Lock()
	defer agentTsStatusMutex.Unlock()

	if agentTsStatusCache[agentID] == nil {
		agentTsStatusCache[agentID] = &AgentTsStatus{}
	}
	agentTsStatusCache[agentID].Hostname = hostname
	agentTsStatusCache[agentID].Runtime = runtime
	agentTsStatusCache[agentID].Networks = networks
}

// GetAgentTsStatus 获取 Agent 隧道状态
func GetAgentTsStatus(agentID int64) *AgentTsStatus {
	agentTsStatusMutex.RLock()
	defer agentTsStatusMutex.RUnlock()

	return agentTsStatusCache[agentID]
}

// GetAgentTsConnectedAt 获取 Agent 连接时间
func GetAgentTsConnectedAt(agentID int64) *time.Time {
	agentTsStatusMutex.RLock()
	defer agentTsStatusMutex.RUnlock()

	if status := agentTsStatusCache[agentID]; status != nil {
		return status.TsConnectedAt
	}
	return nil
}

// ClearAgentTsStatus 清除 Agent 隧道状态（Agent 删除时调用）
func ClearAgentTsStatus(agentID int64) {
	agentTsStatusMutex.Lock()
	defer agentTsStatusMutex.Unlock()

	delete(agentTsStatusCache, agentID)
}
