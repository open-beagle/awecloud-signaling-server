package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// K8SServiceDiscovery K8S Service 自动发现
type K8SServiceDiscovery struct {
	cfg            *EndpointConfig
	mu             sync.RWMutex
	discoveredSvcs []*pb.DiscoveredK8SService
	ctx            context.Context
	cancel         context.CancelFunc
	k8sClient      *kubernetes.Clientset
	k8sAPIServer   string
}

// NewK8SServiceDiscovery 创建 K8S Service 自动发现
func NewK8SServiceDiscovery(cfg *EndpointConfig, parentCtx context.Context) *K8SServiceDiscovery {
	ctx, cancel := context.WithCancel(parentCtx)

	return &K8SServiceDiscovery{
		cfg:    cfg,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start 启动自动发现
func (d *K8SServiceDiscovery) Start() error {
	// 初始化 K8S API 连接信息
	if err := d.initK8SConnection(); err != nil {
		return fmt.Errorf("初始化 K8S 连接失败: %w", err)
	}

	logger.Infof("K8S Service 自动发现已启动: api_server=%s", d.k8sAPIServer)

	// 立即执行一次发现
	d.discover()

	// 启动定期发现（每 30 秒）
	go d.discoveryLoop()

	return nil
}

// Stop 停止自动发现
func (d *K8SServiceDiscovery) Stop() {
	d.cancel()
}

// GetDiscoveredServices 获取发现的 Service 列表
func (d *K8SServiceDiscovery) GetDiscoveredServices() []*pb.DiscoveredK8SService {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// 返回副本
	result := make([]*pb.DiscoveredK8SService, len(d.discoveredSvcs))
	copy(result, d.discoveredSvcs)
	return result
}

func (d *K8SServiceDiscovery) ResolveService(namespace, name, uid, portName string, port int32, protocol string) (string, bool) {
	if d == nil {
		return "", false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, svc := range d.discoveredSvcs {
		if svc.Namespace != namespace || svc.ServiceName != name || svc.ServiceUid != uid || svc.ClusterIp == "" {
			continue
		}
		for _, candidate := range svc.Ports {
			if candidate.Name == portName && candidate.Port == port && candidate.Protocol == protocol {
				return svc.ClusterIp, true
			}
		}
	}
	return "", false
}

// initK8SConnection 初始化 K8S API 连接信息
func (d *K8SServiceDiscovery) initK8SConnection() error {
	// 使用配置文件中的 API Server 地址
	d.k8sAPIServer = d.cfg.K8S.APIServer

	var restConfig *rest.Config
	var err error

	// 优先尝试 InCluster 配置（Pod 内部署）
	restConfig, err = rest.InClusterConfig()
	if err == nil {
		logger.Info("使用 InCluster 配置（Service Account）")
	} else {
		// 降级：从 kubeconfig 加载配置（物理节点部署）
		logger.Infof("InCluster 配置不可用: %v，尝试加载 kubeconfig", err)

		kubeconfigPath := os.Getenv("KUBECONFIG")
		if kubeconfigPath == "" {
			homeDir, err := os.UserHomeDir()
			if err == nil {
				kubeconfigPath = homeDir + "/.kube/config"
			}
		}

		if kubeconfigPath == "" {
			return fmt.Errorf("无法确定 kubeconfig 路径")
		}

		logger.Infof("尝试加载 kubeconfig: %s", kubeconfigPath)

		// 使用 client-go 加载 kubeconfig
		restConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return fmt.Errorf("加载 kubeconfig 失败: %w", err)
		}

		logger.Infof("从 kubeconfig 加载配置成功: %s", kubeconfigPath)
	}

	// 创建 Kubernetes 客户端
	d.k8sClient, err = kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("创建 Kubernetes 客户端失败: %w", err)
	}

	logger.Info("Kubernetes 客户端创建成功")
	return nil
}

// discoveryLoop 定期发现循环
func (d *K8SServiceDiscovery) discoveryLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.discover()
		}
	}
}

// discover 执行一次 Service 发现
func (d *K8SServiceDiscovery) discover() {
	services, err := d.listServices()
	if err != nil {
		logger.Warnf("K8S Service 发现失败: %v", err)
		return
	}

	// 更新发现的 Service 列表
	d.mu.Lock()
	d.discoveredSvcs = services
	d.mu.Unlock()

	if len(services) > 0 {
		logger.Infof("K8S Service 发现完成: 共 %d 个对外暴露的 Service", len(services))
		// 打印每个发现的 Service
		for _, svc := range services {
			var portList []string
			for _, p := range svc.Ports {
				portList = append(portList, fmt.Sprintf("%d/%s", p.Port, p.Protocol))
			}
			logger.Infof("  - %s.%s (ClusterIP: %s, Ports: %s)",
				svc.ServiceName, svc.Namespace, svc.ClusterIp, strings.Join(portList, ", "))
		}
	} else {
		logger.Info("K8S Service 发现完成: 未发现对外暴露的 Service")
	}
}

// listServices 查询 K8S 集群中的所有 Service
func (d *K8SServiceDiscovery) listServices() ([]*pb.DiscoveredK8SService, error) {
	// 使用 Kubernetes 客户端查询所有命名空间的 Service
	serviceList, err := d.k8sClient.CoreV1().Services("").List(d.ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("查询 K8S Service 失败: %w", err)
	}

	// 转换为 DiscoveredK8SService
	var result []*pb.DiscoveredK8SService
	for _, svc := range serviceList.Items {
		// 过滤系统命名空间
		if isSystemNamespace(svc.Namespace) {
			continue
		}

		// 过滤没有 ClusterIP 的 Service（如 Headless Service）
		if svc.Spec.ClusterIP == "" || svc.Spec.ClusterIP == "None" {
			continue
		}

		// 只处理标记为对外暴露的 Service（检查 labels 和 annotations）
		expose := svc.Labels["signal.beagle.io/expose"] == "true" ||
			svc.Annotations["signal.beagle.io/expose"] == "true"
		if !expose {
			continue
		}

		// 解析端口列表
		var ports []*pb.ServicePort
		for _, p := range svc.Spec.Ports {
			ports = append(ports, &pb.ServicePort{
				Port:     int32(p.Port),
				Protocol: string(p.Protocol),
				Name:     p.Name,
			})
		}

		if len(ports) == 0 {
			continue
		}

		result = append(result, &pb.DiscoveredK8SService{
			Namespace:   svc.Namespace,
			ServiceName: svc.Name,
			ClusterIp:   svc.Spec.ClusterIP,
			Ports:       ports,
			ServiceUid:  string(svc.UID),
		})
	}

	return result, nil
}

// isSystemNamespace 判断是否为系统命名空间
func isSystemNamespace(ns string) bool {
	systemNamespaces := []string{
		"kube-system",
		"kube-public",
		"kube-node-lease",
		"default", // 可选：是否过滤 default 命名空间
	}

	for _, sysNs := range systemNamespaces {
		if ns == sysNs {
			return true
		}
	}

	// 过滤以 kube- 开头的命名空间
	if strings.HasPrefix(ns, "kube-") {
		return true
	}

	return false
}
