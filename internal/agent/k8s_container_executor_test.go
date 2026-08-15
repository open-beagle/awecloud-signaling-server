package agent

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type recordingRemoteExecutor struct {
	options remotecommand.StreamOptions
	err     error
	stdout  string
	stderr  string
}

func (e *recordingRemoteExecutor) Stream(remotecommand.StreamOptions) error { return nil }

func (e *recordingRemoteExecutor) StreamWithContext(_ context.Context, options remotecommand.StreamOptions) error {
	e.options = options
	if options.Stdout != nil && e.stdout != "" {
		_, _ = fmt.Fprint(options.Stdout, e.stdout)
	}
	if options.Stderr != nil && e.stderr != "" {
		_, _ = fmt.Fprint(options.Stderr, e.stderr)
	}
	return e.err
}

func TestKubernetesContainerExecutorRejectsRuntimeUserChange(t *testing.T) {
	executor, err := NewKubernetesContainerExecutor(&rest.Config{Host: "https://kubernetes.example"})
	require.NoError(t, err)
	calls := 0
	executor.newExecutor = func(*rest.Config, string, *url.URL) (remotecommand.Executor, error) {
		calls++
		return &recordingRemoteExecutor{stdout: "root\n"}, nil
	}

	err = executor.Execute(context.Background(), &ContainerSSHUserPermission{
		Namespace: "team-a", PodName: "ide-0", ContainerName: "workspace", SSHUser: "code",
	}, ContainerExecStream{})
	require.ErrorContains(t, err, "ContainerSSH user changed: discovered=code actual=root")
	require.Equal(t, 1, calls)
}

func TestKubernetesContainerExecutorForwardsResizeAndStreamFailure(t *testing.T) {
	executor, err := NewKubernetesContainerExecutor(&rest.Config{Host: "https://kubernetes.example"})
	require.NoError(t, err)
	streamFailure := fmt.Errorf("pod exec stream failed")
	probe := &recordingRemoteExecutor{}
	remote := &recordingRemoteExecutor{err: streamFailure}
	calls := 0
	executor.newExecutor = func(*rest.Config, string, *url.URL) (remotecommand.Executor, error) {
		calls++
		if calls == 1 {
			return probe, nil
		}
		return remote, nil
	}
	resize := make(chan ContainerTerminalSize, 1)
	resize <- ContainerTerminalSize{Rows: 55, Cols: 144}
	close(resize)
	err = executor.Execute(context.Background(), &ContainerSSHUserPermission{
		Namespace: "team-a", PodName: "ide-0", ContainerName: "workspace",
	}, ContainerExecStream{TTY: true, Rows: 40, Cols: 120, Resize: resize})
	require.ErrorIs(t, err, streamFailure)
	require.False(t, probe.options.Tty)
	require.Equal(t, &remotecommand.TerminalSize{Width: 120, Height: 40}, remote.options.TerminalSizeQueue.Next())
	require.Equal(t, &remotecommand.TerminalSize{Width: 144, Height: 55}, remote.options.TerminalSizeQueue.Next())
	require.Nil(t, remote.options.TerminalSizeQueue.Next())
}

func TestKubernetesContainerExecutorUsesFixedShellAndTarget(t *testing.T) {
	executor, err := NewKubernetesContainerExecutor(&rest.Config{Host: "https://kubernetes.example/base"})
	require.NoError(t, err)
	probe := &recordingRemoteExecutor{}
	remote := &recordingRemoteExecutor{}
	var method string
	var gotURL *url.URL
	var urls []*url.URL
	executor.newExecutor = func(_ *rest.Config, gotMethod string, got *url.URL) (remotecommand.Executor, error) {
		method, gotURL = gotMethod, got
		urls = append(urls, got)
		if len(urls) == 1 {
			return probe, nil
		}
		return remote, nil
	}

	var stdout, stderr bytes.Buffer
	err = executor.Execute(context.Background(), &ContainerSSHUserPermission{Namespace: "team-a", PodName: "ide-0", ContainerName: "workspace"}, ContainerExecStream{
		TTY: true, Stdin: strings.NewReader("exit\n"), Stdout: &stdout, Stderr: &stderr, Rows: 40, Cols: 120,
	})
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, method)
	require.Len(t, urls, 2)
	require.Equal(t, []string{"/bin/bash", "--noprofile", "--norc", "-c", "exit 0"}, urls[0].Query()["command"])
	require.Equal(t, "false", urls[0].Query().Get("stdin"))
	require.Equal(t, "false", urls[0].Query().Get("stdout"))
	require.Equal(t, "true", urls[0].Query().Get("stderr"))
	require.Equal(t, "false", urls[0].Query().Get("tty"))
	require.Equal(t, "/base/api/v1/namespaces/team-a/pods/ide-0/exec", gotURL.Path)
	require.Equal(t, "/bin/bash", gotURL.Query().Get("command"))
	require.Equal(t, "workspace", gotURL.Query().Get("container"))
	require.Equal(t, "true", gotURL.Query().Get("stdin"))
	require.Equal(t, "true", gotURL.Query().Get("stdout"))
	require.Equal(t, "false", gotURL.Query().Get("stderr"))
	require.Equal(t, "true", gotURL.Query().Get("tty"))
	require.True(t, remote.options.Tty)
	require.Nil(t, remote.options.Stderr)
	size := remote.options.TerminalSizeQueue.Next()
	require.Equal(t, uint16(120), size.Width)
	require.Equal(t, uint16(40), size.Height)
	require.Nil(t, remote.options.TerminalSizeQueue.Next())
}

func TestKubernetesContainerExecutorRunsExecThroughPreferredShellWithoutTTY(t *testing.T) {
	executor, err := NewKubernetesContainerExecutor(&rest.Config{Host: "https://kubernetes.example"})
	require.NoError(t, err)
	probe := &recordingRemoteExecutor{}
	remote := &recordingRemoteExecutor{}
	var urls []*url.URL
	executor.newExecutor = func(_ *rest.Config, _ string, got *url.URL) (remotecommand.Executor, error) {
		urls = append(urls, got)
		if len(urls) == 1 {
			return probe, nil
		}
		return remote, nil
	}

	err = executor.Execute(context.Background(), &ContainerSSHUserPermission{
		Namespace: "team-a", PodName: "ide-0", ContainerName: "workspace",
	}, ContainerExecStream{Command: "bash", Stdin: strings.NewReader("echo ready\n"), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	require.NoError(t, err)
	require.Len(t, urls, 2)
	require.Equal(t, []string{"/bin/bash", "-c", "bash"}, urls[1].Query()["command"])
	require.Equal(t, "true", urls[1].Query().Get("stdin"))
	require.Equal(t, "true", urls[1].Query().Get("stdout"))
	require.Equal(t, "true", urls[1].Query().Get("stderr"))
	require.Equal(t, "false", urls[1].Query().Get("tty"))
	require.False(t, remote.options.Tty)
	require.NotNil(t, remote.options.Stderr)
	require.Nil(t, remote.options.TerminalSizeQueue)
}

func TestKubernetesContainerExecutorDeclaresStdinOnlyWhenProvided(t *testing.T) {
	executor, err := NewKubernetesContainerExecutor(&rest.Config{Host: "https://kubernetes.example"})
	require.NoError(t, err)
	target := &ContainerSSHUserPermission{Namespace: "team-a", PodName: "ide-0", ContainerName: "workspace"}

	nonInteractive, err := executor.execURL(target, []string{"id", "-un"}, false, true, true, false)
	require.NoError(t, err)
	require.Equal(t, "false", nonInteractive.Query().Get("stdin"))
	require.Equal(t, "true", nonInteractive.Query().Get("stdout"))
	require.Equal(t, "true", nonInteractive.Query().Get("stderr"))

	interactive, err := executor.execURL(target, []string{"/bin/bash"}, true, true, false, true)
	require.NoError(t, err)
	require.Equal(t, "true", interactive.Query().Get("stdin"))
	require.Equal(t, "true", interactive.Query().Get("stdout"))
	require.Equal(t, "false", interactive.Query().Get("stderr"))
}

func TestKubernetesContainerExecutorRunsSFTPServerDirectlyWithoutTTY(t *testing.T) {
	executor, err := NewKubernetesContainerExecutor(&rest.Config{Host: "https://kubernetes.example"})
	require.NoError(t, err)
	missing := &recordingRemoteExecutor{
		err:    fmt.Errorf("command terminated with exit code 126"),
		stderr: `exec: "/usr/lib/openssh/sftp-server": stat /usr/lib/openssh/sftp-server: no such file or directory`,
	}
	probe := &recordingRemoteExecutor{stdout: "open\nclose\n"}
	server := &recordingRemoteExecutor{}
	var urls []*url.URL
	executor.newExecutor = func(_ *rest.Config, _ string, got *url.URL) (remotecommand.Executor, error) {
		urls = append(urls, got)
		switch len(urls) {
		case 1:
			return missing, nil
		case 2:
			return probe, nil
		default:
			return server, nil
		}
	}
	stdin := strings.NewReader("sftp protocol bytes")
	var stdout, stderr bytes.Buffer
	err = executor.ExecuteSFTP(context.Background(), &ContainerSSHUserPermission{
		Namespace: "team-a", PodName: "ide-0", ContainerName: "workspace",
	}, ContainerExecStream{TTY: true, Stdin: stdin, Stdout: &stdout, Stderr: &stderr, Rows: 40, Cols: 120})
	require.NoError(t, err)
	require.Len(t, urls, 3)
	require.Equal(t, []string{"/usr/lib/openssh/sftp-server", "-Q", "requests"}, urls[0].Query()["command"])
	require.Equal(t, []string{"/usr/libexec/openssh/sftp-server", "-Q", "requests"}, urls[1].Query()["command"])
	require.Equal(t, []string{"/usr/libexec/openssh/sftp-server"}, urls[2].Query()["command"])
	require.Equal(t, "true", urls[2].Query().Get("stdin"))
	require.Equal(t, "true", urls[2].Query().Get("stdout"))
	require.Equal(t, "true", urls[2].Query().Get("stderr"))
	require.Equal(t, "false", urls[2].Query().Get("tty"))
	require.False(t, server.options.Tty)
	require.Nil(t, server.options.TerminalSizeQueue)
	require.Empty(t, stdout.String())
}

func TestKubernetesContainerExecutorFailsWhenSFTPServerIsMissing(t *testing.T) {
	executor, err := NewKubernetesContainerExecutor(&rest.Config{Host: "https://kubernetes.example"})
	require.NoError(t, err)
	calls := 0
	executor.newExecutor = func(_ *rest.Config, _ string, got *url.URL) (remotecommand.Executor, error) {
		calls++
		candidate := got.Query().Get("command")
		return &recordingRemoteExecutor{
			err:    fmt.Errorf("command terminated with exit code 126"),
			stderr: fmt.Sprintf("exec: %q: stat %s: no such file or directory", candidate, candidate),
		}, nil
	}

	err = executor.ExecuteSFTP(context.Background(), &ContainerSSHUserPermission{
		Namespace: "team-a", PodName: "ide-0", ContainerName: "workspace",
	}, ContainerExecStream{})
	require.EqualError(t, err, "SFTP_SERVER_NOT_FOUND")
	require.Equal(t, len(containerSFTPServerPaths), calls)
}

func TestKubernetesContainerExecutorFallsBackOnlyWhenBashIsMissing(t *testing.T) {
	executor, err := NewKubernetesContainerExecutor(&rest.Config{Host: "https://kubernetes.example"})
	require.NoError(t, err)
	probe := &recordingRemoteExecutor{
		err:    fmt.Errorf("command terminated with exit code 126"),
		stderr: `exec: "/bin/bash": stat /bin/bash: no such file or directory`,
	}
	shell := &recordingRemoteExecutor{}
	var urls []*url.URL
	executor.newExecutor = func(_ *rest.Config, _ string, got *url.URL) (remotecommand.Executor, error) {
		urls = append(urls, got)
		if len(urls) == 1 {
			return probe, nil
		}
		return shell, nil
	}

	err = executor.Execute(context.Background(), &ContainerSSHUserPermission{
		Namespace: "team-a", PodName: "ide-0", ContainerName: "workspace",
	}, ContainerExecStream{})
	require.NoError(t, err)
	require.Len(t, urls, 2)
	require.Equal(t, "/bin/sh", urls[1].Query().Get("command"))
}

func TestKubernetesContainerExecutorDoesNotFallBackForBashProbeFailure(t *testing.T) {
	executor, err := NewKubernetesContainerExecutor(&rest.Config{Host: "https://kubernetes.example"})
	require.NoError(t, err)
	probeFailure := fmt.Errorf("Kubernetes exec connection reset")
	calls := 0
	executor.newExecutor = func(*rest.Config, string, *url.URL) (remotecommand.Executor, error) {
		calls++
		return &recordingRemoteExecutor{err: probeFailure}, nil
	}

	err = executor.Execute(context.Background(), &ContainerSSHUserPermission{
		Namespace: "team-a", PodName: "ide-0", ContainerName: "workspace",
	}, ContainerExecStream{})
	require.ErrorIs(t, err, probeFailure)
	require.Equal(t, 1, calls)
}

func TestKubernetesContainerExecutorRejectsMissingRESTConfig(t *testing.T) {
	_, err := NewKubernetesContainerExecutor(nil)
	require.Error(t, err)
}
