// Package agent 提供 Agent 端功能
// endpoint_k8sapi.go 实现 Endpoint K8S API 代理（gRPC 方案）
// Agent 作为 HTTP 反向代理接收 Desktop 请求，通过 gRPC OpenK8SAPIProxy 转发到 Endpoint
//
// 架构：
//
//	Desktop → tsnet → Agent(FallbackTCPHandler, 端口 50153+N) → TCP 透传 → gRPC OpenK8SAPIProxy → Endpoint → K8S API Server
//	每个 Endpoint 分配一个独立端口（50153 起），Agent 根据端口号确定目标 Endpoint
//	Desktop 端无需修改，Server ResolveDomain 返回 agent_ip:分配端口 即可
package agent

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// EndpointK8SAPIPortBase Endpoint K8S API 代理的起始端口
const EndpointK8SAPIPortBase = 50153

// EndpointK8SAPIProxy Endpoint K8S API 代理
// 在 Agent 内部运行 TCP 代理，接收 Desktop HTTPS 连接
// 通过 gRPC OpenK8SAPIProxy 转发到 Endpoint 的 K8S API Server
type EndpointK8SAPIProxy struct {
	endpointServer *EndpointServer
	tsManager      *TailscaleManager
	permCache      *PermissionCache
	auditCollector *AuditCollector

	// 端口 → Endpoint 名称映射
	portMap map[uint16]string
	// Endpoint 名称 → 端口映射
	nameMap map[string]uint16
	mapMu   sync.RWMutex

	// 下一个可分配的端口
	nextPort uint16

	// fallback handler 取消注册函数
	deregisterFallback func()

	ctx    context.Context
	cancel context.CancelFunc
}

// NewEndpointK8SAPIProxy 创建 Endpoint K8S API 代理
func NewEndpointK8SAPIProxy(endpointServer *EndpointServer, tsManager *TailscaleManager, permCache *PermissionCache, auditCollector *AuditCollector, parentCtx context.Context) *EndpointK8SAPIProxy {
	ctx, cancel := context.WithCancel(parentCtx)
	return &EndpointK8SAPIProxy{
		endpointServer: endpointServer,
		tsManager:      tsManager,
		permCache:      permCache,
		auditCollector: auditCollector,
		portMap:        make(map[uint16]string),
		nameMap:        make(map[string]uint16),
		nextPort:       EndpointK8SAPIPortBase,
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Start 启动 Endpoint K8S API 代理
func (p *EndpointK8SAPIProxy) Start() error {
	// 注册 fallback TCP handler，拦截 Endpoint K8SAPI 端口范围的连接
	deregister := p.tsManager.RegisterFallbackTCPHandler(func(src, dst netip.AddrPort) (func(net.Conn), bool) {
		port := dst.Port()
		if port >= EndpointK8SAPIPortBase && port < EndpointK8SAPIPortBase+100 {
			p.mapMu.RLock()
			endpointName, exists := p.portMap[port]
			p.mapMu.RUnlock()

			if !exists {
				return nil, false
			}

			return func(conn net.Conn) {
				go p.handleConn(conn, endpointName)
			}, true
		}
		return nil, false
	})
	p.deregisterFallback = deregister

	logger.Infof("[EndpointK8SAPI] 代理已启动（FallbackTCPHandler）: 端口范围 %d-%d", EndpointK8SAPIPortBase, EndpointK8SAPIPortBase+99)
	return nil
}

// Stop 停止 Endpoint K8S API 代理
func (p *EndpointK8SAPIProxy) Stop() {
	p.cancel()
	if p.deregisterFallback != nil {
		p.deregisterFallback()
	}
	logger.Info("[EndpointK8SAPI] 代理已停止")
}

// AllocatePort 为 Endpoint 分配端口
func (p *EndpointK8SAPIProxy) AllocatePort(endpointName string) uint16 {
	p.mapMu.Lock()
	defer p.mapMu.Unlock()

	if port, exists := p.nameMap[endpointName]; exists {
		return port
	}

	port := p.nextPort
	p.nextPort++
	p.portMap[port] = endpointName
	p.nameMap[endpointName] = port

	logger.Infof("[EndpointK8SAPI] 分配端口: %s → %d", endpointName, port)
	return port
}

// AllocateSpecificPort 为 Endpoint 分配指定端口（Server 预分配）
// 如果端口已被占用（且不是同一个 Endpoint），返回错误
func (p *EndpointK8SAPIProxy) AllocateSpecificPort(endpointName string, port uint16) error {
	p.mapMu.Lock()
	defer p.mapMu.Unlock()

	// 检查端口是否已被其他 Endpoint 占用
	if existingName, exists := p.portMap[port]; exists && existingName != endpointName {
		return fmt.Errorf("端口 %d 已被 %s 占用", port, existingName)
	}

	// 检查该 Endpoint 是否已分配了不同的端口
	if existingPort, exists := p.nameMap[endpointName]; exists && existingPort != port {
		// 释放旧端口
		delete(p.portMap, existingPort)
		logger.Infof("[EndpointK8SAPI] 释放旧端口: %s ← %d", endpointName, existingPort)
	}

	// 分配端口
	p.portMap[port] = endpointName
	p.nameMap[endpointName] = port
	logger.Infof("[EndpointK8SAPI] 分配指定端口: %s → %d", endpointName, port)
	return nil
}

// ReleasePort 释放 Endpoint 的端口
func (p *EndpointK8SAPIProxy) ReleasePort(endpointName string) {
	p.mapMu.Lock()
	defer p.mapMu.Unlock()

	if port, exists := p.nameMap[endpointName]; exists {
		delete(p.portMap, port)
		delete(p.nameMap, endpointName)
		logger.Infof("[EndpointK8SAPI] 释放端口: %s ← %d", endpointName, port)
	}
}

// GetPort 获取 Endpoint 的分配端口（0 表示未分配）
func (p *EndpointK8SAPIProxy) GetPort(endpointName string) uint16 {
	p.mapMu.RLock()
	defer p.mapMu.RUnlock()
	return p.nameMap[endpointName]
}

// handleConn 处理 Desktop TCP 连接（透明 TCP 代理）
// Desktop 发送 HTTP/HTTPS 请求到 Agent 分配的端口，Agent 通过 gRPC 转发到 Endpoint
func (p *EndpointK8SAPIProxy) handleConn(conn net.Conn, endpointName string) {
	defer conn.Close()
	startedAt := time.Now()

	// 提取 Desktop 用户身份
	clientUserName := ""
	if p.tsManager != nil {
		if lc, err := p.tsManager.LocalClient(); err == nil {
			if whois, err := lc.WhoIs(p.ctx, conn.RemoteAddr().String()); err == nil && whois.UserProfile != nil {
				clientUserName, _ = parseHeadscaleUserName(whois.UserProfile.LoginName)
			}
		}
	}

	// 检查权限
	if p.permCache != nil && clientUserName != "" {
		if _, allowed := p.permCache.CheckEndpointK8SAPIAccess(clientUserName, endpointName); !allowed {
			logger.Warnf("[EndpointK8SAPI] 权限拒绝: user=%s, endpoint=%s", clientUserName, endpointName)
			return
		}
	}

	if !p.endpointServer.IsEndpointConnected(endpointName) {
		logger.Warnf("[EndpointK8SAPI] Endpoint 不在线: %s", endpointName)
		return
	}

	logger.Infof("[EndpointK8SAPI] 连接: endpoint=%s, client=%s", endpointName, clientUserName)

	// 请求 Endpoint 开启 K8S API 代理（通过 gRPC）
	stream, err := p.endpointServer.RequestK8SAPIProxy(p.ctx, endpointName)
	if err != nil {
		logger.Warnf("[EndpointK8SAPI] 请求 Endpoint K8S API 代理失败: %v", err)
		return
	}

	// 设置读取超时（30 秒），等待 Desktop 发送第一个数据包
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	// 等待第一个数据包（TLS ClientHello）
	firstBuf := make([]byte, 32*1024)
	n, err := conn.Read(firstBuf)
	if err != nil {
		logger.Warnf("[EndpointK8SAPI] 等待第一个数据包失败 (endpoint=%s, client=%s): %v", endpointName, clientUserName, err)
		_ = stream.Send(&pb.K8SAPIProxyData{IsClose: true})
		return
	}

	logger.Debugf("[EndpointK8SAPI] 收到第一个数据包: %d bytes (endpoint=%s)", n, endpointName)

	// 发送第一个数据包到 Endpoint
	if err := stream.Send(&pb.K8SAPIProxyData{Data: firstBuf[:n]}); err != nil {
		logger.Warnf("[EndpointK8SAPI] 发送第一个数据包失败: %v", err)
		return
	}

	// 清除读取超时，恢复正常读取
	conn.SetReadDeadline(time.Time{})

	// 双向桥接：TCP conn ↔ gRPC stream
	var wg sync.WaitGroup
	wg.Add(2)

	// TCP → gRPC（Desktop 请求 → Endpoint）
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				if sendErr := stream.Send(&pb.K8SAPIProxyData{
					Data: buf[:n],
				}); sendErr != nil {
					logger.Debugf("[EndpointK8SAPI] gRPC 发送失败: %v", sendErr)
					return
				}
			}
			if err != nil {
				logger.Debugf("[EndpointK8SAPI] TCP 读取结束: %v", err)
				_ = stream.Send(&pb.K8SAPIProxyData{IsClose: true})
				return
			}
		}
	}()

	// gRPC → TCP（Endpoint 响应 → Desktop）
	go func() {
		defer wg.Done()
		for {
			msg, err := stream.Recv()
			if err != nil {
				logger.Debugf("[EndpointK8SAPI] gRPC 接收结束: %v", err)
				conn.Close()
				return
			}
			if msg.IsClose {
				logger.Debugf("[EndpointK8SAPI] 收到关闭信号")
				conn.Close()
				return
			}
			if msg.Error != "" {
				logger.Warnf("[EndpointK8SAPI] Endpoint 错误: %s", msg.Error)
				conn.Close()
				return
			}
			if len(msg.Data) > 0 {
				if _, writeErr := conn.Write(msg.Data); writeErr != nil {
					logger.Debugf("[EndpointK8SAPI] TCP 写入失败: %v", writeErr)
					return
				}
			}
		}
	}()

	wg.Wait()

	logger.Debugf("[EndpointK8SAPI] 连接已关闭: endpoint=%s, client=%s", endpointName, clientUserName)

	// 记录审计
	if p.auditCollector != nil {
		target := fmt.Sprintf("k8sapi@%s", endpointName)
		p.auditCollector.Record(clientUserName, endpointName, "k8s_api_request", target, "", startedAt, time.Now())
	}
}
