package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// K8SServiceDiscovery K8S Service 自动发现
type K8SServiceDiscovery struct {
	cfg              *EndpointConfig
	mu               sync.RWMutex
	discoveredSvcs   []*pb.DiscoveredK8SService
	ctx              context.Context
	cancel           context.CancelFunc
	httpClient       *http.Client
	k8sToken         string
	k8sAPIServer     string
}

// NewK8SServiceDiscovery 创建 K8S Service 自动发现
func NewK8SServiceDiscovery(cfg *EndpointConfig, parentCtx context.Context) *K8SServiceDiscovery {
	ctx, cancel := context.WithCancel(parentCtx)
	
	// 创建 HTTP 客户端（跳过 TLS 验证）
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	
	return &K8SServiceDiscovery{
		cfg:        cfg,
		ctx:        ctx,
		cancel:     cancel,
		httpClient: httpClient,
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

// initK8SConnection 初始化 K8S API 连接信息
func (d *K8SServiceDiscovery) initK8SConnection() error {
	// 使用配置文件中的 API Server 地址
	d.k8sAPIServer = d.cfg.K8S.APIServer
	
	// 读取 Service Account Token（Pod 内部署）
	tokenBytes, err := readFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err == nil {
		d.k8sToken = string(tokenBytes)
		logger.Info("从 Service Account 读取 K8S Token 成功")
	} else {
		logger.Warnf("读取 Service Account Token 失败: %v（将尝试使用 kubeconfig）", err)
	}
	
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
	
	logger.Infof("K8S Service 发现完成: 共 %d 个 Service", len(services))
}

// listServices 查询 K8S 集群中的所有 Service
func (d *K8SServiceDiscovery) listServices() ([]*pb.DiscoveredK8SService, error) {
	// 构建 API 请求
	url := fmt.Sprintf("%s/api/v1/services", d.k8sAPIServer)
	
	req, err := http.NewRequestWithContext(d.ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	// 添加认证 Token
	if d.k8sToken != "" {
		req.Header.Set("Authorization", "Bearer "+d.k8sToken)
	}
	
	// 发送请求
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("K8S API 返回错误: status=%d, body=%s", resp.StatusCode, string(body))
	}
	
	// 解析响应
	var serviceList K8SServiceList
	if err := json.NewDecoder(resp.Body).Decode(&serviceList); err != nil {
		return nil, fmt.Errorf("解析 K8S API 响应失败: %w", err)
	}
	
	// 转换为 DiscoveredK8SService
	var result []*pb.DiscoveredK8SService
	for _, svc := range serviceList.Items {
		// 过滤系统命名空间
		if isSystemNamespace(svc.Metadata.Namespace) {
			continue
		}
		
		// 过滤没有 ClusterIP 的 Service（如 Headless Service）
		if svc.Spec.ClusterIP == "" || svc.Spec.ClusterIP == "None" {
			continue
		}
		
		// 解析端口列表
		var ports []*pb.ServicePort
		for _, p := range svc.Spec.Ports {
			ports = append(ports, &pb.ServicePort{
				Port:     p.Port,
				Protocol: p.Protocol,
				Name:     p.Name,
			})
		}
		
		if len(ports) == 0 {
			continue
		}
		
		result = append(result, &pb.DiscoveredK8SService{
			Namespace:   svc.Metadata.Namespace,
			ServiceName: svc.Metadata.Name,
			ClusterIp:   svc.Spec.ClusterIP,
			Ports:       ports,
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

// K8S API 响应结构（简化版）
type K8SServiceList struct {
	Items []K8SService `json:"items"`
}

type K8SService struct {
	Metadata K8SMetadata `json:"metadata"`
	Spec     K8SServiceSpec `json:"spec"`
}

type K8SMetadata struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type K8SServiceSpec struct {
	ClusterIP string          `json:"clusterIP"`
	Ports     []K8SServicePort `json:"ports"`
}

type K8SServicePort struct {
	Name     string `json:"name"`
	Port     int32  `json:"port"`
	Protocol string `json:"protocol"`
}

// readFile 读取文件内容（辅助函数）
func readFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}
