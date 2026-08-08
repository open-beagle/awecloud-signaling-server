package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

const (
	sessionAuthorizationProtocolV2 = "resource_session_v2"
	sessionAuthorizationClockSkew  = 5 * time.Minute
	maxSessionSnapshotItems        = 10000
)

var (
	ErrSessionSnapshotInvalid          = errors.New("invalid resource session authorization snapshot")
	ErrSessionSnapshotRevisionRollback = errors.New("resource session authorization snapshot revision rolled back")
	ErrSessionSnapshotRevisionConflict = errors.New("resource session authorization snapshot revision hash conflicts")
	ErrSessionSnapshotHashMismatch     = errors.New("resource session authorization snapshot payload hash mismatch")
	ErrSessionSnapshotExpired          = errors.New("resource session authorization snapshot expired")
	ErrSessionSnapshotRouteConflict    = errors.New("resource session authorization snapshot route conflicts")
)

// SessionAuthorizationCache is the v2 fail-closed authorization store. A new
// snapshot is fully validated in temporary maps before the live state changes.
type SessionAuthorizationCache struct {
	mu          sync.RWMutex
	revision    int64
	payloadHash string
	ackRevision int64
	ackHash     string
	issuedAt    time.Time
	validUntil  time.Time
	enforceV2   bool
	permissions map[string]*pb.ResourceSessionPermissionV2
	sshRoutes   map[uint16][]string
	commands    []*pb.ResourceSessionTerminationCommandV2
}

func NewSessionAuthorizationCache() *SessionAuthorizationCache {
	return &SessionAuthorizationCache{
		permissions: make(map[string]*pb.ResourceSessionPermissionV2),
		sshRoutes:   make(map[uint16][]string),
	}
}

// Apply verifies and atomically replaces a complete Server snapshot. Replayed
// snapshots are accepted only when the revision and payload hash both match.
func (c *SessionAuthorizationCache) Apply(snapshot *pb.ResourceSessionAuthorizationSnapshotV2, now time.Time) error {
	if c == nil || snapshot == nil {
		return ErrSessionSnapshotInvalid
	}
	now = now.UTC()
	hash, err := validateSessionSnapshotEnvelope(snapshot, now)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.revision > 0 {
		if snapshot.Revision < c.revision {
			return ErrSessionSnapshotRevisionRollback
		}
		if snapshot.Revision == c.revision {
			if hash != c.payloadHash {
				return ErrSessionSnapshotRevisionConflict
			}
			return nil
		}
	}

	permissions, routes, commands, err := buildSessionSnapshotState(snapshot, now)
	if err != nil {
		return err
	}
	c.revision = snapshot.Revision
	c.payloadHash = hash
	c.issuedAt = snapshot.IssuedAt.AsTime().UTC()
	c.validUntil = snapshot.ValidUntil.AsTime().UTC()
	c.enforceV2 = snapshot.EnforceV2
	c.permissions = permissions
	c.sshRoutes = routes
	c.commands = commands
	return nil
}

func validateSessionSnapshotEnvelope(snapshot *pb.ResourceSessionAuthorizationSnapshotV2, now time.Time) (string, error) {
	if snapshot.Revision <= 0 || !snapshot.ReplaceAll || snapshot.IssuedAt == nil || !snapshot.IssuedAt.IsValid() ||
		snapshot.ValidUntil == nil || !snapshot.ValidUntil.IsValid() || len(snapshot.Permissions) > maxSessionSnapshotItems ||
		len(snapshot.TerminationCommands) > maxSessionSnapshotItems {
		return "", ErrSessionSnapshotInvalid
	}
	issuedAt := snapshot.IssuedAt.AsTime().UTC()
	validUntil := snapshot.ValidUntil.AsTime().UTC()
	if !snapshot.EnforceV2 && len(snapshot.Permissions) != 0 {
		return "", ErrSessionSnapshotInvalid
	}
	if issuedAt.After(now.Add(sessionAuthorizationClockSkew)) || !validUntil.After(issuedAt) {
		return "", ErrSessionSnapshotInvalid
	}
	if !validUntil.After(now) {
		return "", ErrSessionSnapshotExpired
	}
	wantHash := strings.ToLower(strings.TrimSpace(snapshot.PayloadHash))
	decoded, err := hex.DecodeString(wantHash)
	if err != nil || len(decoded) != sha256.Size || wantHash != snapshot.PayloadHash {
		return "", ErrSessionSnapshotInvalid
	}
	copy := proto.Clone(snapshot).(*pb.ResourceSessionAuthorizationSnapshotV2)
	copy.PayloadHash = ""
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(copy)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrSessionSnapshotInvalid, err)
	}
	digest := sha256.Sum256(payload)
	gotHash := hex.EncodeToString(digest[:])
	if gotHash != wantHash {
		return "", ErrSessionSnapshotHashMismatch
	}
	return gotHash, nil
}

func buildSessionSnapshotState(snapshot *pb.ResourceSessionAuthorizationSnapshotV2, now time.Time) (map[string]*pb.ResourceSessionPermissionV2, map[uint16][]string, []*pb.ResourceSessionTerminationCommandV2, error) {
	permissions := make(map[string]*pb.ResourceSessionPermissionV2, len(snapshot.Permissions))
	routes := make(map[uint16][]string)
	routeResources := make(map[uint16]string)
	routeIdentities := make(map[string]string)
	snapshotValidUntil := snapshot.ValidUntil.AsTime().UTC()
	for _, permission := range snapshot.Permissions {
		if err := validateSessionPermission(permission, now, snapshotValidUntil); err != nil {
			return nil, nil, nil, err
		}
		if _, exists := permissions[permission.SessionId]; exists {
			return nil, nil, nil, ErrSessionSnapshotInvalid
		}
		copy := proto.Clone(permission).(*pb.ResourceSessionPermissionV2)
		permissions[copy.SessionId] = copy
		if copy.ResourceType != "container_ssh" {
			continue
		}
		port := uint16(copy.ListenPort)
		if resourceID, exists := routeResources[port]; exists && resourceID != copy.ResourceId {
			return nil, nil, nil, ErrSessionSnapshotRouteConflict
		}
		routeResources[port] = copy.ResourceId
		identityKey := fmt.Sprintf("%d\x00%s\x00%d", port, copy.UserName, copy.DeviceHeadscaleNodeId)
		if sessionID, exists := routeIdentities[identityKey]; exists && sessionID != copy.SessionId {
			return nil, nil, nil, ErrSessionSnapshotRouteConflict
		}
		routeIdentities[identityKey] = copy.SessionId
		routes[port] = append(routes[port], copy.SessionId)
	}
	for port := range routes {
		sort.Strings(routes[port])
	}

	commands := make([]*pb.ResourceSessionTerminationCommandV2, 0, len(snapshot.TerminationCommands))
	seenCommands := make(map[string]struct{}, len(snapshot.TerminationCommands))
	for _, command := range snapshot.TerminationCommands {
		if command == nil || strings.TrimSpace(command.SessionId) == "" || command.CommandRevision <= 0 ||
			strings.TrimSpace(command.ReasonCode) == "" || len(command.ReasonCode) > 100 || len(command.Reason) > 500 {
			return nil, nil, nil, ErrSessionSnapshotInvalid
		}
		key := fmt.Sprintf("%s\x00%d", command.SessionId, command.CommandRevision)
		if _, exists := seenCommands[key]; exists {
			return nil, nil, nil, ErrSessionSnapshotInvalid
		}
		seenCommands[key] = struct{}{}
		commands = append(commands, proto.Clone(command).(*pb.ResourceSessionTerminationCommandV2))
	}
	return permissions, routes, commands, nil
}

func validateSessionPermission(permission *pb.ResourceSessionPermissionV2, now, snapshotValidUntil time.Time) error {
	if permission == nil || strings.TrimSpace(permission.SessionId) == "" || strings.TrimSpace(permission.TenantId) == "" ||
		strings.TrimSpace(permission.ResourceId) == "" || strings.TrimSpace(permission.SourceId) == "" ||
		strings.TrimSpace(permission.TargetRevisionId) == "" || permission.UserId == 0 || strings.TrimSpace(permission.UserName) == "" ||
		permission.DeviceId == 0 || permission.DeviceHeadscaleNodeId == 0 || strings.TrimSpace(permission.AllocationId) == "" ||
		strings.TrimSpace(permission.GrantId) == "" || permission.GrantRevision <= 0 || permission.AuthorizationRevision <= 0 ||
		permission.ValidUntil == nil || !permission.ValidUntil.IsValid() || permission.Target == nil {
		return ErrSessionSnapshotInvalid
	}
	validUntil := permission.ValidUntil.AsTime().UTC()
	if !validUntil.After(now) || validUntil.Before(snapshotValidUntil) {
		return ErrSessionSnapshotExpired
	}
	target := permission.Target
	switch permission.ResourceType {
	case "container_ssh":
		if permission.Action != "shell" || permission.ListenPort < uint32(ContainerSSHPortBase) || permission.ListenPort > uint32(ContainerSSHPortEnd) ||
			len(permission.SshUsers) != 1 || strings.TrimSpace(permission.SshUsers[0]) == "" ||
			strings.TrimSpace(target.NamespaceUid) == "" || strings.TrimSpace(target.NamespaceName) == "" ||
			strings.TrimSpace(target.PodName) == "" || strings.TrimSpace(target.PodUid) == "" || strings.TrimSpace(target.ContainerName) == "" {
			return ErrSessionSnapshotInvalid
		}
	case "container_service":
		if permission.Action != "connect" || permission.ListenPort != 0 || strings.TrimSpace(target.NamespaceUid) == "" ||
			strings.TrimSpace(target.NamespaceName) == "" || strings.TrimSpace(target.ServiceUid) == "" ||
			strings.TrimSpace(target.ServiceName) == "" || target.PortNumber <= 0 || target.PortNumber > 65535 || target.Protocol != "TCP" {
			return ErrSessionSnapshotInvalid
		}
	default:
		return ErrSessionSnapshotInvalid
	}
	return nil
}

func (c *SessionAuthorizationCache) Ack() (int64, string) {
	if c == nil {
		return 0, ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ackRevision, c.ackHash
}

func (c *SessionAuthorizationCache) Current() (int64, string) {
	if c == nil {
		return 0, ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.revision, c.payloadHash
}

func (c *SessionAuthorizationCache) CommitAck(revision int64, payloadHash string) error {
	if c == nil {
		return ErrSessionSnapshotInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if revision != c.revision || payloadHash == "" || payloadHash != c.payloadHash {
		return ErrSessionSnapshotRevisionConflict
	}
	c.ackRevision = revision
	c.ackHash = payloadHash
	return nil
}

func (c *SessionAuthorizationCache) ValidUntil() time.Time {
	if c == nil {
		return time.Time{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.validUntil
}

func (c *SessionAuthorizationCache) Enabled(now time.Time) bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.revision > 0 && c.enforceV2 && c.validUntil.After(now.UTC())
}

// EnforceV2 reports the last completely applied Server policy even after its
// snapshot expires. This prevents v2 traffic from falling back to legacy
// authorization while the control plane is unavailable.
func (c *SessionAuthorizationCache) EnforceV2() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.revision > 0 && c.enforceV2
}

func (c *SessionAuthorizationCache) Permission(sessionID string, now time.Time) (*pb.ResourceSessionPermissionV2, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.revision == 0 || !c.validUntil.After(now.UTC()) {
		return nil, false
	}
	permission := c.permissions[sessionID]
	if permission == nil || !permission.ValidUntil.AsTime().After(now.UTC()) {
		return nil, false
	}
	return proto.Clone(permission).(*pb.ResourceSessionPermissionV2), true
}

func (c *SessionAuthorizationCache) ResolveContainerSSH(listenPort uint16, userName string, headscaleNodeID uint64, now time.Time) (*pb.ResourceSessionPermissionV2, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.revision == 0 || !c.validUntil.After(now.UTC()) {
		return nil, false
	}
	for _, sessionID := range c.sshRoutes[listenPort] {
		permission := c.permissions[sessionID]
		if permission != nil && permission.UserName == userName && permission.DeviceHeadscaleNodeId == headscaleNodeID &&
			permission.ValidUntil.AsTime().After(now.UTC()) {
			return proto.Clone(permission).(*pb.ResourceSessionPermissionV2), true
		}
	}
	return nil, false
}

func (c *SessionAuthorizationCache) HasContainerSSHRoute(listenPort uint16, now time.Time) bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.revision > 0 && c.validUntil.After(now.UTC()) && len(c.sshRoutes[listenPort]) > 0
}

func (c *SessionAuthorizationCache) Permissions(now time.Time) []*pb.ResourceSessionPermissionV2 {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.revision == 0 || !c.validUntil.After(now.UTC()) {
		return nil
	}
	result := make([]*pb.ResourceSessionPermissionV2, 0, len(c.permissions))
	for _, permission := range c.permissions {
		if permission.ValidUntil.AsTime().After(now.UTC()) {
			result = append(result, proto.Clone(permission).(*pb.ResourceSessionPermissionV2))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SessionId < result[j].SessionId })
	return result
}

func (c *SessionAuthorizationCache) Commands() []*pb.ResourceSessionTerminationCommandV2 {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]*pb.ResourceSessionTerminationCommandV2, 0, len(c.commands))
	for _, command := range c.commands {
		result = append(result, proto.Clone(command).(*pb.ResourceSessionTerminationCommandV2))
	}
	return result
}
