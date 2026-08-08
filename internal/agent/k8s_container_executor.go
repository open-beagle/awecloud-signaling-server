package agent

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

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
	shell, err := e.preferredShell(ctx, target)
	if err != nil {
		return err
	}
	return e.execute(ctx, target, []string{shell}, stream, true)
}

func (e *KubernetesContainerExecutor) preferredShell(ctx context.Context, target *ContainerSSHUserPermission) (string, error) {
	var stderr bytes.Buffer
	err := e.execute(ctx, target, []string{"/bin/bash", "--noprofile", "--norc", "-c", "exit 0"}, ContainerExecStream{Stderr: &stderr}, false)
	if err == nil {
		return "/bin/bash", nil
	}
	if bashExecutableMissing(err, stderr.String()) {
		return "/bin/sh", nil
	}
	return "", fmt.Errorf("probe ContainerSSH bash: %w", err)
}

func bashExecutableMissing(err error, stderr string) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error() + "\n" + stderr)
	return (strings.Contains(message, `exec: "/bin/bash"`) || strings.Contains(message, "exec /bin/bash") || strings.Contains(message, "stat /bin/bash")) &&
		(strings.Contains(message, "no such file or directory") || strings.Contains(message, "executable file not found"))
}

func (e *KubernetesContainerExecutor) execute(ctx context.Context, target *ContainerSSHUserPermission, command []string, stream ContainerExecStream, tty bool) error {
	execURL, err := e.execURL(target, command, tty)
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
		Tty:               tty,
		TerminalSizeQueue: terminalSizeQueueForStream(tty, stream),
	})
}

func terminalSizeQueueForStream(tty bool, stream ContainerExecStream) remotecommand.TerminalSizeQueue {
	if !tty {
		return nil
	}
	return newTerminalSizeQueue(stream.Rows, stream.Cols, stream.Resize)
}

func (e *KubernetesContainerExecutor) execURL(target *ContainerSSHUserPermission, command []string, tty bool) (*url.URL, error) {
	base, err := url.Parse(e.restConfig.Host)
	if err != nil || base.Scheme == "" || base.Host == "" || len(command) == 0 {
		return nil, fmt.Errorf("invalid Kubernetes API server address")
	}
	base.Path = path.Join(base.Path, "/api/v1/namespaces", target.Namespace, "pods", target.PodName, "exec")
	query := url.Values{}
	query.Set("container", target.ContainerName)
	for _, argument := range command {
		query.Add("command", argument)
	}
	query.Set("stdin", "true")
	query.Set("stdout", "true")
	query.Set("stderr", "true")
	query.Set("tty", fmt.Sprint(tty))
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
