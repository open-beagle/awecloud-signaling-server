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
	err := DB.AutoMigrate(
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
		&model.ServiceFavorite{},
		&model.ConnectionAuditLog{},
		&model.SystemConfig{},
		&model.SystemSettings{},
		&model.TCPService{},
		&model.TCPServiceAccessLog{},
	)
	if err != nil {
		return err
	}

	// 初始化TCP服务配置
	return initTCPServiceConfig()
}

// initTCPServiceConfig 初始化TCP服务配置
func initTCPServiceConfig() error {
	configs := []model.SystemSettings{
		{
			SettingKey:   "tcp_service_port_start",
			SettingValue: "9000",
			Description:  "TCP实例端口起始值",
		},
		{
			SettingKey:   "tcp_service_max_per_agent",
			SettingValue: "50",
			Description:  "每个Agent最多创建的TCP服务数量",
		},
	}

	for _, cfg := range configs {
		var count int64
		DB.Model(&model.SystemSettings{}).Where("setting_key = ?", cfg.SettingKey).Count(&count)
		if count == 0 {
			if err := DB.Create(&cfg).Error; err != nil {
				return fmt.Errorf("初始化配置 %s 失败: %w", cfg.SettingKey, err)
			}
		}
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
