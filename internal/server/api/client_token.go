package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// CreateClientTokenRequest 创建 Client Token 请求
type CreateClientTokenRequest struct {
	UserID     uint   `json:"user_id" binding:"required"`     // 用户 ID
	Name       string `json:"name" binding:"required"`        // Token 名称
	DeviceName string `json:"device_name" binding:"required"` // 设备名称
}

// CreateClientTokenResponse 创建 Client Token 响应
type CreateClientTokenResponse struct {
	Token      string `json:"token"`       // Token
	Name       string `json:"name"`        // Token 名称
	DeviceName string `json:"device_name"` // 设备名称
	EnvConfig  string `json:"env_config"`  // 环境变量配置
}

// ClientRegisterRequest CloudIDE 注册请求
type ClientRegisterRequest struct {
	Token             string `json:"token" binding:"required"`              // Client Token
	DeviceFingerprint string `json:"device_fingerprint" binding:"required"` // 设备指纹
	DeviceName        string `json:"device_name"`                           // 设备名称（可选，首次使用时可更新）
}

// ClientRegisterResponse CloudIDE 注册响应
type ClientRegisterResponse struct {
	AuthKey      string `json:"auth_key"`      // Headscale 认证密钥
	HeadscaleURL string `json:"headscale_url"` // Headscale 地址
	UserName     string `json:"user_name"`     // 用户名
}

// generateClientToken 生成 Client Token
func generateClientToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "ct_" + hex.EncodeToString(bytes), nil
}

// CreateClientToken 创建 Client Token
// @Summary 创建 Client Token
// @Tags Client Token
// @Accept json
// @Produce json
// @Param request body CreateClientTokenRequest true "请求参数"
// @Success 200 {object} Response{data=CreateClientTokenResponse}
// @Router /api/v1/client/token [post]
func CreateClientToken(c *gin.Context) {
	var req CreateClientTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("参数错误: "+err.Error()))
		return
	}

	ctx := c.Request.Context()

	// 验证用户是否存在
	var user model.User
	if err := db.WithContext(ctx).First(&user, req.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("用户不存在"))
		return
	}

	// 生成 Token
	token, err := generateClientToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("生成 Token 失败"))
		return
	}

	// 获取当前管理员 ID
	adminID, _ := c.Get("admin_id")

	// 创建 Token 记录
	clientToken := &model.ClientToken{
		Token:      token,
		UserID:     req.UserID,
		Name:       req.Name,
		DeviceName: req.DeviceName,
		CreatedBy:  adminID.(uint),
	}

	if err := db.WithContext(ctx).Create(clientToken).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("创建 Token 失败"))
		return
	}

	// 获取系统配置中的 Server 地址
	var serverAddress string
	var sysConfig model.SystemConfig
	if err := db.WithContext(ctx).Where("key = ?", "public_url").First(&sysConfig).Error; err == nil {
		serverAddress = sysConfig.Value
	}

	// 生成环境变量配置
	envConfig := "CLIENT_TOKEN=" + token + "\n" +
		"SERVER_ADDRESS=" + serverAddress + "\n" +
		"DEVICE_NAME=" + req.DeviceName + "\n" +
		"USER_ID=" + user.Name

	c.JSON(http.StatusOK, NewSuccessResponse(CreateClientTokenResponse{
		Token:      token,
		Name:       req.Name,
		DeviceName: req.DeviceName,
		EnvConfig:  envConfig,
	}))
}

// ListClientTokens 获取 Client Token 列表
// @Summary 获取 Client Token 列表
// @Tags Client Token
// @Produce json
// @Param user_id query int false "用户 ID"
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} Response{data=[]model.ClientToken}
// @Router /api/v1/client/tokens [get]
func ListClientTokens(c *gin.Context) {
	ctx := c.Request.Context()

	userIDStr := c.Query("user_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	query := db.WithContext(ctx).Model(&model.ClientToken{})

	// 按用户筛选
	if userIDStr != "" {
		userID, _ := strconv.ParseUint(userIDStr, 10, 64)
		query = query.Where("user_id = ?", userID)
	}

	// 统计总数
	var total int64
	query.Count(&total)

	// 分页查询
	var tokens []model.ClientToken
	offset := (page - 1) * pageSize
	if err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&tokens).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("查询失败"))
		return
	}

	// 填充创建人和用户名称
	for i := range tokens {
		var admin model.Admin
		if err := db.WithContext(ctx).First(&admin, tokens[i].CreatedBy).Error; err == nil {
			tokens[i].CreatedByName = admin.Username
		}
		var user model.User
		if err := db.WithContext(ctx).First(&user, tokens[i].UserID).Error; err == nil {
			tokens[i].UserName = user.Name
		}
	}

	c.JSON(http.StatusOK, NewPagedResponse(tokens, total, page, pageSize))
}

// GetClientToken 获取单个 Client Token
// @Summary 获取单个 Client Token
// @Tags Client Token
// @Produce json
// @Param id path int true "Token ID"
// @Success 200 {object} Response{data=model.ClientToken}
// @Router /api/v1/client/token/{id} [get]
func GetClientToken(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	var token model.ClientToken
	if err := db.WithContext(ctx).First(&token, id).Error; err != nil {
		c.JSON(http.StatusNotFound, NewErrorResponse("Token 不存在"))
		return
	}

	// 填充创建人和用户名称
	var admin model.Admin
	if err := db.WithContext(ctx).First(&admin, token.CreatedBy).Error; err == nil {
		token.CreatedByName = admin.Username
	}
	var user model.User
	if err := db.WithContext(ctx).First(&user, token.UserID).Error; err == nil {
		token.UserName = user.Name
	}

	c.JSON(http.StatusOK, NewSuccessResponse(token))
}

// DeleteClientToken 删除 Client Token
// @Summary 删除 Client Token
// @Tags Client Token
// @Produce json
// @Param id path int true "Token ID"
// @Success 200 {object} Response
// @Router /api/v1/client/token/{id} [delete]
func DeleteClientToken(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("无效的 ID"))
		return
	}

	if err := db.WithContext(ctx).Delete(&model.ClientToken{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("删除失败"))
		return
	}

	c.JSON(http.StatusOK, NewSuccessMessageResponse("删除成功", nil))
}

// ClientRegister CloudIDE 注册
// @Summary CloudIDE 注册
// @Tags Client Token
// @Accept json
// @Produce json
// @Param request body ClientRegisterRequest true "请求参数"
// @Success 200 {object} Response{data=ClientRegisterResponse}
// @Router /api/v1/client/register [post]
func ClientRegister(c *gin.Context) {
	var req ClientRegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, NewErrorResponse("参数错误: "+err.Error()))
		return
	}

	ctx := c.Request.Context()

	// 查找 Token
	var clientToken model.ClientToken
	if err := db.WithContext(ctx).Where("token = ?", req.Token).First(&clientToken).Error; err != nil {
		c.JSON(http.StatusUnauthorized, NewErrorResponse("Token 无效"))
		return
	}

	// 检查 Token 是否可用
	canUse, errMsg := clientToken.CanUse(req.DeviceFingerprint)
	if !canUse {
		c.JSON(http.StatusForbidden, NewErrorResponse(errMsg))
		return
	}

	// 获取关联用户
	var user model.User
	if err := db.WithContext(ctx).First(&user, clientToken.UserID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("用户不存在"))
		return
	}

	// 首次使用，绑定设备
	if clientToken.Status == model.ClientTokenStatusPending {
		deviceName := req.DeviceName
		if deviceName == "" {
			deviceName = clientToken.DeviceName
		}
		clientToken.Bind(req.DeviceFingerprint, deviceName)
	} else {
		// 已绑定，更新最后使用时间
		clientToken.UpdateLastUsed()
	}

	// 获取系统配置
	var headscaleURL string
	var sysConfig model.SystemConfig
	if err := db.WithContext(ctx).Where("key = ?", "headscale_public_url").First(&sysConfig).Error; err == nil {
		headscaleURL = sysConfig.Value
	}

	// 保存 Token 更新
	if err := db.WithContext(ctx).Save(&clientToken).Error; err != nil {
		c.JSON(http.StatusInternalServerError, NewErrorResponse("保存 Token 失败"))
		return
	}

	// 注意：实际的 Headscale AuthKey 创建需要通过配置的 Headscale 客户端
	// 这里返回基本信息，实际集成时需要在 server.go 中注入 headscale 客户端
	c.JSON(http.StatusOK, NewSuccessResponse(ClientRegisterResponse{
		AuthKey:      "", // TODO: 需要通过 headscale 客户端创建
		HeadscaleURL: headscaleURL,
		UserName:     user.Name,
	}))
}
