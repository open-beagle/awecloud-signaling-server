package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

const (
	userSimulationCreateRoute = "/api/v1/management/user-simulations"
	maxSimulationJSONBytes    = 64 * 1024

	ErrorCodeSimulationVersionConflict = "USER_SIMULATION_VERSION_CONFLICT"
	ErrorCodeIdempotencyKeyReused      = "IDEMPOTENCY_KEY_REUSED"
	ErrorCodeIdempotencyInProgress     = "IDEMPOTENCY_IN_PROGRESS"
	ErrorCodeIdempotencyRecoveryNeeded = "IDEMPOTENCY_RECOVERY_REQUIRED"
)

type UserSimulationAPI struct {
	maxDuration time.Duration
}

func NewUserSimulationAPI(maxHours int) *UserSimulationAPI {
	if maxHours <= 0 {
		maxHours = 8
	}
	return &UserSimulationAPI{maxDuration: time.Duration(maxHours) * time.Hour}
}

type createUserSimulationRequest struct {
	EffectiveUserID uint64                        `json:"effective_user_id"`
	ScopeType       model.UserSimulationScopeType `json:"scope_type"`
	ScopeID         string                        `json:"scope_id"`
	Reason          string                        `json:"reason"`
	ExpiresAt       time.Time                     `json:"expires_at"`
}

type revokeUserSimulationRequest struct {
	Reason string `json:"reason"`
}

type userSimulationResponse struct {
	ID                 string                            `json:"id"`
	ActorUserID        uint64                            `json:"actor_user_id"`
	EffectiveUserID    uint64                            `json:"effective_user_id"`
	ScopeType          model.UserSimulationScopeType     `json:"scope_type"`
	ScopeID            string                            `json:"scope_id"`
	Reason             string                            `json:"reason"`
	Status             model.UserSimulationSessionStatus `json:"status"`
	StartedAt          time.Time                         `json:"started_at"`
	ExpiresAt          time.Time                         `json:"expires_at"`
	EndedAt            *time.Time                        `json:"ended_at,omitempty"`
	EndReason          string                            `json:"end_reason,omitempty"`
	CreatedRequestID   string                            `json:"created_request_id"`
	PermissionRevision int64                             `json:"permission_revision"`
	RowVersion         int64                             `json:"row_version"`
	CreatedAt          time.Time                         `json:"created_at"`
	UpdatedAt          time.Time                         `json:"updated_at"`
}

func (a *UserSimulationAPI) Create(c *gin.Context) {
	identity, ok := currentUnifiedManagementIdentity(c)
	if !ok {
		return
	}
	body, ok := readSimulationJSON(c)
	if !ok {
		return
	}
	var request createUserSimulationRequest
	if !decodeSimulationJSON(c, body, &request) {
		return
	}
	now := time.Now()
	request.ScopeID = strings.TrimSpace(request.ScopeID)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.EffectiveUserID == 0 || request.ScopeID == "" || request.Reason == "" || len(request.Reason) > 500 ||
		(request.ScopeType != model.UserSimulationScopeProvider && request.ScopeType != model.UserSimulationScopeTenant) ||
		!request.ExpiresAt.After(now) || request.ExpiresAt.After(now.Add(a.maxDuration)) {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "用户模拟参数或有效期无效")
		return
	}

	targetContext, err := service.ResolveManagementContext(
		db.DB.WithContext(c.Request.Context()), request.EffectiveUserID, model.ManagementScopeType(request.ScopeType), request.ScopeID, now, true,
	)
	if err != nil || targetContext.ScopeStatus != "active" {
		codedError(c, http.StatusNotFound, ErrorCodeManagementObjectMissing, "目标用户或工作域不存在或不可用")
		return
	}

	idempotency := service.NewAPIIdempotencyService(db.DB, map[string]service.JSONFieldPolicy{
		http.MethodPost + " " + userSimulationCreateRoute: service.NewJSONFieldPolicy("success", "data"),
	}, 5*time.Minute, 24*time.Hour)
	begin, err := idempotency.Begin(c.Request.Context(), service.BeginIdempotencyInput{
		ActorType: "user", ActorID: strconv.FormatUint(identity.UserID, 10), ScopeType: string(model.ManagementScopePlatform),
		ScopeID: "global", Method: http.MethodPost, Route: userSimulationCreateRoute,
		Key: singleSafeHeader(c, HeaderIdempotencyKey, 128), Body: body,
	})
	if err != nil {
		writeSimulationIdempotencyError(c, err)
		return
	}
	if begin.Replay {
		var response struct {
			Data userSimulationResponse `json:"data"`
		}
		if json.Unmarshal([]byte(begin.Record.ResponseBody), &response) == nil && response.Data.RowVersion > 0 {
			SetRevisionETag(c, response.Data.RowVersion)
		}
		c.Data(begin.Record.ResponseStatus, "application/json; charset=utf-8", []byte(begin.Record.ResponseBody))
		return
	}

	session := &model.UserSimulationSession{
		ID: uuid.NewString(), ActorUserID: identity.UserID, EffectiveUserID: request.EffectiveUserID,
		ScopeType: request.ScopeType, ScopeID: request.ScopeID, Reason: request.Reason,
		Status: model.UserSimulationSessionActive, StartedAt: now, ExpiresAt: request.ExpiresAt,
		CreatedRequestID: requestID(c), PermissionRevision: targetContext.PermissionRevision, RowVersion: 1,
	}
	var responseBody []byte
	err = db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := service.CreateUserSimulationSession(tx, session); err != nil {
			return err
		}
		var err error
		responseBody, err = json.Marshal(NewSuccessResponse(userSimulationView(session)))
		if err != nil {
			return err
		}
		setSimulationAuditContext(c, session)
		if err := recordAuditLogStrictWithDB(c.Request.Context(), tx, c, "create_user_simulation", "user_simulation", session.ID, session.ID, gin.H{
			"effective_user_id": session.EffectiveUserID, "scope_type": session.ScopeType, "scope_id": session.ScopeID,
			"expires_at": session.ExpiresAt, "reason": session.Reason,
		}); err != nil {
			return err
		}
		_, err = idempotency.Complete(tx, service.CompleteIdempotencyInput{
			RecordID: begin.Record.ID, RequestHash: begin.Record.RequestHash, Status: model.APIIdempotencyCompleted,
			ResponseStatus: http.StatusCreated, ResponseBody: responseBody,
		})
		return err
	})
	if err != nil {
		writeUserSimulationServiceError(c, err)
		return
	}
	SetRevisionETag(c, session.RowVersion)
	c.Data(http.StatusCreated, "application/json; charset=utf-8", responseBody)
}

func (a *UserSimulationAPI) List(c *gin.Context) {
	sessions, err := service.ListUserSimulationSessions(db.DB.WithContext(c.Request.Context()), time.Now())
	if err != nil {
		codedError(c, http.StatusInternalServerError, "USER_SIMULATION_QUERY_FAILED", "查询用户模拟会话失败")
		return
	}
	items := make([]userSimulationResponse, 0, len(sessions))
	for index := range sessions {
		items = append(items, userSimulationView(&sessions[index]))
	}
	c.JSON(http.StatusOK, NewSuccessResponse(items))
}

func (a *UserSimulationAPI) Revoke(c *gin.Context) {
	identity, ok := currentUnifiedManagementIdentity(c)
	if !ok {
		return
	}
	rowVersion, ok := requiredRevision(c)
	if !ok {
		codedError(c, http.StatusPreconditionRequired, ErrorCodePreconditionRequired, "必须提供 If-Match revision")
		return
	}
	body, ok := readSimulationJSON(c)
	if !ok {
		return
	}
	var request revokeUserSimulationRequest
	if !decodeSimulationJSON(c, body, &request) {
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len(request.Reason) > 100 {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "撤销原因无效")
		return
	}

	var session *model.UserSimulationSession
	err := db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var err error
		session, err = service.RevokeUserSimulationSession(tx, c.Param("id"), identity.UserID, rowVersion, request.Reason, time.Now())
		if err != nil {
			return err
		}
		setSimulationAuditContext(c, session)
		return recordAuditLogStrictWithDB(c.Request.Context(), tx, c, "revoke_user_simulation", "user_simulation", session.ID, session.ID, gin.H{
			"effective_user_id": session.EffectiveUserID, "scope_type": session.ScopeType, "scope_id": session.ScopeID,
			"reason": request.Reason, "row_version": session.RowVersion,
		})
	})
	if err != nil {
		writeUserSimulationServiceError(c, err)
		return
	}
	SetRevisionETag(c, session.RowVersion)
	c.JSON(http.StatusOK, NewSuccessResponse(userSimulationView(session)))
}

func readSimulationJSON(c *gin.Context) ([]byte, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSimulationJSONBytes)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "请求体无效或过大")
		return nil, false
	}
	return body, true
}

func decodeSimulationJSON(c *gin.Context, body []byte, target any) bool {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "请求 JSON 无效")
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "请求 JSON 只能包含一个对象")
		return false
	}
	return true
}

func userSimulationView(session *model.UserSimulationSession) userSimulationResponse {
	return userSimulationResponse{
		ID: session.ID, ActorUserID: session.ActorUserID, EffectiveUserID: session.EffectiveUserID,
		ScopeType: session.ScopeType, ScopeID: session.ScopeID, Reason: session.Reason, Status: session.Status,
		StartedAt: session.StartedAt, ExpiresAt: session.ExpiresAt, EndedAt: session.EndedAt, EndReason: session.EndReason,
		CreatedRequestID: session.CreatedRequestID, PermissionRevision: session.PermissionRevision,
		RowVersion: session.RowVersion, CreatedAt: session.CreatedAt, UpdatedAt: session.UpdatedAt,
	}
}

func setSimulationAuditContext(c *gin.Context, session *model.UserSimulationSession) {
	if session == nil {
		return
	}
	c.Set(contextAuditActorUserID, session.ActorUserID)
	c.Set(contextAuditEffectiveUserID, session.EffectiveUserID)
	c.Set(contextAuditSimulationSessionID, session.ID)
	c.Set(contextAuditScopeType, string(session.ScopeType))
	c.Set(contextAuditScopeID, session.ScopeID)
}

func writeSimulationIdempotencyError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrIdempotencyKeyReused):
		codedError(c, http.StatusConflict, ErrorCodeIdempotencyKeyReused, "Idempotency-Key 已用于不同请求")
	case errors.Is(err, service.ErrIdempotencyInProgress):
		codedError(c, http.StatusConflict, ErrorCodeIdempotencyInProgress, "相同请求正在处理中")
	case errors.Is(err, service.ErrIdempotencyRecoveryNeeded):
		codedError(c, http.StatusConflict, ErrorCodeIdempotencyRecoveryNeeded, "相同请求需要业务恢复确认")
	default:
		codedError(c, http.StatusInternalServerError, "IDEMPOTENCY_STATE_FAILED", "读取幂等状态失败")
	}
}

func writeUserSimulationServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrResourceIdentityInvalid):
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "用户模拟请求无效")
	case errors.Is(err, service.ErrResourceIdentityReference), errors.Is(err, service.ErrUserSimulationNotAllowed):
		codedError(c, http.StatusNotFound, ErrorCodeManagementObjectMissing, "用户模拟会话或目标对象不存在")
	case errors.Is(err, service.ErrUserSimulationInactive):
		codedError(c, http.StatusConflict, ErrorCodeSimulationInactive, "用户模拟会话已结束或过期")
	case errors.Is(err, service.ErrUserSimulationVersion):
		codedError(c, http.StatusConflict, ErrorCodeSimulationVersionConflict, "用户模拟会话版本已变化")
	default:
		codedError(c, http.StatusInternalServerError, "USER_SIMULATION_WRITE_FAILED", "写入用户模拟会话失败")
	}
}
