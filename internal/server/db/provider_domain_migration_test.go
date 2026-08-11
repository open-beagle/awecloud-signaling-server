package db

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestEnsureProviderDomainSchemaUpgradesTriggeredSQLiteTablesAdditively(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.Exec(`CREATE TABLE resource_provider (
		id TEXT PRIMARY KEY, key TEXT NOT NULL, display_name TEXT NOT NULL, domain_label TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'active', revision INTEGER NOT NULL DEFAULT 1, row_version INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME, updated_at DATETIME
	)`).Error)
	require.NoError(t, database.Exec(`CREATE TABLE technical_resource (
		id TEXT PRIMARY KEY, provider_id TEXT NOT NULL, type TEXT NOT NULL, stable_key TEXT NOT NULL, parent_id TEXT,
		lifecycle_state TEXT NOT NULL DEFAULT 'pending', health_state TEXT NOT NULL DEFAULT 'unknown', credential_revision INTEGER NOT NULL DEFAULT 1,
		source_epoch TEXT, last_sequence INTEGER NOT NULL DEFAULT 0, last_payload_hash TEXT, last_received_at DATETIME, lease_expires_at DATETIME,
		config_revision INTEGER NOT NULL DEFAULT 1, row_version INTEGER NOT NULL DEFAULT 1,
		runtime_user_id INTEGER NOT NULL DEFAULT 0, deleted_at DATETIME, created_at DATETIME, updated_at DATETIME
	)`).Error)
	require.NoError(t, database.Exec(`CREATE TABLE user (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`).Error)
	require.NoError(t, database.Exec(`CREATE TABLE node (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`).Error)
	require.NoError(t, database.Exec(`CREATE TABLE endpoint (id TEXT PRIMARY KEY, name TEXT NOT NULL)`).Error)
	require.NoError(t, database.Exec(`CREATE TABLE technical_resource_binding (
		technical_resource_id TEXT NOT NULL, source_type TEXT NOT NULL, source_id TEXT NOT NULL, enabled NUMERIC NOT NULL DEFAULT 1
	)`).Error)
	require.NoError(t, database.Exec(`CREATE TABLE domain_registry (
		id INTEGER PRIMARY KEY, domain TEXT NOT NULL, type TEXT NOT NULL, user_id INTEGER NOT NULL,
		node_id INTEGER, endpoint_id TEXT, target_ip TEXT, target_port INTEGER, namespace TEXT, service_name TEXT,
		status TEXT NOT NULL DEFAULT 'online', created_at DATETIME, updated_at DATETIME, service_ports TEXT, ssh_users TEXT
	)`).Error)
	require.NoError(t, database.Exec(`CREATE TRIGGER trg_s2_technical_resource_insert BEFORE INSERT ON technical_resource
		BEGIN SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM resource_provider WHERE id = NEW.provider_id)
		THEN RAISE(ABORT, 'S2_PROVIDER_NOT_FOUND') END; END`).Error)
	require.NoError(t, database.Exec(`INSERT INTO resource_provider (id, key, display_name, domain_label) VALUES
		('provider-root', 'beagle', 'Beijing Beagle', 'beijing'), ('provider-named', 'szzy', 'Shenzhen Zhiyi', 'szzy')`).Error)
	require.NoError(t, database.Exec(`INSERT INTO user (id, name) VALUES (7, 'beijing')`).Error)
	require.NoError(t, database.Exec(`INSERT INTO node (id, name) VALUES (48, 'beagle-242')`).Error)
	require.NoError(t, database.Exec(`INSERT INTO technical_resource
		(id, provider_id, type, stable_key, lifecycle_state, runtime_user_id, created_at) VALUES
		('agent-a', 'provider-root', 'agent', 'legacy-node:48', 'registered', 7, CURRENT_TIMESTAMP),
		('agent-b', 'provider-root', 'agent', 'legacy-node:49', 'registered', 7, CURRENT_TIMESTAMP),
		('endpoint-a', 'provider-root', 'endpoint', 'legacy-endpoint:1', 'registered', 7, CURRENT_TIMESTAMP)`).Error)
	require.NoError(t, database.Exec(`INSERT INTO technical_resource_binding
		(technical_resource_id, source_type, source_id, enabled) VALUES ('agent-a', 'legacy_node', '48', 1)`).Error)
	require.NoError(t, database.Exec(`INSERT INTO domain_registry
		(id, domain, type, user_id, node_id, status) VALUES (1, 'beagle-242.beijing.beagle', 'ssh', 7, 48, 'online')`).Error)

	require.NoError(t, ensureProviderDomainSchema(database))
	require.NoError(t, ensureDomainRegistrySchema(database))
	require.True(t, database.Migrator().HasColumn(&model.ResourceProvider{}, "domain_scope"))
	require.True(t, database.Migrator().HasColumn(&model.TechnicalResource{}, "domain_label"))

	var provider model.ResourceProvider
	require.NoError(t, database.First(&provider, "id = ?", "provider-root").Error)
	require.Equal(t, model.ProviderDomainRoot, provider.DomainScope)
	require.Empty(t, provider.DomainLabel)

	var labels []string
	require.NoError(t, database.Table("technical_resource").Where("type = 'agent'").Order("id").Pluck("domain_label", &labels).Error)
	require.Equal(t, []string{"beijing", "beijing-1"}, labels)
	var domain model.DomainRegistry
	require.NoError(t, database.First(&domain, 1).Error)
	require.Equal(t, "provider-root", domain.ProviderID)
	require.Equal(t, "agent-a", domain.AgentResourceID)
	require.Equal(t, model.DomainResourceNode, domain.ResourceKind)
	require.Equal(t, "48", domain.ResourceID)

	var triggerCount int64
	require.NoError(t, database.Table("sqlite_master").Where("type = 'trigger' AND name = ?", "trg_s2_technical_resource_insert").Count(&triggerCount).Error)
	require.EqualValues(t, 1, triggerCount)
	require.Error(t, database.Exec(`INSERT INTO technical_resource (id, provider_id, type, stable_key) VALUES ('invalid', 'missing', 'agent', 'invalid')`).Error)
}

func TestEnsureProviderDomainLabelSchemaEnforcesExplicitGlobalUniqueness(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&model.ResourceProvider{}, &model.TechnicalResource{}, &model.DomainRegistry{},
	))
	require.NoError(t, database.Create(&[]model.ResourceProvider{
		{ID: "provider-root", Key: "beagle", DisplayName: "Root Provider", DomainScope: model.ProviderDomainRoot, Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1},
		{ID: "provider-a", Key: "beagle-bj", DisplayName: "Provider A", DomainScope: model.ProviderDomainNamed, DomainLabel: "beagle-bj", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1},
		{ID: "provider-b", Key: "zhiyi-sz", DisplayName: "Provider B", DomainScope: model.ProviderDomainNamed, DomainLabel: "zhiyi-sz", Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1},
	}).Error)
	require.NoError(t, ensureProviderDomainLabelSchema(database))
	require.Error(t, database.Model(&model.ResourceProvider{}).Where("id = ?", "provider-b").Update("domain_label", "BEAGLE-BJ").Error)
	require.Error(t, database.Create(&model.ResourceProvider{
		ID: "provider-root-b", Key: "root-b", DisplayName: "Second Root", DomainScope: model.ProviderDomainRoot,
		Status: model.ProviderStatusActive, Revision: 1, RowVersion: 1,
	}).Error)
}
