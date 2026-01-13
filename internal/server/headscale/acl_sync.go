// Package headscale 提供 Headscale API 客户端和 ACL 同步功能
package headscale

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// ACLPolicy Headscale ACL 策略结构
type ACLPolicy struct {
	Groups    map[string][]string `json:"groups,omitempty"`
	TagOwners map[string][]string `json:"tagOwners,omitempty"`
	ACLs      []ACLRule           `json:"acls,omitempty"`
}

// ACLRule ACL 规则
type ACLRule struct {
	Action string   `json:"action"`
	Src    []string `json:"src"`
	Dst    []string `json:"dst"`
}

// ACLSyncService ACL 同步服务
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
func (s *ACLSyncService) SyncACL(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}

	logger.Info("开始同步 ACL 规则到 Headscale")

	// 生成 ACL 策略
	policy, err := s.generateACLPolicy()
	if err != nil {
		return fmt.Errorf("生成 ACL 策略失败: %w", err)
	}

	// 序列化为 JSON
	policyJSON, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 ACL 策略失败: %w", err)
	}

	// 设置 ACL 策略，带重试机制
	var lastErr error
	for i := 0; i < 3; i++ {
		err := s.client.SetPolicy(ctx, string(policyJSON))
		if err == nil {
			logger.Infof("ACL 规则同步完成，共 %d 条规则", len(policy.ACLs))
			return nil
		}

		lastErr = err
		logger.Warnf("ACL 同步失败 (尝试 %d/3): %v", i+1, err)

		if i < 2 {
			time.Sleep(time.Duration(i+1) * time.Second)
		}
	}

	logger.Errorf("ACL 同步最终失败: %v", lastErr)
	return fmt.Errorf("设置 ACL 策略失败（已重试 3 次）: %w", lastErr)
}

// generateACLPolicy 根据数据库配置生成 ACL 策略
func (s *ACLSyncService) generateACLPolicy() (*ACLPolicy, error) {
	policy := &ACLPolicy{
		Groups:    make(map[string][]string),
		TagOwners: make(map[string][]string),
		ACLs:      []ACLRule{},
	}

	// 生成用户分组
	var clientGroups []model.ClientGroup
	db.DB.Preload("Members").Preload("Members.Client").Find(&clientGroups)

	for _, group := range clientGroups {
		groupName := fmt.Sprintf("group:client-%d", group.ID)
		members := []string{}
		for _, m := range group.Members {
			if m.Client != nil {
				members = append(members, m.Client.Name)
			}
		}
		if len(members) > 0 {
			policy.Groups[groupName] = members
		}
	}

	// 生成代理分组
	var agentGroups []model.AgentGroup
	db.DB.Preload("Members").Preload("Members.Agent").Find(&agentGroups)

	for _, group := range agentGroups {
		groupName := fmt.Sprintf("group:agent-%d", group.ID)
		members := []string{}
		for _, m := range group.Members {
			if m.Agent != nil {
				members = append(members, m.Agent.Name)
			}
		}
		if len(members) > 0 {
			policy.Groups[groupName] = members
		}
	}

	// 生成 ACL 规则
	// 1. 用户直接授权
	var clientPerms []model.ServiceClientPermission
	db.DB.Preload("Service").Preload("Service.Agent").Preload("Client").Find(&clientPerms)

	for _, perm := range clientPerms {
		if perm.Service == nil || perm.Client == nil || perm.Service.Agent == nil {
			continue
		}
		rule := ACLRule{
			Action: "accept",
			Src:    []string{perm.Client.Name},
			Dst:    []string{fmt.Sprintf("%s:%s", perm.Service.Agent.Name, perm.Service.ListenAddr)},
		}
		policy.ACLs = append(policy.ACLs, rule)
	}

	// 2. 用户分组授权
	var clientGroupPerms []model.ServiceClientGroupPermission
	db.DB.Preload("Service").Preload("Service.Agent").Preload("Group").Find(&clientGroupPerms)

	for _, perm := range clientGroupPerms {
		if perm.Service == nil || perm.Group == nil || perm.Service.Agent == nil {
			continue
		}
		groupName := fmt.Sprintf("group:client-%d", perm.Group.ID)
		rule := ACLRule{
			Action: "accept",
			Src:    []string{groupName},
			Dst:    []string{fmt.Sprintf("%s:%s", perm.Service.Agent.Name, perm.Service.ListenAddr)},
		}
		policy.ACLs = append(policy.ACLs, rule)
	}

	// 3. Agent 直接授权
	var agentPerms []model.ServiceAgentPermission
	db.DB.Preload("Service").Preload("Service.Agent").Preload("Agent").Find(&agentPerms)

	for _, perm := range agentPerms {
		if perm.Service == nil || perm.Agent == nil || perm.Service.Agent == nil {
			continue
		}
		rule := ACLRule{
			Action: "accept",
			Src:    []string{perm.Agent.Name},
			Dst:    []string{fmt.Sprintf("%s:%s", perm.Service.Agent.Name, perm.Service.ListenAddr)},
		}
		policy.ACLs = append(policy.ACLs, rule)
	}

	// 4. Agent 分组授权
	var agentGroupPerms []model.ServiceAgentGroupPermission
	db.DB.Preload("Service").Preload("Service.Agent").Preload("Group").Find(&agentGroupPerms)

	for _, perm := range agentGroupPerms {
		if perm.Service == nil || perm.Group == nil || perm.Service.Agent == nil {
			continue
		}
		groupName := fmt.Sprintf("group:agent-%d", perm.Group.ID)
		rule := ACLRule{
			Action: "accept",
			Src:    []string{groupName},
			Dst:    []string{fmt.Sprintf("%s:%s", perm.Service.Agent.Name, perm.Service.ListenAddr)},
		}
		policy.ACLs = append(policy.ACLs, rule)
	}

	return policy, nil
}

// StartPeriodicSync 启动定时全量同步
func (s *ACLSyncService) StartPeriodicSync(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	logger.Info("启动 ACL 定时同步任务，间隔 5 分钟")

	// 立即执行一次同步
	if err := s.SyncACL(ctx); err != nil {
		logger.Errorf("初始 ACL 同步失败: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info("ACL 定时同步任务已停止")
			return
		case <-ticker.C:
			logger.Debug("执行定时 ACL 全量同步")
			if err := s.SyncACL(ctx); err != nil {
				logger.Errorf("定时 ACL 同步失败: %v", err)
			}
		}
	}
}
