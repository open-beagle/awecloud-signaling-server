package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	grpcserver "github.com/open-beagle/awecloud-signaling-server/internal/server/grpc"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// DesktopRESTAPI Desktop REST API（gRPC 降级兜底）
type DesktopRESTAPI struct {
	desktopService *grpcserver.DesktopServiceServer
	loginService   *service.DesktopLoginService
}

// NewDesktopRESTAPI 创建 Desktop REST API
func NewDesktopRESTAPI(desktopService *grpcserver.DesktopServiceServer, loginService *service.DesktopLoginService) *DesktopRESTAPI {
	return &DesktopRESTAPI{
		desktopService: desktopService,
		loginService:   loginService,
	}
}

// desktopAuth 从请求头提取 Desktop 凭证并验证
// 返回 desktopID；验证失败时直接写响应并返回 0
func (a *DesktopRESTAPI) desktopAuth(c *gin.Context) uint64 {
	idStr := c.GetHeader("X-Desktop-ID")
	secret := c.GetHeader("X-Desktop-Secret")
	if idStr == "" || secret == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "缺少认证信息"})
		return 0
	}
	desktopID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的 Desktop ID"})
		return 0
	}

	// 调用 gRPC 服务层验证凭证
	resp, err := a.desktopService.Authenticate(c.Request.Context(), &pb.DesktopAuthenticateRequest{
		DesktopId: desktopID,
		Secret:    secret,
	})
	if err != nil || !resp.Success {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "认证失败"})
		return 0
	}
	return desktopID
}

// Authenticate Desktop 认证
// POST /api/v1/desktop/authenticate
func (a *DesktopRESTAPI) Authenticate(c *gin.Context) {
	var req struct {
		DesktopID         uint64 `json:"desktop_id"`
		Secret            string `json:"secret"`
		DeviceFingerprint string `json:"device_fingerprint"`
		SystemInfo        *struct {
			OS        string `json:"os"`
			OSVersion string `json:"os_version"`
			Arch      string `json:"arch"`
			Hostname  string `json:"hostname"`
		} `json:"system_info"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求格式错误"})
		return
	}

	grpcReq := &pb.DesktopAuthenticateRequest{
		DesktopId:         req.DesktopID,
		Secret:            req.Secret,
		DeviceFingerprint: req.DeviceFingerprint,
	}
	if req.SystemInfo != nil {
		grpcReq.SystemInfo = &pb.DesktopSystemInfo{
			Os:        req.SystemInfo.OS,
			OsVersion: req.SystemInfo.OSVersion,
			Arch:      req.SystemInfo.Arch,
			Hostname:  req.SystemInfo.Hostname,
		}
	}

	resp, err := a.desktopService.Authenticate(c.Request.Context(), grpcReq)
	if err != nil {
		logger.Errorf("[DesktopREST] Authenticate 错误: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "服务器内部错误"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    resp.Success,
		"message":    resp.Message,
		"auth_key":   resp.AuthKey,
		"server_url": resp.ServerUrl,
	})
}

// CreateLoginSession 创建登录会话
// POST /api/v1/desktop/create-login-session
func (a *DesktopRESTAPI) CreateLoginSession(c *gin.Context) {
	var req struct {
		UsernameHint      string `json:"username_hint"`
		DeviceFingerprint string `json:"device_fingerprint"`
		DeviceName        string `json:"device_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求格式错误"})
		return
	}

	resp, err := a.desktopService.CreateLoginSession(c.Request.Context(), &pb.CreateLoginSessionRequest{
		UsernameHint:      req.UsernameHint,
		DeviceFingerprint: req.DeviceFingerprint,
		DeviceName:        req.DeviceName,
	})
	if err != nil {
		logger.Errorf("[DesktopREST] CreateLoginSession 错误: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "服务器内部错误"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    resp.Success,
		"message":    resp.Message,
		"session_id": resp.SessionId,
		"login_url":  resp.LoginUrl,
	})
}

// GetLoginResult 轮询登录结果（替代 gRPC 双向流 WaitForLoginResult）
// GET /api/v1/desktop/login-result/:session_id
func (a *DesktopRESTAPI) GetLoginResult(c *gin.Context) {
	sessionID := c.Param("session_id")
	deviceFingerprint := c.Query("device_fingerprint")

	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 session_id"})
		return
	}

	if a.loginService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "登录服务未初始化"})
		return
	}

	// 查询登录会话状态
	result, err := a.loginService.GetLoginResult(sessionID, deviceFingerprint)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  "pending",
			"message": "等待登录",
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// Heartbeat 单次心跳上报（替代 gRPC 双向流）
// POST /api/v1/desktop/heartbeat
func (a *DesktopRESTAPI) Heartbeat(c *gin.Context) {
	desktopID := a.desktopAuth(c)
	if desktopID == 0 {
		return
	}

	var req struct {
		TunnelIP        string `json:"tunnel_ip"`
		TunnelConnected bool   `json:"tunnel_connected"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求格式错误"})
		return
	}

	// 直接调用心跳处理逻辑（复用 gRPC 服务层的 handleDesktopHeartbeat）
	a.desktopService.HandleDesktopHeartbeatREST(c.Request.Context(), desktopID, req.TunnelIP, req.TunnelConnected)

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetData 拉取业务数据（替代 gRPC 双向流 DataStream）
// GET /api/v1/desktop/data
func (a *DesktopRESTAPI) GetData(c *gin.Context) {
	desktopID := a.desktopAuth(c)
	if desktopID == 0 {
		return
	}

	data, err := a.desktopService.GetDataSnapshotREST(c.Request.Context(), desktopID)
	if err != nil {
		logger.Errorf("[DesktopREST] GetData 错误: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取数据失败"})
		return
	}

	c.JSON(http.StatusOK, data)
}

// Logout 注销
// POST /api/v1/desktop/logout
func (a *DesktopRESTAPI) Logout(c *gin.Context) {
	desktopID := a.desktopAuth(c)
	if desktopID == 0 {
		return
	}

	resp, err := a.desktopService.Logout(c.Request.Context(), &pb.DesktopLogoutRequest{
		DesktopId: desktopID,
	})
	if err != nil {
		logger.Errorf("[DesktopREST] Logout 错误: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "注销失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    resp.Success,
		"message":    resp.Message,
		"logout_url": resp.LogoutUrl,
	})
}

// GetHosts 获取主机列表
// GET /api/v1/desktop/hosts
func (a *DesktopRESTAPI) GetHosts(c *gin.Context) {
	desktopID := a.desktopAuth(c)
	if desktopID == 0 {
		return
	}

	resp, err := a.desktopService.GetAuthorizedHosts(c.Request.Context(), &pb.GetAuthorizedHostsRequest{
		DesktopId: desktopID,
	})
	if err != nil {
		logger.Errorf("[DesktopREST] GetHosts 错误: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取主机列表失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "hosts": resp.Hosts})
}

// GetHostServices 获取主机服务
// GET /api/v1/desktop/hosts/:host_id/services
func (a *DesktopRESTAPI) GetHostServices(c *gin.Context) {
	desktopID := a.desktopAuth(c)
	if desktopID == 0 {
		return
	}

	resp, err := a.desktopService.GetHostServices(c.Request.Context(), &pb.GetHostServicesRequest{
		DesktopId: desktopID,
		HostId:    c.Param("host_id"),
	})
	if err != nil {
		logger.Errorf("[DesktopREST] GetHostServices 错误: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取主机服务失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "services": resp.Services})
}

// GetDevices 获取设备列表
// GET /api/v1/desktop/devices
func (a *DesktopRESTAPI) GetDevices(c *gin.Context) {
	desktopID := a.desktopAuth(c)
	if desktopID == 0 {
		return
	}

	resp, err := a.desktopService.GetMyDevices(c.Request.Context(), &pb.GetMyDevicesRequest{
		DesktopId: desktopID,
	})
	if err != nil {
		logger.Errorf("[DesktopREST] GetDevices 错误: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取设备列表失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "devices": resp.Devices})
}

// OfflineDevice 设备下线
// POST /api/v1/desktop/devices/:token/offline
func (a *DesktopRESTAPI) OfflineDevice(c *gin.Context) {
	desktopID := a.desktopAuth(c)
	if desktopID == 0 {
		return
	}

	resp, err := a.desktopService.OfflineDevice(c.Request.Context(), &pb.OfflineDeviceRequest{
		DesktopId:   desktopID,
		DeviceToken: c.Param("token"),
	})
	if err != nil {
		logger.Errorf("[DesktopREST] OfflineDevice 错误: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "设备下线失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": resp.Success, "message": resp.Message})
}

// DeleteDevice 删除设备
// DELETE /api/v1/desktop/devices/:token
func (a *DesktopRESTAPI) DeleteDevice(c *gin.Context) {
	desktopID := a.desktopAuth(c)
	if desktopID == 0 {
		return
	}

	resp, err := a.desktopService.DeleteDevice(c.Request.Context(), &pb.DeleteDeviceRequest{
		DesktopId:   desktopID,
		DeviceToken: c.Param("token"),
	})
	if err != nil {
		logger.Errorf("[DesktopREST] DeleteDevice 错误: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "删除设备失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": resp.Success, "message": resp.Message})
}

// ToggleFavorite 切换收藏
// POST /api/v1/desktop/favorites/toggle
func (a *DesktopRESTAPI) ToggleFavorite(c *gin.Context) {
	desktopID := a.desktopAuth(c)
	if desktopID == 0 {
		return
	}

	var req struct {
		ServiceID string `json:"service_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求格式错误"})
		return
	}

	resp, err := a.desktopService.ToggleFavorite(c.Request.Context(), &pb.ToggleFavoriteRequest{
		DesktopId: desktopID,
		ServiceId: req.ServiceID,
	})
	if err != nil {
		logger.Errorf("[DesktopREST] ToggleFavorite 错误: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "切换收藏失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": resp.Success, "is_favorite": resp.IsFavorite})
}

// GetFavorites 获取收藏列表
// GET /api/v1/desktop/favorites
func (a *DesktopRESTAPI) GetFavorites(c *gin.Context) {
	desktopID := a.desktopAuth(c)
	if desktopID == 0 {
		return
	}

	resp, err := a.desktopService.GetFavoriteServices(c.Request.Context(), &pb.GetFavoriteServicesRequest{
		DesktopId: desktopID,
	})
	if err != nil {
		logger.Errorf("[DesktopREST] GetFavorites 错误: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取收藏列表失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "service_ids": resp.ServiceIds})
}

// ResolveDomain 域名解析
// GET /api/v1/desktop/resolve-domain?domain=xxx
func (a *DesktopRESTAPI) ResolveDomain(c *gin.Context) {
	desktopID := a.desktopAuth(c)
	if desktopID == 0 {
		return
	}

	domain := c.Query("domain")
	if domain == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 domain 参数"})
		return
	}

	resp, err := a.desktopService.ResolveDomain(c.Request.Context(), &pb.ResolveDomainRequest{
		DesktopId: desktopID,
		Domain:    domain,
	})
	if err != nil {
		logger.Errorf("[DesktopREST] ResolveDomain 错误: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "域名解析失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":        resp.Success,
		"message":        resp.Message,
		"domain":         resp.Domain,
		"agent_ip":       resp.AgentIp,
		"target_port":    resp.TargetPort,
		"agent_name":     resp.AgentName,
		"domain_type":    resp.DomainType,
		"namespace":      resp.Namespace,
		"service_name":   resp.ServiceName,
		"svc_proxy_port": resp.SvcProxyPort,
	})
}

// GetResources 资源发现
// GET /api/v1/desktop/resources
func (a *DesktopRESTAPI) GetResources(c *gin.Context) {
	desktopID := a.desktopAuth(c)
	if desktopID == 0 {
		return
	}

	resp, err := a.desktopService.GetResources(c.Request.Context(), &pb.GetResourcesRequest{
		DesktopId: desktopID,
	})
	if err != nil {
		logger.Errorf("[DesktopREST] GetResources 错误: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取资源列表失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"ssh":           resp.Ssh,
		"k8s_api":       resp.K8SApi,
		"k8s_service":   resp.K8SService,
		"container_ssh": resp.ContainerSsh,
	})
}

// GetDomains 域名列表
// GET /api/v1/desktop/domains
func (a *DesktopRESTAPI) GetDomains(c *gin.Context) {
	desktopID := a.desktopAuth(c)
	if desktopID == 0 {
		return
	}

	resp, err := a.desktopService.GetDomainList(c.Request.Context(), &pb.GetDomainListRequest{
		DesktopId: desktopID,
	})
	if err != nil {
		logger.Errorf("[DesktopREST] GetDomains 错误: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取域名列表失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "domains": resp.Domains})
}
