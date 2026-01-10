package service

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// NetworkConfig 网段配置服务
type NetworkConfig struct {
	db                *gorm.DB
	networkConfigPath string // network.toml 文件路径
}

// NewNetworkConfig 创建网段配置服务
func NewNetworkConfig(db *gorm.DB, networkConfigPath string) *NetworkConfig {
	return &NetworkConfig{
		db:                db,
		networkConfigPath: networkConfigPath,
	}
}

// NetworkSegment 网段配置
type NetworkSegment struct {
	CIDR        string `json:"cidr" toml:"cidr"`
	IPStart     string `json:"ip_start" toml:"ip_start"`
	IPEnd       string `json:"ip_end" toml:"ip_end"`
	Description string `json:"description" toml:"description"`
}

// NetworkPlan 完整网段规划
type NetworkPlan struct {
	Agent   NetworkSegment `json:"agent" toml:"agent"`
	Desktop NetworkSegment `json:"desktop" toml:"desktop"`
	Server  NetworkSegment `json:"server" toml:"server"`
}

// NetworkConfigFile network.toml 文件结构
type NetworkConfigFile struct {
	Agent   NetworkSegment `toml:"agent"`
	Desktop NetworkSegment `toml:"desktop"`
	Server  NetworkSegment `toml:"server"`
}

// GetNetworkPlan 获取网段规划（优先数据库，回退到配置文件）
func (nc *NetworkConfig) GetNetworkPlan() (*NetworkPlan, error) {
	// 1. 尝试从数据库加载
	var sysConfig model.SystemConfig
	err := nc.db.First(&sysConfig).Error
	if err == nil {
		// 数据库有配置，使用数据库值
		return &NetworkPlan{
			Agent: NetworkSegment{
				CIDR:    sysConfig.AgentCIDR,
				IPStart: sysConfig.AgentIPStart,
				IPEnd:   sysConfig.AgentIPEnd,
			},
			Desktop: NetworkSegment{
				CIDR:    sysConfig.DesktopCIDR,
				IPStart: sysConfig.DesktopIPStart,
				IPEnd:   sysConfig.DesktopIPEnd,
			},
			Server: NetworkSegment{
				CIDR:    sysConfig.ServerCIDR,
				IPStart: sysConfig.ServerIPStart,
				IPEnd:   sysConfig.ServerIPEnd,
			},
		}, nil
	}

	// 2. 数据库没有配置，从 network.toml 文件读取
	if err == gorm.ErrRecordNotFound {
		return nc.loadFromFile()
	}

	return nil, fmt.Errorf("failed to load network config: %w", err)
}

// loadFromFile 从 network.toml 文件加载配置
func (nc *NetworkConfig) loadFromFile() (*NetworkPlan, error) {
	// 如果没有指定配置文件路径，使用默认值
	if nc.networkConfigPath == "" {
		return nc.getHardcodedDefaults(), nil
	}

	// 检查文件是否存在
	if _, err := os.Stat(nc.networkConfigPath); os.IsNotExist(err) {
		return nc.getHardcodedDefaults(), nil
	}

	// 读取 TOML 文件
	var configFile NetworkConfigFile
	if _, err := toml.DecodeFile(nc.networkConfigPath, &configFile); err != nil {
		return nil, fmt.Errorf("failed to parse network config file: %w", err)
	}

	return &NetworkPlan{
		Agent:   configFile.Agent,
		Desktop: configFile.Desktop,
		Server:  configFile.Server,
	}, nil
}

// getHardcodedDefaults 获取硬编码的默认值（最后的回退）
func (nc *NetworkConfig) getHardcodedDefaults() *NetworkPlan {
	return &NetworkPlan{
		Agent: NetworkSegment{
			CIDR:        "100.64.0.0/16",
			IPStart:     "100.64.0.1",
			IPEnd:       "100.64.255.254",
			Description: "Agent 网段，用于服务端节点",
		},
		Desktop: NetworkSegment{
			CIDR:        "100.65.0.0/16",
			IPStart:     "100.65.0.1",
			IPEnd:       "100.65.255.254",
			Description: "Desktop 网段，用于客户端节点",
		},
		Server: NetworkSegment{
			CIDR:        "100.66.0.0/16",
			IPStart:     "100.66.0.1",
			IPEnd:       "100.66.255.254",
			Description: "Server 网段，用于管理节点",
		},
	}
}

// InitializeDefaultConfig 初始化默认配置到数据库（启动时调用）
func (nc *NetworkConfig) InitializeDefaultConfig() error {
	// 检查数据库是否已有配置
	var count int64
	if err := nc.db.Model(&model.SystemConfig{}).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to check system config: %w", err)
	}

	// 已有配置，跳过初始化
	if count > 0 {
		return nil
	}

	// 从配置文件读取默认值
	defaultPlan, err := nc.loadFromFile()
	if err != nil {
		return fmt.Errorf("failed to load default network plan: %w", err)
	}

	// 写入数据库
	sysConfig := &model.SystemConfig{
		AgentCIDR:      defaultPlan.Agent.CIDR,
		AgentIPStart:   defaultPlan.Agent.IPStart,
		AgentIPEnd:     defaultPlan.Agent.IPEnd,
		DesktopCIDR:    defaultPlan.Desktop.CIDR,
		DesktopIPStart: defaultPlan.Desktop.IPStart,
		DesktopIPEnd:   defaultPlan.Desktop.IPEnd,
		ServerCIDR:     defaultPlan.Server.CIDR,
		ServerIPStart:  defaultPlan.Server.IPStart,
		ServerIPEnd:    defaultPlan.Server.IPEnd,
	}

	if err := nc.db.Create(sysConfig).Error; err != nil {
		return fmt.Errorf("failed to initialize default config: %w", err)
	}

	return nil
}

// UpdateNetworkPlan 更新网段规划（Web 界面调用）
func (nc *NetworkConfig) UpdateNetworkPlan(plan *NetworkPlan) error {
	// 验证网段配置
	if err := nc.validateNetworkPlan(plan); err != nil {
		return fmt.Errorf("invalid network plan: %w", err)
	}

	// 更新数据库
	var sysConfig model.SystemConfig
	if err := nc.db.First(&sysConfig).Error; err != nil {
		return fmt.Errorf("failed to load system config: %w", err)
	}

	sysConfig.AgentCIDR = plan.Agent.CIDR
	sysConfig.AgentIPStart = plan.Agent.IPStart
	sysConfig.AgentIPEnd = plan.Agent.IPEnd
	sysConfig.DesktopCIDR = plan.Desktop.CIDR
	sysConfig.DesktopIPStart = plan.Desktop.IPStart
	sysConfig.DesktopIPEnd = plan.Desktop.IPEnd
	sysConfig.ServerCIDR = plan.Server.CIDR
	sysConfig.ServerIPStart = plan.Server.IPStart
	sysConfig.ServerIPEnd = plan.Server.IPEnd

	if err := nc.db.Save(&sysConfig).Error; err != nil {
		return fmt.Errorf("failed to update network config: %w", err)
	}

	return nil
}

// validateNetworkPlan 验证网段配置合法性
func (nc *NetworkConfig) validateNetworkPlan(plan *NetworkPlan) error {
	// 验证 CIDR 格式
	segments := []struct {
		name string
		seg  NetworkSegment
	}{
		{"agent", plan.Agent},
		{"desktop", plan.Desktop},
		{"server", plan.Server},
	}

	for _, s := range segments {
		// 验证 CIDR
		_, ipNet, err := net.ParseCIDR(s.seg.CIDR)
		if err != nil {
			return fmt.Errorf("%s CIDR invalid: %w", s.name, err)
		}

		// 验证起始 IP
		startIP := net.ParseIP(s.seg.IPStart)
		if startIP == nil {
			return fmt.Errorf("%s start IP invalid", s.name)
		}
		if !ipNet.Contains(startIP) {
			return fmt.Errorf("%s start IP not in CIDR range", s.name)
		}

		// 验证结束 IP
		endIP := net.ParseIP(s.seg.IPEnd)
		if endIP == nil {
			return fmt.Errorf("%s end IP invalid", s.name)
		}
		if !ipNet.Contains(endIP) {
			return fmt.Errorf("%s end IP not in CIDR range", s.name)
		}
	}

	return nil
}

// GetNetworkConfigPath 获取 network.toml 文件路径
// 优先级: 1. 指定路径 2. 当前目录 3. 可执行文件目录
func GetNetworkConfigPath(specifiedPath string) string {
	if specifiedPath != "" {
		return specifiedPath
	}

	// 尝试当前目录
	if _, err := os.Stat("config/network.toml"); err == nil {
		return "config/network.toml"
	}

	// 尝试可执行文件目录
	if exePath, err := os.Executable(); err == nil {
		configPath := filepath.Join(filepath.Dir(exePath), "config", "network.toml")
		if _, err := os.Stat(configPath); err == nil {
			return configPath
		}
	}

	// 返回空，使用硬编码默认值
	return ""
}
