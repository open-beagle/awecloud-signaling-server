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

	// 建立 User 名称 -> Node 的映射
	userNodeMap := make(map[string]*struct {
		HeadscaleNodeID uint64
		IP              string
		Tags            []string
	})
	for _, node := range nodes {
		if node.User != nil {
			ip := ""
			if len(node.IpAddresses) > 0 {
				ip = node.IpAddresses[0]
			}
			userNodeMap[node.User.Name] = &struct {
				HeadscaleNodeID uint64
				IP              string
				Tags            []string
			}{
				HeadscaleNodeID: node.Id,
				IP:              ip,
				Tags:            node.ForcedTags,
			}
		}
	}

	// 同步所有 Node 的 Tag
	var dbNodes []model.Node
	db.DB.WithContext(ctx).Preload("User").Find(&dbNodes)
	logger.Infof("找到 %d 个 Node", len(dbNodes))

	for _, dbNode := range dbNodes {
		if dbNode.User == nil {
			continue
		}

		// 通过 User 名称查找 Headscale Node
		userName := fmt.Sprintf("%s-%s", dbNode.User.Role, dbNode.User.Name)
		nodeInfo, found := userNodeMap[userName]

		if !found {
			logger.Warnf("Node %s 在 Headscale 中没有对应的 Node (User: %s)", dbNode.Name, userName)
			continue
		}

		// 更新数据库中的 Headscale Node ID 和 IP
		if dbNode.HeadscaleNodeID != nodeInfo.HeadscaleNodeID || dbNode.IP != nodeInfo.IP {
			dbNode.HeadscaleNodeID = nodeInfo.HeadscaleNodeID
			dbNode.IP = nodeInfo.IP
			if err := db.DB.WithContext(ctx).Save(&dbNode).Error; err != nil {
				logger.Warnf("更新 Node %s 失败: %v", dbNode.Name, err)
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

	logger.Infof("生成 %d 条 SSH 规则", len(rules))
	return rules, nil
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
