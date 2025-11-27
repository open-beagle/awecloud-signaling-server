package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// DeviceInfo 设备信息
type DeviceInfo struct {
	OS        string `json:"os"`         // 操作系统：windows/linux/darwin
	OSVersion string `json:"os_version"` // 系统版本
	Arch      string `json:"arch"`       // 架构：amd64/arm64
	CPUModel  string `json:"cpu_model"`  // CPU型号
	MachineID string `json:"machine_id"` // 机器ID
	Hostname  string `json:"hostname"`   // 主机名
}

// GenerateFingerprint 生成设备指纹
// 使用设备的静态信息生成SHA256哈希
func GenerateFingerprint(info DeviceInfo) string {
	// 拼接设备信息
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s",
		info.OS,
		info.OSVersion,
		info.Arch,
		info.CPUModel,
		info.MachineID,
		info.Hostname,
	)

	// 生成SHA256哈希
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// ValidateFingerprint 验证设备指纹是否匹配
func ValidateFingerprint(info DeviceInfo, expectedFingerprint string) bool {
	actualFingerprint := GenerateFingerprint(info)
	return actualFingerprint == expectedFingerprint
}

// DeviceInfoToJSON 将设备信息转换为JSON字符串
func DeviceInfoToJSON(info DeviceInfo) (string, error) {
	data, err := json.Marshal(info)
	if err != nil {
		return "", fmt.Errorf("序列化设备信息失败: %w", err)
	}
	return string(data), nil
}

// DeviceInfoFromJSON 从JSON字符串解析设备信息
func DeviceInfoFromJSON(jsonStr string) (DeviceInfo, error) {
	var info DeviceInfo
	if err := json.Unmarshal([]byte(jsonStr), &info); err != nil {
		return DeviceInfo{}, fmt.Errorf("解析设备信息失败: %w", err)
	}
	return info, nil
}
