package headscale

import (
	"testing"

	v1 "github.com/juanfont/headscale/gen/go/headscale/v1"

	"github.com/stretchr/testify/require"
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
