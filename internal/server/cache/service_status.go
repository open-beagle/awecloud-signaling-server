package cache

import (
	"sync"
	"time"
)

// 服务运行状态常量
const (
	ServiceStatusRunning = "running" // 服务正常运行中
	ServiceStatusStopped = "stopped" // 服务已停止
	ServiceStatusError   = "error"   // 服务启动失败或运行异常
	ServiceStatusPending = "pending" // 等待启动（如等待 VPN 就绪）
)

// 前端显示状态常量
const (
	DisplayStatusDisabled = "disabled" // 管理员已禁用
	DisplayStatusOffline  = "offline"  // Agent 离线
	DisplayStatusRunning  = "running"  // 服务正常运行中
	DisplayStatusStopped  = "stopped"  // 服务已停止
	DisplayStatusError    = "error"    // 服务启动失败或运行异常
	DisplayStatusPending  = "pending"  // 等待启动
)

// ServiceRuntimeStatus 服务运行时状态
type ServiceRuntimeStatus struct {
	ServiceID string    // 服务 ID
	Status    string    // 运行状态：running/stopped/error/pending
	ErrorCode string    // 错误码
	ErrorMsg  string    // 错误信息
	UpdatedAt time.Time // 最后更新时间
}

var (
	// 本地服务运行状态缓存 map[service_id]*ServiceRuntimeStatus
	proxyServiceStatusCache = make(map[string]*ServiceRuntimeStatus)
	proxyServiceStatusMutex sync.RWMutex

	// 远程服务运行状态缓存 map[forward_id]*ServiceRuntimeStatus
	portForwardStatusCache = make(map[string]*ServiceRuntimeStatus)
	portForwardStatusMutex sync.RWMutex
)

// UpdateProxyServiceStatus 更新本地服务运行状态
func UpdateProxyServiceStatus(serviceID, status, errorCode, errorMsg string) {
	proxyServiceStatusMutex.Lock()
	defer proxyServiceStatusMutex.Unlock()

	proxyServiceStatusCache[serviceID] = &ServiceRuntimeStatus{
		ServiceID: serviceID,
		Status:    status,
		ErrorCode: errorCode,
		ErrorMsg:  errorMsg,
		UpdatedAt: time.Now(),
	}
}

// GetProxyServiceStatus 获取本地服务运行状态
func GetProxyServiceStatus(serviceID string) *ServiceRuntimeStatus {
	proxyServiceStatusMutex.RLock()
	defer proxyServiceStatusMutex.RUnlock()

	return proxyServiceStatusCache[serviceID]
}

// DeleteProxyServiceStatus 删除本地服务运行状态
func DeleteProxyServiceStatus(serviceID string) {
	proxyServiceStatusMutex.Lock()
	defer proxyServiceStatusMutex.Unlock()

	delete(proxyServiceStatusCache, serviceID)
}

// UpdatePortForwardStatus 更新远程服务运行状态
func UpdatePortForwardStatus(forwardID, status, errorCode, errorMsg string) {
	portForwardStatusMutex.Lock()
	defer portForwardStatusMutex.Unlock()

	portForwardStatusCache[forwardID] = &ServiceRuntimeStatus{
		ServiceID: forwardID,
		Status:    status,
		ErrorCode: errorCode,
		ErrorMsg:  errorMsg,
		UpdatedAt: time.Now(),
	}
}

// GetPortForwardStatus 获取远程服务运行状态
func GetPortForwardStatus(forwardID string) *ServiceRuntimeStatus {
	portForwardStatusMutex.RLock()
	defer portForwardStatusMutex.RUnlock()

	return portForwardStatusCache[forwardID]
}

// DeletePortForwardStatus 删除远程服务运行状态
func DeletePortForwardStatus(forwardID string) {
	portForwardStatusMutex.Lock()
	defer portForwardStatusMutex.Unlock()

	delete(portForwardStatusCache, forwardID)
}

// SetAgentServicesOffline 将指定 Agent 的所有服务状态设置为 stopped
// 当 Agent 离线时调用
func SetAgentServicesOffline(agentID uint64, serviceIDs []string, forwardIDs []string) {
	now := time.Now()

	// 更新本地服务状态
	proxyServiceStatusMutex.Lock()
	for _, id := range serviceIDs {
		proxyServiceStatusCache[id] = &ServiceRuntimeStatus{
			ServiceID: id,
			Status:    ServiceStatusStopped,
			UpdatedAt: now,
		}
	}
	proxyServiceStatusMutex.Unlock()

	// 更新远程服务状态
	portForwardStatusMutex.Lock()
	for _, id := range forwardIDs {
		portForwardStatusCache[id] = &ServiceRuntimeStatus{
			ServiceID: id,
			Status:    ServiceStatusStopped,
			UpdatedAt: now,
		}
	}
	portForwardStatusMutex.Unlock()
}

// GetDisplayStatus 根据 enabled 和运行状态计算前端显示状态
// enabled: 数据库中的启用状态
// agentOnline: Agent 是否在线
// runtimeStatus: 内存中的运行状态（可为 nil）
func GetDisplayStatus(enabled bool, agentOnline bool, runtimeStatus *ServiceRuntimeStatus) (displayStatus string, errorMsg string) {
	if !enabled {
		return DisplayStatusDisabled, ""
	}

	if !agentOnline {
		return DisplayStatusOffline, ""
	}

	if runtimeStatus == nil {
		return DisplayStatusPending, ""
	}

	return runtimeStatus.Status, runtimeStatus.ErrorMsg
}
