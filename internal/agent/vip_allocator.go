package agent

import (
	"fmt"
	"sync"
)

// VIPAllocator VIP 地址分配器
// 使用 127.1.0.0/16 地址段，为每个域名分配唯一的本地地址
type VIPAllocator struct {
	// 域名 → VIP 映射
	domainToVIP map[string]string
	// VIP → 域名 反向映射
	vipToDomain map[string]string

	// 下一个可分配的地址序号（从 1 开始）
	nextIndex int

	mu sync.RWMutex
}

// NewVIPAllocator 创建 VIP 分配器
func NewVIPAllocator() *VIPAllocator {
	return &VIPAllocator{
		domainToVIP: make(map[string]string),
		vipToDomain: make(map[string]string),
		nextIndex:   1,
	}
}

// Allocate 为域名分配 VIP 地址
// 如果域名已分配，返回已有的 VIP；否则分配新的
func (a *VIPAllocator) Allocate(domain string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 已分配，直接返回
	if vip, ok := a.domainToVIP[domain]; ok {
		return vip, nil
	}

	// 分配新 VIP
	if a.nextIndex > 65534 {
		return "", fmt.Errorf("VIP 地址耗尽（最多 65534 个）")
	}

	// 127.1.x.x：高字节 = index/256, 低字节 = index%256
	// 跳过 .0 和 .255 避免广播地址问题
	hi := (a.nextIndex-1)/254 + 0 // 0-255
	lo := (a.nextIndex-1)%254 + 1 // 1-254

	vip := fmt.Sprintf("127.1.%d.%d", hi, lo)
	a.nextIndex++

	a.domainToVIP[domain] = vip
	a.vipToDomain[vip] = domain

	return vip, nil
}

// Resolve 根据 VIP 查找域名
func (a *VIPAllocator) Resolve(vip string) (string, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	domain, ok := a.vipToDomain[vip]
	return domain, ok
}

// GetVIP 根据域名查找 VIP（不分配）
func (a *VIPAllocator) GetVIP(domain string) (string, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	vip, ok := a.domainToVIP[domain]
	return vip, ok
}

// GetAll 获取所有映射（用于调试/展示）
func (a *VIPAllocator) GetAll() map[string]string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make(map[string]string, len(a.domainToVIP))
	for k, v := range a.domainToVIP {
		result[k] = v
	}
	return result
}

// Count 获取已分配的 VIP 数量
func (a *VIPAllocator) Count() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.domainToVIP)
}
