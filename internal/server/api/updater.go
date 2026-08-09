package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/mod/semver"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

type UpdaterAPI struct {
	updates *service.UpdateService
	catalog *service.UpdaterCatalogService
}

func (a *UpdaterAPI) SetCatalog(catalog *service.UpdaterCatalogService) {
	a.catalog = catalog
}

func (a *UpdaterAPI) SyncCatalog(c *gin.Context) {
	if a.catalog == nil {
		c.JSON(http.StatusServiceUnavailable, NewErrorResponse("HTTP 制品目录未配置"))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()
	result, err := a.catalog.Sync(ctx)
	if err != nil {
		logger.Errorf("同步 HTTP 制品与版本失败: %v", err)
		recordAuditLog(c.Request.Context(), c, "updater_catalog_sync_failed", "updater_catalog", "http", "HTTP 制品目录", gin.H{"result": result, "error": err.Error()})
		c.JSON(http.StatusConflict, Response{Success: false, Message: "HTTP 制品与版本同步存在失败项", Data: result})
		return
	}
	recordAuditLog(c.Request.Context(), c, "updater_catalog_sync", "updater_catalog", "http", "HTTP 制品目录", result)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("同步完成", result))
}

func NewUpdaterAPI() *UpdaterAPI {
	return &UpdaterAPI{updates: service.NewUpdateService(db.DB)}
}

type ArtifactInput struct {
	OS          string `json:"os" binding:"required"`
	Arch        string `json:"arch" binding:"required"`
	Role        string `json:"role"`
	PackageType string `json:"package_type"`
	Filename    string `json:"filename" binding:"required"`
	DownloadURL string `json:"download_url" binding:"required"`
	Size        int64  `json:"size" binding:"required"`
	SHA256      string `json:"sha256" binding:"required"`
}

type CreateReleaseRequest struct {
	Component           string          `json:"component" binding:"required"`
	Version             string          `json:"version" binding:"required"`
	CommitID            string          `json:"commit_id"`
	Channel             string          `json:"channel"`
	ReleaseNotes        string          `json:"release_notes"`
	MinSupportedVersion string          `json:"min_supported_version"`
	Artifacts           []ArtifactInput `json:"artifacts" binding:"required,min=1"`
}

func (a *UpdaterAPI) CreateRelease(c *gin.Context) {
	var req CreateReleaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("发布参数无效"))
		return
	}
	component := model.Component(strings.ToLower(req.Component))
	if !component.Valid() {
		c.JSON(http.StatusBadRequest, NewErrorResponse("不支持的组件类型"))
		return
	}
	if component == model.ComponentAgent {
		req.CommitID = strings.TrimSpace(req.CommitID)
		if !validAgentCommitID(req.CommitID) {
			c.JSON(http.StatusBadRequest, NewErrorResponse("Agent commit_id 必须是完整的 40 位小写 Git SHA"))
			return
		}
	}
	version, ok := normalizeVersion(req.Version)
	if !ok {
		c.JSON(http.StatusBadRequest, NewErrorResponse("版本号必须符合语义化版本规范"))
		return
	}
	minVersion := ""
	if req.MinSupportedVersion != "" {
		var valid bool
		minVersion, valid = normalizeVersion(req.MinSupportedVersion)
		if !valid {
			c.JSON(http.StatusBadRequest, NewErrorResponse("最低支持版本必须符合语义化版本规范"))
			return
		}
	}
	if req.Channel == "" {
		req.Channel = "stable"
	}

	artifacts := make([]model.Artifact, 0, len(req.Artifacts))
	for _, item := range req.Artifacts {
		artifact, err := buildArtifact(item)
		if err != nil {
			c.JSON(http.StatusBadRequest, NewErrorResponse(err.Error()))
			return
		}
		artifacts = append(artifacts, artifact)
	}

	ctx := c.Request.Context()
	release := model.Release{
		ID:                  uuid.NewString(),
		Component:           component,
		Version:             version,
		CommitID:            req.CommitID,
		Channel:             req.Channel,
		Status:              model.ReleaseStatusDraft,
		ReleaseNotes:        req.ReleaseNotes,
		MinSupportedVersion: minVersion,
		CreatedBy:           uint64(getAdminIDFromContext(c)),
	}
	if err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&release).Error; err != nil {
			return err
		}
		for i := range artifacts {
			artifacts[i].ReleaseID = release.ID
			if err := tx.Create(&artifacts[i]).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			c.JSON(http.StatusConflict, NewErrorResponse("该组件版本已存在"))
			return
		}
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建发布版本失败"))
		return
	}
	recordAuditLog(ctx, c, "updater_release_create", "release", release.ID, release.Version, release)
	c.JSON(http.StatusCreated, NewSuccessResponse(gin.H{"release": release, "artifacts": artifacts}))
}

func (a *UpdaterAPI) ListReleases(c *gin.Context) {
	ctx := c.Request.Context()
	query := db.DB.WithContext(ctx).Model(&model.Release{})
	if component := c.Query("component"); component != "" {
		query = query.Where("component = ?", component)
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}
	var releases []model.Release
	if err := query.Order("created_at DESC").Find(&releases).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询发布版本失败"))
		return
	}
	c.JSON(http.StatusOK, NewSuccessResponse(releases))
}

func (a *UpdaterAPI) GetRelease(c *gin.Context) {
	ctx := c.Request.Context()
	var release model.Release
	if err := db.DB.WithContext(ctx).First(&release, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("发布版本不存在"))
		return
	}
	var artifacts []model.Artifact
	if err := db.DB.WithContext(ctx).Where("release_id = ?", release.ID).Order("os, arch").Find(&artifacts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询制品失败"))
		return
	}
	c.JSON(http.StatusOK, NewSuccessResponse(gin.H{"release": release, "artifacts": artifacts}))
}

func (a *UpdaterAPI) PublishRelease(c *gin.Context) {
	ctx := c.Request.Context()
	var release model.Release
	if err := db.DB.WithContext(ctx).First(&release, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("发布版本不存在"))
		return
	}
	if release.Status == model.ReleaseStatusRevoked {
		c.JSON(http.StatusConflict, NewErrorResponse("已撤销的发布版本不能再次发布"))
		return
	}
	var artifacts []model.Artifact
	if err := db.DB.WithContext(ctx).Where("release_id = ? AND status = ?", release.ID, model.ArtifactStatusAvailable).Find(&artifacts).Error; err != nil || len(artifacts) == 0 {
		c.JSON(http.StatusConflict, NewErrorResponse("发布版本缺少可用制品"))
		return
	}
	now := time.Now()
	if err := db.DB.WithContext(ctx).Model(&release).Updates(map[string]any{"status": model.ReleaseStatusPublished, "published_at": now}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("发布版本失败"))
		return
	}
	recordAuditLog(ctx, c, "updater_release_publish", "release", release.ID, release.Version, nil)
	c.JSON(http.StatusOK, NewSuccessResponse(gin.H{"id": release.ID, "status": model.ReleaseStatusPublished, "published_at": now}))
}

type CreateUpdateTaskRequest struct {
	Component   string  `json:"component" binding:"required"`
	ReleaseID   string  `json:"release_id" binding:"required"`
	TargetType  string  `json:"target_type" binding:"required"`
	TargetID    string  `json:"target_id" binding:"required"`
	Force       bool    `json:"force"`
	ScheduledAt *string `json:"scheduled_at"`
	DeadlineAt  *string `json:"deadline_at"`
	MaxAttempts int     `json:"max_attempts"`
}

func (a *UpdaterAPI) CreateTask(c *gin.Context) {
	var req CreateUpdateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("更新任务参数无效"))
		return
	}
	scheduledAt, err := parseOptionalTime(req.ScheduledAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("scheduled_at 必须是 RFC3339 时间"))
		return
	}
	deadlineAt, err := parseOptionalTime(req.DeadlineAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("deadline_at 必须是 RFC3339 时间"))
		return
	}
	if scheduledAt != nil && deadlineAt != nil && !deadlineAt.After(*scheduledAt) {
		c.JSON(http.StatusBadRequest, NewErrorResponse("deadline_at 必须晚于 scheduled_at"))
		return
	}
	task, err := a.updates.CreateTask(c.Request.Context(), service.CreateUpdateTaskInput{
		Component:   model.Component(strings.ToLower(req.Component)),
		TargetType:  model.UpdateTargetType(strings.ToLower(req.TargetType)),
		TargetID:    req.TargetID,
		ReleaseID:   req.ReleaseID,
		Force:       req.Force,
		ScheduledAt: scheduledAt,
		DeadlineAt:  deadlineAt,
		MaxAttempts: req.MaxAttempts,
		CreatedBy:   uint64(getAdminIDFromContext(c)),
	})
	if err != nil {
		a.writeTaskError(c, err)
		return
	}
	recordAuditLog(c.Request.Context(), c, "updater_task_create", "update_task", task.ID, task.TargetName, task)
	c.JSON(http.StatusCreated, NewSuccessResponse(task))
}

func (a *UpdaterAPI) ListTasks(c *gin.Context) {
	ctx := c.Request.Context()
	query := db.DB.WithContext(ctx).Preload("Release").Model(&model.UpdateTask{})
	for _, field := range []string{"component", "target_type", "target_id", "status", "release_id"} {
		if value := c.Query(field); value != "" {
			query = query.Where(field+" = ?", value)
		}
	}
	var tasks []model.UpdateTask
	if err := query.Order("created_at DESC").Find(&tasks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询更新任务失败"))
		return
	}
	c.JSON(http.StatusOK, NewSuccessResponse(tasks))
}

func (a *UpdaterAPI) GetTask(c *gin.Context) {
	ctx := c.Request.Context()
	var task model.UpdateTask
	if err := db.DB.WithContext(ctx).Preload("Release").First(&task, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("更新任务不存在"))
		return
	}
	var events []model.UpdateEvent
	if err := db.DB.WithContext(ctx).Where("task_id = ?", task.ID).Order("id ASC").Find(&events).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询更新事件失败"))
		return
	}
	c.JSON(http.StatusOK, NewSuccessResponse(gin.H{"task": task, "events": events}))
}

func (a *UpdaterAPI) CancelTask(c *gin.Context) {
	if err := a.updates.Cancel(c.Request.Context(), c.Param("id")); err != nil {
		a.writeTaskError(c, err)
		return
	}
	recordAuditLog(c.Request.Context(), c, "updater_task_cancel", "update_task", c.Param("id"), "", nil)
	c.JSON(http.StatusOK, NewSuccessMessageResponse("更新任务已取消", nil))
}

func (a *UpdaterAPI) RetryTask(c *gin.Context) {
	task, err := a.updates.Retry(c.Request.Context(), c.Param("id"))
	if err != nil {
		a.writeTaskError(c, err)
		return
	}
	recordAuditLog(c.Request.Context(), c, "updater_task_retry", "update_task", task.ID, task.TargetName, gin.H{"retry_of_task_id": c.Param("id")})
	c.JSON(http.StatusCreated, NewSuccessMessageResponse("已创建新的更新重试任务", task))
}

func (a *UpdaterAPI) writeTaskError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrReleaseNotPublished):
		c.JSON(http.StatusConflict, NewErrorResponse("发布版本尚未发布或组件不匹配"))
	case errors.Is(err, service.ErrArtifactNotFound):
		c.JSON(http.StatusConflict, NewErrorResponse("目标平台没有可用制品"))
	case errors.Is(err, service.ErrActiveTaskExists):
		c.JSON(http.StatusConflict, NewErrorResponse("该目标已有未完成更新任务"))
	case errors.Is(err, service.ErrUpdaterUnsupported):
		c.JSON(http.StatusConflict, NewErrorResponse("Agent 目标尚未报告支持 updater 协议 v2"))
	case errors.Is(err, service.ErrInvalidBuildIdentity):
		c.JSON(http.StatusConflict, NewErrorResponse("目标 Release 或 Artifact 缺少有效构建身份"))
	case errors.Is(err, service.ErrComponentNotSupported):
		c.JSON(http.StatusConflict, NewErrorResponse("该组件的自动更新执行器尚未实现"))
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, NewErrorResponse("目标或发布版本不存在"))
	default:
		c.JSON(http.StatusBadRequest, NewErrorResponse(err.Error()))
	}
}

func buildArtifact(input ArtifactInput) (model.Artifact, error) {
	osName := strings.ToLower(strings.TrimSpace(input.OS))
	arch := strings.ToLower(strings.TrimSpace(input.Arch))
	if osName == "macos" {
		osName = "darwin"
	}
	if osName == "" || arch == "" {
		return model.Artifact{}, errors.New("制品 OS 和架构不能为空")
	}
	if input.Size <= 0 {
		return model.Artifact{}, errors.New("制品大小必须大于 0")
	}
	if len(input.SHA256) != 64 || strings.Trim(input.SHA256, "0123456789abcdefABCDEF") != "" {
		return model.Artifact{}, errors.New("SHA256 必须是 64 位十六进制字符串")
	}
	parsedURL, err := url.Parse(input.DownloadURL)
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
		return model.Artifact{}, errors.New("制品下载地址必须是 HTTPS URL")
	}
	filename := strings.TrimSpace(input.Filename)
	if filename == "" || path.Base(filename) != filename || strings.Contains(filename, "\\") {
		return model.Artifact{}, errors.New("制品文件名无效")
	}
	role := strings.ToLower(strings.TrimSpace(input.Role))
	if role == "" {
		role = "app"
	}
	if role != "app" && role != "launcher" {
		return model.Artifact{}, errors.New("制品 role 必须为 app 或 launcher")
	}
	return model.Artifact{
		ID:          uuid.NewString(),
		OS:          osName,
		Arch:        arch,
		Role:        role,
		PackageType: input.PackageType,
		Filename:    filename,
		DownloadURL: input.DownloadURL,
		Size:        input.Size,
		SHA256:      strings.ToLower(input.SHA256),
		Status:      model.ArtifactStatusAvailable,
	}, nil
}

func normalizeVersion(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value != "" && !strings.HasPrefix(value, "v") {
		value = "v" + value
	}
	return value, semver.IsValid(value)
}

func validAgentCommitID(value string) bool {
	if len(value) != 40 || value != strings.ToLower(value) {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func parseOptionalTime(value *string) (*time.Time, error) {
	if value == nil || *value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, *value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

var _ = strconv.IntSize
