package headscale

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	v1 "github.com/juanfont/headscale/gen/go/headscale/v1"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestSelectPreferredNodeByGivenNamePrefersOnlineThenNewest(t *testing.T) {
	nodes := []*v1.Node{
		{Id: 122, GivenName: "agent-a", Online: false},
		{Id: 138, GivenName: "agent-a", Online: true},
		{Id: 137, GivenName: "agent-a", Online: true},
		{Id: 200, GivenName: "agent-b", Online: true},
	}
	require.Equal(t, uint64(138), selectPreferredNodeByGivenName(nodes, "agent-a").Id)
	require.Nil(t, selectPreferredNodeByGivenName(nodes, "missing"))
	require.Equal(t, uint64(200), selectPreferredNode(nodes).Id)
}

func TestMergeACLPoliciesPreservesBaselineAndAddsZTNA(t *testing.T) {
	baselineRule := ACLRule{Action: "accept", Src: []string{"tag:legacy-client"}, Dst: []string{"tag:legacy-agent:*"}}
	sharedRule := ACLRule{Action: "accept", Src: []string{"tag:shared"}, Dst: []string{"tag:shared:*"}}
	generatedRule := ACLRule{Action: "accept", Src: []string{"tag:s6-client"}, Dst: []string{"tag:s6-agent:*"}}
	baseline := &ACLPolicy{
		Groups:    map[string][]string{"group:legacy": {"tag:legacy-client"}},
		TagOwners: map[string][]string{"tag:legacy-client": {}, "tag:shared": {"group:legacy"}},
		ACLs:      []ACLRule{baselineRule, sharedRule},
		SSH:       []SSHRule{{Action: "accept", Src: []string{"tag:legacy-client"}, Dst: []string{"tag:legacy-agent"}, Users: []string{"root"}}},
	}
	generated := &ACLPolicy{
		Groups:    map[string][]string{"group:s6": {"tag:s6-client"}},
		TagOwners: map[string][]string{"tag:s6-client": {}, "tag:shared": {"group:s6"}},
		ACLs:      []ACLRule{sharedRule, generatedRule},
	}

	merged, err := mergeACLPolicies(baseline, generated)
	require.NoError(t, err)
	require.Equal(t, []ACLRule{baselineRule, sharedRule, generatedRule}, merged.ACLs)
	require.Equal(t, []string{"group:legacy", "group:s6"}, merged.TagOwners["tag:shared"])
	require.Equal(t, baseline.SSH, merged.SSH)
	require.Equal(t, baseline.Groups["group:legacy"], merged.Groups["group:legacy"])
	require.Equal(t, generated.Groups["group:s6"], merged.Groups["group:s6"])
}

func TestMergeACLPoliciesRejectsConflictingGroupDefinitions(t *testing.T) {
	_, err := mergeACLPolicies(
		&ACLPolicy{Groups: map[string][]string{"group:shared": {"tag:a"}}},
		&ACLPolicy{Groups: map[string][]string{"group:shared": {"tag:b"}}},
	)
	require.ErrorContains(t, err, "定义冲突")
}

func TestMergeACLPoliciesAcceptsEquivalentGroupMemberOrder(t *testing.T) {
	merged, err := mergeACLPolicies(
		&ACLPolicy{Groups: map[string][]string{"group:shared": {"tag:a", "tag:b"}}},
		&ACLPolicy{Groups: map[string][]string{"group:shared": {"tag:b", "tag:a"}}},
	)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"tag:a", "tag:b"}, merged.Groups["group:shared"])
}

func TestGenerateACLPolicyAllowsGrantedSSHUserOnNodeDomainTargetPort(t *testing.T) {
	database := newHeadscaleACLTestDB(t)
	agent := model.User{Name: "aliyun", Role: model.UserRoleAgent, Enabled: true}
	client := model.User{Name: "devops", Role: model.UserRoleClient, Enabled: true}
	require.NoError(t, database.Create(&agent).Error)
	require.NoError(t, database.Create(&client).Error)
	require.NoError(t, database.Create(&model.Node{
		UserID: agent.ID,
		Name:   "aliyun-119",
		Type:   model.NodeTypeAgent,
		IP:     "100.64.0.123",
	}).Error)
	require.NoError(t, database.Create(&model.DomainRegistry{
		Domain:       "aliyun-119.ali.szzy.beagle",
		Type:         model.DomainTypeSSH,
		UserID:       agent.ID,
		ResourceKind: model.DomainResourceNode,
		ResourceID:   "1",
		TargetIP:     "100.64.0.123",
		TargetPort:   2222,
		Status:       model.DomainStatusOnline,
	}).Error)
	require.NoError(t, database.Create(&model.AclSSHUserPermission{
		TargetUserID: agent.ID,
		UserID:       client.ID,
		SSHUsers:     `["root"]`,
		Enabled:      true,
	}).Error)

	policy, err := NewACLSyncService(nil).generateACLPolicy(context.Background())
	require.NoError(t, err)
	require.Contains(t, policy.ACLs, ACLRule{
		Action: "accept",
		Src:    []string{"tag:client-devops"},
		Dst:    []string{"tag:agent-aliyun:2222"},
	})
}

func TestGenerateACLPolicyAllowsUnifiedHostSSHGroupGrantOnNodeDomainTargetPort(t *testing.T) {
	database := newHeadscaleACLTestDB(t)
	tenantID := "tenant-a"
	agent := model.User{Name: "szzy-szzy-49533781", Role: model.UserRoleAgent, Enabled: true}
	require.NoError(t, database.Create(&agent).Error)
	require.NoError(t, database.Create(&model.Node{
		ID:     140,
		UserID: agent.ID,
		Name:   "szzy",
		Type:   model.NodeTypeAgent,
		IP:     "100.64.0.126",
	}).Error)
	require.NoError(t, database.Create(&model.Group{ID: 2, TenantID: tenantID, Name: "devops"}).Error)
	require.NoError(t, database.Create(&model.Resource{
		ID:             "resource-host-ssh",
		TenantID:       tenantID,
		Type:           model.ResourceTypeHostSSH,
		DisplayName:    "szzy.szzy.szzy.beagle",
		AgentNodeID:    140,
		TargetRevision: 1,
		State:          model.ResourceStateAvailable,
	}).Error)
	require.NoError(t, database.Create(&model.DomainRegistry{
		Domain:       "szzy.szzy.szzy.beagle",
		Type:         model.DomainTypeSSH,
		UserID:       agent.ID,
		ResourceKind: model.DomainResourceNode,
		ResourceID:   "140",
		NodeID:       140,
		TargetIP:     "100.64.0.126",
		TargetPort:   2222,
		SshUsers:     `["root"]`,
		Status:       model.DomainStatusOnline,
	}).Error)
	require.NoError(t, database.Create(&model.AccessGrant{
		ID:             "grant-group-devops",
		TenantID:       tenantID,
		ResourceID:     "resource-host-ssh",
		SubjectType:    "group",
		SubjectGroupID: ptrInt64(2),
		Actions:        `["shell"]`,
		ValidFrom:      time.Now().Add(-time.Minute),
		ExpiresAt:      time.Now().Add(time.Hour),
		Revision:       1,
		Status:         "enabled",
	}).Error)

	policy, err := NewACLSyncService(nil).generateACLPolicy(context.Background())
	require.NoError(t, err)
	require.Contains(t, policy.ACLs, ACLRule{
		Action: "accept",
		Src:    []string{"tag:group-devops"},
		Dst:    []string{"tag:agent-szzy-szzy-49533781:2222"},
	})
	require.Contains(t, policy.SSH, SSHRule{
		Action: "accept",
		Src:    []string{"tag:group-devops"},
		Dst:    []string{"tag:agent-szzy-szzy-49533781"},
		Users:  []string{"root"},
	})
}

func ptrInt64(value int64) *int64 {
	return &value
}

func TestGenerateACLPolicyAllowsTunnelDeployTokenToTargetAgent(t *testing.T) {
	database := newHeadscaleACLTestDB(t)

	agentUser := model.User{Name: "szzy-agent-user", Role: model.UserRoleAgent, Enabled: true}
	tunnelUser := model.User{Name: "svc-tunnel-5090", Role: model.UserRoleClient, Enabled: true}
	require.NoError(t, database.Create(&agentUser).Error)
	require.NoError(t, database.Create(&tunnelUser).Error)

	agentNode := model.Node{
		UserID: agentUser.ID,
		Name:   "edge-gpu-5090",
		Type:   model.NodeTypeAgent,
		IP:     "100.64.0.50",
	}
	require.NoError(t, database.Create(&agentNode).Error)

	// Case 1: K8s API disabled -> only :50051
	deployToken := model.DeployToken{
		Token:           "dt_test_tunnel_token",
		UserID:          tunnelUser.ID,
		Name:            "5090",
		Status:          model.DeployTokenStatusPending,
		TargetAgentName: "edge-gpu-5090",
		Mode:            "tunnel",
		PortsConfig:     `{"k8s_api_enabled":false}`,
	}
	require.NoError(t, database.Create(&deployToken).Error)

	service := NewACLSyncService(nil)
	policy, err := service.generateACLPolicy(context.Background())
	require.NoError(t, err)

	// Verify that the ACL policy allows tag:client-svc-tunnel-5090 to access :50051 only (no :*)
	require.Contains(t, policy.ACLs, ACLRule{
		Action: "accept",
		Src:    []string{"tag:client-svc-tunnel-5090"},
		Dst:    []string{"tag:agent-edge-gpu-5090:50051"},
	})
	require.Contains(t, policy.ACLs, ACLRule{
		Action: "accept",
		Src:    []string{"tag:client-svc-tunnel-5090"},
		Dst:    []string{"tag:agent-szzy-agent-user:50051"},
	})
	for _, rule := range policy.ACLs {
		if len(rule.Src) > 0 && rule.Src[0] == "tag:client-svc-tunnel-5090" {
			for _, dst := range rule.Dst {
				if strings.HasPrefix(dst, "tag:agent-") {
					require.NotContains(t, dst, ":*", "Tunnel -> Agent ACL rules must not contain :* wildcard")
				}
			}
		}
	}

	// Case 2: K8s API enabled -> :50051 and :6443
	deployToken.PortsConfig = `{"k8s_api_enabled":true}`
	require.NoError(t, database.Save(&deployToken).Error)

	policyWithK8s, err := service.generateACLPolicy(context.Background())
	require.NoError(t, err)
	require.Contains(t, policyWithK8s.ACLs, ACLRule{
		Action: "accept",
		Src:    []string{"tag:client-svc-tunnel-5090"},
		Dst:    []string{"tag:agent-edge-gpu-5090:50051", "tag:agent-edge-gpu-5090:6443"},
	})
	require.Contains(t, policyWithK8s.ACLs, ACLRule{
		Action: "accept",
		Src:    []string{"tag:client-svc-tunnel-5090"},
		Dst:    []string{"tag:agent-szzy-agent-user:50051", "tag:agent-szzy-agent-user:6443"},
	})
}

func TestGenerateACLPolicy_AbortsOnError(t *testing.T) {
	database := newHeadscaleACLTestDB(t)

	// Dropping deploy_tokens table to simulate DB error
	require.NoError(t, database.Migrator().DropTable(&model.DeployToken{}))

	service := NewACLSyncService(nil)
	policy, err := service.generateACLPolicy(context.Background())
	require.Error(t, err)
	require.Nil(t, policy)
	require.Contains(t, err.Error(), "查询 Tunnel DeployToken 失败")

	// SyncACL also aborts before SetPolicy
	syncErr := service.SyncACL(context.Background())
	require.Error(t, syncErr)
	require.Contains(t, syncErr.Error(), "生成 ACL 策略失败")
}


func newHeadscaleACLTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	original := db.DB
	t.Cleanup(func() { db.DB = original })

	database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:headscale_acl_%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.User{},
		&model.Group{},
		&model.GroupMember{},
		&model.Node{},
		&model.DomainRegistry{},
		&model.Resource{},
		&model.AccessGrant{},
		&model.DeployToken{},
		&model.ProxyService{},
		&model.AclServiceUserPermission{},
		&model.AclServiceGroupPermission{},
		&model.AclUserUserPermission{},
		&model.AclUserGroupPermission{},
		&model.AclGroupUserPermission{},
		&model.AclGroupGroupPermission{},
		&model.AclSSHUserPermission{},
		&model.AclSSHGroupPermission{},
	))
	db.DB = database
	return database
}

// B1 回归：多设备用户的每条 desktop 记录只能匹配到自己的设备，不能被"最新在线节点"覆盖
func TestMatchHeadscaleNodeKeepsMultiDeviceUsersOnOwnDevice(t *testing.T) {
	hsNodes := []hsNodeInfo{
		{HeadscaleNodeID: 59, GivenName: "ide", IP: "100.64.0.58", Online: false},
		{HeadscaleNodeID: 111, GivenName: "4.local", IP: "100.64.0.24", Online: true},
		{HeadscaleNodeID: 112, GivenName: "bogon", IP: "100.64.0.25", Online: true},
	}

	ide := &model.Node{Name: "ide", IP: "100.64.0.58"}
	require.Equal(t, uint64(59), matchHeadscaleNode(ide, hsNodes, false).HeadscaleNodeID)

	// IP 为空时按 GivenName 匹配，离线也不能漂移到同用户的其他在线设备
	ideNoIP := &model.Node{Name: "ide"}
	require.Equal(t, uint64(59), matchHeadscaleNode(ideNoIP, hsNodes, false).HeadscaleNodeID)

	// 名称和 IP 都对不上时返回 nil（由调用方清空），不能兜底到别的设备
	unknown := &model.Node{Name: "HOME-MENGK"}
	require.Nil(t, matchHeadscaleNode(unknown, hsNodes, false))
}

// Tunnel 服务账号：Pod 重建后 GivenName 带随机后缀，旧 IP 指向离线旧节点，应取在线最新节点
func TestMatchHeadscaleNodeTunnelAccountPrefersOnlineNewest(t *testing.T) {
	hsNodes := []hsNodeInfo{
		{HeadscaleNodeID: 201, GivenName: "tunnel-gpu-5090-xfipesky", IP: "100.64.0.141", Online: false},
		{HeadscaleNodeID: 202, GivenName: "tunnel-gpu-5090-t1f0ec9i", IP: "100.64.0.142", Online: true},
		{HeadscaleNodeID: 190, GivenName: "tunnel-gpu-5090", IP: "100.64.0.130", Online: false},
	}
	tunnel := &model.Node{Name: "tunnel-gpu-5090", IP: "100.64.0.141"}

	require.Equal(t, uint64(202), matchHeadscaleNode(tunnel, hsNodes, true).HeadscaleNodeID)
	// 不是 Tunnel 账号时维持原有精确匹配语义（按旧 IP 命中旧节点）
	require.Equal(t, uint64(201), matchHeadscaleNode(tunnel, hsNodes, false).HeadscaleNodeID)
}

