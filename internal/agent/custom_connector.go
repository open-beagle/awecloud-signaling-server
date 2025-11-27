package agent

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	libnet "github.com/fatedier/golib/net"
	fmux "github.com/hashicorp/yamux"
	"github.com/samber/lo"

	"github.com/fatedier/frp/client"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/transport"
	netpkg "github.com/fatedier/frp/pkg/util/net"
	"github.com/fatedier/frp/pkg/util/xlog"
)

// CustomConnector 自定义 Connector，支持自定义 WebSocket path 和 TLS 配置
type CustomConnector struct {
	ctx        context.Context
	cfg        *v1.ClientCommonConfig
	muxSession *fmux.Session
	closeOnce  sync.Once

	// 自定义配置
	websocketPath      string      // WebSocket 路径
	tlsConfig          *tls.Config // TLS 配置
	insecureSkipVerify bool        // 是否跳过证书验证
}

// NewCustomConnector 创建自定义 Connector
func NewCustomConnector(ctx context.Context, cfg *v1.ClientCommonConfig, websocketPath string, insecureSkipVerify bool) (client.Connector, error) {
	c := &CustomConnector{
		ctx:                ctx,
		cfg:                cfg,
		websocketPath:      websocketPath,
		insecureSkipVerify: insecureSkipVerify,
	}

	// 如果使用 websocket/wss，准备 TLS 配置
	protocol := strings.ToLower(cfg.Transport.Protocol)
	if protocol == "websocket" || protocol == "wss" {
		tlsEnable := lo.FromPtr(cfg.Transport.TLS.Enable)
		if protocol == "wss" {
			tlsEnable = true
		}

		if tlsEnable {
			sn := cfg.Transport.TLS.ServerName
			if sn == "" {
				sn = cfg.ServerAddr
			}

			// 创建 TLS 配置
			var err error
			if insecureSkipVerify {
				// 跳过证书验证（开发环境）
				c.tlsConfig, err = transport.NewClientTLSConfig("", "", "", sn)
				if err != nil {
					return nil, fmt.Errorf("创建 TLS 配置失败: %w", err)
				}
				// 确保跳过验证
				c.tlsConfig.InsecureSkipVerify = true
			} else {
				// 验证证书（生产环境）
				c.tlsConfig, err = transport.NewClientTLSConfig(
					cfg.Transport.TLS.CertFile,
					cfg.Transport.TLS.KeyFile,
					cfg.Transport.TLS.TrustedCaFile,
					sn)
				if err != nil {
					return nil, fmt.Errorf("创建 TLS 配置失败: %w", err)
				}
			}
		}
	}

	return c, nil
}

// Open 打开底层连接
func (c *CustomConnector) Open() error {
	xl := xlog.FromContextSafe(c.ctx)

	// 如果不使用 TCPMux，直接返回
	if !lo.FromPtr(c.cfg.Transport.TCPMux) {
		return nil
	}

	conn, err := c.realConnect()
	if err != nil {
		return err
	}

	fmuxCfg := fmux.DefaultConfig()
	fmuxCfg.KeepAliveInterval = time.Duration(c.cfg.Transport.TCPMuxKeepaliveInterval) * time.Second
	fmuxCfg.LogOutput = xlog.NewTraceWriter(xl)
	fmuxCfg.MaxStreamWindowSize = 6 * 1024 * 1024
	session, err := fmux.Client(conn, fmuxCfg)
	if err != nil {
		return err
	}
	c.muxSession = session
	return nil
}

// Connect 返回一个连接
func (c *CustomConnector) Connect() (net.Conn, error) {
	if c.muxSession != nil {
		stream, err := c.muxSession.OpenStream()
		if err != nil {
			return nil, err
		}
		return stream, nil
	}

	return c.realConnect()
}

// realConnect 建立真实连接
func (c *CustomConnector) realConnect() (net.Conn, error) {
	xl := xlog.FromContextSafe(c.ctx)

	proxyType, addr, auth, err := libnet.ParseProxyURL(c.cfg.Transport.ProxyURL)
	if err != nil {
		xl.Errorf("解析代理 URL 失败")
		return nil, err
	}

	dialOptions := []libnet.DialOption{}
	protocol := strings.ToLower(c.cfg.Transport.Protocol)

	switch protocol {
	case "websocket":
		// 使用自定义 WebSocket hook
		protocol = "tcp"
		dialOptions = append(dialOptions, libnet.WithAfterHook(libnet.AfterHook{
			Hook: CustomDialHookWebsocket("ws", c.cfg.ServerAddr, c.websocketPath, c.tlsConfig),
		}))
		dialOptions = append(dialOptions, libnet.WithAfterHook(libnet.AfterHook{
			Hook: netpkg.DialHookCustomTLSHeadByte(c.tlsConfig != nil, lo.FromPtr(c.cfg.Transport.TLS.DisableCustomTLSFirstByte)),
		}))
		dialOptions = append(dialOptions, libnet.WithTLSConfig(c.tlsConfig))

	case "wss":
		// 使用自定义 WebSocket hook（wss）
		protocol = "tcp"
		dialOptions = append(dialOptions, libnet.WithTLSConfigAndPriority(100, c.tlsConfig))
		serverName := c.cfg.ServerAddr
		if c.tlsConfig != nil && c.tlsConfig.ServerName != "" {
			serverName = c.tlsConfig.ServerName
		}
		dialOptions = append(dialOptions, libnet.WithAfterHook(libnet.AfterHook{
			Hook:     CustomDialHookWebsocket("wss", serverName, c.websocketPath, c.tlsConfig),
			Priority: 110,
		}))

	default:
		// 其他协议使用默认逻辑
		dialOptions = append(dialOptions, libnet.WithAfterHook(libnet.AfterHook{
			Hook: netpkg.DialHookCustomTLSHeadByte(c.tlsConfig != nil, lo.FromPtr(c.cfg.Transport.TLS.DisableCustomTLSFirstByte)),
		}))
		dialOptions = append(dialOptions, libnet.WithTLSConfig(c.tlsConfig))
	}

	if c.cfg.Transport.ConnectServerLocalIP != "" {
		dialOptions = append(dialOptions, libnet.WithLocalAddr(c.cfg.Transport.ConnectServerLocalIP))
	}

	dialOptions = append(dialOptions,
		libnet.WithProtocol(protocol),
		libnet.WithTimeout(time.Duration(c.cfg.Transport.DialServerTimeout)*time.Second),
		libnet.WithKeepAlive(time.Duration(c.cfg.Transport.DialServerKeepAlive)*time.Second),
		libnet.WithProxy(proxyType, addr),
		libnet.WithProxyAuth(auth),
	)

	conn, err := libnet.DialContext(
		c.ctx,
		net.JoinHostPort(c.cfg.ServerAddr, strconv.Itoa(c.cfg.ServerPort)),
		dialOptions...,
	)
	return conn, err
}

// Close 关闭连接
func (c *CustomConnector) Close() error {
	c.closeOnce.Do(func() {
		if c.muxSession != nil {
			_ = c.muxSession.Close()
		}
	})
	return nil
}
