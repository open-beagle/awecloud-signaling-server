package agent

import (
	"context"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// ContainerExecStream contains the SSH request and its transport endpoints.
// Namespace and target identity always originate from PermissionCache.
type ContainerExecStream struct {
	Command string
	TTY     bool
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Rows    uint16
	Cols    uint16
	Resize  <-chan ContainerTerminalSize
}

type ContainerTerminalSize struct {
	Rows uint16
	Cols uint16
}

// ContainerExecutor is kept small so the authorization and target-validation
// path can be tested without a live Kubernetes API server.
type ContainerExecutor interface {
	Execute(context.Context, *ContainerSSHUserPermission, ContainerExecStream) error
	ExecuteSFTP(context.Context, *ContainerSSHUserPermission, ContainerExecStream) error
}

type podReader interface {
	Get(context.Context, string, metav1.GetOptions) (*corev1.Pod, error)
}

// ContainerExecBroker validates the server-authorized target immediately
// before creating a Kubernetes exec stream. It does not accept arbitrary
// client-provided Kubernetes coordinates or commands.
type ContainerExecBroker struct {
	pods        kubernetes.Interface
	permissions *PermissionCache
	executor    ContainerExecutor
	dialPort    func(context.Context, *ContainerSSHUserPermission, uint32) (net.Conn, error)
}

func NewContainerExecBroker(pods kubernetes.Interface, permissions *PermissionCache, executor ContainerExecutor) *ContainerExecBroker {
	return &ContainerExecBroker{pods: pods, permissions: permissions, executor: executor}
}

// NewContainerExecBrokerFromKubeconfig binds the real Kubernetes client and
// SPDY executor to one shared Agent configuration.
func NewContainerExecBrokerFromKubeconfig(kubeconfig string, permissions *PermissionCache) (*ContainerExecBroker, error) {
	restConfig, err := loadK8SRESTConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	pods, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes clientset: %w", err)
	}
	executor, err := NewKubernetesContainerExecutor(restConfig)
	if err != nil {
		return nil, err
	}
	dialPort, err := newKubernetesPodPortDialer(restConfig, pods.CoreV1().RESTClient())
	if err != nil {
		return nil, err
	}
	broker := NewContainerExecBroker(pods, permissions, executor)
	broker.dialPort = dialPort
	return broker, nil
}

// OpenShell starts the fixed interactive-shell operation for one authorized
// resource. Callers must close the SSH channel to cancel the supplied context.
func (b *ContainerExecBroker) OpenShell(ctx context.Context, userName, resourceID string, stream ContainerExecStream) error {
	if b == nil || b.pods == nil || b.permissions == nil || b.executor == nil {
		return fmt.Errorf("ContainerSSH broker is not configured")
	}
	permission, allowed := b.permissions.CheckContainerSSHAccess(userName, resourceID)
	if !allowed {
		return fmt.Errorf("ContainerSSH access denied")
	}

	return b.openAuthorizedTarget(ctx, permission, stream, b.executor.Execute)
}

func (b *ContainerExecBroker) OpenSFTP(ctx context.Context, userName, resourceID string, stream ContainerExecStream) error {
	if b == nil || b.pods == nil || b.permissions == nil || b.executor == nil {
		return fmt.Errorf("ContainerSSH broker is not configured")
	}
	permission, allowed := b.permissions.CheckContainerSSHAccess(userName, resourceID)
	if !allowed {
		return fmt.Errorf("ContainerSSH access denied")
	}
	return b.openAuthorizedTarget(ctx, permission, stream, b.executor.ExecuteSFTP)
}

func (b *ContainerExecBroker) OpenAuthorizedShell(ctx context.Context, authorizations *SessionAuthorizationCache, permission *pb.ResourceSessionPermissionV2, stream ContainerExecStream) error {
	if b == nil || b.pods == nil || b.executor == nil || authorizations == nil || permission == nil || permission.Target == nil {
		return fmt.Errorf("ContainerSSH broker is not configured")
	}
	target, err := b.authorizedV2Target(authorizations, permission)
	if err != nil {
		return err
	}
	return b.openAuthorizedTarget(ctx, target, stream, b.executor.Execute)
}

func (b *ContainerExecBroker) OpenAuthorizedSFTP(ctx context.Context, authorizations *SessionAuthorizationCache, permission *pb.ResourceSessionPermissionV2, stream ContainerExecStream) error {
	if b == nil || b.pods == nil || b.executor == nil || authorizations == nil || permission == nil || permission.Target == nil {
		return fmt.Errorf("ContainerSSH broker is not configured")
	}
	target, err := b.authorizedV2Target(authorizations, permission)
	if err != nil {
		return err
	}
	return b.openAuthorizedTarget(ctx, target, stream, b.executor.ExecuteSFTP)
}

func (b *ContainerExecBroker) DialAuthorizedPort(ctx context.Context, authorizations *SessionAuthorizationCache, permission *pb.ResourceSessionPermissionV2, host string, port uint32) (net.Conn, error) {
	if b == nil || b.pods == nil || b.dialPort == nil || authorizations == nil || permission == nil || permission.Target == nil {
		return nil, fmt.Errorf("ContainerSSH broker is not configured")
	}
	if !isContainerLoopback(host) || port == 0 || port > 65535 {
		return nil, fmt.Errorf("ContainerSSH port forwarding target is not allowed")
	}
	target, err := b.authorizedV2Target(authorizations, permission)
	if err != nil {
		return nil, err
	}
	if _, err := b.authorizedPod(ctx, target); err != nil {
		return nil, err
	}
	return b.dialPort(ctx, target, port)
}

func (b *ContainerExecBroker) authorizedV2Target(authorizations *SessionAuthorizationCache, permission *pb.ResourceSessionPermissionV2) (*ContainerSSHUserPermission, error) {
	current, allowed := authorizations.Permission(permission.SessionId, time.Now().UTC())
	if !allowed || !sameResourceSessionIdentity(current, permission) {
		return nil, fmt.Errorf("ContainerSSH access denied")
	}
	// The Server can extend valid_until and advance authorization_revision on
	// its short refresh loop between route selection and exec startup. Use the
	// latest authoritative cache entry after verifying that the immutable
	// Session identity is unchanged.
	permission = current
	if len(permission.SshUsers) != 1 {
		return nil, fmt.Errorf("ContainerSSH discovered user is invalid")
	}
	return &ContainerSSHUserPermission{
		UserID: permission.UserId, ResourceID: permission.ResourceId,
		Namespace: permission.Target.NamespaceName, PodName: permission.Target.PodName,
		PodUID: permission.Target.PodUid, ContainerName: permission.Target.ContainerName,
		SSHUser:       permission.SshUsers[0],
		GrantRevision: permission.GrantRevision, ListenPort: uint16(permission.ListenPort),
	}, nil
}

func sameResourceSessionIdentity(current, selected *pb.ResourceSessionPermissionV2) bool {
	return current != nil && selected != nil &&
		current.SessionId == selected.SessionId && current.TenantId == selected.TenantId &&
		current.ResourceId == selected.ResourceId && current.SourceId == selected.SourceId &&
		current.TargetRevisionId == selected.TargetRevisionId && current.UserId == selected.UserId &&
		current.UserName == selected.UserName && current.DeviceId == selected.DeviceId &&
		current.DeviceHeadscaleNodeId == selected.DeviceHeadscaleNodeId &&
		current.ResourceType == selected.ResourceType && current.Action == selected.Action &&
		current.AllocationId == selected.AllocationId && current.GrantId == selected.GrantId &&
		current.GrantRevision == selected.GrantRevision && current.ListenPort == selected.ListenPort &&
		slices.Equal(current.SshUsers, selected.SshUsers) &&
		proto.Equal(current.Target, selected.Target)
}

func (b *ContainerExecBroker) openAuthorizedTarget(ctx context.Context, permission *ContainerSSHUserPermission, stream ContainerExecStream, execute func(context.Context, *ContainerSSHUserPermission, ContainerExecStream) error) error {
	if _, err := b.authorizedPod(ctx, permission); err != nil {
		return err
	}

	if permission.MaxSessionSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(permission.MaxSessionSeconds)*time.Second)
		defer cancel()
	}
	return execute(ctx, permission, stream)
}

func (b *ContainerExecBroker) authorizedPod(ctx context.Context, permission *ContainerSSHUserPermission) (*corev1.Pod, error) {
	pod, err := b.pods.CoreV1().Pods(permission.Namespace).Get(ctx, permission.PodName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("ContainerSSH target no longer exists")
		}
		return nil, fmt.Errorf("read ContainerSSH target: %w", err)
	}
	if string(pod.UID) != permission.PodUID {
		return nil, fmt.Errorf("ContainerSSH target Pod UID changed")
	}
	if !podContainerReady(pod, permission.ContainerName) {
		return nil, fmt.Errorf("ContainerSSH target container is not ready")
	}
	return pod, nil
}

func isContainerLoopback(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}

func podContainerReady(pod *corev1.Pod, containerName string) bool {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName {
			return status.Ready
		}
	}
	return false
}
