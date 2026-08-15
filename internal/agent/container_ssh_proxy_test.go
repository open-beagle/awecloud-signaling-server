package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

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

type resizeExitContainerExecutor struct {
	initialRows uint16
	initialCols uint16
	resized     chan ContainerTerminalSize
}

type testContainerExitError struct{ code int }

func (e testContainerExitError) Error() string   { return "container shell exited" }
func (e testContainerExitError) ExitStatus() int { return e.code }

func (e *resizeExitContainerExecutor) Execute(_ context.Context, _ *ContainerSSHUserPermission, stream ContainerExecStream) error {
	e.initialRows, e.initialCols = stream.Rows, stream.Cols
	select {
	case size := <-stream.Resize:
		e.resized <- size
	case <-time.After(time.Second):
		return fmt.Errorf("window-change was not forwarded")
	}
	return testContainerExitError{code: 37}
}

func (e *resizeExitContainerExecutor) ExecuteSFTP(context.Context, *ContainerSSHUserPermission, ContainerExecStream) error {
	return fmt.Errorf("unexpected SFTP request")
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
		User: "code", HostKeyCallback: ssh.InsecureIgnoreHostKey(),
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

func TestContainerSSHProxyForwardsPTYResizeAndExitStatus(t *testing.T) {
	cache := NewPermissionCache()
	cache.UpdateContainerSSHPermissions(map[string][]*ContainerSSHUserPermission{
		"alice": {{ResourceID: "resource-a", Namespace: "dev", PodName: "workspace-0", PodUID: "pod-uid-a", ContainerName: "workspace", ListenPort: ContainerSSHPortBase}},
	})
	executor := &resizeExitContainerExecutor{resized: make(chan ContainerTerminalSize, 1)}
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
		permissions: cache, broker: broker, sessions: NewContainerSessionManager(),
		identity: staticPeerIdentity{identity: &PeerIdentity{UserName: "alice", Role: "client"}}, sshConfig: sshConfig,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		serverConn, acceptErr := listener.Accept()
		if acceptErr == nil {
			proxy.HandleConn(context.Background(), serverConn, "resource-a")
		}
	}()
	client, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{
		User: "code", HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	require.NoError(t, err)
	session, err := client.NewSession()
	require.NoError(t, err)
	require.NoError(t, session.RequestPty("xterm", 40, 120, ssh.TerminalModes{}))
	require.NoError(t, session.Shell())
	require.NoError(t, session.WindowChange(55, 144))
	waitErr := session.Wait()
	var exitErr *ssh.ExitError
	require.ErrorAs(t, waitErr, &exitErr)
	require.Equal(t, 37, exitErr.ExitStatus())
	require.Equal(t, uint16(40), executor.initialRows)
	require.Equal(t, uint16(120), executor.initialCols)
	require.Equal(t, ContainerTerminalSize{Rows: 55, Cols: 144}, <-executor.resized)
	_ = client.Close()
	<-done
}

func TestContainerSSHProxyForwardsExecWithoutPTY(t *testing.T) {
	cache := NewPermissionCache()
	cache.UpdateContainerSSHPermissions(map[string][]*ContainerSSHUserPermission{
		"alice": {{ResourceID: "resource-a", Namespace: "dev", PodName: "workspace-0", PodUID: "pod-uid-a", ContainerName: "workspace", ListenPort: ContainerSSHPortBase}},
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
		permissions: cache, broker: broker, sessions: NewContainerSessionManager(),
		identity: staticPeerIdentity{identity: &PeerIdentity{UserName: "alice", Role: "client"}}, sshConfig: sshConfig,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		serverConn, acceptErr := listener.Accept()
		if acceptErr == nil {
			proxy.HandleConn(context.Background(), serverConn, "resource-a")
		}
	}()
	client, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{
		User: "code", HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	require.NoError(t, err)
	session, err := client.NewSession()
	require.NoError(t, err)
	require.NoError(t, session.Run("bash"))
	_ = client.Close()
	<-done

	require.True(t, executor.called)
	require.Equal(t, "bash", executor.stream.Command)
	require.False(t, executor.stream.TTY)
}

func TestContainerSSHProxyForwardsSFTPSubsystemWithoutPTY(t *testing.T) {
	cache := NewPermissionCache()
	cache.UpdateContainerSSHPermissions(map[string][]*ContainerSSHUserPermission{
		"alice": {{ResourceID: "resource-a", Namespace: "dev", PodName: "workspace-0", PodUID: "pod-uid-a", ContainerName: "workspace", ListenPort: ContainerSSHPortBase}},
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
		permissions: cache, broker: broker, sessions: NewContainerSessionManager(),
		identity: staticPeerIdentity{identity: &PeerIdentity{UserName: "alice", Role: "client"}}, sshConfig: sshConfig,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		serverConn, acceptErr := listener.Accept()
		if acceptErr == nil {
			proxy.HandleConn(context.Background(), serverConn, "resource-a")
		}
	}()
	client, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{User: "code", HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	require.NoError(t, err)
	session, err := client.NewSession()
	require.NoError(t, err)
	require.NoError(t, session.RequestSubsystem("sftp"))
	_ = session.Close()
	_ = client.Close()
	<-done

	require.True(t, executor.sftp)
	require.False(t, executor.stream.TTY)
}

func TestContainerSSHProxyV2UsesServerSessionAndHeadscaleNodeIdentity(t *testing.T) {
	now := time.Now().UTC()
	authorizations := NewSessionAuthorizationCache()
	permission := sessionPermission(now, "session-server", "resource-v2", "alice", 7001, uint32(ContainerSSHPortBase))
	require.NoError(t, authorizations.Apply(signedSessionSnapshot(t, 1, now, permission), now))

	executor := &recordingContainerExecutor{}
	broker := NewContainerExecBroker(fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "ide-0", Namespace: "dev", UID: types.UID("pod-a")},
		Status:     corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "workspace", Ready: true}}},
	}), NewPermissionCache(), executor)
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	hostKey, err := ssh.NewSignerFromKey(privateKey)
	require.NoError(t, err)
	sshConfig := &ssh.ServerConfig{NoClientAuth: true}
	sshConfig.AddHostKey(hostKey)
	sessions := NewContainerSessionManager()
	sessions.idleGrace = 0
	proxy := &ContainerSSHProxy{
		permissions: NewPermissionCache(), authorizations: authorizations, broker: broker, sessions: sessions,
		identity: staticPeerIdentity{identity: &PeerIdentity{UserName: "alice", NodeID: 7001, Role: "client"}}, sshConfig: sshConfig,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		serverConn, acceptErr := listener.Accept()
		if acceptErr == nil {
			proxy.handleConn(context.Background(), serverConn, ContainerSSHPortBase, "")
		}
	}()
	client, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{
		User: "code", HostKeyCallback: ssh.InsecureIgnoreHostKey(),
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
	require.Equal(t, "resource-v2", executor.target.ResourceID)
	events := sessions.ResourceEventsForHeartbeat()
	require.Len(t, events, 2)
	require.Equal(t, "session-server", events[0].SessionId)
	require.Equal(t, int64(1), events[0].SourceSequence)
	require.Equal(t, int64(2), events[1].SourceSequence)
}

func TestContainerSSHProxyV2ForwardsDirectTCPIPToAuthorizedPod(t *testing.T) {
	now := time.Now().UTC()
	authorizations := NewSessionAuthorizationCache()
	permission := sessionPermission(now, "session-server", "resource-v2", "alice", 7001, uint32(ContainerSSHPortBase))
	require.NoError(t, authorizations.Apply(signedSessionSnapshot(t, 1, now, permission), now))

	backend, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer backend.Close()
	backendDone := make(chan struct{})
	go func() {
		defer close(backendDone)
		conn, acceptErr := backend.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(conn, conn)
	}()

	broker := NewContainerExecBroker(fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "ide-0", Namespace: "dev", UID: types.UID("pod-a")},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{Name: "workspace", Ready: true}},
		},
	}), NewPermissionCache(), &recordingContainerExecutor{})
	broker.dialPort = func(ctx context.Context, _ *ContainerSSHUserPermission, _ uint32) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", backend.Addr().String())
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	hostKey, err := ssh.NewSignerFromKey(privateKey)
	require.NoError(t, err)
	sshConfig := &ssh.ServerConfig{NoClientAuth: true}
	sshConfig.AddHostKey(hostKey)
	sessions := NewContainerSessionManager()
	sessions.idleGrace = 0
	proxy := &ContainerSSHProxy{
		permissions: NewPermissionCache(), authorizations: authorizations, broker: broker, sessions: sessions,
		identity: staticPeerIdentity{identity: &PeerIdentity{UserName: "alice", NodeID: 7001, Role: "client"}}, sshConfig: sshConfig,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		serverConn, acceptErr := listener.Accept()
		if acceptErr == nil {
			proxy.handleConn(context.Background(), serverConn, ContainerSSHPortBase, "")
		}
	}()
	client, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{
		User: "code", HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	require.NoError(t, err)
	forwarded, err := client.Dial("tcp", backend.Addr().String())
	require.NoError(t, err)
	_, err = forwarded.Write([]byte("vscode-forward"))
	require.NoError(t, err)
	reply := make([]byte, len("vscode-forward"))
	_, err = io.ReadFull(forwarded, reply)
	require.NoError(t, err)
	require.Equal(t, "vscode-forward", string(reply))
	require.NoError(t, forwarded.Close())
	require.NoError(t, client.Close())
	<-done
	<-backendDone

	events := sessions.ResourceEventsForHeartbeat()
	require.Len(t, events, 2)
	require.Equal(t, "connected", events[0].EventType)
	require.Equal(t, "ended", events[1].EventType)
}

func TestContainerSSHProxyV2EnforcementBlocksLegacyFallback(t *testing.T) {
	now := time.Now().UTC()
	authorizations := NewSessionAuthorizationCache()
	require.NoError(t, authorizations.Apply(signedSessionSnapshot(t, 1, now), now))
	legacy := NewPermissionCache()
	legacy.UpdateContainerSSHPermissions(map[string][]*ContainerSSHUserPermission{
		"alice": {{
			ResourceID: "resource-a", Namespace: "dev", PodName: "workspace-0", PodUID: "pod-a",
			ContainerName: "workspace", ListenPort: ContainerSSHPortBase,
		}},
	})
	proxy := &ContainerSSHProxy{
		permissions: legacy, authorizations: authorizations,
		identity: staticPeerIdentity{identity: &PeerIdentity{UserName: "alice", NodeID: 7001, Role: "client"}},
	}
	serverConn, clientConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		proxy.handleConn(context.Background(), serverConn, 0, "resource-a")
	}()
	require.NoError(t, clientConn.SetReadDeadline(time.Now().Add(time.Second)))
	buffer := make([]byte, 1)
	_, err := clientConn.Read(buffer)
	require.Error(t, err)
	_ = clientConn.Close()
	<-done
}
