package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// DomainService 域名管理服务
type DomainService struct {
	db *gorm.DB
}

// NewDomainService 创建域名管理服务
func NewDomainService(db *gorm.DB) *DomainService {
	return &DomainService{
		db: db,
	}
}

func (s *DomainService) providerDomain(ctx context.Context, label string, segments ...string) string {
	parts := append(append(make([]string, 0, len(segments)+1), segments...), label)
	return strings.Join(parts, ".") + domainSuffix(ctx, s.db)
}

// CreateNodeSSHDomain 创建 Node SSH 域名
// domain = "{node_name}.{domain_label}.{domain_suffix}"
func (s *DomainService) CreateNodeSSHDomain(ctx context.Context, node *model.Node, user *model.User) error {
	// 生成域名
	label := EffectiveProviderDomainLabel(ctx, s.db, user.ID, user.Name)
	domain := s.providerDomain(ctx, label, node.Name)

	// 检查是否已存在
	var existing model.DomainRegistry
	err := s.db.WithContext(ctx).Where("domain = ? AND user_id = ?", domain, user.ID).First(&existing).Error
	if err == nil {
		logger.Infof("Node SSH 域名已存在，跳过创建: domain=%s", domain)
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return fmt.Errorf("查询域名失败: %w", err)
	}

	// 创建域名记录
	domainRecord := &model.DomainRegistry{
		Domain:     domain,
		Type:       model.DomainTypeSSH,
		UserID:     user.ID,
		NodeID:     node.ID,
		TargetIP:   node.IP,
		TargetPort: 22,
	}

	if err := s.db.WithContext(ctx).Create(domainRecord).Error; err != nil {
		return fmt.Errorf("创建域名记录失败: %w", err)
	}

	logger.Infof("创建 Node SSH 域名成功: domain=%s, node_id=%d, user_id=%d", domain, node.ID, user.ID)
	return nil
}

// CreateNodeK8SAPIDomain 创建 Node K8S API 域名
// domain = "kubernetes.{domain_label}.{domain_suffix}"
func (s *DomainService) CreateNodeK8SAPIDomain(ctx context.Context, node *model.Node, user *model.User) error {
	// 生成域名
	label := EffectiveProviderDomainLabel(ctx, s.db, user.ID, user.Name)
	domain := s.providerDomain(ctx, label, "kubernetes")

	// 检查是否已存在
	var existing model.DomainRegistry
	err := s.db.WithContext(ctx).Where("domain = ? AND user_id = ?", domain, user.ID).First(&existing).Error
	if err == nil {
		logger.Infof("Node K8S API 域名已存在，跳过创建: domain=%s", domain)
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return fmt.Errorf("查询域名失败: %w", err)
	}

	// 获取 K8S API 端口（默认 6443）
	port := 6443
	if node.K8SListenPort != nil && *node.K8SListenPort > 0 {
		port = *node.K8SListenPort
	}

	// 创建域名记录
	domainRecord := &model.DomainRegistry{
		Domain:     domain,
		Type:       model.DomainTypeK8SAPI,
		UserID:     user.ID,
		NodeID:     node.ID,
		TargetIP:   node.IP,
		TargetPort: port,
	}

	if err := s.db.WithContext(ctx).Create(domainRecord).Error; err != nil {
		return fmt.Errorf("创建域名记录失败: %w", err)
	}

	logger.Infof("创建 Node K8S API 域名成功: domain=%s, node_id=%d, user_id=%d, port=%d", domain, node.ID, user.ID, port)
	return nil
}

// DeleteNodeSSHDomain 删除 Node SSH 域名
func (s *DomainService) DeleteNodeSSHDomain(ctx context.Context, node *model.Node, user *model.User) error {
	label := EffectiveProviderDomainLabel(ctx, s.db, user.ID, user.Name)
	domain := s.providerDomain(ctx, label, node.Name)

	result := s.db.WithContext(ctx).Where("domain = ? AND user_id = ? AND type = ?", domain, user.ID, model.DomainTypeSSH).
		Delete(&model.DomainRegistry{})

	if result.Error != nil {
		return fmt.Errorf("删除域名记录失败: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		logger.Infof("删除 Node SSH 域名成功: domain=%s, node_id=%d, user_id=%d", domain, node.ID, user.ID)
	}

	return nil
}

// DeleteNodeK8SAPIDomain 删除 Node K8S API 域名
func (s *DomainService) DeleteNodeK8SAPIDomain(ctx context.Context, node *model.Node, user *model.User) error {
	label := EffectiveProviderDomainLabel(ctx, s.db, user.ID, user.Name)
	domain := s.providerDomain(ctx, label, "kubernetes")

	result := s.db.WithContext(ctx).Where("domain = ? AND user_id = ? AND type = ?", domain, user.ID, model.DomainTypeK8SAPI).
		Delete(&model.DomainRegistry{})

	if result.Error != nil {
		return fmt.Errorf("删除域名记录失败: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		logger.Infof("删除 Node K8S API 域名成功: domain=%s, node_id=%d, user_id=%d", domain, node.ID, user.ID)
	}

	return nil
}

// DeleteNodeAllDomains 删除 Node 的所有域名
func (s *DomainService) DeleteNodeAllDomains(ctx context.Context, nodeID uint64) error {
	result := s.db.WithContext(ctx).Where("node_id = ?", nodeID).Delete(&model.DomainRegistry{})

	if result.Error != nil {
		return fmt.Errorf("删除 Node 所有域名失败: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		logger.Infof("删除 Node 所有域名成功: node_id=%d, count=%d", nodeID, result.RowsAffected)
	}

	return nil
}

// CreateEndpointSSHDomain 创建 Endpoint SSH 域名
// domain = "{endpoint_name}.{domain_label}.{domain_suffix}"
func (s *DomainService) CreateEndpointSSHDomain(ctx context.Context, endpoint *model.Endpoint, agentNode *model.Node, user *model.User) error {
	// 生成域名
	label := EffectiveProviderDomainLabel(ctx, s.db, user.ID, user.Name)
	domain := s.providerDomain(ctx, label, endpoint.Name)

	// 检查是否已存在
	var existing model.DomainRegistry
	err := s.db.WithContext(ctx).Where("domain = ? AND user_id = ?", domain, user.ID).First(&existing).Error
	if err == nil {
		logger.Infof("Endpoint SSH 域名已存在，跳过创建: domain=%s", domain)
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return fmt.Errorf("查询域名失败: %w", err)
	}

	// 创建域名记录（使用 Endpoint 预分配的端口）
	domainRecord := &model.DomainRegistry{
		Domain:     domain,
		Type:       model.DomainTypeSSH,
		UserID:     user.ID,
		NodeID:     agentNode.ID,          // Agent Node ID
		EndpointID: endpoint.Name,         // Endpoint 名称
		TargetIP:   agentNode.IP,          // Agent IP
		TargetPort: int(endpoint.SSHPort), // 从 Endpoint 表读取端口
	}

	if err := s.db.WithContext(ctx).Create(domainRecord).Error; err != nil {
		return fmt.Errorf("创建域名记录失败: %w", err)
	}

	logger.Infof("创建 Endpoint SSH 域名成功: domain=%s, endpoint=%s, node_id=%d, user_id=%d",
		domain, endpoint.Name, agentNode.ID, user.ID)
	return nil
}

// DeleteEndpointSSHDomain 删除 Endpoint SSH 域名
func (s *DomainService) DeleteEndpointSSHDomain(ctx context.Context, endpointName string, user *model.User) error {
	label := EffectiveProviderDomainLabel(ctx, s.db, user.ID, user.Name)
	domain := s.providerDomain(ctx, label, endpointName)

	result := s.db.WithContext(ctx).Where("domain = ? AND user_id = ? AND type = ?", domain, user.ID, model.DomainTypeSSH).
		Delete(&model.DomainRegistry{})

	if result.Error != nil {
		return fmt.Errorf("删除域名记录失败: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		logger.Infof("删除 Endpoint SSH 域名成功: domain=%s, endpoint=%s, user_id=%d", domain, endpointName, user.ID)
	}

	return nil
}

// DeleteEndpointAllDomains 删除 Endpoint 的所有域名
func (s *DomainService) DeleteEndpointAllDomains(ctx context.Context, endpointName string) error {
	result := s.db.WithContext(ctx).Where("endpoint_id = ?", endpointName).Delete(&model.DomainRegistry{})

	if result.Error != nil {
		return fmt.Errorf("删除 Endpoint 所有域名失败: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		logger.Infof("删除 Endpoint 所有域名成功: endpoint=%s, count=%d", endpointName, result.RowsAffected)
	}

	return nil
}

// CreateEndpointK8SAPIDomain 创建 Endpoint K8S API 域名
// domain = "kubernetes.{domain_label}.{domain_suffix}"
// 注意：Endpoint K8S API 域名格式和 Node K8S API 一样，通过 endpoint_id 字段区分
func (s *DomainService) CreateEndpointK8SAPIDomain(ctx context.Context, endpoint *model.Endpoint, agentNode *model.Node, user *model.User) error {
	// 生成域名
	label := EffectiveProviderDomainLabel(ctx, s.db, user.ID, user.Name)
	domain := s.providerDomain(ctx, label, "kubernetes")

	// 检查是否已存在（按 endpoint_id 区分）
	var existing model.DomainRegistry
	err := s.db.WithContext(ctx).Where("domain = ? AND user_id = ? AND endpoint_id = ?", domain, user.ID, endpoint.Name).First(&existing).Error
	if err == nil {
		logger.Infof("Endpoint K8S API 域名已存在，跳过创建: domain=%s, endpoint=%s", domain, endpoint.Name)
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return fmt.Errorf("查询域名失败: %w", err)
	}

	// 创建域名记录（使用 Endpoint 预分配的端口）
	domainRecord := &model.DomainRegistry{
		Domain:     domain,
		Type:       model.DomainTypeK8SAPI,
		UserID:     user.ID,
		NodeID:     agentNode.ID,             // Agent Node ID
		EndpointID: endpoint.Name,            // Endpoint 名称
		TargetIP:   agentNode.IP,             // Agent IP
		TargetPort: int(endpoint.K8SAPIPort), // 从 Endpoint 表读取端口
	}

	if err := s.db.WithContext(ctx).Create(domainRecord).Error; err != nil {
		return fmt.Errorf("创建域名记录失败: %w", err)
	}

	logger.Infof("创建 Endpoint K8S API 域名成功: domain=%s, endpoint=%s, node_id=%d, user_id=%d",
		domain, endpoint.Name, agentNode.ID, user.ID)
	return nil
}

// DeleteEndpointK8SAPIDomain 删除 Endpoint K8S API 域名
func (s *DomainService) DeleteEndpointK8SAPIDomain(ctx context.Context, endpointName string, user *model.User) error {
	label := EffectiveProviderDomainLabel(ctx, s.db, user.ID, user.Name)
	domain := s.providerDomain(ctx, label, "kubernetes")

	result := s.db.WithContext(ctx).Where("domain = ? AND user_id = ? AND endpoint_id = ?", domain, user.ID, endpointName).
		Delete(&model.DomainRegistry{})

	if result.Error != nil {
		return fmt.Errorf("删除域名记录失败: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		logger.Infof("删除 Endpoint K8S API 域名成功: domain=%s, endpoint=%s, user_id=%d", domain, endpointName, user.ID)
	}

	return nil
}

// CreateNodeK8SSVCDomain 创建 Node K8S Service 域名
// domain = "{service_name}.{namespace}.{domain_label}.{domain_suffix}"
func (s *DomainService) CreateNodeK8SSVCDomain(ctx context.Context, node *model.Node, user *model.User, namespace, serviceName, clusterIP string, port int) error {
	// 生成域名
	label := EffectiveProviderDomainLabel(ctx, s.db, user.ID, user.Name)
	domain := s.providerDomain(ctx, label, serviceName, namespace)

	// 检查是否已存在
	var existing model.DomainRegistry
	err := s.db.WithContext(ctx).Where("domain = ? AND user_id = ?", domain, user.ID).First(&existing).Error
	if err == nil {
		// 已存在，更新记录
		updates := map[string]any{
			"target_ip":   clusterIP,
			"target_port": port,
		}
		if err := s.db.WithContext(ctx).Model(&existing).Updates(updates).Error; err != nil {
			return fmt.Errorf("更新域名记录失败: %w", err)
		}
		logger.Infof("更新 Node K8S Service 域名: domain=%s, node_id=%d, user_id=%d, cluster_ip=%s, port=%d",
			domain, node.ID, user.ID, clusterIP, port)
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return fmt.Errorf("查询域名失败: %w", err)
	}

	// 创建域名记录
	domainRecord := &model.DomainRegistry{
		Domain:      domain,
		Type:        model.DomainTypeK8SSVC,
		UserID:      user.ID,
		NodeID:      node.ID,
		TargetIP:    clusterIP,
		TargetPort:  port,
		Namespace:   namespace,
		ServiceName: serviceName,
	}

	if err := s.db.WithContext(ctx).Create(domainRecord).Error; err != nil {
		return fmt.Errorf("创建域名记录失败: %w", err)
	}

	logger.Infof("创建 Node K8S Service 域名成功: domain=%s, node_id=%d, user_id=%d, cluster_ip=%s, port=%d",
		domain, node.ID, user.ID, clusterIP, port)
	return nil
}

// DeleteNodeK8SSVCDomains 删除 Node 的所有 K8S Service 域名
func (s *DomainService) DeleteNodeK8SSVCDomains(ctx context.Context, nodeID uint64) error {
	result := s.db.WithContext(ctx).Where("node_id = ? AND type = ?", nodeID, model.DomainTypeK8SSVC).
		Delete(&model.DomainRegistry{})

	if result.Error != nil {
		return fmt.Errorf("删除 Node K8S Service 域名失败: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		logger.Infof("删除 Node K8S Service 域名成功: node_id=%d, count=%d", nodeID, result.RowsAffected)
	}

	return nil
}

// CreateEndpointK8SSVCDomain 创建 Endpoint K8S Service 域名
// domain = "{service_name}.{namespace}.{domain_label}.{domain_suffix}"
func (s *DomainService) CreateEndpointK8SSVCDomain(ctx context.Context, endpoint *model.Endpoint, agentNode *model.Node, user *model.User, namespace, serviceName string, ports []int32) error {
	// 生成域名
	label := EffectiveProviderDomainLabel(ctx, s.db, user.ID, user.Name)
	domain := s.providerDomain(ctx, label, serviceName, namespace)

	// 将端口数组序列化为 JSON
	portsJSON := "[]"
	if len(ports) > 0 {
		if data, err := json.Marshal(ports); err == nil {
			portsJSON = string(data)
		}
	}

	// 检查是否已存在（按 domain + user_id + endpoint_id 唯一）
	var existing model.DomainRegistry
	err := s.db.WithContext(ctx).Where("domain = ? AND user_id = ? AND endpoint_id = ?", domain, user.ID, endpoint.Name).First(&existing).Error
	if err == nil {
		// 已存在，更新记录
		updates := map[string]any{
			"target_ip":     agentNode.IP,
			"target_port":   50055, // Endpoint K8SSVC 固定端口
			"service_ports": portsJSON,
		}
		if err := s.db.WithContext(ctx).Model(&existing).Updates(updates).Error; err != nil {
			return fmt.Errorf("更新域名记录失败: %w", err)
		}
		logger.Infof("更新 Endpoint K8S Service 域名: domain=%s, endpoint=%s, node_id=%d, user_id=%d, ports=%v",
			domain, endpoint.Name, agentNode.ID, user.ID, ports)
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return fmt.Errorf("查询域名失败: %w", err)
	}

	// 创建域名记录
	domainRecord := &model.DomainRegistry{
		Domain:       domain,
		Type:         model.DomainTypeK8SSVC,
		UserID:       user.ID,
		NodeID:       agentNode.ID,  // Agent Node ID
		EndpointID:   endpoint.Name, // Endpoint 名称
		TargetIP:     agentNode.IP,  // Agent IP
		TargetPort:   50055,         // Endpoint K8SSVC 固定端口
		Namespace:    namespace,
		ServiceName:  serviceName,
		ServicePorts: portsJSON,
	}

	if err := s.db.WithContext(ctx).Create(domainRecord).Error; err != nil {
		return fmt.Errorf("创建域名记录失败: %w", err)
	}

	logger.Infof("创建 Endpoint K8S Service 域名成功: domain=%s, endpoint=%s, node_id=%d, user_id=%d, ports=%v",
		domain, endpoint.Name, agentNode.ID, user.ID, ports)
	return nil
}

// DeleteEndpointK8SSVCDomains 删除 Endpoint 的所有 K8S Service 域名
func (s *DomainService) DeleteEndpointK8SSVCDomains(ctx context.Context, endpointName string) error {
	result := s.db.WithContext(ctx).Where("endpoint_id = ? AND type = ?", endpointName, model.DomainTypeK8SSVC).
		Delete(&model.DomainRegistry{})

	if result.Error != nil {
		return fmt.Errorf("删除 Endpoint K8S Service 域名失败: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		logger.Infof("删除 Endpoint K8S Service 域名成功: endpoint=%s, count=%d", endpointName, result.RowsAffected)
	}

	return nil
}
