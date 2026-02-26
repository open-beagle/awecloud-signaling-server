package agent

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// KubeconfigManager 管理 K8S API Server 的连接配置
// 自动检测主机模式（~/.kube/config）和 Pod 模式（ServiceAccount）
type KubeconfigManager struct {
	restConfig   *rest.Config // client-go 的标准配置对象
	apiServerURL string       // API Server 地址
	mode         string       // "host" / "pod"
}

// NewKubeconfigManager 创建 Kubeconfig 管理器
// 自动检测环境并加载配置
func NewKubeconfigManager() (*KubeconfigManager, error) {
	// 1. 尝试主机模式：从 ~/.kube/config 加载
	restConfig, err := loadFromKubeconfig()
	if err == nil {
		logger.Info("Kubeconfig 管理器: 使用主机模式（~/.kube/config）")
		return &KubeconfigManager{
			restConfig:   restConfig,
			apiServerURL: restConfig.Host,
			mode:         "host",
		}, nil
	}

	logger.Debugf("主机模式加载失败: %v，尝试 Pod 模式", err)

	// 2. 尝试 Pod 模式：从 ServiceAccount 加载
	restConfig, err = rest.InClusterConfig()
	if err == nil {
		logger.Info("Kubeconfig 管理器: 使用 Pod 模式（ServiceAccount）")
		return &KubeconfigManager{
			restConfig:   restConfig,
			apiServerURL: restConfig.Host,
			mode:         "pod",
		}, nil
	}

	logger.Debugf("Pod 模式加载失败: %v", err)

	// 3. 都失败了
	return nil, fmt.Errorf("无法加载 kubeconfig：主机模式和 Pod 模式都失败")
}

// GetRESTConfig 获取 rest.Config 对象
func (m *KubeconfigManager) GetRESTConfig() *rest.Config {
	return m.restConfig
}

// GetAPIServerURL 获取 API Server 地址
func (m *KubeconfigManager) GetAPIServerURL() string {
	return m.apiServerURL
}

// GetMode 获取模式（"host" / "pod"）
func (m *KubeconfigManager) GetMode() string {
	return m.mode
}

// GetHTTPClient 创建配置好认证的 HTTP 客户端
// 使用 client-go 的 TransportFor 方法，自动处理所有认证方式
func (m *KubeconfigManager) GetHTTPClient() (*http.Client, error) {
	transport, err := rest.TransportFor(m.restConfig)
	if err != nil {
		return nil, fmt.Errorf("创建 HTTP Transport 失败: %w", err)
	}

	return &http.Client{
		Transport: transport,
	}, nil
}

// loadFromKubeconfig 从 ~/.kube/config 加载配置
func loadFromKubeconfig() (*rest.Config, error) {
	// 1. 确定 kubeconfig 路径
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("获取用户主目录失败: %w", err)
		}
		kubeconfigPath = filepath.Join(homeDir, ".kube", "config")
	}

	// 2. 检查文件是否存在
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("kubeconfig 文件不存在: %s", kubeconfigPath)
	}

	// 3. 使用 client-go 加载配置
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("加载 kubeconfig 失败: %w", err)
	}

	logger.Infof("成功加载 kubeconfig: %s, API Server: %s", kubeconfigPath, config.Host)
	return config, nil
}

// GetStatusForHeartbeat 获取状态信息用于心跳上报
func (m *KubeconfigManager) GetStatusForHeartbeat() (enabled bool, mode, apiServer string, authConfigured bool, errorMsg string) {
	if m == nil {
		return false, "", "", false, "KubeconfigManager 未初始化"
	}

	// 检查是否配置了认证
	authConfigured = false
	if m.restConfig != nil {
		// 检查是否有任何认证配置
		if m.restConfig.BearerToken != "" ||
			m.restConfig.BearerTokenFile != "" ||
			m.restConfig.Username != "" ||
			len(m.restConfig.TLSClientConfig.CertData) > 0 ||
			m.restConfig.TLSClientConfig.CertFile != "" ||
			m.restConfig.ExecProvider != nil {
			authConfigured = true
		}
	}

	return true, m.mode, m.apiServerURL, authConfigured, ""
}

// ValidateConnection 验证到 API Server 的连接（可选，用于启动时检查）
func (m *KubeconfigManager) ValidateConnection() error {
	client, err := m.GetHTTPClient()
	if err != nil {
		return fmt.Errorf("创建 HTTP 客户端失败: %w", err)
	}

	// 尝试访问 /version 端点（不需要认证）
	resp, err := client.Get(m.apiServerURL + "/version")
	if err != nil {
		return fmt.Errorf("连接 API Server 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("API Server 返回错误: %d", resp.StatusCode)
	}

	logger.Infof("API Server 连接验证成功: %s", m.apiServerURL)
	return nil
}

// GetAuthInfo 获取认证信息摘要（用于日志）
func (m *KubeconfigManager) GetAuthInfo() string {
	if m.restConfig == nil {
		return "无认证配置"
	}

	var authMethods []string

	if m.restConfig.BearerToken != "" {
		authMethods = append(authMethods, "Bearer Token")
	}
	if m.restConfig.BearerTokenFile != "" {
		authMethods = append(authMethods, "Bearer Token File")
	}
	if m.restConfig.Username != "" {
		authMethods = append(authMethods, "Basic Auth")
	}
	if len(m.restConfig.TLSClientConfig.CertData) > 0 || m.restConfig.TLSClientConfig.CertFile != "" {
		authMethods = append(authMethods, "Client Certificate")
	}
	if m.restConfig.ExecProvider != nil {
		authMethods = append(authMethods, "Exec Provider")
	}

	if len(authMethods) == 0 {
		return "无认证配置"
	}

	return strings.Join(authMethods, ", ")
}
