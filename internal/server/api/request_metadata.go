package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
)

const (
	HeaderRequestID      = "X-Request-ID"
	HeaderIdempotencyKey = "Idempotency-Key"
	HeaderIfMatch        = "If-Match"

	ErrorCodeFeatureDisabled        = "FEATURE_DISABLED"
	ErrorCodeIdempotencyKeyRequired = "IDEMPOTENCY_KEY_REQUIRED"
	ErrorCodeInvalidArgument        = "INVALID_ARGUMENT"
	ErrorCodePreconditionRequired   = "PRECONDITION_REQUIRED"
	ErrorCodeAuditWriteFailed       = "AUDIT_WRITE_FAILED"
)

const (
	contextRequestID      = "request_id"
	contextIdempotencyKey = "idempotency_key"
	contextIfMatch        = "if_match_revision"
)

// RequestMetadataMiddleware establishes correlation metadata only. Request IDs
// are deliberately not used for authorization or idempotency decisions.
func RequestMetadataMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		value := singleSafeHeader(c, HeaderRequestID, 64)
		if value == "" {
			value = uuid.NewString()
		}
		c.Set(contextRequestID, value)
		c.Header(HeaderRequestID, value)
		c.Next()
	}
}

// RequireIdempotencyKey defines the validation boundary for new create and
// high-risk action routes. Persistence and replay are introduced in M0-B.
func RequireIdempotencyKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		values := c.Request.Header.Values(HeaderIdempotencyKey)
		if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
			codedError(c, http.StatusBadRequest, ErrorCodeIdempotencyKeyRequired, "必须提供 Idempotency-Key")
			c.Abort()
			return
		}
		key := singleSafeHeader(c, HeaderIdempotencyKey, 128)
		if key == "" {
			codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "Idempotency-Key 无效")
			c.Abort()
			return
		}
		c.Set(contextIdempotencyKey, key)
		c.Next()
	}
}

// RequireIfMatch parses an integer revision from If-Match and stores it in the
// request context for new update and state-transition handlers.
func RequireIfMatch() gin.HandlerFunc {
	return func(c *gin.Context) {
		revision, ok := parseIfMatch(c)
		if !ok {
			c.Abort()
			return
		}
		c.Set(contextIfMatch, revision)
		c.Next()
	}
}

func parseIfMatch(c *gin.Context) (int64, bool) {
	values := c.Request.Header.Values(HeaderIfMatch)
	if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		codedError(c, http.StatusPreconditionRequired, ErrorCodePreconditionRequired, "必须提供 If-Match revision")
		return 0, false
	}
	if len(values) != 1 || strings.Contains(values[0], ",") {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "If-Match 只能包含一个 revision")
		return 0, false
	}
	value := strings.TrimSpace(values[0])
	if strings.HasPrefix(value, "W/") {
		value = strings.TrimSpace(strings.TrimPrefix(value, "W/"))
	}
	if strings.HasPrefix(value, "\"") || strings.HasSuffix(value, "\"") {
		if len(value) < 2 || !strings.HasPrefix(value, "\"") || !strings.HasSuffix(value, "\"") {
			codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "If-Match revision 无效")
			return 0, false
		}
		value = value[1 : len(value)-1]
	}
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision <= 0 {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "If-Match revision 无效")
		return 0, false
	}
	return revision, true
}

func requiredRevision(c *gin.Context) (int64, bool) {
	value, exists := c.Get(contextIfMatch)
	if !exists {
		return 0, false
	}
	revision, ok := value.(int64)
	return revision, ok
}

func SetRevisionETag(c *gin.Context, revision int64) {
	c.Header("ETag", fmt.Sprintf("\"%d\"", revision))
}

// RequireFeatureFlag keeps new resource routes inert until their milestone is
// explicitly enabled. Rejected writes are audited before a response is sent.
func RequireFeatureFlag(flags config.FeatureFlagsSection, flag config.FeatureFlag, auditRejection bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if flags.Enabled(flag) {
			c.Next()
			return
		}
		if auditRejection && requestCanMutate(c.Request.Method) {
			detail := gin.H{
				"feature_flag": string(flag),
				"method":       c.Request.Method,
				"route":        c.FullPath(),
				"result":       ErrorCodeFeatureDisabled,
			}
			if key := singleSafeHeader(c, HeaderIdempotencyKey, 128); key != "" {
				detail["idempotency_key"] = key
			}
			if err := recordAuditLogStrict(c.Request.Context(), c, "feature_flag_write_rejected", "feature_flag", string(flag), string(flag), detail); err != nil {
				codedError(c, http.StatusServiceUnavailable, ErrorCodeAuditWriteFailed, "安全审计写入失败，请求已拒绝")
				c.Abort()
				return
			}
		}
		codedError(c, http.StatusServiceUnavailable, ErrorCodeFeatureDisabled, "该能力尚未启用")
		c.Abort()
	}
}

func requestCanMutate(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func singleSafeHeader(c *gin.Context, name string, maxLength int) string {
	values := c.Request.Header.Values(name)
	if len(values) != 1 {
		return ""
	}
	value := strings.TrimSpace(values[0])
	if value == "" || len(value) > maxLength || strings.Contains(value, ",") {
		return ""
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || strings.ContainsRune("-_.:", char) {
			continue
		}
		return ""
	}
	return value
}
