package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

type recordingContainerExecutor struct {
	called bool
	target *ContainerSSHUserPermission
}

func (e *recordingContainerExecutor) Execute(_ context.Context, target *ContainerSSHUserPermission, _ ContainerExecStream) error {
	e.called = true
	e.target = target
	return nil
}

func TestContainerExecBrokerValidatesAuthorizedReadyPod(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "workspace-0", Namespace: "dev", UID: types.UID("pod-uid-a")},
		Status:     corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "workspace", Ready: true}}},
	})
	cache := NewPermissionCache()
	cache.UpdateContainerSSHPermissions(map[string][]*ContainerSSHUserPermission{
		"alice": {{ResourceID: "resource-a", Namespace: "dev", PodName: "workspace-0", PodUID: "pod-uid-a", ContainerName: "workspace", TargetRevision: 3, GrantRevision: 4}},
	})
	executor := &recordingContainerExecutor{}
	broker := NewContainerExecBroker(client, cache, executor)

	err := broker.OpenShell(context.Background(), "alice", "resource-a", ContainerExecStream{})
	require.NoError(t, err)
	require.True(t, executor.called)
	require.Equal(t, "pod-uid-a", executor.target.PodUID)
}

func TestContainerExecBrokerRejectsChangedPodUIDWithoutExecuting(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "workspace-0", Namespace: "dev", UID: types.UID("new-uid")},
		Status:     corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "workspace", Ready: true}}},
	})
	cache := NewPermissionCache()
	cache.UpdateContainerSSHPermissions(map[string][]*ContainerSSHUserPermission{
		"alice": {{ResourceID: "resource-a", Namespace: "dev", PodName: "workspace-0", PodUID: "old-uid", ContainerName: "workspace"}},
	})
	executor := &recordingContainerExecutor{}

	err := NewContainerExecBroker(client, cache, executor).OpenShell(context.Background(), "alice", "resource-a", ContainerExecStream{})
	require.ErrorContains(t, err, "Pod UID changed")
	require.False(t, executor.called)
}

func TestContainerExecBrokerRejectsUnknownResource(t *testing.T) {
	cache := NewPermissionCache()
	executor := &recordingContainerExecutor{}
	broker := NewContainerExecBroker(fake.NewSimpleClientset(), cache, executor)

	err := broker.OpenShell(context.Background(), "alice", "resource-a", ContainerExecStream{})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "access denied"))
	require.False(t, executor.called)
}

func TestContainerSSHPermissionsEmptySnapshotRevokesAccess(t *testing.T) {
	cache := NewPermissionCache()
	cache.UpdateContainerSSHPermissions(map[string][]*ContainerSSHUserPermission{
		"alice": {{ResourceID: "resource-a", Namespace: "dev", PodName: "workspace-0", PodUID: "pod-uid-a", ContainerName: "workspace"}},
	})
	require.True(t, func() bool {
		_, allowed := cache.CheckContainerSSHAccess("alice", "resource-a")
		return allowed
	}())

	cache.UpdateContainerSSHPermissions(nil)
	_, allowed := cache.CheckContainerSSHAccess("alice", "resource-a")
	require.False(t, allowed)
}
