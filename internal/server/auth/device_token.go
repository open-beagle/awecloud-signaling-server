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

// CreateDeviceToken 创建或更新Device Token记录
// 如果该设备已存在记录，则生成新token并更新；否则创建新记录
func CreateDeviceToken(db *gorm.DB, clientID int64, deviceInfo DeviceInfo) (*model.DeviceToken, error) {
	// 生成设备指纹
	fingerprint := GenerateFingerprint(deviceInfo)

	// 将设备信息转换为JSON
	deviceInfoJSON, err := DeviceInfoToJSON(deviceInfo)
	if err != nil {
		return nil, err
	}

	// 计算过期时间
	expiresAt := time.Now().Add(DeviceTokenExpireDays * 24 * time.Hour)

	// 查找是否已存在该设备的记录
	var existingToken model.DeviceToken
	err = db.Where("client_id = ? AND device_fingerprint = ?", clientID, fingerprint).
		First(&existingToken).Error

	if err == nil {
		// 设备已存在，生成新token并更新记录
		newToken := GenerateDeviceToken()
		existingToken.DeviceToken = newToken
		existingToken.DeviceInfo = deviceInfoJSON
		existingToken.LastUsedAt = time.Now()
		existingToken.ExpiresAt = expiresAt
		existingToken.Revoked = false

		if err := db.Save(&existingToken).Error; err != nil {
			return nil, fmt.Errorf("更新Device Token失败: %w", err)
		}

		return &existingToken, nil
	}

	if err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("查询Device Token失败: %w", err)
	}

	// 设备不存在，创建新记录
	token := GenerateDeviceToken()

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
	// 直接删除Device Token（不需要先撤销）
	result := db.Where("client_id = ? AND device_token = ?", clientID, token).
		Delete(&model.DeviceToken{})

	if result.Error != nil {
		return fmt.Errorf("删除Device Token失败: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("Device Token不存在")
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
