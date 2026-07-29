package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type PlatformAdminAPI struct{}

func NewPlatformAdminAPI() *PlatformAdminAPI { return &PlatformAdminAPI{} }

type platformAdminItem struct {
	ID           int64              `json:"id"`
	Username     string             `json:"username"`
	PlatformRole model.PlatformRole `json:"platform_role"`
	Enabled      bool               `json:"enabled"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

type createPlatformAdminRequest struct {
	Username     string `json:"username" binding:"required"`
	Password     string `json:"password" binding:"required"`
	PlatformRole string `json:"platform_role" binding:"required"`
	Enabled      *bool  `json:"enabled"`
}

type updatePlatformAdminRequest struct {
	PlatformRole string `json:"platform_role" binding:"required"`
	Enabled      *bool  `json:"enabled" binding:"required"`
}

func (a *PlatformAdminAPI) List(c *gin.Context) {
	if !requirePlatformAccess(c, false) {
		return
	}
	page, size := pageParams(c)
	query := db.DB.WithContext(c.Request.Context()).Model(&model.Admin{})
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		query = query.Where("username LIKE ?", "%"+search+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询平台管理账号失败"))
		return
	}
	var admins []model.Admin
	if err := query.Order("created_at ASC").Offset((page - 1) * size).Limit(size).Find(&admins).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询平台管理账号失败"))
		return
	}
	items := make([]platformAdminItem, 0, len(admins))
	for _, admin := range admins {
		items = append(items, platformAdminResponse(admin))
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}

func (a *PlatformAdminAPI) Create(c *gin.Context) {
	if !requirePlatformAccess(c, true) {
		return
	}
	var req createPlatformAdminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("账号、初始密码和平台角色不能为空"))
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || len(req.Username) > 50 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("账号不能为空且不能超过 50 个字符"))
		return
	}
	if len(req.Password) < 8 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("初始密码至少需要 8 个字符"))
		return
	}
	role, ok := canonicalPlatformRole(req.PlatformRole)
	if !ok {
		c.JSON(http.StatusBadRequest, NewErrorResponse("平台角色无效"))
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("生成初始密码失败"))
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	admin := model.Admin{Username: req.Username, PasswordHash: string(hash), Role: string(role), Enabled: enabled}
	err = db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&admin).Error; err != nil {
			return err
		}
		if !enabled {
			if err := tx.Model(&admin).Update("enabled", false).Error; err != nil {
				return err
			}
			admin.Enabled = false
		}
		_, err := db.SyncLegacyAdminIdentity(tx, admin.ID, "platform administrator created")
		return err
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			c.JSON(http.StatusConflict, NewErrorResponse("平台管理账号已存在"))
			return
		}
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建平台管理账号失败"))
		return
	}
	recordAuditLog(c.Request.Context(), c, "create_platform_admin", "admin", strconv.FormatInt(admin.ID, 10), admin.Username, map[string]interface{}{"platform_role": role, "enabled": enabled})
	c.JSON(http.StatusCreated, NewSuccessResponse(platformAdminResponse(admin)))
}

func (a *PlatformAdminAPI) Update(c *gin.Context) {
	if !requirePlatformAccess(c, true) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("平台管理账号 ID 无效"))
		return
	}
	var req updatePlatformAdminRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("平台角色和状态不能为空"))
		return
	}
	role, ok := canonicalPlatformRole(req.PlatformRole)
	if !ok {
		c.JSON(http.StatusBadRequest, NewErrorResponse("平台角色无效"))
		return
	}
	var admin model.Admin
	if err := db.DB.WithContext(c.Request.Context()).First(&admin, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, NewErrorResponse("平台管理账号不存在"))
			return
		}
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询平台管理账号失败"))
		return
	}
	if admin.ID == getAdminIDFromContext(c) && (!*req.Enabled || role != model.PlatformRoleAdmin) {
		c.JSON(http.StatusConflict, NewErrorResponse("不能停用当前账号或移除当前账号的平台管理员角色"))
		return
	}
	before := platformAdminResponse(admin)
	err = db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&admin).Updates(map[string]interface{}{"role": string(role), "enabled": *req.Enabled}).Error; err != nil {
			return err
		}
		_, err := db.SyncLegacyAdminIdentity(tx, admin.ID, "platform administrator updated")
		return err
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新平台管理账号失败"))
		return
	}
	admin.Role = string(role)
	admin.Enabled = *req.Enabled
	recordAuditLog(c.Request.Context(), c, "update_platform_admin", "admin", strconv.FormatInt(admin.ID, 10), admin.Username, map[string]interface{}{"before": before, "after": platformAdminResponse(admin)})
	c.JSON(http.StatusOK, NewSuccessResponse(platformAdminResponse(admin)))
}

func canonicalPlatformRole(role string) (model.PlatformRole, bool) {
	switch model.PlatformRole(strings.TrimSpace(role)) {
	case model.PlatformRoleAdmin:
		return model.PlatformRoleAdmin, true
	case model.PlatformRoleViewer:
		return model.PlatformRoleViewer, true
	case model.PlatformRoleNone:
		return model.PlatformRoleNone, true
	default:
		return "", false
	}
}

func platformAdminResponse(admin model.Admin) platformAdminItem {
	return platformAdminItem{ID: admin.ID, Username: admin.Username, PlatformRole: model.NormalizePlatformRole(admin.Role), Enabled: admin.Enabled, CreatedAt: admin.CreatedAt, UpdatedAt: admin.UpdatedAt}
}
