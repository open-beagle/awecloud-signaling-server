package headscale

import (
	"context"
	"fmt"
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
