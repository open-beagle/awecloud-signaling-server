package agent

import (
	"net"
	"sync"
)

// channelListener 基于 channel 的 net.Listener 实现
// 用于桥接 RegisterFallbackTCPHandler 回调和 http.Server / grpc.Server 的 Serve(listener) 模式。
// fallback handler 收到连接后调用 Enqueue 放入 channel，Serve 循环通过 Accept 取出。
type channelListener struct {
	ch     chan net.Conn
	closed chan struct{}
	once   sync.Once
}

// newChannelListener 创建 channelListener
func newChannelListener() *channelListener {
	return &channelListener{
		ch:     make(chan net.Conn, 64), // 缓冲 64 个连接，避免 fallback handler 阻塞
		closed: make(chan struct{}),
	}
}

// Enqueue 将连接放入队列（由 fallback handler 调用）
func (l *channelListener) Enqueue(conn net.Conn) {
	select {
	case l.ch <- conn:
	case <-l.closed:
		// listener 已关闭，直接关闭连接
		conn.Close()
	}
}

// Accept 实现 net.Listener 接口
func (l *channelListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.ch:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

// Close 实现 net.Listener 接口
func (l *channelListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
	})
	return nil
}

// Addr 实现 net.Listener 接口
func (l *channelListener) Addr() net.Addr {
	return channelAddr{}
}

// channelAddr 虚拟地址（channelListener 没有真实监听地址）
type channelAddr struct{}

func (channelAddr) Network() string { return "channel" }
func (channelAddr) String() string  { return "fallback-tcp-handler" }
