package agent

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	k8sversion "k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

type staticContainerUserDiscoverer struct {
	user string
	err  error
}

func (d staticContainerUserDiscoverer) Discover(context.Context, string, string, string) (string, error) {
	return d.user, d.err
}

func TestInventoryOptionsRequireExplicitNamespaceUnion(t *testing.T) {
	cfg := &config.AgentConfig{}
	cfg.SVC.Namespaces = []string{"tenant-b", " tenant-a ", "tenant-b"}
	cfg.Container.Namespaces = []string{"tenant-a", "tenant-c"}
	cfg.SVC.Enabled = true
	cfg.Container.Enabled = true

	options := inventoryOptionsFromAgentConfig(cfg)
	require.Equal(t, []string{"tenant-a", "tenant-b", "tenant-c"}, options.Namespaces)
	require.Equal(t, []string{"tenant-a", "tenant-b"}, options.ServiceNamespaces)
	require.Equal(t, []string{"tenant-a", "tenant-c"}, options.ContainerNamespaces)

	cfg.SVC.Namespaces = nil
	cfg.Container.Namespaces = nil
	require.Empty(t, inventoryOptionsFromAgentConfig(cfg).Namespaces)
}

func TestInventoryKubeconfigPrefersExplicitDiscoveryIdentity(t *testing.T) {
	cfg := &config.AgentConfig{}
	cfg.K8S.Enabled = true
	cfg.K8S.Kubeconfig = "api.conf"
	cfg.SVC.Kubeconfig = "svc.conf"
	cfg.Container.Kubeconfig = "container.conf"
	require.Equal(t, "container.conf", inventoryKubeconfig(cfg))
	cfg.Container.Kubeconfig = ""
	require.Equal(t, "svc.conf", inventoryKubeconfig(cfg))
	cfg.SVC.Kubeconfig = ""
	require.Equal(t, "api.conf", inventoryKubeconfig(cfg))
	cfg.K8S.Enabled = false
	require.Empty(t, inventoryKubeconfig(cfg))
}

func TestInventoryReporterResetsSequenceWhenTrustedSourceChanges(t *testing.T) {
	reporter := newKubernetesInventoryReporter(nil, nil, kubernetesInventoryOptions{}, context.Background())
	reporter.supplyConfig = &pb.SupplyInventoryConfig{TechnicalResourceId: "agent-a"}
	reporter.workloadConfig = &pb.WorkloadInventoryConfig{TechnicalResourceId: "agent-a"}
	reporter.supplySequence = 9
	reporter.workSequence = 11
	oldSupplyEpoch, oldWorkEpoch := reporter.supplyEpoch, reporter.workEpoch

	reporter.Update(
		&pb.SupplyInventoryConfig{TechnicalResourceId: "agent-b"},
		&pb.WorkloadInventoryConfig{TechnicalResourceId: "agent-b"},
		kubernetesInventoryOptions{},
	)
	require.Zero(t, reporter.supplySequence)
	require.Zero(t, reporter.workSequence)
	require.NotEqual(t, oldSupplyEpoch, reporter.supplyEpoch)
	require.NotEqual(t, oldWorkEpoch, reporter.workEpoch)
}

func TestKubernetesInventoryCollectsOnlyConfiguredSelectedWorkloads(t *testing.T) {
	kubeSystem := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: metav1.NamespaceSystem, UID: types.UID("system-uid")}}
	tenant := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant-a", UID: types.UID("namespace-a"), Labels: map[string]string{"team": "a", "tenant_id": "forbidden"}},
		Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
	}
	ignoredNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-b", UID: types.UID("namespace-b")}}
	selectedService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "tenant-a", UID: types.UID("service-a"), Labels: map[string]string{"signal.beagle.io/expose": "true", "tenant_id": "forbidden"}},
		Spec:       corev1.ServiceSpec{ClusterIP: "10.96.0.10", Ports: []corev1.ServicePort{{Name: "HTTPS", Port: 443, Protocol: corev1.ProtocolTCP}, {Name: "dns", Port: 53, Protocol: corev1.ProtocolUDP}}},
	}
	ignoredService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "private", Namespace: "tenant-a", UID: types.UID("service-private")},
		Spec:       corev1.ServiceSpec{ClusterIP: "10.96.0.11", Ports: []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}}},
	}
	selectedPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "shell", Namespace: "tenant-a", UID: types.UID("pod-a"),
			Labels:          map[string]string{"signal.beagle.io/container-ssh": "true", "team": "a", "resource_id": "forbidden"},
			OwnerReferences: []metav1.OwnerReference{{UID: types.UID("deployment-a"), Kind: "Deployment", Name: "shell", Controller: boolPointer(true)}},
		},
		Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "shell"}}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "shell", Ready: true}}},
	}
	client := kubernetesfake.NewSimpleClientset(kubeSystem, tenant, ignoredNamespace, selectedService, ignoredService, selectedPod)
	discovery := client.Discovery().(*fake.FakeDiscovery)
	discovery.FakedServerVersion = &k8sversion.Info{GitVersion: "v1.30.14"}
	reporter := newKubernetesInventoryReporter(nil, client, kubernetesInventoryOptions{}, context.Background())
	reporter.users = staticContainerUserDiscoverer{user: "code"}

	snapshot, err := reporter.collect(kubernetesInventoryOptions{
		Namespaces: []string{"tenant-a", "tenant-b"}, ServiceNamespaces: []string{"tenant-a", "tenant-b"}, ContainerNamespaces: []string{"tenant-a"},
		ServiceEnabled: true, ServiceLabelSelector: "signal.beagle.io/expose=true",
		ContainerEnabled: true, ContainerLabelSelector: "signal.beagle.io/container-ssh=true",
	})
	require.NoError(t, err)
	require.Equal(t, stableInventoryDigest("kubernetes-cluster-v1:cluster_uid", "system-uid"), snapshot.clusterKey)
	require.Equal(t, "v1.30.14", snapshot.cluster.KubernetesVersion)
	require.Len(t, snapshot.cluster.Namespaces, 2)
	require.Equal(t, map[string]string{"team": "a"}, snapshot.cluster.Namespaces[0].Labels)
	require.Len(t, snapshot.servicePorts["tenant-a"], 1)
	require.NotContains(t, snapshot.servicePorts, "tenant-b")
	require.Equal(t, uint32(443), snapshot.servicePorts["tenant-a"][0].PortNumber)
	require.Equal(t, map[string]string{"signal.beagle.io/expose": "true"}, snapshot.servicePorts["tenant-a"][0].LabelsAllowlist)
	require.Len(t, snapshot.containers["tenant-a"], 1)
	require.Equal(t, "deployment-a", snapshot.containers["tenant-a"][0].WorkloadUid)
	require.Equal(t, []string{"code"}, snapshot.containers["tenant-a"][0].SshUsers)
	require.Equal(t, map[string]string{"team": "a"}, snapshot.containers["tenant-a"][0].LabelsAllowlist)

	actions := client.Actions()
	namespaceLists := 0
	serviceLists := 0
	for _, action := range actions {
		if action.GetVerb() == "list" && action.GetResource() == (schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}) {
			namespaceLists++
		}
		if action.GetVerb() == "list" && action.GetResource() == (schema.GroupVersionResource{Version: "v1", Resource: "services"}) {
			serviceLists++
		}
	}
	require.Equal(t, 1, namespaceLists)
	require.Equal(t, 1, serviceLists)
}

func TestParseNamespacesUsesManagementJSONEncoding(t *testing.T) {
	require.Equal(t, []string{"tenant-a", "tenant-b"}, parseNamespaces(`["tenant-a"," tenant-b ",""]`))
	require.Empty(t, parseNamespaces("tenant-a,tenant-b"))
}

func TestWorkloadContainerUserDiscoveryFailureMarksContainerNotReady(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "ide-public-0", Namespace: "beagle-ide", UID: types.UID("pod-a")},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "ide"}}},
		Status:     corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "ide", Ready: true}}},
	}
	containers := workloadContainersWithUsers(context.Background(), staticContainerUserDiscoverer{
		err: fmt.Errorf("id -un failed"),
	}, []corev1.Pod{pod}, nil)
	require.Len(t, containers, 1)
	require.False(t, containers[0].Ready)
	require.Empty(t, containers[0].SshUsers)
}

func TestCanonicalInventoryPayloadsMatchWireContract(t *testing.T) {
	supply, err := canonicalSupplyInventoryPayload([]*pb.KubernetesClusterInventory{{
		KubeSystemNamespaceUid: "system-uid", DisplayName: "cluster", KubernetesVersion: "v1",
		Capabilities: []string{"workload_inventory_v1"},
		Namespaces:   []*pb.KubernetesNamespaceInventory{{Uid: "namespace-a", Name: "tenant-a", Labels: map[string]string{}, Status: "active"}},
	}})
	require.NoError(t, err)
	require.JSONEq(t, `{"kubernetes_clusters":[{"cluster_uid":"","kube_system_namespace_uid":"system-uid","ca_sha256":"","display_name":"cluster","kubernetes_version":"v1","capabilities":["workload_inventory_v1"],"namespaces":[{"uid":"namespace-a","name":"tenant-a","labels":null,"status":"active"}]}]}`, string(supply))

	workload, err := canonicalWorkloadInventoryPayload("service_port", []*pb.WorkloadServicePort{{
		ServiceUid: "service-a", ServiceName: "api", ClusterIp: "10.96.0.10", PortName: "https",
		PortNumber: 443, Protocol: "TCP", Ready: true, LabelsAllowlist: map[string]string{},
	}}, nil)
	require.NoError(t, err)
	require.JSONEq(t, `{"service_ports":[{"service_uid":"service-a","service_name":"api","cluster_ip":"10.96.0.10","port_name":"https","port_number":443,"protocol":"TCP","ready":true,"labels_allowlist":null}]}`, string(workload))
}

func TestCanonicalSupplyPayloadSurvivesProtobufRoundTrip(t *testing.T) {
	before := []*pb.KubernetesClusterInventory{{
		ClusterUid: "cluster-a", KubeSystemNamespaceUid: "system-a",
		Namespaces: []*pb.KubernetesNamespaceInventory{{Uid: "namespace-a", Name: "tenant-a", Labels: map[string]string{}}},
	}}
	raw, err := proto.Marshal(before[0])
	require.NoError(t, err)
	after := &pb.KubernetesClusterInventory{}
	require.NoError(t, proto.Unmarshal(raw, after))
	beforePayload, err := canonicalSupplyInventoryPayload(before)
	require.NoError(t, err)
	afterPayload, err := canonicalSupplyInventoryPayload([]*pb.KubernetesClusterInventory{after})
	require.NoError(t, err)
	require.Equal(t, beforePayload, afterPayload)
}

type inventoryAckServer struct {
	pb.UnimplementedAgentServiceServer
	supply    chan *pb.SupplyInventoryEnvelope
	workloads chan *pb.WorkloadInventoryEnvelope
}

func (s *inventoryAckServer) ReportSupplyInventory(stream pb.AgentService_ReportSupplyInventoryServer) error {
	for {
		envelope, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		s.supply <- envelope
		if err := stream.Send(&pb.SupplyInventoryAck{AcceptedSequence: envelope.Sequence, SnapshotId: envelope.SnapshotId, ResultCode: "SNAPSHOT_COMMITTED", SnapshotCommitted: true}); err != nil {
			return err
		}
	}
}

func (s *inventoryAckServer) ReportWorkloadInventory(stream pb.AgentService_ReportWorkloadInventoryServer) error {
	for {
		envelope, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		s.workloads <- envelope
		if err := stream.Send(&pb.WorkloadInventoryAck{AcceptedSequence: envelope.Sequence, SnapshotId: envelope.SnapshotId, BatchIndex: envelope.BatchIndex, ResultCode: "WORKLOAD_ACCEPTED", Committed: true}); err != nil {
			return err
		}
	}
}

func TestInventoryReporterAdvancesSequencesOnlyAfterCommittedAck(t *testing.T) {
	server := &inventoryAckServer{supply: make(chan *pb.SupplyInventoryEnvelope, 1), workloads: make(chan *pb.WorkloadInventoryEnvelope, 2)}
	client, stop := startAgentSupplyInventoryTestServer(t, server)
	defer stop()
	reporter := newKubernetesInventoryReporter(client, nil, kubernetesInventoryOptions{}, context.Background())
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a", UID: types.UID("namespace-a")}}
	snapshot := &kubernetesInventorySnapshot{
		observedAt: time.Now().UTC(), clusterKey: stableInventoryDigest("cluster", "a"),
		cluster:      &pb.KubernetesClusterInventory{KubeSystemNamespaceUid: "system-uid", Namespaces: []*pb.KubernetesNamespaceInventory{{Uid: "namespace-a", Name: "tenant-a"}}},
		namespaces:   map[string]*corev1.Namespace{"tenant-a": namespace},
		servicePorts: map[string][]*pb.WorkloadServicePort{"tenant-a": {}}, containers: map[string][]*pb.WorkloadContainer{"tenant-a": {}},
	}
	supplyConfig := &pb.SupplyInventoryConfig{Enabled: true, ProtocolVersion: "v1", TechnicalResourceId: "agent-a", CredentialRevision: 3}
	workloadConfig := &pb.WorkloadInventoryConfig{Enabled: true, ProtocolVersion: "v1", TechnicalResourceId: "agent-a", CredentialRevision: 3, Capability: "workload_inventory_v1"}

	require.NoError(t, reporter.reportSupply(supplyConfig, snapshot))
	require.NoError(t, reporter.reportWorkloads(workloadConfig, snapshot, kubernetesInventoryOptions{
		Namespaces: []string{"tenant-a"}, ServiceNamespaces: []string{"tenant-a"}, ContainerNamespaces: []string{"tenant-a"},
		ServiceEnabled: true, ContainerEnabled: true,
	}))
	require.Equal(t, uint64(1), reporter.supplySequence)
	require.Equal(t, uint64(2), reporter.workSequence)
	require.Equal(t, uint64(1), (<-server.supply).Sequence)
	require.Equal(t, uint64(1), (<-server.workloads).Sequence)
	require.Equal(t, uint64(2), (<-server.workloads).Sequence)
}

func TestWorkloadContainersResolveReplicaSetOwnerToDeployment(t *testing.T) {
	deploymentOwner := metav1.OwnerReference{
		APIVersion: "apps/v1", Kind: "Deployment", Name: "ide-public", UID: types.UID("deployment-uid"), Controller: boolPointer(true),
	}
	replicaSet := appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Name: "ide-public-6ffbc8766b", UID: types.UID("replicaset-uid"), OwnerReferences: []metav1.OwnerReference{deploymentOwner},
	}}
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ide-public-6ffbc8766b-6nrrz", UID: types.UID("pod-uid"),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "ReplicaSet", Name: replicaSet.Name, UID: replicaSet.UID, Controller: boolPointer(true),
			}},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "ide"}}},
	}

	containers := workloadContainers([]corev1.Pod{pod}, []appsv1.ReplicaSet{replicaSet})
	require.Len(t, containers, 1)
	require.Equal(t, "Deployment", containers[0].WorkloadKind)
	require.Equal(t, "ide-public", containers[0].WorkloadName)
	require.Equal(t, "deployment-uid", containers[0].WorkloadUid)
}

func TestWorkloadContainersKeepDirectControllerIdentity(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "database-0", UID: types.UID("pod-uid"),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "StatefulSet", Name: "database", UID: types.UID("statefulset-uid"), Controller: boolPointer(true),
			}},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "database"}}},
	}

	containers := workloadContainers([]corev1.Pod{pod}, nil)
	require.Len(t, containers, 1)
	require.Equal(t, "StatefulSet", containers[0].WorkloadKind)
	require.Equal(t, "database", containers[0].WorkloadName)
	require.Equal(t, "statefulset-uid", containers[0].WorkloadUid)
}

func boolPointer(value bool) *bool { return &value }
