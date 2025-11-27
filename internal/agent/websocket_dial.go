package agent

import (
	"context"
	"crypto/tls"
	"net"
	"net/url"

	"golang.org/x/net/websocket"
)

// CustomDialHookWebsocket 自定义 WebSocket dial hook，支持自定义 path 和 TLS 配置
// 参数:
//   - protocol: "ws" 或 "wss"
//   - host: 主机名（用于 WebSocket 握手）
//   - path: WebSocket 路径（如 "/ws"）
//   - tlsConfig: TLS 配置（用于 wss，可以设置 InsecureSkipVerify）
func CustomDialHookWebsocket(protocol string, host string, path string, tlsConfig *tls.Config) func(ctx context.Context, c net.Conn, addr string) (context.Context, net.Conn, error) {
	return func(ctx context.Context, c net.Conn, addr string) (context.Context, net.Conn, error) {
		// 确定协议
		if protocol != "wss" {
			protocol = "ws"
		}

		// 确定主机名
		if host == "" {
			host = addr
		}

		// 确定路径（默认使用 FRP 的路径）
		if path == "" {
			path = "/~!frp"
		}

		// 构建 WebSocket URL
		wsURL := protocol + "://" + host + path
		uri, err := url.Parse(wsURL)
		if err != nil {
			return nil, nil, err
		}

		// 构建 origin
		origin := "http://" + uri.Host
		if protocol == "wss" {
			origin = "https://" + uri.Host
		}

		// 创建 WebSocket 配置
		cfg, err := websocket.NewConfig(wsURL, origin)
		if err != nil {
			return nil, nil, err
		}

		// 如果是 wss，设置 TLS 配置
		if protocol == "wss" && tlsConfig != nil {
			cfg.TlsConfig = tlsConfig
		}

		// 创建 WebSocket 连接
		conn, err := websocket.NewClient(cfg, c)
		if err != nil {
			return nil, nil, err
		}

		return ctx, conn, nil
	}
}
