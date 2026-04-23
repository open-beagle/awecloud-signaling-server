package service

import (
	"fmt"
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
	IsDisabled   bool // 用户已禁用/待审批
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
	// 用户 ID → 会话 ID 映射（用于注销时反查 Storage）
	userSessions     map[uint64]string
	userSessionMutex sync.RWMutex
}

// NewDesktopLoginService 创建 Desktop 登录服务
func NewDesktopLoginService(cfg *config.ServerConfig) *DesktopLoginService {
	svc := &DesktopLoginService{
		config:          cfg,
		loginResults:    make(map[string]chan *LoginResult),
		sessionStorages: make(map[string]*auth.MemorySessionStorage),
		userSessions:    make(map[uint64]string),
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

// GetLoginResultChannel 获取登录结果通道
func (s *DesktopLoginService) GetLoginResultChannel(sessionID string) chan *LoginResult {
	s.loginResultMutex.RLock()
	defer s.loginResultMutex.RUnlock()

	return s.loginResults[sessionID]
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
// 注意：此方法只创建会话和 Storage，不生成 Logto 登录 URL
// Logto URL 在 DesktopLoginRedirect（WebView 访问时）才生成，避免重复调用覆盖 state
func (s *DesktopLoginService) CreateLoginSession(deviceFingerprint, deviceName, usernameHint string) (*model.DesktopLoginSession, string, error) {
	sessionID := uuid.New().String()

	// 创建 Logto SDK 需要的会话存储（URL 稍后在 DesktopLoginRedirect 中生成）
	storage := auth.NewMemorySessionStorage()

	// 保存会话存储
	s.sessionStorageMutex.Lock()
	s.sessionStorages[sessionID] = storage
	storageCount := len(s.sessionStorages)
	s.sessionStorageMutex.Unlock()

	logger.Infof("创建 Session 存储: sessionID=%s, 当前存储数量=%d", sessionID, storageCount)

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

	return session, "", nil
}

// RegisterSessionStorage 注册会话存储
func (s *DesktopLoginService) RegisterSessionStorage(sessionID string, storage *auth.MemorySessionStorage) {
	s.sessionStorageMutex.Lock()
	defer s.sessionStorageMutex.Unlock()

	s.sessionStorages[sessionID] = storage
	logger.Infof("注册 Session 存储: sessionID=%s, 当前存储数量=%d", sessionID, len(s.sessionStorages))
}

// BindUserSession 绑定用户 ID 与会话 ID 的映射（登录成功后调用）
func (s *DesktopLoginService) BindUserSession(userID uint64, sessionID string) {
	s.userSessionMutex.Lock()
	defer s.userSessionMutex.Unlock()

	s.userSessions[userID] = sessionID
	logger.Infof("绑定用户会话: userID=%d, sessionID=%s", userID, sessionID)
}

// LogoutSession 注销用户的 Logto 会话
// 通过 userID 反查 sessionID，获取 Storage 后调用 Logto SignOut
// 返回 Logto 注销 URL（需要浏览器访问以清除 cookie）
func (s *DesktopLoginService) LogoutSession(userID uint64) string {
	if s.logtoClient == nil || !s.logtoClient.IsConfigured() {
		logger.Warnf("Logto 未配置，跳过上游注销: userID=%d", userID)
		return ""
	}

	// 通过 userID 查找 sessionID
	s.userSessionMutex.RLock()
	sessionID, exists := s.userSessions[userID]
	s.userSessionMutex.RUnlock()

	if !exists {
		logger.Warnf("未找到用户会话映射，跳过 Logto 注销: userID=%d", userID)
		return ""
	}

	// 获取 SessionStorage
	s.sessionStorageMutex.RLock()
	storage := s.sessionStorages[sessionID]
	s.sessionStorageMutex.RUnlock()

	if storage == nil {
		logger.Warnf("Session 存储不存在（可能 Server 已重启），跳过 Logto 注销: userID=%d, sessionID=%s", userID, sessionID)
		// 清理映射
		s.userSessionMutex.Lock()
		delete(s.userSessions, userID)
		s.userSessionMutex.Unlock()
		return ""
	}

	// 调用 Logto SignOut（撤销 token + 生成注销 URL）
	postLogoutURI := s.config.Logto.CallbackURL
	if postLogoutURI == "" {
		postLogoutURI = "https://localhost/logout-callback"
	}

	logger.Infof("调用 Logto 注销: userID=%d, sessionID=%s", userID, sessionID)
	logoutURL, err := s.logtoClient.SignOut(storage, postLogoutURI)
	if err != nil {
		logger.Warnf("Logto 注销失败（忽略）: userID=%d, err=%v", userID, err)
	}

	// 清理 Storage 和映射
	s.sessionStorageMutex.Lock()
	delete(s.sessionStorages, sessionID)
	s.sessionStorageMutex.Unlock()

	s.userSessionMutex.Lock()
	delete(s.userSessions, userID)
	s.userSessionMutex.Unlock()

	logger.Infof("用户 Logto 会话已注销: userID=%d, sessionID=%s, logoutURL=%s", userID, sessionID, logoutURL)
	return logoutURL
}

// GetLoginResult 非阻塞查询登录结果（REST 轮询用）
// 返回结果 map 和 error；如果还没有结果返回 error
func (s *DesktopLoginService) GetLoginResult(sessionID, deviceFingerprint string) (map[string]any, error) {
	// 查数据库中的会话状态
	var session model.DesktopLoginSession
	if err := db.DB.Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		return nil, fmt.Errorf("会话不存在")
	}

	// 检查是否过期
	if time.Now().After(session.ExpiresAt) {
		return map[string]any{
			"status":  "expired",
			"message": "登录会话已过期",
		}, nil
	}

	// 检查会话状态
	switch session.Status {
	case model.DesktopLoginSessionStatusCompleted:
		return map[string]any{
			"status":  "success",
			"message": "登录成功",
			"user_id": session.UserID,
		}, nil

	case model.DesktopLoginSessionStatusFailed:
		return map[string]any{
			"status":  "failed",
			"message": session.ErrorMessage,
		}, nil

	case model.DesktopLoginSessionStatusExpired:
		return map[string]any{
			"status":  "expired",
			"message": "登录会话已过期",
		}, nil

	default:
		// pending 状态
		return nil, fmt.Errorf("等待登录")
	}
}
