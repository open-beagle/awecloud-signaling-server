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

func (s *DomainService) agentDomain(ctx context.Context, identity AgentDomainIdentity, segments ...string) string {
	parts := append(append(make([]string, 0, len(segments)+1), segments...), identity.Namespace())
	return strings.Join(parts, ".") + domainSuffix(ctx, s.db)
}

// CreateNodeSSHDomain 创建 Node SSH 域名
// domain = "{node_name}.{domain_label}.{domain_suffix}"
func (s *DomainService) CreateNodeSSHDomain(ctx context.Context, node *model.Node, user *model.User) error {
	return s.CreateNodeSSHDomainWithUsers(ctx, node, user, nil)
}

// CreateNodeSSHDomainWithUsers 创建或刷新 Node SSH 域名，并保存 Agent 实际可用 SSH 用户。
func (s *DomainService) CreateNodeSSHDomainWithUsers(ctx context.Context, node *model.Node, user *model.User, sshUsers []string) error {
	return s.createNodeSSHDomainWithUsers(ctx, node, user, "", sshUsers)
}

func (s *DomainService) createNodeSSHDomainWithUsers(ctx context.Context, node *model.Node, user *model.User, agentResourceID string, sshUsers []string) error {
	identity, err := ResolveAgentDomainForNodeUserResource(ctx, s.db, node.ID, user.ID, agentResourceID)
	if err != nil {
		return err
	}
	hostLabel, err := NormalizeHostDomainLabel(node.HostDomainLabel)
	if err != nil {
		return err
	}
	domain := s.agentDomain(ctx, identity, hostLabel)

	resourceID := fmt.Sprint(node.ID)
	var existingRecords []model.DomainRegistry
	err = s.db.WithContext(ctx).
		Where("resource_kind = ? AND resource_id = ? AND type = ?", model.DomainResourceNode, resourceID, model.DomainTypeSSH).
		Order("id ASC").Find(&existingRecords).Error
	if err != nil {
		return fmt.Errorf("查询域名失败: %w", err)
	}
	if len(existingRecords) > 0 {
		existingIndex := preferredNodeSSHDomainIndex(existingRecords, domain, identity, user.ID)
		existing := existingRecords[existingIndex]
		existingIDs := make([]int64, 0, len(existingRecords))
		for i := range existingRecords {
			existingIDs = append(existingIDs, existingRecords[i].ID)
		}
		var conflictCount int64
		if err := s.db.WithContext(ctx).Model(&model.DomainRegistry{}).
			Where("lower(domain) = ? AND type = ? AND id NOT IN ?", strings.ToLower(domain), model.DomainTypeSSH, existingIDs).Count(&conflictCount).Error; err != nil {
			return err
		}
		if conflictCount > 0 {
			return ErrHostDomainLabelExists
		}
		updates := map[string]any{
			"domain":            domain,
			"user_id":           user.ID,
			"provider_id":       identity.ProviderID,
			"agent_resource_id": identity.AgentResourceID,
			"resource_kind":     model.DomainResourceNode,
			"resource_id":       resourceID,
			"node_id":           node.ID,
			"target_ip":         node.IP,
			"target_port":       22,
			"status":            model.DomainStatusOnline,
		}
		if sshUsers != nil {
			updates["ssh_users"] = encodeDomainSSHUsers(sshUsers)
		}
		if err := s.db.WithContext(ctx).Model(&existing).Updates(updates).Error; err != nil {
			return fmt.Errorf("更新 Node SSH 域名失败: %w", err)
		}
		if len(existingRecords) > 1 {
			duplicateIDs := make([]int64, 0, len(existingRecords)-1)
			for i := range existingRecords {
				if i == existingIndex {
					continue
				}
				duplicateIDs = append(duplicateIDs, existingRecords[i].ID)
			}
			if err := s.db.WithContext(ctx).Where("id IN ?", duplicateIDs).Delete(&model.DomainRegistry{}).Error; err != nil {
				return fmt.Errorf("清理重复 Node SSH 域名失败: %w", err)
			}
		}
		logger.Infof("Node SSH 域名已存在，已刷新目标地址: domain=%s, target_ip=%s", domain, node.IP)
		return nil
	}
	var conflictCount int64
	if err := s.db.WithContext(ctx).Model(&model.DomainRegistry{}).
		Where("lower(domain) = ? AND type = ?", strings.ToLower(domain), model.DomainTypeSSH).Count(&conflictCount).Error; err != nil {
		return err
	}
	if conflictCount > 0 {
		return ErrHostDomainLabelExists
	}

	// 创建域名记录
	domainRecord := &model.DomainRegistry{
		Domain:          domain,
		Type:            model.DomainTypeSSH,
		UserID:          user.ID,
		ProviderID:      identity.ProviderID,
		AgentResourceID: identity.AgentResourceID,
		ResourceKind:    model.DomainResourceNode,
		ResourceID:      resourceID,
		NodeID:          node.ID,
		TargetIP:        node.IP,
		TargetPort:      22,
	}
	if sshUsers != nil {
		domainRecord.SshUsers = encodeDomainSSHUsers(sshUsers)
	}

	if err := s.db.WithContext(ctx).Create(domainRecord).Error; err != nil {
		return fmt.Errorf("创建域名记录失败: %w", err)
	}

	logger.Infof("创建 Node SSH 域名成功: domain=%s, node_id=%d, user_id=%d", domain, node.ID, user.ID)
	return nil
}

func encodeDomainSSHUsers(users []string) string {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(users))
	for _, user := range users {
		user = strings.TrimSpace(user)
		if user == "" {
			continue
		}
		if _, exists := seen[user]; exists {
			continue
		}
		seen[user] = struct{}{}
		normalized = append(normalized, user)
	}
	if len(normalized) == 0 {
		return "[]"
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	return string(data)
}

func preferredNodeSSHDomainIndex(records []model.DomainRegistry, domain string, identity AgentDomainIdentity, userID uint64) int {
	bestIndex := 0
	bestScore := -1
	for i := range records {
		score := 0
		record := records[i]
		if strings.EqualFold(record.Domain, domain) {
			score += 100
		}
		if record.ProviderID == identity.ProviderID {
			score += 20
		}
		if record.AgentResourceID == identity.AgentResourceID {
			score += 20
		}
		if record.UserID == userID {
			score += 10
		}
		if record.Status == model.DomainStatusOnline {
			score += 10
		}
		if hasDomainSSHUsers(record.SshUsers) {
			score += 5
		}
		if score > bestScore {
			bestScore = score
			bestIndex = i
		}
	}
	return bestIndex
}

func hasDomainSSHUsers(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value == "[]" {
		return false
	}
	var users []string
	if err := json.Unmarshal([]byte(value), &users); err != nil {
		return true
	}
	for _, user := range users {
		if strings.TrimSpace(user) != "" {
			return true
		}
	}
	return false
}

// CreateNodeK8SAPIDomain 创建 Node K8S API 域名
// domain = "kubernetes.{domain_label}.{domain_suffix}"
func (s *DomainService) CreateNodeK8SAPIDomain(ctx context.Context, node *model.Node, user *model.User) error {
	identity, err := ResolveAgentDomainForNode(ctx, s.db, node.ID)
	if err != nil {
		return err
	}
	domain := s.agentDomain(ctx, identity, "kubernetes")

	// 检查是否已存在
	var existing model.DomainRegistry
	err = s.db.WithContext(ctx).Where("domain = ? AND resource_kind = ? AND resource_id = ?", domain, model.DomainResourceKubernetes, fmt.Sprint(node.ID)).First(&existing).Error
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
		Domain:          domain,
		Type:            model.DomainTypeK8SAPI,
		UserID:          user.ID,
		ProviderID:      identity.ProviderID,
		AgentResourceID: identity.AgentResourceID,
		ResourceKind:    model.DomainResourceKubernetes,
		ResourceID:      fmt.Sprint(node.ID),
		NodeID:          node.ID,
		TargetIP:        node.IP,
		TargetPort:      port,
	}

	if err := s.db.WithContext(ctx).Create(domainRecord).Error; err != nil {
		return fmt.Errorf("创建域名记录失败: %w", err)
	}

	logger.Infof("创建 Node K8S API 域名成功: domain=%s, node_id=%d, user_id=%d, port=%d", domain, node.ID, user.ID, port)
	return nil
}

// DeleteNodeSSHDomain 删除 Node SSH 域名
func (s *DomainService) DeleteNodeSSHDomain(ctx context.Context, node *model.Node, user *model.User) error {
	result := s.db.WithContext(ctx).Where("resource_kind = ? AND resource_id = ? AND type = ?", model.DomainResourceNode, fmt.Sprint(node.ID), model.DomainTypeSSH).
		Delete(&model.DomainRegistry{})

	if result.Error != nil {
		return fmt.Errorf("删除域名记录失败: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		logger.Infof("删除 Node SSH 域名成功: node_id=%d, user_id=%d", node.ID, user.ID)
	}

	return nil
}

// DeleteNodeK8SAPIDomain 删除 Node K8S API 域名
func (s *DomainService) DeleteNodeK8SAPIDomain(ctx context.Context, node *model.Node, user *model.User) error {
	result := s.db.WithContext(ctx).Where("resource_kind = ? AND resource_id = ? AND type = ?", model.DomainResourceKubernetes, fmt.Sprint(node.ID), model.DomainTypeK8SAPI).
		Delete(&model.DomainRegistry{})

	if result.Error != nil {
		return fmt.Errorf("删除域名记录失败: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		logger.Infof("删除 Node K8S API 域名成功: node_id=%d, user_id=%d", node.ID, user.ID)
	}

	return nil
}

// DeleteNodeAllDomains 删除 Node 的所有域名
func (s *DomainService) DeleteNodeAllDomains(ctx context.Context, nodeID uint64) error {
	result := s.db.WithContext(ctx).
		Where("resource_id = ? AND resource_kind IN ?", fmt.Sprint(nodeID), []model.DomainResourceKind{
			model.DomainResourceNode, model.DomainResourceKubernetes, model.DomainResourceService,
		}).Delete(&model.DomainRegistry{})

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
	identity, err := ResolveAgentDomainForEndpoint(ctx, s.db, endpoint.ID)
	if err != nil {
		return err
	}
	hostLabel, err := NormalizeHostDomainLabel(endpoint.HostDomainLabel)
	if err != nil {
		return err
	}
	domain := s.agentDomain(ctx, identity, hostLabel)

	// 检查是否已存在
	var existing model.DomainRegistry
	err = s.db.WithContext(ctx).Where("domain = ? AND resource_kind = ? AND resource_id = ?", domain, model.DomainResourceEndpoint, endpoint.ID).First(&existing).Error
	if err == nil {
		logger.Infof("Endpoint SSH 域名已存在，跳过创建: domain=%s", domain)
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return fmt.Errorf("查询域名失败: %w", err)
	}
	var conflictCount int64
	if err := s.db.WithContext(ctx).Model(&model.DomainRegistry{}).
		Where("lower(domain) = ? AND type = ?", strings.ToLower(domain), model.DomainTypeSSH).Count(&conflictCount).Error; err != nil {
		return err
	}
	if conflictCount > 0 {
		return ErrHostDomainLabelExists
	}

	// 创建域名记录（使用 Endpoint 预分配的端口）
	domainRecord := &model.DomainRegistry{
		Domain:          domain,
		Type:            model.DomainTypeSSH,
		UserID:          user.ID,
		ProviderID:      identity.ProviderID,
		AgentResourceID: identity.AgentResourceID,
		ResourceKind:    model.DomainResourceEndpoint,
		ResourceID:      endpoint.ID,
		NodeID:          agentNode.ID, // Agent Node ID
		EndpointID:      endpoint.ID,
		TargetIP:        agentNode.IP,          // Agent IP
		TargetPort:      int(endpoint.SSHPort), // 从 Endpoint 表读取端口
	}

	if err := s.db.WithContext(ctx).Create(domainRecord).Error; err != nil {
		return fmt.Errorf("创建域名记录失败: %w", err)
	}

	logger.Infof("创建 Endpoint SSH 域名成功: domain=%s, endpoint=%s, node_id=%d, user_id=%d",
		domain, endpoint.Name, agentNode.ID, user.ID)
	return nil
}

// DeleteEndpointSSHDomain 删除 Endpoint SSH 域名
func (s *DomainService) DeleteEndpointSSHDomain(ctx context.Context, endpointID string, user *model.User) error {
	result := s.db.WithContext(ctx).Where("resource_kind = ? AND resource_id = ? AND type = ?", model.DomainResourceEndpoint, endpointID, model.DomainTypeSSH).
		Delete(&model.DomainRegistry{})

	if result.Error != nil {
		return fmt.Errorf("删除域名记录失败: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		logger.Infof("删除 Endpoint SSH 域名成功: endpoint=%s, user_id=%d", endpointID, user.ID)
	}

	return nil
}

// DeleteEndpointAllDomains 删除 Endpoint 的所有域名
func (s *DomainService) DeleteEndpointAllDomains(ctx context.Context, endpointID string) error {
	result := s.db.WithContext(ctx).Where("resource_kind = ? AND resource_id = ?", model.DomainResourceEndpoint, endpointID).Delete(&model.DomainRegistry{})

	if result.Error != nil {
		return fmt.Errorf("删除 Endpoint 所有域名失败: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		logger.Infof("删除 Endpoint 所有域名成功: endpoint=%s, count=%d", endpointID, result.RowsAffected)
	}

	return nil
}

// CreateEndpointK8SAPIDomain 创建 Endpoint K8S API 域名
// domain = "kubernetes.{domain_label}.{domain_suffix}"
// 注意：Endpoint K8S API 域名格式和 Node K8S API 一样，通过 endpoint_id 字段区分
func (s *DomainService) CreateEndpointK8SAPIDomain(ctx context.Context, endpoint *model.Endpoint, agentNode *model.Node, user *model.User) error {
	identity, err := ResolveAgentDomainForEndpoint(ctx, s.db, endpoint.ID)
	if err != nil {
		return err
	}
	hostLabel, err := NormalizeHostDomainLabel(endpoint.HostDomainLabel)
	if err != nil {
		return err
	}
	domain := s.agentDomain(ctx, identity, "kubernetes", hostLabel)

	// 检查是否已存在（按 endpoint_id 区分）
	var existing model.DomainRegistry
	err = s.db.WithContext(ctx).Where("domain = ? AND resource_kind = ? AND resource_id = ?", domain, model.DomainResourceEndpoint, endpoint.ID).First(&existing).Error
	if err == nil {
		logger.Infof("Endpoint K8S API 域名已存在，跳过创建: domain=%s, endpoint=%s", domain, endpoint.Name)
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return fmt.Errorf("查询域名失败: %w", err)
	}

	// 创建域名记录（使用 Endpoint 预分配的端口）
	domainRecord := &model.DomainRegistry{
		Domain:          domain,
		Type:            model.DomainTypeK8SAPI,
		UserID:          user.ID,
		ProviderID:      identity.ProviderID,
		AgentResourceID: identity.AgentResourceID,
		ResourceKind:    model.DomainResourceEndpoint,
		ResourceID:      endpoint.ID,
		NodeID:          agentNode.ID, // Agent Node ID
		EndpointID:      endpoint.ID,
		TargetIP:        agentNode.IP,             // Agent IP
		TargetPort:      int(endpoint.K8SAPIPort), // 从 Endpoint 表读取端口
	}

	if err := s.db.WithContext(ctx).Create(domainRecord).Error; err != nil {
		return fmt.Errorf("创建域名记录失败: %w", err)
	}

	logger.Infof("创建 Endpoint K8S API 域名成功: domain=%s, endpoint=%s, node_id=%d, user_id=%d",
		domain, endpoint.Name, agentNode.ID, user.ID)
	return nil
}

// DeleteEndpointK8SAPIDomain 删除 Endpoint K8S API 域名
func (s *DomainService) DeleteEndpointK8SAPIDomain(ctx context.Context, endpointID string, user *model.User) error {
	result := s.db.WithContext(ctx).Where("resource_kind = ? AND resource_id = ? AND type = ?", model.DomainResourceEndpoint, endpointID, model.DomainTypeK8SAPI).
		Delete(&model.DomainRegistry{})

	if result.Error != nil {
		return fmt.Errorf("删除域名记录失败: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		logger.Infof("删除 Endpoint K8S API 域名成功: endpoint=%s, user_id=%d", endpointID, user.ID)
	}

	return nil
}

// CreateNodeK8SSVCDomain 创建 Node K8S Service 域名
// domain = "{service_name}.{namespace}.{domain_label}.{domain_suffix}"
func (s *DomainService) CreateNodeK8SSVCDomain(ctx context.Context, node *model.Node, user *model.User, namespace, serviceName, clusterIP string, port int) error {
	identity, err := ResolveAgentDomainForNode(ctx, s.db, node.ID)
	if err != nil {
		return err
	}
	domain := s.agentDomain(ctx, identity, serviceName, namespace)

	// 检查是否已存在
	var existing model.DomainRegistry
	err = s.db.WithContext(ctx).Where("domain = ? AND resource_kind = ? AND resource_id = ?", domain, model.DomainResourceService, fmt.Sprint(node.ID)).First(&existing).Error
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
		Domain:          domain,
		Type:            model.DomainTypeK8SSVC,
		UserID:          user.ID,
		ProviderID:      identity.ProviderID,
		AgentResourceID: identity.AgentResourceID,
		ResourceKind:    model.DomainResourceService,
		ResourceID:      fmt.Sprint(node.ID),
		NodeID:          node.ID,
		TargetIP:        clusterIP,
		TargetPort:      port,
		Namespace:       namespace,
		ServiceName:     serviceName,
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
	identity, err := ResolveAgentDomainForEndpoint(ctx, s.db, endpoint.ID)
	if err != nil {
		return err
	}
	hostLabel, err := NormalizeHostDomainLabel(endpoint.HostDomainLabel)
	if err != nil {
		return err
	}
	domain := s.agentDomain(ctx, identity, serviceName, namespace, hostLabel)

	// 将端口数组序列化为 JSON
	portsJSON := "[]"
	if len(ports) > 0 {
		if data, err := json.Marshal(ports); err == nil {
			portsJSON = string(data)
		}
	}

	// 检查是否已存在（按 domain + user_id + endpoint_id 唯一）
	var existing model.DomainRegistry
	err = s.db.WithContext(ctx).Where("domain = ? AND resource_kind = ? AND resource_id = ?", domain, model.DomainResourceEndpoint, endpoint.ID).First(&existing).Error
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
		Domain:          domain,
		Type:            model.DomainTypeK8SSVC,
		UserID:          user.ID,
		ProviderID:      identity.ProviderID,
		AgentResourceID: identity.AgentResourceID,
		ResourceKind:    model.DomainResourceEndpoint,
		ResourceID:      endpoint.ID,
		NodeID:          agentNode.ID, // Agent Node ID
		EndpointID:      endpoint.ID,
		TargetIP:        agentNode.IP, // Agent IP
		TargetPort:      50055,        // Endpoint K8SSVC 固定端口
		Namespace:       namespace,
		ServiceName:     serviceName,
		ServicePorts:    portsJSON,
	}

	if err := s.db.WithContext(ctx).Create(domainRecord).Error; err != nil {
		return fmt.Errorf("创建域名记录失败: %w", err)
	}

	logger.Infof("创建 Endpoint K8S Service 域名成功: domain=%s, endpoint=%s, node_id=%d, user_id=%d, ports=%v",
		domain, endpoint.Name, agentNode.ID, user.ID, ports)
	return nil
}

// DeleteEndpointK8SSVCDomains 删除 Endpoint 的所有 K8S Service 域名
func (s *DomainService) DeleteEndpointK8SSVCDomains(ctx context.Context, endpointID string) error {
	result := s.db.WithContext(ctx).Where("resource_kind = ? AND resource_id = ? AND type = ?", model.DomainResourceEndpoint, endpointID, model.DomainTypeK8SSVC).
		Delete(&model.DomainRegistry{})

	if result.Error != nil {
		return fmt.Errorf("删除 Endpoint K8S Service 域名失败: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		logger.Infof("删除 Endpoint K8S Service 域名成功: endpoint=%s, count=%d", endpointID, result.RowsAffected)
	}

	return nil
}

func (s *DomainService) UpdateNodeHostDomainLabel(ctx context.Context, nodeID uint64, value string) error {
	label, err := NormalizeHostDomainLabel(value)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return updateNodeHostDomainLabel(ctx, tx, nodeID, label)
	})
}

func updateNodeHostDomainLabel(ctx context.Context, tx *gorm.DB, nodeID uint64, label string) error {
	return updateNodeHostDomainLabelForAgent(ctx, tx, nodeID, label, "")
}

func updateNodeHostDomainLabelForAgent(ctx context.Context, tx *gorm.DB, nodeID uint64, label, agentResourceID string) error {
	var node model.Node
	if err := tx.First(&node, nodeID).Error; err != nil {
		return err
	}
	identity, err := ResolveAgentDomainForNodeUserResource(ctx, tx, nodeID, node.UserID, agentResourceID)
	if err != nil {
		return err
	}
	var agent model.TechnicalResource
	if err := tx.Select("runtime_user_id").First(&agent, "id = ?", identity.AgentResourceID).Error; err != nil {
		return err
	}
	var nodeCount, endpointCount int64
	if err := tx.Model(&model.Node{}).Where("user_id = ? AND type = ? AND lower(host_domain_label) = ? AND id <> ?", agent.RuntimeUserID, model.NodeTypeAgent, label, nodeID).Count(&nodeCount).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.Endpoint{}).Where("user_id = ? AND revoked = ? AND lower(host_domain_label) = ?", agent.RuntimeUserID, false, label).Count(&endpointCount).Error; err != nil {
		return err
	}
	if nodeCount+endpointCount > 0 {
		return ErrHostDomainLabelExists
	}
	if err := tx.Model(&node).Update("host_domain_label", label).Error; err != nil {
		return err
	}
	node.HostDomainLabel = label
	var user model.User
	if err := tx.First(&user, node.UserID).Error; err != nil {
		return err
	}
	domains := NewDomainService(tx)
	node.HostDomainLabel = label
	if !user.SSHEnabled {
		return domains.DeleteNodeSSHDomain(ctx, &node, &user)
	}
	return domains.createNodeSSHDomainWithUsers(ctx, &node, &user, agentResourceID, nil)
}

func (s *DomainService) UpdateEndpointHostDomainLabel(ctx context.Context, endpointID, value string) error {
	label, err := NormalizeHostDomainLabel(value)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		identity, err := ResolveAgentDomainForEndpoint(ctx, tx, endpointID)
		if err != nil {
			return err
		}
		var agent model.TechnicalResource
		if err := tx.Select("runtime_user_id").First(&agent, "id = ?", identity.AgentResourceID).Error; err != nil {
			return err
		}
		var nodeCount, endpointCount int64
		if err := tx.Model(&model.Node{}).Where("user_id = ? AND type = ? AND lower(host_domain_label) = ?", agent.RuntimeUserID, model.NodeTypeAgent, label).Count(&nodeCount).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Endpoint{}).Where("user_id = ? AND revoked = ? AND lower(host_domain_label) = ? AND id <> ?", agent.RuntimeUserID, false, label, endpointID).Count(&endpointCount).Error; err != nil {
			return err
		}
		if nodeCount+endpointCount > 0 {
			return ErrHostDomainLabelExists
		}
		var endpoint model.Endpoint
		if err := tx.First(&endpoint, "id = ?", endpointID).Error; err != nil {
			return err
		}
		if err := tx.Model(&endpoint).Update("host_domain_label", label).Error; err != nil {
			return err
		}
		var user model.User
		if err := tx.First(&user, endpoint.UserID).Error; err != nil {
			return err
		}
		domains := NewDomainService(tx)
		if err := domains.DeleteEndpointSSHDomain(ctx, endpoint.ID, &user); err != nil {
			return err
		}
		if !endpoint.SSHEnabled {
			return nil
		}
		var agentNode model.Node
		if err := tx.Where("user_id = ? AND type = ?", endpoint.UserID, model.NodeTypeAgent).Order("last_heartbeat DESC").First(&agentNode).Error; err != nil {
			return err
		}
		endpoint.HostDomainLabel = label
		return domains.CreateEndpointSSHDomain(ctx, &endpoint, &agentNode, &user)
	})
}
