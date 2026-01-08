// Package headscale 提供 Headscale API 客户端和 ACL 同步功能
package headscale

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// ACLSyncService ACL 同步服务
// 负责将数据库中的权限配置同步到 Headscale ACL
type ACLSyncService struct {
	client *Client
	mutex  sync.Mutex
}

// NewACLSyncService 创建 ACL 同步服务
func NewACLSyncService(client *Client) *ACLSyncService {
	return &ACLSyncService{
		client: client,
	}
}

// SyncACL 同步 ACL 规则到 Headscale
// 根据数据库中的权限配置生成 ACL 规则
func (s *ACLSyncService) SyncACL(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger.Info("开始同步 ACL 规则到 Headscale")

	// 生成 ACL 规则
	rules, err := s.generateACLRules()
	if err != nil {
		return fmt.Errorf("生成 ACL 规则失败: %w", err)
	}

	// 构建 ACL 策略
	policy := &ACLPolicy{
		ACLs: rules,
		Groups: map[string][]string{
			"group:agents":   {"tag:agent"},
			"group:desktops": {"tag:desktop"},
		},
		TagOwners: map[string][]string{
			"tag:agent":   {"autogroup:admin"},
			"tag:desktop": {"autogroup:admin"},
		},
	}

	// 设置 ACL 策略
	if err := s.client.SetACLPolicy(ctx, policy); err != nil {
		return fmt.Errorf("设置 ACL 策略失败: %w", err)
	}

	logger.Infof("ACL 规则同步完成，共 %d 条规则", len(rules))
	return nil
}

// generateACLRules 根据数据库配置生成 ACL 规则
func (s *ACLSyncService) generateACLRules() ([]ACLRule, error) {
	var rules []ACLRule

	// 1. 获取所有服务
	var services []model.ProxyService
	if err := db.DB.Preload("Agent").Find(&services).Error; err != nil {
		return nil, fmt.Errorf("查询服务失败: %w", err)
	}

	// 2. 获取所有 Client（Desktop）
	var clients []model.Client
	if err := db.DB.Find(&clients).Error; err != nil {
		return nil, fmt.Errorf("查询 Client 失败: %w", err)
	}

	// 3. 获取所有 Agent
	var agents []model.Agent
	if err := db.DB.Find(&agents).Error; err != nil {
		return nil, fmt.Errorf("查询 Agent 失败: %w", err)
	}

	// 4. 获取所有服务权限
	var servicePerms []model.ServicePermission
	if err := db.DB.Preload("Service").Preload("Service.Agent").Preload("Client").Find(&servicePerms).Error; err != nil {
		return nil, fmt.Errorf("查询服务权限失败: %w", err)
	}

	// 5. 获取所有 Agent 服务权限
	var agentPerms []model.AgentServicePermission
	if err := db.DB.Preload("Service").Preload("Service.Agent").Preload("Agent").Find(&agentPerms).Error; err != nil {
		return nil, fmt.Errorf("查询 Agent 服务权限失败: %w", err)
	}

	// 6. 获取所有组成员
	var groupMembers []model.GroupMember
	if err := db.DB.Find(&groupMembers).Error; err != nil {
		return nil, fmt.Errorf("查询组成员失败: %w", err)
	}

	// 7. 获取所有 Desktop 实例
	var desktopInstances []model.DesktopInstance
	if err := db.DB.Find(&desktopInstances).Error; err != nil {
		return nil, fmt.Errorf("查询 Desktop 实例失败: %w", err)
	}

	// 8. 获取所有 Desktop 服务
	var desktopServices []model.DesktopService
	if err := db.DB.Preload("DesktopInstance").Find(&desktopServices).Error; err != nil {
		return nil, fmt.Errorf("查询 Desktop 服务失败: %w", err)
	}

	// 构建 Client ID -> Tailscale IP 映射
	clientIPMap := make(map[int64]string)
	for _, c := range clients {
		if c.TailscaleIP != "" {
			clientIPMap[c.ID] = c.TailscaleIP
		}
	}

	// 构建 Agent ID -> Tailscale IP 映射
	agentIPMap := make(map[int64]string)
	for _, a := range agents {
		if a.TailscaleIP != "" {
			agentIPMap[a.ID] = a.TailscaleIP
		}
	}

	// 构建 Group ID -> Client IPs 映射
	groupClientIPs := make(map[int64][]string)
	for _, gm := range groupMembers {
		if ip, ok := clientIPMap[gm.ClientID]; ok {
			groupClientIPs[gm.GroupID] = append(groupClientIPs[gm.GroupID], ip)
		}
	}

	// 构建 Agent GroupName -> Agent IPs 映射
	agentGroupIPs := make(map[string][]string)
	for _, a := range agents {
		if a.GroupName != "" && a.TailscaleIP != "" {
			agentGroupIPs[a.GroupName] = append(agentGroupIPs[a.GroupName], a.TailscaleIP)
		}
	}

	// 构建 Client ID -> Desktop Instance IPs 映射
	clientDesktopIPs := make(map[int64][]string)
	for _, di := range desktopInstances {
		if di.TailscaleIP != "" {
			clientDesktopIPs[di.ClientID] = append(clientDesktopIPs[di.ClientID], di.TailscaleIP)
		}
	}

	// 生成服务访问规则
	for _, svc := range services {
		if svc.Agent == nil || svc.Agent.TailscaleIP == "" {
			continue
		}

		dst := fmt.Sprintf("%s:%d", svc.Agent.TailscaleIP, svc.ListenPort)

		switch svc.AccessType {
		case model.AccessTypePublic:
			// public: 所有 Desktop 可访问
			// 使用 100.65.0.0/16 网段（Desktop 网段）
			rules = append(rules, ACLRule{
				Action: "accept",
				Src:    []string{"100.65.0.0/16"},
				Dst:    []string{dst},
			})

		case model.AccessTypePrivate:
			// private: 仅创建者可访问
			if ownerIP, ok := clientIPMap[svc.OwnerID]; ok {
				rules = append(rules, ACLRule{
					Action: "accept",
					Src:    []string{ownerIP},
					Dst:    []string{dst},
				})
			}
			// 也允许创建者的所有 Desktop 实例访问
			if ips, ok := clientDesktopIPs[svc.OwnerID]; ok && len(ips) > 0 {
				rules = append(rules, ACLRule{
					Action: "accept",
					Src:    ips,
					Dst:    []string{dst},
				})
			}

		case model.AccessTypeGroup:
			// group: 指定组成员可访问
			if svc.GroupID != nil {
				if ips, ok := groupClientIPs[*svc.GroupID]; ok && len(ips) > 0 {
					rules = append(rules, ACLRule{
						Action: "accept",
						Src:    ips,
						Dst:    []string{dst},
					})
				}
			}
		}
	}

	// 生成额外授权规则（ServicePermission）
	now := time.Now()
	for _, perm := range servicePerms {
		// 检查是否过期
		if perm.ExpiresAt != nil && now.After(*perm.ExpiresAt) {
			continue
		}

		if perm.Service == nil || perm.Service.Agent == nil || perm.Client == nil {
			continue
		}

		if perm.Service.Agent.TailscaleIP == "" || perm.Client.TailscaleIP == "" {
			continue
		}

		dst := fmt.Sprintf("%s:%d", perm.Service.Agent.TailscaleIP, perm.Service.ListenPort)
		rules = append(rules, ACLRule{
			Action: "accept",
			Src:    []string{perm.Client.TailscaleIP},
			Dst:    []string{dst},
		})

		// 也允许该 Client 的所有 Desktop 实例访问
		if ips, ok := clientDesktopIPs[perm.ClientID]; ok && len(ips) > 0 {
			rules = append(rules, ACLRule{
				Action: "accept",
				Src:    ips,
				Dst:    []string{dst},
			})
		}
	}

	// 生成 Agent 间访问规则
	// 1. 同组 Agent 互访
	for groupName, ips := range agentGroupIPs {
		if len(ips) < 2 {
			continue
		}
		logger.Debugf("生成 Agent 组 %s 互访规则，%d 个 Agent", groupName, len(ips))
		rules = append(rules, ACLRule{
			Action: "accept",
			Src:    ips,
			Dst:    appendPort(ips, "*"),
		})
	}

	// 2. 显式授权的 Agent 访问
	for _, perm := range agentPerms {
		if perm.Service == nil || perm.Service.Agent == nil || perm.Agent == nil {
			continue
		}

		if perm.Service.Agent.TailscaleIP == "" || perm.Agent.TailscaleIP == "" {
			continue
		}

		dst := fmt.Sprintf("%s:%d", perm.Service.Agent.TailscaleIP, perm.Service.ListenPort)
		rules = append(rules, ACLRule{
			Action: "accept",
			Src:    []string{perm.Agent.TailscaleIP},
			Dst:    []string{dst},
		})
	}

	// 生成 Desktop 服务暴露规则
	for _, ds := range desktopServices {
		if ds.DesktopInstance == nil || ds.DesktopInstance.TailscaleIP == "" {
			continue
		}

		dst := fmt.Sprintf("%s:%d", ds.DesktopInstance.TailscaleIP, ds.Port)

		if ds.AllowAll {
			// 允许所有 Desktop 访问
			rules = append(rules, ACLRule{
				Action: "accept",
				Src:    []string{"100.65.0.0/16"},
				Dst:    []string{dst},
			})
		} else if ds.AllowSelf {
			// 仅允许同一 Client 的其他设备访问
			if ips, ok := clientDesktopIPs[ds.DesktopInstance.ClientID]; ok && len(ips) > 0 {
				rules = append(rules, ACLRule{
					Action: "accept",
					Src:    ips,
					Dst:    []string{dst},
				})
			}
		}
	}

	return rules, nil
}

// appendPort 为 IP 列表添加端口
func appendPort(ips []string, port string) []string {
	result := make([]string, len(ips))
	for i, ip := range ips {
		result[i] = fmt.Sprintf("%s:%s", ip, port)
	}
	return result
}

// AddServicePermission 添加服务权限并同步 ACL
func (s *ACLSyncService) AddServicePermission(ctx context.Context, serviceID, clientID, grantedBy int64, expiresAt *time.Time) error {
	// 创建权限记录
	perm := &model.ServicePermission{
		ServiceID: serviceID,
		ClientID:  clientID,
		GrantedBy: grantedBy,
		GrantedAt: time.Now(),
		ExpiresAt: expiresAt,
	}

	if err := db.DB.Create(perm).Error; err != nil {
		return fmt.Errorf("创建权限记录失败: %w", err)
	}

	// 同步 ACL
	if err := s.SyncACL(ctx); err != nil {
		logger.Errorf("同步 ACL 失败: %v", err)
		// 不回滚权限记录，下次同步会修复
	}

	return nil
}

// RemoveServicePermission 删除服务权限并同步 ACL
func (s *ACLSyncService) RemoveServicePermission(ctx context.Context, permissionID int64) error {
	// 删除权限记录
	if err := db.DB.Delete(&model.ServicePermission{}, permissionID).Error; err != nil {
		return fmt.Errorf("删除权限记录失败: %w", err)
	}

	// 同步 ACL
	if err := s.SyncACL(ctx); err != nil {
		logger.Errorf("同步 ACL 失败: %v", err)
	}

	return nil
}

// AddAgentServicePermission 添加 Agent 服务权限并同步 ACL
func (s *ACLSyncService) AddAgentServicePermission(ctx context.Context, agentID, serviceID, grantedBy int64) error {
	// 创建权限记录
	perm := &model.AgentServicePermission{
		AgentID:   agentID,
		ServiceID: serviceID,
		GrantedBy: grantedBy,
		GrantedAt: time.Now(),
	}

	if err := db.DB.Create(perm).Error; err != nil {
		return fmt.Errorf("创建权限记录失败: %w", err)
	}

	// 同步 ACL
	if err := s.SyncACL(ctx); err != nil {
		logger.Errorf("同步 ACL 失败: %v", err)
	}

	return nil
}

// RemoveAgentServicePermission 删除 Agent 服务权限并同步 ACL
func (s *ACLSyncService) RemoveAgentServicePermission(ctx context.Context, permissionID int64) error {
	// 删除权限记录
	if err := db.DB.Delete(&model.AgentServicePermission{}, permissionID).Error; err != nil {
		return fmt.Errorf("删除权限记录失败: %w", err)
	}

	// 同步 ACL
	if err := s.SyncACL(ctx); err != nil {
		logger.Errorf("同步 ACL 失败: %v", err)
	}

	return nil
}

// UpdateServiceAccessType 更新服务访问类型并同步 ACL
func (s *ACLSyncService) UpdateServiceAccessType(ctx context.Context, serviceID int64, accessType string, groupID *int64) error {
	// 更新服务
	updates := map[string]interface{}{
		"access_type": accessType,
		"group_id":    groupID,
	}

	if err := db.DB.Model(&model.ProxyService{}).Where("id = ?", serviceID).Updates(updates).Error; err != nil {
		return fmt.Errorf("更新服务失败: %w", err)
	}

	// 同步 ACL
	if err := s.SyncACL(ctx); err != nil {
		logger.Errorf("同步 ACL 失败: %v", err)
	}

	return nil
}
