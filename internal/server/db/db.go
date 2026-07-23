package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebarez/sqlite" // 纯 Go SQLite 驱动
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
		Logger: NewGormLogger(gormlogger.Warn),
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
	err := DB.AutoMigrate(
		// 基础模型
		&model.Admin{},
		&model.AdminTenantMembership{},
		&model.User{},
		&model.Node{},

		// 统一资源模型
		&model.Tenant{},
		&model.TenantMembership{},
		&model.ProviderTenantBinding{},
		&model.WorkspaceBinding{},
		&model.Resource{},
		&model.ResourceTarget{},
		&model.AccessGrant{},
		&model.DiscoveryCandidate{},
		&model.ContainerSession{},
		&model.LegacyResourceClaim{},

		// 分组模型
		&model.Group{},
		&model.GroupMember{},

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

		// 部署 Token（统一）
		&model.DeployToken{},

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
			return nil
		}
		return err
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
