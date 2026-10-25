package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

const mcpProtocolVersion = "2025-03-26"

// Handler 内嵌于 signal-server 的远端 MCP Streamable HTTP 处理器（零本地 CLI）
type Handler struct {
	config *config.ServerConfig
	router http.Handler
}

// NewHandler 创建远端 MCP 处理器
func NewHandler(cfg *config.ServerConfig, router http.Handler) *Handler {
	return &Handler{
		config: cfg,
		router: router,
	}
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type toolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolCallResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ServeHTTP 处理 POST /api/v1/mcp 请求
func (h *Handler) ServeHTTP(c *gin.Context) {
	tokenString, ok := h.authenticateMCPToken(c)
	if !ok {
		return
	}

	var req jsonRPCRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      nil,
			Error:   &jsonRPCError{Code: -32700, Message: "无效的 JSON-RPC 请求: " + err.Error()},
		})
		return
	}

	switch req.Method {
	case "initialize":
		c.JSON(http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: gin.H{
				"protocolVersion": mcpProtocolVersion,
				"serverInfo": gin.H{
					"name":    "beagle-signal-mcp",
					"version": "1.0.5",
				},
				"capabilities": gin.H{
					"tools": gin.H{"listChanged": false},
				},
			},
		})
	case "notifications/initialized":
		c.Status(http.StatusAccepted)
	case "ping":
		c.JSON(http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  gin.H{},
		})
	case "tools/list":
		c.JSON(http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: gin.H{
				"tools": h.toolDefinitions(),
			},
		})
	case "tools/call":
		var params toolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			c.JSON(http.StatusOK, jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &jsonRPCError{Code: -32602, Message: "工具调用参数格式错误: " + err.Error()},
			})
			return
		}
		if params.Arguments == nil {
			params.Arguments = map[string]interface{}{}
		}
		res, err := h.dispatchTool(c.Request.Context(), tokenString, req.ID, params.Name, params.Arguments)
		if err != nil {
			c.JSON(http.StatusOK, jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: toolCallResult{
					IsError: true,
					Content: []toolContent{{Type: "text", Text: err.Error()}},
				},
			})
			return
		}
		textBytes, _ := json.MarshalIndent(res, "", "  ")
		c.JSON(http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: toolCallResult{
				Content: []toolContent{{Type: "text", Text: string(textBytes)}},
			},
		})
	default:
		c.JSON(http.StatusOK, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32601, Message: "不支持的 MCP 方法: " + req.Method},
		})
	}
}

// authenticateMCPToken 严格校验 Bearer SIGNAL_MCP_TOKEN（拒绝明文密码，校验管理员状态与 credential_revision）
func (h *Handler) authenticateMCPToken(c *gin.Context) (string, bool) {
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if !strings.HasPrefix(authHeader, "Bearer ") {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"code":    "MCP_TOKEN_REQUIRED",
			"message": "必须提供 Authorization: Bearer <SIGNAL_MCP_TOKEN>，严禁直接传输明文密码",
		})
		c.Abort()
		return "", false
	}

	tokenString := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return []byte(h.config.Security.JWTSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || !token.Valid {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"code":    "MCP_TOKEN_INVALID",
			"message": "SIGNAL_MCP_TOKEN 无效或已过期",
		})
		c.Abort()
		return "", false
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "code": "MCP_TOKEN_INVALID", "message": "Token Claims 无效"})
		c.Abort()
		return "", false
	}

	adminID := claimInt64(claims["admin_id"])
	if adminID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "code": "MCP_TOKEN_INVALID", "message": "Token 缺少有效管理员身份"})
		c.Abort()
		return "", false
	}

	ctx := c.Request.Context()
	var admin model.Admin
	if err := db.DB.WithContext(ctx).Where("id = ? AND enabled = ?", adminID, true).First(&admin).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "code": "ADMIN_DISABLED", "message": "管理员不存在或已停用"})
		c.Abort()
		return "", false
	}

	// 若映射了统一 User，实时校验 auth_revision 与 credential_revision（确保管理员改密后旧 SIGNAL_MCP_TOKEN 立即失效）
	claimUserID := uint64(claimInt64(claims["user_id"]))
	claimAuthRev := claimInt64(claims["auth_revision"])
	claimCredRev := claimInt64(claims["credential_revision"])
	if claimUserID > 0 || claimAuthRev > 0 || claimCredRev > 0 {
		if _, err := service.ResolveLegacyAdminIdentity(db.DB.WithContext(ctx), adminID, claimUserID, claimAuthRev, claimCredRev); err != nil {
			if !errors.Is(err, service.ErrManagementIdentityNotMapped) {
				c.JSON(http.StatusUnauthorized, gin.H{
					"success": false,
					"code":    "AUTH_REVISION_STALE",
					"message": "管理员凭据或权限版本已变更，当前 SIGNAL_MCP_TOKEN 已自动失效，请重新签发",
				})
				c.Abort()
				return "", false
			}
		}
	}

	return tokenString, true
}

func (h *Handler) dispatchTool(ctx context.Context, token string, reqID interface{}, name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "signal_ssh_acl":
		return h.handleSSHACL(ctx, token, args)
	case "signal_k8s_acl":
		return h.handleK8SACL(ctx, token, args)
	case "signal_service_acl":
		return h.handleServiceACL(ctx, token, args)
	case "signal_user_inspect":
		return h.handleUserInspect(ctx, token, args)
	case "signal_node_tunnel":
		return h.handleNodeTunnel(ctx, token, args)
	case "signal_workspace_v2":
		return h.handleWorkspaceV2(ctx, token, reqID, args)
	case "signal_raw_api":
		return h.handleRawAPI(ctx, token, reqID, args)
	default:
		return nil, fmt.Errorf("未知的 MCP 工具: %s", name)
	}
}

// ==================== B1. SSH 登录权限管理 ====================

func (h *Handler) handleSSHACL(ctx context.Context, token string, args map[string]interface{}) (interface{}, error) {
	action := strings.ToLower(strings.TrimSpace(argString(args, "action", "list")))
	switch action {
	case "list":
		search := argString(args, "search", "")
		path := "/api/v1/admin/acl/ssh?page=1&size=100"
		if search != "" {
			path += "&search=" + url.QueryEscape(search)
		}
		return h.callInternalJSON(ctx, token, http.MethodGet, path, nil, nil)

	case "inspect_target":
		target, err := resolveUserByNameOrID(ctx, argString(args, "target", ""))
		if err != nil {
			return nil, fmt.Errorf("定位目标机器失败: %w", err)
		}
		return h.callInternalJSON(ctx, token, http.MethodGet, fmt.Sprintf("/api/v1/admin/acl/ssh/%d", target.ID), nil, nil)

	case "grant_user":
		target, err := resolveUserByNameOrID(ctx, argString(args, "target", ""))
		if err != nil {
			return nil, fmt.Errorf("定位目标机器失败: %w", err)
		}
		user, err := resolveUserByNameOrID(ctx, argString(args, "user", argString(args, "subject", "")))
		if err != nil {
			return nil, fmt.Errorf("定位被授权用户失败: %w", err)
		}
		sshUsers := argStringSlice(args, "ssh_users")
		if len(sshUsers) == 0 {
			sshUsers = []string{"root"}
		}
		autoEnable := argBool(args, "auto_enable_ssh", true)
		sshAutoEnabled := false
		if target.Role == model.UserRoleAgent && !target.SSHEnabled && autoEnable {
			if _, err := h.callInternalJSON(ctx, token, http.MethodPut, fmt.Sprintf("/api/v1/admin/users/%d/ssh", target.ID), gin.H{"enabled": true}, nil); err != nil {
				return nil, fmt.Errorf("自动开启目标机器 SSH 能力失败: %w", err)
			}
			sshAutoEnabled = true
		}
		body := gin.H{
			"user_ids":  []uint64{user.ID},
			"ssh_users": sshUsers,
		}
		if _, err := h.callInternalJSON(ctx, token, http.MethodPost, fmt.Sprintf("/api/v1/admin/acl/ssh/%d/users", target.ID), body, nil); err != nil {
			return nil, err
		}
		detail, _ := h.callInternalJSON(ctx, token, http.MethodGet, fmt.Sprintf("/api/v1/admin/acl/ssh/%d", target.ID), nil, nil)
		return gin.H{
			"business":         "grant_ssh_user",
			"target_id":        target.ID,
			"target_name":      target.Name,
			"target_alias":     target.Alias,
			"granted_user_id":  user.ID,
			"granted_username": user.Name,
			"ssh_users":        sshUsers,
			"ssh_auto_enabled": sshAutoEnabled,
			"acl_detail":       detail,
		}, nil

	case "grant_group":
		target, err := resolveUserByNameOrID(ctx, argString(args, "target", ""))
		if err != nil {
			return nil, fmt.Errorf("定位目标机器失败: %w", err)
		}
		group, err := resolveGroupByNameOrID(ctx, argString(args, "group", argString(args, "subject", "")))
		if err != nil {
			return nil, fmt.Errorf("定位被授权分组失败: %w", err)
		}
		sshUsers := argStringSlice(args, "ssh_users")
		if len(sshUsers) == 0 {
			sshUsers = []string{"root"}
		}
		if target.Role == model.UserRoleAgent && !target.SSHEnabled && argBool(args, "auto_enable_ssh", true) {
			_, _ = h.callInternalJSON(ctx, token, http.MethodPut, fmt.Sprintf("/api/v1/admin/users/%d/ssh", target.ID), gin.H{"enabled": true}, nil)
		}
		body := gin.H{
			"group_ids": []int64{group.ID},
			"ssh_users": sshUsers,
		}
		if _, err := h.callInternalJSON(ctx, token, http.MethodPost, fmt.Sprintf("/api/v1/admin/acl/ssh/%d/groups", target.ID), body, nil); err != nil {
			return nil, err
		}
		detail, _ := h.callInternalJSON(ctx, token, http.MethodGet, fmt.Sprintf("/api/v1/admin/acl/ssh/%d", target.ID), nil, nil)
		return gin.H{
			"business":      "grant_ssh_group",
			"target_id":     target.ID,
			"target_name":   target.Name,
			"granted_group": group.Name,
			"ssh_users":     sshUsers,
			"acl_detail":    detail,
		}, nil

	case "revoke_user":
		target, err := resolveUserByNameOrID(ctx, argString(args, "target", ""))
		if err != nil {
			return nil, fmt.Errorf("定位目标机器失败: %w", err)
		}
		user, err := resolveUserByNameOrID(ctx, argString(args, "user", argString(args, "subject", "")))
		if err != nil {
			return nil, fmt.Errorf("定位用户失败: %w", err)
		}
		return h.callInternalJSON(ctx, token, http.MethodDelete, fmt.Sprintf("/api/v1/admin/acl/ssh/%d/users/%d", target.ID, user.ID), nil, nil)

	case "revoke_group":
		target, err := resolveUserByNameOrID(ctx, argString(args, "target", ""))
		if err != nil {
			return nil, fmt.Errorf("定位目标机器失败: %w", err)
		}
		group, err := resolveGroupByNameOrID(ctx, argString(args, "group", argString(args, "subject", "")))
		if err != nil {
			return nil, fmt.Errorf("定位分组失败: %w", err)
		}
		return h.callInternalJSON(ctx, token, http.MethodDelete, fmt.Sprintf("/api/v1/admin/acl/ssh/%d/groups/%d", target.ID, group.ID), nil, nil)

	default:
		return nil, fmt.Errorf("signal_ssh_acl 不支持的 action: %s", action)
	}
}

// ==================== B2. K8S 集群 API 权限管理 ====================

func (h *Handler) handleK8SACL(ctx context.Context, token string, args map[string]interface{}) (interface{}, error) {
	action := strings.ToLower(strings.TrimSpace(argString(args, "action", "list")))
	targetKind := strings.ToLower(strings.TrimSpace(argString(args, "target_kind", "cluster")))

	switch action {
	case "list":
		if targetKind == "endpoint" {
			return h.callInternalJSON(ctx, token, http.MethodGet, "/api/v1/admin/acl/endpoint-k8sapi?page=1&size=100", nil, nil)
		}
		return h.callInternalJSON(ctx, token, http.MethodGet, "/api/v1/admin/acl/k8s?page=1&size=100", nil, nil)

	case "inspect":
		if targetKind == "endpoint" {
			ep, err := resolveEndpointByNameOrID(ctx, argString(args, "target", ""))
			if err != nil {
				return nil, err
			}
			return h.callInternalJSON(ctx, token, http.MethodGet, fmt.Sprintf("/api/v1/admin/acl/endpoint-k8sapi/%s", ep.ID), nil, nil)
		}
		cluster, err := resolveUserByNameOrID(ctx, argString(args, "target", ""))
		if err != nil {
			return nil, err
		}
		return h.callInternalJSON(ctx, token, http.MethodGet, fmt.Sprintf("/api/v1/admin/acl/k8s/%d", cluster.ID), nil, nil)

	case "grant_user":
		user, err := resolveUserByNameOrID(ctx, argString(args, "user", argString(args, "subject", "")))
		if err != nil {
			return nil, fmt.Errorf("定位被授权用户失败: %w", err)
		}
		k8sGroups := argStringSlice(args, "k8s_groups")
		if len(k8sGroups) == 0 {
			k8sGroups = []string{"system:masters"}
		}
		namespaces := argStringSlice(args, "namespaces")
		body := gin.H{
			"user_ids":   []uint64{user.ID},
			"k8s_groups": k8sGroups,
			"namespaces": namespaces,
		}
		if targetKind == "endpoint" {
			ep, err := resolveEndpointByNameOrID(ctx, argString(args, "target", ""))
			if err != nil {
				return nil, err
			}
			return h.callInternalJSON(ctx, token, http.MethodPost, fmt.Sprintf("/api/v1/admin/acl/endpoint-k8sapi/%s/users", ep.ID), body, nil)
		}
		cluster, err := resolveUserByNameOrID(ctx, argString(args, "target", ""))
		if err != nil {
			return nil, err
		}
		return h.callInternalJSON(ctx, token, http.MethodPost, fmt.Sprintf("/api/v1/admin/acl/k8s/%d/users", cluster.ID), body, nil)

	case "grant_group":
		group, err := resolveGroupByNameOrID(ctx, argString(args, "group", argString(args, "subject", "")))
		if err != nil {
			return nil, fmt.Errorf("定位被授权分组失败: %w", err)
		}
		k8sGroups := argStringSlice(args, "k8s_groups")
		if len(k8sGroups) == 0 {
			k8sGroups = []string{"system:masters"}
		}
		namespaces := argStringSlice(args, "namespaces")
		body := gin.H{
			"group_ids":  []int64{group.ID},
			"k8s_groups": k8sGroups,
			"namespaces": namespaces,
		}
		if targetKind == "endpoint" {
			ep, err := resolveEndpointByNameOrID(ctx, argString(args, "target", ""))
			if err != nil {
				return nil, err
			}
			return h.callInternalJSON(ctx, token, http.MethodPost, fmt.Sprintf("/api/v1/admin/acl/endpoint-k8sapi/%s/groups", ep.ID), body, nil)
		}
		cluster, err := resolveUserByNameOrID(ctx, argString(args, "target", ""))
		if err != nil {
			return nil, err
		}
		return h.callInternalJSON(ctx, token, http.MethodPost, fmt.Sprintf("/api/v1/admin/acl/k8s/%d/groups", cluster.ID), body, nil)

	case "revoke_user":
		user, err := resolveUserByNameOrID(ctx, argString(args, "user", argString(args, "subject", "")))
		if err != nil {
			return nil, err
		}
		if targetKind == "endpoint" {
			ep, err := resolveEndpointByNameOrID(ctx, argString(args, "target", ""))
			if err != nil {
				return nil, err
			}
			return h.callInternalJSON(ctx, token, http.MethodDelete, fmt.Sprintf("/api/v1/admin/acl/endpoint-k8sapi/%s/users/%d", ep.ID, user.ID), nil, nil)
		}
		cluster, err := resolveUserByNameOrID(ctx, argString(args, "target", ""))
		if err != nil {
			return nil, err
		}
		return h.callInternalJSON(ctx, token, http.MethodDelete, fmt.Sprintf("/api/v1/admin/acl/k8s/%d/users/%d", cluster.ID, user.ID), nil, nil)

	case "revoke_group":
		group, err := resolveGroupByNameOrID(ctx, argString(args, "group", argString(args, "subject", "")))
		if err != nil {
			return nil, err
		}
		if targetKind == "endpoint" {
			ep, err := resolveEndpointByNameOrID(ctx, argString(args, "target", ""))
			if err != nil {
				return nil, err
			}
			return h.callInternalJSON(ctx, token, http.MethodDelete, fmt.Sprintf("/api/v1/admin/acl/endpoint-k8sapi/%s/groups/%d", ep.ID, group.ID), nil, nil)
		}
		cluster, err := resolveUserByNameOrID(ctx, argString(args, "target", ""))
		if err != nil {
			return nil, err
		}
		return h.callInternalJSON(ctx, token, http.MethodDelete, fmt.Sprintf("/api/v1/admin/acl/k8s/%d/groups/%d", cluster.ID, group.ID), nil, nil)

	default:
		return nil, fmt.Errorf("signal_k8s_acl 不支持的 action: %s", action)
	}
}

// ==================== B3. 内网服务（ProxyService）与分组互通授权 ====================

func (h *Handler) handleServiceACL(ctx context.Context, token string, args map[string]interface{}) (interface{}, error) {
	action := strings.ToLower(strings.TrimSpace(argString(args, "action", "list")))
	switch action {
	case "list":
		return h.callInternalJSON(ctx, token, http.MethodGet, "/api/v1/admin/acl/services?page=1&size=100", nil, nil)

	case "inspect":
		svc, err := resolveProxyServiceByNameOrID(ctx, argString(args, "service", argString(args, "target", "")))
		if err != nil {
			return nil, err
		}
		return h.callInternalJSON(ctx, token, http.MethodGet, fmt.Sprintf("/api/v1/admin/acl/services/%s", svc.ID), nil, nil)

	case "grant_user":
		svc, err := resolveProxyServiceByNameOrID(ctx, argString(args, "service", argString(args, "target", "")))
		if err != nil {
			return nil, err
		}
		user, err := resolveUserByNameOrID(ctx, argString(args, "user", argString(args, "subject", "")))
		if err != nil {
			return nil, err
		}
		return h.callInternalJSON(ctx, token, http.MethodPost, fmt.Sprintf("/api/v1/admin/acl/services/%s/users", svc.ID), gin.H{
			"user_ids": []uint64{user.ID},
		}, nil)

	case "grant_group":
		svc, err := resolveProxyServiceByNameOrID(ctx, argString(args, "service", argString(args, "target", "")))
		if err != nil {
			return nil, err
		}
		group, err := resolveGroupByNameOrID(ctx, argString(args, "group", argString(args, "subject", "")))
		if err != nil {
			return nil, err
		}
		return h.callInternalJSON(ctx, token, http.MethodPost, fmt.Sprintf("/api/v1/admin/acl/services/%s/groups", svc.ID), gin.H{
			"group_ids": []int64{group.ID},
		}, nil)

	case "revoke_user":
		svc, err := resolveProxyServiceByNameOrID(ctx, argString(args, "service", argString(args, "target", "")))
		if err != nil {
			return nil, err
		}
		user, err := resolveUserByNameOrID(ctx, argString(args, "user", argString(args, "subject", "")))
		if err != nil {
			return nil, err
		}
		return h.callInternalJSON(ctx, token, http.MethodDelete, fmt.Sprintf("/api/v1/admin/acl/services/%s/users/%d", svc.ID, user.ID), nil, nil)

	case "revoke_group":
		svc, err := resolveProxyServiceByNameOrID(ctx, argString(args, "service", argString(args, "target", "")))
		if err != nil {
			return nil, err
		}
		group, err := resolveGroupByNameOrID(ctx, argString(args, "group", argString(args, "subject", "")))
		if err != nil {
			return nil, err
		}
		return h.callInternalJSON(ctx, token, http.MethodDelete, fmt.Sprintf("/api/v1/admin/acl/services/%s/groups/%d", svc.ID, group.ID), nil, nil)

	case "grant_group_acl":
		targetGroup, err := resolveGroupByNameOrID(ctx, argString(args, "target", ""))
		if err != nil {
			return nil, fmt.Errorf("定位目标分组失败: %w", err)
		}
		if userStr := argString(args, "user", ""); userStr != "" {
			user, err := resolveUserByNameOrID(ctx, userStr)
			if err != nil {
				return nil, err
			}
			return h.callInternalJSON(ctx, token, http.MethodPost, fmt.Sprintf("/api/v1/admin/acl/groups/%d/users", targetGroup.ID), gin.H{"user_ids": []uint64{user.ID}}, nil)
		}
		srcGroup, err := resolveGroupByNameOrID(ctx, argString(args, "group", argString(args, "subject", "")))
		if err != nil {
			return nil, err
		}
		return h.callInternalJSON(ctx, token, http.MethodPost, fmt.Sprintf("/api/v1/admin/acl/groups/%d/groups", targetGroup.ID), gin.H{"group_ids": []int64{srcGroup.ID}}, nil)

	default:
		return nil, fmt.Errorf("signal_service_acl 不支持的 action: %s", action)
	}
}

// ==================== B4. 用户权限全景盘点、建号加组与令牌签发 ====================

func (h *Handler) handleUserInspect(ctx context.Context, token string, args map[string]interface{}) (interface{}, error) {
	action := strings.ToLower(strings.TrimSpace(argString(args, "action", "inspect_permissions")))
	switch action {
	case "inspect_permissions":
		user, err := resolveUserByNameOrID(ctx, argString(args, "user", ""))
		if err != nil {
			return nil, err
		}
		// 1. 所属分组
		var memberships []model.GroupMember
		db.DB.WithContext(ctx).Preload("Group").Where("user_id = ?", user.ID).Find(&memberships)
		groupIDs := make([]int64, 0, len(memberships))
		groupNames := make([]string, 0, len(memberships))
		for _, m := range memberships {
			groupIDs = append(groupIDs, m.GroupID)
			if m.Group != nil {
				groupNames = append(groupNames, m.Group.Name)
			}
		}

		// 2. 直接 SSH 权限 + 组继承 SSH 权限
		var directSSH []model.AclSSHUserPermission
		db.DB.WithContext(ctx).Preload("TargetUser").Where("user_id = ? AND enabled = ?", user.ID, true).Find(&directSSH)
		var groupSSH []model.AclSSHGroupPermission
		if len(groupIDs) > 0 {
			db.DB.WithContext(ctx).Preload("TargetUser").Preload("Group").Where("group_id IN ? AND enabled = ?", groupIDs, true).Find(&groupSSH)
		}
		sshTargets := make([]gin.H, 0, len(directSSH)+len(groupSSH))
		for _, p := range directSSH {
			targetName := ""
			sshEnabled := false
			if p.TargetUser != nil {
				targetName = p.TargetUser.Name
				sshEnabled = p.TargetUser.SSHEnabled
			}
			sshTargets = append(sshTargets, gin.H{
				"source":             "direct_user",
				"target_user_id":     p.TargetUserID,
				"target_name":        targetName,
				"target_ssh_enabled": sshEnabled,
				"ssh_users":          p.SSHUsers,
			})
		}
		for _, p := range groupSSH {
			targetName := ""
			sshEnabled := false
			if p.TargetUser != nil {
				targetName = p.TargetUser.Name
				sshEnabled = p.TargetUser.SSHEnabled
			}
			viaGroup := ""
			if p.Group != nil {
				viaGroup = p.Group.Name
			}
			sshTargets = append(sshTargets, gin.H{
				"source":             "group:" + viaGroup,
				"target_user_id":     p.TargetUserID,
				"target_name":        targetName,
				"target_ssh_enabled": sshEnabled,
				"ssh_users":          p.SSHUsers,
			})
		}

		// 3. K8S 权限
		var directK8S []model.AclK8SUserPermission
		db.DB.WithContext(ctx).Preload("TargetUser").Where("user_id = ? AND enabled = ?", user.ID, true).Find(&directK8S)
		var groupK8S []model.AclK8SGroupPermission
		if len(groupIDs) > 0 {
			db.DB.WithContext(ctx).Preload("TargetUser").Preload("Group").Where("group_id IN ? AND enabled = ?", groupIDs, true).Find(&groupK8S)
		}
		k8sTargets := make([]gin.H, 0, len(directK8S)+len(groupK8S))
		for _, p := range directK8S {
			targetName := ""
			if p.TargetUser != nil {
				targetName = p.TargetUser.Name
			}
			k8sTargets = append(k8sTargets, gin.H{
				"source":         "direct_user",
				"target_user_id": p.TargetUserID,
				"target_name":    targetName,
				"k8s_groups":     p.K8SGroups,
				"namespaces":     p.Namespaces,
			})
		}
		for _, p := range groupK8S {
			targetName := ""
			if p.TargetUser != nil {
				targetName = p.TargetUser.Name
			}
			viaGroup := ""
			if p.Group != nil {
				viaGroup = p.Group.Name
			}
			k8sTargets = append(k8sTargets, gin.H{
				"source":         "group:" + viaGroup,
				"target_user_id": p.TargetUserID,
				"target_name":    targetName,
				"k8s_groups":     p.K8SGroups,
				"namespaces":     p.Namespaces,
			})
		}

		// 4. 内网服务权限
		var directSvc []model.AclServiceUserPermission
		db.DB.WithContext(ctx).Preload("Service").Where("user_id = ?", user.ID).Find(&directSvc)
		var groupSvc []model.AclServiceGroupPermission
		if len(groupIDs) > 0 {
			db.DB.WithContext(ctx).Preload("Service").Preload("Group").Where("group_id IN ?", groupIDs).Find(&groupSvc)
		}
		svcTargets := make([]gin.H, 0, len(directSvc)+len(groupSvc))
		for _, p := range directSvc {
			svcName := ""
			if p.Service != nil {
				svcName = p.Service.Name
			}
			svcTargets = append(svcTargets, gin.H{"source": "direct_user", "service_id": p.ServiceID, "service_name": svcName})
		}
		for _, p := range groupSvc {
			svcName := ""
			if p.Service != nil {
				svcName = p.Service.Name
			}
			viaGroup := ""
			if p.Group != nil {
				viaGroup = p.Group.Name
			}
			svcTargets = append(svcTargets, gin.H{"source": "group:" + viaGroup, "service_id": p.ServiceID, "service_name": svcName})
		}

		// 5. 若传入了 check_target，自动给出连通性排障结论
		var diagnosis interface{}
		if checkTargetStr := strings.TrimSpace(argString(args, "check_target", "")); checkTargetStr != "" {
			targetNode, err := resolveUserByNameOrID(ctx, checkTargetStr)
			if err != nil {
				diagnosis = gin.H{"target": checkTargetStr, "reachable": false, "reason": "目标机器不存在"}
			} else {
				matched := false
				for _, item := range sshTargets {
					if item["target_user_id"] == targetNode.ID {
						matched = true
						break
					}
				}
				switch {
				case !user.Enabled:
					diagnosis = gin.H{"target": targetNode.Name, "reachable": false, "reason": "用户账号已被禁用 (enabled=false)"}
				case targetNode.Role == model.UserRoleAgent && !targetNode.SSHEnabled:
					diagnosis = gin.H{"target": targetNode.Name, "reachable": false, "reason": "目标机器尚未开启 SSH 能力 (ssh_enabled=false)"}
				case !matched:
					diagnosis = gin.H{"target": targetNode.Name, "reachable": false, "reason": "该用户及其所属分组均未被授予此机器的 SSH 权限"}
				default:
					diagnosis = gin.H{"target": targetNode.Name, "reachable": true, "reason": "用户状态、目标机器 SSH 开关与 ACL 授权均正常"}
				}
			}
		}

		return gin.H{
			"user": gin.H{
				"id":          user.ID,
				"name":        user.Name,
				"alias":       user.Alias,
				"role":        user.Role,
				"enabled":     user.Enabled,
				"ssh_enabled": user.SSHEnabled,
			},
			"groups":              groupNames,
			"ssh_permissions":     sshTargets,
			"k8s_permissions":     k8sTargets,
			"service_permissions": svcTargets,
			"diagnosis":           diagnosis,
		}, nil

	case "list_users":
		search := argString(args, "search", "")
		role := argString(args, "role", "")
		path := "/api/v1/admin/users?page=1&size=100"
		if search != "" {
			path += "&search=" + url.QueryEscape(search)
		}
		if role != "" {
			path += "&role=" + url.QueryEscape(role)
		}
		return h.callInternalJSON(ctx, token, http.MethodGet, path, nil, nil)

	case "create_user":
		name := argString(args, "name", argString(args, "user", ""))
		role := argString(args, "role", "client")
		alias := argString(args, "alias", name)
		res, err := h.callInternalJSON(ctx, token, http.MethodPost, "/api/v1/admin/users", gin.H{
			"name":  name,
			"alias": alias,
			"role":  role,
		}, nil)
		if err != nil {
			return nil, err
		}
		if groupStr := argString(args, "group", ""); groupStr != "" {
			if createdUser, uErr := resolveUserByNameOrID(ctx, name); uErr == nil {
				if group, gErr := resolveGroupByNameOrID(ctx, groupStr); gErr == nil {
					_, _ = h.callInternalJSON(ctx, token, http.MethodPost, fmt.Sprintf("/api/v1/admin/groups/%d/members", group.ID), gin.H{
						"user_ids": []uint64{createdUser.ID},
					}, nil)
				}
			}
		}
		return res, nil

	case "add_to_group":
		user, err := resolveUserByNameOrID(ctx, argString(args, "user", ""))
		if err != nil {
			return nil, err
		}
		group, err := resolveGroupByNameOrID(ctx, argString(args, "group", ""))
		if err != nil {
			return nil, err
		}
		return h.callInternalJSON(ctx, token, http.MethodPost, fmt.Sprintf("/api/v1/admin/groups/%d/members", group.ID), gin.H{
			"user_ids": []uint64{user.ID},
		}, nil)

	case "remove_from_group":
		user, err := resolveUserByNameOrID(ctx, argString(args, "user", ""))
		if err != nil {
			return nil, err
		}
		group, err := resolveGroupByNameOrID(ctx, argString(args, "group", ""))
		if err != nil {
			return nil, err
		}
		return h.callInternalJSON(ctx, token, http.MethodDelete, fmt.Sprintf("/api/v1/admin/groups/%d/members/%d", group.ID, user.ID), nil, nil)

	case "issue_deploy_token":
		user, err := resolveUserByNameOrID(ctx, argString(args, "user", ""))
		if err != nil {
			return nil, err
		}
		tokenName := argString(args, "token_name", "mcp-issued")
		return h.callInternalJSON(ctx, token, http.MethodPost, fmt.Sprintf("/api/v1/admin/users/%d/deploy-token", user.ID), gin.H{
			"name":              tokenName,
			"target_agent_name": argString(args, "target_agent_name", ""),
			"mode":              argString(args, "mode", ""),
		}, nil)

	default:
		return nil, fmt.Errorf("signal_user_inspect 不支持的 action: %s", action)
	}
}

// ==================== B5. 节点能力、域名与 Signal Tunnel 专用隧道 ====================

func (h *Handler) handleNodeTunnel(ctx context.Context, token string, args map[string]interface{}) (interface{}, error) {
	action := strings.ToLower(strings.TrimSpace(argString(args, "action", "list_nodes")))
	switch action {
	case "list_nodes":
		search := argString(args, "search", "")
		path := "/api/v1/admin/nodes?page=1&size=100"
		if search != "" {
			path += "&search=" + url.QueryEscape(search)
		}
		return h.callInternalJSON(ctx, token, http.MethodGet, path, nil, nil)

	case "enable_ssh":
		target, err := resolveUserByNameOrID(ctx, argString(args, "target", ""))
		if err != nil {
			return nil, err
		}
		enabled := argBool(args, "enabled", true)
		return h.callInternalJSON(ctx, token, http.MethodPut, fmt.Sprintf("/api/v1/admin/users/%d/ssh", target.ID), gin.H{"enabled": enabled}, nil)

	case "list_tunnels":
		return h.callInternalJSON(ctx, token, http.MethodGet, "/api/v1/admin/tunnels", nil, nil)

	case "available_agents":
		return h.callInternalJSON(ctx, token, http.MethodGet, "/api/v1/admin/tunnels/available-agents", nil, nil)

	case "inspect_tunnel":
		tok, err := resolveTunnelByNameOrID(ctx, argString(args, "tunnel", argString(args, "tunnel_id", "")))
		if err != nil {
			return nil, err
		}
		detail, err := h.callInternalJSON(ctx, token, http.MethodGet, fmt.Sprintf("/api/v1/admin/tunnels/%d", tok.ID), nil, nil)
		if err != nil {
			return nil, err
		}
		candidates, _ := h.callInternalJSON(ctx, token, http.MethodGet, fmt.Sprintf("/api/v1/admin/tunnels/%d/candidate-services", tok.ID), nil, nil)
		return gin.H{
			"tunnel_id":          tok.ID,
			"tunnel_name":        tok.Name,
			"target_agent":       tok.TargetAgentName,
			"detail":             detail,
			"candidate_services": candidates,
		}, nil

	case "candidate_services":
		tok, err := resolveTunnelByNameOrID(ctx, argString(args, "tunnel", argString(args, "tunnel_id", "")))
		if err != nil {
			return nil, err
		}
		return h.callInternalJSON(ctx, token, http.MethodGet, fmt.Sprintf("/api/v1/admin/tunnels/%d/candidate-services", tok.ID), nil, nil)

	case "bind_tunnel_service":
		tok, err := resolveTunnelByNameOrID(ctx, argString(args, "tunnel", argString(args, "tunnel_id", "")))
		if err != nil {
			return nil, err
		}
		serviceQuery := strings.TrimSpace(argString(args, "service", ""))
		namespaceFilter := strings.TrimSpace(argString(args, "namespace", ""))
		if strings.Contains(serviceQuery, "/") && namespaceFilter == "" {
			parts := strings.SplitN(serviceQuery, "/", 2)
			namespaceFilter = parts[0]
			serviceQuery = parts[1]
		}
		targetPortFilter := int(argInt64(args, "target_port", 0))
		localPort := int(argInt64(args, "local_port", -1))

		// 1. 读取当前已配置的端口白名单，保留所有其他已暴露服务端口（防误覆盖）
		k8sAPIEnabled := false
		k8sAPIPort := 6443
		existingMap := make(map[string]int)
		existingOrder := make([]string, 0)
		if tok.PortsConfig != "" {
			if cfg, err := service.ParseTunnelPortsConfig(tok.PortsConfig); err == nil {
				k8sAPIEnabled = cfg.K8sAPIEnabled
				if cfg.K8sAPIPort > 0 {
					k8sAPIPort = cfg.K8sAPIPort
				}
				for _, p := range cfg.Ports {
					existingMap[p.ResourceID] = int(p.LocalPort)
					existingOrder = append(existingOrder, p.ResourceID)
				}
			}
		}
		if _, hasK8sEnable := args["k8s_api_enabled"]; hasK8sEnable {
			k8sAPIEnabled = argBool(args, "k8s_api_enabled", k8sAPIEnabled)
		}
		if customK8sPort := int(argInt64(args, "k8s_api_port", 0)); customK8sPort > 0 {
			k8sAPIPort = customK8sPort
		}

		// 2. 若指定了 service，从目标 Agent 候选服务集中按名称/Namespace/端口匹配 ResourceID 并增量更新 local_port
		var matchedCandidate *service.TunnelCandidateService
		if serviceQuery != "" {
			candidates, err := service.QueryTunnelCandidateServices(ctx, db.DB, tok.TargetAgentName, time.Now())
			if err != nil {
				return nil, fmt.Errorf("查询目标 Agent (%s) 候选服务失败: %w", tok.TargetAgentName, err)
			}
			for i := range candidates {
				cand := &candidates[i]
				if namespaceFilter != "" && !strings.EqualFold(cand.Namespace, namespaceFilter) {
					continue
				}
				if targetPortFilter > 0 && int(cand.PortNumber) != targetPortFilter {
					continue
				}
				if cand.ResourceID == serviceQuery || strings.EqualFold(cand.ServiceName, serviceQuery) {
					matchedCandidate = cand
					break
				}
			}
			if matchedCandidate == nil {
				for i := range candidates {
					cand := &candidates[i]
					if namespaceFilter != "" && !strings.EqualFold(cand.Namespace, namespaceFilter) {
						continue
					}
					if targetPortFilter > 0 && int(cand.PortNumber) != targetPortFilter {
						continue
					}
					if strings.Contains(strings.ToLower(cand.ServiceName), strings.ToLower(serviceQuery)) {
						matchedCandidate = cand
						break
					}
				}
			}
			if matchedCandidate == nil {
				return nil, fmt.Errorf("在隧道 %s (Agent=%s) 的候选服务中找不到匹配 service=%q (namespace=%q, target_port=%d) 的服务", tok.Name, tok.TargetAgentName, serviceQuery, namespaceFilter, targetPortFilter)
			}
			if localPort < 0 {
				return nil, errors.New("必须提供 local_port（大于 1024 表示暴露端口，0 表示解绑关闭该服务端口）")
			}
			if _, exists := existingMap[matchedCandidate.ResourceID]; !exists {
				existingOrder = append(existingOrder, matchedCandidate.ResourceID)
			}
			existingMap[matchedCandidate.ResourceID] = localPort
		}

		portsPayload := make([]gin.H, 0, len(existingOrder))
		for _, resID := range existingOrder {
			lp := existingMap[resID]
			if lp > 0 {
				portsPayload = append(portsPayload, gin.H{
					"resource_id": resID,
					"local_port":  lp,
				})
			}
		}

		putBody := gin.H{
			"k8s_api_enabled": k8sAPIEnabled,
			"k8s_api_port":    k8sAPIPort,
			"ports":           portsPayload,
		}
		if _, err := h.callInternalJSON(ctx, token, http.MethodPut, fmt.Sprintf("/api/v1/admin/tunnels/%d/ports", tok.ID), putBody, nil); err != nil {
			return nil, err
		}
		updatedDetail, _ := h.callInternalJSON(ctx, token, http.MethodGet, fmt.Sprintf("/api/v1/admin/tunnels/%d", tok.ID), nil, nil)
		return gin.H{
			"business":          "bind_tunnel_service",
			"tunnel_id":         tok.ID,
			"tunnel_name":       tok.Name,
			"target_agent":      tok.TargetAgentName,
			"matched_candidate": matchedCandidate,
			"local_port":        localPort,
			"k8s_api_enabled":   k8sAPIEnabled,
			"k8s_api_port":      k8sAPIPort,
			"tunnel_detail":     updatedDetail,
		}, nil

	case "create_tunnel":
		if body, ok := args["body"]; ok && body != nil {
			return h.callInternalJSON(ctx, token, http.MethodPost, "/api/v1/admin/tunnels", body, nil)
		}
		return h.callInternalJSON(ctx, token, http.MethodPost, "/api/v1/admin/tunnels", gin.H{
			"name":         argString(args, "name", argString(args, "tunnel", "")),
			"target_agent": argString(args, "target_agent", argString(args, "target", "")),
		}, nil)

	case "update_tunnel_ports":
		tok, err := resolveTunnelByNameOrID(ctx, argString(args, "tunnel", argString(args, "tunnel_id", "")))
		if err != nil {
			return nil, err
		}
		return h.callInternalJSON(ctx, token, http.MethodPut, fmt.Sprintf("/api/v1/admin/tunnels/%d/ports", tok.ID), args["body"], nil)

	case "delete_tunnel":
		tok, err := resolveTunnelByNameOrID(ctx, argString(args, "tunnel", argString(args, "tunnel_id", "")))
		if err != nil {
			return nil, err
		}
		return h.callInternalJSON(ctx, token, http.MethodDelete, fmt.Sprintf("/api/v1/admin/tunnels/%d", tok.ID), nil, nil)

	case "refresh_domains":
		return h.callInternalJSON(ctx, token, http.MethodPost, "/api/v1/admin/domains/refresh", nil, nil)

	case "sync_acl":
		return h.callInternalJSON(ctx, token, http.MethodPost, "/api/v1/admin/tunnel/acl/sync", nil, nil)

	default:
		return nil, fmt.Errorf("signal_node_tunnel 不支持的 action: %s", action)
	}
}

// ==================== B6. 三工作域多租户治理 (Platform / Provider / Tenant) ====================

func (h *Handler) handleWorkspaceV2(ctx context.Context, token string, reqID interface{}, args map[string]interface{}) (interface{}, error) {
	action := strings.ToLower(strings.TrimSpace(argString(args, "action", "contexts")))
	if action == "contexts" {
		return h.callInternalJSON(ctx, token, http.MethodGet, "/api/v1/management/contexts", nil, nil)
	}

	scopeType := strings.ToLower(strings.TrimSpace(argString(args, "scope_type", "platform")))
	scopeInput := strings.TrimSpace(argString(args, "scope", ""))
	scopeID, err := resolveManagementScopeID(ctx, scopeType, scopeInput)
	if err != nil {
		return nil, err
	}

	headers := map[string]string{
		"X-Management-Scope-Type": scopeType,
	}
	if scopeID != "" {
		headers["X-Management-Scope-ID"] = scopeID
	}

	method := strings.ToUpper(strings.TrimSpace(argString(args, "method", http.MethodGet)))
	path := strings.TrimSpace(argString(args, "path", "/api/v1/management/contexts/current"))
	if !strings.HasPrefix(path, "/api/") {
		path = "/api/v1/management/" + strings.TrimPrefix(path, "/")
	}
	path = strings.ReplaceAll(path, ":tenant_id", scopeID)
	path = strings.ReplaceAll(path, "{tenant_id}", scopeID)

	if method != http.MethodGet && method != http.MethodHead {
		headers["Idempotency-Key"] = fmt.Sprintf("mcp-%v-%s", reqID, uuid.NewString())
		if ifMatch := argString(args, "if_match", ""); ifMatch != "" {
			headers["If-Match"] = ifMatch
		} else if getPath := deriveResourceGetPath(path); getPath != "" {
			if etag := h.probeResourceETag(ctx, token, getPath, headers); etag != "" {
				headers["If-Match"] = etag
			}
		}
	}

	return h.callInternalJSON(ctx, token, method, path, args["body"], headers)
}

func (h *Handler) handleRawAPI(ctx context.Context, token string, reqID interface{}, args map[string]interface{}) (interface{}, error) {
	method := strings.ToUpper(strings.TrimSpace(argString(args, "method", http.MethodGet)))
	path := strings.TrimSpace(argString(args, "path", "/api/v1/admin/auth/me"))
	headers := map[string]string{}
	if method != http.MethodGet && method != http.MethodHead {
		headers["Idempotency-Key"] = fmt.Sprintf("mcp-raw-%v-%s", reqID, uuid.NewString())
	}
	return h.callInternalJSON(ctx, token, method, path, args["body"], headers)
}

// ==================== 进程内路由调用与按名称智能寻址辅助函数 ====================

func (h *Handler) callInternalJSON(ctx context.Context, token, method, path string, body interface{}, extraHeaders map[string]string) (interface{}, error) {
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)

	var parsed interface{}
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	}
	if rec.Code >= 400 {
		return nil, fmt.Errorf("HTTP %d (%s %s): %s", rec.Code, method, path, strings.TrimSpace(rec.Body.String()))
	}
	return parsed, nil
}

func (h *Handler) probeResourceETag(ctx context.Context, token, getPath string, headers map[string]string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, getPath, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return strings.TrimSpace(rec.Header().Get("ETag"))
}

func deriveResourceGetPath(mutatePath string) string {
	idx := strings.Index(mutatePath, "?")
	clean := mutatePath
	if idx >= 0 {
		clean = mutatePath[:idx]
	}
	actionSuffixes := []string{
		"/suspend", "/resume", "/revoke", "/activate", "/schedule", "/renew",
		"/accept", "/reject", "/retire", "/disable", "/maintenance",
		"/mark-allocatable", "/publish", "/hide", "/show", "/terminate",
		"/domain-label", "/host-domain-label", "/display-name", "/config",
	}
	for _, suffix := range actionSuffixes {
		if strings.HasSuffix(clean, suffix) {
			return strings.TrimSuffix(clean, suffix)
		}
	}
	return clean
}

func resolveUserByNameOrID(ctx context.Context, identifier string) (*model.User, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, errors.New("未提供用户或节点名称/ID")
	}
	var user model.User
	if id, err := strconv.ParseUint(identifier, 10, 64); err == nil {
		if err := db.DB.WithContext(ctx).First(&user, id).Error; err == nil {
			return &user, nil
		}
	}
	if err := db.DB.WithContext(ctx).Where("name = ? OR alias = ?", identifier, identifier).First(&user).Error; err == nil {
		return &user, nil
	}
	if err := db.DB.WithContext(ctx).Where("name LIKE ? OR alias LIKE ?", "%"+identifier+"%", "%"+identifier+"%").First(&user).Error; err == nil {
		return &user, nil
	}
	return nil, fmt.Errorf("找不到名称或别名为 %q 的用户/节点", identifier)
}

func resolveGroupByNameOrID(ctx context.Context, identifier string) (*model.Group, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, errors.New("未提供分组名称/ID")
	}
	var group model.Group
	if id, err := strconv.ParseInt(identifier, 10, 64); err == nil {
		if err := db.DB.WithContext(ctx).First(&group, id).Error; err == nil {
			return &group, nil
		}
	}
	if err := db.DB.WithContext(ctx).Where("name = ? OR alias = ?", identifier, identifier).First(&group).Error; err == nil {
		return &group, nil
	}
	if err := db.DB.WithContext(ctx).Where("name LIKE ? OR alias LIKE ?", "%"+identifier+"%", "%"+identifier+"%").First(&group).Error; err == nil {
		return &group, nil
	}
	return nil, fmt.Errorf("找不到名称或别名为 %q 的分组", identifier)
}

func resolveProxyServiceByNameOrID(ctx context.Context, identifier string) (*model.ProxyService, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, errors.New("未提供代理服务名称/ID")
	}
	var svc model.ProxyService
	if err := db.DB.WithContext(ctx).Where("id = ? OR name = ? OR alias = ?", identifier, identifier, identifier).First(&svc).Error; err == nil {
		return &svc, nil
	}
	if err := db.DB.WithContext(ctx).Where("name LIKE ? OR alias LIKE ?", "%"+identifier+"%", "%"+identifier+"%").First(&svc).Error; err == nil {
		return &svc, nil
	}
	return nil, fmt.Errorf("找不到名称或别名为 %q 的内网代理服务", identifier)
}

func resolveEndpointByNameOrID(ctx context.Context, identifier string) (*model.Endpoint, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, errors.New("未提供 Endpoint 名称/ID")
	}
	var ep model.Endpoint
	if err := db.DB.WithContext(ctx).Where("id = ? OR name = ? OR alias = ?", identifier, identifier, identifier).First(&ep).Error; err == nil {
		return &ep, nil
	}
	return nil, fmt.Errorf("找不到名称或别名为 %q 的 Endpoint", identifier)
}

func resolveTunnelByNameOrID(ctx context.Context, identifier string) (*model.DeployToken, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, errors.New("未提供 Signal Tunnel 名称、目标 Agent 名或 ID（例如 tunnel-gpu-5090）")
	}
	var tok model.DeployToken
	base := db.DB.WithContext(ctx).Model(&model.DeployToken{}).Scopes(model.ActiveTunnelTokenScope)
	if id, err := strconv.ParseUint(identifier, 10, 64); err == nil {
		if err := base.Where("id = ?", id).First(&tok).Error; err == nil {
			return &tok, nil
		}
	}
	if err := db.DB.WithContext(ctx).Model(&model.DeployToken{}).Scopes(model.ActiveTunnelTokenScope).
		Where("name = ? OR target_agent_name = ?", identifier, identifier).First(&tok).Error; err == nil {
		return &tok, nil
	}
	if err := db.DB.WithContext(ctx).Model(&model.DeployToken{}).Scopes(model.ActiveTunnelTokenScope).
		Where("name LIKE ? OR target_agent_name LIKE ?", "%"+identifier+"%", "%"+identifier+"%").First(&tok).Error; err == nil {
		return &tok, nil
	}
	return nil, fmt.Errorf("找不到名称或目标 Agent 为 %q 的 Signal Tunnel 隧道实例", identifier)
}

func resolveManagementScopeID(ctx context.Context, scopeType, identifier string) (string, error) {
	if scopeType == "platform" || identifier == "" {
		return "", nil
	}
	switch scopeType {
	case "provider":
		var p model.ResourceProvider
		if err := db.DB.WithContext(ctx).Where("id = ? OR key = ? OR display_name = ?", identifier, identifier, identifier).First(&p).Error; err == nil {
			return p.ID, nil
		}
		return "", fmt.Errorf("找不到名称或 key 为 %q 的资源提供方 (Provider)", identifier)
	case "tenant":
		var t model.Tenant
		if err := db.DB.WithContext(ctx).Where("id = ? OR key = ? OR name = ?", identifier, identifier, identifier).First(&t).Error; err == nil {
			return t.ID, nil
		}
		return "", fmt.Errorf("找不到名称或 key 为 %q 的消费租户 (Tenant)", identifier)
	default:
		return identifier, nil
	}
}

func (h *Handler) toolDefinitions() []gin.H {
	return []gin.H{
		{
			"name":        "signal_ssh_acl",
			"description": "B1. SSH 登录权限业务工具：支持直接按用户名/分组名与机器名查询、单独授权或撤销某台机器的 SSH 权限（自动检查并开启目标机器 ssh_enabled）",
			"inputSchema": gin.H{
				"type": "object",
				"properties": gin.H{
					"action":          gin.H{"type": "string", "enum": []string{"list", "inspect_target", "grant_user", "grant_group", "revoke_user", "revoke_group"}},
					"target":          gin.H{"type": "string", "description": "目标机器名称、别名或 ID（如 bj-node-01）"},
					"user":            gin.H{"type": "string", "description": "被授权或撤销的用户名、别名或 ID（如 zhangsan）"},
					"group":           gin.H{"type": "string", "description": "被授权或撤销的分组名、别名或 ID（如 ai-team）"},
					"ssh_users":       gin.H{"type": "array", "items": gin.H{"type": "string"}, "description": "允许登录的 Linux 系统账号列表，默认 [\"root\"]"},
					"auto_enable_ssh": gin.H{"type": "boolean", "description": "若目标机器未开启 SSH，是否自动开启，默认 true"},
				},
				"required": []string{"action"},
			},
		},
		{
			"name":        "signal_k8s_acl",
			"description": "B2. K8S 集群 API 权限业务工具：按用户名/分组名与集群/Endpoint 名开通、撤销或查询 kubectl 访问权限",
			"inputSchema": gin.H{
				"type": "object",
				"properties": gin.H{
					"action":      gin.H{"type": "string", "enum": []string{"list", "inspect", "grant_user", "grant_group", "revoke_user", "revoke_group"}},
					"target_kind": gin.H{"type": "string", "enum": []string{"cluster", "endpoint"}, "description": "cluster(Agent 集群) 或 endpoint(内网 Endpoint K8SAPI)"},
					"target":      gin.H{"type": "string", "description": "目标集群或 Endpoint 的名称、别名或 ID"},
					"user":        gin.H{"type": "string", "description": "用户名、别名或 ID"},
					"group":       gin.H{"type": "string", "description": "分组名、别名或 ID"},
					"k8s_groups":  gin.H{"type": "array", "items": gin.H{"type": "string"}, "description": "K8S RBAC 组，默认 [\"system:masters\"]"},
					"namespaces":  gin.H{"type": "array", "items": gin.H{"type": "string"}, "description": "允许的命名空间列表（空表示全部）"},
				},
				"required": []string{"action"},
			},
		},
		{
			"name":        "signal_service_acl",
			"description": "B3. 内网代理服务（ProxyService）与分组互通授权工具：按服务名与用户名/分组名开通或撤销内网服务访问权限",
			"inputSchema": gin.H{
				"type": "object",
				"properties": gin.H{
					"action":  gin.H{"type": "string", "enum": []string{"list", "inspect", "grant_user", "grant_group", "revoke_user", "revoke_group", "grant_group_acl"}},
					"service": gin.H{"type": "string", "description": "内网代理服务名称、别名或 ID（如 gitlab-web）"},
					"target":  gin.H{"type": "string", "description": "目标服务或目标分组名称"},
					"user":    gin.H{"type": "string", "description": "用户名、别名或 ID"},
					"group":   gin.H{"type": "string", "description": "分组名、别名或 ID"},
				},
				"required": []string{"action"},
			},
		},
		{
			"name":        "signal_user_inspect",
			"description": "B4. 用户权限全景盘点、连通性排障、创建用户加组及签发 DeployToken 安装命令工具",
			"inputSchema": gin.H{
				"type": "object",
				"properties": gin.H{
					"action":       gin.H{"type": "string", "enum": []string{"inspect_permissions", "list_users", "create_user", "add_to_group", "remove_from_group", "issue_deploy_token"}},
					"user":         gin.H{"type": "string", "description": "用户名、别名或 ID"},
					"check_target": gin.H{"type": "string", "description": "可选：指定要诊断连通性的目标机器名称"},
					"role":         gin.H{"type": "string", "enum": []string{"client", "agent"}},
					"alias":        gin.H{"type": "string"},
					"group":        gin.H{"type": "string", "description": "分组名称或 ID"},
					"token_name":   gin.H{"type": "string"},
				},
				"required": []string{"action"},
			},
		},
		{
			"name":        "signal_node_tunnel",
			"description": "B5. 节点能力开关、.beagle 域名刷新、Headscale ACL 同步与 Signal Tunnel 专用出站隧道管理工具（支持按隧道名如 tunnel-gpu-5090 与服务名如 studio 直接绑定/修改/解绑 local_port 端口白名单）",
			"inputSchema": gin.H{
				"type": "object",
				"properties": gin.H{
					"action":          gin.H{"type": "string", "enum": []string{"list_nodes", "enable_ssh", "list_tunnels", "inspect_tunnel", "available_agents", "candidate_services", "bind_tunnel_service", "create_tunnel", "update_tunnel_ports", "delete_tunnel", "refresh_domains", "sync_acl"}},
					"target":          gin.H{"type": "string", "description": "目标节点名称或 ID"},
					"tunnel":          gin.H{"type": "string", "description": "Signal Tunnel 名称、目标 Agent 名或 ID（如 tunnel-gpu-5090）"},
					"tunnel_id":       gin.H{"type": "string", "description": "Signal Tunnel ID（兼容参数）"},
					"service":         gin.H{"type": "string", "description": "要暴露的边缘容器服务名、namespace/service 或 resource_id（如 studio、verdantflare-studio、www、hub、login）"},
					"namespace":       gin.H{"type": "string", "description": "可选：限定匹配的目标 K8s Namespace（如 verdantflare-station）"},
					"target_port":     gin.H{"type": "integer", "description": "可选：限定匹配的边缘 Service 目标端口（如 8000、443）"},
					"local_port":      gin.H{"type": "integer", "description": "云端 Tunnel Pod 监听暴露端口白名单（如 18080、18030、18040、18050；传 0 表示解绑关闭该服务端口）"},
					"k8s_api_enabled": gin.H{"type": "boolean", "description": "可选：是否开启隧道内置 K8S API 特殊路径"},
					"k8s_api_port":    gin.H{"type": "integer", "description": "可选：隧道内置 K8S API 端口（默认 6443）"},
					"enabled":         gin.H{"type": "boolean"},
					"body":            gin.H{"type": "object"},
				},
				"required": []string{"action"},
			},
		},
		{
			"name":        "signal_workspace_v2",
			"description": "B6. 三工作域（Platform / Provider / Tenant）治理工具：自动将组织名称/key 转为 Scope UUID，自动补齐 If-Match 与 Idempotency-Key",
			"inputSchema": gin.H{
				"type": "object",
				"properties": gin.H{
					"action":     gin.H{"type": "string", "enum": []string{"contexts", "call"}},
					"scope_type": gin.H{"type": "string", "enum": []string{"platform", "provider", "tenant"}},
					"scope":      gin.H{"type": "string", "description": "Provider 或 Tenant 的名称、key 或 UUID"},
					"method":     gin.H{"type": "string", "enum": []string{"GET", "POST", "PATCH", "PUT", "DELETE"}},
					"path":       gin.H{"type": "string", "description": "管理 API 路径，如 /api/v1/management/platform/organizations"},
					"body":       gin.H{"type": "object"},
				},
				"required": []string{"action"},
			},
		},
		{
			"name":        "signal_raw_api",
			"description": "兜底通用 API 调用工具",
			"inputSchema": gin.H{
				"type": "object",
				"properties": gin.H{
					"method": gin.H{"type": "string"},
					"path":   gin.H{"type": "string"},
					"body":   gin.H{"type": "object"},
				},
				"required": []string{"method", "path"},
			},
		},
	}
}

func argString(args map[string]interface{}, key, fallback string) string {
	if v, ok := args[key]; ok && v != nil {
		switch s := v.(type) {
		case string:
			return s
		default:
			return fmt.Sprintf("%v", v)
		}
	}
	return fallback
}

func argBool(args map[string]interface{}, key string, fallback bool) bool {
	if v, ok := args[key]; ok && v != nil {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return fallback
}

func argInt64(args map[string]interface{}, key string, fallback int64) int64 {
	v, ok := args[key]
	if !ok || v == nil {
		return fallback
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func argStringSlice(args map[string]interface{}, key string) []string {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	switch arr := v.(type) {
	case []string:
		return arr
	case []interface{}:
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			if s := strings.TrimSpace(fmt.Sprintf("%v", item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		out := []string{}
		for _, part := range strings.Split(arr, ",") {
			if s := strings.TrimSpace(part); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func claimInt64(val interface{}) int64 {
	switch v := val.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case uint64:
		return int64(v)
	default:
		return 0
	}
}
