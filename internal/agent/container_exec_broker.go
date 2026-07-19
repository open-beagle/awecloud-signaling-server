package agent

import (
	"context"
	"fmt"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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
