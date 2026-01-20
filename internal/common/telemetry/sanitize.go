package telemetry

import (
	"encoding/json"
	"strings"
)

const (
	// MaxBodySize 最大记录的 Body 大小（4KB）
	MaxBodySize = 4 * 1024
	// LargeBodyThreshold 超大 Body 阈值（64KB），超过只记录大小
	LargeBodyThreshold = 64 * 1024
	// RedactedValue 脱敏后的替换值
	RedactedValue = "[REDACTED]"
	// TruncatedSuffix 截断标记
	TruncatedSuffix = "...[truncated]"
	// BinaryMarker 二进制内容标记
	BinaryMarker = "[binary]"
	// TooLargeMarker 超大内容标记
	TooLargeMarker = "[too large]"
)

// 敏感字段列表（小写）
var sensitiveFields = []string{
	"password", "passwd", "pwd",
	"token", "access_token", "refresh_token",
	"secret", "api_key", "apikey",
	"authorization",
	"credential", "credentials",
}

// SanitizeBody 处理 Body：脱敏 + 截断
// 返回处理后的字符串
func SanitizeBody(body []byte, contentType string) string {
	if len(body) == 0 {
		return ""
	}

	// 超大 Body 只返回标记
	if len(body) > LargeBodyThreshold {
		return TooLargeMarker
	}

	// 二进制类型只返回标记
	if isBinaryContentType(contentType) {
		return BinaryMarker
	}

	// JSON 类型进行脱敏处理
	if isJSONContentType(contentType) {
		sanitized := sanitizeJSON(body)
		return truncateString(sanitized, MaxBodySize)
	}

	// 其他文本类型直接截断
	return truncateString(string(body), MaxBodySize)
}

// sanitizeJSON 对 JSON 进行敏感字段脱敏
func sanitizeJSON(data []byte) string {
	var obj interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		// 解析失败，返回原始字符串（截断）
		return string(data)
	}

	sanitized := sanitizeValue(obj)
	result, err := json.Marshal(sanitized)
	if err != nil {
		return string(data)
	}
	return string(result)
}

// sanitizeValue 递归脱敏
func sanitizeValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, v := range val {
			if isSensitiveField(k) {
				result[k] = RedactedValue
			} else {
				result[k] = sanitizeValue(v)
			}
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, v := range val {
			result[i] = sanitizeValue(v)
		}
		return result
	default:
		return val
	}
}

// isSensitiveField 判断是否为敏感字段
func isSensitiveField(field string) bool {
	lower := strings.ToLower(field)
	for _, sensitive := range sensitiveFields {
		if lower == sensitive || strings.Contains(lower, sensitive) {
			return true
		}
	}
	return false
}

// isJSONContentType 判断是否为 JSON 类型
func isJSONContentType(contentType string) bool {
	return strings.Contains(contentType, "application/json")
}

// isBinaryContentType 判断是否为二进制类型
func isBinaryContentType(contentType string) bool {
	binaryTypes := []string{
		"multipart/form-data",
		"application/octet-stream",
		"image/",
		"audio/",
		"video/",
		"application/pdf",
		"application/zip",
	}
	for _, t := range binaryTypes {
		if strings.Contains(contentType, t) {
			return true
		}
	}
	return false
}

// truncateString 截断字符串
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-len(TruncatedSuffix)] + TruncatedSuffix
}
