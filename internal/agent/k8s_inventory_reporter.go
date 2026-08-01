package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"github.com/google/uuid"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

const (
	k8sInventoryProtocolVersion = "v1"
	k8sInventoryReportInterval  = 30 * time.Second
)

// KubernetesInventoryReporter maintains the S2/S4 inventory leases for an
// Agent. It deliberately uses only explicitly configured namespaces: a Server
// capability flag must not turn an existing Agent into a cluster-wide reader.
type KubernetesInventoryReporter struct {
	client pb.AgentServiceClient
	k8s    kubernetes.Interface
	ctx    context.Context

	mu             sync.RWMutex
	supplyConfig   *pb.SupplyInventoryConfig
	workloadConfig *pb.WorkloadInventoryConfig
	options        kubernetesInventoryOptions
	supplyEpoch    string
	workEpoch      string
	supplySequence uint64
	workSequence   uint64
	wake           chan struct{}
}

type kubernetesInventoryOptions struct {
	Namespaces             []string
	ServiceNamespaces      []string
	ContainerNamespaces    []string
	DisplayName            string
	ServiceEnabled         bool
	ServiceLabelSelector   string
	ContainerEnabled       bool
	ContainerLabelSelector string
}

type kubernetesInventorySnapshot struct {
	observedAt   time.Time
	cluster      *pb.KubernetesClusterInventory
	clusterKey   string
	namespaces   map[string]*corev1.Namespace
	servicePorts map[string][]*pb.WorkloadServicePort
	containers   map[string][]*pb.WorkloadContainer
}

func NewKubernetesInventoryReporter(client pb.AgentServiceClient, cfg *config.AgentConfig, parent context.Context) (*KubernetesInventoryReporter, error) {
	if client == nil || cfg == nil || parent == nil {
		return nil, fmt.Errorf("inventory reporter requires client, config, and context")
	}
	clientset, err := createK8SClientset(inventoryKubeconfig(cfg))
	if err != nil {
		return nil, fmt.Errorf("create inventory Kubernetes client: %w", err)
	}
	return newKubernetesInventoryReporter(client, clientset, inventoryOptionsFromAgentConfig(cfg), parent), nil
}

func inventoryKubeconfig(cfg *config.AgentConfig) string {
	if cfg == nil {
		return ""
	}
	if value := strings.TrimSpace(cfg.Container.Kubeconfig); value != "" {
		return value
	}
	if value := strings.TrimSpace(cfg.SVC.Kubeconfig); value != "" {
		return value
	}
	if cfg.K8S.Enabled {
		return strings.TrimSpace(cfg.K8S.Kubeconfig)
	}
	return ""
}

func newKubernetesInventoryReporter(client pb.AgentServiceClient, clientset kubernetes.Interface, options kubernetesInventoryOptions, parent context.Context) *KubernetesInventoryReporter {
	return &KubernetesInventoryReporter{
		client: client, k8s: clientset, ctx: parent, options: options,
		supplyEpoch: uuid.NewString(), workEpoch: uuid.NewString(), wake: make(chan struct{}, 1),
	}
}

func inventoryOptionsFromAgentConfig(cfg *config.AgentConfig) kubernetesInventoryOptions {
	if cfg == nil {
		return kubernetesInventoryOptions{}
	}
	serviceNamespaces := normalizedInventoryNamespaces(cfg.SVC.Namespaces)
	containerNamespaces := normalizedInventoryNamespaces(cfg.Container.Namespaces)
	namespaces := normalizedInventoryNamespaces(append(append([]string(nil), serviceNamespaces...), containerNamespaces...))
	return kubernetesInventoryOptions{
		Namespaces: namespaces, ServiceNamespaces: serviceNamespaces, ContainerNamespaces: containerNamespaces,
		DisplayName:    strings.TrimSpace(cfg.Telemetry.Cluster),
		ServiceEnabled: cfg.SVC.Enabled, ServiceLabelSelector: cfg.SVC.LabelSelector,
		ContainerEnabled: cfg.Container.Enabled, ContainerLabelSelector: cfg.Container.LabelSelector,
	}
}

func normalizedInventoryNamespaces(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, namespace := range values {
		if namespace = strings.TrimSpace(namespace); namespace != "" {
			set[namespace] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for namespace := range set {
		result = append(result, namespace)
	}
	sort.Strings(result)
	return result
}

func (r *KubernetesInventoryReporter) Update(supply *pb.SupplyInventoryConfig, workload *pb.WorkloadInventoryConfig, options kubernetesInventoryOptions) {
	if r == nil {
		return
	}
	r.mu.Lock()
	changed := !proto.Equal(r.supplyConfig, supply) || !proto.Equal(r.workloadConfig, workload) || !equalInventoryOptions(r.options, options)
	if inventorySourceChanged(r.supplyConfig, supply) {
		r.supplyEpoch, r.supplySequence = uuid.NewString(), 0
	}
	if workloadInventorySourceChanged(r.workloadConfig, workload) {
		r.workEpoch, r.workSequence = uuid.NewString(), 0
	}
	r.supplyConfig = cloneSupplyInventoryConfig(supply)
	r.workloadConfig = cloneWorkloadInventoryConfig(workload)
	r.options = options
	r.mu.Unlock()
	if changed {
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
}

func inventorySourceChanged(oldConfig, newConfig *pb.SupplyInventoryConfig) bool {
	return oldConfig != nil && newConfig != nil && oldConfig.TechnicalResourceId != newConfig.TechnicalResourceId
}

func workloadInventorySourceChanged(oldConfig, newConfig *pb.WorkloadInventoryConfig) bool {
	return oldConfig != nil && newConfig != nil && oldConfig.TechnicalResourceId != newConfig.TechnicalResourceId
}

func equalInventoryOptions(left, right kubernetesInventoryOptions) bool {
	return left.DisplayName == right.DisplayName && left.ServiceEnabled == right.ServiceEnabled &&
		left.ServiceLabelSelector == right.ServiceLabelSelector && left.ContainerEnabled == right.ContainerEnabled &&
		left.ContainerLabelSelector == right.ContainerLabelSelector && stringSliceEqual(left.Namespaces, right.Namespaces) &&
		stringSliceEqual(left.ServiceNamespaces, right.ServiceNamespaces) && stringSliceEqual(left.ContainerNamespaces, right.ContainerNamespaces)
}

func cloneSupplyInventoryConfig(value *pb.SupplyInventoryConfig) *pb.SupplyInventoryConfig {
	if value == nil {
		return nil
	}
	return proto.Clone(value).(*pb.SupplyInventoryConfig)
}

func cloneWorkloadInventoryConfig(value *pb.WorkloadInventoryConfig) *pb.WorkloadInventoryConfig {
	if value == nil {
		return nil
	}
	return proto.Clone(value).(*pb.WorkloadInventoryConfig)
}

func (r *KubernetesInventoryReporter) Run() {
	if r == nil {
		return
	}
	ticker := time.NewTicker(k8sInventoryReportInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.wake:
			r.report()
		case <-ticker.C:
			r.report()
		case <-r.ctx.Done():
			return
		}
	}
}

func (r *KubernetesInventoryReporter) report() {
	supply, workload, options := r.currentConfig()
	if !validSupplyInventoryConfig(supply) || len(options.Namespaces) == 0 {
		return
	}
	snapshot, err := r.collect(options)
	if err != nil {
		logger.Warnf("采集 Kubernetes Inventory 失败: %v", err)
		return
	}
	if err := r.reportSupply(supply, snapshot); err != nil {
		logger.Warnf("上报 Kubernetes Supply Inventory 失败: %v", err)
		return
	}
	if validWorkloadInventoryConfig(workload) {
		if err := r.reportWorkloads(workload, snapshot, options); err != nil {
			logger.Warnf("上报 Kubernetes Workload Inventory 失败: %v", err)
		}
	}
}

func (r *KubernetesInventoryReporter) currentConfig() (*pb.SupplyInventoryConfig, *pb.WorkloadInventoryConfig, kubernetesInventoryOptions) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	options := r.options
	options.Namespaces = append([]string(nil), r.options.Namespaces...)
	options.ServiceNamespaces = append([]string(nil), r.options.ServiceNamespaces...)
	options.ContainerNamespaces = append([]string(nil), r.options.ContainerNamespaces...)
	return cloneSupplyInventoryConfig(r.supplyConfig), cloneWorkloadInventoryConfig(r.workloadConfig), options
}

func validSupplyInventoryConfig(cfg *pb.SupplyInventoryConfig) bool {
	return cfg != nil && cfg.Enabled && cfg.ProtocolVersion == k8sInventoryProtocolVersion &&
		cfg.TechnicalResourceId != "" && cfg.CredentialRevision > 0
}

func validWorkloadInventoryConfig(cfg *pb.WorkloadInventoryConfig) bool {
	return cfg != nil && cfg.Enabled && cfg.ProtocolVersion == k8sInventoryProtocolVersion &&
		cfg.Capability == "workload_inventory_v1" && cfg.TechnicalResourceId != "" && cfg.CredentialRevision > 0
}

func (r *KubernetesInventoryReporter) collect(options kubernetesInventoryOptions) (*kubernetesInventorySnapshot, error) {
	if r.k8s == nil {
		return nil, fmt.Errorf("Kubernetes client is unavailable")
	}
	ctx, cancel := context.WithTimeout(r.ctx, 20*time.Second)
	defer cancel()

	serverVersion, err := r.k8s.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("read Kubernetes version: %w", err)
	}
	kubeSystem, err := r.k8s.CoreV1().Namespaces().Get(ctx, metav1.NamespaceSystem, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("read kube-system identity: %w", err)
	}
	if kubeSystem.UID == "" {
		return nil, fmt.Errorf("kube-system identity is empty")
	}

	snapshot := &kubernetesInventorySnapshot{
		observedAt: time.Now().UTC(), namespaces: make(map[string]*corev1.Namespace, len(options.Namespaces)),
		servicePorts: make(map[string][]*pb.WorkloadServicePort), containers: make(map[string][]*pb.WorkloadContainer),
	}
	displayName := options.DisplayName
	if displayName == "" || displayName == "default" {
		displayName = "kubernetes"
	}
	snapshot.cluster = &pb.KubernetesClusterInventory{
		// An in-cluster Agent has no independent registered Cluster UID. Keep
		// the kube-system UID in both fields so existing strong-identity
		// candidates created by the S2 probe retain their stable lineage, while
		// the dedicated evidence field still records how the identity was read.
		ClusterUid: string(kubeSystem.UID), KubeSystemNamespaceUid: string(kubeSystem.UID), DisplayName: displayName,
		KubernetesVersion: serverVersion.GitVersion, Capabilities: []string{"kubernetes_api", "workload_inventory_v1"},
	}
	snapshot.clusterKey = stableInventoryDigest("kubernetes-cluster-v1:cluster_uid", string(kubeSystem.UID))

	serviceSelector, err := parseInventorySelector(options.ServiceEnabled, options.ServiceLabelSelector)
	if err != nil {
		return nil, fmt.Errorf("service selector: %w", err)
	}
	containerSelector, err := parseInventorySelector(options.ContainerEnabled, options.ContainerLabelSelector)
	if err != nil {
		return nil, fmt.Errorf("container selector: %w", err)
	}
	for _, namespaceName := range options.Namespaces {
		namespace, err := r.k8s.CoreV1().Namespaces().Get(ctx, namespaceName, metav1.GetOptions{})
		if err != nil {
			logger.Warnf("Kubernetes Inventory 跳过不可读 Namespace: namespace=%s err=%v", namespaceName, err)
			continue
		}
		snapshot.namespaces[namespaceName] = namespace
		snapshot.cluster.Namespaces = append(snapshot.cluster.Namespaces, &pb.KubernetesNamespaceInventory{
			Uid: string(namespace.UID), Name: namespace.Name, Labels: inventoryLabels(namespace.Labels),
			Status: strings.ToLower(string(namespace.Status.Phase)),
		})
		if options.ServiceEnabled && inventoryNamespaceIncluded(options.ServiceNamespaces, namespaceName) {
			services, err := r.k8s.CoreV1().Services(namespaceName).List(ctx, metav1.ListOptions{LabelSelector: serviceSelector})
			if err != nil {
				logger.Warnf("Kubernetes Inventory 跳过不可读 Service 快照: namespace=%s err=%v", namespaceName, err)
			} else {
				snapshot.servicePorts[namespaceName] = workloadServicePorts(services.Items)
			}
		}
		if options.ContainerEnabled && inventoryNamespaceIncluded(options.ContainerNamespaces, namespaceName) {
			pods, err := r.k8s.CoreV1().Pods(namespaceName).List(ctx, metav1.ListOptions{LabelSelector: containerSelector})
			if err != nil {
				logger.Warnf("Kubernetes Inventory 跳过不可读 Container 快照: namespace=%s err=%v", namespaceName, err)
			} else {
				snapshot.containers[namespaceName] = workloadContainers(pods.Items)
			}
		}
	}
	return snapshot, nil
}

func inventoryNamespaceIncluded(namespaces []string, target string) bool {
	index := sort.SearchStrings(namespaces, target)
	return index < len(namespaces) && namespaces[index] == target
}

func parseInventorySelector(enabled bool, raw string) (string, error) {
	if !enabled {
		return "", nil
	}
	selector, err := labels.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	return selector.String(), nil
}

func workloadServicePorts(services []corev1.Service) []*pb.WorkloadServicePort {
	result := make([]*pb.WorkloadServicePort, 0)
	for i := range services {
		service := &services[i]
		address, err := netip.ParseAddr(strings.TrimSpace(service.Spec.ClusterIP))
		if err != nil || address.IsUnspecified() || address.IsMulticast() {
			continue
		}
		for _, port := range service.Spec.Ports {
			if port.Protocol != corev1.ProtocolTCP || port.Port <= 0 {
				continue
			}
			result = append(result, &pb.WorkloadServicePort{
				ServiceUid: string(service.UID), ServiceName: service.Name, ClusterIp: address.String(),
				PortName: strings.ToLower(port.Name), PortNumber: uint32(port.Port), Protocol: string(port.Protocol),
				Ready: true, LabelsAllowlist: inventoryLabels(service.Labels),
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		return strings.Join([]string{left.ServiceUid, left.PortName, fmt.Sprint(left.PortNumber), left.Protocol}, "\x00") <
			strings.Join([]string{right.ServiceUid, right.PortName, fmt.Sprint(right.PortNumber), right.Protocol}, "\x00")
	})
	return result
}

func workloadContainers(pods []corev1.Pod) []*pb.WorkloadContainer {
	result := make([]*pb.WorkloadContainer, 0)
	for i := range pods {
		pod := &pods[i]
		owner := metav1.OwnerReference{}
		for _, candidate := range pod.OwnerReferences {
			if candidate.Controller != nil && *candidate.Controller {
				owner = candidate
				break
			}
		}
		for _, container := range pod.Spec.Containers {
			result = append(result, &pb.WorkloadContainer{
				WorkloadUid: string(owner.UID), WorkloadKind: owner.Kind, WorkloadName: owner.Name,
				PodUid: string(pod.UID), PodName: pod.Name, ContainerName: container.Name,
				Ready: containerReady(pod, container.Name), LabelsAllowlist: inventoryLabels(pod.Labels),
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		return strings.Join([]string{left.PodUid, left.ContainerName}, "\x00") < strings.Join([]string{right.PodUid, right.ContainerName}, "\x00")
	})
	return result
}

func inventoryLabels(source map[string]string) map[string]string {
	result := make(map[string]string)
	for rawKey, rawValue := range source {
		key, value := strings.TrimSpace(rawKey), strings.TrimSpace(rawValue)
		if key == "signal.beagle.io/expose" || key == "environment" || key == "team" || key == "owner" || strings.HasPrefix(key, "app.kubernetes.io/") {
			result[key] = value
		}
	}
	return result
}

func stableInventoryDigest(domain, value string) string {
	sum := sha256.Sum256([]byte(domain + "\x00" + strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func (r *KubernetesInventoryReporter) reportSupply(cfg *pb.SupplyInventoryConfig, snapshot *kubernetesInventorySnapshot) error {
	clusters := []*pb.KubernetesClusterInventory{snapshot.cluster}
	payload, err := canonicalSupplyInventoryPayload(clusters)
	if err != nil {
		return err
	}
	r.mu.RLock()
	sequence := r.supplySequence + 1
	epoch := r.supplyEpoch
	r.mu.RUnlock()
	now := time.Now().UTC()
	envelope := &pb.SupplyInventoryEnvelope{
		SchemaVersion: 1, SourceEpoch: epoch, Sequence: sequence, SnapshotId: uuid.NewString(), BatchIndex: 0, BatchCount: 1,
		ObservedAt: timestamppb.New(snapshot.observedAt), SentAt: timestamppb.New(now), PayloadHash: sha256Hex(payload),
		CredentialRevision: cfg.CredentialRevision, KubernetesClusters: clusters,
	}
	stream, err := r.client.ReportSupplyInventory(r.ctx)
	if err != nil {
		return err
	}
	defer stream.CloseSend()
	if err := stream.Send(envelope); err != nil {
		return err
	}
	ack, err := stream.Recv()
	if err != nil {
		return err
	}
	if ack.AcceptedSequence != sequence || ack.SnapshotId != envelope.SnapshotId || (!ack.SnapshotCommitted && !ack.Replay) {
		return fmt.Errorf("supply rejected: code=%s retryable=%v", ack.ResultCode, ack.Retryable)
	}
	r.mu.Lock()
	if r.supplySequence < sequence {
		r.supplySequence = sequence
	}
	r.mu.Unlock()
	return nil
}

func (r *KubernetesInventoryReporter) reportWorkloads(cfg *pb.WorkloadInventoryConfig, snapshot *kubernetesInventorySnapshot, options kubernetesInventoryOptions) error {
	for _, namespaceName := range options.Namespaces {
		namespace := snapshot.namespaces[namespaceName]
		if namespace == nil {
			continue
		}
		if options.ServiceEnabled && inventoryNamespaceIncluded(options.ServiceNamespaces, namespaceName) {
			ports, collected := snapshot.servicePorts[namespaceName]
			if collected {
				if err := r.reportWorkload(cfg, snapshot, namespace, "service_port", ports, nil); err != nil {
					return err
				}
			}
		}
		if options.ContainerEnabled && inventoryNamespaceIncluded(options.ContainerNamespaces, namespaceName) {
			containers, collected := snapshot.containers[namespaceName]
			if collected {
				if err := r.reportWorkload(cfg, snapshot, namespace, "container", nil, containers); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (r *KubernetesInventoryReporter) reportWorkload(cfg *pb.WorkloadInventoryConfig, snapshot *kubernetesInventorySnapshot, namespace *corev1.Namespace, kind string, ports []*pb.WorkloadServicePort, containers []*pb.WorkloadContainer) error {
	payload, err := canonicalWorkloadInventoryPayload(kind, ports, containers)
	if err != nil {
		return err
	}
	r.mu.RLock()
	sequence := r.workSequence + 1
	epoch := r.workEpoch
	r.mu.RUnlock()
	now := time.Now().UTC()
	envelope := &pb.WorkloadInventoryEnvelope{
		SchemaVersion: 1, SourceEpoch: epoch, Sequence: sequence, SnapshotId: uuid.NewString(), BatchIndex: 0, BatchCount: 1,
		ObservedAt: timestamppb.New(snapshot.observedAt), SentAt: timestamppb.New(now), PayloadHash: sha256Hex(payload),
		CredentialRevision: cfg.CredentialRevision, ClusterIdentityDigest: snapshot.clusterKey,
		NamespaceUid: string(namespace.UID), NamespaceName: namespace.Name, SnapshotKind: kind,
		ServicePorts: ports, Containers: containers,
	}
	stream, err := r.client.ReportWorkloadInventory(r.ctx)
	if err != nil {
		return err
	}
	defer stream.CloseSend()
	if err := stream.Send(envelope); err != nil {
		return err
	}
	ack, err := stream.Recv()
	if err != nil {
		return err
	}
	if ack.AcceptedSequence != sequence || ack.SnapshotId != envelope.SnapshotId || ack.BatchIndex != 0 || (!ack.Committed && !ack.Replayed) {
		return fmt.Errorf("workload rejected: kind=%s namespace=%s code=%s retryable=%v", kind, namespace.Name, ack.ResultCode, ack.Retryable)
	}
	r.mu.Lock()
	if r.workSequence < sequence {
		r.workSequence = sequence
	}
	r.mu.Unlock()
	return nil
}

type supplyPayload struct {
	KubernetesClusters []supplyPayloadCluster `json:"kubernetes_clusters"`
}

type supplyPayloadCluster struct {
	ClusterUID             string                   `json:"cluster_uid"`
	KubeSystemNamespaceUID string                   `json:"kube_system_namespace_uid"`
	CASHA256               string                   `json:"ca_sha256"`
	DisplayName            string                   `json:"display_name"`
	KubernetesVersion      string                   `json:"kubernetes_version"`
	Capabilities           []string                 `json:"capabilities"`
	Namespaces             []supplyPayloadNamespace `json:"namespaces"`
}

type supplyPayloadNamespace struct {
	UID    string            `json:"uid"`
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
	Status string            `json:"status"`
}

func canonicalSupplyInventoryPayload(clusters []*pb.KubernetesClusterInventory) ([]byte, error) {
	document := supplyPayload{KubernetesClusters: make([]supplyPayloadCluster, 0, len(clusters))}
	for _, cluster := range clusters {
		if cluster == nil {
			return nil, errors.New("nil Kubernetes cluster inventory")
		}
		item := supplyPayloadCluster{
			ClusterUID: cluster.ClusterUid, KubeSystemNamespaceUID: cluster.KubeSystemNamespaceUid, CASHA256: cluster.CaSha256,
			DisplayName: cluster.DisplayName, KubernetesVersion: cluster.KubernetesVersion,
			Capabilities: append([]string(nil), cluster.Capabilities...), Namespaces: make([]supplyPayloadNamespace, 0, len(cluster.Namespaces)),
		}
		for _, namespace := range cluster.Namespaces {
			if namespace == nil {
				return nil, errors.New("nil Kubernetes namespace inventory")
			}
			item.Namespaces = append(item.Namespaces, supplyPayloadNamespace{UID: namespace.Uid, Name: namespace.Name, Labels: namespace.Labels, Status: namespace.Status})
		}
		document.KubernetesClusters = append(document.KubernetesClusters, item)
	}
	return canonicalJSON(document)
}

type workloadPayload struct {
	ServicePorts []workloadServicePortPayload `json:"service_ports,omitempty"`
	Containers   []workloadContainerPayload   `json:"containers,omitempty"`
}

type workloadServicePortPayload struct {
	ServiceUID      string            `json:"service_uid"`
	ServiceName     string            `json:"service_name"`
	ClusterIP       string            `json:"cluster_ip"`
	PortName        string            `json:"port_name"`
	PortNumber      int               `json:"port_number"`
	Protocol        string            `json:"protocol"`
	Ready           bool              `json:"ready"`
	LabelsAllowlist map[string]string `json:"labels_allowlist"`
}

type workloadContainerPayload struct {
	WorkloadUID     string            `json:"workload_uid"`
	WorkloadKind    string            `json:"workload_kind"`
	WorkloadName    string            `json:"workload_name"`
	PodUID          string            `json:"pod_uid"`
	PodName         string            `json:"pod_name"`
	ContainerName   string            `json:"container_name"`
	Ready           bool              `json:"ready"`
	LabelsAllowlist map[string]string `json:"labels_allowlist"`
}

func canonicalWorkloadInventoryPayload(kind string, ports []*pb.WorkloadServicePort, containers []*pb.WorkloadContainer) ([]byte, error) {
	document := workloadPayload{}
	switch kind {
	case "service_port":
		document.ServicePorts = make([]workloadServicePortPayload, 0, len(ports))
		for _, item := range ports {
			if item == nil {
				return nil, errors.New("nil service port inventory")
			}
			document.ServicePorts = append(document.ServicePorts, workloadServicePortPayload{
				ServiceUID: item.ServiceUid, ServiceName: item.ServiceName, ClusterIP: item.ClusterIp, PortName: item.PortName,
				PortNumber: int(item.PortNumber), Protocol: item.Protocol, Ready: item.Ready, LabelsAllowlist: item.LabelsAllowlist,
			})
		}
	case "container":
		document.Containers = make([]workloadContainerPayload, 0, len(containers))
		for _, item := range containers {
			if item == nil {
				return nil, errors.New("nil container inventory")
			}
			document.Containers = append(document.Containers, workloadContainerPayload{
				WorkloadUID: item.WorkloadUid, WorkloadKind: item.WorkloadKind, WorkloadName: item.WorkloadName,
				PodUID: item.PodUid, PodName: item.PodName, ContainerName: item.ContainerName,
				Ready: item.Ready, LabelsAllowlist: item.LabelsAllowlist,
			})
		}
	default:
		return nil, fmt.Errorf("unsupported workload inventory kind %q", kind)
	}
	return canonicalJSON(document)
}

func canonicalJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var canonical any
	if err := json.Unmarshal(raw, &canonical); err != nil {
		return nil, err
	}
	return json.Marshal(canonical)
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
