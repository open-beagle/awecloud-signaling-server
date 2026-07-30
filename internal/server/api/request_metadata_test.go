package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestRequestMetadataUsesSafeCorrelationIDWithoutTrustingDuplicates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestMetadataMiddleware())
	router.GET("/metadata", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"first": requestID(c), "second": requestID(c)})
	})

	valid := httptest.NewRequest(http.MethodGet, "/metadata", nil)
	valid.Header.Set(HeaderRequestID, "client-request-1")
	validResponse := httptest.NewRecorder()
	router.ServeHTTP(validResponse, valid)
	require.Equal(t, "client-request-1", validResponse.Header().Get(HeaderRequestID))

	duplicate := httptest.NewRequest(http.MethodGet, "/metadata", nil)
	duplicate.Header.Add(HeaderRequestID, "reused")
	duplicate.Header.Add(HeaderRequestID, "reused")
	duplicateResponse := httptest.NewRecorder()
	router.ServeHTTP(duplicateResponse, duplicate)
	require.NotEmpty(t, duplicateResponse.Header().Get(HeaderRequestID))
	require.NotEqual(t, "reused", duplicateResponse.Header().Get(HeaderRequestID))
	var body map[string]string
	require.NoError(t, json.Unmarshal(duplicateResponse.Body.Bytes(), &body))
	require.Equal(t, body["first"], body["second"])
	require.Equal(t, duplicateResponse.Header().Get(HeaderRequestID), body["first"])

	unsafe := httptest.NewRequest(http.MethodGet, "/metadata", nil)
	unsafe.Header.Set(HeaderRequestID, "contains spaces")
	unsafeResponse := httptest.NewRecorder()
	router.ServeHTTP(unsafeResponse, unsafe)
	require.NotEqual(t, "contains spaces", unsafeResponse.Header().Get(HeaderRequestID))
}

func TestNewRoutePreconditionsDoNotAffectLegacyRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestMetadataMiddleware())
	router.POST("/legacy", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.POST("/new-create", RequireIdempotencyKey(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.PATCH("/new-update", RequireIfMatch(), func(c *gin.Context) {
		revision, ok := requiredRevision(c)
		require.True(t, ok)
		SetRevisionETag(c, revision+1)
		c.Status(http.StatusNoContent)
	})

	legacyResponse := httptest.NewRecorder()
	router.ServeHTTP(legacyResponse, httptest.NewRequest(http.MethodPost, "/legacy", nil))
	require.Equal(t, http.StatusNoContent, legacyResponse.Code)

	missingKeyResponse := httptest.NewRecorder()
	router.ServeHTTP(missingKeyResponse, httptest.NewRequest(http.MethodPost, "/new-create", nil))
	require.Equal(t, http.StatusBadRequest, missingKeyResponse.Code)
	assertResponseErrorCode(t, missingKeyResponse, ErrorCodeIdempotencyKeyRequired)

	invalidKeyRequest := httptest.NewRequest(http.MethodPost, "/new-create", nil)
	invalidKeyRequest.Header.Set(HeaderIdempotencyKey, "contains spaces")
	invalidKeyResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidKeyResponse, invalidKeyRequest)
	require.Equal(t, http.StatusBadRequest, invalidKeyResponse.Code)
	assertResponseErrorCode(t, invalidKeyResponse, ErrorCodeInvalidArgument)

	createRequest := httptest.NewRequest(http.MethodPost, "/new-create", nil)
	createRequest.Header.Set(HeaderIdempotencyKey, "create-tenant-1")
	createResponse := httptest.NewRecorder()
	router.ServeHTTP(createResponse, createRequest)
	require.Equal(t, http.StatusNoContent, createResponse.Code)

	missingRevisionResponse := httptest.NewRecorder()
	router.ServeHTTP(missingRevisionResponse, httptest.NewRequest(http.MethodPatch, "/new-update", nil))
	require.Equal(t, http.StatusPreconditionRequired, missingRevisionResponse.Code)
	assertResponseErrorCode(t, missingRevisionResponse, ErrorCodePreconditionRequired)

	invalidRevisionRequest := httptest.NewRequest(http.MethodPatch, "/new-update", nil)
	invalidRevisionRequest.Header.Set(HeaderIfMatch, "not-a-revision")
	invalidRevisionResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidRevisionResponse, invalidRevisionRequest)
	require.Equal(t, http.StatusBadRequest, invalidRevisionResponse.Code)
	assertResponseErrorCode(t, invalidRevisionResponse, ErrorCodeInvalidArgument)

	malformedQuotedRevisionRequest := httptest.NewRequest(http.MethodPatch, "/new-update", nil)
	malformedQuotedRevisionRequest.Header.Set(HeaderIfMatch, `"7`)
	malformedQuotedRevisionResponse := httptest.NewRecorder()
	router.ServeHTTP(malformedQuotedRevisionResponse, malformedQuotedRevisionRequest)
	require.Equal(t, http.StatusBadRequest, malformedQuotedRevisionResponse.Code)
	assertResponseErrorCode(t, malformedQuotedRevisionResponse, ErrorCodeInvalidArgument)

	updateRequest := httptest.NewRequest(http.MethodPatch, "/new-update", nil)
	updateRequest.Header.Set(HeaderIfMatch, `W/"7"`)
	updateResponse := httptest.NewRecorder()
	router.ServeHTTP(updateResponse, updateRequest)
	require.Equal(t, http.StatusNoContent, updateResponse.Code)
	require.Equal(t, `"8"`, updateResponse.Header().Get("ETag"))
}

func TestDisabledFeatureAuditsWriteRejectionAndFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldDB := db.DB
	t.Cleanup(func() { db.DB = oldDB })
	database, err := gorm.Open(sqlite.Open("file:feature_gate_audit_test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.AuditLog{}))
	db.DB = database

	flags := config.FeatureFlagsSection{}
	router := gin.New()
	router.Use(RequestMetadataMiddleware())
	router.POST("/new-resource", RequireFeatureFlag(flags, config.FeatureResourceModelWrite, true), func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})
	request := httptest.NewRequest(http.MethodPost, "/new-resource", nil)
	request.Header.Set(HeaderIdempotencyKey, "new-resource-1")
	traceID, err := trace.TraceIDFromHex("102030405060708090a0b0c0d0e0f001")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("1020304050607080")
	require.NoError(t, err)
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled})
	request = request.WithContext(trace.ContextWithSpanContext(request.Context(), spanContext))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	assertResponseErrorCode(t, response, ErrorCodeFeatureDisabled)

	var audit model.AuditLog
	require.NoError(t, database.First(&audit).Error)
	require.Equal(t, "feature_flag_write_rejected", audit.ActionType)
	require.Equal(t, string(config.FeatureResourceModelWrite), audit.TargetID)
	require.Equal(t, response.Header().Get(HeaderRequestID), audit.RequestID)
	require.Equal(t, traceID.String(), audit.TraceID)
	require.Contains(t, audit.Detail, "new-resource-1")

	db.DB = nil
	failClosedResponse := httptest.NewRecorder()
	router.ServeHTTP(failClosedResponse, httptest.NewRequest(http.MethodPost, "/new-resource", nil))
	require.Equal(t, http.StatusServiceUnavailable, failClosedResponse.Code)
	assertResponseErrorCode(t, failClosedResponse, ErrorCodeAuditWriteFailed)
}

func TestEnabledFeaturePassesThrough(t *testing.T) {
	flags := config.FeatureFlagsSection{ResourceModelWrite: true}
	router := gin.New()
	router.POST("/new-resource", RequireFeatureFlag(flags, config.FeatureResourceModelWrite, true), func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/new-resource", nil))
	require.Equal(t, http.StatusCreated, response.Code)
}

func assertResponseErrorCode(t *testing.T, response *httptest.ResponseRecorder, expected string) {
	t.Helper()
	var body Response
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, expected, body.Code)
	require.NotEmpty(t, body.RequestID)
}
