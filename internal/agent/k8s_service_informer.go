package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	agentconfig "github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// DiscoveredService Agent 发现的 K8S Service
type DiscoveredService struct {
	Namespace string
	Name      string
	ClusterIP string
	Ports     []DiscoveredServicePort
	Labels    map[string]string
	Alias     string // signal.beagle.io/alias 标签值
}

// DiscoveredServicePort 发现的 Service 端口
type DiscoveredServicePort struct {
	Name     string
	Port     int32
	Protocol string
}

// K8SServiceInformer 使用 client-go Informer 监听 K8S Service 变更
type K8SServiceInformer struct {
	config    *agentconfig.SVCSection
	clientset kubernetes.Interface
	factory   informers.SharedInformerFactory

	// 发现的 Service 列表
	services map[string]*DiscoveredService // key: namespace/name
	mutex    sync.RWMutex

	// 变更通知
	onChange func() // 变更时回调（通知心跳模块上报）

	ctx    context.Context
	cancel context.CancelFunc
}

// NewK8SServiceInformer 创建 K8S Service Informer
func NewK8SServiceInformer(cfg *agentconfig.SVCSection, onChange func(), parentCtx context.Context) (*K8SServiceInformer, error) {
	ctx, cancel := context.WithCancel(parentCtx)

	// 创建 kubernetes clientset
	clientset, err := createK8SClientset(cfg.Kubeconfig)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建 K8S 客户端失败: %w", err)
	}

	return &K8SServiceInformer{
		config:    cfg,
		clientset: clientset,
		services:  make(map[string]*DiscoveredService),
		onChange:  onChange,
		ctx:       ctx,
		cancel:    cancel,
	}, nil
}

// Start 启动 Informer
func (i *K8SServiceInformer) Start() error {
	// 解析标签选择器
	selector, err := labels.Parse(i.config.LabelSelector)
	if err != nil {
		return fmt.Errorf("解析标签选择器失败: %w", err)
	}

	// 创建 Informer Factory
	// 根据配置的命名空间列表决定监听范围
	tweakListOptions := func(opts *metav1.ListOptions) {
		opts.LabelSelector = selector.String()
	}

	if len(i.config.Namespaces) > 0 {
		// 监听指定命名空间（为每个命名空间创建 Informer）
		for _, ns := range i.config.Namespaces {
			factory := informers.NewSharedInformerFactoryWithOptions(
				i.clientset,
				30*time.Second,
				informers.WithNamespace(ns),
				informers.WithTweakListOptions(tweakListOptions),
			)
			i.setupInformer(factory)
			factory.Start(i.ctx.Done())
		}
	} else {
		// 监听所有命名空间
		factory := informers.NewSharedInformerFactoryWithOptions(
			i.clientset,
			30*time.Second,
			informers.WithTweakListOptions(tweakListOptions),
		)
		i.factory = factory
		i.setupInformer(factory)
		factory.Start(i.ctx.Done())
	}

	logger.Infof("K8S Service Informer 已启动: selector=%s", i.config.LabelSelector)
	return nil
}

// setupInformer 设置 Informer 事件处理
func (i *K8SServiceInformer) setupInformer(factory informers.SharedInformerFactory) {
	serviceInformer := factory.Core().V1().Services().Informer()
	serviceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			svc, ok := obj.(*corev1.Service)
			if !ok {
				return
			}
			i.onServiceAdd(svc)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			svc, ok := newObj.(*corev1.Service)
			if !ok {
				return
			}
			i.onServiceUpdate(svc)
		},
		DeleteFunc: func(obj interface{}) {
			svc, ok := obj.(*corev1.Service)
			if !ok {
				return
			}
			i.onServiceDelete(svc)
		},
	})
}

// onServiceAdd 处理 Service 添加事件
func (i *K8SServiceInformer) onServiceAdd(svc *corev1.Service) {
	ds := i.convertService(svc)
	key := svc.Namespace + "/" + svc.Name

	i.mutex.Lock()
	i.services[key] = ds
	i.mutex.Unlock()

	logger.Infof("K8S Service 发现: %s/%s (ClusterIP=%s, ports=%d)",
		svc.Namespace, svc.Name, svc.Spec.ClusterIP, len(svc.Spec.Ports))

	if i.onChange != nil {
		i.onChange()
	}
}

// onServiceUpdate 处理 Service 更新事件
func (i *K8SServiceInformer) onServiceUpdate(svc *corev1.Service) {
	ds := i.convertService(svc)
	key := svc.Namespace + "/" + svc.Name

	i.mutex.Lock()
	i.services[key] = ds
	i.mutex.Unlock()

	logger.Debugf("K8S Service 更新: %s/%s", svc.Namespace, svc.Name)

	if i.onChange != nil {
		i.onChange()
	}
}

// onServiceDelete 处理 Service 删除事件
func (i *K8SServiceInformer) onServiceDelete(svc *corev1.Service) {
	key := svc.Namespace + "/" + svc.Name

	i.mutex.Lock()
	delete(i.services, key)
	i.mutex.Unlock()

	logger.Infof("K8S Service 移除: %s/%s", svc.Namespace, svc.Name)

	if i.onChange != nil {
		i.onChange()
	}
}

// convertService 将 K8S Service 转换为 DiscoveredService
func (i *K8SServiceInformer) convertService(svc *corev1.Service) *DiscoveredService {
	ports := make([]DiscoveredServicePort, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		ports = append(ports, DiscoveredServicePort{
			Name:     p.Name,
			Port:     p.Port,
			Protocol: string(p.Protocol),
		})
	}

	alias := svc.Labels["signal.beagle.io/alias"]

	return &DiscoveredService{
		Namespace: svc.Namespace,
		Name:      svc.Name,
		ClusterIP: svc.Spec.ClusterIP,
		Ports:     ports,
		Labels:    svc.Labels,
		Alias:     alias,
	}
}

// GetDiscoveredServices 获取所有发现的 Service
func (i *K8SServiceInformer) GetDiscoveredServices() []*DiscoveredService {
	i.mutex.RLock()
	defer i.mutex.RUnlock()

	result := make([]*DiscoveredService, 0, len(i.services))
	for _, svc := range i.services {
		result = append(result, svc)
	}
	return result
}

// FindService 根据 namespace + name 查找 Service
func (i *K8SServiceInformer) FindService(namespace, name string) *DiscoveredService {
	i.mutex.RLock()
	defer i.mutex.RUnlock()
	return i.services[namespace+"/"+name]
}

// Stop 停止 Informer
func (i *K8SServiceInformer) Stop() {
	i.cancel()
	logger.Info("K8S Service Informer 已停止")
}

// createK8SClientset 创建 K8S 客户端
// 优先级：显式 kubeconfig > InCluster > ~/.kube/config
func createK8SClientset(kubeconfig string) (kubernetes.Interface, error) {
	config, err := loadK8SRESTConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("创建 clientset 失败: %w", err)
	}
	return clientset, nil
}

// loadK8SRESTConfig is shared by discovery and ContainerSSH exec so both use
// the exact same kubeconfig/in-cluster identity and RBAC boundary.
func loadK8SRESTConfig(kubeconfig string) (*rest.Config, error) {
	var config *rest.Config
	var err error

	if kubeconfig != "" {
		// 显式指定 kubeconfig，展开 ~
		if strings.HasPrefix(kubeconfig, "~/") {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("获取用户主目录失败: %w", err)
			}
			kubeconfig = filepath.Join(homeDir, kubeconfig[2:])
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("从 kubeconfig 创建配置失败: %w", err)
		}
		logger.Infof("使用 kubeconfig: %s", kubeconfig)
	} else {
		// 尝试 InCluster（Pod 内部署）
		config, err = rest.InClusterConfig()
		if err != nil {
			// InCluster 失败，降级到 ~/.kube/config（物理节点部署）
			logger.Infof("InCluster 配置不可用，降级到 kubeconfig: %v", err)
			kubeconfigPath := os.Getenv("KUBECONFIG")
			if kubeconfigPath == "" {
				homeDir, err := os.UserHomeDir()
				if err != nil {
					return nil, fmt.Errorf("获取用户主目录失败: %w", err)
				}
				kubeconfigPath = filepath.Join(homeDir, ".kube", "config")
			}
			config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
			if err != nil {
				return nil, fmt.Errorf("kubeconfig 配置失败 (%s): %w", kubeconfigPath, err)
			}
			logger.Infof("使用 kubeconfig: %s", kubeconfigPath)
		} else {
			logger.Info("使用 InCluster K8S 配置")
		}
	}

	return config, nil
}
