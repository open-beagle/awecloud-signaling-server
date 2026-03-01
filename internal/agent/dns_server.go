// Package agent 提供 Agent 端功能
// dns_server.go 提供本地 DNS 服务器，拦截 .beagle 域名解析
package agent

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// DNSResolveFunc 域名解析回调函数
// 输入域名（不含末尾点），返回 VIP 地址和是否成功
type DNSResolveFunc func(domain string) (vip string, ok bool)

// DNSServer 本地 DNS 服务器
type DNSServer struct {
	listenAddr  string
	conn        *net.UDPConn
	resolve     DNSResolveFunc
	upstreamDNS string // 上游 DNS 地址（用于转发非 .beagle 域名）

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewDNSServer 创建 DNS 服务器
func NewDNSServer(listenAddr string, resolve DNSResolveFunc, upstreamDNS string) *DNSServer {
	if upstreamDNS == "" {
		upstreamDNS = "8.8.8.8:53"
	}
	return &DNSServer{
		listenAddr:  listenAddr,
		resolve:     resolve,
		upstreamDNS: upstreamDNS,
		stopCh:      make(chan struct{}),
	}
}

// Start 启动 DNS 服务器
func (s *DNSServer) Start() error {
	addr, err := net.ResolveUDPAddr("udp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("解析监听地址失败: %w", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("监听 DNS 端口失败: %w", err)
	}

	s.conn = conn
	logger.Infof("[DNS] 本地 DNS 服务器已启动: %s (上游: %s)", s.listenAddr, s.upstreamDNS)

	s.wg.Add(1)
	go s.serve()

	return nil
}

// Stop 停止 DNS 服务器
func (s *DNSServer) Stop() {
	close(s.stopCh)
	if s.conn != nil {
		s.conn.Close()
	}
	s.wg.Wait()
	logger.Info("[DNS] 本地 DNS 服务器已停止")
}

// serve 处理 DNS 请求
func (s *DNSServer) serve() {
	defer s.wg.Done()

	buf := make([]byte, 512)
	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		n, remoteAddr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-s.stopCh:
				return
			default:
				logger.Debugf("[DNS] 读取请求失败: %v", err)
				continue
			}
		}

		// 异步处理请求
		packet := make([]byte, n)
		copy(packet, buf[:n])
		go s.handleQuery(packet, remoteAddr)
	}
}

// handleQuery 处理单个 DNS 查询
func (s *DNSServer) handleQuery(packet []byte, remoteAddr *net.UDPAddr) {
	// 解析查询域名
	domain, qtype, err := parseDNSQuestion(packet)
	if err != nil {
		logger.Debugf("[DNS] 解析查询失败: %v", err)
		return
	}

	// 去掉末尾的点
	domain = strings.TrimSuffix(domain, ".")

	// 检查是否是 .beagle 域名
	if strings.HasSuffix(domain, ".beagle") && qtype == 1 { // A 记录
		vip, ok := s.resolve(domain)
		if ok {
			resp := buildDNSResponse(packet, vip)
			s.conn.WriteToUDP(resp, remoteAddr)
			logger.Infof("[DNS] 解析: %s → %s", domain, vip)
			return
		}
		// 域名未注册，返回 NXDOMAIN
		resp := buildDNSNXDomain(packet)
		s.conn.WriteToUDP(resp, remoteAddr)
		logger.Debugf("[DNS] 域名未注册: %s", domain)
		return
	}

	// 非 .beagle 域名，转发到上游 DNS
	resp, err := s.forwardToUpstream(packet)
	if err != nil {
		logger.Debugf("[DNS] 转发到上游失败 (%s): %v", domain, err)
		resp = buildDNSServerFailure(packet)
	}
	s.conn.WriteToUDP(resp, remoteAddr)
}

// forwardToUpstream 转发查询到上游 DNS
func (s *DNSServer) forwardToUpstream(packet []byte) ([]byte, error) {
	upstreamAddr, err := net.ResolveUDPAddr("udp", s.upstreamDNS)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialUDP("udp", nil, upstreamAddr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if _, err := conn.Write(packet); err != nil {
		return nil, err
	}

	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}

	return buf[:n], nil
}

// parseDNSQuestion 从 DNS 报文中解析查询域名和类型
func parseDNSQuestion(packet []byte) (string, uint16, error) {
	if len(packet) < 12 {
		return "", 0, fmt.Errorf("报文太短")
	}

	// 跳过 DNS 头部（12 字节）
	offset := 12
	var parts []string

	for offset < len(packet) {
		length := int(packet[offset])
		if length == 0 {
			offset++
			break
		}
		offset++
		if offset+length > len(packet) {
			return "", 0, fmt.Errorf("域名标签越界")
		}
		parts = append(parts, string(packet[offset:offset+length]))
		offset += length
	}

	if offset+4 > len(packet) {
		return "", 0, fmt.Errorf("查询类型越界")
	}

	qtype := binary.BigEndian.Uint16(packet[offset : offset+2])
	domain := strings.Join(parts, ".")

	return domain, qtype, nil
}

// buildDNSResponse 构建 DNS A 记录响应
func buildDNSResponse(query []byte, ip string) []byte {
	parsedIP := net.ParseIP(ip).To4()
	if parsedIP == nil {
		return buildDNSServerFailure(query)
	}

	// 复制查询报文作为响应基础
	resp := make([]byte, len(query))
	copy(resp, query)

	// 设置响应标志
	resp[2] = 0x81 // QR=1, Opcode=0, AA=1
	resp[3] = 0x80 // RA=1, RCODE=0 (No Error)

	// 设置 Answer Count = 1
	resp[6] = 0x00
	resp[7] = 0x01

	// 追加 Answer 段
	// Name: 指针指向 Question 中的域名（offset 12）
	answer := []byte{
		0xc0, 0x0c, // 名称指针
		0x00, 0x01, // Type: A
		0x00, 0x01, // Class: IN
		0x00, 0x00, 0x00, 0x3c, // TTL: 60 秒
		0x00, 0x04, // RDLENGTH: 4
	}
	answer = append(answer, parsedIP...)

	return append(resp, answer...)
}

// buildDNSNXDomain 构建 NXDOMAIN 响应
func buildDNSNXDomain(query []byte) []byte {
	resp := make([]byte, len(query))
	copy(resp, query)
	resp[2] = 0x81 // QR=1, AA=1
	resp[3] = 0x83 // RA=1, RCODE=3 (NXDOMAIN)
	return resp
}

// buildDNSServerFailure 构建 Server Failure 响应
func buildDNSServerFailure(query []byte) []byte {
	resp := make([]byte, len(query))
	copy(resp, query)
	resp[2] = 0x81 // QR=1, AA=1
	resp[3] = 0x82 // RA=1, RCODE=2 (Server Failure)
	return resp
}
