package cache

import (
	"context"
	"errors"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var ErrNodeNotFound = errors.New("node not found in runtime store")

// UserTypeNodeKey 用户名、节点类型与节点名组合次索引键
type UserTypeNodeKey struct {
	UserID   uint64
	NodeType model.NodeType
	NodeName string
}

// RuntimeNode 节点在内存中的轻量级运行态结构
type RuntimeNode struct {
	ID                   uint64         `json:"id"`
	UserID               uint64         `json:"user_id"`
	Name                 string         `json:"name"`
	Type                 model.NodeType `json:"type"`
	HeadscaleNodeID      uint64         `json:"headscale_node_id"`
	IP                   string         `json:"ip"`
	Version              string         `json:"version"`
	UpdaterProtocol      string         `json:"updater_protocol"`
	ContainerSSHProtocol string         `json:"container_ssh_protocol"`
	Hostname             string         `json:"hostname"`
	HostDomainLabel      string         `json:"host_domain_label"`
	SystemInfo           string         `json:"system_info"`
	LastHeartbeat        time.Time      `json:"last_heartbeat"`
	Revision             uint64         `json:"revision"`
	Dirty                bool           `json:"dirty"`

	// Agent / Endpoint 配置与能力
	K8SEnabled         *bool  `json:"k8s_enabled,omitempty"`
	K8SListenPort      *int   `json:"k8s_listen_port,omitempty"`
	K8SApiServer       string `json:"k8s_api_server,omitempty"`
	SVCEnabled         *bool  `json:"svc_enabled,omitempty"`
	SVCLabelSelector   string `json:"svc_label_selector,omitempty"`
	SVCNamespaces      string `json:"svc_namespaces,omitempty"`
	SVCListenPortBase  *int   `json:"svc_listen_port_base,omitempty"`
	EndpointEnabled    *bool  `json:"endpoint_enabled,omitempty"`
	EndpointListenPort *int   `json:"endpoint_listen_port,omitempty"`
}

// RuntimeNodeDirtySnapshot 脏节点快照，包含落库所需字段与 snapshot 时的 Revision
type RuntimeNodeDirtySnapshot struct {
	NodeID          uint64
	UserID          uint64
	Name            string
	Type            model.NodeType
	HeadscaleNodeID uint64
	IP              string
	Version         string
	Hostname        string
	SystemInfo      string
	LastHeartbeat   time.Time
	Revision        uint64
}

// NodeRuntimeStore 线程安全的节点运行态内存存储
type NodeRuntimeStore struct {
	mu           sync.RWMutex
	nodes        map[uint64]*RuntimeNode
	userTypeIndex map[UserTypeNodeKey]uint64
}

// NewNodeRuntimeStore 创建空的 NodeRuntimeStore
func NewNodeRuntimeStore() *NodeRuntimeStore {
	return &NodeRuntimeStore{
		nodes:         make(map[uint64]*RuntimeNode),
		userTypeIndex: make(map[UserTypeNodeKey]uint64),
	}
}

// LoadFromDB 从数据库全量加载节点信息建立初始索引
func (s *NodeRuntimeStore) LoadFromDB(ctx context.Context, db *gorm.DB) error {
	var dbNodes []model.Node
	if err := db.WithContext(ctx).Find(&dbNodes).Error; err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.nodes = make(map[uint64]*RuntimeNode, len(dbNodes))
	s.userTypeIndex = make(map[UserTypeNodeKey]uint64, len(dbNodes))

	for i := range dbNodes {
		node := &dbNodes[i]
		rn := s.modelToRuntimeNode(node)
		s.nodes[rn.ID] = rn
		s.userTypeIndex[UserTypeNodeKey{
			UserID:   rn.UserID,
			NodeType: rn.Type,
			NodeName: rn.Name,
		}] = rn.ID
	}

	return nil
}

func (s *NodeRuntimeStore) modelToRuntimeNode(node *model.Node) *RuntimeNode {
	var lastHb time.Time
	if node.LastHeartbeat != nil {
		lastHb = *node.LastHeartbeat
	}
	return &RuntimeNode{
		ID:                   node.ID,
		UserID:               node.UserID,
		Name:                 node.Name,
		Type:                 node.Type,
		HeadscaleNodeID:      node.HeadscaleNodeID,
		IP:                   node.IP,
		Version:              node.Version,
		UpdaterProtocol:      node.UpdaterProtocol,
		ContainerSSHProtocol: node.ContainerSSHProtocol,
		Hostname:             node.Hostname,
		HostDomainLabel:      node.HostDomainLabel,
		SystemInfo:           node.SystemInfo,
		LastHeartbeat:        lastHb,
		Revision:             1,
		Dirty:                false,
		K8SEnabled:           node.K8SEnabled,
		K8SListenPort:        node.K8SListenPort,
		K8SApiServer:         node.K8SApiServer,
		SVCEnabled:           node.SVCEnabled,
		SVCLabelSelector:     node.SVCLabelSelector,
		SVCNamespaces:        node.SVCNamespaces,
		SVCListenPortBase:    node.SVCListenPortBase,
		EndpointEnabled:      node.EndpointEnabled,
		EndpointListenPort:   node.EndpointListenPort,
	}
}

// GetNode 按 NodeID 查询运行态副本
func (s *NodeRuntimeStore) GetNode(nodeID uint64) (RuntimeNode, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rn, ok := s.nodes[nodeID]
	if !ok || rn == nil {
		return RuntimeNode{}, false
	}
	return *rn, true
}

// GetNodeByUserAndName 按 UserID、NodeType 和 Name 查询运行态副本
func (s *NodeRuntimeStore) GetNodeByUserAndName(userID uint64, nodeType model.NodeType, name string) (RuntimeNode, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nodeID, ok := s.userTypeIndex[UserTypeNodeKey{UserID: userID, NodeType: nodeType, NodeName: name}]
	if !ok {
		return RuntimeNode{}, false
	}
	rn, ok := s.nodes[nodeID]
	if !ok || rn == nil {
		return RuntimeNode{}, false
	}
	return *rn, true
}

// ListNodes 返回所有节点的运行态只读快照列表
func (s *NodeRuntimeStore) ListNodes() []RuntimeNode {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]RuntimeNode, 0, len(s.nodes))
	for _, rn := range s.nodes {
		if rn != nil {
			result = append(result, *rn)
		}
	}
	return result
}

// ListNodesByUser 返回属于指定 UserID 的所有节点运行态
func (s *NodeRuntimeStore) ListNodesByUser(userID uint64) []RuntimeNode {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []RuntimeNode
	for _, rn := range s.nodes {
		if rn != nil && rn.UserID == userID {
			result = append(result, *rn)
		}
	}
	return result
}

// UpdateHeartbeat 心跳热路径更新接口：只改内存，增加 Revision，标记 Dirty
func (s *NodeRuntimeStore) UpdateHeartbeat(nodeID uint64, ip, hostname, version, systemInfo, updaterProto, sshProto string, now time.Time) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rn, ok := s.nodes[nodeID]
	if !ok || rn == nil {
		return 0, ErrNodeNotFound
	}

	if ip != "" {
		rn.IP = ip
	}
	if hostname != "" {
		rn.Hostname = hostname
	}
	if version != "" {
		rn.Version = version
	}
	if systemInfo != "" {
		rn.SystemInfo = systemInfo
	}
	if updaterProto != "" {
		rn.UpdaterProtocol = updaterProto
	}
	if sshProto != "" {
		rn.ContainerSSHProtocol = sshProto
	}

	rn.LastHeartbeat = now
	rn.Revision++
	rn.Dirty = true

	return rn.Revision, nil
}

// UpdateHeadscaleNodeID 绑定 Headscale Node ID 接口
func (s *NodeRuntimeStore) UpdateHeadscaleNodeID(nodeID uint64, headscaleNodeID uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	rn, ok := s.nodes[nodeID]
	if !ok || rn == nil {
		return false
	}
	if rn.HeadscaleNodeID == headscaleNodeID {
		return false
	}

	rn.HeadscaleNodeID = headscaleNodeID
	rn.Revision++
	rn.Dirty = true
	return true
}

// UpsertNode 节点创建/修改管理接口调用：事务成功后同步更新内存 Store
func (s *NodeRuntimeStore) UpsertNode(node *model.Node) {
	if node == nil || node.ID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	rn := s.modelToRuntimeNode(node)
	existing, ok := s.nodes[node.ID]
	if ok && existing != nil {
		rn.Revision = existing.Revision + 1
		rn.Dirty = existing.Dirty
		if rn.LastHeartbeat.IsZero() {
			rn.LastHeartbeat = existing.LastHeartbeat
		}
	}

	s.nodes[rn.ID] = rn
	s.userTypeIndex[UserTypeNodeKey{
		UserID:   rn.UserID,
		NodeType: rn.Type,
		NodeName: rn.Name,
	}] = rn.ID
}

// DeleteNode 节点删除管理接口调用：事务成功后同步从内存删除
func (s *NodeRuntimeStore) DeleteNode(nodeID uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rn, ok := s.nodes[nodeID]
	if !ok || rn == nil {
		return
	}
	delete(s.userTypeIndex, UserTypeNodeKey{
		UserID:   rn.UserID,
		NodeType: rn.Type,
		NodeName: rn.Name,
	})
	delete(s.nodes, nodeID)
}

// SnapshotDirty 提取当前所有脏节点的不可变快照及关联 Revision
func (s *NodeRuntimeStore) SnapshotDirty() map[uint64]RuntimeNodeDirtySnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[uint64]RuntimeNodeDirtySnapshot)
	for id, rn := range s.nodes {
		if rn != nil && rn.Dirty {
			result[id] = RuntimeNodeDirtySnapshot{
				NodeID:          rn.ID,
				UserID:          rn.UserID,
				Name:            rn.Name,
				Type:            rn.Type,
				HeadscaleNodeID: rn.HeadscaleNodeID,
				IP:              rn.IP,
				Version:         rn.Version,
				Hostname:        rn.Hostname,
				SystemInfo:      rn.SystemInfo,
				LastHeartbeat:   rn.LastHeartbeat,
				Revision:        rn.Revision,
			}
		}
	}
	return result
}

// ClearDirty 如果节点在落库期间没有收到新心跳（Revision 未变），清除 Dirty 标记
func (s *NodeRuntimeStore) ClearDirty(clearedRevisions map[uint64]uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for nodeID, rev := range clearedRevisions {
		rn, ok := s.nodes[nodeID]
		if ok && rn != nil && rn.Revision == rev {
			rn.Dirty = false
		}
	}
}
