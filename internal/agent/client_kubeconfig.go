// Package agent 提供 Agent 端功能
// client_kubeconfig.go 自动管理 CloudIDE 的 ~/.kube/config
package agent

import (
	"fmt"
	"os"
	"os/user"
	"strings"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

const (
	kubeconfigMarkerBegin = "# >>> AWECloud Signaling Clusters >>>"
	kubeconfigMarkerEnd   = "# <<< AWECloud Signaling Clusters <<<"
)

// ClientKubeconfigManager 管理 CloudIDE 的 ~/.kube/config
type ClientKubeconfigManager struct {
	domainCache *DomainCache
	vipAlloc    *VIPAllocator
}

// NewClientKubeconfigManager 创建 KubeConfig 管理器
func NewClientKubeconfigManager(domainCache *DomainCache, vipAlloc *VIPAllocator) *ClientKubeconfigManager {
	return &ClientKubeconfigManager{
		domainCache: domainCache,
		vipAlloc:    vipAlloc,
	}
}

// Generate 生成 kubeconfig
func (m *ClientKubeconfigManager) Generate() error {
	// 获取真实用户的 home 目录
	homeDir, err := getRealUserHomeDir()
	if err != nil {
		return fmt.Errorf("获取 home 目录失败: %w", err)
	}

	kubeDir := homeDir + "/.kube"
	if err := os.MkdirAll(kubeDir, 0700); err != nil {
		return fmt.Errorf("创建 .kube 目录失败: %w", err)
	}

	kubeconfigPath := kubeDir + "/config"

	// 获取所有域名列表
	allDomains := m.domainCache.List()
	
	// 筛选 k8sapi 类型域名
	var k8sapiDomains []string
	for _, info := range allDomains {
		if info.Type == "k8sapi" {
			k8sapiDomains = append(k8sapiDomains, info.Domain)
		}
	}
	
	if len(k8sapiDomains) == 0 {
		logger.Info("[KubeConfig] 没有 k8sapi 域名，跳过生成")
		return nil
	}

	// 为每个域名生成集群条目
	var clusters []clusterEntry
	for _, domain := range k8sapiDomains {
		// 分配 VIP（幂等）
		vip, err := m.vipAlloc.Allocate(domain)
		if err != nil {
			logger.Warnf("[KubeConfig] 分配 VIP 失败 (%s): %v", domain, err)
			continue
		}

		// 提取集群名称
		clusterName := extractClusterNameFromDomain(domain)

		// K8S API 默认端口 6443
		port := 6443

		clusters = append(clusters, clusterEntry{
			Name:   clusterName,
			Domain: domain,
			VIP:    vip,
			Port:   port,
		})
	}

	if len(clusters) == 0 {
		logger.Info("[KubeConfig] 没有可用的集群，跳过生成")
		return nil
	}

	// 读取现有 kubeconfig
	existingContent, _ := os.ReadFile(kubeconfigPath)

	// 检查同名集群冲突
	if err := m.checkConflicts(string(existingContent), clusters, kubeconfigPath); err != nil {
		logger.Warnf("[KubeConfig] 检查冲突失败: %v", err)
	}

	// 构建新的 kubeconfig 内容
	newContent := m.buildKubeconfig(string(existingContent), clusters)

	// 写入 kubeconfig
	if err := os.WriteFile(kubeconfigPath, []byte(newContent), 0600); err != nil {
		return fmt.Errorf("写入 kubeconfig 失败: %w", err)
	}

	// 设置文件所有者（sudo 场景）
	if err := setFileOwner(kubeconfigPath); err != nil {
		logger.Warnf("[KubeConfig] 设置文件所有者失败: %v", err)
	}

	clusterNames := make([]string, 0, len(clusters))
	for _, c := range clusters {
		clusterNames = append(clusterNames, c.Name)
	}

	logger.Infof("[KubeConfig] 已生成: %s（%d 个集群: %s）", kubeconfigPath, len(clusters), strings.Join(clusterNames, ", "))
	return nil
}

// clusterEntry 集群条目
type clusterEntry struct {
	Name   string
	Domain string
	VIP    string
	Port   int
}

// extractClusterNameFromDomain 从域名提取集群名称
// kubernetes.neimeng.beagle → neimeng
// kubernetes.beagle-002.neimeng.beagle → beagle-002-neimeng
func extractClusterNameFromDomain(domain string) string {
	// 移除域名后缀（.beagle 或其他）
	parts := strings.Split(domain, ".")
	if len(parts) < 3 {
		return domain
	}

	// 去掉第一个 "kubernetes" 和最后一个后缀
	middle := parts[1 : len(parts)-1]
	return strings.Join(middle, "-")
}

// checkConflicts 检查同名集群冲突
func (m *ClientKubeconfigManager) checkConflicts(existing string, clusters []clusterEntry, kubeconfigPath string) error {
	if existing == "" {
		return nil
	}

	// 简单检查：查找 "name: {cluster_name}" 和 "server: https://"
	for _, c := range clusters {
		namePattern := fmt.Sprintf("name: %s", c.Name)
		serverPattern := fmt.Sprintf("server: https://%s:%d", c.VIP, c.Port)

		if strings.Contains(existing, namePattern) && !strings.Contains(existing, serverPattern) {
			// 同名但 server 不同，备份
			backupPath := kubeconfigPath + ".bak"
			if err := os.WriteFile(backupPath, []byte(existing), 0600); err != nil {
				return fmt.Errorf("备份 kubeconfig 失败: %w", err)
			}
			logger.Warnf("[KubeConfig] 检测到同名集群冲突（%s），已备份到: %s", c.Name, backupPath)
			break
		}
	}

	return nil
}

// buildKubeconfig 构建 kubeconfig YAML 内容
func (m *ClientKubeconfigManager) buildKubeconfig(existing string, clusters []clusterEntry) string {
	// 构建集群、上下文、用户条目
	var clusterYAML, contextYAML, userYAML strings.Builder

	for _, c := range clusters {
		// cluster 条目
		clusterYAML.WriteString("- cluster:\n")
		clusterYAML.WriteString(fmt.Sprintf("    server: https://%s:%d\n", c.VIP, c.Port))
		clusterYAML.WriteString("    insecure-skip-tls-verify: true\n")
		clusterYAML.WriteString(fmt.Sprintf("  name: %s\n", c.Name))

		// context 条目
		contextYAML.WriteString("- context:\n")
		contextYAML.WriteString(fmt.Sprintf("    cluster: %s\n", c.Name))
		contextYAML.WriteString("    user: signaling-user\n")
		contextYAML.WriteString(fmt.Sprintf("  name: %s\n", c.Name))
	}

	// 只需要一个 user 条目
	userYAML.WriteString("- name: signaling-user\n")
	userYAML.WriteString("  user: {}\n")

	signalingBlock := fmt.Sprintf(`%s
apiVersion: v1
kind: Config
clusters:
%scontexts:
%susers:
%s%s`,
		kubeconfigMarkerBegin,
		clusterYAML.String(),
		contextYAML.String(),
		userYAML.String(),
		kubeconfigMarkerEnd,
	)

	// 如果没有现有配置，直接返回完整 kubeconfig
	if strings.TrimSpace(existing) == "" {
		return fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: %s
clusters:
%scontexts:
%susers:
%s`, clusters[0].Name, clusterYAML.String(), contextYAML.String(), userYAML.String())
	}

	// 如果已有标记块，替换
	beginIdx := strings.Index(existing, kubeconfigMarkerBegin)
	endIdx := strings.Index(existing, kubeconfigMarkerEnd)
	if beginIdx >= 0 && endIdx >= 0 {
		endIdx += len(kubeconfigMarkerEnd)
		if endIdx < len(existing) && existing[endIdx] == '\n' {
			endIdx++
		}
		return existing[:beginIdx] + signalingBlock + "\n" + existing[endIdx:]
	}

	// 没有标记块，追加到末尾
	return existing + "\n" + signalingBlock + "\n"
}

// getRealUserHomeDir 获取真实用户的 home 目录
// sudo 运行时 os.UserHomeDir() 返回 /root，需要通过 SUDO_USER 获取原始用户的 home
func getRealUserHomeDir() (string, error) {
	// 优先检查 SUDO_USER（sudo 场景）
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		u, err := user.Lookup(sudoUser)
		if err == nil {
			return u.HomeDir, nil
		}
		// 兜底：尝试 /home/{user}
		homeDir := "/home/" + sudoUser
		if _, err := os.Stat(homeDir); err == nil {
			return homeDir, nil
		}
	}

	// 非 sudo 场景，使用标准方式
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return homeDir, nil
}

// setFileOwner 设置文件所有者为真实用户（sudo 场景）
func setFileOwner(path string) error {
	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser == "" {
		return nil // 非 sudo 场景，无需设置
	}

	u, err := user.Lookup(sudoUser)
	if err != nil {
		return err
	}

	uid := 0
	gid := 0
	fmt.Sscanf(u.Uid, "%d", &uid)
	fmt.Sscanf(u.Gid, "%d", &gid)

	return os.Chown(path, uid, gid)
}
