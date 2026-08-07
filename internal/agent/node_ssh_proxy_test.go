package agent

import (
	"context"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestNodeSSHProxyForwardsConfiguredPortToLocalSSH(t *testing.T) {
	var registered func(net.Conn)
	proxy := &NodeSSHProxy{
		listenPort: 2222,
		targetAddr: "127.0.0.1:2222",
		register: func(handler func(src, dst netip.AddrPort) (func(net.Conn), bool)) func() {
			h, ok := handler(netip.MustParseAddrPort("100.64.0.10:40000"), netip.MustParseAddrPort("100.64.0.123:2222"))
			if !ok || h == nil {
				t.Fatalf("handler did not accept configured port")
			}
			registered = h
			return func() {}
		},
		dial: func(context.Context, string, string) (net.Conn, error) {
			targetClient, targetServer := net.Pipe()
			go func() {
				defer targetServer.Close()
				buf := make([]byte, 4)
				if _, err := io.ReadFull(targetServer, buf); err != nil {
					t.Errorf("target read: %v", err)
					return
				}
				if string(buf) != "ping" {
					t.Errorf("target got %q, want ping", string(buf))
					return
				}
				if _, err := targetServer.Write([]byte("pong")); err != nil {
					t.Errorf("target write: %v", err)
				}
			}()
			return targetClient, nil
		},
		ctx: context.Background(),
	}

	if err := proxy.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if registered == nil {
		t.Fatalf("handler was not registered")
	}

	client, server := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		registered(server)
		close(done)
	}()

	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatalf("client write: %v", err)
	}
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 4)
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(buf) != "pong" {
		t.Fatalf("client got %q, want pong", string(buf))
	}
	<-done
}

func TestNodeSSHProxySkipsDefaultTailscaleSSHPort(t *testing.T) {
	called := false
	proxy := &NodeSSHProxy{
		listenPort: 22,
		register: func(func(src, dst netip.AddrPort) (func(net.Conn), bool)) func() {
			called = true
			return func() {}
		},
		ctx: context.Background(),
	}

	if err := proxy.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if called {
		t.Fatalf("default Tailscale SSH port should not register raw TCP forwarder")
	}
}
