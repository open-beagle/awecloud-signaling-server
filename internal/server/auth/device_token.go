package auth

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

const (
	// DeviceTokenExpireDays Device Token有效期（天）
	DeviceTokenExpireDays = 7
)

// GenerateDeviceToken 生成新的Device Token
func GenerateDeviceToken() string {
	return "dt_" + uuid.New().String()
}

// CreateDeviceToken 创建Device Token记录
func CreateDeviceToken(db *gorm.DB, clientID int64, deviceInfo DeviceInfo) (*model.DeviceToken, error) {
	// 生成设备指纹
	fingerprint := GenerateFingerprint(deviceInfo)

	// 将设备信息转换为JSON
	deviceInfoJSON, err := DeviceInfoToJSON(deviceInfo)
	if err != nil {
		return nil, err
	}

	// 生成Token
	token := GenerateDeviceToken()

	// 计算过期时间
	expiresAt := time.Now().Add(DeviceTokenExpireDays * 24 * time.Hour)

	// 创建记录
	deviceToken := &model.DeviceToken{
		ClientID:          clientID,
		DeviceToken:       token,
		DeviceFingerprint: fingerprint,
		DeviceInfo:        deviceInfoJSON,
		LastUsedAt:        time.Now(),
		ExpiresAt:         expiresAt,
		Revoked:           false,
	}

	if err := db.Create(deviceToken).Error; err != nil {
		return nil, fmt.Errorf("创建Device Token失败: %w", err)
	}

	return deviceToken, nil
}

// ValidateDeviceToken 验证Device Token
func ValidateDeviceToken(db *gorm.DB, clientID int64, token string, deviceInfo DeviceInfo) (*model.DeviceToken, error) {
	var deviceToken model.DeviceToken

	// 查询Token
	if err := db.Where("client_id = ? AND device_token = ?", clientID, token).First(&deviceToken).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("Device Token不存在")
		}
		return nil, fmt.Errorf("查询Device Token失败: %w", err)
	}

	// 检查是否已撤销
	if deviceToken.Revoked {
		return nil, fmt.Errorf("Device Token已被撤销")
	}

	// 检查是否过期
	if time.Now().After(deviceToken.ExpiresAt) {
		return nil, fmt.Errorf("Device Token已过期")
	}

	// 验证设备指纹
	if !ValidateFingerprint(deviceInfo, deviceToken.DeviceFingerprint) {
		return nil, fmt.Errorf("设备指纹不匹配")
	}

	// 更新最后使用时间
	deviceToken.LastUsedAt = time.Now()
	if err := db.Save(&deviceToken).Error; err != nil {
		return nil, fmt.Errorf("更新Device Token失败: %w", err)
	}

	return &deviceToken, nil
}

// RevokeDeviceToken 撤销Device Token
func RevokeDeviceToken(db *gorm.DB, clientID int64, token string) error {
	result := db.Model(&model.DeviceToken{}).
		Where("client_id = ? AND device_token = ?", clientID, token).
		Update("revoked", true)

	if result.Error != nil {
		return fmt.Errorf("撤销Device Token失败: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("Device Token不存在")
	}

	return nil
}

// DeleteDeviceToken 删除Device Token记录
func DeleteDeviceToken(db *gorm.DB, clientID int64, token string) error {
	// 只能删除已撤销或已过期的Token
	result := db.Where("client_id = ? AND device_token = ? AND (revoked = ? OR expires_at < ?)",
		clientID, token, true, time.Now()).
		Delete(&model.DeviceToken{})

	if result.Error != nil {
		return fmt.Errorf("删除Device Token失败: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("无法删除活跃的Device Token，请先撤销")
	}

	return nil
}

// ListDeviceTokens 列出用户的所有Device Token
func ListDeviceTokens(db *gorm.DB, clientID int64) ([]model.DeviceToken, error) {
	var tokens []model.DeviceToken

	if err := db.Where("client_id = ?", clientID).
		Order("created_at DESC").
		Find(&tokens).Error; err != nil {
		return nil, fmt.Errorf("查询Device Token列表失败: %w", err)
	}

	return tokens, nil
}

// CleanupExpiredTokens 清理过期的Device Token
func CleanupExpiredTokens(db *gorm.DB) error {
	result := db.Where("expires_at < ?", time.Now()).Delete(&model.DeviceToken{})
	if result.Error != nil {
		return fmt.Errorf("清理过期Token失败: %w", result.Error)
	}
	return nil
}
