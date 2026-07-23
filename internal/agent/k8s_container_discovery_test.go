package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
)

func TestContainerDiscoveryConvertsOptedInPodEvidence(t *testing.T) {
	discovery := &K8SContainerDiscovery{config: &config.ContainerSection{
		ProviderLabel: "provider", WorkspaceLabel: "workspace", GenerationLabel: "generation", LeaseSeconds: 120,
	}, ctx: context.Background()}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "ide-a", Namespace: "acme", UID: types.UID("pod-a"), Labels: map[string]string{
			"provider": "beagle-ide", "workspace": "ws-a", "generation": "7", "team": "acme",
		}},
		Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "workspace"}}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "workspace", Ready: true}}},
	}
	candidate := discovery.convertPod(pod, "workspace")
	require.Equal(t, "beagle-ide", candidate.ProviderHint)
	require.Equal(t, "ws-a", candidate.WorkspaceHint)
	require.Equal(t, int64(7), candidate.GenerationHint)
	require.Equal(t, "pod-a", candidate.PodUID)
	require.True(t, candidate.Ready)
	require.Equal(t, "acme", candidate.Namespace)
	require.Equal(t, "acme", candidate.Labels["team"])
}

func TestContainerDiscoverySelectsExplicitContainer(t *testing.T) {
	discovery := &K8SContainerDiscovery{config: &config.ContainerSection{ContainerNameLabel: "container"}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"container": "ide"}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "ide"}, {Name: "sync"}}},
	}

	require.Equal(t, []corev1.Container{{Name: "ide"}}, discovery.selectedContainers(pod))

	delete(pod.Labels, "container")
	require.Equal(t, pod.Spec.Containers, discovery.selectedContainers(pod))

	pod.Labels["container"] = "missing"
	require.Empty(t, discovery.selectedContainers(pod))
}
