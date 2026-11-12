package db

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

type preRDPUserSchema struct {
	ID         uint64 `gorm:"primaryKey;autoIncrement"`
	Name       string `gorm:"uniqueIndex;size:100;not null"`
	Role       string `gorm:"size:20;not null;index"`
	SecretHash string `gorm:"size:255;not null"`
	Enabled    bool   `gorm:"default:true"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (preRDPUserSchema) TableName() string {
	return "user"
}

type preRDPNodeSchema struct {
	ID              uint64         `gorm:"primaryKey;autoIncrement"`
	UserID          uint64         `gorm:"not null;index;uniqueIndex:uk_node_user_type_name,priority:1"`
	Name            string         `gorm:"size:100;not null;uniqueIndex:uk_node_user_type_name,priority:3"`
	Type            model.NodeType `gorm:"size:20;not null;index;uniqueIndex:uk_node_user_type_name,priority:2"`
	HeadscaleNodeID uint64         `gorm:"index"`
	IP              string         `gorm:"size:50"`
	Version         string         `gorm:"size:50"`
	Hostname        string         `gorm:"size:100"`
	HostDomainLabel string         `gorm:"size:63;not null;default:'';index"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (preRDPNodeSchema) TableName() string {
	return "node"
}

func TestAutoMigrateUpgradesLegacyNodeTableWithS2Triggers(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		simulateLegacyTypeAlter bool
	}{
		{name: "pre_rdp_node_table", simulateLegacyTypeAlter: false},
		{name: "varchar_boolean_rdp_columns_rebuild", simulateLegacyTypeAlter: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "legacy_upgrade.db")
			database, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
			require.NoError(t, err)

			prevDB := DB
			t.Cleanup(func() {
				DB = prevDB
				if sqlDB, err := database.DB(); err == nil {
					_ = sqlDB.Close()
				}
			})

			// 模拟存量库已具备 S2/S3/S4 表与触发器，而 node 表处于升级前（或旧类型列待重建）状态
			require.NoError(t, database.AutoMigrate(
				&model.Tenant{},
				&preRDPUserSchema{},
				&preRDPNodeSchema{},
				&model.Endpoint{},
				&model.ResourceProvider{},
				&model.TechnicalResource{},
				&model.TechnicalResourceBinding{},
				&model.SupplyInventoryReceipt{},
				&model.SupplyCandidate{},
				&model.PlatformResource{},
				&model.PlatformResourceSource{},
				&model.NamespaceObservation{},
				&model.ResourceScope{},
				&model.ResourceAllocation{},
				&model.ResourceAllocationItem{},
				&model.TenantMembership{},
				&model.UserSimulationSession{},
				&model.WorkloadObservation{},
				&model.WorkloadObservationSource{},
				&model.TenantResource{},
				&model.TenantResourceSource{},
				&model.TenantResourceReviewDecision{},
				&model.TenantResourceTargetRevision{},
				&model.TenantAccessGrant{},
				&model.TenantAccessGrantEvent{},
				&model.ResourceSession{},
				&model.ResourceSessionEvent{},
				&model.ResourceSessionTermination{},
			))
			require.NoError(t, database.Create(&preRDPUserSchema{
				ID:         1,
				Name:       "edge-agent",
				Role:       "agent",
				SecretHash: "hash",
			}).Error)
			require.NoError(t, database.Create(&preRDPNodeSchema{
				ID:     10,
				UserID: 1,
				Name:   "edge-agent",
				Type:   model.NodeTypeAgent,
			}).Error)

			if tc.simulateLegacyTypeAlter {
				require.NoError(t, database.Exec("ALTER TABLE node ADD COLUMN rdp_mode VARCHAR(20) NOT NULL DEFAULT 'auto'").Error)
				require.NoError(t, database.Exec("ALTER TABLE node ADD COLUMN rdp_allow_share BOOLEAN NOT NULL DEFAULT 0").Error)
				require.NoError(t, database.Exec("ALTER TABLE node ADD COLUMN rdp_admin_disabled BOOLEAN NOT NULL DEFAULT 0").Error)
			}

			require.NoError(t, ensureProviderSupplyConstraints(database))
			require.NoError(t, ensurePlatformAllocationConstraints(database))
			require.NoError(t, ensureTenantResourceConstraints(database))

			DB = database
			require.NoError(t, autoMigrate())
			require.NoError(t, autoMigrate())

			var upgradedUser model.User
			require.NoError(t, database.First(&upgradedUser, 1).Error)
			require.Equal(t, "", upgradedUser.DomainLabel)

			var upgraded model.Node
			require.NoError(t, database.First(&upgraded, 10).Error)
			require.Equal(t, "edge-agent", upgraded.Name)
			require.Equal(t, model.DesktopRDPModeAuto, upgraded.RDPMode)
			require.False(t, upgraded.RDPAllowShare)
			require.False(t, upgraded.RDPAdminDisabled)

			// 验证 S2/S3/S4 约束触发器在迁移后全部恢复生效
			for _, trigName := range []string{
				"trg_s2_technical_resource_binding_insert",
				"trg_s3_resource_allocation_insert",
				"trg_s4_tenant_access_grant_insert",
			} {
				var triggerCount int64
				require.NoError(t, database.Table("sqlite_master").
					Where("type = 'trigger' AND name = ?", trigName).
					Count(&triggerCount).Error)
				require.EqualValues(t, 1, triggerCount, "missing trigger %s", trigName)
			}
		})
	}
}
