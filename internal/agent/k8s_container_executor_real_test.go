//go:build s6real

package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type synchronizedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestS6RealKubernetesContainerExecutorPTYResizeExit(t *testing.T) {
	config := realKubernetesConfig(t)
	namespace := requireRealEnv(t, "S6_K8S_NAMESPACE")
	podName := requireRealEnv(t, "S6_K8S_POD")
	containerName := requireRealEnv(t, "S6_K8S_CONTAINER")

	executor, err := NewKubernetesContainerExecutor(config)
	require.NoError(t, err)
	stdinReader, stdinWriter := io.Pipe()
	t.Cleanup(func() { _ = stdinWriter.Close() })
	output := &synchronizedBuffer{}
	resize := make(chan ContainerTerminalSize, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- executor.Execute(ctx, &ContainerSSHUserPermission{
			Namespace: namespace, PodName: podName, ContainerName: containerName,
		}, ContainerExecStream{
			Stdin: stdinReader, Stdout: output, Rows: 40, Cols: 120, Resize: resize,
		})
	}()

	_, err = io.WriteString(stdinWriter, "printf 'S6_SIZE_1='; stty size\n")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return strings.Contains(output.String(), "S6_SIZE_1=40 120")
	}, 10*time.Second, 100*time.Millisecond, output.String())

	resize <- ContainerTerminalSize{Rows: 55, Cols: 144}
	_, err = io.WriteString(stdinWriter, "sleep 1; printf 'S6_SIZE_2='; stty size; exit 37\n")
	require.NoError(t, err)
	err = <-done
	var status interface{ ExitStatus() int }
	require.ErrorAs(t, err, &status)
	require.Equal(t, 37, status.ExitStatus())
	require.Contains(t, output.String(), "S6_SIZE_2=55 144")
	t.Logf("PASS real pods/exec PTY=40x120 resize=55x144 exit=37")
}

func TestS6RealContainerExecBrokerFailsClosed(t *testing.T) {
	config := realKubernetesConfig(t)
	namespace := requireRealEnv(t, "S6_K8S_NAMESPACE")
	podName := requireRealEnv(t, "S6_K8S_POD")
	containerName := requireRealEnv(t, "S6_K8S_CONTAINER")
	client, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)
	pod, err := client.CoreV1().Pods(namespace).Get(context.Background(), podName, metav1.GetOptions{})
	require.NoError(t, err)

	cache := NewPermissionCache()
	cache.UpdateContainerSSHPermissions(map[string][]*ContainerSSHUserPermission{
		"s6-real": {{
			ResourceID: "deleted-target", Namespace: namespace, PodName: podName + "-deleted",
			PodUID: string(pod.UID), ContainerName: containerName, ListenPort: ContainerSSHPortBase,
		}},
	})
	recorder := &recordingContainerExecutor{}
	err = NewContainerExecBroker(client, cache, recorder).OpenShell(context.Background(), "s6-real", "deleted-target", ContainerExecStream{})
	require.ErrorContains(t, err, "target no longer exists")
	require.False(t, recorder.called)

	failedConfig := rest.CopyConfig(config)
	failedConfig.WrapTransport = func(http.RoundTripper) http.RoundTripper {
		return failingKubernetesTransport{}
	}
	failedClient, err := kubernetes.NewForConfig(failedConfig)
	require.NoError(t, err)
	cache.UpdateContainerSSHPermissions(map[string][]*ContainerSSHUserPermission{
		"s6-real": {{
			ResourceID: "api-failure", Namespace: namespace, PodName: podName,
			PodUID: string(pod.UID), ContainerName: containerName, ListenPort: ContainerSSHPortBase,
		}},
	})
	recorder = &recordingContainerExecutor{}
	err = NewContainerExecBroker(failedClient, cache, recorder).OpenShell(context.Background(), "s6-real", "api-failure", ContainerExecStream{})
	require.ErrorContains(t, err, "injected Kubernetes API failure")
	require.False(t, recorder.called)
	t.Log("PASS real target lookup rejects deleted Pod identity and injected API failure before exec")
}

type failingKubernetesTransport struct{}

func (failingKubernetesTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("injected Kubernetes API failure")
}

func realKubernetesConfig(t *testing.T) *rest.Config {
	t.Helper()
	path := requireRealEnv(t, "S6_KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", path)
	require.NoError(t, err)
	return config
}

func requireRealEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s is required", name)
	}
	return value
}
