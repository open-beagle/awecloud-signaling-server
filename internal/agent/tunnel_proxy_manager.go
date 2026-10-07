package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// TunnelDialer 拨号函数抽象，支持 tsnet 拨号或测试 Mock 拨号
type TunnelDialer func(ctx context.Context, network, addr string) (net.Conn, error)

// TunnelResourceStatus 隧道资源运行状态与探针指标
type TunnelResourceStatus struct {
	ResourceID          string `json:"resource_id"`
	ServiceName         string `json:"service_name"`
	Namespace           string `json:"namespace"`
	LocalPort           int32  `json:"local_port"`
	PortNumber          int32  `json:"port_number"`
	Path                string `json:"path"` // "svcproxy" | "host_direct"
	SVCProxyOK          int64  `json:"svcproxy_ok"`
	SVCProxyRejected    int64  `json:"svcproxy_rejected"`
	LastSVCProxyError   string `json:"last_svcproxy_error,omitempty"`
	DirectDialViolation int64  `json:"direct_dial_violation"`
}

// TunnelProxyManager 专用出站隧道管理器
// 遵循显式 local_port 白名单驱动与 1:1 对等绑定原则：
// 1. 严格过滤只处理属于 targetAgent 的资源；
// 2. 仅当 LocalPort > 0 时才监听 0.0.0.0:<LocalPort>；
// 3. 不分配 VIP，不改写 DNS，支持非 root 运行。
type TunnelProxyManager struct {
	targetAgent string // 绑定的目标 Agent 名称
	tsManager   *TailscaleManager
	dialer      TunnelDialer

	listeners map[int]net.Listener
	resources map[int]*pb.ContainerServiceResource
	statusMap map[string]*TunnelResourceStatus
	statusServer *http.Server
	mu        sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewTunnelProxyManager 创建专用出站隧道管理器
func NewTunnelProxyManager(targetAgent string, tsManager *TailscaleManager, parentCtx context.Context) *TunnelProxyManager {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithCancel(parentCtx)
	return &TunnelProxyManager{
		targetAgent: targetAgent,
		tsManager:   tsManager,
		listeners:   make(map[int]net.Listener),
		resources:   make(map[int]*pb.ContainerServiceResource),
		statusMap:   make(map[string]*TunnelResourceStatus),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// SetDialer 设置自定义拨号器（用于单元测试或特殊网络路由）
func (m *TunnelProxyManager) SetDialer(dialer TunnelDialer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dialer = dialer
}

// TargetAgent 获取绑定的目标 Agent 名称
func (m *TunnelProxyManager) TargetAgent() string {
	return m.targetAgent
}

// SyncTunnelProxies 响应 Server 资源下发事件
func (m *TunnelProxyManager) SyncTunnelProxies(resources []*pb.ContainerServiceResource) {
	m.mu.Lock()
	defer m.mu.Unlock()

	activePorts := make(map[int]bool)
	activeResourceIDs := make(map[string]bool)

	for _, res := range resources {
		if res == nil {
			continue
		}

		// 1. 校验：严格过滤只处理属于目标 Agent 的资源（保障 1:1 专线边界）
		if m.targetAgent != "" && res.AgentName != "" && res.AgentName != m.targetAgent {
			continue
		}

		// 2. 核心原则：只有显式设定了 LocalPort > 0 才开放监听
		if res.LocalPort <= 0 {
			continue
		}

		port := int(res.LocalPort)
		activePorts[port] = true
		activeResourceIDs[res.ResourceId] = true
		m.resources[port] = res

		path := "svcproxy"
		if isHostDirectService(res) {
			path = "host_direct"
		}
		if st, exists := m.statusMap[res.ResourceId]; !exists {
			m.statusMap[res.ResourceId] = &TunnelResourceStatus{
				ResourceID:  res.ResourceId,
				ServiceName: res.ServiceName,
				Namespace:   res.Namespace,
				LocalPort:   res.LocalPort,
				PortNumber:  res.PortNumber,
				Path:        path,
			}
		} else {
			st.ServiceName = res.ServiceName
			st.Namespace = res.Namespace
			st.LocalPort = res.LocalPort
			st.PortNumber = res.PortNumber
			st.Path = path
		}

		if _, exists := m.listeners[port]; !exists {
			listenAddr := fmt.Sprintf("0.0.0.0:%d", port)
			listener, err := net.Listen("tcp", listenAddr)
			if err != nil {
				logger.Errorf("[Tunnel] 监听本地端口失败 %s: %v", listenAddr, err)
				continue
			}
			m.listeners[port] = listener
			logger.Infof("[Tunnel] 成功为服务 %s 开启出站代理监听: %s -> 边缘节点 (target=%s:%d)",
				res.ServiceName, listenAddr, res.AgentIp, res.PortNumber)

			m.wg.Add(1)
			go m.serveConn(listener, port)
		}
	}

	// 自动清理已被取消 LocalPort 或撤销授权的废弃监听器与状态项
	for port, listener := range m.listeners {
		if !activePorts[port] {
			listener.Close()
			delete(m.listeners, port)
			delete(m.resources, port)
			logger.Infof("[Tunnel] 资源授权已变更，停止本地监听端口: %d", port)
		}
	}
	for resID := range m.statusMap {
		if !activeResourceIDs[resID] {
			delete(m.statusMap, resID)
		}
	}
}

// GetActivePorts 获取当前处于活跃监听状态的端口列表（已排序）
func (m *TunnelProxyManager) GetActivePorts() []int {
	m.mu.Lock()
	defer m.mu.Unlock()

	ports := make([]int, 0, len(m.listeners))
	for p := range m.listeners {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports
}

// GetListener 获取指定端口的 Listener（供测试验证）
func (m *TunnelProxyManager) GetListener(port int) net.Listener {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listeners[port]
}

// serveConn 接受本地连接并异步分发转发协程
func (m *TunnelProxyManager) serveConn(listener net.Listener, port int) {
	defer m.wg.Done()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-m.ctx.Done():
				return
			default:
				// Listener 被主动关闭
				return
			}
		}

		m.mu.Lock()
		res := m.resources[port]
		m.mu.Unlock()

		if res == nil {
			conn.Close()
			continue
		}

		go m.handleConn(conn, res)
	}
}

// dialTarget 统一拨号到目标地址
func (m *TunnelProxyManager) dialTarget(ctx context.Context, network, addr string) (net.Conn, error) {
	m.mu.Lock()
	customDialer := m.dialer
	tsMgr := m.tsManager
	m.mu.Unlock()

	if customDialer != nil {
		return customDialer(ctx, network, addr)
	}
	if tsMgr != nil {
		return tsMgr.Dial(ctx, network, addr)
	}
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

// isHostDirectService 判断是否为宿主机原生控制面服务（仅限 Kubernetes API Server，精确匹配 ResourceId == "k8s-api"）
func isHostDirectService(res *pb.ContainerServiceResource) bool {
	if res == nil {
		return false
	}
	return res.ResourceId == "k8s-api"
}

// handleConn 处理单个本地入站连接，路由转发至对端
func (m *TunnelProxyManager) handleConn(clientConn net.Conn, res *pb.ContainerServiceResource) {
	defer clientConn.Close()

	ctx, cancel := context.WithCancel(m.ctx)
	defer cancel()

	// 路径 1: 宿主机原生控制面服务（仅限 Kubernetes API Server 6443），采用直接 TCP 通道转发到宿主机固定 6443 端口
	if isHostDirectService(res) {
		targetAddr := fmt.Sprintf("%s:%d", res.AgentIp, 6443)
		targetConn, err := m.dialTarget(ctx, "tcp", targetAddr)
		if err != nil {
			logger.Errorf("[Tunnel] 拨号边缘宿主机服务目标失败 (%s -> %s): %v", res.ServiceName, targetAddr, err)
			m.recordStatusReject(res.ResourceId, err.Error())
			return
		}
		defer targetConn.Close()

		m.recordStatusSuccess(res.ResourceId)
		m.bridgeConns(clientConn, targetConn)
		return
	}

	// 路径 2: Kubernetes Service（ClusterIP 业务应用），遵循与 Desktop 一致的规范：
	// 严禁直连物理节点 AgentIp:PortNumber，必须且只能通过 Agent 的 gRPC SVCProxy 双向流代理到集群内网 ClusterIP
	if res.SvcProxyPort <= 0 || res.AgentIp == "" {
		logger.Errorf("[Tunnel] 容器服务 %s (id=%s) 未配置 SvcProxyPort 或 AgentIp，禁止盲目直连", res.ServiceName, res.ResourceId)
		m.recordStatusReject(res.ResourceId, "未配置 SvcProxyPort 或 AgentIp")
		return
	}

	grpcAddr := fmt.Sprintf("%s:%d", res.AgentIp, res.SvcProxyPort)
	grpcConn, err := grpc.NewClient(
		grpcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(dialCtx context.Context, addr string) (net.Conn, error) {
			return m.dialTarget(dialCtx, "tcp", addr)
		}),
	)
	if err != nil {
		logger.Errorf("[Tunnel] 连接 Agent gRPC 代理端口失败 (%s): %v", grpcAddr, err)
		m.recordStatusReject(res.ResourceId, err.Error())
		return
	}
	defer grpcConn.Close()

	svcClient := pb.NewAgentServiceClient(grpcConn)

	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	stream, streamErr := svcClient.SVCProxy(streamCtx)
	if streamErr != nil {
		logger.Errorf("[Tunnel] 创建 Agent SVCProxy 流失败 (%s -> %s): %v", res.ServiceName, grpcAddr, streamErr)
		m.recordStatusReject(res.ResourceId, streamErr.Error())
		return
	}

	// 发送首包建立连接
	firstMsg := &pb.SVCProxyData{
		Namespace:             res.Namespace,
		ServiceName:           res.ServiceName,
		Port:                  res.PortNumber,
		IsConnect:             true,
		SessionId:             res.SessionId,
		ResourceId:            res.ResourceId,
		SourceId:              res.SourceId,
		TargetRevisionId:      res.TargetRevisionId,
		ServiceUid:            res.ServiceUid,
		PortName:              res.PortName,
		Protocol:              res.Protocol,
		AuthorizationRevision: res.AuthorizationRevision,
	}
	if sendErr := stream.Send(firstMsg); sendErr == nil {
		// 等待对端首包确认（快速错误排查）
		firstRespCh := make(chan *pb.SVCProxyData, 1)
		firstErrCh := make(chan error, 1)
		go func() {
			resp, err := stream.Recv()
			if err != nil {
				firstErrCh <- err
				return
			}
			firstRespCh <- resp
		}()

		select {
		case resp := <-firstRespCh:
			if resp.Error != "" {
				logger.Warnf("[Tunnel] 对端 Agent 拒绝连接 (%s): %s", res.ServiceName, resp.Error)
				m.recordStatusReject(res.ResourceId, resp.Error)
				return
			}
			m.recordStatusSuccess(res.ResourceId)
			if len(resp.Data) > 0 {
				_, _ = clientConn.Write(resp.Data)
			}
			if resp.IsClose {
				return
			}
		case err := <-firstErrCh:
			logger.Warnf("[Tunnel] 对端 Agent 首包接收失败 (%s): %v", res.ServiceName, err)
			m.recordStatusReject(res.ResourceId, err.Error())
			return
		case <-time.After(5 * time.Second):
			// 握手正常建立
			m.recordStatusSuccess(res.ResourceId)
		}

		m.bridgeStream(clientConn, stream)
		return
	} else {
		logger.Errorf("[Tunnel] 发送 SVCProxy 首包失败 (%s): %v", res.ServiceName, sendErr)
		m.recordStatusReject(res.ResourceId, sendErr.Error())
	}
}

// recordStatusSuccess 记录成功代理
func (m *TunnelProxyManager) recordStatusSuccess(resourceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.statusMap[resourceID]; ok {
		st.SVCProxyOK++
	}
}

// recordStatusReject 记录被拒绝或失败
func (m *TunnelProxyManager) recordStatusReject(resourceID, errStr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.statusMap[resourceID]; ok {
		st.SVCProxyRejected++
		st.LastSVCProxyError = errStr
	}
}

// RecordDirectDialViolation 记录非 k8s-api 资源违规直连的守卫计数
func (m *TunnelProxyManager) RecordDirectDialViolation(resourceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.statusMap[resourceID]; ok {
		st.DirectDialViolation++
	}
}

// GetStatuses 获取所有隧道资源当前状态列表
func (m *TunnelProxyManager) GetStatuses() []*TunnelResourceStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*TunnelResourceStatus, 0, len(m.statusMap))
	for _, st := range m.statusMap {
		cp := *st
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Namespace != result[j].Namespace {
			return result[i].Namespace < result[j].Namespace
		}
		return result[i].ServiceName < result[j].ServiceName
	})
	return result
}

// StartStatusServer 在指定地址（默认 127.0.0.1:19090）启动只读 /statusz 端点
func (m *TunnelProxyManager) StartStatusServer(addr string) error {
	if addr == "" {
		addr = "127.0.0.1:19090"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/statusz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(m.GetStatuses())
	})

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.statusServer = server
	m.mu.Unlock()

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Warnf("[Tunnel] /statusz 服务退出: %v", err)
		}
	}()

	logger.Infof("[Tunnel] 成功启动 /statusz 端点: http://%s/statusz", addr)
	return nil
}

// bridgeStream 双向桥接 TCP 连接与 gRPC SVCProxy 流
func (m *TunnelProxyManager) bridgeStream(conn net.Conn, stream pb.AgentService_SVCProxyClient) {
	var wg sync.WaitGroup
	wg.Add(2)

	// conn -> stream
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				if sendErr := stream.Send(&pb.SVCProxyData{Data: buf[:n]}); sendErr != nil {
					return
				}
			}
			if err != nil {
				_ = stream.Send(&pb.SVCProxyData{IsClose: true})
				_ = stream.CloseSend()
				return
			}
		}
	}()

	// stream -> conn
	go func() {
		defer wg.Done()
		for {
			msg, err := stream.Recv()
			if err != nil {
				return
			}
			if msg.Error != "" {
				logger.Warnf("[Tunnel] 对端返回错误: %s", msg.Error)
				return
			}
			if msg.IsClose {
				return
			}
			if len(msg.Data) > 0 {
				if _, writeErr := conn.Write(msg.Data); writeErr != nil {
					return
				}
			}
		}
	}()

	wg.Wait()
}

// bridgeConns 双向桥接两个纯 TCP 连接
func (m *TunnelProxyManager) bridgeConns(client, target net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	// client -> target
	go func() {
		defer wg.Done()
		_, _ = io.Copy(target, client)
		if tc, ok := target.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()

	// target -> client
	go func() {
		defer wg.Done()
		_, _ = io.Copy(client, target)
		if cc, ok := client.(*net.TCPConn); ok {
			_ = cc.CloseWrite()
		}
	}()

	wg.Wait()
}

// Stop 停止出站隧道管理器并释放所有端口
func (m *TunnelProxyManager) Stop() {
	m.cancel()

	m.mu.Lock()
	if m.statusServer != nil {
		_ = m.statusServer.Close()
		m.statusServer = nil
	}
	for port, listener := range m.listeners {
		_ = listener.Close()
		delete(m.listeners, port)
		delete(m.resources, port)
	}
	m.mu.Unlock()

	m.wg.Wait()
	logger.Info("[Tunnel] 出站隧道管理器已停止，所有本地端口已释放")
}
