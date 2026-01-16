package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// autoMigrate 自动迁移数据库表
func autoMigrate() error {
	err := DB.AutoMigrate(
		// 基础模型
		&model.Admin{},
		&model.Agent{},
		&model.Client{},
		&model.Desktop{},

		// 分组模型
		&model.ClientGroup{},
		&model.ClientGroupMember{},
		&model.AgentGroup{},
		&model.AgentGroupMember{},

		// 服务模型
		&model.ProxyService{},
		&model.PortForward{},

		// 服务授权模型
		&model.ServiceClientPermission{},
		&model.ServiceClientGroupPermission{},
		&model.ServiceAgentPermission{},
		&model.ServiceAgentGroupPermission{},

		// Agent 级别授权模型
		&model.AgentClientPermission{},
		&model.AgentClientGroupPermission{},
		&model.AgentAgentPermission{},
		&model.AgentAgentGroupPermission{},

		// SSH 授权模型
		&model.SSHClientPermission{},
		&model.SSHClientGroupPermission{},

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
