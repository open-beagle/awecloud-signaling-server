package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"

	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/stretchr/testify/require"
)

type staticPeerIdentity struct {
	identity *PeerIdentity
}

func (s staticPeerIdentity) ExtractFromConn(context.Context, net.Addr) (*PeerIdentity, error) {
	return s.identity, nil
}

func TestContainerSSHProxyRoutesAuthenticatedDesktopToBroker(t *testing.T) {
	cache := NewPermissionCache()
	cache.UpdateContainerSSHPermissions(map[string][]*ContainerSSHUserPermission{
		"alice": {{
			ResourceID: "resource-a", Namespace: "dev", PodName: "workspace-0",
			PodUID: "pod-uid-a", ContainerName: "workspace", ListenPort: ContainerSSHPortBase,
		}},
	})
	executor := &recordingContainerExecutor{}
	broker := NewContainerExecBroker(fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "workspace-0", Namespace: "dev", UID: types.UID("pod-uid-a")},
		Status:     corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "workspace", Ready: true}}},
	}), cache, executor)

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	hostKey, err := ssh.NewSignerFromKey(privateKey)
	require.NoError(t, err)
	sshConfig := &ssh.ServerConfig{NoClientAuth: true}
	sshConfig.AddHostKey(hostKey)
	proxy := &ContainerSSHProxy{
		permissions: cache,
		broker:      broker,
		sessions:    NewContainerSessionManager(),
		identity:    staticPeerIdentity{identity: &PeerIdentity{UserName: "alice", Role: "client"}},
		sshConfig:   sshConfig,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		serverConn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		proxy.HandleConn(context.Background(), serverConn, "resource-a")
	}()

	client, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{
		User: "ignored-shell-user", HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	require.NoError(t, err)
	session, err := client.NewSession()
	require.NoError(t, err)
	require.NoError(t, session.RequestPty("xterm", 40, 120, ssh.TerminalModes{}))
	require.NoError(t, session.Shell())
	_ = session.Wait()
	_ = client.Close()
	<-done

	require.True(t, executor.called)
	require.Equal(t, "resource-a", executor.target.ResourceID)
	require.Equal(t, "pod-uid-a", executor.target.PodUID)
}
