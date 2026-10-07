package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"

	v1 "github.com/juanfont/headscale/gen/go/headscale/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type auditPolicyServer struct {
	v1.UnimplementedHeadscaleServiceServer
	mu     sync.Mutex
	policy string
	fail   bool
}

func (s *auditPolicyServer) GetPolicy(context.Context, *v1.GetPolicyRequest) (*v1.GetPolicyResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &v1.GetPolicyResponse{Policy: s.policy}, nil
}

func (s *auditPolicyServer) SetPolicy(_ context.Context, req *v1.SetPolicyRequest) (*v1.SetPolicyResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return nil, errors.New("injected policy failure")
	}
	s.policy = req.Policy
	return &v1.SetPolicyResponse{}, nil
}

func TestTunnelWriteAudit_PolicyAndSync(t *testing.T) {
	database := setupACLWriteAuditDB(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	policy := &auditPolicyServer{policy: `{"acls":[]}`}
	v1.RegisterHeadscaleServiceServer(server, policy)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	client, err := headscale.NewClient(headscale.Config{URL: "http://" + listener.Addr().String(), APIKey: "must-not-log"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	api := NewTunnelAPI(&config.ServerConfig{})
	api.hsClient = client
	api.aclSync = &testMockACLSyncer{}
	body, err := json.Marshal(UpdateTunnelACLRequest{Policy: `{"acls":[{"action":"accept","src":["tag:test"],"dst":["tag:target:6443"]}],"credential":"must-not-log"}`})
	require.NoError(t, err)
	w := auditRequest(api.UpdateTunnelACL, http.MethodPut, "", "", string(body))
	require.Equal(t, 200, w.Code, w.Body.String())
	entry, detail := lastSensitiveAudit(t, database)
	require.Equal(t, model.ActionUpdateTunnelACL, entry.ActionType)
	require.Equal(t, "applied", detail.ExternalStatus)
	require.False(t, detail.DatabaseCommitted)
	require.Contains(t, entry.Detail, "tag:target:6443")
	require.NotEqual(t, detail.Before, detail.After)
	policy.mu.Lock()
	policy.fail = true
	policy.mu.Unlock()
	w = auditRequest(api.UpdateTunnelACL, http.MethodPut, "", "", string(body))
	require.Equal(t, 500, w.Code)
	_, detail = lastSensitiveAudit(t, database)
	require.Equal(t, "failed", detail.Result)
	require.Equal(t, "unknown", detail.ExternalStatus)
	require.Nil(t, detail.After)
	w = auditRequest(api.SyncTunnelACL, http.MethodPost, "", "", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	_, detail = lastSensitiveAudit(t, database)
	require.Equal(t, "succeeded", detail.SyncStatus)
	require.Equal(t, "applied", detail.ExternalStatus)
	api.aclSync = &testMockACLSyncer{syncErr: errors.New("injected sync failure")}
	w = auditRequest(api.SyncTunnelACL, http.MethodPost, "", "", "")
	require.Equal(t, 500, w.Code)
	_, detail = lastSensitiveAudit(t, database)
	require.Equal(t, "failed", detail.SyncStatus)
	require.Equal(t, "unknown", detail.ExternalStatus)
}

func TestTunnelWriteAudit_RejectedAndRolledBack(t *testing.T) {
	database := setupACLWriteAuditDB(t)
	api := NewTunnelAPI(&config.ServerConfig{})
	w := auditRequest(api.UpdateSignalTunnelPorts, http.MethodPut, "bad-id", "", `{}`)
	require.Equal(t, 400, w.Code)
	_, detail := lastSensitiveAudit(t, database)
	require.False(t, detail.DatabaseCommitted)
	w = auditRequest(api.DeleteSignalTunnel, http.MethodDelete, "999", "", "")
	require.Equal(t, 404, w.Code)
	_, detail = lastSensitiveAudit(t, database)
	require.Equal(t, "failed", detail.Result)
	// Token creation and its service account must roll back if audit evidence cannot commit.
	require.NoError(t, database.Exec(`CREATE TRIGGER fail_tunnel_audit BEFORE UPDATE ON audit_log WHEN json_extract(NEW.detail, '$.database_committed')=1 BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END`).Error)
	w = auditRequest(api.CreateSignalTunnel, http.MethodPost, "", "", `{"name":"audit-rollback","target_agent":"edge","token":"must-not-log"}`)
	require.Equal(t, 500, w.Code)
	_, detail = lastSensitiveAudit(t, database)
	require.False(t, detail.DatabaseCommitted)
	require.Nil(t, detail.After)
	var count int64
	require.NoError(t, database.Model(&model.User{}).Where("name = ?", "svc-tunnel-audit-rollback").Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, database.Model(&model.DeployToken{}).Count(&count).Error)
	require.Zero(t, count)
}
