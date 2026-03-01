// Package agent 提供 Agent 端功能
// k8s_kubeconfig_manager.go 管理 K8S API 代理的 kubeconfig 配置
package agent

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// KubeconfigManager K8S API 代理的 Kubeconfig 管理器
// 从本地 ~/.kube/config 读取配置，用于连接本机 K8S API Server
type KubeconfigManager struct {
	restConfig   *rest.Config
	apiServerURL string
	mode         string // in-cluster 或 kubeconfig
	authInfo     string // 认证方式描述
}

// NewKubeconfigManager 创建 Kubeconfig 管理器
// 优先使用 in-cluster 配置，否则使用 ~/.kube/config
func NewKubeconfigManager() (*KubeconfigManager, error) {
	// 1. 尝试 in-cluster 配置（Pod 内运行）
	if restConfig, err := rest.InClusterConfig(); err == nil {
		logger.Info("[K8SAPIProxy] 使用 in-cluster 配置")
		return &KubeconfigManager{
			restConfig:   restConfig,
			apiServerURL: restConfig.Host,
			mode:         "in-cluster",
			authInfo:     "ServiceAccount",
		}, nil
	}

	// 2. 使用 ~/.kube/config
	kubeconfigPath := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	if kubeconfigEnv := os.Getenv("KUBECONFIG"); kubeconfigEnv != "" {
		kubeconfigPath = kubeconfigEnv
	}

	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("加载 kubeconfig 失败: %w", err)
	}

	logger.Infof("[K8SAPIProxy] 使用 kubeconfig: %s", kubeconfigPath)
	return &KubeconfigManager{
		restConfig:   restConfig,
		apiServerURL: restConfig.Host,
		mode:         "kubeconfig",
		authInfo:     fmt.Sprintf("kubeconfig(%s)", kubeconfigPath),
	}, nil
}

// GetHTTPClient 创建配置好认证的 HTTP 客户端
func (m *KubeconfigManager) GetHTTPClient() (*http.Client, error) {
	// 使用 rest.TransportFor 创建完整的 Transport（包含证书加载）
	transport, err := rest.TransportFor(m.restConfig)
	if err != nil {
		return nil, fmt.Errorf("创建 Transport 失败: %w", err)
	}

	return &http.Client{
		Transport: transport,
		Timeout:   0, // 不设置超时，由 K8S API Server 控制
	}, nil
}

// GetAPIServerURL 获取 API Server URL
func (m *KubeconfigManager) GetAPIServerURL() string {
	return m.apiServerURL
}

// GetRESTConfig 获取 rest.Config
func (m *KubeconfigManager) GetRESTConfig() *rest.Config {
	return m.restConfig
}

// GetMode 获取配置模式
func (m *KubeconfigManager) GetMode() string {
	return m.mode
}

// GetAuthInfo 获取认证信息描述
func (m *KubeconfigManager) GetAuthInfo() string {
	return m.authInfo
}
