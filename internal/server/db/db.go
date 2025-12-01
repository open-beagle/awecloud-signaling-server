package db

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
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

	// 连接数据库（默认 Warn 级别，不打印 SQL 语句）
	DB, err = gorm.Open(sqlite.Open(cfg.Path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}

	// 自动迁移
	if err := autoMigrate(); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}

	log.Println("数据库初始化成功")
	return nil
}

// autoMigrate 自动迁移数据库表
func autoMigrate() error {
	return DB.AutoMigrate(
		&model.Admin{},
		&model.Agent{},
		&model.Client{},
		&model.Group{},
		&model.GroupMember{},
		&model.STCPInstance{},
		&model.STCPAccess{},
		&model.ClientSession{},
		&model.DeviceToken{},
		&model.PortPreference{},
		&model.ConnectionAuditLog{},
	)
}

// CreateDefaultAdmin 创建默认管理员
func CreateDefaultAdmin(username, password string) error {
	// 检查是否已存在管理员
	var count int64
	if err := DB.Model(&model.Admin{}).Count(&count).Error; err != nil {
		return err
	}

	if count > 0 {
		log.Println("管理员已存在，跳过创建")
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

	log.Printf("默认管理员创建成功: %s", username)
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
