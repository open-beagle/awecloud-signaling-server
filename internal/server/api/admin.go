package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// AdminAPI 管理员 API
type AdminAPI struct {
	config *config.ServerConfig
}

// NewAdminAPI 创建 AdminAPI
func NewAdminAPI(cfg *config.ServerConfig) *AdminAPI {
	return &AdminAPI{config: cfg}
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse 登录响应
type LoginResponse struct {
	Token     string     `json:"token"`
	ExpiresAt time.Time  `json:"expires_at"`
	Admin     *AdminInfo `json:"admin"`
}

// AdminInfo 管理员信息
type AdminInfo struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// Login 管理员登录
func (a *AdminAPI) Login(c *gin.Context) {
	ctx := c.Request.Context()
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	// 查询管理员
	var admin model.Admin
	if err := db.DB.WithContext(ctx).Where("username = ? AND enabled = ?", req.Username, true).First(&admin).Error; err != nil {
		c.JSON(http.StatusUnauthorized, NewErrorResponse("用户名或密码错误"))
		return
	}

	// 验证密码
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, NewErrorResponse("用户名或密码错误"))
		return
	}

	// 生成 JWT Token
	expiresAt := time.Now().Add(time.Hour * time.Duration(a.config.Security.JWTExpireHours))
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"admin_id": admin.ID,
		"username": admin.Username,
		"exp":      expiresAt.Unix(),
	})

	tokenString, err := token.SignedString([]byte(a.config.Security.JWTSecret))
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("生成 Token 失败"))
		return
	}

	logger.Infof("管理员登录: username=%s", admin.Username)

	c.JSON(http.StatusOK, NewSuccessResponse(LoginResponse{
		Token:     tokenString,
		ExpiresAt: expiresAt,
		Admin: &AdminInfo{
			ID:        admin.ID,
			Username:  admin.Username,
			Role:      admin.Role,
			Enabled:   admin.Enabled,
			CreatedAt: admin.CreatedAt,
		},
	}))
}

// Logout 管理员登出
func (a *AdminAPI) Logout(c *gin.Context) {
	// JWT 无状态，客户端删除 Token 即可
	c.JSON(http.StatusOK, NewSuccessMessageResponse("登出成功", nil))
}

// GetMe 获取当前管理员信息
func (a *AdminAPI) GetMe(c *gin.Context) {
	ctx := c.Request.Context()
	adminID := getAdminIDFromContext(c)
	if adminID == 0 {
		c.JSON(http.StatusUnauthorized, NewErrorResponse("未认证"))
		return
	}

	var admin model.Admin
	if err := db.DB.WithContext(ctx).Where("id = ? AND enabled = ?", adminID, true).First(&admin).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("管理员不存在"))
		return
	}

	c.JSON(http.StatusOK, NewSuccessResponse(AdminInfo{
		ID:        admin.ID,
		Username:  admin.Username,
		Role:      admin.Role,
		Enabled:   admin.Enabled,
		CreatedAt: admin.CreatedAt,
	}))
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// ChangePassword 修改密码
func (a *AdminAPI) ChangePassword(c *gin.Context) {
	ctx := c.Request.Context()
	adminID := getAdminIDFromContext(c)
	if adminID == 0 {
		c.JSON(http.StatusUnauthorized, NewErrorResponse("未认证"))
		return
	}

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	var admin model.Admin
	if err := db.DB.WithContext(ctx).First(&admin, adminID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("管理员不存在"))
		return
	}

	// 验证旧密码
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("旧密码错误"))
		return
	}

	// 生成新密码哈希
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("加密密码失败"))
		return
	}

	admin.PasswordHash = string(newHash)
	if err := db.DB.WithContext(ctx).Save(&admin).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新失败"))
		return
	}

	logger.Infof("管理员修改密码: id=%d", adminID)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("密码修改成功", nil))
}

// getAdminIDFromContext 从上下文获取管理员 ID
func getAdminIDFromContext(c *gin.Context) int64 {
	if adminID, exists := c.Get("admin_id"); exists {
		switch v := adminID.(type) {
		case int64:
			return v
		case float64:
			return int64(v)
		case int:
			return int64(v)
		}
	}
	return 0
}
