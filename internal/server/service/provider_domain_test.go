package service

import (
	"context"
	"strconv"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func newProviderDomainTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.User{}, &model.ResourceProvider{}, &model.TechnicalResource{}, &model.TechnicalResourceBinding{},
		&model.Node{}, &model.Endpoint{}, &model.DomainRegistry{}, &model.SystemConfig{},
	))
	require.NoError(t, database.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uk_test_ssh_domain ON domain_registry(lower(domain)) WHERE type = 'ssh'`).Error)
	return database
}

func createDomainAgent(t *testing.T, database *gorm.DB, provider model.ResourceProvider, agentLabel string, nodeIDs ...uint64) (model.User, model.TechnicalResource, []model.Node) {
	t.Helper()
	require.NoError(t, database.Create(&provider).Error)
	user := model.User{Name: provider.Key + "-agent", Role: model.UserRoleAgent, SecretHash: "fixture", Enabled: true, SSHEnabled: true}
	require.NoError(t, database.Create(&user).Error)
	agent := model.TechnicalResource{
		ID: provider.ID + "-agent", ProviderID: provider.ID, Type: model.TechnicalResourceAgent,
		StableKey: provider.ID + "-agent", DomainLabel: agentLabel, LifecycleState: model.TechnicalResourceRegistered,
		HealthState: model.ResourceHealthOnline, CredentialRevision: 1, RuntimeUserID: user.ID, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&agent).Error)
	nodes := make([]model.Node, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		label := "beagle-" + strconv.FormatUint(nodeID, 10)
		node := model.Node{ID: nodeID, UserID: user.ID, Name: label, Hostname: label, HostDomainLabel: label, Type: model.NodeTypeAgent, IP: "100.64.0." + strconv.FormatUint(nodeID%255, 10)}
		require.NoError(t, database.Create(&node).Error)
		binding := model.TechnicalResourceBinding{
			ID: agent.ID + "-" + strconv.FormatUint(nodeID, 10), TechnicalResourceID: agent.ID,
			SourceType: model.TechnicalResourceBindingLegacyNode, SourceID: strconv.FormatUint(nodeID, 10),
			CredentialRevision: 1, Enabled: true, BoundByUserID: user.ID, Reason: "test", RowVersion: 1,
		}
		require.NoError(t, database.Create(&binding).Error)
		nodes = append(nodes, node)
	}
	return user, agent, nodes
}

func TestProviderDomainLabelValidation(t *testing.T) {
	label, err := NormalizeProviderDomainLabel(" Beagle-BJ ")
	require.NoError(t, err)
	require.Equal(t, "beagle-bj", label)
	for _, value := range []string{"", "-beagle", "beagle_1", "kubernetes", "a.b", "中文"} {
		_, err := NormalizeProviderDomainLabel(value)
		require.ErrorIs(t, err, ErrProviderDomainLabelInvalid, value)
	}
}

func TestDomainServiceBuildsRootAgentDomainsForMultipleNodes(t *testing.T) {
	database := newProviderDomainTestDB(t)
	ctx := context.Background()
	provider := model.ResourceProvider{ID: "beagle", Key: "beagle", DisplayName: "北京比格", DomainScope: model.ProviderDomainRoot, Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	user, agent, nodes := createDomainAgent(t, database, provider, "beijing", 241, 242)
	domains := NewDomainService(database)

	for i := range nodes {
		require.NoError(t, domains.CreateNodeSSHDomain(ctx, &nodes[i], &user))
		require.NoError(t, domains.CreateNodeK8SAPIDomain(ctx, &nodes[i], &user))
	}

	var sshDomains []string
	require.NoError(t, database.Model(&model.DomainRegistry{}).Where("type = ?", model.DomainTypeSSH).Order("domain").Pluck("domain", &sshDomains).Error)
	require.Equal(t, []string{"beagle-241.beijing.beagle", "beagle-242.beijing.beagle"}, sshDomains)
	var clusterRecords []model.DomainRegistry
	require.NoError(t, database.Where("domain = ? AND type = ?", "kubernetes.beijing.beagle", model.DomainTypeK8SAPI).Find(&clusterRecords).Error)
	require.Len(t, clusterRecords, 2)
	require.Equal(t, agent.ID, clusterRecords[0].AgentResourceID)
	var implicitSSH int64
	require.NoError(t, database.Model(&model.DomainRegistry{}).Where("domain = ? AND type = ?", "beijing.beagle", model.DomainTypeSSH).Count(&implicitSSH).Error)
	require.Zero(t, implicitSSH)
}

func TestDomainServiceBuildsNamedProviderAndEndpointDomains(t *testing.T) {
	database := newProviderDomainTestDB(t)
	ctx := context.Background()
	provider := model.ResourceProvider{ID: "szzy", Key: "szzy", DisplayName: "深圳智翼", DomainScope: model.ProviderDomainNamed, DomainLabel: "szzy", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	user, agent, nodes := createDomainAgent(t, database, provider, "aliyun", 242)
	domains := NewDomainService(database)
	require.NoError(t, domains.CreateNodeSSHDomain(ctx, &nodes[0], &user))
	require.Empty(t, SuggestedHostDomainLabel(ctx, database, user.ID, "beagle-242"))
	require.Empty(t, SuggestedHostDomainLabel(ctx, database, user.ID, "invalid_host"))

	endpoint := model.Endpoint{ID: "endpoint-243", UserID: user.ID, Name: "beagle-243", Hostname: "beagle-243", HostDomainLabel: "beagle-243", SSHEnabled: true, SSHPort: 22001}
	require.NoError(t, database.Create(&endpoint).Error)
	endpointResource := model.TechnicalResource{
		ID: "endpoint-resource-243", ProviderID: provider.ID, Type: model.TechnicalResourceEndpoint, StableKey: "endpoint-243",
		ParentID: &agent.ID, LifecycleState: model.TechnicalResourceRegistered, HealthState: model.ResourceHealthOnline,
		CredentialRevision: 1, RuntimeUserID: user.ID, ConfigRevision: 1, RowVersion: 1,
	}
	require.NoError(t, database.Create(&endpointResource).Error)
	require.NoError(t, database.Create(&model.TechnicalResourceBinding{
		ID: "endpoint-binding-243", TechnicalResourceID: endpointResource.ID, SourceType: model.TechnicalResourceBindingLegacyEndpoint,
		SourceID: endpoint.ID, CredentialRevision: 1, Enabled: true, BoundByUserID: user.ID, Reason: "test", RowVersion: 1,
	}).Error)
	require.NoError(t, domains.CreateEndpointSSHDomain(ctx, &endpoint, &nodes[0], &user))
	require.Empty(t, SuggestedHostDomainLabel(ctx, database, user.ID, "beagle-243"))

	var records []model.DomainRegistry
	require.NoError(t, database.Where("type = ?", model.DomainTypeSSH).Order("domain").Find(&records).Error)
	require.Equal(t, []string{"beagle-242.aliyun.szzy.beagle", "beagle-243.aliyun.szzy.beagle"}, []string{records[0].Domain, records[1].Domain})
	require.Equal(t, endpoint.ID, records[1].ResourceID)

	require.ErrorIs(t, domains.UpdateEndpointHostDomainLabel(ctx, endpoint.ID, "beagle-242"), ErrHostDomainLabelExists)
	require.NoError(t, domains.UpdateNodeHostDomainLabel(ctx, nodes[0].ID, "beagle-244"))
	var oldCount, newCount int64
	require.NoError(t, database.Model(&model.DomainRegistry{}).Where("domain = ?", "beagle-242.aliyun.szzy.beagle").Count(&oldCount).Error)
	require.NoError(t, database.Model(&model.DomainRegistry{}).Where("domain = ?", "beagle-244.aliyun.szzy.beagle").Count(&newCount).Error)
	require.Zero(t, oldCount)
	require.EqualValues(t, 1, newCount)
}

func TestDomainServiceRejectsMissingStructuredOwnership(t *testing.T) {
	database := newProviderDomainTestDB(t)
	user := model.User{Name: "unbound-agent", Role: model.UserRoleAgent, SecretHash: "fixture", Enabled: true, SSHEnabled: true}
	require.NoError(t, database.Create(&user).Error)
	node := model.Node{ID: 999, UserID: user.ID, Name: "unbound", HostDomainLabel: "unbound", Type: model.NodeTypeAgent}
	require.NoError(t, database.Create(&node).Error)
	require.ErrorIs(t, NewDomainService(database).CreateNodeSSHDomain(context.Background(), &node, &user), ErrDomainOwnershipMissing)
}

func TestChangeProviderDomainLabelUsesStructuredProviderOwnership(t *testing.T) {
	database := newProviderDomainTestDB(t)
	ctx := context.Background()
	provider := model.ResourceProvider{ID: "szzy", Key: "szzy", DisplayName: "深圳智翼", DomainScope: model.ProviderDomainNamed, DomainLabel: "szzy", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1}
	user, _, nodes := createDomainAgent(t, database, provider, "aliyun", 241, 242)
	domains := NewDomainService(database)
	for i := range nodes {
		require.NoError(t, domains.CreateNodeK8SAPIDomain(ctx, &nodes[i], &user))
	}

	result, err := ChangeProviderDomainLabel(ctx, database, provider.ID, "szzy", "zhiyi")
	require.NoError(t, err)
	require.EqualValues(t, 2, result.DomainCount)
	var oldCount, newCount int64
	require.NoError(t, database.Model(&model.DomainRegistry{}).Where("domain = ?", "kubernetes.aliyun.szzy.beagle").Count(&oldCount).Error)
	require.NoError(t, database.Model(&model.DomainRegistry{}).Where("domain = ?", "kubernetes.aliyun.zhiyi.beagle").Count(&newCount).Error)
	require.Zero(t, oldCount)
	require.EqualValues(t, 2, newCount)
}
