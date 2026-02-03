package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

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

// DesktopLoginRedirect 重定向到 Logto 登录页面
// GET /auth/desktop/:session_id
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

		// 清理会话存储（延迟清理，给 gRPC 流一点时间接收结果）
		go func() {
			time.Sleep(5 * time.Second)
			a.loginService.UnregisterLoginSession(session.SessionID)
			logger.Infof("清理登录会话存储: sessionID=%s", session.SessionID)
		}()

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
