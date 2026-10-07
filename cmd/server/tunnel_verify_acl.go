package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	v1 "github.com/juanfont/headscale/gen/go/headscale/v1"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type tunnelACLIdentity struct {
	Selectors map[string]bool
	IPs       []net.IP
}

type tunnelNodeReader interface {
	ListNodesByUser(context.Context, string) ([]*v1.Node, error)
}

func loadTunnelACLIdentity(ctx context.Context, client tunnelNodeReader, tok model.DeployToken) (tunnelACLIdentity, error) {
	id := tunnelACLIdentity{Selectors: map[string]bool{}}
	if tok.User == nil || tok.User.Name == "" {
		return id, fmt.Errorf("Tunnel 服务账号缺失")
	}
	id.Selectors["tag:client-"+tok.User.Name] = true
	id.Selectors[tok.User.Name] = true
	hsUserName := fmt.Sprintf("%s-%s", tok.User.Role, tok.User.Name)
	id.Selectors[hsUserName] = true
	var members []model.GroupMember
	if err := db.DB.WithContext(ctx).Preload("Group").Where("user_id = ?", tok.UserID).Find(&members).Error; err != nil {
		return id, err
	}
	for _, member := range members {
		if member.Group == nil {
			return id, fmt.Errorf("Tunnel 分组绑定缺少 Group")
		}
		id.Selectors["tag:group-"+member.Group.Name] = true
	}
	nodes, err := client.ListNodesByUser(ctx, hsUserName)
	if err != nil {
		return id, fmt.Errorf("读取 Tunnel 实际节点身份: %w", err)
	}
	if len(nodes) == 0 {
		return id, fmt.Errorf("Tunnel 无 Headscale 节点，无法核验有效源身份")
	}
	for _, node := range nodes {
		for _, tag := range node.ForcedTags {
			id.Selectors[tag] = true
		}
		for _, addr := range node.IpAddresses {
			ip := net.ParseIP(addr)
			if ip == nil {
				return id, fmt.Errorf("无效 Tunnel IP: %q", addr)
			}
			id.IPs = append(id.IPs, ip)
		}
	}
	return id, nil
}

// Treat broad/automatic selectors conservatively. Group cycles or missing definitions
// cannot establish non-applicability and therefore fail closed.
func tunnelSourceMatches(src string, id tunnelACLIdentity, groups map[string][]string, visiting map[string]bool) (bool, error) {
	if id.Selectors[src] || src == "*" || strings.HasPrefix(src, "autogroup:") {
		return true, nil
	}
	if strings.HasPrefix(src, "group:") {
		members, ok := groups[src]
		if !ok || visiting[src] {
			return false, fmt.Errorf("无法解析 ACL 源分组 %s", src)
		}
		visiting[src] = true
		defer delete(visiting, src)
		for _, member := range members {
			match, err := tunnelSourceMatches(member, id, groups, visiting)
			if err != nil || match {
				return match, err
			}
		}
		return false, nil
	}
	if ip := net.ParseIP(src); ip != nil {
		for _, actual := range id.IPs {
			if ip.Equal(actual) {
				return true, nil
			}
		}
		return false, nil
	}
	if _, network, err := net.ParseCIDR(src); err == nil {
		for _, ip := range id.IPs {
			if network.Contains(ip) {
				return true, nil
			}
		}
		return false, nil
	}
	if strings.HasPrefix(src, "tag:") && !strings.ContainsAny(src, "*/") {
		return false, nil
	}
	return false, fmt.Errorf("无法判定 ACL 源 %q（不支持未解析的用户名或 hosts 别名）", src)
}

func tunnelAllowedDestinations(agentTags []string, k8s bool) map[string]bool {
	allowed := map[string]bool{}
	for _, tag := range agentTags {
		allowed[tag+":50051"] = true
		if k8s {
			allowed[tag+":6443"] = true
		}
	}
	return allowed
}

func checkTunnelACL(policy *headscale.ACLPolicy, id tunnelACLIdentity, allowed map[string]bool) error {
	seen := map[string]bool{}
	for _, rule := range policy.ACLs {
		applies := false
		for _, src := range rule.Src {
			match, err := tunnelSourceMatches(src, id, policy.Groups, map[string]bool{})
			if err != nil {
				return err
			}
			applies = applies || match
		}
		if !applies {
			continue
		}
		if rule.Action != "accept" {
			return fmt.Errorf("不支持的 Tunnel ACL action %q", rule.Action)
		}
		for _, dst := range rule.Dst {
			if !allowed[dst] {
				return fmt.Errorf("Tunnel 有超出最小权限的授权: %s", dst)
			}
			seen[dst] = true
		}
	}
	for dst := range allowed {
		if !seen[dst] {
			return fmt.Errorf("Tunnel 缺少授权: %s", dst)
		}
	}
	return nil
}

func parseVerifiedACL(raw string) (*headscale.ACLPolicy, error) {
	var policy *headscale.ACLPolicy
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return nil, fmt.Errorf("解析 ACL 策略失败: %w", err)
	}
	if policy == nil || policy.ACLs == nil {
		return nil, fmt.Errorf("ACL 策略缺少显式 acls 数组")
	}
	for _, rule := range policy.ACLs {
		if rule.Action == "" || len(rule.Src) == 0 || len(rule.Dst) == 0 {
			return nil, fmt.Errorf("ACL 规则结构不完整")
		}
	}
	return policy, nil
}

func tunnelAgentTags(ctx context.Context, agentName string) ([]string, error) {
	var node model.Node
	if err := db.DB.WithContext(ctx).Preload("User").Where("name = ? AND type = ?", agentName, model.NodeTypeAgent).First(&node).Error; err != nil {
		return nil, err
	}
	if node.User == nil || node.User.Name == "" {
		return nil, fmt.Errorf("目标 Agent 账号缺失")
	}
	tags := []string{"tag:agent-" + agentName}
	if userTag := "tag:agent-" + node.User.Name; userTag != tags[0] {
		tags = append(tags, userTag)
	}
	return tags, nil
}
