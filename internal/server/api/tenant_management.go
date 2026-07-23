package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// TenantSettingsAPI manages the small set of tenant-owned settings. Stable
// keys and lifecycle status remain platform-governed and are intentionally
// read-only here.
type TenantSettingsAPI struct{}

func NewTenantSettingsAPI() *TenantSettingsAPI { return &TenantSettingsAPI{} }

type tenantSettingsResponse struct {
	ID        string             `json:"id"`
	Key       string             `json:"key"`
	Name      string             `json:"name"`
	Status    model.TenantStatus `json:"status"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

type updateTenantSettingsRequest struct {
	Name string `json:"name" binding:"required"`
}

func (a *TenantSettingsAPI) Get(c *gin.Context) {
	tenantID := strings.TrimSpace(c.Param("id"))
	if !requireTenantPermission(c, tenantID, PermissionTenantSettingsRead) {
		return
	}
	var tenant model.Tenant
	if err := db.DB.WithContext(c.Request.Context()).First(&tenant, "id = ?", tenantID).Error; err != nil {
		codedError(c, http.StatusNotFound, ErrorCodeTenantObjectNotFound, "当前租户范围内对象不存在")
		return
	}
	c.JSON(http.StatusOK, NewSuccessResponse(tenantSettingsResponse{
		ID: tenant.ID, Key: tenant.Key, Name: tenant.Name, Status: tenant.Status,
		CreatedAt: tenant.CreatedAt, UpdatedAt: tenant.UpdatedAt,
	}))
}

func (a *TenantSettingsAPI) Update(c *gin.Context) {
	tenantID := strings.TrimSpace(c.Param("id"))
	if !requireTenantPermission(c, tenantID, PermissionTenantSettingsWrite) {
		return
	}
	var req updateTenantSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, NewErrorResponse("租户名称不能为空"))
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if len([]rune(req.Name)) > 200 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("租户名称不能超过 200 个字符"))
		return
	}
	var tenant model.Tenant
	if err := db.DB.WithContext(c.Request.Context()).First(&tenant, "id = ?", tenantID).Error; err != nil {
		codedError(c, http.StatusNotFound, ErrorCodeTenantObjectNotFound, "当前租户范围内对象不存在")
		return
	}
	oldName := tenant.Name
	if oldName != req.Name {
		if err := db.DB.WithContext(c.Request.Context()).Model(&tenant).Update("name", req.Name).Error; err != nil {
			c.JSON(http.StatusInternalServerError, NewErrorResponse("更新租户设置失败"))
			return
		}
		tenant.Name = req.Name
		recordAuditLog(c.Request.Context(), c, "update_tenant_settings", "tenant", tenant.ID, tenant.Name, map[string]interface{}{
			"before": map[string]string{"name": oldName}, "after": map[string]string{"name": req.Name},
		})
	}
	c.JSON(http.StatusOK, NewSuccessResponse(tenantSettingsResponse{
		ID: tenant.ID, Key: tenant.Key, Name: tenant.Name, Status: tenant.Status,
		CreatedAt: tenant.CreatedAt, UpdatedAt: tenant.UpdatedAt,
	}))
}

// TenantAdminMembershipAPI is a platform-governance API. It never creates a
// business TenantMembership and therefore never grants Desktop access.
type TenantAdminMembershipAPI struct{}

func NewTenantAdminMembershipAPI() *TenantAdminMembershipAPI {
	return &TenantAdminMembershipAPI{}
}

type tenantAdminMembershipItem struct {
	ID                 int64      `json:"id"`
	AdminID            int64      `json:"admin_id"`
	AdminUsername      string     `json:"admin_username"`
	AdminEnabled       bool       `json:"admin_enabled"`
	TenantID           string     `json:"tenant_id"`
	TenantKey          string     `json:"tenant_key"`
	TenantName         string     `json:"tenant_name"`
	TenantStatus       string     `json:"tenant_status"`
	Role               string     `json:"role"`
	Enabled            bool       `json:"enabled"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	PermissionRevision int64      `json:"permission_revision"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type tenantAdminOption struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Enabled  bool   `json:"enabled"`
}

type tenantAdminMembershipRequest struct {
	AdminID   int64      `json:"admin_id"`
	TenantID  string     `json:"tenant_id"`
	Role      string     `json:"role" binding:"required"`
	Enabled   *bool      `json:"enabled"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func (a *TenantAdminMembershipAPI) List(c *gin.Context) {
	if !requirePlatformAccess(c, false) {
		return
	}
	page, size := pageParams(c)
	query := db.DB.WithContext(c.Request.Context()).Table("admin_tenant_membership AS membership").
		Joins("JOIN admin ON admin.id = membership.admin_id").
		Joins("JOIN tenant ON tenant.id = membership.tenant_id")
	if tenantID := strings.TrimSpace(c.Query("tenant_id")); tenantID != "" {
		query = query.Where("membership.tenant_id = ?", tenantID)
	}
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		like := "%" + search + "%"
		query = query.Where("admin.username LIKE ? OR tenant.name LIKE ? OR tenant.key LIKE ?", like, like, like)
	}
	if role := strings.TrimSpace(c.Query("role")); role != "" {
		query = query.Where("membership.role = ?", role)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询租户管理员授权失败"))
		return
	}
	var items []tenantAdminMembershipItem
	if err := query.Select(`membership.id, membership.admin_id, admin.username AS admin_username,
		admin.enabled AS admin_enabled, membership.tenant_id, tenant.key AS tenant_key,
		tenant.name AS tenant_name, tenant.status AS tenant_status, membership.role,
		membership.enabled, membership.expires_at, membership.permission_revision,
		membership.created_at, membership.updated_at`).
		Order("membership.updated_at DESC").Offset((page - 1) * size).Limit(size).Scan(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询租户管理员授权失败"))
		return
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}

func (a *TenantAdminMembershipAPI) ListAdminOptions(c *gin.Context) {
	if !requirePlatformAccess(c, false) {
		return
	}
	var admins []model.Admin
	if err := db.DB.WithContext(c.Request.Context()).Order("username ASC").Find(&admins).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询平台管理账号失败"))
		return
	}
	items := make([]tenantAdminOption, 0, len(admins))
	for _, admin := range admins {
		items = append(items, tenantAdminOption{ID: admin.ID, Username: admin.Username, Role: admin.Role, Enabled: admin.Enabled})
	}
	c.JSON(http.StatusOK, NewSuccessResponse(items))
}

func (a *TenantAdminMembershipAPI) Create(c *gin.Context) {
	if !requirePlatformAccess(c, true) {
		return
	}
	var req tenantAdminMembershipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("管理员、租户和管理角色不能为空"))
		return
	}
	if req.AdminID == 0 || strings.TrimSpace(req.TenantID) == "" {
		c.JSON(http.StatusBadRequest, NewErrorResponse("管理员和租户不能为空"))
		return
	}
	role := model.NormalizeTenantManagementRole(req.Role)
	if role == "" {
		c.JSON(http.StatusBadRequest, NewErrorResponse("租户管理角色无效"))
		return
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		c.JSON(http.StatusBadRequest, NewErrorResponse("授权有效期必须晚于当前时间"))
		return
	}
	if !adminAndTenantExist(c, req.AdminID, req.TenantID) {
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	membership := model.AdminTenantMembership{AdminID: req.AdminID, TenantID: req.TenantID, Role: string(role), Enabled: enabled, ExpiresAt: req.ExpiresAt, PermissionRevision: 1}
	if err := db.DB.WithContext(c.Request.Context()).Create(&membership).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			c.JSON(http.StatusConflict, NewErrorResponse("该管理员已存在此租户的管理授权"))
			return
		}
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建租户管理员授权失败"))
		return
	}
	// GORM applies the model's default:true tag to a false bool during Create.
	// Persist an explicitly disabled initial grant without changing the model's
	// compatibility default for existing callers.
	if !enabled {
		if err := db.DB.WithContext(c.Request.Context()).Model(&membership).Update("enabled", false).Error; err != nil {
			c.JSON(http.StatusInternalServerError, NewErrorResponse("创建租户管理员授权失败"))
			return
		}
	}
	c.Set("audit_tenant_id", req.TenantID)
	recordAuditLog(c.Request.Context(), c, "create_tenant_admin_membership", "admin_tenant_membership", strconv.FormatInt(membership.ID, 10), "租户管理员授权", membership)
	c.JSON(http.StatusCreated, NewSuccessResponse(membership))
}

func (a *TenantAdminMembershipAPI) Update(c *gin.Context) {
	if !requirePlatformAccess(c, true) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("授权 ID 无效"))
		return
	}
	var membership model.AdminTenantMembership
	if err := db.DB.WithContext(c.Request.Context()).First(&membership, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, NewErrorResponse("租户管理员授权不存在"))
			return
		}
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询租户管理员授权失败"))
		return
	}
	var req tenantAdminMembershipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}
	role := model.NormalizeTenantManagementRole(req.Role)
	if role == "" {
		c.JSON(http.StatusBadRequest, NewErrorResponse("租户管理角色无效"))
		return
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		c.JSON(http.StatusBadRequest, NewErrorResponse("授权有效期必须晚于当前时间"))
		return
	}
	before := membership
	updates := map[string]interface{}{"role": string(role), "expires_at": req.ExpiresAt, "permission_revision": gorm.Expr("permission_revision + 1")}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if err := db.DB.WithContext(c.Request.Context()).Model(&membership).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("更新租户管理员授权失败"))
		return
	}
	if err := db.DB.WithContext(c.Request.Context()).First(&membership, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("读取更新后的租户管理员授权失败"))
		return
	}
	c.Set("audit_tenant_id", membership.TenantID)
	recordAuditLog(c.Request.Context(), c, "update_tenant_admin_membership", "admin_tenant_membership", strconv.FormatInt(membership.ID, 10), "租户管理员授权", map[string]interface{}{"before": before, "after": membership})
	c.JSON(http.StatusOK, NewSuccessResponse(membership))
}

func adminAndTenantExist(c *gin.Context, adminID int64, tenantID string) bool {
	var admin model.Admin
	if err := db.DB.WithContext(c.Request.Context()).First(&admin, adminID).Error; err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("平台管理账号不存在"))
		return false
	}
	var tenant model.Tenant
	if err := db.DB.WithContext(c.Request.Context()).First(&tenant, "id = ?", tenantID).Error; err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("租户不存在"))
		return false
	}
	return true
}
