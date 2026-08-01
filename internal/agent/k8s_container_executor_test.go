package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type recordingRemoteExecutor struct {
	options remotecommand.StreamOptions
	err     error
}

func (e *recordingRemoteExecutor) Stream(remotecommand.StreamOptions) error { return nil }

func (e *recordingRemoteExecutor) StreamWithContext(_ context.Context, options remotecommand.StreamOptions) error {
	e.options = options
	return e.err
}

func TestKubernetesContainerExecutorForwardsResizeAndStreamFailure(t *testing.T) {
	executor, err := NewKubernetesContainerExecutor(&rest.Config{Host: "https://kubernetes.example"})
	require.NoError(t, err)
	streamFailure := fmt.Errorf("pod exec stream failed")
	remote := &recordingRemoteExecutor{err: streamFailure}
	executor.newExecutor = func(*rest.Config, string, *url.URL) (remotecommand.Executor, error) { return remote, nil }
	resize := make(chan ContainerTerminalSize, 1)
	resize <- ContainerTerminalSize{Rows: 55, Cols: 144}
	close(resize)
	err = executor.Execute(context.Background(), &ContainerSSHUserPermission{
		Namespace: "team-a", PodName: "ide-0", ContainerName: "workspace",
	}, ContainerExecStream{Rows: 40, Cols: 120, Resize: resize})
	require.ErrorIs(t, err, streamFailure)
	require.Equal(t, &remotecommand.TerminalSize{Width: 120, Height: 40}, remote.options.TerminalSizeQueue.Next())
	require.Equal(t, &remotecommand.TerminalSize{Width: 144, Height: 55}, remote.options.TerminalSizeQueue.Next())
	require.Nil(t, remote.options.TerminalSizeQueue.Next())
}

func TestKubernetesContainerExecutorUsesFixedShellAndTarget(t *testing.T) {
	executor, err := NewKubernetesContainerExecutor(&rest.Config{Host: "https://kubernetes.example/base"})
	require.NoError(t, err)
	remote := &recordingRemoteExecutor{}
	var method string
	var gotURL *url.URL
	executor.newExecutor = func(_ *rest.Config, gotMethod string, got *url.URL) (remotecommand.Executor, error) {
		method, gotURL = gotMethod, got
		return remote, nil
	}

	err = executor.Execute(context.Background(), &ContainerSSHUserPermission{Namespace: "team-a", PodName: "ide-0", ContainerName: "workspace"}, ContainerExecStream{Rows: 40, Cols: 120})
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, method)
	require.Equal(t, "/base/api/v1/namespaces/team-a/pods/ide-0/exec", gotURL.Path)
	require.Equal(t, "/bin/sh", gotURL.Query().Get("command"))
	require.Equal(t, "workspace", gotURL.Query().Get("container"))
	require.Equal(t, "true", gotURL.Query().Get("tty"))
	require.True(t, remote.options.Tty)
	size := remote.options.TerminalSizeQueue.Next()
	require.Equal(t, uint16(120), size.Width)
	require.Equal(t, uint16(40), size.Height)
	require.Nil(t, remote.options.TerminalSizeQueue.Next())
}

func TestKubernetesContainerExecutorRejectsMissingRESTConfig(t *testing.T) {
	_, err := NewKubernetesContainerExecutor(nil)
	require.Error(t, err)
}
