package agent

import (
	"sync"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

// DomainCache 域名缓存
// 存储从 Server 获取的可访问域名列表
type DomainCache struct {
	// 域名 → 域名信息映射
	domains map[string]*pb.DomainInfo
	mu      sync.RWMutex
}

// NewDomainCache 创建域名缓存
func NewDomainCache() *DomainCache {
	return &DomainCache{
		domains: make(map[string]*pb.DomainInfo),
	}
}

// Get 根据域名获取信息
func (c *DomainCache) Get(domain string) (*pb.DomainInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	info, ok := c.domains[domain]
	return info, ok
}

// Update 更新域名列表（全量替换）
func (c *DomainCache) Update(domains []*pb.DomainInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 清空旧数据
	c.domains = make(map[string]*pb.DomainInfo, len(domains))

	// 写入新数据
	for _, d := range domains {
		c.domains[d.Domain] = d
	}
}

// List 获取所有域名列表
func (c *DomainCache) List() []*pb.DomainInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]*pb.DomainInfo, 0, len(c.domains))
	for _, d := range c.domains {
		result = append(result, d)
	}
	return result
}

// Count 获取域名数量
func (c *DomainCache) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.domains)
}
