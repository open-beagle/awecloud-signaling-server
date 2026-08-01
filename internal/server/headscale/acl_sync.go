// Package headscale 提供 Headscale API 客户端和 ACL 同步功能
package headscale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// ACLPolicy Headscale ACL 策略结构
type ACLPolicy struct {
	Groups    map[string][]string `json:"groups,omitempty"`
	TagOwners map[string][]string `json:"tagOwners,omitempty"`
	ACLs      []ACLRule           `json:"acls,omitempty"`
	SSH       []SSHRule           `json:"ssh,omitempty"`
}

// ACLRule ACL 规则
type ACLRule struct {
	Action string   `json:"action"`
	Src    []string `json:"src"`
	Dst    []string `json:"dst"`
}

// SSHRule SSH 访问规则
type SSHRule struct {
	Action string   `json:"action"`
	Src    []string `json:"src"`
	Dst    []string `json:"dst"`
	Users  []string `json:"users,omitempty"`
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

	policy, err := s.generateACLPolicy(ctx)
	if err != nil {
		return fmt.Errorf("生成 ACL 策略失败: %w", err)
	}
	policy, err = s.mergeStoredACLPolicyBaseline(ctx, policy)
	if err != nil {
		return fmt.Errorf("合并旧 ACL 基线失败: %w", err)
	}

	policyJSON, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 ACL 策略失败: %w", err)
	}

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

func (s *ACLSyncService) mergeStoredACLPolicyBaseline(ctx context.Context, generated *ACLPolicy) (*ACLPolicy, error) {
	var config model.SystemConfig
	err := db.DB.WithContext(ctx).Where("key = ?", model.ConfigHeadscaleACLBaseline).First(&config).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return generated, nil
		}
		return nil, err
	}
	var baseline ACLPolicy
	if err := json.Unmarshal([]byte(config.Value), &baseline); err != nil {
		return nil, fmt.Errorf("baseline JSON 无效: %w", err)
	}
	return mergeACLPolicies(&baseline, generated)
}

func mergeACLPolicies(baseline, generated *ACLPolicy) (*ACLPolicy, error) {
	if baseline == nil || generated == nil {
		return nil, fmt.Errorf("baseline 和 generated policy 均不能为空")
	}
	result := &ACLPolicy{Groups: map[string][]string{}, TagOwners: map[string][]string{}}
	for name, members := range baseline.Groups {
		result.Groups[name] = append([]string(nil), members...)
	}
	for name, members := range generated.Groups {
		if existing, ok := result.Groups[name]; ok && !stringSetsEqual(existing, members) {
			return nil, fmt.Errorf("Group %s 在 baseline 与 ZTNA 中定义冲突", name)
		}
		result.Groups[name] = append([]string(nil), members...)
	}
	for tag, owners := range baseline.TagOwners {
		result.TagOwners[tag] = append([]string(nil), owners...)
	}
	for tag, owners := range generated.TagOwners {
		result.TagOwners[tag] = unionStrings(result.TagOwners[tag], owners)
	}
	result.ACLs = appendUniqueJSON(result.ACLs, baseline.ACLs)
	result.ACLs = appendUniqueJSON(result.ACLs, generated.ACLs)
	result.SSH = appendUniqueJSON(result.SSH, baseline.SSH)
	result.SSH = appendUniqueJSON(result.SSH, generated.SSH)
	return result, nil
}

func appendUniqueJSON[T any](dst, values []T) []T {
	seen := make(map[string]struct{}, len(dst)+len(values))
	for _, value := range dst {
		encoded, _ := json.Marshal(value)
		seen[string(encoded)] = struct{}{}
	}
	for _, value := range values {
		encoded, _ := json.Marshal(value)
		key := string(encoded)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}

func unionStrings(left, right []string) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	result := make([]string, 0, len(left)+len(right))
	for _, values := range [][]string{left, right} {
		for _, value := range values {
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func stringSetsEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	return slices.Equal(unionStrings(nil, left), unionStrings(nil, right))
}

// FullSync 完整同步：先同步 Node Tag，再同步 ACL 规则
// 所有 API 操作（分组成员变更、ACL 权限变更等）应调用此方法，
// 确保节点 Tag 和 ACL 规则同时更新，避免 Tag 未同步导致 ACL 规则不生效
func (s *ACLSyncService) FullSync(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// 先同步 Node Tag（确保节点有正确的分组标签）
	if err := s.SyncAllNodeTags(ctx); err != nil {
		logger.Warnf("FullSync: Node Tag 同步失败: %v", err)
		// 不中断，继续同步 ACL
	}

	// 再同步 ACL 规则
	if err := s.SyncACL(ctx); err != nil {
		return fmt.Errorf("FullSync: ACL 同步失败: %w", err)
	}

	return nil
}

// generateACLPolicy 根据数据库配置生成 ACL 策略
// 使用新的统一模型：User, Node, Group, GroupMember
// Tag 格式:
//   - 身份 Tag: tag:agent-{name} / tag:client-{name}
//   - 分组 Tag: tag:group-{group.name}
func (s *ACLSyncService) generateACLPolicy(ctx context.Context) (*ACLPolicy, error) {
	policy := &ACLPolicy{
		Groups:    make(map[string][]string),
		TagOwners: make(map[string][]string),
		ACLs:      []ACLRule{},
	}

	usedTags := make(map[string]bool)

	// 生成所有用户的身份 Tag
	var users []model.User
	db.DB.WithContext(ctx).Find(&users)
	for _, user := range users {
		tagName := fmt.Sprintf("tag:%s-%s", user.Role, user.Name)
		usedTags[tagName] = true
	}

	// 生成所有分组的 Tag
	var groups []model.Group
	db.DB.WithContext(ctx).Find(&groups)
	for _, group := range groups {
		tagName := fmt.Sprintf("tag:group-%s", group.Name)
		usedTags[tagName] = true
	}

	// 1. 分组内互访规则 - 同一分组的成员可以互相访问
	for _, group := range groups {
		tagName := fmt.Sprintf("tag:group-%s", group.Name)
		rule := ACLRule{
			Action: "accept",
			Src:    []string{tagName},
			Dst:    []string{fmt.Sprintf("%s:*", tagName)},
		}
		policy.ACLs = append(policy.ACLs, rule)
	}

	// 2. 服务授权规则 - 用户级别
	var serviceUserPerms []model.AclServiceUserPermission
	db.DB.WithContext(ctx).Preload("Service").Preload("Service.User").Preload("User").Find(&serviceUserPerms)

	for _, perm := range serviceUserPerms {
		if perm.Service == nil || perm.User == nil || perm.Service.User == nil {
			continue
		}

		srcTag := fmt.Sprintf("tag:%s-%s", perm.User.Role, perm.User.Name)
		dstTag := fmt.Sprintf("tag:%s-%s", perm.Service.User.Role, perm.Service.User.Name)
		usedTags[srcTag] = true
		usedTags[dstTag] = true

		port := extractPort(perm.Service.SourceAddr)
		rule := ACLRule{
			Action: "accept",
			Src:    []string{srcTag},
			Dst:    []string{fmt.Sprintf("%s:%s", dstTag, port)},
		}
		policy.ACLs = append(policy.ACLs, rule)
	}

	// 3. 服务授权规则 - 分组级别
	var serviceGroupPerms []model.AclServiceGroupPermission
	db.DB.WithContext(ctx).Preload("Service").Preload("Service.User").Preload("Group").Find(&serviceGroupPerms)

	for _, perm := range serviceGroupPerms {
		if perm.Service == nil || perm.Group == nil || perm.Service.User == nil {
			continue
		}

		srcTag := fmt.Sprintf("tag:group-%s", perm.Group.Name)
		dstTag := fmt.Sprintf("tag:%s-%s", perm.Service.User.Role, perm.Service.User.Name)
		usedTags[srcTag] = true
		usedTags[dstTag] = true

		port := extractPort(perm.Service.SourceAddr)
		rule := ACLRule{
			Action: "accept",
			Src:    []string{srcTag},
			Dst:    []string{fmt.Sprintf("%s:%s", dstTag, port)},
		}
		policy.ACLs = append(policy.ACLs, rule)
	}

	// 4. 用户授权规则 - 用户级别（访问某个 Agent 的所有端口）
	var userUserPerms []model.AclUserUserPermission
	db.DB.WithContext(ctx).Preload("TargetUser").Preload("GrantedUser").Find(&userUserPerms)

	for _, perm := range userUserPerms {
		if perm.TargetUser == nil || perm.GrantedUser == nil {
			continue
		}

		srcTag := fmt.Sprintf("tag:%s-%s", perm.GrantedUser.Role, perm.GrantedUser.Name)
		dstTag := fmt.Sprintf("tag:%s-%s", perm.TargetUser.Role, perm.TargetUser.Name)
		usedTags[srcTag] = true
		usedTags[dstTag] = true

		rule := ACLRule{
			Action: "accept",
			Src:    []string{srcTag},
			Dst:    []string{fmt.Sprintf("%s:*", dstTag)},
		}
		policy.ACLs = append(policy.ACLs, rule)
	}

	// 5. 用户授权规则 - 分组级别
	var userGroupPerms []model.AclUserGroupPermission
	db.DB.WithContext(ctx).Preload("TargetUser").Preload("Group").Find(&userGroupPerms)

	for _, perm := range userGroupPerms {
		if perm.TargetUser == nil || perm.Group == nil {
			continue
		}

		srcTag := fmt.Sprintf("tag:group-%s", perm.Group.Name)
		dstTag := fmt.Sprintf("tag:%s-%s", perm.TargetUser.Role, perm.TargetUser.Name)
		usedTags[srcTag] = true
		usedTags[dstTag] = true

		rule := ACLRule{
			Action: "accept",
			Src:    []string{srcTag},
			Dst:    []string{fmt.Sprintf("%s:*", dstTag)},
		}
		policy.ACLs = append(policy.ACLs, rule)
	}

	// 6. 分组授权规则 - 用户级别（访问某个分组的所有端口）
	var groupUserPerms []model.AclGroupUserPermission
	db.DB.WithContext(ctx).Preload("TargetGroup").Preload("User").Find(&groupUserPerms)

	for _, perm := range groupUserPerms {
		if perm.TargetGroup == nil || perm.User == nil {
			continue
		}

		srcTag := fmt.Sprintf("tag:%s-%s", perm.User.Role, perm.User.Name)
		dstTag := fmt.Sprintf("tag:group-%s", perm.TargetGroup.Name)
		usedTags[srcTag] = true
		usedTags[dstTag] = true

		rule := ACLRule{
			Action: "accept",
			Src:    []string{srcTag},
			Dst:    []string{fmt.Sprintf("%s:*", dstTag)},
		}
		policy.ACLs = append(policy.ACLs, rule)
	}

	// 7. 分组授权规则 - 分组级别
	var groupGroupPerms []model.AclGroupGroupPermission
	db.DB.WithContext(ctx).Preload("TargetGroup").Preload("GrantedGroup").Find(&groupGroupPerms)

	for _, perm := range groupGroupPerms {
		if perm.TargetGroup == nil || perm.GrantedGroup == nil {
			continue
		}

		srcTag := fmt.Sprintf("tag:group-%s", perm.GrantedGroup.Name)
		dstTag := fmt.Sprintf("tag:group-%s", perm.TargetGroup.Name)
		usedTags[srcTag] = true
		usedTags[dstTag] = true

		rule := ACLRule{
			Action: "accept",
			Src:    []string{srcTag},
			Dst:    []string{fmt.Sprintf("%s:*", dstTag)},
		}
		policy.ACLs = append(policy.ACLs, rule)
	}

	// 生成 tagOwners
	for tag := range usedTags {
		policy.TagOwners[tag] = []string{}
	}

	// 8. 同用户节点互访规则 — Client 用户的多个设备（Desktop、CloudIDE 等）之间互相访问
	for _, user := range users {
		if user.Role == model.UserRoleClient {
			tagName := fmt.Sprintf("tag:client-%s", user.Name)
			usedTags[tagName] = true
			policy.TagOwners[tagName] = []string{}
			rule := ACLRule{
				Action: "accept",
				Src:    []string{tagName},
				Dst:    []string{fmt.Sprintf("%s:*", tagName)},
			}
			policy.ACLs = append(policy.ACLs, rule)
		}
	}

	// 生成 SSH 规则
	sshRules, err := s.generateSSHRules(ctx, usedTags)
	if err != nil {
		logger.Warnf("生成 SSH 规则失败: %v", err)
	} else {
		policy.SSH = sshRules
	}

	return policy, nil
}

// extractPort 从地址中提取端口号
func extractPort(addr string) string {
	if addr == "" {
		return "*"
	}
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[i+1:]
		}
	}
	return addr
}

// StartPeriodicSync 启动定时全量同步
func (s *ACLSyncService) StartPeriodicSync(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	logger.Info("启动 ACL 定时同步任务，间隔 5 分钟")

	if err := s.SyncAllNodeTags(ctx); err != nil {
		logger.Errorf("初始 Node Tag 同步失败: %v", err)
	}

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
func (s *ACLSyncService) SyncAllNodeTags(ctx context.Context) error {
	logger.Info("开始同步所有 Node 的 Tag")

	nodes, err := s.client.ListNodes(ctx)
	if err != nil {
		logger.Warnf("获取 Headscale Node 列表失败: %v", err)
		return err
	}

	// 建立 Headscale User 名称 -> Node 列表的映射（一个 User 下可能有多个 Node）
	type hsNodeInfo struct {
		HeadscaleNodeID uint64
		GivenName       string
		IP              string
		Tags            []string
		Online          bool
	}
	userNodesMap := make(map[string][]hsNodeInfo)
	for _, node := range nodes {
		if node.User != nil {
			ip := ""
			if len(node.IpAddresses) > 0 {
				ip = node.IpAddresses[0]
			}
			userNodesMap[node.User.Name] = append(userNodesMap[node.User.Name], hsNodeInfo{
				HeadscaleNodeID: node.Id,
				GivenName:       node.GivenName,
				IP:              ip,
				Tags:            node.ForcedTags,
				Online:          node.Online,
			})
		}
	}

	// 同步所有 Node 的 Tag（Agent 模式节点）
	var dbNodes []model.Node
	db.DB.WithContext(ctx).Preload("User").Find(&dbNodes)
	logger.Infof("找到 %d 个 Agent Node", len(dbNodes))

	// 记录已处理的 Headscale Node ID，避免重复处理
	processedNodeIDs := make(map[uint64]bool)

	for _, dbNode := range dbNodes {
		if dbNode.User == nil {
			continue
		}

		// 通过 User 名称查找 Headscale Node 列表
		userName := fmt.Sprintf("%s-%s", dbNode.User.Role, dbNode.User.Name)
		hsNodes, found := userNodesMap[userName]

		if !found || len(hsNodes) == 0 {
			logger.Warnf("Node %s 在 Headscale 中没有对应的 Node (User: %s)", dbNode.Name, userName)
			// 清空离线设备的 IP 和 HeadscaleNodeID
			if dbNode.HeadscaleNodeID != 0 || dbNode.IP != "" {
				dbNode.HeadscaleNodeID = 0
				dbNode.IP = ""
				if err := db.DB.WithContext(ctx).Save(&dbNode).Error; err != nil {
					logger.Warnf("清空 Node %s IP 失败: %v", dbNode.Name, err)
				} else {
					logger.Infof("Node %s 在 Headscale 中不存在，已清空 IP", dbNode.Name)
				}
			}
			continue
		}

		// 在同一 User 的多个 Headscale Node 中，按 GivenName 精确匹配
		var nodeInfo *hsNodeInfo
		for i := range hsNodes {
			if dbNode.IP != "" && hsNodes[i].IP == dbNode.IP {
				nodeInfo = &hsNodes[i]
				break
			}
		}
		for i := range hsNodes {
			if nodeInfo != nil {
				break
			}
			candidate := &hsNodes[i]
			if candidate.GivenName != dbNode.Name {
				continue
			}
			if nodeInfo == nil || (candidate.Online && !nodeInfo.Online) ||
				(candidate.Online == nodeInfo.Online && candidate.HeadscaleNodeID > nodeInfo.HeadscaleNodeID) {
				nodeInfo = candidate
			}
		}

		if nodeInfo == nil {
			logger.Warnf("Node %s 在 Headscale User %s 下未找到匹配的 Node（共 %d 个 Node）", dbNode.Name, userName, len(hsNodes))
			// 未找到匹配设备，清空 IP 和 HeadscaleNodeID
			if dbNode.HeadscaleNodeID != 0 || dbNode.IP != "" {
				dbNode.HeadscaleNodeID = 0
				dbNode.IP = ""
				if err := db.DB.WithContext(ctx).Save(&dbNode).Error; err != nil {
					logger.Warnf("清空 Node %s IP 失败: %v", dbNode.Name, err)
				} else {
					logger.Infof("Node %s 未匹配到 Headscale Node，已清空 IP", dbNode.Name)
				}
			}
			continue
		}

		// 用户匹配 + 设备名匹配，更新 HeadscaleNodeID 和 IP
		if dbNode.HeadscaleNodeID != nodeInfo.HeadscaleNodeID || dbNode.IP != nodeInfo.IP {
			dbNode.HeadscaleNodeID = nodeInfo.HeadscaleNodeID
			dbNode.IP = nodeInfo.IP
			if err := db.DB.WithContext(ctx).Save(&dbNode).Error; err != nil {
				logger.Warnf("更新 Node %s 失败: %v", dbNode.Name, err)
			} else {
				logger.Infof("Node %s IP 已更新: %s", dbNode.Name, nodeInfo.IP)
			}
		}

		// 构建期望的 Tag 列表
		expectedTags := []string{
			fmt.Sprintf("tag:%s-%s", dbNode.User.Role, dbNode.User.Name),
		}

		// 查询用户所属的分组
		var groupMembers []model.GroupMember
		db.DB.WithContext(ctx).Preload("Group").Where("user_id = ?", dbNode.UserID).Find(&groupMembers)
		for _, gm := range groupMembers {
			if gm.Group != nil {
				expectedTags = append(expectedTags, fmt.Sprintf("tag:group-%s", gm.Group.Name))
			}
		}

		// 检查是否需要更新
		if !tagsEqual(nodeInfo.Tags, expectedTags) {
			if err := s.client.SetTags(ctx, nodeInfo.HeadscaleNodeID, expectedTags); err != nil {
				logger.Warnf("设置 Node %s Tag 失败: %v", dbNode.Name, err)
			} else {
				logger.Infof("Node %s Tag 已同步: %v", dbNode.Name, expectedTags)
			}
		}

		processedNodeIDs[nodeInfo.HeadscaleNodeID] = true
	}

	// 同步 Client 模式节点的 Tag
	// Client 节点不在 node 表中，但在 Headscale 中有对应的节点
	// 需要根据 Headscale User 名称（client-xxx）来设置标签
	var clientUsers []model.User
	if err := db.DB.WithContext(ctx).Where("role = ?", model.UserRoleClient).Find(&clientUsers).Error; err != nil {
		logger.Warnf("查询 Client 用户失败: %v", err)
	} else {
		logger.Infof("找到 %d 个 Client 用户", len(clientUsers))

		// 调试：输出 userNodesMap 中所有 User 名称
		logger.Debugf("userNodesMap 中的所有 User 名称:")
		for userName := range userNodesMap {
			logger.Debugf("  - %s (%d 个 Node)", userName, len(userNodesMap[userName]))
		}

		for _, user := range clientUsers {
			userName := fmt.Sprintf("client-%s", user.Name)
			logger.Debugf("查找 Client 用户: %s", userName)
			hsNodes, found := userNodesMap[userName]
			if !found || len(hsNodes) == 0 {
				logger.Warnf("Client 用户 %s 在 Headscale 中没有对应的 Node", userName)
				continue
			}
			logger.Infof("Client 用户 %s 在 Headscale 中有 %d 个 Node", userName, len(hsNodes))

			// 构建期望的 Tag 列表
			expectedTags := []string{
				fmt.Sprintf("tag:client-%s", user.Name),
			}

			// 查询用户所属的分组
			var groupMembers []model.GroupMember
			db.DB.WithContext(ctx).Preload("Group").Where("user_id = ?", user.ID).Find(&groupMembers)
			for _, gm := range groupMembers {
				if gm.Group != nil {
					expectedTags = append(expectedTags, fmt.Sprintf("tag:group-%s", gm.Group.Name))
				}
			}

			// 为该 Client User 的所有 Headscale Node 设置标签
			for _, nodeInfo := range hsNodes {
				logger.Debugf("处理 Client Node: %s (ID: %d, IP: %s, 当前 Tags: %v)",
					nodeInfo.GivenName, nodeInfo.HeadscaleNodeID, nodeInfo.IP, nodeInfo.Tags)

				// 跳过已处理的节点（避免重复处理）
				if processedNodeIDs[nodeInfo.HeadscaleNodeID] {
					logger.Debugf("Client Node %s 已被处理，跳过", nodeInfo.GivenName)
					continue
				}

				// 检查是否需要更新
				logger.Debugf("期望 Tags: %v, 当前 Tags: %v, 是否相等: %v",
					expectedTags, nodeInfo.Tags, tagsEqual(nodeInfo.Tags, expectedTags))

				if !tagsEqual(nodeInfo.Tags, expectedTags) {
					if err := s.client.SetTags(ctx, nodeInfo.HeadscaleNodeID, expectedTags); err != nil {
						logger.Warnf("设置 Client Node %s (User: %s) Tag 失败: %v", nodeInfo.GivenName, userName, err)
					} else {
						logger.Infof("Client Node %s (User: %s) Tag 已同步: %v", nodeInfo.GivenName, userName, expectedTags)
					}
				} else {
					logger.Debugf("Client Node %s Tag 已是最新，无需更新", nodeInfo.GivenName)
				}

				processedNodeIDs[nodeInfo.HeadscaleNodeID] = true
			}
		}
	}

	logger.Info("Node Tag 同步完成")
	return nil
}

// tagsEqual 比较两个 Tag 列表是否相等
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

// generateSSHRules 根据数据库配置生成 SSH 规则
func (s *ACLSyncService) generateSSHRules(ctx context.Context, usedTags map[string]bool) ([]SSHRule, error) {
	var rules []SSHRule

	// 1. SSH 用户授权
	var userPerms []model.AclSSHUserPermission
	if err := db.DB.WithContext(ctx).Preload("TargetUser").Preload("User").Where("enabled = ?", true).Find(&userPerms).Error; err != nil {
		return nil, fmt.Errorf("查询 SSH 用户授权失败: %w", err)
	}

	for _, perm := range userPerms {
		if perm.TargetUser == nil || perm.User == nil {
			continue
		}

		srcTag := fmt.Sprintf("tag:%s-%s", perm.User.Role, perm.User.Name)
		dstTag := fmt.Sprintf("tag:%s-%s", perm.TargetUser.Role, perm.TargetUser.Name)
		usedTags[srcTag] = true
		usedTags[dstTag] = true

		users := parseSSHUsers(perm.SSHUsers)
		if len(users) == 0 {
			continue
		}

		rule := SSHRule{
			Action: "accept",
			Src:    []string{srcTag},
			Dst:    []string{dstTag},
			Users:  users,
		}
		rules = append(rules, rule)
	}

	// 2. SSH 分组授权
	var groupPerms []model.AclSSHGroupPermission
	if err := db.DB.WithContext(ctx).Preload("TargetUser").Preload("Group").Where("enabled = ?", true).Find(&groupPerms).Error; err != nil {
		return nil, fmt.Errorf("查询 SSH 分组授权失败: %w", err)
	}

	for _, perm := range groupPerms {
		if perm.TargetUser == nil || perm.Group == nil {
			continue
		}

		srcTag := fmt.Sprintf("tag:group-%s", perm.Group.Name)
		dstTag := fmt.Sprintf("tag:%s-%s", perm.TargetUser.Role, perm.TargetUser.Name)
		usedTags[srcTag] = true
		usedTags[dstTag] = true

		users := parseSSHUsers(perm.SSHUsers)
		if len(users) == 0 {
			continue
		}

		rule := SSHRule{
			Action: "accept",
			Src:    []string{srcTag},
			Dst:    []string{dstTag},
			Users:  users,
		}
		rules = append(rules, rule)
	}

	// 3. 同用户 Client SSH 互访规则
	// Client 用户（如 CloudIDE）的多个节点之间可以 SSH 互访，无需 ssh_enabled 开关
	var clientUsers []model.User
	if err := db.DB.WithContext(ctx).Where("role = ?", model.UserRoleClient).Find(&clientUsers).Error; err != nil {
		logger.Warnf("查询 Client 用户失败: %v", err)
	} else {
		for _, user := range clientUsers {
			tagName := fmt.Sprintf("tag:client-%s", user.Name)
			usedTags[tagName] = true

			// 解析 deploy token 中配置的 SSH 用户名列表
			sshUsers := s.getClientSSHUsers(ctx, user.ID)
			if len(sshUsers) == 0 {
				sshUsers = []string{"root"} // 默认允许 root
			}

			rule := SSHRule{
				Action: "accept",
				Src:    []string{tagName},
				Dst:    []string{tagName},
				Users:  sshUsers,
			}
			rules = append(rules, rule)
		}
	}

	logger.Infof("生成 %d 条 SSH 规则", len(rules))
	return rules, nil
}

// getClientSSHUsers 获取 Client 用户的 SSH 用户名列表（从 deploy token 中读取）
func (s *ACLSyncService) getClientSSHUsers(ctx context.Context, userID uint64) []string {
	var tokens []model.DeployToken
	db.DB.WithContext(ctx).Where("user_id = ? AND ssh_enabled = ? AND status != ?",
		userID, true, model.DeployTokenStatusRevoked).Find(&tokens)

	// 合并所有 token 的 SSH 用户名
	userSet := make(map[string]bool)
	for _, t := range tokens {
		users := parseSSHUsers(t.SSHUsers)
		for _, u := range users {
			userSet[u] = true
		}
	}

	if len(userSet) == 0 {
		return nil
	}

	result := make([]string, 0, len(userSet))
	for u := range userSet {
		result = append(result, u)
	}
	return result
}

// parseSSHUsers 解析 SSHUsers JSON 数组
func parseSSHUsers(jsonStr string) []string {
	if jsonStr == "" {
		return nil
	}

	var users []string
	if err := json.Unmarshal([]byte(jsonStr), &users); err != nil {
		logger.Warnf("解析 SSHUsers 失败: %v, 原始值: %s", err, jsonStr)
		return nil
	}

	return users
}
