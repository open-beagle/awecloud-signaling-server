package cache

import (
	"sync"
	"time"
)

// DiscoveredServicePort K8S Service 端口信息
type DiscoveredServicePort struct {
	Name     string `json:"name"`
	Port     int32  `json:"port"`
	Protocol string `json:"protocol"`
}

// DiscoveredService Agent 发现的 K8S Service
type DiscoveredService struct {
	Namespace   string                  `json:"namespace"`
	ServiceName string                  `json:"service_name"`
	ClusterIP   string                  `json:"cluster_ip"`
	Ports       []DiscoveredServicePort `json:"ports"`
	Labels      map[string]string       `json:"labels"`
	UpdatedAt   time.Time               `json:"updated_at"`
}

var (
	// K8S Service 发现缓存 map[agent_user_id][]DiscoveredService
	k8sServiceDiscoveryCache = make(map[uint64][]DiscoveredService)
	k8sServiceDiscoveryMutex sync.RWMutex
)

// UpdateK8SServiceDiscovery 更新 Agent 发现的 K8S Service 列表
func UpdateK8SServiceDiscovery(agentUserID uint64, services []DiscoveredService) {
	k8sServiceDiscoveryMutex.Lock()
	defer k8sServiceDiscoveryMutex.Unlock()

	now := time.Now()
	for i := range services {
		services[i].UpdatedAt = now
	}
	k8sServiceDiscoveryCache[agentUserID] = services
}

// GetK8SServiceDiscovery 获取 Agent 发现的 K8S Service 列表
func GetK8SServiceDiscovery(agentUserID uint64) []DiscoveredService {
	k8sServiceDiscoveryMutex.RLock()
	defer k8sServiceDiscoveryMutex.RUnlock()

	return k8sServiceDiscoveryCache[agentUserID]
}

// GetAllK8SServiceDiscovery 获取所有 Agent 发现的 K8S Service
func GetAllK8SServiceDiscovery() map[uint64][]DiscoveredService {
	k8sServiceDiscoveryMutex.RLock()
	defer k8sServiceDiscoveryMutex.RUnlock()

	result := make(map[uint64][]DiscoveredService, len(k8sServiceDiscoveryCache))
	for k, v := range k8sServiceDiscoveryCache {
		result[k] = v
	}
	return result
}

// ClearK8SServiceDiscovery 清除 Agent 的 K8S Service 发现缓存
func ClearK8SServiceDiscovery(agentUserID uint64) {
	k8sServiceDiscoveryMutex.Lock()
	defer k8sServiceDiscoveryMutex.Unlock()

	delete(k8sServiceDiscoveryCache, agentUserID)
}
