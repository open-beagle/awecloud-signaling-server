// Package headscale 提供 Headscale gRPC 客户端
package headscale

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"
	"time"

	v1 "github.com/juanfont/headscale/gen/go/headscale/v1"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// Client Headscale gRPC 客户端
type Client struct {
	conn   *grpc.ClientConn
	client v1.HeadscaleServiceClient
}

// Config Headscale 客户端配置
type Config struct {
	URL      string // Headscale gRPC 地址
	APIKey   string // API 密钥
	Insecure bool   // 跳过 TLS 证书验证
}

// parsedURL 解析后的 URL 信息
type parsedURL struct {
	host   string
	prefix string
	useTLS bool
}

// parseURL 解析 URL
func parseURL(rawURL string) (*parsedURL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("URL 解析失败: %w", err)
	}

	result := &parsedURL{
		host:   u.Host,
		prefix: strings.TrimSuffix(u.Path, "/"),
		useTLS: u.Scheme == "https",
	}

	if !strings.Contains(result.host, ":") {
		if result.useTLS {
			result.host += ":443"
		} else {
			result.host += ":80"
		}
	}

	return result, nil
}

// prefixUnaryInterceptor 为 Unary 调用添加路径前缀
func prefixUnaryInterceptor(prefix string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		newMethod := prefix + method
		return invoker(ctx, newMethod, req, reply, cc, opts...)
	}
}

// prefixStreamInterceptor 为 Stream 调用添加路径前缀
func prefixStreamInterceptor(prefix string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn,
		method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		newMethod := prefix + method
		return streamer(ctx, desc, cc, newMethod, opts...)
	}
}

// apiKeyAuth API Key 认证
type apiKeyAuth struct {
	apiKey string
}

func (a *apiKeyAuth) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{
		"authorization": "Bearer " + a.apiKey,
	}, nil
}

func (a *apiKeyAuth) RequireTransportSecurity() bool {
	return false
}

// NewClient 创建 Headscale gRPC 客户端
func NewClient(cfg Config) (*Client, error) {
	parsed, err := parseURL(cfg.URL)
	if err != nil {
		return nil, err
	}

	var opts []grpc.DialOption

	opts = append(opts, grpc.WithPerRPCCredentials(&apiKeyAuth{
		apiKey: cfg.APIKey,
	}))

	if parsed.useTLS {
		tlsConfig := &tls.Config{
			// 必须设置 NextProtos 以支持 HTTP/2（gRPC 要求）
			NextProtos: []string{"h2"},
		}
		if cfg.Insecure {
			tlsConfig.InsecureSkipVerify = true
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	if parsed.prefix != "" {
		opts = append(opts, grpc.WithUnaryInterceptor(prefixUnaryInterceptor(parsed.prefix)))
		opts = append(opts, grpc.WithStreamInterceptor(prefixStreamInterceptor(parsed.prefix)))
	}

	// 添加 OpenTelemetry gRPC 客户端追踪
	// 使用 peer.service 属性让 Headscale 在 Jaeger 中显示为独立节点
	opts = append(opts, grpc.WithStatsHandler(otelgrpc.NewClientHandler(
		otelgrpc.WithSpanAttributes(
			attribute.String("peer.service", "headscale"),
		),
	)))

	conn, err := grpc.NewClient(parsed.host, opts...)
	if err != nil {
		return nil, fmt.Errorf("连接 Headscale gRPC 失败: %w", err)
	}

	logger.Info("Headscale OpenTelemetry 追踪已启用")

	return &Client{
		conn:   conn,
		client: v1.NewHeadscaleServiceClient(conn),
	}, nil
}

// Close 关闭连接
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// --- User 管理 ---

// CreateUser 创建用户
func (c *Client) CreateUser(ctx context.Context, name string) (*v1.User, error) {
	resp, err := c.client.CreateUser(ctx, &v1.CreateUserRequest{
		Name: name,
	})
	if err != nil {
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}
	return resp.User, nil
}

// ListUsers 列出所有用户
func (c *Client) ListUsers(ctx context.Context) ([]*v1.User, error) {
	resp, err := c.client.ListUsers(ctx, &v1.ListUsersRequest{})
	if err != nil {
		return nil, fmt.Errorf("列出用户失败: %w", err)
	}
	return resp.Users, nil
}

// GetUserByName 根据名称获取用户
func (c *Client) GetUserByName(ctx context.Context, name string) (*v1.User, error) {
	users, err := c.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	for _, user := range users {
		if user.Name == name {
			return user, nil
		}
	}
	return nil, nil
}

// GetOrCreateUser 获取或创建用户
func (c *Client) GetOrCreateUser(ctx context.Context, name string) (*v1.User, error) {
	user, err := c.GetUserByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if user != nil {
		return user, nil
	}
	return c.CreateUser(ctx, name)
}

// DeleteUser 删除用户（通过名称查找 ID 后删除）
func (c *Client) DeleteUser(ctx context.Context, name string) error {
	user, err := c.GetUserByName(ctx, name)
	if err != nil {
		return err
	}
	if user == nil {
		return nil // 用户不存在，视为成功
	}

	_, err = c.client.DeleteUser(ctx, &v1.DeleteUserRequest{
		Id: user.Id,
	})
	if err != nil {
		return fmt.Errorf("删除用户失败: %w", err)
	}
	return nil
}

// --- PreAuthKey 管理 ---

// CreatePreAuthKey 创建预认证密钥
func (c *Client) CreatePreAuthKey(ctx context.Context, userID uint64, expiry time.Duration, ephemeral bool) (*v1.PreAuthKey, error) {
	return c.CreatePreAuthKeyWithTags(ctx, userID, expiry, ephemeral, nil)
}

// CreatePreAuthKeyWithTags 创建带 Tags 的预认证密钥
// tags 格式: ["tag:agent-xxx", "tag:agent-group-yyy"]
func (c *Client) CreatePreAuthKeyWithTags(ctx context.Context, userID uint64, expiry time.Duration, ephemeral bool, tags []string) (*v1.PreAuthKey, error) {
	resp, err := c.client.CreatePreAuthKey(ctx, &v1.CreatePreAuthKeyRequest{
		User:       userID,
		Reusable:   false,
		Ephemeral:  ephemeral,
		Expiration: timestamppb.New(time.Now().Add(expiry)),
		AclTags:    tags,
	})
	if err != nil {
		return nil, fmt.Errorf("创建预认证密钥失败: %w", err)
	}
	return resp.PreAuthKey, nil
}

// CreatePreAuthKeyByName 通过用户名创建预认证密钥
func (c *Client) CreatePreAuthKeyByName(ctx context.Context, userName string, expiry time.Duration, ephemeral bool) (*v1.PreAuthKey, error) {
	user, err := c.GetUserByName(ctx, userName)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("用户 %s 不存在", userName)
	}
	return c.CreatePreAuthKey(ctx, user.Id, expiry, ephemeral)
}

// ExpirePreAuthKey 过期预认证密钥
func (c *Client) ExpirePreAuthKey(ctx context.Context, userID uint64, key string) error {
	_, err := c.client.ExpirePreAuthKey(ctx, &v1.ExpirePreAuthKeyRequest{
		User: userID,
		Key:  key,
	})
	if err != nil {
		return fmt.Errorf("过期预认证密钥失败: %w", err)
	}
	return nil
}

// ListPreAuthKeys 列出预认证密钥
func (c *Client) ListPreAuthKeys(ctx context.Context, userID uint64) ([]*v1.PreAuthKey, error) {
	resp, err := c.client.ListPreAuthKeys(ctx, &v1.ListPreAuthKeysRequest{
		User: userID,
	})
	if err != nil {
		return nil, fmt.Errorf("列出预认证密钥失败: %w", err)
	}
	return resp.PreAuthKeys, nil
}

// --- Node 管理 ---

// GetNode 获取节点信息
func (c *Client) GetNode(ctx context.Context, nodeID uint64) (*v1.Node, error) {
	resp, err := c.client.GetNode(ctx, &v1.GetNodeRequest{
		NodeId: nodeID,
	})
	if err != nil {
		return nil, fmt.Errorf("获取节点失败: %w", err)
	}
	return resp.Node, nil
}

// ListNodes 列出所有节点
func (c *Client) ListNodes(ctx context.Context) ([]*v1.Node, error) {
	resp, err := c.client.ListNodes(ctx, &v1.ListNodesRequest{})
	if err != nil {
		return nil, fmt.Errorf("列出节点失败: %w", err)
	}
	return resp.Nodes, nil
}

// DeleteNode 删除节点
func (c *Client) DeleteNode(ctx context.Context, nodeID uint64) error {
	_, err := c.client.DeleteNode(ctx, &v1.DeleteNodeRequest{
		NodeId: nodeID,
	})
	if err != nil {
		return fmt.Errorf("删除节点失败: %w", err)
	}
	return nil
}

// SetTags 设置节点标签
func (c *Client) SetTags(ctx context.Context, nodeID uint64, tags []string) error {
	_, err := c.client.SetTags(ctx, &v1.SetTagsRequest{
		NodeId: nodeID,
		Tags:   tags,
	})
	if err != nil {
		return fmt.Errorf("设置节点标签失败: %w", err)
	}
	return nil
}

// ExpireNode 过期节点
func (c *Client) ExpireNode(ctx context.Context, nodeID uint64) error {
	_, err := c.client.ExpireNode(ctx, &v1.ExpireNodeRequest{
		NodeId: nodeID,
	})
	if err != nil {
		return fmt.Errorf("过期节点失败: %w", err)
	}
	return nil
}

// RenameNode 重命名节点
func (c *Client) RenameNode(ctx context.Context, nodeID uint64, newName string) (*v1.Node, error) {
	resp, err := c.client.RenameNode(ctx, &v1.RenameNodeRequest{
		NodeId:  nodeID,
		NewName: newName,
	})
	if err != nil {
		return nil, fmt.Errorf("重命名节点失败: %w", err)
	}
	return resp.Node, nil
}

// GetNodeByIP 根据 IP 获取节点
func (c *Client) GetNodeByIP(ctx context.Context, ip string) (*v1.Node, error) {
	nodes, err := c.ListNodes(ctx)
	if err != nil {
		return nil, err
	}

	for _, node := range nodes {
		for _, nodeIP := range node.IpAddresses {
			if nodeIP == ip {
				return node, nil
			}
		}
	}

	return nil, nil
}

// --- ACL Policy 管理 ---

// GetPolicy 获取 ACL 策略
func (c *Client) GetPolicy(ctx context.Context) (string, error) {
	resp, err := c.client.GetPolicy(ctx, &v1.GetPolicyRequest{})
	if err != nil {
		return "", fmt.Errorf("获取 ACL 策略失败: %w", err)
	}
	return resp.Policy, nil
}

// SetPolicy 设置 ACL 策略
func (c *Client) SetPolicy(ctx context.Context, policy string) error {
	_, err := c.client.SetPolicy(ctx, &v1.SetPolicyRequest{
		Policy: policy,
	})
	if err != nil {
		return fmt.Errorf("设置 ACL 策略失败: %w", err)
	}
	return nil
}
