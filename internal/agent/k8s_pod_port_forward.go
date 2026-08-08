package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

func newKubernetesPodPortDialer(config *rest.Config, client rest.Interface) (func(context.Context, *ContainerSSHUserPermission, uint32) (net.Conn, error), error) {
	if config == nil || client == nil {
		return nil, fmt.Errorf("Kubernetes Pod port forward is not configured")
	}
	roundTripper, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes Pod port forward transport: %w", err)
	}
	return func(ctx context.Context, target *ContainerSSHUserPermission, port uint32) (net.Conn, error) {
		requestURL := client.Post().Resource("pods").Namespace(target.Namespace).Name(target.PodName).SubResource("portforward").URL()
		dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, requestURL)
		stop := make(chan struct{})
		ready := make(chan struct{})
		forwardDone := make(chan error, 1)
		var errorOutput bytes.Buffer
		forwarder, err := portforward.NewOnAddresses(
			dialer,
			[]string{"127.0.0.1"},
			[]string{"0:" + strconv.FormatUint(uint64(port), 10)},
			stop,
			ready,
			io.Discard,
			&errorOutput,
		)
		if err != nil {
			return nil, fmt.Errorf("create Kubernetes Pod port forward: %w", err)
		}
		go func() {
			forwardDone <- forwarder.ForwardPorts()
		}()
		select {
		case <-ready:
		case err := <-forwardDone:
			close(stop)
			return nil, podPortForwardError(err, errorOutput.String())
		case <-ctx.Done():
			close(stop)
			return nil, ctx.Err()
		}
		ports, err := forwarder.GetPorts()
		if err != nil {
			close(stop)
			return nil, fmt.Errorf("resolve Kubernetes Pod port forward listener: %w", err)
		}
		if len(ports) != 1 || ports[0].Local == 0 {
			close(stop)
			return nil, fmt.Errorf("resolve Kubernetes Pod port forward listener: unexpected ports %v", ports)
		}
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(ports[0].Local))))
		if err != nil {
			close(stop)
			return nil, fmt.Errorf("connect Kubernetes Pod port forward listener: %w", err)
		}
		return &podPortForwardConn{Conn: conn, stop: stop, done: forwardDone}, nil
	}, nil
}

func podPortForwardError(err error, output string) error {
	if output != "" {
		return fmt.Errorf("Kubernetes Pod port forward failed: %s: %w", output, err)
	}
	return fmt.Errorf("Kubernetes Pod port forward failed: %w", err)
}

type podPortForwardConn struct {
	net.Conn
	stop     chan struct{}
	done     <-chan error
	stopOnce sync.Once
}

func (c *podPortForwardConn) Close() error {
	err := c.Conn.Close()
	c.stopOnce.Do(func() { close(c.stop) })
	select {
	case <-c.done:
	default:
	}
	return err
}
