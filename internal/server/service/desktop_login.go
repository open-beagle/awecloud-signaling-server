package service

import (
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/auth"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// LoginResult 登录结果
type LoginResult struct {
	Success      bool
	UserID       uint64
	UserName     string
	Email        string
	DisplayName  string
	Avatar       string
	ErrorMessage string
}

// SessionData 会话数据（包含 Logto SDK 需要的存储）
type SessionData struct {
	Session *model.DesktopLoginSession
	Storage *auth.MemorySessionStorage
}

// DesktopLoginService Desktop 登录服务
type DesktopLoginService struct {
	config      *config.ServerConfig
	logtoClient *auth.LogtoClient
	// 登录结果通道，用于通知 gRPC 流
	loginResults     map[string]chan *LoginResult
	loginResultMutex sync.RWMutex
	// 会话存储（用于 Logto SDK）
	sessionStorages     map[string]*auth.MemorySessionStorage
	sessionStorageMutex sync.RWMutex
}

// NewDesktopLoginService 创建 Desktop 登录服务
func NewDesktopLoginService(cfg *config.ServerConfig) *DesktopLoginService {
	svc := &DesktopLoginService{
		config:          cfg,
		loginResults:    make(map[string]chan *LoginResult),
		sessionStorages: make(map[string]*auth.MemorySessionStorage),
	}
	if cfg.Logto.Endpoint != "" {
		svc.logtoClient = auth.NewLogtoClient(cfg.Logto)
	}
	return svc
}

// IsLogtoConfigured 检查 Logto 是否已配置
func (s *DesktopLoginService) IsLogtoConfigured() bool {
	return s.logtoClient != nil && s.logtoClient.IsConfigured()
}

// GetLogtoClient 获取 Logto 客户端
func (s *DesktopLoginService) GetLogtoClient() *auth.LogtoClient {
	return s.logtoClient
}

// RegisterLoginSession 注册登录会话，返回结果通道
func (s *DesktopLoginService) RegisterLoginSession(sessionID string) chan *LoginResult {
	s.loginResultMutex.Lock()
	defer s.loginResultMutex.Unlock()

	ch := make(chan *LoginResult, 1)
	s.loginResults[sessionID] = ch
	return ch
}

// UnregisterLoginSession 注销登录会话
func (s *DesktopLoginService) UnregisterLoginSession(sessionID string) {
	s.loginResultMutex.Lock()
	defer s.loginResultMutex.Unlock()

	if ch, exists := s.loginResults[sessionID]; exists {
		close(ch)
		delete(s.loginResults, sessionID)
	}

	// 同时清理会话存储
	s.sessionStorageMutex.Lock()
	delete(s.sessionStorages, sessionID)
	s.sessionStorageMutex.Unlock()
}

// NotifyLoginResult 通知登录结果
func (s *DesktopLoginService) NotifyLoginResult(sessionID string, result *LoginResult) {
	s.loginResultMutex.RLock()
	ch, exists := s.loginResults[sessionID]
	s.loginResultMutex.RUnlock()

	if exists {
		select {
		case ch <- result:
		default:
			logger.Warnf("登录结果通道已满或已关闭: sessionID=%s", sessionID)
		}
	}
}

// GetSessionStorage 获取会话存储
func (s *DesktopLoginService) GetSessionStorage(sessionID string) *auth.MemorySessionStorage {
	s.sessionStorageMutex.RLock()
	defer s.sessionStorageMutex.RUnlock()

	storage := s.sessionStorages[sessionID]
	if storage == nil {
		logger.Warnf("获取 Session 存储失败: sessionID=%s 不存在，当前存储数量=%d", sessionID, len(s.sessionStorages))
	} else {
		logger.Infof("获取 Session 存储成功: sessionID=%s", sessionID)
	}

	return storage
}

// CreateLoginSession 创建登录会话
func (s *DesktopLoginService) CreateLoginSession(deviceFingerprint, deviceName, usernameHint string) (*model.DesktopLoginSession, string, error) {
	sessionID := uuid.New().String()

	// 创建 Logto SDK 需要的会话存储
	storage := auth.NewMemorySessionStorage()

	// 保存会话存储
	s.sessionStorageMutex.Lock()
	s.sessionStorages[sessionID] = storage
	storageCount := len(s.sessionStorages)
	s.sessionStorageMutex.Unlock()

	logger.Infof("创建 Session 存储: sessionID=%s, 当前存储数量=%d", sessionID, storageCount)

	// 使用 Logto SDK 生成登录 URL
	loginURL, err := s.logtoClient.GetSignInURL(storage, sessionID)
	if err != nil {
		// 清理存储
		s.sessionStorageMutex.Lock()
		delete(s.sessionStorages, sessionID)
		s.sessionStorageMutex.Unlock()
		logger.Errorf("生成登录 URL 失败: %v", err)
		return nil, "", err
	}

	logger.Infof("生成登录 URL 成功: sessionID=%s, url=%s", sessionID, loginURL)

	// 创建会话记录
	session := &model.DesktopLoginSession{
		SessionID:         sessionID,
		DeviceFingerprint: deviceFingerprint,
		DeviceName:        deviceName,
		UsernameHint:      usernameHint,
		Status:            model.DesktopLoginSessionStatusPending,
		ExpiresAt:         time.Now().Add(10 * time.Minute), // 10 分钟过期
	}

	if err := db.DB.Create(session).Error; err != nil {
		// 清理存储
		s.sessionStorageMutex.Lock()
		delete(s.sessionStorages, sessionID)
		s.sessionStorageMutex.Unlock()
		logger.Errorf("创建会话记录失败: %v", err)
		return nil, "", err
	}

	logger.Infof("创建 Desktop 登录会话: sessionID=%s, device=%s", sessionID, deviceName)

	return session, loginURL, nil
}
