package agent

import (
	"context"
	"fmt"
	"io"
	"time"

	"google.golang.org/protobuf/proto"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// ContainerExecStream contains only the transport endpoints needed for an
// interactive shell. It deliberately has no command, namespace, or target
// fields; those always originate from PermissionCache.
type ContainerExecStream struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Rows   uint16
	Cols   uint16
	Resize <-chan ContainerTerminalSize
}

type ContainerTerminalSize struct {
	Rows uint16
	Cols uint16
}

// ContainerExecutor is kept small so the authorization and target-validation
// path can be tested without a live Kubernetes API server.
type ContainerExecutor interface {
	Execute(context.Context, *ContainerSSHUserPermission, ContainerExecStream) error
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
	return NewContainerExecBroker(pods, permissions, executor), nil
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

	return b.openAuthorizedTarget(ctx, permission, stream)
}

func (b *ContainerExecBroker) OpenAuthorizedShell(ctx context.Context, authorizations *SessionAuthorizationCache, permission *pb.ResourceSessionPermissionV2, stream ContainerExecStream) error {
	if b == nil || b.pods == nil || b.executor == nil || authorizations == nil || permission == nil || permission.Target == nil {
		return fmt.Errorf("ContainerSSH broker is not configured")
	}
	current, allowed := authorizations.Permission(permission.SessionId, time.Now().UTC())
	if !allowed || !sameResourceSessionIdentity(current, permission) {
		return fmt.Errorf("ContainerSSH access denied")
	}
	// The Server can extend valid_until and advance authorization_revision on
	// its short refresh loop between route selection and exec startup. Use the
	// latest authoritative cache entry after verifying that the immutable
	// Session identity is unchanged.
	permission = current
	target := &ContainerSSHUserPermission{
		UserID: permission.UserId, ResourceID: permission.ResourceId,
		Namespace: permission.Target.NamespaceName, PodName: permission.Target.PodName,
		PodUID: permission.Target.PodUid, ContainerName: permission.Target.ContainerName,
		GrantRevision: permission.GrantRevision, ListenPort: uint16(permission.ListenPort),
	}
	return b.openAuthorizedTarget(ctx, target, stream)
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
		proto.Equal(current.Target, selected.Target)
}

func (b *ContainerExecBroker) openAuthorizedTarget(ctx context.Context, permission *ContainerSSHUserPermission, stream ContainerExecStream) error {
	pod, err := b.pods.CoreV1().Pods(permission.Namespace).Get(ctx, permission.PodName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("ContainerSSH target no longer exists")
		}
		return fmt.Errorf("read ContainerSSH target: %w", err)
	}
	if string(pod.UID) != permission.PodUID {
		return fmt.Errorf("ContainerSSH target Pod UID changed")
	}
	if !podContainerReady(pod, permission.ContainerName) {
		return fmt.Errorf("ContainerSSH target container is not ready")
	}

	if permission.MaxSessionSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(permission.MaxSessionSeconds)*time.Second)
		defer cancel()
	}
	return b.executor.Execute(ctx, permission, stream)
}

func podContainerReady(pod *corev1.Pod, containerName string) bool {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName {
			return status.Ready
		}
	}
	return false
}
