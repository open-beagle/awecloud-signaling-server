package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/glebarez/sqlite" // 纯 Go SQLite 驱动
	"github.com/google/uuid"
	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var DB *gorm.DB

const (
	beagleWorkspaceKey        = "beagle"
	beagleWorkspaceName       = "Beagle"
	legacyBeagleWorkspaceName = "Beagle 工作空间"
	sqliteMaxOpenConnections  = 8
	sqliteBusyTimeout         = 30 * time.Second
)

// InitDB 初始化数据库
func InitDB(cfg config.DatabaseSection) error {
	var err error

	// 创建数据库目录
	dbDir := filepath.Dir(cfg.Path)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("创建数据库目录失败: %w", err)
	}

	// 连接数据库（使用自定义 logger，统一日志格式）
	DB, err = gorm.Open(sqlite.Open(sqliteDSN(cfg.Path)), &gorm.Config{
		Logger:                           NewGormLogger(gormlogger.Warn),
		IgnoreRelationshipsWhenMigrating: true,
	})
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}
	sqlDB, err := DB.DB()
	if err != nil {
		return fmt.Errorf("获取数据库连接池失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(sqliteMaxOpenConnections)
	sqlDB.SetMaxIdleConns(sqliteMaxOpenConnections)
	sqlDB.SetConnMaxLifetime(0)

	// 自动迁移
	if err := autoMigrate(); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}

	logger.Info("数据库初始化成功")
	return nil
}

func sqliteDSN(path string) string {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return fmt.Sprintf("%s%s_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate",
		path, separator, sqliteBusyTimeout.Milliseconds())
}

// EnableTracing 启用 OpenTelemetry 追踪
func EnableTracing() error {
	if DB == nil {
		return fmt.Errorf("数据库未初始化")
	}

	// 创建独立的 TracerProvider 给 SQLite，使用不同的 service.name
	sqliteTracerProvider := otel.GetTracerProvider()

	// 使用 otelgorm 官方插件，配置独立的 TracerProvider
	if err := DB.Use(otelgorm.NewPlugin(
		otelgorm.WithTracerProvider(sqliteTracerProvider),
		otelgorm.WithoutQueryVariables(),
		otelgorm.WithAttributes(
			attribute.String("peer.service", "sqlite"),
			attribute.String("service.name", "sqlite"), // 设置独立的 service.name
		),
	)); err != nil {
		return fmt.Errorf("启用 GORM OpenTelemetry 插件失败: %w", err)
	}

	logger.Info("GORM OpenTelemetry 追踪已启用")
	return nil
}

// autoMigrate 自动迁移数据库表
func autoMigrate() error {
	if err := ensureTenantGovernanceSchema(DB); err != nil {
		return err
	}
	if err := ensureProviderDomainSchema(DB); err != nil {
		return err
	}
	if err := ensureDomainRegistrySchema(DB); err != nil {
		return err
	}
	if err := ensureUpdaterReleaseSchema(DB); err != nil {
		return err
	}
	err := DB.AutoMigrate(
		// 基础模型
		&model.Admin{},
		&model.AdminTenantMembership{},
		&model.User{},
		&model.Node{},
		&model.DeployToken{},

		// 统一资源模型
		&model.TenantMembership{},
		&model.ProviderTenantBinding{},
		&model.WorkspaceBinding{},
		&model.Resource{},
		&model.ResourceTarget{},
		&model.AccessGrant{},
		&model.DiscoveryCandidate{},
		&model.ContainerSession{},
		&model.LegacyResourceClaim{},

		// 新资源业务迁移、幂等与事务 Outbox 基础
		&model.MigrationBatch{},
		&model.MigrationSourceMapping{},
		&model.APIIdempotencyRecord{},
		&model.OutboxEvent{},
		&model.ConsumerRevision{},

		// 新资源业务目标身份与管理关系（M1-A，仅新增 Schema）
		&model.UserIdentityProfile{},
		&model.UserAuthenticationLink{},
		&model.PlatformRoleMembership{},
		&model.AdminProviderMembership{},
		&model.UserTenantManagementMembership{},
		&model.UserSimulationSession{},

		// Provider 供给对象（S2，新增 Schema，业务入口仍由 Feature Flag 关闭）
		&model.TechnicalResourceBinding{},
		&model.TechnicalResourceDeployToken{},
		&model.SupplyInventoryReceipt{},
		&model.SupplyCandidate{},
		&model.PlatformResource{},
		&model.PlatformResourceSource{},
		&model.NamespaceObservation{},
		&model.ResourceScope{},

		// Platform 资源分配（S3，新增 Schema，业务入口仍由 Feature Flag 关闭）
		&model.ResourceAllocation{},
		&model.ResourceAllocationItem{},

		// 分组模型
		&model.Group{},
		&model.GroupMember{},

		// Tenant 资源与会话（S4，新增 Schema，业务入口仍由 Feature Flag 关闭）
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

		// 服务模型
		&model.ProxyService{},
		&model.PortForward{},

		// ACL 服务授权模型
		&model.AclServiceUserPermission{},
		&model.AclServiceGroupPermission{},

		// ACL 用户授权模型
		&model.AclUserUserPermission{},
		&model.AclUserGroupPermission{},

		// ACL 分组授权模型
		&model.AclGroupUserPermission{},
		&model.AclGroupGroupPermission{},

		// ACL SSH 授权模型
		&model.AclSSHUserPermission{},
		&model.AclSSHGroupPermission{},

		// 审计日志
		&model.AuditLog{},

		// 系统配置
		&model.SystemConfig{},

		// 用户偏好
		&model.PortPreference{},
		&model.ServiceFavorite{},

		// Visitor
		&model.Visitor{},

		// Device Token
		&model.DeviceToken{},

		// Desktop 登录会话
		&model.DesktopLoginSession{},

		// ACL K8S API 授权模型
		&model.AclK8SUserPermission{},
		&model.AclK8SGroupPermission{},

		// ACL K8S Service 授权模型
		&model.AclK8SServiceUserPermission{},
		&model.AclK8SServiceGroupPermission{},

		// Endpoint 模型（统一表）
		&model.Endpoint{},

		// Updater 发布和任务模型
		&model.Release{},
		&model.Artifact{},
		&model.UpdateTask{},
		&model.UpdateEvent{},

		// 操作级审计日志
		&model.OperationAuditLog{},
	)
	if err != nil {
		// 忽略"索引已存在"的错误（SQLite 在某些情况下会报这个错误）
		if strings.Contains(err.Error(), "already exists") {
			logger.Warnf("数据库迁移警告（已忽略）: %v", err)
		} else {
			return err
		}
	}

	if err := ensureProviderSupplyConstraints(DB); err != nil {
		return err
	}
	if err := ensureProviderDomainLabelSchema(DB); err != nil {
		return err
	}
	if err := ensurePlatformAllocationConstraints(DB); err != nil {
		return err
	}
	return ensureTenantResourceConstraints(DB)
}

func ensureUpdaterReleaseSchema(database *gorm.DB) error {
	if err := database.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`DROP INDEX IF EXISTS uk_release_component`).Error; err != nil {
			return fmt.Errorf("remove obsolete single release per component constraint: %w", err)
		}
		if err := tx.Exec(`DROP INDEX IF EXISTS uk_release_component_version_commit`).Error; err != nil {
			return fmt.Errorf("remove obsolete release component/version/commit constraint: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	// Recreate this index from the current Artifact model. Older databases used
	// the same name for (release_id, os, arch), which rejects app and launcher
	// artifacts for the same platform.
	if err := database.Exec(`DROP INDEX IF EXISTS uk_artifact_release_platform`).Error; err != nil {
		return fmt.Errorf("rebuild artifact release/platform/role constraint: %w", err)
	}
	return nil
}

func ensureProviderDomainLabelSchema(database *gorm.DB) error {
	if err := database.Exec(`DROP INDEX IF EXISTS uk_active_technical_resource_binding`).Error; err != nil {
		return fmt.Errorf("remove obsolete single-node Agent constraint: %w", err)
	}
	statements := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_resource_provider_root ON resource_provider(domain_scope) WHERE domain_scope = 'root'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_resource_provider_domain_label ON resource_provider(lower(domain_label)) WHERE domain_scope = 'named'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_agent_domain_label ON technical_resource(provider_id, lower(domain_label)) WHERE type = 'agent' AND deleted_at IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_domain_registry_resource ON domain_registry(domain, resource_kind, resource_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_domain_registry_ssh_domain ON domain_registry(lower(domain)) WHERE type = 'ssh' AND provider_id <> '' AND agent_resource_id <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_domain_registry_provider_id ON domain_registry(provider_id)`,
		`CREATE INDEX IF NOT EXISTS idx_domain_registry_agent_resource_id ON domain_registry(agent_resource_id)`,
		`CREATE INDEX IF NOT EXISTS idx_domain_registry_resource_kind ON domain_registry(resource_kind)`,
		`CREATE INDEX IF NOT EXISTS idx_domain_registry_resource_id ON domain_registry(resource_id)`,
	}
	for _, statement := range statements {
		if err := database.Exec(statement).Error; err != nil {
			return fmt.Errorf("create domain namespace constraint: %w", err)
		}
	}
	return nil
}

func ensureDomainRegistrySchema(database *gorm.DB) error {
	migrator := database.Migrator()
	if !migrator.HasTable(&model.DomainRegistry{}) {
		if err := database.AutoMigrate(&model.DomainRegistry{}); err != nil {
			return fmt.Errorf("create domain registry schema: %w", err)
		}
		return nil
	}

	return database.Transaction(func(tx *gorm.DB) error {
		for _, column := range []string{"provider_id", "agent_resource_id", "resource_kind", "resource_id"} {
			hasColumn, err := hasSQLiteColumn(tx, "domain_registry", column)
			if err != nil {
				return err
			}
			if hasColumn {
				continue
			}
			if err := tx.Exec("ALTER TABLE domain_registry ADD COLUMN " + column + " TEXT NOT NULL DEFAULT ''").Error; err != nil {
				return fmt.Errorf("add domain registry ownership column %s: %w", column, err)
			}
		}

		statements := []string{
			`UPDATE domain_registry SET
				provider_id = COALESCE((SELECT resource.provider_id FROM technical_resource_binding binding
					JOIN technical_resource resource ON resource.id = binding.technical_resource_id AND resource.type = 'agent'
					WHERE binding.source_type = 'legacy_node' AND binding.source_id = CAST(domain_registry.node_id AS TEXT) AND binding.enabled = 1 LIMIT 1), ''),
				agent_resource_id = COALESCE((SELECT resource.id FROM technical_resource_binding binding
					JOIN technical_resource resource ON resource.id = binding.technical_resource_id AND resource.type = 'agent'
					WHERE binding.source_type = 'legacy_node' AND binding.source_id = CAST(domain_registry.node_id AS TEXT) AND binding.enabled = 1 LIMIT 1), '')
			WHERE COALESCE(endpoint_id, '') = ''
				AND (provider_id = '' OR agent_resource_id = '' OR resource_kind = '' OR resource_id = '')`,
			`UPDATE domain_registry SET
				provider_id = COALESCE((SELECT agent.provider_id FROM endpoint legacy_endpoint
					JOIN technical_resource_binding endpoint_binding ON endpoint_binding.source_type = 'legacy_endpoint'
						AND endpoint_binding.source_id = legacy_endpoint.id AND endpoint_binding.enabled = 1
					JOIN technical_resource endpoint_resource ON endpoint_resource.id = endpoint_binding.technical_resource_id AND endpoint_resource.type = 'endpoint'
					JOIN technical_resource agent ON agent.id = endpoint_resource.parent_id AND agent.type = 'agent'
					WHERE legacy_endpoint.id = domain_registry.endpoint_id OR legacy_endpoint.name = domain_registry.endpoint_id LIMIT 1), ''),
				agent_resource_id = COALESCE((SELECT agent.id FROM endpoint legacy_endpoint
					JOIN technical_resource_binding endpoint_binding ON endpoint_binding.source_type = 'legacy_endpoint'
						AND endpoint_binding.source_id = legacy_endpoint.id AND endpoint_binding.enabled = 1
					JOIN technical_resource endpoint_resource ON endpoint_resource.id = endpoint_binding.technical_resource_id AND endpoint_resource.type = 'endpoint'
					JOIN technical_resource agent ON agent.id = endpoint_resource.parent_id AND agent.type = 'agent'
					WHERE legacy_endpoint.id = domain_registry.endpoint_id OR legacy_endpoint.name = domain_registry.endpoint_id LIMIT 1), ''),
				resource_id = COALESCE((SELECT id FROM endpoint WHERE id = domain_registry.endpoint_id OR name = domain_registry.endpoint_id LIMIT 1), domain_registry.endpoint_id),
				resource_kind = CASE
					WHEN type = 'ssh' THEN 'endpoint'
					WHEN type = 'k8sapi' THEN 'kubernetes'
					ELSE 'service' END
			WHERE COALESCE(endpoint_id, '') <> ''
				AND (provider_id = '' OR agent_resource_id = '' OR resource_kind = '' OR resource_id = '')`,
			`UPDATE domain_registry SET resource_kind = CASE
				WHEN type = 'ssh' THEN 'node'
				WHEN type = 'k8sapi' THEN 'kubernetes'
				ELSE 'service' END,
				resource_id = CAST(node_id AS TEXT)
			WHERE COALESCE(endpoint_id, '') = ''
				AND (resource_kind = '' OR resource_id = '')`,
		}
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("backfill domain registry ownership: %w", err)
			}
		}
		return nil
	})
}

func hasSQLiteColumn(database *gorm.DB, table, column string) (bool, error) {
	var columns []struct {
		Name string
	}
	if err := database.Raw("PRAGMA table_info(" + table + ")").Scan(&columns).Error; err != nil {
		return false, fmt.Errorf("inspect SQLite table %s: %w", table, err)
	}
	for _, candidate := range columns {
		if candidate.Name == column {
			return true, nil
		}
	}
	return false, nil
}

type legacyAgentDomainRow struct {
	ID            string
	ProviderID    string
	StableKey     string
	DomainLabel   string
	RuntimeName   string
	BoundNodeName string
}

var invalidDomainLabelCharacters = regexp.MustCompile(`[^a-z0-9-]+`)

// ensureProviderDomainSchema keeps upgrades additive. Rebuilding either table
// breaks existing SQLite triggers while the referenced table is temporarily
// absent.
func ensureProviderDomainSchema(database *gorm.DB) error {
	migrator := database.Migrator()
	if !migrator.HasTable(&model.ResourceProvider{}) {
		if err := database.AutoMigrate(&model.ResourceProvider{}, &model.TechnicalResource{}); err != nil {
			return fmt.Errorf("create Provider domain schema: %w", err)
		}
		return nil
	}

	return database.Transaction(func(tx *gorm.DB) error {
		txMigrator := tx.Migrator()
		if !txMigrator.HasColumn(&model.ResourceProvider{}, "domain_label") {
			if err := tx.Exec(`ALTER TABLE resource_provider ADD COLUMN domain_label TEXT NOT NULL DEFAULT ''`).Error; err != nil {
				return fmt.Errorf("add Provider domain label: %w", err)
			}
		}
		if !txMigrator.HasColumn(&model.ResourceProvider{}, "domain_scope") {
			if err := tx.Exec(`ALTER TABLE resource_provider ADD COLUMN domain_scope TEXT NOT NULL DEFAULT 'named'`).Error; err != nil {
				return fmt.Errorf("add Provider domain scope: %w", err)
			}
		}
		if err := tx.Exec(`UPDATE resource_provider SET domain_scope = 'root', domain_label = '' WHERE lower(key) = 'beagle'`).Error; err != nil {
			return fmt.Errorf("set root Provider domain scope: %w", err)
		}
		if err := tx.Exec(`UPDATE resource_provider SET domain_scope = 'named', domain_label = lower(key) WHERE lower(key) <> 'beagle' AND domain_label = ''`).Error; err != nil {
			return fmt.Errorf("backfill named Provider domain scope: %w", err)
		}

		if !txMigrator.HasTable(&model.TechnicalResource{}) {
			if err := tx.AutoMigrate(&model.TechnicalResource{}); err != nil {
				return fmt.Errorf("create technical resource domain schema: %w", err)
			}
			return nil
		}
		if !txMigrator.HasColumn(&model.TechnicalResource{}, "domain_label") {
			if err := tx.Exec(`ALTER TABLE technical_resource ADD COLUMN domain_label TEXT NOT NULL DEFAULT ''`).Error; err != nil {
				return fmt.Errorf("add Agent domain label: %w", err)
			}
		}
		return backfillAgentDomainLabels(tx)
	})
}

func backfillAgentDomainLabels(database *gorm.DB) error {
	var agents []legacyAgentDomainRow
	err := database.Table("technical_resource AS resource").
		Select(`resource.id, resource.provider_id, resource.stable_key, resource.domain_label,
			COALESCE(runtime_user.name, '') AS runtime_name,
			COALESCE(bound_node.name, '') AS bound_node_name`).
		Joins("LEFT JOIN user AS runtime_user ON runtime_user.id = resource.runtime_user_id").
		Joins(`LEFT JOIN technical_resource_binding AS binding
			ON binding.technical_resource_id = resource.id AND binding.source_type = 'legacy_node' AND binding.enabled = 1`).
		Joins("LEFT JOIN node AS bound_node ON CAST(bound_node.id AS TEXT) = binding.source_id").
		Where("resource.type = ? AND resource.deleted_at IS NULL", model.TechnicalResourceAgent).
		Order("resource.provider_id, resource.created_at, resource.id").
		Scan(&agents).Error
	if err != nil {
		return fmt.Errorf("list historical Agent domain labels: %w", err)
	}

	used := make(map[string]struct{}, len(agents))
	for _, agent := range agents {
		label := normalizeLegacyDomainLabel(agent.DomainLabel)
		if label == "" {
			label = normalizeLegacyDomainLabel(agent.RuntimeName)
		}
		if label == "" {
			label = normalizeLegacyDomainLabel(agent.BoundNodeName)
		}
		if label == "" {
			label = normalizeLegacyDomainLabel(agent.StableKey)
		}
		if label == "" {
			label = "agent"
		}
		label = uniqueLegacyDomainLabel(used, agent.ProviderID, label)
		if label == agent.DomainLabel {
			continue
		}
		if err := database.Model(&model.TechnicalResource{}).Where("id = ?", agent.ID).Update("domain_label", label).Error; err != nil {
			return fmt.Errorf("backfill Agent domain label %s: %w", agent.ID, err)
		}
	}
	return nil
}

func normalizeLegacyDomainLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = invalidDomainLabelCharacters.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if len(value) > 63 {
		value = strings.TrimRight(value[:63], "-")
	}
	return value
}

func uniqueLegacyDomainLabel(used map[string]struct{}, providerID, label string) string {
	base := label
	for suffix := 1; ; suffix++ {
		key := providerID + "\x00" + label
		if _, exists := used[key]; !exists {
			used[key] = struct{}{}
			return label
		}
		tail := fmt.Sprintf("-%d", suffix)
		prefixLength := 63 - len(tail)
		label = strings.TrimRight(base[:min(len(base), prefixLength)], "-") + tail
	}
}

// ensureTenantGovernanceSchema owns Tenant schema migration. Existing SQLite
// installations can have triggers in other tables that reference tenant; asking
// GORM to rebuild that table makes SQLite reject the migration while those
// triggers temporarily point at a missing table. New installations still use
// AutoMigrate, while existing installations receive additive columns only.
func ensureTenantGovernanceSchema(database *gorm.DB) error {
	migrator := database.Migrator()
	if !migrator.HasTable(&model.Tenant{}) {
		if err := database.AutoMigrate(&model.Tenant{}); err != nil {
			return fmt.Errorf("create tenant governance schema: %w", err)
		}
		return nil
	}
	for _, column := range []string{"revision", "row_version"} {
		if migrator.HasColumn(&model.Tenant{}, column) {
			continue
		}
		if err := database.Exec("ALTER TABLE tenant ADD COLUMN " + column + " INTEGER NOT NULL DEFAULT 1").Error; err != nil {
			return fmt.Errorf("add tenant governance column %s: %w", column, err)
		}
	}
	return nil
}

// CreateDefaultAdmin 创建默认管理员
func CreateDefaultAdmin(username, password string) error {
	// 初始化操作不需要 trace，使用 background context
	ctx := context.Background()

	// 检查是否已存在管理员
	var count int64
	if err := DB.WithContext(ctx).Model(&model.Admin{}).Count(&count).Error; err != nil {
		return err
	}

	if count > 0 {
		logger.Info("管理员已存在，跳过创建")
		return nil
	}

	// 生成密码哈希
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("生成密码哈希失败: %w", err)
	}

	// 创建管理员
	admin := &model.Admin{
		Username:     username,
		PasswordHash: string(hash),
		Enabled:      true,
	}

	if err := DB.WithContext(ctx).Create(admin).Error; err != nil {
		return fmt.Errorf("创建管理员失败: %w", err)
	}

	logger.Infof("默认管理员创建成功: %s", username)
	return nil
}

// EnsureBeagleWorkspace gives legacy, previously unscoped Desktop users an
// explicit Tenant boundary without changing users already assigned elsewhere.
func EnsureBeagleWorkspace(adminUsername string) error {
	ctx := context.Background()
	migratedUsers := int64(0)

	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var admin model.Admin
		if err := tx.Where("username = ?", adminUsername).First(&admin).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				logger.Warnf("默认管理员 %s 不存在，跳过 Beagle 默认租户初始化", adminUsername)
				return nil
			}
			return fmt.Errorf("查询默认管理员失败: %w", err)
		}

		var tenant model.Tenant
		if err := tx.Where("key = ?", beagleWorkspaceKey).First(&tenant).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("查询 Beagle 默认租户失败: %w", err)
			}
			tenant = model.Tenant{
				ID:     uuid.NewString(),
				Key:    beagleWorkspaceKey,
				Name:   beagleWorkspaceName,
				Status: model.TenantStatusActive,
			}
			if err := tx.Create(&tenant).Error; err != nil {
				return fmt.Errorf("创建 Beagle 默认租户失败: %w", err)
			}
		} else if tenant.Name == legacyBeagleWorkspaceName {
			if err := tx.Model(&tenant).Update("name", beagleWorkspaceName).Error; err != nil {
				return fmt.Errorf("更新 Beagle 默认租户名称失败: %w", err)
			}
			tenant.Name = beagleWorkspaceName
		}

		var adminMembership model.AdminTenantMembership
		err := tx.Where("admin_id = ? AND tenant_id = ?", admin.ID, tenant.ID).First(&adminMembership).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			adminMembership = model.AdminTenantMembership{
				AdminID:            admin.ID,
				TenantID:           tenant.ID,
				Role:               string(model.TenantManagementRoleAdmin),
				Enabled:            true,
				PermissionRevision: 1,
			}
			if err := tx.Create(&adminMembership).Error; err != nil {
				return fmt.Errorf("授权默认管理员管理 Beagle 默认租户失败: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("查询 Beagle 默认租户管理员授权失败: %w", err)
		}

		var users []model.User
		if err := tx.Table("user AS scoped_user").
			Select("scoped_user.*").
			Joins("LEFT JOIN tenant_membership AS membership ON membership.user_id = scoped_user.id").
			Where("scoped_user.role = ? AND membership.id IS NULL", model.UserRoleClient).
			Where(`NOT EXISTS (
				SELECT 1 FROM user_authentication_link AS authentication_link
				WHERE authentication_link.user_id = scoped_user.id AND authentication_link.provider_type = ?
			)`, model.AuthenticationProviderLegacyAdmin).
			Find(&users).Error; err != nil {
			return fmt.Errorf("查询未归属 Desktop 用户失败: %w", err)
		}

		for _, user := range users {
			membership := model.TenantMembership{
				TenantID: tenant.ID,
				UserID:   user.ID,
				Role:     "member",
				Enabled:  true,
			}
			if err := tx.Create(&membership).Error; err != nil {
				return fmt.Errorf("将用户 %d 加入 Beagle 默认租户失败: %w", user.ID, err)
			}
			migratedUsers++
		}

		return nil
	})
	if err != nil {
		return err
	}

	logger.Infof("Beagle 默认租户初始化完成: admin=%s, migrated_users=%d", adminUsername, migratedUsers)
	return nil
}

// Ping 检查数据库连接是否正常
func Ping() error {
	sqlDB, err := DB.DB()
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}
	return sqlDB.Ping()
}

// WithContext 返回带 context 的数据库实例，用于 OpenTelemetry 追踪
func WithContext(ctx context.Context) *gorm.DB {
	return DB.WithContext(ctx)
}
