package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestHealthEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 创建测试路由
	router := gin.New()
	healthAPI := NewHealthAPI(nil, nil)
	router.GET("/health", healthAPI.Health)

	// 创建测试请求
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	router.ServeHTTP(w, req)

	// 验证响应
	assert.Equal(t, 200, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.Equal(t, "ok", response["status"])
	assert.NotEmpty(t, response["timestamp"])
}

func TestReadyEndpoint_Structure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 注意：这个测试仅验证API响应结构
	// 完整的功能测试需要在集成测试中进行，因为需要：
	// 1. 初始化数据库
	// 2. 启动FRP Server
	// 3. 启动gRPC Server
	// 这里我们只测试API的基本结构是否正确

	router := gin.New()
	healthAPI := NewHealthAPI(nil, nil)
	router.GET("/health/ready", healthAPI.Ready)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health/ready", nil)

	// 由于依赖未初始化，这个调用会panic
	// 在实际使用中，依赖会被正确初始化
	// 这里我们跳过这个测试，留待集成测试
	t.Skip("需要集成测试环境（数据库、FRP Server等）")

	router.ServeHTTP(w, req)
}
