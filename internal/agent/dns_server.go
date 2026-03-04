// Package agent 提供 Agent 端功能
// dns_server.go 提供本地 DNS 服务器，拦截 .beagle 域名解析
package agent

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/miekg/dns"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// DNSResolveFunc 域名解析回调函数
// 输入域名（不含末尾点），返回 VIP 地址和是否成功
type DNSResolveFunc func(domain string) (vip string, ok bool)

// DNSServer 本地 DNS 服务器
type DNSServer struct {
	listenAddr  string
	server      *dns.Server
	resolve     DNSResolveFunc
	upstreamDNS string // 上游 DNS 地址（用于转发非 .beagle 域名）
	client      *dns.Client

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewDNSServer 创建 DNS 服务器
func NewDNSServer(listenAddr string, resolve DNSResolveFunc, upstreamDNS string) *DNSServer {
	if upstreamDNS == "" {
		upstreamDNS = "8.8.8.8:53"
	}

	s := &DNSServer{
		listenAddr:  listenAddr,
		resolve:     resolve,
		upstreamDNS: upstreamDNS,
		client: &dns.Client{
			Net: "udp",
		},
		stopCh: make(chan struct{}),
	}

	// 创建 DNS 服务器
	s.server = &dns.Server{
		Addr:    listenAddr,
		Net:     "udp",
		Handler: dns.HandlerFunc(s.handleQuery),
	}

	return s
}

// Start 启动 DNS 服务器
func (s *DNSServer) Start() error {
	logger.Infof("[DNS] 本地 DNS 服务器已启动: %s (上游: %s)", s.listenAddr, s.upstreamDNS)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.server.ListenAndServe(); err != nil {
			select {
			case <-s.stopCh:
				// 正常关闭
			default:
				logger.Errorf("[DNS] 服务器错误: %v", err)
			}
		}
	}()

	return nil
}

// Stop 停止 DNS 服务器
func (s *DNSServer) Stop() {
	close(s.stopCh)
	if s.server != nil {
		s.server.Shutdown()
	}
	s.wg.Wait()
	logger.Info("[DNS] 本地 DNS 服务器已停止")
}

// handleQuery 处理 DNS 查询
func (s *DNSServer) handleQuery(w dns.ResponseWriter, r *dns.Msg) {
	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.RecursionAvailable = true

	// 处理每个查询问题
	for _, question := range r.Question {
		domain := strings.TrimSuffix(question.Name, ".")

		logger.Debugf("[DNS] 查询: %s %s 来自 %s",
			question.Name,
			dns.TypeToString[question.Qtype],
			w.RemoteAddr())

		// 检查是否是 .beagle 域名的 A 记录查询
		if strings.HasSuffix(domain, ".beagle") && question.Qtype == dns.TypeA {
			vip, ok := s.resolve(domain)
			if ok {
				// 解析成功，返回 A 记录
				rr := &dns.A{
					Hdr: dns.RR_Header{
						Name:   question.Name,
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    60,
					},
					A: net.ParseIP(vip).To4(),
				}
				msg.Answer = append(msg.Answer, rr)
				logger.Infof("[DNS] 解析: %s → %s", domain, vip)
			} else {
				// 域名未注册，返回 NXDOMAIN
				msg.Rcode = dns.RcodeNameError
				logger.Debugf("[DNS] 域名未注册: %s", domain)
			}
		} else {
			// 非 .beagle 域名或非 A 记录，转发到上游 DNS
			answers, err := s.forwardToUpstream(question.Name, question.Qtype)
			if err != nil {
				logger.Debugf("[DNS] 转发到上游失败 (%s): %v", domain, err)
				msg.Rcode = dns.RcodeServerFailure
			} else {
				msg.Answer = append(msg.Answer, answers...)
			}
		}
	}

	// 发送响应
	if err := w.WriteMsg(msg); err != nil {
		logger.Debugf("[DNS] 写入响应失败: %v", err)
	}
}

// forwardToUpstream 转发查询到上游 DNS
func (s *DNSServer) forwardToUpstream(name string, qtype uint16) ([]dns.RR, error) {
	m := new(dns.Msg)
	m.SetQuestion(name, qtype)
	m.RecursionDesired = true

	in, _, err := s.client.Exchange(m, s.upstreamDNS)
	if err != nil {
		return nil, fmt.Errorf("查询失败: %w", err)
	}

	if in.Rcode != dns.RcodeSuccess {
		return nil, fmt.Errorf("DNS 错误: %s", dns.RcodeToString[in.Rcode])
	}

	return in.Answer, nil
}
