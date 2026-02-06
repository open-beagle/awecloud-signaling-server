package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/auth"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

// DesktopAuthAPI Desktop 认证 API
type DesktopAuthAPI struct {
	config       *config.ServerConfig
	loginService *service.DesktopLoginService
}

// NewDesktopAuthAPI 创建 Desktop 认证 API
func NewDesktopAuthAPI(cfg *config.ServerConfig, loginService *service.DesktopLoginService) *DesktopAuthAPI {
	return &DesktopAuthAPI{
		config:       cfg,
		loginService: loginService,
	}
}

// IsLogtoConfigured 检查 Logto 是否已配置
func (a *DesktopAuthAPI) IsLogtoConfigured() bool {
	return a.loginService != nil && a.loginService.IsLogtoConfigured()
}

// GetLoginURL 获取登录页面 URL
// GET /api/auth/desktop/login-url?username_hint=xxx
// 返回 Server 页面 URL，Desktop 在 WebView 中打开此 URL
// 用户在此页面完成 Logto 登录，Server 通过 gRPC 流推送结果给 Desktop
func (a *DesktopAuthAPI) GetLoginURL(c *gin.Context) {
	usernameHint := c.Query("username_hint")
	ctx := c.Request.Context()

	logger.Infof("Desktop 获取登录 URL: username_hint=%s", usernameHint)

	// 检查 Logto 是否配置
	if !a.IsLogtoConfigured() {
		logger.Errorf("Logto 未配置")
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Logto 未配置，请联系管理员",
		})
		return
	}

	// 创建登录会话
	session := model.DesktopLoginSession{
		SessionID: generateSessionID(),
		Status:    model.DesktopLoginSessionStatusPending,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	if err := db.WithContext(ctx).Create(&session).Error; err != nil {
		logger.Errorf("创建登录会话失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "创建登录会话失败",
		})
		return
	}

	// 注册登录会话（获取结果通道）
	// 这必须在这里完成，因为回调可能在 WaitForLoginResult 之前发生
	_ = a.loginService.RegisterLoginSession(session.SessionID)

	// 创建会话存储
	storage := auth.NewMemorySessionStorage()
	a.loginService.RegisterSessionStorage(session.SessionID, storage)

	// 生成 Logto 登录 URL
	logtoLoginURL, err := a.loginService.GetLogtoClient().GetSignInURL(storage, session.SessionID)
	if err != nil {
		logger.Errorf("生成登录 URL 失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "生成登录链接失败",
		})
		return
	}

	// 构建 Server 页面 URL（Desktop 在 WebView 中打开此 URL）
	// 格式：http://server/auth/desktop/{sessionId}
	// 此页面会重定向到 Logto 登录页面
	serverPageURL := "/auth/desktop/" + session.SessionID

	logger.Infof("登录 URL 生成成功: sessionID=%s, serverPageURL=%s, logtoURL=%s", session.SessionID, serverPageURL, logtoLoginURL)

	c.JSON(http.StatusOK, gin.H{
		"login_url": serverPageURL,
		"session_id": session.SessionID,
		"message":   "success",
	})
}

// GetLoginResult 获取登录结果
// GET /api/auth/desktop/login-result?session_id=xxx
func (a *DesktopAuthAPI) GetLoginResult(c *gin.Context) {
	sessionID := c.Query("session_id")
	ctx := c.Request.Context()

	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "缺少 session_id 参数",
		})
		return
	}

	logger.Infof("Desktop 获取登录结果: sessionID=%s", sessionID)

	// 查找会话
	var session model.DesktopLoginSession
	if err := db.WithContext(ctx).Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		logger.Errorf("查找登录会话失败: %v", err)
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "登录会话不存在",
		})
		return
	}

	// 检查会话状态
	if session.Status == model.DesktopLoginSessionStatusPending {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "登录未完成，请稍候",
		})
		return
	}

	if session.Status == model.DesktopLoginSessionStatusFailed {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": session.ErrorMessage,
		})
		return
	}

	if session.Status == model.DesktopLoginSessionStatusExpired {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "登录会话已过期",
		})
		return
	}

	// 登录成功
	if session.Status == model.DesktopLoginSessionStatusCompleted {
		// 查找用户
		var user model.User
		if err := db.WithContext(ctx).Where("id = ?", session.UserID).First(&user).Error; err != nil {
			logger.Errorf("查找用户失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "查找用户失败",
			})
			return
		}

		// 生成 Desktop 凭证
		desktopID, secret, authKey, err := a.generateDesktopCredentials(ctx, user.ID)
		if err != nil {
			logger.Errorf("生成 Desktop 凭证失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "生成凭证失败",
			})
			return
		}

		logger.Infof("Desktop 登录成功: sessionID=%s, user=%s, desktopID=%d", sessionID, user.Name, desktopID)

		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"desktop_id": desktopID,
			"secret":     secret,
			"auth_key":   authKey,
			"server_url": a.config.Tailscale.HeadscalePublicURL,
			"username":   user.Name,
			"message":    "登录成功",
		})
		return
	}

	c.JSON(http.StatusInternalServerError, gin.H{
		"success": false,
		"message": "未知的会话状态",
	})
}

// DesktopLoginRedirect 显示登录页面
// GET /auth/desktop/:session_id
// 此页面显示 Logto 登录表单，用户在此完成登录
func (a *DesktopAuthAPI) DesktopLoginRedirect(c *gin.Context) {
	sessionID := c.Param("session_id")
	ctx := c.Request.Context()

	// 查找会话
	var session model.DesktopLoginSession
	if err := db.WithContext(ctx).Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		a.renderCallbackError(c, "登录会话不存在")
		return
	}

	// 检查会话状态
	if !session.IsPending() {
		a.renderCallbackError(c, "登录会话已完成或已过期")
		return
	}

	if session.IsExpired() {
		session.Expire()
		db.WithContext(ctx).Save(&session)
		a.renderCallbackError(c, "登录会话已过期，请重新登录")
		return
	}

	// 获取会话存储
	storage := a.loginService.GetSessionStorage(sessionID)
	if storage == nil {
		a.renderCallbackError(c, "登录会话存储不存在，请重新登录")
		return
	}

	// 使用 Logto SDK 生成登录 URL
	loginURL, err := a.loginService.GetLogtoClient().GetSignInURL(storage, sessionID)
	if err != nil {
		logger.Errorf("生成 Logto 登录 URL 失败: %v", err)
		a.renderCallbackError(c, "生成登录链接失败")
		return
	}

	logger.Infof("显示登录页面: sessionID=%s, loginURL=%s", sessionID, loginURL)

	// 重定向到 Logto 登录页面
	c.Redirect(http.StatusFound, loginURL)
}

// DesktopLoginCallback Logto 回调处理
// GET /auth/desktop/callback
func (a *DesktopAuthAPI) DesktopLoginCallback(c *gin.Context) {
	ctx := c.Request.Context()
	errorParam := c.Query("error")
	errorDesc := c.Query("error_description")

	logger.Infof("Desktop 登录回调: error=%s, query=%s", errorParam, c.Request.URL.RawQuery)

	// 处理错误
	if errorParam != "" {
		logger.Errorf("Logto 回调错误: error=%s, description=%s", errorParam, errorDesc)
		a.renderCallbackError(c, "登录失败: "+errorDesc)
		return
	}

	logger.Infof("处理回调请求: URL=%s", c.Request.URL.String())

	// 从 state 参数中获取 session_id
	// Logto SDK 会在 state 中编码信息，我们需要遍历所有会话来找到匹配的
	var session model.DesktopLoginSession
	var storage *auth.MemorySessionStorage

	// 查找所有 pending 状态的会话
	var sessions []model.DesktopLoginSession
	if err := db.WithContext(ctx).Where("status = ?", model.DesktopLoginSessionStatusPending).Find(&sessions).Error; err != nil {
		logger.Errorf("查询会话失败: %v", err)
		a.renderCallbackError(c, "查询会话失败")
		return
	}

	logger.Infof("找到 %d 个 pending 状态的会话", len(sessions))

	// 尝试用每个会话的存储来处理回调
	for _, s := range sessions {
		logger.Infof("尝试会话: sessionID=%s", s.SessionID)

		st := a.loginService.GetSessionStorage(s.SessionID)
		if st == nil {
			logger.Warnf("会话 %s 的存储不存在", s.SessionID)
			continue
		}

		// 尝试处理回调 - 直接传入 Gin 的 Request
		logtoClient := a.loginService.GetLogtoClient()
		userInfo, err := logtoClient.HandleCallback(st, c.Request)
		if err != nil {
			logger.Warnf("会话 %s 处理回调失败: %v", s.SessionID, err)
			continue // 不是这个会话，继续尝试
		}

		// 找到了匹配的会话
		logger.Infof("找到匹配的会话: sessionID=%s, user=%s", s.SessionID, userInfo.Username)
		session = s
		storage = st

		// 处理成功，创建或查找用户
		user, err := a.findOrCreateUser(ctx, userInfo)
		if err != nil {
			logger.Errorf("创建用户失败: %v", err)
			session.Fail("创建用户失败")
			db.WithContext(ctx).Save(&session)
			a.loginService.NotifyLoginResult(session.SessionID, &service.LoginResult{
				Success:      false,
				ErrorMessage: "创建用户失败",
			})
			a.renderCallbackError(c, "登录失败，请重试")
			return
		}

		// 更新会话状态
		session.Complete(user.ID, "")
		db.WithContext(ctx).Save(&session)

		// 通知 gRPC 流
		a.loginService.NotifyLoginResult(session.SessionID, &service.LoginResult{
			Success:     true,
			UserID:      user.ID,
			UserName:    user.Name,
			Email:       userInfo.Email,
			DisplayName: userInfo.Name,
			Avatar:      userInfo.Picture,
		})

		logger.Infof("Desktop 登录成功: sessionID=%s, user=%s", session.SessionID, user.Name)

		// 显示成功页面
		a.renderCallbackSuccess(c)
		return
	}

	// 没有找到匹配的会话
	logger.Errorf("没有找到匹配的会话，共尝试了 %d 个会话", len(sessions))
	if storage == nil {
		a.renderCallbackError(c, "登录会话不存在或已过期")
		return
	}
}

// findOrCreateUser 查找或创建用户
func (a *DesktopAuthAPI) findOrCreateUser(ctx interface{}, userInfo *auth.LogtoUserInfo) (*model.User, error) {
	// 优先使用 username，如果没有则使用 email 前缀，最后使用 sub
	userName := userInfo.Username
	if userName == "" && userInfo.Email != "" {
		parts := strings.Split(userInfo.Email, "@")
		userName = parts[0]
	}
	if userName == "" {
		userName = userInfo.Sub
	}

	// 查找现有用户
	var user model.User
	err := db.DB.Where("name = ? AND role = ?", userName, model.UserRoleClient).First(&user).Error
	if err == nil {
		return &user, nil
	}

	// 创建新用户
	user = model.User{
		Name:  userName,
		Alias: userInfo.Name,
		Role:  model.UserRoleClient,
	}

	if err := db.DB.Create(&user).Error; err != nil {
		return nil, err
	}

	logger.Infof("创建新用户: name=%s, alias=%s", user.Name, user.Alias)
	return &user, nil
}

// renderCallbackSuccess 渲染成功页面
func (a *DesktopAuthAPI) renderCallbackSuccess(c *gin.Context) {
	html := `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>登录成功</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; background: #f5f5f5; }
        .container { text-align: center; padding: 40px; background: white; border-radius: 8px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        .icon { font-size: 64px; color: #52c41a; }
        h1 { color: #333; margin: 20px 0 10px; }
        p { color: #666; }
    </style>
</head>
<body>
    <div class="container">
        <div class="icon">✓</div>
        <h1>登录成功</h1>
        <p>您可以关闭此页面，返回 Desktop 客户端</p>
    </div>
</body>
</html>`
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

// renderCallbackError 渲染错误页面
func (a *DesktopAuthAPI) renderCallbackError(c *gin.Context, message string) {
	html := `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>登录失败</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; background: #f5f5f5; }
        .container { text-align: center; padding: 40px; background: white; border-radius: 8px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        .icon { font-size: 64px; color: #ff4d4f; }
        h1 { color: #333; margin: 20px 0 10px; }
        p { color: #666; }
    </style>
</head>
<body>
    <div class="container">
        <div class="icon">✗</div>
        <h1>登录失败</h1>
        <p>` + message + `</p>
    </div>
</body>
</html>`
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}


// generateSessionID 生成会话 ID
func generateSessionID() string {
	return uuid.New().String()
}

// generateDesktopCredentials 生成 Desktop 凭证
func (a *DesktopAuthAPI) generateDesktopCredentials(ctx interface{}, userID uint64) (uint64, string, string, error) {
	// 1. 为用户创建 DeviceToken
	deviceToken := model.DeviceToken{
		ClientID:          int64(userID),
		DeviceToken:       generateDeviceToken(),
		DeviceFingerprint: generateDeviceFingerprint(),
		LastUsedAt:        time.Now(),
		ExpiresAt:         time.Now().AddDate(1, 0, 0), // 1 年过期
	}

	if err := db.DB.Create(&deviceToken).Error; err != nil {
		logger.Errorf("创建 DeviceToken 失败: %v", err)
		return 0, "", "", err
	}

	logger.Infof("创建 DeviceToken: id=%d, token=%s", deviceToken.ID, maskToken(deviceToken.DeviceToken))

	// 2. 在 Headscale 中创建 Node 并获取 AuthKey
	// TODO: 调用 Headscale API 创建 Node 和 PreAuthKey
	// 暂时使用临时值
	authKey := "temp-auth-key-" + generateDeviceToken()[:16]

	logger.Infof("生成 Desktop 凭证: desktopID=%d, authKey=%s", deviceToken.ID, maskToken(authKey))

	return uint64(deviceToken.ID), deviceToken.DeviceToken, authKey, nil
}

// generateDeviceToken 生成设备令牌
func generateDeviceToken() string {
	return uuid.New().String()
}

// generateDeviceFingerprint 生成设备指纹
func generateDeviceFingerprint() string {
	return uuid.New().String()
}

// maskToken 隐藏 token 中间部分，用于日志
func maskToken(token string) string {
	if token == "" {
		return "<empty>"
	}
	if len(token) <= 10 {
		return "***"
	}
	return token[:5] + "***" + token[len(token)-5:]
}
