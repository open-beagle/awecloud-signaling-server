package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
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
// 只在有父 span 时才创建子 span，避免孤立 trace
func EnableTracing() error {
	if DB == nil {
		return fmt.Errorf("数据库未初始化")
	}

	// 添加 OpenTelemetry GORM 插件
	if err := DB.Use(otelgorm.NewPlugin()); err != nil {
		return fmt.Errorf("启用 GORM OpenTelemetry 插件失败: %w", err)
	}

	// 注册回调，在没有父 span 时跳过 trace
	// 这通过检查 context 中是否有有效的 span 来实现
	DB.Callback().Query().Before("otelgorm:before_query").Register("skip_orphan_trace", skipOrphanTrace)
	DB.Callback().Create().Before("otelgorm:before_create").Register("skip_orphan_trace", skipOrphanTrace)
	DB.Callback().Update().Before("otelgorm:before_update").Register("skip_orphan_trace", skipOrphanTrace)
	DB.Callback().Delete().Before("otelgorm:before_delete").Register("skip_orphan_trace", skipOrphanTrace)
	DB.Callback().Row().Before("otelgorm:before_row").Register("skip_orphan_trace", skipOrphanTrace)
	DB.Callback().Raw().Before("otelgorm:before_raw").Register("skip_orphan_trace", skipOrphanTrace)

	logger.Info("GORM OpenTelemetry 追踪已启用")
	return nil
}

// skipOrphanTrace 检查是否有父 span，没有则设置标记跳过 trace
func skipOrphanTrace(db *gorm.DB) {
	ctx := db.Statement.Context
	if ctx == nil {
		return
	}

	// 检查 context 中是否有有效的 span
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		// 没有有效的父 span，使用一个干净的 context 替换
		// 这样 otelgorm 就不会创建孤立的 span
		db.Statement.Context = context.Background()
	}
}

// autoMigrate 自动迁移数据库表
func autoMigrate() error {
	err := DB.AutoMigrate(
		// 基础模型
		&model.Admin{},
		&model.User{},
		&model.Node{},

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
	// 检查是否已存在管理员
	var count int64
	if err := DB.Model(&model.Admin{}).Count(&count).Error; err != nil {
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
	}

	if err := DB.Create(admin).Error; err != nil {
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
