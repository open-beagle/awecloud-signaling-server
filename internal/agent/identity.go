package agent

import (
	"context"
	"fmt"
	"net"
	"strings"

	"tailscale.com/client/tailscale"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// PeerIdentity 对端身份信息（从 tsnet 连接中提取）
type PeerIdentity struct {
	UserID   uint64 // 用户 ID（从 Headscale 用户名解析）
	UserName string // 用户名（如 zhangsan）
	NodeName string // 节点名称
	NodeID   uint64 // Tailscale 节点 ID
	Role     string // 角色：client / agent
	RemoteIP string // 对端 IP
}

// IdentityExtractor 从 tsnet 连接中提取对端身份
type IdentityExtractor struct {
	localClient *tailscale.LocalClient
}

// NewIdentityExtractor 创建身份提取器
func NewIdentityExtractor(tsManager *TailscaleManager) (*IdentityExtractor, error) {
	lc, err := tsManager.LocalClient()
	if err != nil {
		return nil, fmt.Errorf("获取 LocalClient 失败: %w", err)
	}
	return &IdentityExtractor{localClient: lc}, nil
}

// ExtractFromConn 从网络连接中提取对端身份
func (e *IdentityExtractor) ExtractFromConn(ctx context.Context, remoteAddr net.Addr) (*PeerIdentity, error) {
	// 通过 WhoIs 获取对端信息
	whois, err := e.localClient.WhoIs(ctx, remoteAddr.String())
	if err != nil {
		return nil, fmt.Errorf("WhoIs 查询失败: %w", err)
	}

	if whois.UserProfile == nil {
		return nil, fmt.Errorf("无法获取对端用户信息")
	}

	// 解析用户名和角色
	// Headscale 用户名格式: client-zhangsan 或 agent-beijing
	loginName := whois.UserProfile.LoginName
	userName, role := parseHeadscaleUserName(loginName)

	identity := &PeerIdentity{
		UserName: userName,
		NodeName: whois.Node.ComputedName,
		NodeID:   uint64(whois.Node.ID),
		Role:     role,
		RemoteIP: remoteAddr.String(),
	}

	// 尝试从 UserProfile.ID 获取用户 ID
	identity.UserID = uint64(whois.UserProfile.ID)

	logger.Debugf("身份提取: remote=%s, user=%s, role=%s, node=%s",
		remoteAddr.String(), userName, role, whois.Node.ComputedName)

	return identity, nil
}

// parseHeadscaleUserName 解析 Headscale 用户名
// 格式: {role}-{name}，如 client-zhangsan、agent-beijing
// 返回: 用户名, 角色
func parseHeadscaleUserName(loginName string) (string, string) {
	// 尝试按 "client-" 或 "agent-" 前缀解析
	if strings.HasPrefix(loginName, "client-") {
		return strings.TrimPrefix(loginName, "client-"), "client"
	}
	if strings.HasPrefix(loginName, "agent-") {
		return strings.TrimPrefix(loginName, "agent-"), "agent"
	}
	// 无法识别前缀，返回原始名称
	return loginName, "unknown"
}
