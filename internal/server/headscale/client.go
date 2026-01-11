// Package headscale 提供 Headscale API 客户端
package headscale

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client Headscale API 客户端
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// Config Headscale 客户端配置
type Config struct {
	URL    string // Headscale API 地址
	APIKey string // API 密钥
}

// NewClient 创建 Headscale 客户端
func NewClient(cfg Config) *Client {
	// 创建跳过 TLS 验证的 HTTP 客户端（用于自签名证书）
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	return &Client{
		baseURL: cfg.URL,
		apiKey:  cfg.APIKey,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

// PreAuthKey 预认证密钥
type PreAuthKey struct {
	ID         string    `json:"id"`
	Key        string    `json:"key"`
	User       User      `json:"user"` // 修改为 User 对象
	Reusable   bool      `json:"reusable"`
	Ephemeral  bool      `json:"ephemeral"`
	Used       bool      `json:"used"`
	Expiration time.Time `json:"expiration"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Node Headscale 节点
type Node struct {
	ID             string    `json:"id"`
	MachineKey     string    `json:"machineKey"`
	NodeKey        string    `json:"nodeKey"`
	Name           string    `json:"name"`
	GivenName      string    `json:"givenName"`
	User           User      `json:"user"`
	IPAddresses    []string  `json:"ipAddresses"`
	Online         bool      `json:"online"`
	LastSeen       time.Time `json:"lastSeen"`
	Expiry         time.Time `json:"expiry"`
	CreatedAt      time.Time `json:"createdAt"`
	RegisterMethod string    `json:"registerMethod"`
}

// User Headscale 用户
type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// CreatePreAuthKeyRequest 创建预认证密钥请求
type CreatePreAuthKeyRequest struct {
	User       string `json:"user"`
	Reusable   bool   `json:"reusable"`
	Ephemeral  bool   `json:"ephemeral"`
	Expiration string `json:"expiration"` // RFC3339 格式
}

// CreatePreAuthKeyResponse 创建预认证密钥响应
type CreatePreAuthKeyResponse struct {
	PreAuthKey PreAuthKey `json:"preAuthKey"`
}

// ListNodesResponse 列出节点响应
type ListNodesResponse struct {
	Nodes []Node `json:"nodes"`
}

// ACLRule ACL 规则
type ACLRule struct {
	Action string   `json:"action"` // accept, deny
	Src    []string `json:"src"`    // 源地址列表
	Dst    []string `json:"dst"`    // 目标地址列表（格式：IP:port 或 IP:*）
}

// ACLPolicy ACL 策略
type ACLPolicy struct {
	ACLs      []ACLRule           `json:"acls"`
	Groups    map[string][]string `json:"groups,omitempty"`
	TagOwners map[string][]string `json:"tagOwners,omitempty"`
	Hosts     map[string]string   `json:"hosts,omitempty"`
}

// CreateUser 创建用户
func (c *Client) CreateUser(ctx context.Context, name string) (*User, error) {
	req := struct {
		Name string `json:"name"`
	}{
		Name: name,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.doRequest(ctx, "POST", "/api/v1/user", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create user failed: status=%d, body=%s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		User User `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result.User, nil
}

// GetOrCreateUser 获取或创建用户
func (c *Client) GetOrCreateUser(ctx context.Context, name string) (*User, error) {
	// 先尝试获取用户列表
	users, err := c.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}

	// 检查用户是否已存在
	for _, user := range users {
		if user.Name == name {
			return &user, nil
		}
	}

	// 用户不存在，创建新用户
	return c.CreateUser(ctx, name)
}

// CreatePreAuthKey 创建预认证密钥
// userID: 用户 ID（字符串格式，如 "1", "2"）
func (c *Client) CreatePreAuthKey(ctx context.Context, userID string, expiry time.Duration, ephemeral bool) (*PreAuthKey, error) {
	req := CreatePreAuthKeyRequest{
		User:       userID,
		Reusable:   false,
		Ephemeral:  ephemeral,
		Expiration: time.Now().Add(expiry).Format(time.RFC3339),
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.doRequest(ctx, "POST", "/api/v1/preauthkey", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create preauth key: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create preauth key failed: status=%d, body=%s", resp.StatusCode, string(bodyBytes))
	}

	var result CreatePreAuthKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result.PreAuthKey, nil
}

// ListNodes 列出所有节点
func (c *Client) ListNodes(ctx context.Context) ([]Node, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/node", nil)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list nodes failed: status=%d, body=%s", resp.StatusCode, string(bodyBytes))
	}

	var result ListNodesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return result.Nodes, nil
}

// GetNode 获取节点信息
func (c *Client) GetNode(ctx context.Context, nodeID string) (*Node, error) {
	resp, err := c.doRequest(ctx, "GET", fmt.Sprintf("/api/v1/node/%s", nodeID), nil)
	if err != nil {
		return nil, fmt.Errorf("get node: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get node failed: status=%d, body=%s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Node Node `json:"node"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result.Node, nil
}

// DeleteNode 删除节点
func (c *Client) DeleteNode(ctx context.Context, nodeID string) error {
	resp, err := c.doRequest(ctx, "DELETE", fmt.Sprintf("/api/v1/node/%s", nodeID), nil)
	if err != nil {
		return fmt.Errorf("delete node: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete node failed: status=%d, body=%s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// GetNodeByIP 根据 IP 获取节点
func (c *Client) GetNodeByIP(ctx context.Context, ip string) (*Node, error) {
	nodes, err := c.ListNodes(ctx)
	if err != nil {
		return nil, err
	}

	for _, node := range nodes {
		for _, nodeIP := range node.IPAddresses {
			if nodeIP == ip {
				return &node, nil
			}
		}
	}

	return nil, nil
}

// ListUsers 列出所有用户
func (c *Client) ListUsers(ctx context.Context) ([]User, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/user", nil)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list users failed: status=%d, body=%s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Users []User `json:"users"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return result.Users, nil
}

// GetACLPolicy 获取当前 ACL 策略
func (c *Client) GetACLPolicy(ctx context.Context) (*ACLPolicy, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/v1/policy", nil)
	if err != nil {
		return nil, fmt.Errorf("get acl policy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// 没有 ACL 策略，返回空策略
		return &ACLPolicy{ACLs: []ACLRule{}}, nil
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get acl policy failed: status=%d, body=%s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Policy string `json:"policy"` // Headscale 返回的是 JSON 字符串
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// 解析 policy JSON 字符串
	var policy ACLPolicy
	if result.Policy != "" {
		if err := json.Unmarshal([]byte(result.Policy), &policy); err != nil {
			return nil, fmt.Errorf("parse policy: %w", err)
		}
	}

	return &policy, nil
}

// SetACLPolicy 设置 ACL 策略
func (c *Client) SetACLPolicy(ctx context.Context, policy *ACLPolicy) error {
	// 将策略序列化为 JSON 字符串
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("marshal policy: %w", err)
	}

	// Headscale API 需要将策略作为字符串发送
	reqBody := struct {
		Policy string `json:"policy"`
	}{
		Policy: string(policyJSON),
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.doRequest(ctx, "PUT", "/api/v1/policy", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("set acl policy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("set acl policy failed: status=%d, body=%s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// doRequest 执行 HTTP 请求
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	return c.httpClient.Do(req)
}
