package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// These are ACL fields only: never capture arbitrary request/response bodies,
// credentials, deploy tokens or generated deployment YAML in audit detail.
type aclWriteRequest struct {
	UserIDs    []uint64 `json:"user_ids,omitempty"`
	GroupIDs   []int64  `json:"group_ids,omitempty"`
	SSHUsers   []string `json:"ssh_users,omitempty"`
	K8SGroups  []string `json:"k8s_groups,omitempty"`
	Namespaces []string `json:"namespaces,omitempty"`
}

func (a *ACLAPI) writeACL(c *gin.Context, kind string, groups, remove bool) {
	verb, principal := "grant", "user"
	if remove {
		verb = "revoke"
	}
	if groups {
		principal = "group"
	}
	audit := beginSensitiveWrite(c, verb+"_"+kind+"_acl_"+principal, "acl_"+kind, c.Param("id"))
	if audit == nil {
		return
	}
	defer audit.finish()
	ctx := c.Request.Context()

	var req aclWriteRequest
	audit.detail.Request = &req
	var principals []interface{}
	if remove {
		param := "uid"
		if groups {
			param = "gid"
		}
		id, err := strconv.ParseInt(c.Param(param), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, NewErrorResponse("无效的授权对象 ID"))
			return
		}
		if groups {
			req.GroupIDs = []int64{id}
		} else {
			req.UserIDs = []uint64{uint64(id)}
		}
	} else if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}
	if groups {
		for _, id := range req.GroupIDs {
			if id <= 0 {
				c.JSON(http.StatusBadRequest, NewErrorResponse("无效的分组 ID"))
				return
			}
			principals = append(principals, id)
		}
	} else {
		for _, id := range req.UserIDs {
			if id == 0 {
				c.JSON(http.StatusBadRequest, NewErrorResponse("无效的用户 ID"))
				return
			}
			principals = append(principals, id)
		}
	}
	if len(principals) == 0 || (!remove && kind == "ssh" && len(req.SSHUsers) == 0) ||
		(!remove && kind == "k8s" && len(req.K8SGroups) == 0) {
		c.JSON(http.StatusBadRequest, NewErrorResponse("请求参数错误"))
		return
	}

	targetID := c.Param("id")
	var target interface{} = targetID
	targetColumn, tableKind := "target_user_id", kind
	var lookupErr error
	switch kind {
	case "service":
		var service model.ProxyService
		lookupErr = audit.database.WithContext(ctx).First(&service, "id = ?", targetID).Error
		audit.entry.TargetName = service.Name
		targetColumn = "service_id"
	case "endpoint_k8sapi":
		var endpoint model.Endpoint
		lookupErr = audit.database.WithContext(ctx).First(&endpoint, "id = ?", targetID).Error
		audit.entry.TargetName = endpoint.Name
		target, tableKind = endpoint.UserID, "k8s"
	default:
		id, err := strconv.ParseInt(targetID, 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, NewErrorResponse("无效的目标 ID"))
			return
		}
		target = id
		if kind == "group" {
			var group model.Group
			lookupErr = audit.database.WithContext(ctx).First(&group, id).Error
			audit.entry.TargetName = group.Name
			targetColumn = "target_group_id"
		} else {
			var user model.User
			lookupErr = audit.database.WithContext(ctx).First(&user, id).Error
			audit.entry.TargetName = user.Name
			if lookupErr == nil && !remove && kind == "ssh" && user.Role == model.UserRoleAgent && !user.SSHEnabled {
				c.JSON(http.StatusBadRequest, NewErrorResponse("该用户未启用 SSH"))
				return
			}
		}
	}
	if lookupErr != nil {
		status := http.StatusInternalServerError
		if errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, NewErrorResponse("查询授权目标失败"))
		return
	}

	// Table/column names come only from the handler's fixed kind, never user input.
	table := "acl_" + tableKind + "_" + principal + "_permission"
	principalColumn := principal + "_id"
	if kind == "user" && !groups {
		principalColumn = "granted_user_id"
	}
	where := targetColumn + " = ? AND " + principalColumn + " IN ?"
	args := []interface{}{target, principals}
	var before, after []map[string]interface{}
	err := audit.transaction(func(tx *gorm.DB) error {
		before = make([]map[string]interface{}, 0)
		if err := tx.Table(table).Where(where, args...).Order("id").Find(&before).Error; err != nil {
			return err
		}
		audit.detail.Before = before
		if remove {
			if len(before) == 0 {
				return gorm.ErrRecordNotFound
			}
			if err := tx.Table(table).Where(where, args...).Delete(map[string]interface{}{}).Error; err != nil {
				return err
			}
		} else {
			for _, id := range principals {
				values := map[string]interface{}{targetColumn: target, principalColumn: id, "granted_at": time.Now()}
				updates := map[string]interface{}{}
				switch tableKind {
				case "ssh":
					updates["ssh_users"] = formatSSHUsers(req.SSHUsers)
					updates["enabled"] = true
				case "k8s":
					updates[tx.NamingStrategy.ColumnName(table, "K8SGroups")] = formatJSONStringArray(req.K8SGroups)
					updates["namespaces"] = formatJSONStringArray(req.Namespaces)
					updates["enabled"] = true
				}
				for key, value := range updates {
					values[key] = value
				}
				conflict := clause.OnConflict{Columns: []clause.Column{{Name: targetColumn}, {Name: principalColumn}}, DoNothing: len(updates) == 0}
				if len(updates) > 0 {
					conflict.DoUpdates = clause.Assignments(updates)
				}
				if err := tx.Table(table).Clauses(conflict).Create(values).Error; err != nil {
					return err
				}
			}
		}
		after = make([]map[string]interface{}, 0)
		if err := tx.Table(table).Where(where, args...).Order("id").Find(&after).Error; err != nil {
			return err
		}
		audit.detail.After = after
		return nil
	})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, NewErrorResponse("授权变更未生效"))
		return
	}
	if tableKind == "k8s" {
		audit.detail.SyncStatus = "not_required"
	} else if !audit.sync(a.aclSync) {
		return
	}
	if kind == "user" {
		a.notifyDesktopDataChange(pb.DesktopDataType_DESKTOP_DATA_TYPE_HOSTS)
	} else if kind == "k8s" && !groups && !remove {
		a.notifyDesktopDataChange(pb.DesktopDataType_DESKTOP_DATA_TYPE_ALL)
	}
	c.JSON(http.StatusOK, NewSuccessMessageResponse("授权变更成功", nil))
}
