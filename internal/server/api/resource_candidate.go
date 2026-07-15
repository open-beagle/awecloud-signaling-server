package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// ResourceCandidateAPI exposes the safe boundary between Agent discovery and
// published resources. Agent observations are never published implicitly.
type ResourceCandidateAPI struct{}

func NewResourceCandidateAPI() *ResourceCandidateAPI { return &ResourceCandidateAPI{} }

type resourceCandidateRequest struct {
	AgentNodeID    uint64            `json:"agent_node_id" binding:"required"`
	ProviderHint   string            `json:"provider_hint"`
	ClusterID      string            `json:"cluster_id"`
	Namespace      string            `json:"namespace" binding:"required"`
	PodName        string            `json:"pod_name"`
	PodUID         string            `json:"pod_uid" binding:"required"`
	ContainerName  string            `json:"container_name" binding:"required"`
	WorkspaceHint  string            `json:"workspace_hint"`
	GenerationHint int64             `json:"generation_hint"`
	Ready          bool              `json:"ready"`
	Labels         map[string]string `json:"labels"`
	LeaseSeconds   int               `json:"lease_seconds"`
}

type resourceCandidateListItem struct {
	model.DiscoveryCandidate
	AgentName string `json:"agent_name,omitempty"`
}

// List returns runtime candidates. A Tenant filter is intentionally not
// accepted because candidates have no trusted tenant assignment yet.
func (a *ResourceCandidateAPI) List(c *gin.Context) {
	ctx := c.Request.Context()
	page, size := pageParams(c)
	// Lease expiry is derived from the last Agent observation. Explicit
	// conflict/rejected decisions are preserved for operator review.
	db.DB.WithContext(ctx).Model(&model.DiscoveryCandidate{}).
		Where("lease_expires_at IS NOT NULL AND lease_expires_at < ? AND status IN ?", time.Now(), []model.DiscoveryCandidateStatus{
			model.DiscoveryCandidateObserved, model.DiscoveryCandidatePendingClaim,
		}).Updates(map[string]interface{}{"status": model.DiscoveryCandidateStale})
	query := db.DB.WithContext(ctx).Model(&model.DiscoveryCandidate{})
	if state := strings.TrimSpace(c.Query("status")); state != "" {
		query = query.Where("status = ?", state)
	}
	if agentID := strings.TrimSpace(c.Query("agent_node_id")); agentID != "" {
		query = query.Where("agent_node_id = ?", agentID)
	}
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		like := "%" + search + "%"
		query = query.Where("namespace LIKE ? OR pod_name LIKE ? OR pod_uid LIKE ? OR workspace_hint LIKE ? OR provider_hint LIKE ?", like, like, like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询发现候选失败"))
		return
	}
	var candidates []model.DiscoveryCandidate
	if err := query.Order("updated_at DESC").Offset((page - 1) * size).Limit(size).Find(&candidates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询发现候选失败"))
		return
	}
	items := make([]resourceCandidateListItem, 0, len(candidates))
	for _, candidate := range candidates {
		item := resourceCandidateListItem{DiscoveryCandidate: candidate}
		var node model.Node
		if err := db.DB.WithContext(ctx).First(&node, candidate.AgentNodeID).Error; err == nil {
			item.AgentName = node.Name
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, NewPagedResponse(items, total, page, size))
}

// Observe upserts one runtime observation. It only changes the candidate and
// cannot create or mutate a Resource.
func (a *ResourceCandidateAPI) Observe(c *gin.Context) {
	ctx := c.Request.Context()
	var req resourceCandidateRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.AgentNodeID == 0 || strings.TrimSpace(req.Namespace) == "" || strings.TrimSpace(req.PodUID) == "" || strings.TrimSpace(req.ContainerName) == "" {
		c.JSON(http.StatusBadRequest, NewErrorResponse("Agent、Namespace、Pod UID 和容器不能为空"))
		return
	}
	var node model.Node
	if err := db.DB.WithContext(ctx).Where("id = ? AND type = ?", req.AgentNodeID, model.NodeTypeAgent).First(&node).Error; err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("Agent 不存在或类型无效"))
		return
	}
	labels, err := json.Marshal(req.Labels)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("标签格式无效"))
		return
	}
	now := time.Now()
	lease := time.Duration(req.LeaseSeconds) * time.Second
	if req.LeaseSeconds <= 0 {
		lease = 2 * time.Minute
	}
	var candidate model.DiscoveryCandidate
	query := db.DB.WithContext(ctx).Where("agent_node_id = ? AND pod_uid = ? AND container_name = ?", req.AgentNodeID, req.PodUID, req.ContainerName)
	err = query.First(&candidate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		candidate = model.DiscoveryCandidate{ID: uuid.NewString(), AgentNodeID: req.AgentNodeID, PodUID: strings.TrimSpace(req.PodUID), ContainerName: strings.TrimSpace(req.ContainerName), Status: model.DiscoveryCandidateObserved}
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询发现候选失败"))
		return
	}
	// Rejected and conflict are explicit operator decisions. A heartbeat may
	// refresh evidence but must not silently clear those states.
	if candidate.Status == "" || candidate.Status == model.DiscoveryCandidateStale {
		candidate.Status = model.DiscoveryCandidateObserved
	}
	candidate.ClusterID = strings.TrimSpace(req.ClusterID)
	candidate.ProviderHint = strings.TrimSpace(req.ProviderHint)
	candidate.Namespace = strings.TrimSpace(req.Namespace)
	candidate.PodName = strings.TrimSpace(req.PodName)
	candidate.WorkspaceHint = strings.TrimSpace(req.WorkspaceHint)
	candidate.GenerationHint = req.GenerationHint
	candidate.Ready = req.Ready
	candidate.LabelSnapshot = string(labels)
	candidate.ObservedAt = now
	expires := now.Add(lease)
	candidate.LeaseExpiresAt = &expires
	if err := db.DB.WithContext(ctx).Save(&candidate).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("保存发现候选失败"))
		return
	}
	c.JSON(http.StatusOK, NewSuccessResponse(candidate))
}

// Reject marks a candidate as an explicit operator decision. It still does
// not delete the observation, preserving the audit trail and runtime key.
func (a *ResourceCandidateAPI) Reject(c *gin.Context) {
	ctx := c.Request.Context()
	var candidate model.DiscoveryCandidate
	if err := db.DB.WithContext(ctx).First(&candidate, "id = ?", c.Param("id")).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, NewErrorResponse("发现候选不存在"))
			return
		}
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询发现候选失败"))
		return
	}
	if candidate.Status == model.DiscoveryCandidatePublished {
		c.JSON(http.StatusConflict, NewErrorResponse("已发布候选不能直接拒绝，请在资源详情撤销资源"))
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, NewErrorResponse("拒绝原因格式无效"))
			return
		}
	}
	candidate.Status = model.DiscoveryCandidateRejected
	candidate.ConflictReason = strings.TrimSpace(body.Reason)
	if candidate.ConflictReason == "" {
		candidate.ConflictReason = "管理员拒绝"
	}
	if err := db.DB.WithContext(ctx).Save(&candidate).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("拒绝发现候选失败"))
		return
	}
	recordAuditLog(ctx, c, "reject_resource_candidate", "resource_candidate", candidate.ID, candidate.PodName, candidate)
	c.JSON(http.StatusOK, NewSuccessResponse(candidate))
}

// Reconcile matches an untrusted runtime observation to a trusted Workspace
// Binding. Unknown workspaces remain PendingClaim and never create a Resource.
func (a *ResourceCandidateAPI) Reconcile(c *gin.Context) {
	ctx := c.Request.Context()
	var candidate model.DiscoveryCandidate
	if err := db.DB.WithContext(ctx).First(&candidate, "id = ?", c.Param("id")).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, NewErrorResponse("发现候选不存在"))
			return
		}
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询发现候选失败"))
		return
	}
	if candidate.Status == model.DiscoveryCandidateRejected {
		c.JSON(http.StatusConflict, NewErrorResponse("已拒绝候选不能重新发布"))
		return
	}
	if candidate.Status == model.DiscoveryCandidatePublished && candidate.ResourceID != "" {
		var target model.ResourceTarget
		if err := db.DB.WithContext(ctx).Where("resource_id = ?", candidate.ResourceID).Order("revision DESC").First(&target).Error; err == nil && target.PodUID == candidate.PodUID && target.ContainerName == candidate.ContainerName && target.Ready == candidate.Ready {
			c.JSON(http.StatusOK, NewSuccessResponse(candidate))
			return
		}
	}
	if candidate.LeaseExpiresAt != nil && candidate.LeaseExpiresAt.Before(time.Now()) {
		candidate.Status = model.DiscoveryCandidateStale
		candidate.ConflictReason = "Agent 观测租约已过期"
		_ = db.DB.WithContext(ctx).Save(&candidate).Error
		c.JSON(http.StatusConflict, NewErrorResponse(candidate.ConflictReason))
		return
	}
	if strings.TrimSpace(candidate.ProviderHint) == "" || strings.TrimSpace(candidate.WorkspaceHint) == "" {
		candidate.Status = model.DiscoveryCandidatePendingClaim
		candidate.ConflictReason = "缺少可信 Provider 或 Workspace Hint"
		if err := db.DB.WithContext(ctx).Save(&candidate).Error; err != nil {
			c.JSON(http.StatusInternalServerError, NewErrorResponse("更新候选状态失败"))
			return
		}
		c.JSON(http.StatusAccepted, NewSuccessResponse(candidate))
		return
	}
	var binding model.WorkspaceBinding
	err := db.DB.WithContext(ctx).Where("provider_id = ? AND external_workspace_id = ?", candidate.ProviderHint, candidate.WorkspaceHint).First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		candidate.Status = model.DiscoveryCandidatePendingClaim
		candidate.ConflictReason = "未找到可信 Workspace Binding"
		if err := db.DB.WithContext(ctx).Save(&candidate).Error; err != nil {
			c.JSON(http.StatusInternalServerError, NewErrorResponse("更新候选状态失败"))
			return
		}
		c.JSON(http.StatusAccepted, NewSuccessResponse(candidate))
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询 Workspace Binding 失败"))
		return
	}
	if binding.Status != model.WorkspaceBindingActive {
		candidate.Status = model.DiscoveryCandidateConflict
		candidate.ConflictReason = "Workspace Binding 已停止或撤销"
		_ = db.DB.WithContext(ctx).Save(&candidate).Error
		c.JSON(http.StatusConflict, NewErrorResponse(candidate.ConflictReason))
		return
	}
	if candidate.GenerationHint != 0 && candidate.GenerationHint != binding.Generation {
		candidate.Status = model.DiscoveryCandidateConflict
		candidate.ConflictReason = "Workspace generation 与可信绑定不一致"
		_ = db.DB.WithContext(ctx).Save(&candidate).Error
		c.JSON(http.StatusConflict, NewErrorResponse(candidate.ConflictReason))
		return
	}
	var resource model.Resource
	if err := db.DB.WithContext(ctx).First(&resource, "id = ?", binding.ResourceID).Error; err != nil {
		candidate.Status = model.DiscoveryCandidateConflict
		candidate.ConflictReason = "Workspace Resource 不存在"
		_ = db.DB.WithContext(ctx).Save(&candidate).Error
		c.JSON(http.StatusConflict, NewErrorResponse(candidate.ConflictReason))
		return
	}
	target, status, message := saveResourceTarget(ctx, &resource, targetRequest{
		AgentNodeID: candidate.AgentNodeID, ClusterID: candidate.ClusterID, Namespace: candidate.Namespace,
		PodName: candidate.PodName, PodUID: candidate.PodUID, ContainerName: candidate.ContainerName, Ready: candidate.Ready,
	})
	if status != http.StatusCreated {
		candidate.Status = model.DiscoveryCandidateConflict
		candidate.ConflictReason = message
		_ = db.DB.WithContext(ctx).Save(&candidate).Error
		c.JSON(status, NewErrorResponse(message))
		return
	}
	candidate.Status = model.DiscoveryCandidatePublished
	candidate.ResourceID = binding.ResourceID
	candidate.ConflictReason = ""
	if err := db.DB.WithContext(ctx).Save(&candidate).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("发布候选失败"))
		return
	}
	recordAuditLog(ctx, c, "publish_resource_candidate", "resource", resource.ID, resource.DisplayName, map[string]interface{}{"candidate": candidate, "target": target})
	c.JSON(http.StatusOK, NewSuccessResponse(candidate))
}
