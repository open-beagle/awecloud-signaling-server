package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func AuthMiddleware(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 调试模式：允许从 localhost 访问时跳过认证
		// 这样可以在容器内部使用 curl http://127.0.0.1:8080 调用管理 API
		// 检查多个可能的 IP 来源
		clientIP := c.ClientIP()
		remoteAddr := c.Request.RemoteAddr
		xForwardedFor := c.GetHeader("X-Forwarded-For")
		xRealIP := c.GetHeader("X-Real-IP")

		// 判断是否为 localhost 请求
		isLocalhost := clientIP == "127.0.0.1" || clientIP == "::1" || clientIP == "localhost" ||
			remoteAddr == "127.0.0.1" || remoteAddr == "[::1]" ||
			strings.HasPrefix(remoteAddr, "127.0.0.1:") || strings.HasPrefix(remoteAddr, "[::1]:")

		if isLocalhost {
			// 设置默认的管理员信息（用于日志记录）
			c.Set("admin_id", float64(0))
			c.Set("username", "localhost-debug")
			c.Next()
			return
		}

		// 调试日志：记录被拒绝的请求信息
		if xForwardedFor == "" && xRealIP == "" {
			// 只在非代理请求时记录，避免日志过多
			logger.Debugf("Auth rejected: clientIP=%s, remoteAddr=%s", clientIP, remoteAddr)
		}

		// 获取Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "未提供认证信息",
			})
			c.Abort()
			return
		}

		// 解析Bearer Token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "认证格式错误",
			})
			c.Abort()
			return
		}

		tokenString := parts[1]

		// 验证Token
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "Token无效或已过期",
			})
			c.Abort()
			return
		}

		// 提取claims
		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			c.Set("admin_id", claims["admin_id"])
			c.Set("username", claims["username"])
		}

		c.Next()
	}
}

// ManagementAuthorizationMiddleware protects legacy global management APIs
// from the new tenant-scoped role. Individual unified-resource handlers still
// enforce the selected Tenant and membership after this coarse route gate.
func ManagementAuthorizationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		adminID := getAdminIDFromContext(c)
		if adminID == 0 {
			c.Next()
			return
		}
		var admin model.Admin
		if err := db.DB.WithContext(c.Request.Context()).Where("id = ? AND enabled = ?", adminID, true).First(&admin).Error; err != nil {
			c.JSON(http.StatusUnauthorized, NewErrorResponse("管理员身份无效"))
			c.Abort()
			return
		}
		allowed := false
		switch admin.Role {
		case "admin":
			allowed = true
		case "viewer":
			isManagementAccountRoute := strings.HasPrefix(c.Request.URL.Path, "/api/v1/admin/management-accounts")
			allowed = !isManagementAccountRoute && (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions ||
				(c.Request.Method == http.MethodPut && c.Request.URL.Path == "/api/v1/admin/auth/password"))
		case "tenant_admin":
			allowed = tenantAdminRouteAllowed(c.Request.Method, c.Request.URL.Path)
		}
		if !allowed {
			c.JSON(http.StatusForbidden, NewErrorResponse("当前管理角色不能访问该平台级接口"))
			c.Abort()
			return
		}
		c.Set("admin_role", admin.Role)
		c.Next()
	}
}

func tenantAdminRouteAllowed(method, path string) bool {
	const prefix = "/api/v1/admin/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, prefix), "/"), "/")
	if len(parts) == 0 {
		return false
	}
	switch parts[0] {
	case "auth":
		return (len(parts) == 2 && parts[1] == "me" && method == http.MethodGet) ||
			(len(parts) == 2 && parts[1] == "password" && method == http.MethodPut)
	case "tenants":
		if len(parts) == 1 {
			return method == http.MethodGet
		}
		if len(parts) == 3 && parts[2] == "members" {
			return method == http.MethodGet || method == http.MethodPost
		}
		return len(parts) == 5 && parts[2] == "members" && parts[4] == "disable" && method == http.MethodPost
	case "groups":
		if len(parts) == 1 {
			return method == http.MethodGet || method == http.MethodPost
		}
		if len(parts) == 2 {
			return method == http.MethodGet || method == http.MethodPut || method == http.MethodDelete
		}
		if len(parts) == 3 && parts[2] == "members" {
			return method == http.MethodGet || method == http.MethodPost
		}
		return len(parts) == 4 && parts[2] == "members" && method == http.MethodDelete
	case "workspace-bindings":
		return len(parts) == 1 && (method == http.MethodGet || method == http.MethodPost)
	case "grants":
		if len(parts) == 1 {
			return method == http.MethodGet
		}
		return len(parts) == 3 && parts[2] == "revoke" && method == http.MethodPost
	case "resources":
		if len(parts) == 1 {
			return method == http.MethodGet || method == http.MethodPost
		}
		if parts[1] == "sync" || parts[1] == "k8s-services" {
			return false
		}
		if len(parts) == 2 {
			return method == http.MethodGet
		}
		if len(parts) == 3 && parts[2] == "grants" {
			return method == http.MethodGet || method == http.MethodPost
		}
		if len(parts) == 3 && parts[2] == "events" {
			return method == http.MethodGet
		}
		return len(parts) == 3 && parts[2] == "targets" && method == http.MethodPost
	case "sessions":
		if len(parts) == 1 || len(parts) == 2 {
			return method == http.MethodGet
		}
		return len(parts) == 3 && (parts[2] == "revoke" || parts[2] == "force-disconnect") && method == http.MethodPost
	default:
		return false
	}
}

// ClientAuthMiddleware Client JWT认证中间件
func ClientAuthMiddleware(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "未提供认证信息",
			})
			c.Abort()
			return
		}

		// 解析Bearer Token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "认证格式错误",
			})
			c.Abort()
			return
		}

		tokenString := parts[1]

		// 验证Token
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "Token无效或已过期",
			})
			c.Abort()
			return
		}

		// 提取claims（Client的JWT包含client_id）
		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			if clientID, exists := claims["client_id"]; exists {
				c.Set("client_id", clientID)
			}
		}

		c.Next()
	}
}
