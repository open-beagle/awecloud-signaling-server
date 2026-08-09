package headscale

import (
	"time"

	v1 "github.com/juanfont/headscale/gen/go/headscale/v1"
)

// UserNodeKey 用户名与节点名组合键
type UserNodeKey struct {
	UserName string
	NodeName string
}

// HeadscaleNodeView 单个 Headscale 节点的不可变轻量视图
type HeadscaleNodeView struct {
	ID          uint64
	User        string
	GivenName   string
	IPAddresses []string
	Online      bool
	ForcedTags  []string
	LastSeen    time.Time
}

// HeadscaleNodeSnapshot 全量 Headscale 节点内存只读快照
type HeadscaleNodeSnapshot struct {
	Version     uint64
	RefreshedAt time.Time
	ByID        map[uint64]HeadscaleNodeView
	ByIP        map[string]HeadscaleNodeView
	ByUserName  map[UserNodeKey]HeadscaleNodeView
	ByUser      map[string][]HeadscaleNodeView
}

// NewHeadscaleNodeSnapshot 从 v1.Node 列表一次性构造不可变节点快照
func NewHeadscaleNodeSnapshot(version uint64, refreshedAt time.Time, rawNodes []*v1.Node) *HeadscaleNodeSnapshot {
	byID := make(map[uint64]HeadscaleNodeView, len(rawNodes))
	byIP := make(map[string]HeadscaleNodeView, len(rawNodes)*2)
	byUserName := make(map[UserNodeKey]HeadscaleNodeView, len(rawNodes))
	byUser := make(map[string][]HeadscaleNodeView, len(rawNodes))

	for _, node := range rawNodes {
		if node == nil {
			continue
		}
		userName := ""
		if node.User != nil {
			userName = node.User.Name
		}
		var lastSeen time.Time
		if node.LastSeen != nil {
			lastSeen = node.LastSeen.AsTime()
		}

		ipAddresses := make([]string, len(node.IpAddresses))
		copy(ipAddresses, node.IpAddresses)

		forcedTags := make([]string, len(node.ForcedTags))
		copy(forcedTags, node.ForcedTags)

		view := HeadscaleNodeView{
			ID:          node.Id,
			User:        userName,
			GivenName:   node.GivenName,
			IPAddresses: ipAddresses,
			Online:      node.Online,
			ForcedTags:  forcedTags,
			LastSeen:    lastSeen,
		}

		byID[view.ID] = view

		for _, ip := range view.IPAddresses {
			if ip != "" {
				byIP[ip] = view
			}
		}

		if userName != "" {
			byUser[userName] = append(byUser[userName], view)
			if view.GivenName != "" {
				key := UserNodeKey{UserName: userName, NodeName: view.GivenName}
				existing, exists := byUserName[key]
				if !exists || isPreferredNodeView(view, existing) {
					byUserName[key] = view
				}
			}
		}
	}

	return &HeadscaleNodeSnapshot{
		Version:     version,
		RefreshedAt: refreshedAt,
		ByID:        byID,
		ByIP:        byIP,
		ByUserName:  byUserName,
		ByUser:      byUser,
	}
}

// isPreferredNodeView 当同 User+GivenName 出现多个 Headscale 记录时，判断 candidate 是否优于 current
func isPreferredNodeView(candidate, current HeadscaleNodeView) bool {
	if candidate.Online && !current.Online {
		return true
	}
	if candidate.Online == current.Online && candidate.ID > current.ID {
		return true
	}
	return false
}

// GetByID 按 Node ID 查询视图
func (s *HeadscaleNodeSnapshot) GetByID(id uint64) (HeadscaleNodeView, bool) {
	if s == nil || s.ByID == nil {
		return HeadscaleNodeView{}, false
	}
	v, ok := s.ByID[id]
	return v, ok
}

// GetByIP 按 IP 地址查询视图
func (s *HeadscaleNodeSnapshot) GetByIP(ip string) (HeadscaleNodeView, bool) {
	if s == nil || s.ByIP == nil {
		return HeadscaleNodeView{}, false
	}
	v, ok := s.ByIP[ip]
	return v, ok
}

// GetByUserNameAndNodeName 按 UserName 和 GivenName 查询最佳视图
func (s *HeadscaleNodeSnapshot) GetByUserNameAndNodeName(userName, nodeName string) (HeadscaleNodeView, bool) {
	if s == nil || s.ByUserName == nil {
		return HeadscaleNodeView{}, false
	}
	v, ok := s.ByUserName[UserNodeKey{UserName: userName, NodeName: nodeName}]
	return v, ok
}

// GetByUser 获取指定 User 名下的所有视图
func (s *HeadscaleNodeSnapshot) GetByUser(userName string) []HeadscaleNodeView {
	if s == nil || s.ByUser == nil {
		return nil
	}
	views := s.ByUser[userName]
	result := make([]HeadscaleNodeView, len(views))
	copy(result, views)
	return result
}

// Age 计算快照当前年龄
func (s *HeadscaleNodeSnapshot) Age(now time.Time) time.Duration {
	if s == nil || s.RefreshedAt.IsZero() {
		return time.Hour * 24
	}
	return now.Sub(s.RefreshedAt)
}

// IsFresh 判断快照年龄是否 <= 30s
func (s *HeadscaleNodeSnapshot) IsFresh(now time.Time) bool {
	return s.Age(now) <= 30*time.Second
}

// IsDegraded 判断快照年龄是否在 30s - 60s
func (s *HeadscaleNodeSnapshot) IsDegraded(now time.Time) bool {
	age := s.Age(now)
	return age > 30*time.Second && age <= 60*time.Second
}

// IsExpired 判断快照年龄是否 > 60s
func (s *HeadscaleNodeSnapshot) IsExpired(now time.Time) bool {
	return s.Age(now) > 60*time.Second
}
