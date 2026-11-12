package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

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

// resolveLogtoSignInPrompt 解析浏览器登录会话策略：默认复用既有 SSO 登录态，仅切换账号（prompt=login）时强制重验
func resolveLogtoSignInPrompt(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), "login") {
		return "login"
	}
	return ""
}

// DesktopLoginRedirect 跳转至 Logto 授权页
// GET /auth/desktop/:session_id
func (a *DesktopAuthAPI) DesktopLoginRedirect(c *gin.Context) {
	sessionID := c.Param("session_id")
	ctx := c.Request.Context()

	var session model.DesktopLoginSession
	if err := db.WithContext(ctx).Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		a.renderCallbackError(c, "登录会话不存在")
		return
	}

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

	storage := a.loginService.GetSessionStorage(sessionID)
	if storage == nil {
		a.renderCallbackError(c, "登录会话存储不存在，请重新登录")
		return
	}

	loginHint := session.UsernameHint
	prompt := resolveLogtoSignInPrompt(c.Query("prompt"))
	loginURL, err := a.loginService.GetLogtoClient().GetSignInURLWithPrompt(storage, loginHint, prompt)
	if err != nil {
		logger.Errorf("生成 Logto 登录 URL 失败: %v", err)
		a.renderCallbackError(c, "生成登录链接失败")
		return
	}

	logger.Infof("跳转 Logto 登录页: sessionID=%s, loginHint=%s, prompt=%s", sessionID, loginHint, prompt)
	c.Redirect(http.StatusFound, loginURL)
}

// DesktopLoginCallback Logto 回调处理
// GET /auth/desktop/callback
func (a *DesktopAuthAPI) DesktopLoginCallback(c *gin.Context) {
	ctx := c.Request.Context()
	errorParam := c.Query("error")
	errorDesc := c.Query("error_description")

	logger.Infof("Desktop 登录回调: error=%s", errorParam)

	if errorParam != "" {
		logger.Errorf("Logto 回调错误: error=%s, description=%s", errorParam, errorDesc)
		a.renderCallbackError(c, "登录失败: "+errorDesc)
		return
	}

	var session model.DesktopLoginSession
	var storage *auth.MemorySessionStorage

	var sessions []model.DesktopLoginSession
	if err := db.WithContext(ctx).Where("status = ?", model.DesktopLoginSessionStatusPending).Find(&sessions).Error; err != nil {
		logger.Errorf("查询会话失败: %v", err)
		a.renderCallbackError(c, "查询会话失败")
		return
	}

	logger.Infof("找到 %d 个 pending 状态的会话", len(sessions))

	for _, s := range sessions {
		st := a.loginService.GetSessionStorage(s.SessionID)
		if st == nil {
			continue
		}

		logtoClient := a.loginService.GetLogtoClient()
		userInfo, err := logtoClient.HandleCallback(st, c.Request)
		if err != nil {
			logger.Warnf("会话 %s 处理回调失败: %v", s.SessionID, err)
			continue
		}

		logger.Infof("找到匹配的会话: sessionID=%s, sub=%s, user=%s", s.SessionID, userInfo.Sub, userInfo.Username)
		session = s
		storage = st

		user, isDisabled, err := a.findOrCreateUser(ctx, userInfo)
		if err != nil {
			logger.Errorf("同步 Logto 用户失败: %v", err)
			session.Fail("同步用户身份失败")
			db.WithContext(ctx).Save(&session)
			a.loginService.NotifyLoginResult(session.SessionID, &service.LoginResult{
				Success:      false,
				ErrorMessage: "同步用户身份失败",
			})
			a.renderCallbackError(c, "登录失败，请重试")
			return
		}

		if isDisabled {
			errMsg := "用户已被管理员禁用，请联系管理员"
			logger.Warnf("用户已被管理员禁用: sessionID=%s, user=%s", session.SessionID, user.Name)
			session.Fail("用户已禁用")
			db.WithContext(ctx).Save(&session)
			a.loginService.NotifyLoginResult(session.SessionID, &service.LoginResult{
				Success:      false,
				IsDisabled:   true,
				ErrorMessage: errMsg,
			})
			a.renderCallbackError(c, errMsg)
			return
		}

		session.Complete(user.ID, "")
		db.WithContext(ctx).Save(&session)
		a.loginService.BindUserSession(user.ID, session.SessionID)

		a.loginService.NotifyLoginResult(session.SessionID, &service.LoginResult{
			Success:     true,
			UserID:      user.ID,
			UserName:    user.Name,
			Email:       userInfo.Email,
			DisplayName: userInfo.Name,
			Avatar:      userInfo.Picture,
		})

		logger.Infof("Desktop 登录成功: sessionID=%s, user=%s", session.SessionID, user.Name)
		a.renderCallbackSuccess(c)
		return
	}

	logger.Errorf("没有找到匹配的会话，共尝试了 %d 个会话", len(sessions))
	if storage == nil {
		a.renderCallbackError(c, "登录会话不存在或已过期")
		return
	}
}

func normalizeLogtoIdentifier(raw string) string {
	lower := strings.ToLower(strings.TrimSpace(raw))
	if lower == "" {
		return ""
	}
	var b strings.Builder
	prevDash := false
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func normalizeLogtoUsername(userInfo *auth.LogtoUserInfo) string {
	candidates := []string{userInfo.Username}
	if userInfo.Email != "" {
		parts := strings.SplitN(userInfo.Email, "@", 2)
		candidates = append(candidates, parts[0])
	}
	candidates = append(candidates, userInfo.Sub)

	for _, c := range candidates {
		if slug := normalizeLogtoIdentifier(c); slug != "" {
			if len(slug) > 80 {
				slug = strings.Trim(slug[:80], "-")
			}
			if slug != "" {
				return slug
			}
		}
	}
	return "logto-user"
}

func generatePlaceholderSecretHash() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "oidc-managed-identity"
	}
	return "oidc:" + hex.EncodeToString(buf)
}

// findOrCreateUser 直接信任上游受邀制 Logto 授权用户并绑定 OIDC 身份；仅已绑定且被管理员显式禁用的账号返回 isDisabled=true
func (a *DesktopAuthAPI) findOrCreateUser(_ interface{}, userInfo *auth.LogtoUserInfo) (*model.User, bool, error) {
	userName := normalizeLogtoUsername(userInfo)
	displayName := strings.TrimSpace(userInfo.Name)
	if displayName == "" {
		displayName = userName
	}
	sub := strings.TrimSpace(userInfo.Sub)

	var resolved *model.User
	var isDisabled bool

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		// 1. 优先按 OIDC (provider_type=oidc, provider_subject=sub) 精确匹配已绑定身份
		if sub != "" && tx.Migrator().HasTable(&model.UserAuthenticationLink{}) {
			var link model.UserAuthenticationLink
			linkErr := tx.Where(
				"provider_type = ? AND provider_subject = ?",
				model.AuthenticationProviderOIDC,
				sub,
			).First(&link).Error
			if linkErr == nil {
				var boundUser model.User
				if err := tx.First(&boundUser, link.UserID).Error; err != nil {
					return err
				}
				if !boundUser.Enabled || !link.Enabled {
					resolved = &boundUser
					isDisabled = true
					return nil
				}
				if strings.TrimSpace(userInfo.Name) != "" && boundUser.Alias != displayName {
					if err := tx.Model(&boundUser).Update("alias", displayName).Error; err != nil {
						return err
					}
					boundUser.Alias = displayName
				}
				if err := ensureOIDCIdentityBinding(tx, &boundUser, sub, displayName); err != nil {
					return err
				}
				if err := ensureDefaultBeagleTenantMembership(tx, boundUser.ID); err != nil {
					return err
				}
				resolved = &boundUser
				return nil
			} else if !errors.Is(linkErr, gorm.ErrRecordNotFound) {
				return linkErr
			}
		}

		// 2. 尚未绑定 OIDC Link：忽略大小写查找同名 client 账号（含自动转正旧版未绑定的 logto 待审记录）
		var existing model.User
		findErr := tx.Where("LOWER(name) = ? AND role = ?", userName, model.UserRoleClient).First(&existing).Error
		if findErr == nil {
			if !existing.Enabled && existing.Source != model.UserSourceLogto {
				resolved = &existing
				isDisabled = true
				return nil
			}

			updates := map[string]any{"enabled": true}
			if existing.Name != userName {
				var collisionCount int64
				if err := tx.Model(&model.User{}).Where("name = ? AND id <> ?", userName, existing.ID).Count(&collisionCount).Error; err != nil {
					return err
				}
				if collisionCount == 0 {
					updates["name"] = userName
					existing.Name = userName
				}
			}
			if strings.TrimSpace(userInfo.Name) != "" && existing.Alias != displayName {
				updates["alias"] = displayName
				existing.Alias = displayName
			}
			if err := tx.Model(&existing).Updates(updates).Error; err != nil {
				return err
			}
			existing.Enabled = true

			if err := ensureOIDCIdentityBinding(tx, &existing, sub, displayName); err != nil {
				return err
			}
			if err := ensureDefaultBeagleTenantMembership(tx, existing.ID); err != nil {
				return err
			}
			resolved = &existing
			return nil
		} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}

		// 3. 若因非 client 角色占用同名 uniqueIndex，追加 -client 后缀避免冲突
		finalName := userName
		var anyNameCount int64
		if err := tx.Model(&model.User{}).Where("LOWER(name) = ?", finalName).Count(&anyNameCount).Error; err != nil {
			return err
		}
		if anyNameCount > 0 {
			finalName = fmt.Sprintf("%s-client", userName)
		}

		created := model.User{
			Name:       finalName,
			Alias:      displayName,
			Role:       model.UserRoleClient,
			SecretHash: generatePlaceholderSecretHash(),
			Enabled:    true,
			Source:     model.UserSourceLogto,
		}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}

		if err := ensureOIDCIdentityBinding(tx, &created, sub, displayName); err != nil {
			return err
		}
		if err := ensureDefaultBeagleTenantMembership(tx, created.ID); err != nil {
			return err
		}

		logger.Infof("自动创建并启用 Logto 授权用户: id=%d, name=%s, alias=%s", created.ID, created.Name, created.Alias)
		resolved = &created
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return resolved, isDisabled, nil
}

func ensureOIDCIdentityBinding(tx *gorm.DB, user *model.User, sub, displayName string) error {
	if !tx.Migrator().HasTable(&model.UserIdentityProfile{}) {
		return nil
	}

	var profile model.UserIdentityProfile
	err := tx.Where("user_id = ?", user.ID).First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		profileUsername := user.Name
		var usernameConflict int64
		if err := tx.Model(&model.UserIdentityProfile{}).Where("username = ?", profileUsername).Count(&usernameConflict).Error; err != nil {
			return err
		}
		if usernameConflict > 0 {
			profileUsername = fmt.Sprintf("%s-%d", user.Name, user.ID)
		}
		profile = model.UserIdentityProfile{
			UserID:       user.ID,
			Username:     profileUsername,
			DisplayName:  displayName,
			Enabled:      true,
			AuthRevision: 1,
			RowVersion:   1,
		}
		if err := tx.Create(&profile).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		updates := map[string]any{}
		if !profile.Enabled {
			updates["enabled"] = true
		}
		if displayName != "" && profile.DisplayName != displayName {
			updates["display_name"] = displayName
		}
		if len(updates) > 0 {
			if err := tx.Model(&profile).Updates(updates).Error; err != nil {
				return err
			}
		}
	}

	if sub == "" || !tx.Migrator().HasTable(&model.UserAuthenticationLink{}) {
		return nil
	}

	var link model.UserAuthenticationLink
	linkErr := tx.Where(
		"provider_type = ? AND provider_subject = ?",
		model.AuthenticationProviderOIDC,
		sub,
	).First(&link).Error
	if errors.Is(linkErr, gorm.ErrRecordNotFound) {
		link = model.UserAuthenticationLink{
			ID:                 uuid.NewString(),
			UserID:             user.ID,
			ProviderType:       model.AuthenticationProviderOIDC,
			ProviderSubject:    sub,
			CredentialRevision: 1,
			Enabled:            true,
			RowVersion:         1,
		}
		return tx.Create(&link).Error
	}
	return linkErr
}

func ensureDefaultBeagleTenantMembership(tx *gorm.DB, userID uint64) error {
	if !tx.Migrator().HasTable(&model.Tenant{}) || !tx.Migrator().HasTable(&model.TenantMembership{}) {
		return nil
	}

	var count int64
	if err := tx.Model(&model.TenantMembership{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	var tenant model.Tenant
	if err := tx.Where("key = ?", "beagle").First(&tenant).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	membership := model.TenantMembership{
		TenantID: tenant.ID,
		UserID:   userID,
		Role:     "member",
		Enabled:  true,
	}
	return tx.Create(&membership).Error
}

// renderCallbackSuccess 渲染系统浏览器回调成功页面
func (a *DesktopAuthAPI) renderCallbackSuccess(c *gin.Context) {
	page := `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>登录成功 - Signaling Desktop</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; background: #f5f7fa; }
        .container { text-align: center; padding: 44px 48px; background: white; border-radius: 12px; box-shadow: 0 6px 24px rgba(0,0,0,0.08); max-width: 420px; }
        .icon { font-size: 56px; color: #137440; line-height: 1; }
        h1 { color: #1f2329; margin: 20px 0 10px; font-size: 24px; }
        p { color: #646a73; margin: 0; line-height: 1.6; }
    </style>
</head>
<body>
    <div class="container">
        <div class="icon">✓</div>
        <h1>登录成功</h1>
        <p>身份已同步至 Signaling Desktop 客户端，您可以直接关闭此浏览器标签页。</p>
    </div>
</body>
</html>`
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
}

// renderCallbackError 渲染系统浏览器回调错误页面
func (a *DesktopAuthAPI) renderCallbackError(c *gin.Context, message string) {
	safeMsg := html.EscapeString(message)
	page := `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>登录失败 - Signaling Desktop</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; background: #f5f7fa; }
        .container { text-align: center; padding: 44px 48px; background: white; border-radius: 12px; box-shadow: 0 6px 24px rgba(0,0,0,0.08); max-width: 420px; }
        .icon { font-size: 56px; color: #f54a45; line-height: 1; }
        h1 { color: #1f2329; margin: 20px 0 10px; font-size: 24px; }
        p { color: #646a73; margin: 0; line-height: 1.6; }
    </style>
</head>
<body>
    <div class="container">
        <div class="icon">✗</div>
        <h1>登录失败</h1>
        <p>` + safeMsg + `</p>
    </div>
</body>
</html>`
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
}
