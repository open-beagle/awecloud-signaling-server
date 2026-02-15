package cache

import (
	"sync"
	"time"
)

// EndpointDiscoveredService Endpoint 发现的 K8S Service
type EndpointDiscoveredService struct {
	Namespace   string                  `json:"namespace"`
	ServiceName string                  `json:"service_name"`
	ClusterIP   string                  `json:"cluster_ip"`
	Ports       []DiscoveredServicePort `json:"ports"`
	UpdatedAt   time.Time               `json:"updated_at"`
}

var (
	// Endpoint K8S Service 发现缓存 map[endpoint_name][]EndpointDiscoveredService
	// 按 endpoint_name 索引，因为 Endpoint 通过 Agent 心跳间接上报
	endpointK8SServiceCache = make(map[string][]EndpointDiscoveredService)
	endpointK8SServiceMutex sync.RWMutex
)

// UpdateEndpointK8SServiceDiscovery 更新 Endpoint 发现的 K8S Service 列表
func UpdateEndpointK8SServiceDiscovery(endpointName string, services []EndpointDiscoveredService) {
	endpointK8SServiceMutex.Lock()
	defer endpointK8SServiceMutex.Unlock()

	now := time.Now()
	for i := range services {
		services[i].UpdatedAt = now
	}
	endpointK8SServiceCache[endpointName] = services
}

// GetEndpointK8SServiceDiscovery 获取 Endpoint 发现的 K8S Service 列表
func GetEndpointK8SServiceDiscovery(endpointName string) []EndpointDiscoveredService {
	endpointK8SServiceMutex.RLock()
	defer endpointK8SServiceMutex.RUnlock()

	return endpointK8SServiceCache[endpointName]
}

// GetAllEndpointK8SServiceDiscovery 获取所有 Endpoint 发现的 K8S Service
func GetAllEndpointK8SServiceDiscovery() map[string][]EndpointDiscoveredService {
	endpointK8SServiceMutex.RLock()
	defer endpointK8SServiceMutex.RUnlock()

	result := make(map[string][]EndpointDiscoveredService, len(endpointK8SServiceCache))
	for k, v := range endpointK8SServiceCache {
		result[k] = v
	}
	return result
}

// ClearEndpointK8SServiceDiscovery 清除 Endpoint 的 K8S Service 发现缓存
func ClearEndpointK8SServiceDiscovery(endpointName string) {
	endpointK8SServiceMutex.Lock()
	defer endpointK8SServiceMutex.Unlock()

	delete(endpointK8SServiceCache, endpointName)
}

// ClearAgentEndpointK8SServiceDiscovery 清除指定 Agent 下所有 Endpoint 的缓存
// endpointNames 为该 Agent 下所有 Endpoint 的名称列表
func ClearAgentEndpointK8SServiceDiscovery(endpointNames []string) {
	endpointK8SServiceMutex.Lock()
	defer endpointK8SServiceMutex.Unlock()

	for _, name := range endpointNames {
		delete(endpointK8SServiceCache, name)
	}
}
