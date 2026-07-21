package api

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

const (
	managementRoleAdmin       = "admin"
	managementRoleViewer      = "viewer"
	managementRoleTenantAdmin = "tenant_admin"
)

var (
	managementUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{2,49}$`)
	errTenantUnavailable      = errors.New("tenant unavailable")
	errLastPlatformAdmin      = errors.New("last enabled platform admin")
)

type ManagementAccountAPI struct{}

func NewManagementAccountAPI() *ManagementAccountAPI { return &ManagementAccountAPI{} }

type managementTenantBindingRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
	Role     string `json:"role"`
}

type createManagementAccountRequest struct {
	Username          string                           `json:"username" binding:"required"`
	Password          string                           `json:"password" binding:"required"`
	Role              string                           `json:"role" binding:"required"`
	TenantMemberships []managementTenantBindingRequest `json:"tenant_memberships"`
}

type resetManagementPasswordRequest struct {
	Password string `json:"password" binding:"required"`
}

type managementTenantMembershipView struct {
	ID         int64     `json:"id"`
	TenantID   string    `json:"tenant_id"`
	TenantKey  string    `json:"tenant_key"`
	TenantName string    `json:"tenant_name"`
	Role       string    `json:"role"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type managementAccountView struct {
	ID                int64                            `json:"id"`
	Username          string                           `json:"username"`
	Role              string                           `json:"role"`
	Enabled           bool                             `json:"enabled"`
	TenantMemberships []managementTenantMembershipView `json:"tenant_memberships"`
	CreatedAt         time.Time                        `json:"created_at"`
	UpdatedAt         time.Time                        `json:"updated_at"`
}

func validateManagementPassword(password string) bool {
	return len(password) >= 12 && len(password) <= 72
}

func validManagementRole(role string) bool {
	return role == managementRoleAdmin || role == managementRoleViewer || role == managementRoleTenantAdmin
}

func validTenantManagementRole(role string) bool {
	return role == managementRoleTenantAdmin || role == managementRoleViewer
}

func normalizeManagementBindings(bindings []managementTenantBindingRequest) ([]managementTenantBindingRequest, bool) {
	result := make([]managementTenantBindingRequest, 0, len(bindings))
	seen := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		binding.TenantID = strings.TrimSpace(binding.TenantID)
		binding.Role = strings.TrimSpace(binding.Role)
		if binding.Role == "" {
			binding.Role = managementRoleTenantAdmin
		}
		if binding.TenantID == "" || !validTenantManagementRole(binding.Role) {
			return nil, false
		}
		if _, ok := seen[binding.TenantID]; ok {
			return nil, false
		}
		seen[binding.TenantID] = struct{}{}
		result = append(result, binding)
	}
	return result, true
}

func managementAccountID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("管理账号 ID 无效"))
		return 0, false
	}
	return id, true
}

func loadManagementMemberships(database *gorm.DB, adminIDs []int64) (map[int64][]managementTenantMembershipView, error) {
	result := make(map[int64][]managementTenantMembershipView, len(adminIDs))
	if len(adminIDs) == 0 {
		return result, nil
	}
	type membershipRow struct {
		ID         int64
		AdminID    int64
		TenantID   string
		TenantKey  string
		TenantName string
		Role       string
		Enabled    bool
		CreatedAt  time.Time
		UpdatedAt  time.Time
	}
	var rows []membershipRow
	err := database.Table("admin_tenant_membership AS membership").
		Select("membership.id, membership.admin_id, membership.tenant_id, tenant.key AS tenant_key, tenant.name AS tenant_name, membership.role, membership.enabled, membership.created_at, membership.updated_at").
		Joins("LEFT JOIN tenant ON tenant.id = membership.tenant_id").
		Where("membership.admin_id IN ?", adminIDs).
		Order("membership.created_at ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.AdminID] = append(result[row.AdminID], managementTenantMembershipView{
			ID: row.ID, TenantID: row.TenantID, TenantKey: row.TenantKey, TenantName: row.TenantName,
			Role: row.Role, Enabled: row.Enabled, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}
	return result, nil
}

func managementAccountViews(database *gorm.DB, accounts []model.Admin) ([]managementAccountView, error) {
	ids := make([]int64, 0, len(accounts))
	for _, account := range accounts {
		ids = append(ids, account.ID)
	}
	memberships, err := loadManagementMemberships(database, ids)
	if err != nil {
		return nil, err
	}
	views := make([]managementAccountView, 0, len(accounts))
	for _, account := range accounts {
		accountMemberships := memberships[account.ID]
		if accountMemberships == nil {
			accountMemberships = []managementTenantMembershipView{}
		}
		views = append(views, managementAccountView{
			ID: account.ID, Username: account.Username, Role: account.Role, Enabled: account.Enabled,
			TenantMemberships: accountMemberships, CreatedAt: account.CreatedAt, UpdatedAt: account.UpdatedAt,
		})
	}
	return views, nil
}

func managementAccountViewByID(database *gorm.DB, id int64) (managementAccountView, error) {
	var account model.Admin
	if err := database.First(&account, id).Error; err != nil {
		return managementAccountView{}, err
	}
	views, err := managementAccountViews(database, []model.Admin{account})
	if err != nil {
		return managementAccountView{}, err
	}
	return views[0], nil
}

func (a *ManagementAccountAPI) List(c *gin.Context) {
	if !requirePlatformAdmin(c) {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	query := db.DB.WithContext(c.Request.Context()).Model(&model.Admin{})
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		query = query.Where("username LIKE ?", "%"+search+"%")
	}
	if role := strings.TrimSpace(c.Query("role")); role != "" {
		if !validManagementRole(role) {
			c.JSON(http.StatusBadRequest, NewErrorResponse("管理角色无效"))
			return
		}
		query = query.Where("role = ?", role)
	}
	if enabledValue := strings.TrimSpace(c.Query("enabled")); enabledValue != "" {
		enabled, err := strconv.ParseBool(enabledValue)
		if err != nil {
			c.JSON(http.StatusBadRequest, NewErrorResponse("enabled 必须是 true 或 false"))
			return
		}
		query = query.Where("enabled = ?", enabled)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询管理账号失败"))
		return
	}
	var accounts []model.Admin
	if err := query.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&accounts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询管理账号失败"))
		return
	}
	views, err := managementAccountViews(db.DB.WithContext(c.Request.Context()), accounts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询管理范围失败"))
		return
	}
	c.JSON(http.StatusOK, NewPagedResponse(views, total, page, size))
}

func (a *ManagementAccountAPI) Create(c *gin.Context) {
	if !requirePlatformAdmin(c) {
		return
	}
	var req createManagementAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("用户名、密码和管理角色不能为空"))
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Role = strings.TrimSpace(req.Role)
	if !managementUsernamePattern.MatchString(req.Username) {
		c.JSON(http.StatusBadRequest, NewErrorResponse("用户名需为 3-50 位字母、数字或 ._@-"))
		return
	}
	if !validateManagementPassword(req.Password) {
		c.JSON(http.StatusBadRequest, NewErrorResponse("密码长度需为 12-72 字节"))
		return
	}
	if !validManagementRole(req.Role) {
		c.JSON(http.StatusBadRequest, NewErrorResponse("管理角色无效"))
		return
	}
	bindings, ok := normalizeManagementBindings(req.TenantMemberships)
	if !ok {
		c.JSON(http.StatusBadRequest, NewErrorResponse("Tenant 管理范围无效或重复"))
		return
	}
	if req.Role == managementRoleTenantAdmin && len(bindings) == 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("Tenant Admin 创建时必须绑定至少一个 Tenant"))
		return
	}
	if req.Role != managementRoleTenantAdmin && len(bindings) > 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("Platform Admin 和 Viewer 不使用 Tenant 管理范围"))
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("生成密码哈希失败"))
		return
	}
	account := model.Admin{Username: req.Username, PasswordHash: string(hash), Role: req.Role, Enabled: true}
	err = db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		for _, binding := range bindings {
			var tenant model.Tenant
			if err := tx.Where("id = ? AND status = ?", binding.TenantID, model.TenantStatusActive).First(&tenant).Error; err != nil {
				return errTenantUnavailable
			}
		}
		if err := tx.Create(&account).Error; err != nil {
			return err
		}
		for _, binding := range bindings {
			membership := model.AdminTenantMembership{
				AdminID: account.ID, TenantID: binding.TenantID, Role: binding.Role, Enabled: true,
			}
			if err := tx.Create(&membership).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, errTenantUnavailable) {
		c.JSON(http.StatusBadRequest, NewErrorResponse("Tenant 不存在或未启用"))
		return
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			c.JSON(http.StatusConflict, NewErrorResponse("管理账号用户名已存在"))
			return
		}
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建管理账号失败"))
		return
	}
	detail := map[string]interface{}{"role": account.Role, "tenant_memberships": bindings}
	recordAuditLog(c.Request.Context(), c, "create_management_account", "management_account", strconv.FormatInt(account.ID, 10), account.Username, detail)
	view, err := managementAccountViewByID(db.DB.WithContext(c.Request.Context()), account.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("读取新建管理账号失败"))
		return
	}
	c.JSON(http.StatusCreated, NewSuccessResponse(view))
}

func (a *ManagementAccountAPI) ResetPassword(c *gin.Context) {
	if !requirePlatformAdmin(c) {
		return
	}
	id, ok := managementAccountID(c)
	if !ok {
		return
	}
	var req resetManagementPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil || !validateManagementPassword(req.Password) {
		c.JSON(http.StatusBadRequest, NewErrorResponse("密码长度需为 12-72 字节"))
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("生成密码哈希失败"))
		return
	}
	var account model.Admin
	if err := db.DB.WithContext(c.Request.Context()).First(&account, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("管理账号不存在"))
		return
	}
	if err := db.DB.WithContext(c.Request.Context()).Model(&account).Update("password_hash", string(hash)).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("重置密码失败"))
		return
	}
	recordAuditLog(c.Request.Context(), c, "reset_management_account_password", "management_account", strconv.FormatInt(account.ID, 10), account.Username, map[string]bool{"password_reset": true})
	c.JSON(http.StatusOK, NewSuccessMessageResponse("密码已重置", nil))
}

func (a *ManagementAccountAPI) Enable(c *gin.Context) {
	if !requirePlatformAdmin(c) {
		return
	}
	id, ok := managementAccountID(c)
	if !ok {
		return
	}
	var account model.Admin
	if err := db.DB.WithContext(c.Request.Context()).First(&account, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("管理账号不存在"))
		return
	}
	if !account.Enabled {
		if err := db.DB.WithContext(c.Request.Context()).Model(&account).Update("enabled", true).Error; err != nil {
			c.JSON(http.StatusInternalServerError, NewErrorResponse("恢复管理账号失败"))
			return
		}
		recordAuditLog(c.Request.Context(), c, "enable_management_account", "management_account", strconv.FormatInt(account.ID, 10), account.Username, nil)
	}
	view, err := managementAccountViewByID(db.DB.WithContext(c.Request.Context()), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("读取管理账号失败"))
		return
	}
	c.JSON(http.StatusOK, NewSuccessResponse(view))
}

func (a *ManagementAccountAPI) Disable(c *gin.Context) {
	if !requirePlatformAdmin(c) {
		return
	}
	id, ok := managementAccountID(c)
	if !ok {
		return
	}
	if currentID := getAdminIDFromContext(c); currentID != 0 && currentID == id {
		c.JSON(http.StatusConflict, NewErrorResponse("不能停用当前登录的管理账号"))
		return
	}
	var account model.Admin
	wasEnabled := false
	err := db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&account, id).Error; err != nil {
			return err
		}
		if !account.Enabled {
			return nil
		}
		wasEnabled = true
		if account.Role == managementRoleAdmin {
			var enabledPlatformAdmins int64
			if err := tx.Model(&model.Admin{}).Where("role = ? AND enabled = ?", managementRoleAdmin, true).Count(&enabledPlatformAdmins).Error; err != nil {
				return err
			}
			if enabledPlatformAdmins <= 1 {
				return errLastPlatformAdmin
			}
		}
		return tx.Model(&account).Update("enabled", false).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, NewErrorResponse("管理账号不存在"))
		return
	}
	if errors.Is(err, errLastPlatformAdmin) {
		c.JSON(http.StatusConflict, NewErrorResponse("不能停用最后一个 Platform Admin"))
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("停用管理账号失败"))
		return
	}
	if wasEnabled {
		recordAuditLog(c.Request.Context(), c, "disable_management_account", "management_account", strconv.FormatInt(account.ID, 10), account.Username, nil)
	}
	view, err := managementAccountViewByID(db.DB.WithContext(c.Request.Context()), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("读取管理账号失败"))
		return
	}
	c.JSON(http.StatusOK, NewSuccessResponse(view))
}

func (a *ManagementAccountAPI) ListTenantMemberships(c *gin.Context) {
	if !requirePlatformAdmin(c) {
		return
	}
	id, ok := managementAccountID(c)
	if !ok {
		return
	}
	var account model.Admin
	if err := db.DB.WithContext(c.Request.Context()).First(&account, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("管理账号不存在"))
		return
	}
	memberships, err := loadManagementMemberships(db.DB.WithContext(c.Request.Context()), []int64{id})
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询管理范围失败"))
		return
	}
	items := memberships[id]
	if items == nil {
		items = []managementTenantMembershipView{}
	}
	c.JSON(http.StatusOK, NewSuccessResponse(items))
}

func (a *ManagementAccountAPI) BindTenant(c *gin.Context) {
	if !requirePlatformAdmin(c) {
		return
	}
	id, ok := managementAccountID(c)
	if !ok {
		return
	}
	var req managementTenantBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("Tenant ID 不能为空"))
		return
	}
	bindings, valid := normalizeManagementBindings([]managementTenantBindingRequest{req})
	if !valid {
		c.JSON(http.StatusBadRequest, NewErrorResponse("Tenant 管理范围无效"))
		return
	}
	req = bindings[0]
	var account model.Admin
	if err := db.DB.WithContext(c.Request.Context()).First(&account, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("管理账号不存在"))
		return
	}
	if account.Role != managementRoleTenantAdmin {
		c.JSON(http.StatusBadRequest, NewErrorResponse("只有 Tenant Admin 账号可绑定 Tenant 管理范围"))
		return
	}
	var tenant model.Tenant
	if err := db.DB.WithContext(c.Request.Context()).Where("id = ? AND status = ?", req.TenantID, model.TenantStatusActive).First(&tenant).Error; err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("Tenant 不存在或未启用"))
		return
	}
	membership := model.AdminTenantMembership{AdminID: id, TenantID: req.TenantID, Role: req.Role, Enabled: true}
	if err := db.DB.WithContext(c.Request.Context()).Where("admin_id = ? AND tenant_id = ?", id, req.TenantID).
		Assign(map[string]interface{}{"role": req.Role, "enabled": true}).FirstOrCreate(&membership).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("绑定 Tenant 管理范围失败"))
		return
	}
	recordAuditLog(c.Request.Context(), c, "bind_management_account_tenant", "management_account", strconv.FormatInt(account.ID, 10), account.Username, map[string]interface{}{"tenant_id": req.TenantID, "role": req.Role})
	view, err := managementAccountViewByID(db.DB.WithContext(c.Request.Context()), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("读取管理账号失败"))
		return
	}
	c.JSON(http.StatusCreated, NewSuccessResponse(view))
}

func (a *ManagementAccountAPI) DisableTenantMembership(c *gin.Context) {
	if !requirePlatformAdmin(c) {
		return
	}
	id, ok := managementAccountID(c)
	if !ok {
		return
	}
	tenantID := strings.TrimSpace(c.Param("tenant_id"))
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, NewErrorResponse("Tenant ID 无效"))
		return
	}
	var account model.Admin
	if err := db.DB.WithContext(c.Request.Context()).First(&account, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("管理账号不存在"))
		return
	}
	var membership model.AdminTenantMembership
	if err := db.DB.WithContext(c.Request.Context()).Where("admin_id = ? AND tenant_id = ?", id, tenantID).First(&membership).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Tenant 管理范围不存在"))
		return
	}
	if membership.Enabled {
		if err := db.DB.WithContext(c.Request.Context()).Model(&membership).Update("enabled", false).Error; err != nil {
			c.JSON(http.StatusInternalServerError, NewErrorResponse("停用 Tenant 管理范围失败"))
			return
		}
		recordAuditLog(c.Request.Context(), c, "disable_management_account_tenant", "management_account", strconv.FormatInt(account.ID, 10), account.Username, map[string]string{"tenant_id": tenantID})
	}
	view, err := managementAccountViewByID(db.DB.WithContext(c.Request.Context()), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("读取管理账号失败"))
		return
	}
	c.JSON(http.StatusOK, NewSuccessResponse(view))
}
