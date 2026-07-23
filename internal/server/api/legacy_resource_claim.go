package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type LegacyResourceClaimAPI struct{}

func NewLegacyResourceClaimAPI() *LegacyResourceClaimAPI { return &LegacyResourceClaimAPI{} }

type legacyClaimRequest struct {
	SourceType string `json:"source_type" binding:"required"`
	SourceID   string `json:"source_id" binding:"required"`
	TenantID   string `json:"tenant_id" binding:"required"`
	Reason     string `json:"reason"`
}

type legacyClaimListItem struct {
	model.LegacyResourceClaim
	TenantName  string `json:"tenant_name"`
	SourceName  string `json:"source_name"`
	SourceState string `json:"source_state"`
}

func (a *LegacyResourceClaimAPI) List(c *gin.Context) {
	if !requirePlatformAccess(c, false) {
		return
	}
	page, size := pageParams(c)
	query := db.DB.WithContext(c.Request.Context()).Model(&model.LegacyResourceClaim{})
	if sourceType := strings.TrimSpace(c.Query("source_type")); sourceType != "" {
		query = query.Where("source_type = ?", sourceType)
	}
	if tenantID := strings.TrimSpace(c.Query("tenant_id")); tenantID != "" {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询存量归属失败"))
		return
	}
	var claims []model.LegacyResourceClaim
	if err := query.Order("updated_at DESC").Offset((page - 1) * size).Limit(size).Find(&claims).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询存量归属失败"))
		return
	}
	items := make([]legacyClaimListItem, 0, len(claims))
	for _, claim := range claims {
		items = append(items, a.decorate(c, claim))
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}

func (a *LegacyResourceClaimAPI) Claim(c *gin.Context) {
	if !requirePlatformAccess(c, true) {
		return
	}
	var req legacyClaimRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("存量对象、Tenant 和认领原因不能为空"))
		return
	}
	req.SourceType = strings.TrimSpace(req.SourceType)
	req.SourceID = strings.TrimSpace(req.SourceID)
	req.TenantID = strings.TrimSpace(req.TenantID)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.SourceType == "" || req.SourceID == "" || req.TenantID == "" || req.Reason == "" {
		c.JSON(http.StatusBadRequest, NewErrorResponse("存量对象、Tenant 和认领原因不能为空"))
		return
	}
	if !requireTenantPermission(c, req.TenantID, PermissionTenantResourcesWrite) {
		return
	}
	var tenant model.Tenant
	if err := db.DB.WithContext(c.Request.Context()).First(&tenant, "id = ?", req.TenantID).Error; err != nil || tenant.Status != model.TenantStatusActive {
		c.JSON(http.StatusBadRequest, NewErrorResponse("Tenant 不存在或不可用"))
		return
	}
	if _, _, err := legacySource(req.SourceType, req.SourceID); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse(err.Error()))
		return
	}
	var claim model.LegacyResourceClaim
	err := db.DB.WithContext(c.Request.Context()).Where("source_type = ? AND source_id = ?", req.SourceType, req.SourceID).First(&claim).Error
	created := false
	if errors.Is(err, gorm.ErrRecordNotFound) {
		claim = model.LegacyResourceClaim{ID: uuid.NewString(), SourceType: req.SourceType, SourceID: req.SourceID}
		created = true
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询存量归属失败"))
		return
	} else if claim.Status == "active" {
		if claim.TenantID != req.TenantID {
			c.JSON(http.StatusConflict, NewErrorResponse("存量对象已归属其他 Tenant，请先撤销原归属"))
			return
		}
		c.JSON(http.StatusOK, NewSuccessResponse(a.decorate(c, claim)))
		return
	}
	claim.TenantID = req.TenantID
	claim.Status = "active"
	claim.ClaimedBy = getAdminIDFromContext(c)
	claim.ClaimReason = req.Reason
	if created {
		err = db.DB.WithContext(c.Request.Context()).Create(&claim).Error
	} else {
		err = db.DB.WithContext(c.Request.Context()).Save(&claim).Error
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("保存存量归属失败"))
		return
	}
	action := "claim_legacy_resource"
	if !created {
		action = "reclaim_legacy_resource"
	}
	recordAuditLog(c.Request.Context(), c, action, "legacy_resource_claim", claim.ID, req.SourceType+":"+req.SourceID, claim)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, NewSuccessResponse(a.decorate(c, claim)))
}

func (a *LegacyResourceClaimAPI) Revoke(c *gin.Context) {
	if !requirePlatformAccess(c, true) {
		return
	}
	var claim model.LegacyResourceClaim
	if err := db.DB.WithContext(c.Request.Context()).First(&claim, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("存量归属不存在"))
		return
	}
	if !requireTenantPermission(c, claim.TenantID, PermissionTenantResourcesWrite) {
		return
	}
	if claim.Status != "active" {
		c.JSON(http.StatusConflict, NewErrorResponse("存量归属已撤销"))
		return
	}
	claim.Status = "revoked"
	if err := db.DB.WithContext(c.Request.Context()).Save(&claim).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("撤销存量归属失败"))
		return
	}
	recordAuditLog(c.Request.Context(), c, "revoke_legacy_resource_claim", "legacy_resource_claim", claim.ID, claim.SourceType+":"+claim.SourceID, claim)
	c.JSON(http.StatusOK, NewSuccessResponse(a.decorate(c, claim)))
}

func (a *LegacyResourceClaimAPI) decorate(c *gin.Context, claim model.LegacyResourceClaim) legacyClaimListItem {
	item := legacyClaimListItem{LegacyResourceClaim: claim}
	var tenant model.Tenant
	if err := db.DB.WithContext(c.Request.Context()).First(&tenant, "id = ?", claim.TenantID).Error; err == nil {
		item.TenantName = tenant.Name
	}
	item.SourceName, item.SourceState, _ = legacySource(claim.SourceType, claim.SourceID)
	return item
}

func legacySource(sourceType, sourceID string) (string, string, error) {
	switch sourceType {
	case model.LegacySourceAgentNode:
		id, err := strconv.ParseUint(sourceID, 10, 64)
		if err != nil {
			return "", "", errors.New("Agent Node ID 无效")
		}
		var node model.Node
		if err := db.DB.Where("id = ? AND type = ?", id, model.NodeTypeAgent).First(&node).Error; err != nil {
			return "", "", errors.New("Agent Node 不存在")
		}
		state := "offline"
		if node.LastHeartbeat != nil && time.Since(*node.LastHeartbeat) < 60*time.Second {
			state = "online"
		}
		return node.Name, state, nil
	case model.LegacySourceEndpoint:
		var endpoint model.Endpoint
		if err := db.DB.Where("id = ? AND revoked = ?", sourceID, false).First(&endpoint).Error; err != nil {
			return "", "", errors.New("Endpoint 不存在或已注销")
		}
		name := endpoint.Alias
		if name == "" {
			name = endpoint.Name
		}
		return name, endpoint.Status, nil
	default:
		return "", "", errors.New("不支持的存量对象类型")
	}
}
