package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
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
