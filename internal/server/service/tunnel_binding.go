package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// TunnelServiceBinding 统一的出站隧道服务端口绑定模型
type TunnelServiceBinding struct {
	ResourceID   string `json:"resource_id"`
	Namespace    string `json:"namespace"`
	NamespaceUID string `json:"namespace_uid"`
	ServiceName  string `json:"service_name"`
	ServiceUID   string `json:"service_uid"`
	PortName     string `json:"port_name"` // 允许为空（K8s 未命名端口合法）
	PortNumber   int32  `json:"target_port"`
	Protocol     string `json:"protocol"`
	LocalPort    int32  `json:"local_port"`
}

// UnmarshalJSON 自定义反序列化，兼容 target_port 与 port_number
func (b *TunnelServiceBinding) UnmarshalJSON(data []byte) error {
	type Alias TunnelServiceBinding
	aux := struct {
		*Alias
		AltPortNumber int32 `json:"port_number"`
	}{
		Alias: (*Alias)(b),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if b.PortNumber <= 0 && aux.AltPortNumber > 0 {
		b.PortNumber = aux.AltPortNumber
	}
	if b.Protocol == "" {
		b.Protocol = "TCP"
	}
	return nil
}

// Complete 校验绑定条目的完整性。
// 要求 resource_id, service_uid, service_name, namespace, namespace_uid 非空，port_number > 0，protocol 为 TCP。
// port_name 允许为空（K8s 允许未命名的容器服务端口）。
func (b TunnelServiceBinding) Complete() bool {
	if strings.TrimSpace(b.ResourceID) == "" ||
		strings.TrimSpace(b.ServiceUID) == "" ||
		strings.TrimSpace(b.ServiceName) == "" ||
		strings.TrimSpace(b.Namespace) == "" ||
		strings.TrimSpace(b.NamespaceUID) == "" {
		return false
	}
	if b.PortNumber <= 0 || b.PortNumber > 65535 {
		return false
	}
	if strings.ToUpper(strings.TrimSpace(b.Protocol)) != "TCP" {
		return false
	}
	return true
}

// TunnelPortsConfig 统一解析 ports_config JSON 的容器结构
type TunnelPortsConfig struct {
	K8sAPIEnabled bool                   `json:"k8s_api_enabled"`
	K8sAPIPort    int                    `json:"k8s_api_port"`
	Ports         []TunnelServiceBinding `json:"ports"`
}

// ParseTunnelPortsConfig 解析 ports_config 字符串
func ParseTunnelPortsConfig(raw string) (TunnelPortsConfig, error) {
	if strings.TrimSpace(raw) == "" {
		return TunnelPortsConfig{K8sAPIPort: 6443}, nil
	}
	var cfg TunnelPortsConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return TunnelPortsConfig{}, err
	}
	if cfg.K8sAPIPort <= 0 {
		cfg.K8sAPIPort = 6443
	}
	return cfg, nil
}

// TunnelSessionIDs 构造统一的 Session / Source / TargetRevision ID
func TunnelSessionIDs(tokenID uint64, resourceID string) (sessionID, sourceID, targetRevisionID string) {
	sessionID = fmt.Sprintf("tunnel-%d-%s", tokenID, resourceID)
	sourceID = fmt.Sprintf("tunnel-token-%d", tokenID)
	targetRevisionID = fmt.Sprintf("tunnel-target-%d-%s", tokenID, resourceID)
	return
}

// IsTunnelSessionID 判断给定 SessionID 是否为 Tunnel 合成会话
func IsTunnelSessionID(id string) bool {
	return strings.HasPrefix(id, "tunnel-")
}

// TunnelCandidateService 候选真实 Kubernetes Service 端口条目
type TunnelCandidateService struct {
	ResourceID   string `json:"resource_id"`
	Namespace    string `json:"namespace"`
	NamespaceUID string `json:"namespace_uid"`
	ServiceName  string `json:"service_name"`
	ServiceUID   string `json:"service_uid"`
	PortName     string `json:"port_name"`
	PortNumber   int    `json:"port_number"`
	Protocol     string `json:"protocol"`
	Ready        bool   `json:"ready"`
}

// QueryTunnelCandidateServices 查询指定 targetAgentName 所在边缘集群上报的全部 TCP 容器服务候选
func QueryTunnelCandidateServices(ctx context.Context, gormDB *gorm.DB, targetAgentName string, now time.Time) ([]TunnelCandidateService, error) {
	if gormDB == nil || strings.TrimSpace(targetAgentName) == "" {
		return []TunnelCandidateService{}, nil
	}

	var agentNode model.Node
	if err := gormDB.WithContext(ctx).
		Where("name = ? AND type = ?", targetAgentName, model.NodeTypeAgent).
		First(&agentNode).Error; err != nil {
		return []TunnelCandidateService{}, nil
	}

	var binding model.TechnicalResourceBinding
	if err := gormDB.WithContext(ctx).
		Where("source_type = ? AND source_id = ? AND enabled = ?", model.TechnicalResourceBindingLegacyNode, fmt.Sprint(agentNode.ID), true).
		First(&binding).Error; err != nil {
		return []TunnelCandidateService{}, nil
	}

	var technical model.TechnicalResource
	if err := gormDB.WithContext(ctx).
		Where("id = ? AND lifecycle_state = ?", binding.TechnicalResourceID, model.TechnicalResourceRegistered).
		First(&technical).Error; err != nil {
		return []TunnelCandidateService{}, nil
	}

	var sources []model.WorkloadObservationSource
	if err := gormDB.WithContext(ctx).
		Preload("WorkloadObservation").
		Where("source_technical_resource_id = ? AND state = ? AND lease_expires_at > ?",
			technical.ID, model.WorkloadObservationSourceObserved, now).
		Find(&sources).Error; err != nil {
		return nil, err
	}

	candidates := make([]TunnelCandidateService, 0, len(sources))
	seen := make(map[string]bool)

	for _, src := range sources {
		obs := src.WorkloadObservation
		if obs == nil || obs.Kind != model.WorkloadObservationServicePort || obs.State == model.WorkloadObservationStale {
			continue
		}

		var target struct {
			ServiceUID  string `json:"service_uid"`
			ServiceName string `json:"service_name"`
			PortName    string `json:"port_name"`
			Protocol    string `json:"protocol"`
			PortNumber  int    `json:"port_number"`
		}
		if err := json.Unmarshal([]byte(src.TargetSnapshot), &target); err != nil {
			continue
		}

		if strings.ToUpper(strings.TrimSpace(target.Protocol)) != "TCP" || target.PortNumber <= 0 {
			continue
		}

		// 获取所属命名空间
		var scope model.ResourceScope
		if err := gormDB.WithContext(ctx).First(&scope, "id = ?", obs.NamespaceScopeID).Error; err != nil || scope.NamespaceObservationID == nil {
			continue
		}

		var nsObs model.NamespaceObservation
		if err := gormDB.WithContext(ctx).First(&nsObs, "id = ?", *scope.NamespaceObservationID).Error; err != nil {
			continue
		}

		candidateKey := fmt.Sprintf("%s:%d", obs.ID, target.PortNumber)
		if seen[candidateKey] {
			continue
		}
		seen[candidateKey] = true

		candidates = append(candidates, TunnelCandidateService{
			ResourceID:   obs.ID,
			Namespace:    nsObs.Name,
			NamespaceUID: nsObs.NamespaceUID,
			ServiceName:  target.ServiceName,
			ServiceUID:   target.ServiceUID,
			PortName:     target.PortName,
			PortNumber:   target.PortNumber,
			Protocol:     "TCP",
			Ready:        src.Ready && obs.Ready,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Namespace != candidates[j].Namespace {
			return candidates[i].Namespace < candidates[j].Namespace
		}
		if candidates[i].ServiceName != candidates[j].ServiceName {
			return candidates[i].ServiceName < candidates[j].ServiceName
		}
		return candidates[i].PortNumber < candidates[j].PortNumber
	})

	return candidates, nil
}
