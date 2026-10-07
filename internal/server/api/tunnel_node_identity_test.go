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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type tunnelNodeIdentityServer struct {
	v1.UnimplementedHeadscaleServiceServer
	mu        sync.Mutex
	node      *v1.Node
	getErr    error
	deleteErr error
	onDelete  func() error
	deleteIDs []uint64
}

func (s *tunnelNodeIdentityServer) GetNode(_ context.Context, req *v1.GetNodeRequest) (*v1.GetNodeResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.node == nil || req.NodeId != 208 {
		return nil, status.Error(codes.NotFound, "not found")
	}
	return &v1.GetNodeResponse{Node: s.node}, nil
}

func (s *tunnelNodeIdentityServer) DeleteNode(_ context.Context, req *v1.DeleteNodeRequest) (*v1.DeleteNodeResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteIDs = append(s.deleteIDs, req.NodeId)
	if s.deleteErr != nil {
		return nil, s.deleteErr
	}
	if s.onDelete != nil {
		if err := s.onDelete(); err != nil {
			return nil, err
		}
	}
	return &v1.DeleteNodeResponse{}, nil
}

func (s *tunnelNodeIdentityServer) deletedIDs() []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]uint64(nil), s.deleteIDs...)
}

func setupTunnelNodeIdentityAPI(t *testing.T, mock *tunnelNodeIdentityServer) *TunnelAPI {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	v1.RegisterHeadscaleServiceServer(server, mock)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	client, err := headscale.NewClient(headscale.Config{URL: "http://" + listener.Addr().String(), APIKey: "must-not-log"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return &TunnelAPI{hsClient: client}
}

func identityCollisionNodes(t *testing.T, database *gorm.DB, kind model.NodeType) (model.Node, model.Node) {
	t.Helper()
	linked := model.Node{ID: 72, UserID: 1, Type: kind, Name: "target", HeadscaleNodeID: 208, IP: "100.64.0.148", SecretHash: "must-not-log"}
	collision := model.Node{ID: 208, UserID: 1, Type: kind, Name: "unrelated", HeadscaleNodeID: 900, IP: "100.64.0.99"}
	require.NoError(t, database.Create(&linked).Error)
	require.NoError(t, database.Create(&collision).Error)
	// Compare database snapshots, including timestamps normalized by SQLite.
	require.NoError(t, database.First(&collision, collision.ID).Error)
	return linked, collision
}

func TestTunnelNodeIdentity_ExactBinding(t *testing.T) {
	for _, kind := range []model.NodeType{model.NodeTypeAgent, model.NodeTypeDesktop} {
		t.Run(string(kind), func(t *testing.T) {
			database := setupACLWriteAuditDB(t)
			_, collision := identityCollisionNodes(t, database, kind)
			prefix := "agent-provider-scoped-name"
			if kind == model.NodeTypeDesktop {
				prefix = "client-svc-tunnel-target"
			}
			mock := &tunnelNodeIdentityServer{node: &v1.Node{Id: 208, User: &v1.User{Id: 62, Name: prefix}}}
			api := setupTunnelNodeIdentityAPI(t, mock)
			w := auditRequest(api.GetTunnelNode, http.MethodGet, "208", "", "")
			require.Equal(t, 200, w.Code, w.Body.String())
			var detail struct {
				Data TunnelNodeDetail `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
			require.Equal(t, uint64(72), detail.Data.LinkedID)
			require.Equal(t, string(kind), detail.Data.LinkedType)
			w = auditRequest(api.DeleteTunnelNode, http.MethodDelete, "208", "", "")
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, []uint64{208}, mock.deletedIDs())
			var untouched model.Node
			require.NoError(t, database.First(&untouched, 208).Error)
			require.Equal(t, collision, untouched, "numeric primary-key collision must not be changed")
			var actual model.Node
			err := database.First(&actual, 72).Error
			if kind == model.NodeTypeDesktop {
				require.ErrorIs(t, err, gorm.ErrRecordNotFound)
			} else {
				require.NoError(t, err)
				require.Empty(t, actual.IP)
				require.Zero(t, actual.HeadscaleNodeID)
			}
			entry, audit := lastSensitiveAudit(t, database)
			require.Equal(t, "208", entry.TargetID)
			require.Equal(t, "succeeded", audit.Result)
			require.Equal(t, "applied", audit.ExternalStatus)
			require.True(t, audit.DatabaseCommitted)
			require.Equal(t, float64(72), audit.Before.(map[string]interface{})["id"])
		})
	}
}

func TestTunnelNodeIdentity_OrphanDoesNotUseIDOrNameFallback(t *testing.T) {
	database := setupACLWriteAuditDB(t)
	_, collision := identityCollisionNodes(t, database, model.NodeTypeDesktop)
	require.NoError(t, database.Model(&model.Node{}).Where("id = 72").Update("headscale_node_id", 0).Error)
	mock := &tunnelNodeIdentityServer{node: &v1.Node{Id: 208, Name: "target", User: &v1.User{Name: "client-audit-target"}}}
	api := setupTunnelNodeIdentityAPI(t, mock)
	w := auditRequest(api.GetTunnelNode, http.MethodGet, "208", "", "")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"linked_type":"none"`)
	w = auditRequest(api.DeleteTunnelNode, http.MethodDelete, "208", "", "")
	require.Equal(t, 200, w.Code)
	var nodes []model.Node
	require.NoError(t, database.Order("id").Find(&nodes).Error)
	require.Len(t, nodes, 2)
	require.Equal(t, collision, nodes[1])
	require.Zero(t, nodes[0].HeadscaleNodeID)
	_, audit := lastSensitiveAudit(t, database)
	require.False(t, audit.DatabaseCommitted)
	require.Nil(t, audit.Before)
}

func TestTunnelNodeIdentity_FailuresAndRebinding(t *testing.T) {
	for _, scenario := range []string{"duplicate", "unknown-type", "lookup-db-failure", "not-found", "get-failure", "wrong-hs-id", "delete-failure", "local-delete-failure", "audit-commit-failure", "audit-preflight-failure", "rebound-desktop", "rebound-agent", "zero", "invalid"} {
		t.Run(scenario, func(t *testing.T) {
			database := setupACLWriteAuditDB(t)
			kind := model.NodeTypeDesktop
			if scenario == "rebound-agent" {
				kind = model.NodeTypeAgent
			}
			_, collision := identityCollisionNodes(t, database, kind)
			mock := &tunnelNodeIdentityServer{node: &v1.Node{Id: 208, User: &v1.User{Name: "client-target"}}}
			wantStatus, deleteCalled, requestID := 500, false, "208"
			switch scenario {
			case "duplicate":
				require.NoError(t, database.Create(&model.Node{ID: 73, UserID: 1, Name: "duplicate", Type: kind, HeadscaleNodeID: 208}).Error)
				wantStatus = 409
			case "unknown-type":
				require.NoError(t, database.Model(&model.Node{}).Where("id = 72").Update("type", "unknown").Error)
				wantStatus = 409
			case "lookup-db-failure":
				require.NoError(t, database.Migrator().DropTable(&model.Node{}))
			case "not-found":
				mock.getErr, wantStatus = status.Error(codes.NotFound, "missing"), 404
			case "get-failure":
				mock.getErr = status.Error(codes.Unavailable, "unavailable")
			case "wrong-hs-id":
				mock.node.Id = 999
			case "delete-failure":
				mock.deleteErr, deleteCalled = errors.New("injected delete failure"), true
			case "local-delete-failure":
				require.NoError(t, database.Exec(`CREATE TRIGGER fail_local_delete BEFORE DELETE ON node BEGIN SELECT RAISE(ABORT, 'injected local failure'); END`).Error)
				deleteCalled = true
			case "audit-commit-failure":
				require.NoError(t, database.Exec(`CREATE TRIGGER fail_delete_audit BEFORE UPDATE ON audit_log WHEN json_extract(NEW.detail, '$.database_committed')=1 BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END`).Error)
				deleteCalled = true
			case "audit-preflight-failure":
				require.NoError(t, database.Exec(`CREATE TRIGGER fail_delete_audit BEFORE UPDATE ON audit_log BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END`).Error)
			case "rebound-desktop", "rebound-agent":
				mock.onDelete = func() error {
					return database.Model(&model.Node{}).Where("id = 72").Updates(map[string]interface{}{"headscale_node_id": 901, "ip": "100.64.0.100"}).Error
				}
				wantStatus, deleteCalled = 409, true
			case "zero":
				requestID, wantStatus = "0", 400
			case "invalid":
				requestID, wantStatus = "bad", 400
			}
			api := setupTunnelNodeIdentityAPI(t, mock)
			if !deleteCalled && scenario != "audit-preflight-failure" {
				w := auditRequest(api.GetTunnelNode, http.MethodGet, requestID, "", "")
				require.Equal(t, wantStatus, w.Code, w.Body.String())
			}
			w := auditRequest(api.DeleteTunnelNode, http.MethodDelete, requestID, "", "")
			require.Equal(t, wantStatus, w.Code, w.Body.String())
			if deleteCalled {
				require.Equal(t, []uint64{208}, mock.deletedIDs())
			} else {
				require.Empty(t, mock.deletedIDs())
			}
			if scenario != "lookup-db-failure" {
				var untouched, linked model.Node
				require.NoError(t, database.First(&untouched, 208).Error)
				require.Equal(t, collision, untouched)
				require.NoError(t, database.First(&linked, 72).Error)
				if scenario == "rebound-desktop" || scenario == "rebound-agent" {
					require.Equal(t, uint64(901), linked.HeadscaleNodeID)
					require.Equal(t, "100.64.0.100", linked.IP)
				} else {
					require.Equal(t, uint64(208), linked.HeadscaleNodeID)
				}
			}
			if scenario == "local-delete-failure" || scenario == "audit-commit-failure" || scenario == "rebound-desktop" || scenario == "rebound-agent" {
				_, audit := lastSensitiveAudit(t, database)
				require.Equal(t, "partial", audit.Result)
				require.Equal(t, "applied", audit.ExternalStatus)
				require.False(t, audit.DatabaseCommitted)
			}
		})
	}
}
