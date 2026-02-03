package auth

import (
	"net/http"
	"sync"

	"github.com/logto-io/go/v2/client"
	"github.com/logto-io/go/v2/core"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// LogtoClient Logto 客户端封装
type LogtoClient struct {
	config config.LogtoSection
}

// LogtoUserInfo Logto 用户信息
type LogtoUserInfo struct {
	Sub      string `json:"sub"`      // 用户 ID
	Username string `json:"username"` // 用户名
	Email    string `json:"email"`    // 邮箱
	Name     string `json:"name"`     // 显示名称
	Picture  string `json:"picture"`  // 头像 URL
}

// NewLogtoClient 创建 Logto 客户端
func NewLogtoClient(cfg config.LogtoSection) *LogtoClient {
	return &LogtoClient{
		config: cfg,
	}
}

// IsConfigured 检查 Logto 是否已配置
func (c *LogtoClient) IsConfigured() bool {
	return c.config.Endpoint != "" && c.config.AppID != ""
}

// GetConfig 获取配置
func (c *LogtoClient) GetConfig() config.LogtoSection {
	return c.config
}

// MemorySessionStorage 内存会话存储（实现 Logto SDK 的 Storage 接口）
type MemorySessionStorage struct {
	data map[string]string
	mu   sync.RWMutex
}

// NewMemorySessionStorage 创建内存会话存储
func NewMemorySessionStorage() *MemorySessionStorage {
	return &MemorySessionStorage{
		data: make(map[string]string),
	}
}

// GetItem 获取存储项
func (s *MemorySessionStorage) GetItem(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[key]
}

// SetItem 设置存储项
func (s *MemorySessionStorage) SetItem(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
}

// CreateLogtoClient 创建 Logto SDK 客户端实例
func (c *LogtoClient) CreateLogtoClient(storage *MemorySessionStorage) *client.LogtoClient {
	logtoConfig := &client.LogtoConfig{
		Endpoint: c.config.Endpoint,
		AppId:    c.config.AppID,
	}

	// 如果配置了 AppSecret，添加到配置中
	if c.config.AppSecret != "" {
		logtoConfig.AppSecret = c.config.AppSecret
	}

	// 如果配置了 Resource，添加到配置中
	if c.config.Resource != "" {
		logtoConfig.Resources = []string{c.config.Resource}
	}

	logtoClient := client.NewLogtoClient(logtoConfig, storage)
	return logtoClient
}

// GetSignInURL 获取登录 URL
func (c *LogtoClient) GetSignInURL(storage *MemorySessionStorage, loginHint string) (string, error) {
	logtoClient := c.CreateLogtoClient(storage)

	// 使用配置的回调 URL
	options := &client.SignInOptions{
		RedirectUri: c.config.CallbackURL,
	}

	if loginHint != "" {
		options.LoginHint = loginHint
	}

	signInURL, err := logtoClient.SignIn(options)
	if err != nil {
		logger.Errorf("生成 Logto 登录 URL 失败: %v", err)
		return "", err
	}

	logger.Infof("生成 Logto 登录 URL: %s", signInURL)
	return signInURL, nil
}

// HandleCallback 处理回调
func (c *LogtoClient) HandleCallback(storage *MemorySessionStorage, req *http.Request) (*LogtoUserInfo, error) {
	logtoClient := c.CreateLogtoClient(storage)

	// 直接使用传入的 http.Request
	// 处理回调
	err := logtoClient.HandleSignInCallback(req)
	if err != nil {
		logger.Errorf("处理 Logto 回调失败: %v", err)
		return nil, err
	}

	// 获取用户信息
	userInfoClaims, err := logtoClient.FetchUserInfo()
	if err != nil {
		logger.Errorf("获取 Logto 用户信息失败: %v", err)
		return nil, err
	}

	// 转换为我们的用户信息结构
	userInfo := &LogtoUserInfo{
		Sub:      userInfoClaims.Sub,
		Username: userInfoClaims.Username,
		Email:    userInfoClaims.Email,
		Name:     userInfoClaims.Name,
		Picture:  userInfoClaims.Picture,
	}

	logger.Infof("Logto 登录成功: user_id=%s, username=%s, email=%s",
		userInfo.Sub, userInfo.Username, userInfo.Email)

	return userInfo, nil
}

// IsAuthenticated 检查是否已认证
func (c *LogtoClient) IsAuthenticated(storage *MemorySessionStorage) bool {
	logtoClient := c.CreateLogtoClient(storage)
	return logtoClient.IsAuthenticated()
}

// GetIDTokenClaims 获取 ID Token Claims
func (c *LogtoClient) GetIDTokenClaims(storage *MemorySessionStorage) (core.IdTokenClaims, error) {
	logtoClient := c.CreateLogtoClient(storage)
	return logtoClient.GetIdTokenClaims()
}
