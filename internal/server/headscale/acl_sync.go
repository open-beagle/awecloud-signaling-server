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
// 设计文档: docs/design_headscale_integration.md 第 10 节
// Tag 格式:
//   - 身份 Tag: tag:agent-{name} / tag:desktop-{name}
//   - 分组 Tag: tag:agent-group-{group.name} / tag:desktop-group-{group.name}
func (s *ACLSyncService) generateACLPolicy() (*ACLPolicy, error) {
	policy := &ACLPolicy{
		Groups:    make(map[string][]string),
		TagOwners: make(map[string][]string),
		ACLs:      []ACLRule{},
	}

	// 收集所有用到的 Tag，用于生成 tagOwners
	// Headscale 要求 ACL 中引用的 Tag 必须在 tagOwners 中定义
	usedTags := make(map[string]bool)

	// 生成代理分组的同组互访规则
	// 业务 1: Agent 同组互访 - src: tag:agent-group-{name}, dst: tag:agent-group-{name}:*
	var agentGroups []model.AgentGroup
	db.DB.Find(&agentGroups)

	for _, group := range agentGroups {
		tagName := fmt.Sprintf("tag:agent-group-%s", group.Name)
		usedTags[tagName] = true

		// Agent 同组互访规则
		rule := ACLRule{
			Action: "accept",
			Src:    []string{tagName},
			Dst:    []string{fmt.Sprintf("%s:*", tagName)},
		}
		policy.ACLs = append(policy.ACLs, rule)
	}

	// 生成 Desktop 分组的 Tag
	var desktopGroups []model.ClientGroup
	db.DB.Find(&desktopGroups)
	for _, group := range desktopGroups {
		tagName := fmt.Sprintf("tag:desktop-group-%s", group.Name)
		usedTags[tagName] = true
	}

	// 生成所有 Agent 的身份 Tag
	var agents []model.Agent
	db.DB.Find(&agents)
	for _, agent := range agents {
		tagName := fmt.Sprintf("tag:agent-%s", agent.Name)
		usedTags[tagName] = true
	}

	// 生成所有 Desktop/Client 的身份 Tag
	var clients []model.Client
	db.DB.Find(&clients)
	for _, client := range clients {
		tagName := fmt.Sprintf("tag:desktop-%s", client.Name)
		usedTags[tagName] = true
	}

	// 生成 ACL 规则
	// 1. Desktop 分组授权 -> Agent（基于 Tag）
	var desktopGroupPerms []model.ServiceClientGroupPermission
	db.DB.Preload("Service").Preload("Service.Agent").Preload("Group").Find(&desktopGroupPerms)

	// 按 DesktopGroup -> 目标 聚合，避免重复规则
	desktopToTargetPerms := make(map[string]map[string]bool)
	for _, perm := range desktopGroupPerms {
		if perm.Service == nil || perm.Group == nil || perm.Service.Agent == nil {
			continue
		}

		// 使用 desktop-group 格式
		desktopGroupTag := fmt.Sprintf("tag:desktop-group-%s", perm.Group.Name)
		usedTags[desktopGroupTag] = true

		if desktopToTargetPerms[desktopGroupTag] == nil {
			desktopToTargetPerms[desktopGroupTag] = make(map[string]bool)
		}

		// 查找 Agent 所属的分组
		var agentGroupMembers []model.AgentGroupMember
		db.DB.Preload("Group").Where("agent_id = ?", perm.Service.AgentID).Find(&agentGroupMembers)

		if len(agentGroupMembers) > 0 {
			// Agent 在分组中，授权到分组 Tag
			for _, agm := range agentGroupMembers {
				if agm.Group != nil {
					agentGroupTag := fmt.Sprintf("tag:agent-group-%s", agm.Group.Name)
					usedTags[agentGroupTag] = true
					desktopToTargetPerms[desktopGroupTag][agentGroupTag] = true
				}
			}
		} else {
			// Agent 不在任何分组，使用身份 Tag
			// 业务 4: 单个 Agent 端口给 Desktop 组 - dst: tag:agent-{agent.name}:{port}
			port := extractPort(perm.Service.ListenAddr)
			if port != "" {
				agentTag := fmt.Sprintf("tag:agent-%s", perm.Service.Agent.Name)
				usedTags[agentTag] = true
				dst := fmt.Sprintf("%s:%s", agentTag, port)
				desktopToTargetPerms[desktopGroupTag][dst] = true
			}
		}
	}

	// 生成 Desktop 分组 -> Agent 的 ACL 规则
	for desktopGroupTag, targets := range desktopToTargetPerms {
		for target := range targets {
			dst := target
			// 如果是分组 Tag 且没有端口，添加 :* 后缀
			if isTagFormat(target) && !containsPort(target) {
				dst = fmt.Sprintf("%s:*", target)
			}
			rule := ACLRule{
				Action: "accept",
				Src:    []string{desktopGroupTag},
				Dst:    []string{dst},
			}
			policy.ACLs = append(policy.ACLs, rule)
		}
	}

	// 2. Agent 分组授权 -> Agent（基于 Tag）
	var agentGroupPerms []model.ServiceAgentGroupPermission
	db.DB.Preload("Service").Preload("Service.Agent").Preload("Group").Find(&agentGroupPerms)

	// 按 AgentGroup -> 目标 聚合
	agentToTargetPerms := make(map[string]map[string]bool)
	for _, perm := range agentGroupPerms {
		if perm.Service == nil || perm.Group == nil || perm.Service.Agent == nil {
			continue
		}

		srcGroupTag := fmt.Sprintf("tag:agent-group-%s", perm.Group.Name)
		usedTags[srcGroupTag] = true

		if agentToTargetPerms[srcGroupTag] == nil {
			agentToTargetPerms[srcGroupTag] = make(map[string]bool)
		}

		// 查找目标 Agent 所属的分组
		var agentGroupMembers []model.AgentGroupMember
		db.DB.Preload("Group").Where("agent_id = ?", perm.Service.AgentID).Find(&agentGroupMembers)

		if len(agentGroupMembers) > 0 {
			for _, agm := range agentGroupMembers {
				if agm.Group != nil {
					dstGroupTag := fmt.Sprintf("tag:agent-group-%s", agm.Group.Name)
					usedTags[dstGroupTag] = true
					agentToTargetPerms[srcGroupTag][dstGroupTag] = true
				}
			}
		} else {
			// 目标 Agent 不在任何分组，使用身份 Tag
			port := extractPort(perm.Service.ListenAddr)
			if port != "" {
				agentTag := fmt.Sprintf("tag:agent-%s", perm.Service.Agent.Name)
				usedTags[agentTag] = true
				dst := fmt.Sprintf("%s:%s", agentTag, port)
				agentToTargetPerms[srcGroupTag][dst] = true
			}
		}
	}

	// 生成 Agent 分组 -> Agent 的 ACL 规则
	for srcGroupTag, targets := range agentToTargetPerms {
		for target := range targets {
			dst := target
			if isTagFormat(target) && !containsPort(target) {
				dst = fmt.Sprintf("%s:*", target)
			}
			rule := ACLRule{
				Action: "accept",
				Src:    []string{srcGroupTag},
				Dst:    []string{dst},
			}
			policy.ACLs = append(policy.ACLs, rule)
		}
	}

	// 生成 tagOwners - Headscale 要求 ACL 中使用的 Tag 必须在 tagOwners 中定义
	// 使用空数组表示由 Headscale 管理员管理
	for tag := range usedTags {
		policy.TagOwners[tag] = []string{}
	}

	return policy, nil
}

// extractPort 从地址中提取端口号
func extractPort(addr string) string {
	if addr == "" {
		return "*"
	}
	// 处理格式: "100.64.0.1:3306" 或 ":3306" 或 "3306"
	if idx := len(addr) - 1; idx >= 0 {
		for i := idx; i >= 0; i-- {
			if addr[i] == ':' {
				return addr[i+1:]
			}
		}
	}
	// 如果没有冒号，可能整个就是端口号
	return addr
}

// isTagFormat 检查字符串是否是 Tag 格式
func isTagFormat(s string) bool {
	return len(s) > 4 && s[:4] == "tag:"
}

// containsPort 检查字符串是否包含端口号（以 :数字 或 :* 结尾）
func containsPort(s string) bool {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return true
		}
	}
	return false
}

// StartPeriodicSync 启动定时全量同步
func (s *ACLSyncService) StartPeriodicSync(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	logger.Info("启动 ACL 定时同步任务，间隔 5 分钟")

	// 启动时先同步所有 Node 的 Tag
	if err := s.SyncAllNodeTags(ctx); err != nil {
		logger.Errorf("初始 Node Tag 同步失败: %v", err)
	}

	// 然后同步 ACL 规则
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
			if err := s.SyncAllNodeTags(ctx); err != nil {
				logger.Errorf("定时 Node Tag 同步失败: %v", err)
			}
			if err := s.SyncACL(ctx); err != nil {
				logger.Errorf("定时 ACL 同步失败: %v", err)
			}
		}
	}
}

// SyncAllNodeTags 同步所有 Node 的 Tag
// Server 启动时调用，确保所有 Agent 和 Desktop 的 Node 都有正确的 Tag
func (s *ACLSyncService) SyncAllNodeTags(ctx context.Context) error {
	logger.Info("开始同步所有 Node 的 Tag")

	// 先获取所有 Headscale Node，建立 User 名称到 Node 的映射
	nodes, err := s.client.ListNodes(ctx)
	if err != nil {
		logger.Warnf("获取 Headscale Node 列表失败: %v", err)
		return err
	}

	// 建立 User 名称 -> Node 的映射
	userNodeMap := make(map[string]*struct {
		NodeID uint64
		IP     string
		Tags   []string
	})
	for _, node := range nodes {
		if node.User != nil {
			ip := ""
			if len(node.IpAddresses) > 0 {
				ip = node.IpAddresses[0]
			}
			userNodeMap[node.User.Name] = &struct {
				NodeID uint64
				IP     string
				Tags   []string
			}{
				NodeID: node.Id,
				IP:     ip,
				Tags:   node.ForcedTags,
			}
		}
	}

	// 同步所有 Agent 的 Tag
	var agents []model.Agent
	db.DB.Find(&agents)
	logger.Infof("找到 %d 个 Agent", len(agents))

	for _, agent := range agents {
		// 通过 User 名称查找 Node（User 名称格式: agent-{agent.name}）
		userName := "agent-" + agent.Name
		nodeInfo, found := userNodeMap[userName]

		if !found {
			logger.Warnf("Agent %s 在 Headscale 中没有对应的 Node (User: %s)", agent.Name, userName)
			continue
		}

		// 如果数据库中的 NodeID 为空，更新它
		if agent.NodeID == 0 || agent.NodeID != nodeInfo.NodeID {
			agent.NodeID = nodeInfo.NodeID
			if nodeInfo.IP != "" {
				agent.IP = nodeInfo.IP
			}
			if err := db.DB.Save(&agent).Error; err != nil {
				logger.Warnf("更新 Agent %s NodeID 失败: %v", agent.Name, err)
			} else {
				logger.Infof("Agent %s NodeID 已更新为 %d, IP: %s", agent.Name, nodeInfo.NodeID, nodeInfo.IP)
			}
		}

		// 构建 Agent 应该有的 Tag 列表
		expectedTags := []string{
			fmt.Sprintf("tag:agent-%s", agent.Name), // 身份 Tag
		}

		// 查询 Agent 所属的分组
		var groupMembers []model.AgentGroupMember
		db.DB.Preload("Group").Where("agent_id = ?", agent.ID).Find(&groupMembers)
		for _, gm := range groupMembers {
			if gm.Group != nil {
				expectedTags = append(expectedTags, fmt.Sprintf("tag:agent-group-%s", gm.Group.Name))
			}
		}

		logger.Infof("Agent %s (NodeID=%d) 当前 Tag: %v, 期望 Tag: %v", agent.Name, nodeInfo.NodeID, nodeInfo.Tags, expectedTags)

		// 检查是否需要更新
		if !tagsEqual(nodeInfo.Tags, expectedTags) {
			if err := s.client.SetTags(ctx, nodeInfo.NodeID, expectedTags); err != nil {
				logger.Warnf("设置 Agent %s Tag 失败: %v", agent.Name, err)
			} else {
				logger.Infof("Agent %s Tag 已同步: %v", agent.Name, expectedTags)
			}
		} else {
			logger.Infof("Agent %s Tag 已是最新，无需更新", agent.Name)
		}
	}

	// 同步所有 Desktop 的 Tag
	var desktops []model.Desktop
	db.DB.Find(&desktops)

	for _, desktop := range desktops {
		if desktop.ID == 0 {
			continue
		}

		// 查询 Client 信息
		var client model.Client
		if err := db.DB.First(&client, desktop.ClientID).Error; err != nil {
			logger.Warnf("获取 Desktop %d 的 Client 失败: %v", desktop.ID, err)
			continue
		}

		// 构建 Desktop 应该有的 Tag 列表
		expectedTags := []string{
			fmt.Sprintf("tag:desktop-%s", client.Name), // 身份 Tag
		}

		// 查询 Client 所属的分组
		var groupMembers []model.ClientGroupMember
		db.DB.Preload("Group").Where("client_id = ?", desktop.ClientID).Find(&groupMembers)
		for _, gm := range groupMembers {
			if gm.Group != nil {
				expectedTags = append(expectedTags, fmt.Sprintf("tag:desktop-group-%s", gm.Group.Name))
			}
		}

		// 获取当前 Node 的 Tag
		node, err := s.client.GetNode(ctx, desktop.ID)
		if err != nil {
			logger.Warnf("获取 Desktop %d 失败: %v", desktop.ID, err)
			continue
		}

		// 检查是否需要更新
		if !tagsEqual(node.ForcedTags, expectedTags) {
			if err := s.client.SetTags(ctx, desktop.ID, expectedTags); err != nil {
				logger.Warnf("设置 Desktop %d Tag 失败: %v", desktop.ID, err)
			} else {
				logger.Infof("Desktop %d Tag 已同步: %v", desktop.ID, expectedTags)
			}
		}
	}

	logger.Info("Node Tag 同步完成")
	return nil
}

// tagsEqual 比较两个 Tag 列表是否相等（忽略顺序）
func tagsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aMap := make(map[string]bool)
	for _, t := range a {
		aMap[t] = true
	}
	for _, t := range b {
		if !aMap[t] {
			return false
		}
	}
	return true
}
