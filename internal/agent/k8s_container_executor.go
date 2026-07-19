package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// KubernetesContainerExecutor is the data-plane implementation for
// ContainerExecBroker. Its command is deliberately fixed: callers can choose
// neither a command nor a different Kubernetes target.
type KubernetesContainerExecutor struct {
	restConfig  *rest.Config
	newExecutor func(*rest.Config, string, *url.URL) (remotecommand.Executor, error)
}

func NewKubernetesContainerExecutor(restConfig *rest.Config) (*KubernetesContainerExecutor, error) {
	if restConfig == nil || restConfig.Host == "" {
		return nil, fmt.Errorf("Kubernetes REST config is required")
	}
	return &KubernetesContainerExecutor{
		restConfig:  rest.CopyConfig(restConfig),
		newExecutor: remotecommand.NewSPDYExecutor,
	}, nil
}

func (e *KubernetesContainerExecutor) Execute(ctx context.Context, target *ContainerSSHUserPermission, stream ContainerExecStream) error {
	if e == nil || e.restConfig == nil || e.newExecutor == nil || target == nil {
		return fmt.Errorf("Kubernetes ContainerSSH executor is not configured")
	}
	execURL, err := e.execURL(target)
	if err != nil {
		return err
	}
	executor, err := e.newExecutor(e.restConfig, http.MethodPost, execURL)
	if err != nil {
		return fmt.Errorf("create Kubernetes exec stream: %w", err)
	}
	return executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stream.Stdin,
		Stdout:            stream.Stdout,
		Stderr:            stream.Stderr,
		Tty:               true,
		TerminalSizeQueue: newTerminalSizeQueue(stream.Rows, stream.Cols, stream.Resize),
	})
}

func (e *KubernetesContainerExecutor) execURL(target *ContainerSSHUserPermission) (*url.URL, error) {
	base, err := url.Parse(e.restConfig.Host)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("invalid Kubernetes API server address")
	}
	base.Path = path.Join(base.Path, "/api/v1/namespaces", target.Namespace, "pods", target.PodName, "exec")
	query := url.Values{}
	query.Set("container", target.ContainerName)
	query.Add("command", "/bin/sh")
	query.Set("stdin", "true")
	query.Set("stdout", "true")
	query.Set("stderr", "true")
	query.Set("tty", "true")
	base.RawQuery = query.Encode()
	return base, nil
}

type terminalSizeQueue struct {
	initial *remotecommand.TerminalSize
	resize  <-chan ContainerTerminalSize
}

func newTerminalSizeQueue(rows, cols uint16, resize <-chan ContainerTerminalSize) remotecommand.TerminalSizeQueue {
	if rows == 0 {
		rows = 24
	}
	if cols == 0 {
		cols = 80
	}
	return &terminalSizeQueue{initial: &remotecommand.TerminalSize{Width: cols, Height: rows}, resize: resize}
}

func (q *terminalSizeQueue) Next() *remotecommand.TerminalSize {
	if q.initial != nil {
		size := q.initial
		q.initial = nil
		return size
	}
	if q.resize == nil {
		return nil
	}
	size, ok := <-q.resize
	if !ok {
		return nil
	}
	return &remotecommand.TerminalSize{Width: size.Cols, Height: size.Rows}
}

var _ ContainerExecutor = (*KubernetesContainerExecutor)(nil)
