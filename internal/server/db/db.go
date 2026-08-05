package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	DB, err = gorm.Open(sqlite.Open(cfg.Path), &gorm.Config{
		Logger:                           NewGormLogger(gormlogger.Warn),
		IgnoreRelationshipsWhenMigrating: true,
	})
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}

	// 自动迁移
	if err := autoMigrate(); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}

	logger.Info("数据库初始化成功")
	return nil
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
		&model.ResourceProvider{},
		&model.AdminProviderMembership{},
		&model.UserTenantManagementMembership{},
		&model.UserSimulationSession{},

		// Provider 供给对象（S2，新增 Schema，业务入口仍由 Feature Flag 关闭）
		&model.TechnicalResource{},
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
		&model.WorkloadInventoryReceipt{},
		&model.WorkloadInventoryBatch{},
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

		// ZTNA 域名注册表
		&model.DomainRegistry{},

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

func ensureProviderDomainLabelSchema(database *gorm.DB) error {
	if err := database.Exec(`DROP INDEX IF EXISTS uk_active_technical_resource_binding`).Error; err != nil {
		return fmt.Errorf("remove obsolete single-node Agent constraint: %w", err)
	}
	statements := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_resource_provider_root ON resource_provider(domain_scope) WHERE domain_scope = 'root'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_resource_provider_domain_label ON resource_provider(lower(domain_label)) WHERE domain_scope = 'named'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_agent_domain_label ON technical_resource(provider_id, lower(domain_label)) WHERE type = 'agent' AND deleted_at IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_domain_registry_resource ON domain_registry(domain, resource_kind, resource_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_domain_registry_ssh_domain ON domain_registry(lower(domain)) WHERE type = 'ssh'`,
	}
	for _, statement := range statements {
		if err := database.Exec(statement).Error; err != nil {
			return fmt.Errorf("create domain namespace constraint: %w", err)
		}
	}
	return nil
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
