package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/cache"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	grpcserver "github.com/open-beagle/awecloud-signaling-server/internal/server/grpc"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

const maxDesktopRDPGrantDuration = 7 * 24 * time.Hour

type TenantBusinessAPI struct {
	desktopService *grpcserver.DesktopServiceServer
	aclSyncService *headscale.ACLSyncService
	runtimeStore   *cache.NodeRuntimeStore
}

func NewTenantBusinessAPI() *TenantBusinessAPI { return &TenantBusinessAPI{} }

func (a *TenantBusinessAPI) SetDesktopService(svc *grpcserver.DesktopServiceServer) {
	a.desktopService = svc
}

func (a *TenantBusinessAPI) SetACLSyncService(svc *headscale.ACLSyncService) {
	a.aclSyncService = svc
}

func (a *TenantBusinessAPI) SetRuntimeStore(store *cache.NodeRuntimeStore) {
	a.runtimeStore = store
}

type TenantMemberDeviceItem struct {
	NodeID           uint64     `json:"node_id"`
	UserID           uint64     `json:"user_id"`
	UserName         string     `json:"user_name"`
	UserAlias        string     `json:"user_alias,omitempty"`
	DeviceName       string     `json:"device_name"`
	Hostname         string     `json:"hostname,omitempty"`
	IP               string     `json:"ip,omitempty"`
	Version          string     `json:"version,omitempty"`
	LastHeartbeat    *time.Time `json:"last_heartbeat,omitempty"`
	Online           bool       `json:"online"`
	RDPMode          string     `json:"rdp_mode"`
	RDPAllowShare    bool       `json:"rdp_allow_share"`
	RDPAdminDisabled bool       `json:"rdp_admin_disabled"`
	RDPLocalReady    bool       `json:"rdp_local_ready"`
	RDPLocalPort     int        `json:"rdp_local_port"`
	RDPTunnelExposed bool       `json:"rdp_tunnel_exposed"`
	ActiveGrantCount int64      `json:"active_grant_count"`
}

type TenantDeviceRDPGrantItem struct {
	ID              uint64     `json:"id"`
	TenantID        string     `json:"tenant_id"`
	TargetNodeID    uint64     `json:"target_node_id"`
	OwnerUserID     uint64     `json:"owner_user_id"`
	GranteeUserID   uint64     `json:"grantee_user_id"`
	GranteeUserName string     `json:"grantee_user_name"`
	GranteeAlias    string     `json:"grantee_alias,omitempty"`
	CreatedByName   string     `json:"created_by_name,omitempty"`
	Reason          string     `json:"reason,omitempty"`
	Status          string     `json:"status"`
	ExpiresAt       time.Time  `json:"expires_at"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	RevokeReason    string     `json:"revoke_reason,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type createDeviceRDPGrantRequest struct {
	GranteeUserID   uint64     `json:"grantee_user_id"`
	ExpiresAt       *time.Time `json:"expires_at"`
	DurationMinutes int        `json:"duration_minutes"`
	Reason          string     `json:"reason"`
}

type TenantAuditItem struct {
	ID                  int64     `json:"id"`
	ActorAdminID        int64     `json:"actor_admin_id"`
	ActorUsername       string    `json:"actor_username"`
	ActorUserID         uint64    `json:"actor_user_id"`
	EffectiveUserID     uint64    `json:"effective_user_id"`
	SimulationSessionID string    `json:"simulation_session_id"`
	PlatformRole        string    `json:"platform_role"`
	TenantRole          string    `json:"tenant_role"`
	RequiredPermission  string    `json:"required_permission"`
	PermissionRevision  int64     `json:"permission_revision"`
	ActionType          string    `json:"action_type"`
	TargetType          string    `json:"target_type"`
	TargetID            string    `json:"target_id"`
	TargetName          string    `json:"target_name"`
	RequestID           string    `json:"request_id"`
	SourceIP            string    `json:"source_ip"`
	Detail              string    `json:"detail,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}

func (a *TenantBusinessAPI) ListMemberDevices(c *gin.Context) {
	tenantID := strings.TrimSpace(c.Param("id"))
	if !requireTenantPermission(c, tenantID, PermissionTenantDevicesRead) {
		return
	}
	page, size := pageParams(c)
	now := time.Now()
	base := db.DB.WithContext(c.Request.Context()).Table("node AS n").
		Joins("JOIN user AS u ON u.id = n.user_id").
		Joins("JOIN tenant_membership AS tm ON tm.user_id = u.id").
		Where("tm.tenant_id = ? AND tm.enabled = ? AND tm.deleted_at IS NULL AND u.enabled = ? AND n.type = ? AND (tm.expires_at IS NULL OR tm.expires_at > ?)",
			tenantID, true, true, model.NodeTypeDesktop, now)
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		pattern := "%" + search + "%"
		base = base.Where("u.name LIKE ? OR u.alias LIKE ? OR n.name LIKE ? OR n.hostname LIKE ?", pattern, pattern, pattern, pattern)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询成员设备失败"))
		return
	}
	type row struct {
		NodeID           uint64
		UserID           uint64
		UserName         string
		UserAlias        string
		DeviceName       string
		Hostname         string
		IP               string
		Version          string
		LastHeartbeat    *time.Time
		RDPMode          string
		RDPAllowShare    bool
		RDPAdminDisabled bool
	}
	var rows []row
	if err := base.Select(`n.id AS node_id, u.id AS user_id, u.name AS user_name, u.alias AS user_alias,
			n.name AS device_name, n.hostname, n.ip, n.version, n.last_heartbeat,
			n.rdp_mode, n.rdp_allow_share, n.rdp_admin_disabled`).
		Order(gorm.Expr("CASE WHEN n.last_heartbeat > ? THEN 0 ELSE 1 END ASC", now.Add(-2*time.Minute))).
		Order("n.last_heartbeat DESC").
		Order("n.id ASC").
		Offset((page - 1) * size).Limit(size).Scan(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询成员设备失败"))
		return
	}

	grantCounts := make(map[uint64]int64)
	if len(rows) > 0 && db.DB.Migrator().HasTable(&model.AclDesktopRDPPermission{}) {
		nodeIDs := make([]uint64, 0, len(rows))
		for _, r := range rows {
			nodeIDs = append(nodeIDs, r.NodeID)
		}
		type countRow struct {
			TargetNodeID uint64
			Total        int64
		}
		var counts []countRow
		if err := db.DB.WithContext(c.Request.Context()).Model(&model.AclDesktopRDPPermission{}).
			Select("target_node_id, COUNT(*) AS total").
			Where("tenant_id = ? AND target_node_id IN ? AND enabled = ? AND revoked_at IS NULL AND expires_at > ?",
				tenantID, nodeIDs, true, now).
			Group("target_node_id").
			Scan(&counts).Error; err == nil {
			for _, cr := range counts {
				grantCounts[cr.TargetNodeID] = cr.Total
			}
		}
	}

	items := make([]TenantMemberDeviceItem, 0, len(rows))
	for _, value := range rows {
		online := value.LastHeartbeat != nil && value.LastHeartbeat.After(now.Add(-2*time.Minute))
		ip := value.IP
		var rdpLocalReady bool
		var rdpLocalPort int
		var rdpTunnelExposed bool
		if a.runtimeStore != nil {
			if rn, ok := a.runtimeStore.GetNode(value.NodeID); ok {
				if !rn.LastHeartbeat.IsZero() {
					hb := rn.LastHeartbeat
					value.LastHeartbeat = &hb
					online = hb.After(now.Add(-2 * time.Minute))
				} else {
					online = false
				}
				if rn.IP != "" {
					ip = rn.IP
				}
				rdpLocalReady = rn.RDPLocalReady
				rdpLocalPort = rn.RDPLocalPort
				rdpTunnelExposed = rn.RDPTunnelExposed
			}
		}
		items = append(items, TenantMemberDeviceItem{
			NodeID:           value.NodeID,
			UserID:           value.UserID,
			UserName:         value.UserName,
			UserAlias:        value.UserAlias,
			DeviceName:       value.DeviceName,
			Hostname:         value.Hostname,
			IP:               ip,
			Version:          value.Version,
			LastHeartbeat:    value.LastHeartbeat,
			Online:           online,
			RDPMode:          model.NormalizeDesktopRDPMode(value.RDPMode),
			RDPAllowShare:    value.RDPAllowShare,
			RDPAdminDisabled: value.RDPAdminDisabled,
			RDPLocalReady:    rdpLocalReady,
			RDPLocalPort:     rdpLocalPort,
			RDPTunnelExposed: rdpTunnelExposed,
			ActiveGrantCount: grantCounts[value.NodeID],
		})
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}

func (a *TenantBusinessAPI) loadTenantMemberDesktopNode(ctx context.Context, tenantID string, nodeID uint64, now time.Time) (*model.Node, bool) {
	var node model.Node
	err := db.DB.WithContext(ctx).Preload("User").
		Joins("JOIN user AS u ON u.id = node.user_id AND u.enabled = ?", true).
		Joins("JOIN tenant_membership AS tm ON tm.user_id = node.user_id").
		Where("node.id = ? AND node.type = ?", nodeID, model.NodeTypeDesktop).
		Where("tm.tenant_id = ? AND tm.enabled = ? AND tm.deleted_at IS NULL AND (tm.expires_at IS NULL OR tm.expires_at > ?)",
			tenantID, true, now).
		First(&node).Error
	if err != nil {
		return nil, false
	}
	return &node, true
}

// ListDeviceRDPGrants 查询指定成员 Desktop 设备在当前租户下的跨人远程桌面临时授权列表
func (a *TenantBusinessAPI) ListDeviceRDPGrants(c *gin.Context) {
	tenantID := strings.TrimSpace(c.Param("id"))
	if !requireTenantPermission(c, tenantID, PermissionTenantDevicesRead) {
		return
	}
	nodeID, err := strconv.ParseUint(c.Param("node_id"), 10, 64)
	if err != nil || nodeID == 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的设备 ID"))
		return
	}
	now := time.Now()
	if _, ok := a.loadTenantMemberDesktopNode(c.Request.Context(), tenantID, nodeID, now); !ok {
		codedError(c, http.StatusNotFound, ErrorCodeTenantObjectNotFound, "当前租户范围内成员设备不存在")
		return
	}

	var perms []model.AclDesktopRDPPermission
	if err := db.DB.WithContext(c.Request.Context()).
		Preload("GranteeUser").
		Where("tenant_id = ? AND target_node_id = ?", tenantID, nodeID).
		Order("created_at DESC").
		Limit(50).
		Find(&perms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询设备远程桌面授权失败"))
		return
	}

	items := make([]TenantDeviceRDPGrantItem, 0, len(perms))
	for _, perm := range perms {
		status := "active"
		if !perm.Enabled || perm.RevokedAt != nil {
			status = "revoked"
		} else if !now.Before(perm.ExpiresAt) {
			status = "expired"
		}
		granteeName := ""
		granteeAlias := ""
		if perm.GranteeUser != nil {
			granteeName = perm.GranteeUser.Name
			granteeAlias = perm.GranteeUser.Alias
		}
		items = append(items, TenantDeviceRDPGrantItem{
			ID:              perm.ID,
			TenantID:        perm.TenantID,
			TargetNodeID:    perm.TargetNodeID,
			OwnerUserID:     perm.OwnerUserID,
			GranteeUserID:   perm.GranteeUserID,
			GranteeUserName: granteeName,
			GranteeAlias:    granteeAlias,
			CreatedByName:   perm.CreatedByName,
			Reason:          perm.Reason,
			Status:          status,
			ExpiresAt:       perm.ExpiresAt,
			RevokedAt:       perm.RevokedAt,
			RevokeReason:    perm.RevokeReason,
			CreatedAt:       perm.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, NewSuccessResponse(items))
}

// CreateDeviceRDPGrant 为已显式开启共享许可的成员 Desktop 设备签发带到期时间的同租户临时 RDP 授权
func (a *TenantBusinessAPI) CreateDeviceRDPGrant(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := strings.TrimSpace(c.Param("id"))
	if !requireTenantPermission(c, tenantID, PermissionTenantGrantsWrite) {
		return
	}
	nodeID, err := strconv.ParseUint(c.Param("node_id"), 10, 64)
	if err != nil || nodeID == 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的设备 ID"))
		return
	}

	var req createDeviceRDPGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.GranteeUserID == 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请指定有效的被授权租户成员"))
		return
	}

	now := time.Now()
	node, ok := a.loadTenantMemberDesktopNode(ctx, tenantID, nodeID, now)
	if !ok {
		codedError(c, http.StatusNotFound, ErrorCodeTenantObjectNotFound, "当前租户范围内成员设备不存在")
		return
	}

	if node.RDPAdminDisabled {
		c.JSON(http.StatusConflict, NewErrorResponse("平台管理员已对该设备启用远程桌面紧急熔断，无法创建临时授权"))
		return
	}
	if !node.RDPAllowShare {
		c.JSON(http.StatusForbidden, NewErrorResponse("机主未在该设备上开启“允许同租户临时共享”，管理员不能代开跨人远程桌面"))
		return
	}
	if model.NormalizeDesktopRDPMode(node.RDPMode) == model.DesktopRDPModeDisabled {
		c.JSON(http.StatusConflict, NewErrorResponse("机主当前已关闭该设备的远程桌面暴露"))
		return
	}
	if req.GranteeUserID == node.UserID {
		c.JSON(http.StatusBadRequest, NewErrorResponse("设备归属人已天然具备本人 Desktop 远程桌面权限，无需重复授权"))
		return
	}

	var expiresAt time.Time
	if req.ExpiresAt != nil && !req.ExpiresAt.IsZero() {
		expiresAt = *req.ExpiresAt
	} else if req.DurationMinutes > 0 {
		expiresAt = now.Add(time.Duration(req.DurationMinutes) * time.Minute)
	}
	if expiresAt.IsZero() || !expiresAt.After(now) {
		c.JSON(http.StatusBadRequest, NewErrorResponse("跨人远程桌面临时授权必须设置未来的到期时间"))
		return
	}
	if expiresAt.Sub(now) > maxDesktopRDPGrantDuration {
		c.JSON(http.StatusBadRequest, NewErrorResponse("单次临时远程桌面授权最长不超过 7 天"))
		return
	}

	var grantee model.User
	if err := db.DB.WithContext(ctx).
		Joins("JOIN tenant_membership AS tm ON tm.user_id = user.id").
		Where("user.id = ? AND user.enabled = ? AND user.role = ?", req.GranteeUserID, true, model.UserRoleClient).
		Where("tm.tenant_id = ? AND tm.enabled = ? AND tm.deleted_at IS NULL AND (tm.expires_at IS NULL OR tm.expires_at > ?)",
			tenantID, true, now).
		First(&grantee).Error; err != nil {
		c.JSON(http.StatusForbidden, NewErrorResponse("被授权用户不属于当前有效租户成员或非客户端用户"))
		return
	}

	var existingCount int64
	if err := db.DB.WithContext(ctx).Model(&model.AclDesktopRDPPermission{}).
		Where("tenant_id = ? AND target_node_id = ? AND grantee_user_id = ? AND enabled = ? AND revoked_at IS NULL AND expires_at > ?",
			tenantID, node.ID, grantee.ID, true, now).
		Count(&existingCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("检查既有远程桌面授权失败"))
		return
	}
	if existingCount > 0 {
		c.JSON(http.StatusConflict, NewErrorResponse("该成员已拥有此设备尚未到期的远程桌面临时授权，请先撤销后再重新签发"))
		return
	}

	admin, _, _ := currentAdmin(c)
	createdByName := ""
	var createdByID uint64
	if admin != nil {
		createdByName = admin.Username
		if admin.ID > 0 {
			createdByID = uint64(admin.ID)
		}
	}

	perm := model.AclDesktopRDPPermission{
		TenantID:        tenantID,
		TargetNodeID:    node.ID,
		OwnerUserID:     node.UserID,
		GranteeUserID:   grantee.ID,
		CreatedByUserID: createdByID,
		CreatedByName:   createdByName,
		Reason:          strings.TrimSpace(req.Reason),
		Enabled:         true,
		ExpiresAt:       expiresAt,
	}
	if err := db.DB.WithContext(ctx).Create(&perm).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建远程桌面临时授权失败"))
		return
	}

	recordAuditLog(ctx, c, model.ActionGrantDesktop, "node", strconv.FormatUint(node.ID, 10), node.Name, map[string]any{
		"grant_id":        perm.ID,
		"grantee_user_id": grantee.ID,
		"grantee_name":    grantee.Name,
		"expires_at":      perm.ExpiresAt,
		"reason":          perm.Reason,
	})

	a.syncAndNotifyRDPGrantChange(node.ID, node.UserID, grantee.ID)

	c.JSON(http.StatusCreated, NewSuccessResponse(TenantDeviceRDPGrantItem{
		ID:              perm.ID,
		TenantID:        perm.TenantID,
		TargetNodeID:    perm.TargetNodeID,
		OwnerUserID:     perm.OwnerUserID,
		GranteeUserID:   perm.GranteeUserID,
		GranteeUserName: grantee.Name,
		GranteeAlias:    grantee.Alias,
		CreatedByName:   perm.CreatedByName,
		Reason:          perm.Reason,
		Status:          "active",
		ExpiresAt:       perm.ExpiresAt,
		CreatedAt:       perm.CreatedAt,
	}))
}

// RevokeDeviceRDPGrant 撤销同租户 Desktop 设备的远程桌面临时授权并立即推送被控端断开活动连接
func (a *TenantBusinessAPI) RevokeDeviceRDPGrant(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := strings.TrimSpace(c.Param("id"))
	if !requireTenantPermission(c, tenantID, PermissionTenantGrantsWrite) {
		return
	}
	nodeID, err := strconv.ParseUint(c.Param("node_id"), 10, 64)
	if err != nil || nodeID == 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的设备 ID"))
		return
	}
	grantID, err := strconv.ParseUint(c.Param("grant_id"), 10, 64)
	if err != nil || grantID == 0 {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的授权 ID"))
		return
	}

	var perm model.AclDesktopRDPPermission
	if err := db.DB.WithContext(ctx).
		Where("id = ? AND tenant_id = ? AND target_node_id = ?", grantID, tenantID, nodeID).
		First(&perm).Error; err != nil {
		codedError(c, http.StatusNotFound, ErrorCodeTenantObjectNotFound, "临时远程桌面授权记录不存在")
		return
	}
	if !perm.Enabled || perm.RevokedAt != nil {
		c.JSON(http.StatusConflict, NewErrorResponse("该临时远程桌面授权已撤销"))
		return
	}

	now := time.Now()
	revokeReason := strings.TrimSpace(c.Query("reason"))
	if revokeReason == "" {
		revokeReason = "租户管理员手动撤销"
	}
	if err := db.DB.WithContext(ctx).Model(&model.AclDesktopRDPPermission{}).
		Where("id = ? AND enabled = ?", perm.ID, true).
		Updates(map[string]any{
			"enabled":       false,
			"revoked_at":    &now,
			"revoke_reason": revokeReason,
		}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("撤销远程桌面临时授权失败"))
		return
	}

	var node model.Node
	_ = db.DB.WithContext(ctx).First(&node, perm.TargetNodeID).Error
	recordAuditLog(ctx, c, model.ActionRevokeDesktop, "node", strconv.FormatUint(perm.TargetNodeID, 10), node.Name, map[string]any{
		"grant_id":        perm.ID,
		"grantee_user_id": perm.GranteeUserID,
		"revoke_reason":   revokeReason,
	})

	a.syncAndNotifyRDPGrantChange(perm.TargetNodeID, perm.OwnerUserID, perm.GranteeUserID)

	c.JSON(http.StatusOK, NewSuccessMessageResponse("临时远程桌面授权已撤销", nil))
}

func (a *TenantBusinessAPI) syncAndNotifyRDPGrantChange(targetNodeID, ownerUserID, granteeUserID uint64) {
	if a.desktopService != nil {
		a.desktopService.NotifyDesktopDataChange(targetNodeID, pb.DesktopDataType_DESKTOP_DATA_TYPE_RDP_POLICY)
		if granteeUserID != 0 {
			a.desktopService.NotifyUserDesktopsDataChange(granteeUserID, pb.DesktopDataType_DESKTOP_DATA_TYPE_DEVICES)
		}
		if ownerUserID != 0 {
			a.desktopService.NotifyUserDesktopsDataChange(ownerUserID, pb.DesktopDataType_DESKTOP_DATA_TYPE_DEVICES)
		}
	}
	if a.aclSyncService != nil {
		go func() {
			syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := a.aclSyncService.FullSync(syncCtx); err != nil {
				logger.Warnf("Desktop RDP 临时授权变更后同步 Headscale Tag/ACL 失败: %v", err)
			}
		}()
	}
}

func (a *TenantBusinessAPI) ListAuditLogs(c *gin.Context) {
	tenantID := strings.TrimSpace(c.Param("id"))
	if !requireTenantPermission(c, tenantID, PermissionTenantAuditRead) {
		return
	}
	page, size := pageParams(c)
	query := db.DB.WithContext(c.Request.Context()).Model(&model.AuditLog{}).Where("tenant_id = ?", tenantID)
	if actionType := strings.TrimSpace(c.Query("action_type")); actionType != "" {
		query = query.Where("action_type = ?", actionType)
	}
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		pattern := "%" + search + "%"
		query = query.Where("actor_username LIKE ? OR target_name LIKE ? OR target_id LIKE ? OR request_id LIKE ?", pattern, pattern, pattern, pattern)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询租户审计失败"))
		return
	}
	var logs []model.AuditLog
	if err := query.Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询租户审计失败"))
		return
	}
	items := make([]TenantAuditItem, 0, len(logs))
	for _, log := range logs {
		items = append(items, TenantAuditItem{
			ID: log.ID, ActorAdminID: log.ActorAdminID, ActorUsername: log.ActorUsername, PlatformRole: log.PlatformRole,
			ActorUserID: log.ActorUserID, EffectiveUserID: log.EffectiveUserID, SimulationSessionID: log.SimulationSessionID,
			TenantRole: log.TenantRole, RequiredPermission: log.RequiredPermission, PermissionRevision: log.PermissionRevision,
			ActionType: log.ActionType, TargetType: log.TargetType, TargetID: log.TargetID, TargetName: log.TargetName,
			RequestID: log.RequestID, SourceIP: log.SourceIP, Detail: log.Detail, CreatedAt: log.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}
