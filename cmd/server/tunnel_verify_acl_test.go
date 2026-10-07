package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/glebarez/sqlite"
	v1 "github.com/juanfont/headscale/gen/go/headscale/v1"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"gorm.io/gorm"
	"net"
	"testing"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/stretchr/testify/require"
)

type verifyNodeReader func(context.Context, string) ([]*v1.Node, error)

func (f verifyNodeReader) ListNodesByUser(ctx context.Context, name string) ([]*v1.Node, error) {
	return f(ctx, name)
}

func TestTunnelACLIdentityUsesHeadscaleUsernameAndActualTags(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, database.AutoMigrate(&model.Group{}, &model.GroupMember{}))
	previous := db.DB
	db.DB = database
	t.Cleanup(func() { db.DB = previous })
	tok := model.DeployToken{UserID: 7, User: &model.User{ID: 7, Name: "svc-tunnel-a", Role: model.UserRoleClient}}
	reader := verifyNodeReader(func(_ context.Context, name string) ([]*v1.Node, error) {
		require.Equal(t, "client-svc-tunnel-a", name)
		return []*v1.Node{{ForcedTags: []string{"tag:group-from-headscale"}, IpAddresses: []string{"100.64.0.8"}}}, nil
	})
	id, err := loadTunnelACLIdentity(context.Background(), reader, tok)
	require.NoError(t, err)
	require.True(t, id.Selectors["client-svc-tunnel-a"])
	require.True(t, id.Selectors["tag:group-from-headscale"])
	require.Len(t, id.IPs, 1)
	_, err = loadTunnelACLIdentity(context.Background(), verifyNodeReader(func(context.Context, string) ([]*v1.Node, error) { return nil, errors.New("headscale down") }), tok)
	require.Error(t, err)
}

func TestTunnelACLMinimumEffectivePermissions(t *testing.T) {
	id := tunnelACLIdentity{Selectors: map[string]bool{"tag:client-tunnel": true, "tag:group-ops": true, "tunnel": true}, IPs: []net.IP{net.ParseIP("100.64.0.8")}}
	allowed := tunnelAllowedDestinations([]string{"tag:agent-node", "tag:agent-owner"}, true)
	base := []headscale.ACLRule{
		{Action: "accept", Src: []string{"tag:client-tunnel"}, Dst: []string{"tag:agent-node:50051", "tag:agent-node:6443"}},
		{Action: "accept", Src: []string{"tag:client-tunnel"}, Dst: []string{"tag:agent-owner:50051", "tag:agent-owner:6443"}},
	}
	tests := []struct {
		name, src, dst string
		fail           bool
	}{
		{"legal dual tags", "", "", false},
		{"other client unrelated", "tag:client-other", "tag:agent-other:*", false},
		{"extra port", "tag:client-tunnel", "tag:agent-node:8080", true},
		{"wrong target same port", "tag:client-tunnel", "tag:agent-other:50051", true},
		{"self wildcard", "tag:client-tunnel", "tag:client-tunnel:*", true},
		{"destination wildcard", "tag:client-tunnel", "*:*", true},
		{"port range", "tag:client-tunnel", "tag:agent-node:1-65535", true},
		{"actual group tag", "tag:group-ops", "tag:agent-node:22", true},
		{"policy group", "group:ops", "tag:agent-node:22", true},
		{"all sources", "*", "tag:agent-node:22", true},
		{"source CIDR", "100.64.0.0/10", "tag:agent-node:22", true},
		{"source IP", "100.64.0.8", "tag:agent-node:22", true},
		{"outside CIDR", "192.168.0.0/16", "tag:agent-node:22", false},
		{"autogroup", "autogroup:member", "tag:agent-node:22", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &headscale.ACLPolicy{ACLs: append([]headscale.ACLRule{}, base...), Groups: map[string][]string{"group:ops": {"group:nested"}, "group:nested": {"tunnel"}}}
			if tt.src != "" {
				p.ACLs = append(p.ACLs, headscale.ACLRule{Action: "accept", Src: []string{tt.src}, Dst: []string{tt.dst}})
			}
			err := checkTunnelACL(p, id, allowed)
			if tt.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
	t.Run("missing alias permission", func(t *testing.T) {
		require.Error(t, checkTunnelACL(&headscale.ACLPolicy{ACLs: base[:1]}, id, allowed))
	})
	t.Run("disabled k8s rejects 6443", func(t *testing.T) {
		require.Error(t, checkTunnelACL(&headscale.ACLPolicy{ACLs: base}, id, tunnelAllowedDestinations([]string{"tag:agent-node", "tag:agent-owner"}, false)))
	})
	t.Run("group cycle fails closed", func(t *testing.T) {
		_, err := tunnelSourceMatches("group:a", id, map[string][]string{"group:a": {"group:a"}}, map[string]bool{})
		require.Error(t, err)
	})
}

func TestTunnelProbeReadWriteAndCleanupFailures(t *testing.T) {
	const src = "tag:client-probe"
	created, _ := json.Marshal(headscale.ACLPolicy{ACLs: []headscale.ACLRule{{Action: "accept", Src: []string{src}, Dst: []string{"tag:agent-a:50051"}}}})
	tests := []struct {
		name         string
		failAt       string
		readOverride string
		wantPass     bool
	}{
		{"success", "", "", true},
		{"create database error", "create", "", false},
		{"create sync error", "sync1", "", false},
		{"create read error", "read1", "", false},
		{"create malformed json", "", "[invalid", false},
		{"revoke database error", "revoke", "", false},
		{"revoke sync error", "sync2", "", false},
		{"revoke read error", "read2", "", false},
		{"cleanup error invalidates success", "cleanup", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syncs, reads, cleanups := 0, 0, 0
			failure := func(at string) error {
				if tt.failAt == at {
					return errors.New(at + " failed")
				}
				return nil
			}
			ops := tunnelProbeOps{
				Create: func() error { return failure("create") }, Revoke: func() error { return failure("revoke") },
				Sync: func() error {
					syncs++
					if syncs == 1 {
						return failure("sync1")
					}
					return failure("sync2")
				},
				ReadPolicy: func() (string, error) {
					reads++
					if reads == 1 {
						if tt.readOverride != "" {
							return tt.readOverride, nil
						}
						return string(created), failure("read1")
					}
					return `{"acls":[]}`, failure("read2")
				},
				Cleanup: func() error { cleanups++; return failure("cleanup") },
			}
			r := runTunnelPolicyProbe(ops, src, tunnelAllowedDestinations([]string{"tag:agent-a"}, false))
			require.Equal(t, tt.wantPass, r.Passed, r.Message)
			require.Equal(t, 1, cleanups, "cleanup must run even after partial creation failure")
			if tt.failAt == "cleanup" {
				require.Contains(t, r.Message, "cleanup FAIL")
			} else {
				require.Contains(t, r.Message, "cleanup PASS")
			}
			if tt.wantPass {
				require.Contains(t, r.Message, "API hooks NOT COVERED")
			}
		})
	}
}

func TestTunnelProbeRevocationRequiresStructuredPolicy(t *testing.T) {
	const src = "tag:client-probe"
	for _, raw := range []string{"", "null", "{}", `{"acls":null}`, `{bad`, `{"acls":[{"action":"accept","src":["tag:client-probe"],"dst":["tag:agent-a:50051"]}]}`, `{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}`} {
		t.Run(raw, func(t *testing.T) {
			reads := 0
			ops := tunnelProbeOps{Create: func() error { return nil }, Revoke: func() error { return nil }, Sync: func() error { return nil }, Cleanup: func() error { return nil }, ReadPolicy: func() (string, error) {
				reads++
				if reads == 1 {
					return `{"acls":[{"action":"accept","src":["tag:client-probe"],"dst":["tag:agent-a:50051"]}]}`, nil
				}
				return raw, nil
			}}
			r := runTunnelPolicyProbe(ops, src, tunnelAllowedDestinations([]string{"tag:agent-a"}, false))
			require.False(t, r.Passed, r.Message)
		})
	}
	// Tag ownership metadata is not an access rule and must not cause a false residual.
	p, err := parseVerifiedACL(`{"tagOwners":{"tag:client-probe":[]},"acls":[]}`)
	require.NoError(t, err)
	require.NoError(t, checkTunnelACL(p, tunnelACLIdentity{Selectors: map[string]bool{src: true}}, map[string]bool{}))
}
