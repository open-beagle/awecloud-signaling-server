package agent

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// DiscoveredContainer is runtime evidence collected from an explicitly
// opted-in Pod. It is not a business Resource and carries no Tenant identity.
type DiscoveredContainer struct {
	ProviderHint   string
	WorkspaceHint  string
	GenerationHint int64
	ClusterID      string
	Namespace      string
	PodName        string
	PodUID         string
	ContainerName  string
	Ready          bool
	LeaseSeconds   int
	Labels         map[string]string
}

type K8SContainerDiscovery struct {
	config    *config.ContainerSection
	clientset kubernetes.Interface

	ctx        context.Context
	cancel     context.CancelFunc
	mutex      sync.RWMutex
	candidates map[string]*DiscoveredContainer
}

func NewK8SContainerDiscovery(cfg *config.ContainerSection, parentCtx context.Context) (*K8SContainerDiscovery, error) {
	ctx, cancel := context.WithCancel(parentCtx)
	clientset, err := createK8SClientset(cfg.Kubeconfig)
	if err != nil {
		cancel()
		return nil, err
	}
	return &K8SContainerDiscovery{
		config: cfg, clientset: clientset, ctx: ctx, cancel: cancel,
		candidates: make(map[string]*DiscoveredContainer),
	}, nil
}

func (d *K8SContainerDiscovery) Start() {
	go func() {
		d.refresh()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				d.refresh()
			case <-d.ctx.Done():
				return
			}
		}
	}()
	logger.Infof("ContainerSSH Pod 候选发现已启动: selector=%s", d.config.LabelSelector)
}

func (d *K8SContainerDiscovery) refresh() {
	selector, err := labels.Parse(d.config.LabelSelector)
	if err != nil {
		logger.Warnf("ContainerSSH LabelSelector 无效: %v", err)
		return
	}
	options := metav1.ListOptions{LabelSelector: selector.String()}
	var namespaces []string
	if len(d.config.Namespaces) == 0 {
		namespaces = []string{metav1.NamespaceAll}
	} else {
		namespaces = d.config.Namespaces
	}
	found := make(map[string]*DiscoveredContainer)
	for _, namespace := range namespaces {
		pods, err := d.clientset.CoreV1().Pods(namespace).List(d.ctx, options)
		if err != nil {
			logger.Warnf("ContainerSSH Pod 发现失败: namespace=%s err=%v", namespace, err)
			continue
		}
		for i := range pods.Items {
			pod := &pods.Items[i]
			for _, container := range pod.Spec.Containers {
				candidate := d.convertPod(pod, container.Name)
				found[pod.Namespace+"/"+string(pod.UID)+"/"+container.Name] = candidate
			}
		}
	}
	d.mutex.Lock()
	d.candidates = found
	d.mutex.Unlock()
}

func (d *K8SContainerDiscovery) convertPod(pod *corev1.Pod, containerName string) *DiscoveredContainer {
	labelsCopy := make(map[string]string, len(pod.Labels))
	for key, value := range pod.Labels {
		labelsCopy[key] = value
	}
	generation, _ := strconv.ParseInt(strings.TrimSpace(pod.Labels[d.config.GenerationLabel]), 10, 64)
	return &DiscoveredContainer{
		ProviderHint: pod.Labels[d.config.ProviderLabel], WorkspaceHint: pod.Labels[d.config.WorkspaceLabel],
		GenerationHint: generation, Namespace: pod.Namespace, PodName: pod.Name, PodUID: string(pod.UID),
		ContainerName: containerName, Ready: containerReady(pod, containerName), LeaseSeconds: d.config.LeaseSeconds,
		Labels: labelsCopy,
	}
}

func containerReady(pod *corev1.Pod, containerName string) bool {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName {
			return status.Ready
		}
	}
	return false
}

func (d *K8SContainerDiscovery) GetCandidates() []*DiscoveredContainer {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	result := make([]*DiscoveredContainer, 0, len(d.candidates))
	for _, candidate := range d.candidates {
		copy := *candidate
		copy.Labels = make(map[string]string, len(candidate.Labels))
		for key, value := range candidate.Labels {
			copy.Labels[key] = value
		}
		result = append(result, &copy)
	}
	return result
}

func (d *K8SContainerDiscovery) Stop() {
	d.cancel()
	logger.Info("ContainerSSH Pod 候选发现已停止")
}
