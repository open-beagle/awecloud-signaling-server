package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// An attempt is durable before a sensitive mutation starts. The committed state
// and its before/after evidence are saved in the same transaction as DB changes.
// If finalization is interrupted, "committed"/"started" remains distinguishable
// from a completed request; it must never be interpreted as successful sync.
type sensitiveWriteDetail struct {
	Result            string      `json:"result"`
	HTTPStatus        int         `json:"http_status,omitempty"`
	DatabaseCommitted bool        `json:"database_committed"`
	ExternalStatus    string      `json:"external_status,omitempty"`
	SyncStatus        string      `json:"sync_status"`
	Request           interface{} `json:"request,omitempty"`
	Before            interface{} `json:"before"`
	After             interface{} `json:"after"`
}

type sensitiveWriteAudit struct {
	c        *gin.Context
	database *gorm.DB
	entry    *model.AuditLog
	detail   sensitiveWriteDetail
}

func beginSensitiveWrite(c *gin.Context, action, targetType, targetID string) *sensitiveWriteAudit {
	a := &sensitiveWriteAudit{c: c, database: db.DB, detail: sensitiveWriteDetail{Result: "started", SyncStatus: "not_started"}}
	entry, err := recordAuditLogEntryWithDB(c.Request.Context(), a.database, c, action, targetType, targetID, "", a.detail)
	if err != nil {
		logger.Errorf("敏感操作审计创建失败: action=%s request_id=%s error=%v", action, requestID(c), err)
		c.JSON(http.StatusInternalServerError, NewErrorResponse("审计记录不可用，操作未执行"))
		return nil
	}
	a.entry = entry
	return a
}

func (a *sensitiveWriteAudit) save(ctx context.Context, database *gorm.DB) error {
	data, err := json.Marshal(a.detail)
	if err != nil {
		return err
	}
	result := database.WithContext(ctx).Model(&model.AuditLog{}).Where("id = ?", a.entry.ID).Updates(map[string]interface{}{
		"target_id": a.entry.TargetID, "target_name": a.entry.TargetName, "detail": string(data),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("audit record %d missing", a.entry.ID)
	}
	return nil
}

func (a *sensitiveWriteAudit) transaction(change func(*gorm.DB) error) error {
	err := a.database.WithContext(a.c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := change(tx); err != nil {
			return err
		}
		a.detail.DatabaseCommitted = true
		a.detail.Result = "committed"
		return a.save(a.c.Request.Context(), tx)
	})
	if err != nil {
		a.detail.DatabaseCommitted = false
		a.detail.After = a.detail.Before
		a.detail.Result = "failed"
	}
	return err
}

func (a *sensitiveWriteAudit) sync(syncer aclSyncer) bool {
	if syncer == nil {
		a.detail.SyncStatus = "not_configured"
		return true
	}
	ctx, cancel := context.WithTimeout(a.c.Request.Context(), 30*time.Second)
	defer cancel()
	a.detail.SyncStatus = "failed"
	if err := syncer.FullSync(ctx); err != nil {
		logger.Errorf("敏感操作 ACL 同步失败: audit_id=%d error=%v", a.entry.ID, err)
		message := "ACL 同步失败，请重试或检查服务状态"
		if a.detail.DatabaseCommitted {
			message = "配置已保存，ACL 同步失败，请在 ACL 页面手动同步"
		}
		a.c.JSON(http.StatusInternalServerError, NewErrorResponse(message))
		return false
	}
	a.detail.SyncStatus = "succeeded"
	return true
}

func (a *sensitiveWriteAudit) finish() {
	a.detail.HTTPStatus = a.c.Writer.Status()
	if !a.c.Writer.Written() {
		a.detail.Result = "interrupted"
		a.detail.HTTPStatus = 0
	} else if a.detail.HTTPStatus >= 400 {
		a.detail.Result = "failed"
		if a.detail.DatabaseCommitted || a.detail.ExternalStatus == "applied" {
			a.detail.Result = "partial"
		}
	} else {
		a.detail.Result = "succeeded"
	}
	// Client cancellation must not discard the outcome of an already committed write.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(a.c.Request.Context()), 5*time.Second)
	defer cancel()
	if err := a.save(ctx, a.database); err != nil {
		logger.Errorf("敏感操作审计结果更新失败（保留先前阶段）: audit_id=%d error=%v", a.entry.ID, err)
	}
}

func policyAuditState(raw string) map[string]interface{} {
	var policy headscale.ACLPolicy
	err := json.Unmarshal([]byte(raw), &policy)
	return map[string]interface{}{
		"sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(raw))),
		"policy": policy, "parsed": err == nil,
	}
}

func tunnelAuditState(token *model.DeployToken) map[string]interface{} {
	var ports UpdateSignalTunnelPortsRequest
	err := json.Unmarshal([]byte(token.PortsConfig), &ports)
	return map[string]interface{}{
		"id": token.ID, "name": token.Name, "user_id": token.UserID,
		"target_agent": token.TargetAgentName, "mode": token.Mode, "status": token.Status,
		"ports_config": ports, "ports_config_valid": token.PortsConfig == "" || err == nil,
	}
}
