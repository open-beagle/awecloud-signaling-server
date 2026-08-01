package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxDeliveryJSONBytes = 64 * 1024
	maxSummaryBytes      = 512
)

var (
	ErrInvalidDeliveryInput = errors.New("invalid resource delivery input")
	ErrSensitiveJSONField   = errors.New("sensitive field is not allowed")
	sensitiveAssignment     = regexp.MustCompile(`(?i)(authorization|access[_-]?token|refresh[_-]?token|client[_-]?secret|private[_-]?key|token|secret|password|cookie|kubeconfig)\s*[:=]\s*[^\s,;]+`)
	bearerCredential        = regexp.MustCompile(`(?i)bearer\s+[^\s,;]+`)
	dsnCredential           = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/@\s]+@`)
)

type JSONFieldPolicy struct {
	allowed map[string]struct{}
}

func NewJSONFieldPolicy(fields ...string) JSONFieldPolicy {
	allowed := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		allowed[field] = struct{}{}
	}
	return JSONFieldPolicy{allowed: allowed}
}

func (p JSONFieldPolicy) Validate(data []byte) ([]byte, error) {
	if len(data) == 0 {
		data = []byte("{}")
	}
	if len(data) > maxDeliveryJSONBytes {
		return nil, fmt.Errorf("%w: JSON exceeds %d bytes", ErrInvalidDeliveryInput, maxDeliveryJSONBytes)
	}

	value, err := decodeJSONObject(data)
	if err != nil {
		return nil, err
	}
	for field := range value {
		if _, ok := p.allowed[field]; !ok {
			return nil, fmt.Errorf("%w: field %q is not declared", ErrInvalidDeliveryInput, field)
		}
	}
	if field, ok := findSensitiveJSONField(value); ok {
		return nil, fmt.Errorf("%w: %s", ErrSensitiveJSONField, field)
	}

	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: canonicalize JSON: %v", ErrInvalidDeliveryInput, err)
	}
	if len(canonical) > maxDeliveryJSONBytes {
		return nil, fmt.Errorf("%w: canonical JSON exceeds %d bytes", ErrInvalidDeliveryInput, maxDeliveryJSONBytes)
	}
	return canonical, nil
}

func decodeJSONObject(data []byte) (map[string]any, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%w: JSON is not valid UTF-8", ErrInvalidDeliveryInput)
	}
	var value map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%w: invalid JSON object: %v", ErrInvalidDeliveryInput, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("%w: multiple JSON values", ErrInvalidDeliveryInput)
		}
		return nil, fmt.Errorf("%w: invalid trailing JSON: %v", ErrInvalidDeliveryInput, err)
	}
	if value == nil {
		return nil, fmt.Errorf("%w: JSON value must be an object", ErrInvalidDeliveryInput)
	}
	return value, nil
}

func findSensitiveJSONField(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			normalized := strings.NewReplacer("-", "", "_", "", ".", "").Replace(strings.ToLower(key))
			switch normalized {
			case "authorization", "cookie", "credential", "credentials", "kubeconfig", "password", "privatekey", "secret", "clientsecret", "token", "accesstoken", "refreshtoken":
				return key, true
			}
			if field, ok := findSensitiveJSONField(nested); ok {
				return key + "." + field, true
			}
		}
	case []any:
		for _, nested := range typed {
			if field, ok := findSensitiveJSONField(nested); ok {
				return field, true
			}
		}
	case string:
		if containsSensitiveText(typed) {
			return "value", true
		}
	}
	return "", false
}

func containsSensitiveText(value string) bool {
	upper := strings.ToUpper(value)
	return sensitiveAssignment.MatchString(value) || bearerCredential.MatchString(value) || dsnCredential.MatchString(value) || strings.Contains(upper, "PRIVATE KEY-----")
}

func redactSensitiveText(value string) string {
	if strings.Contains(strings.ToUpper(value), "PRIVATE KEY-----") {
		return "[REDACTED SENSITIVE SUMMARY]"
	}
	value = bearerCredential.ReplaceAllString(value, "Bearer [REDACTED]")
	value = sensitiveAssignment.ReplaceAllStringFunc(value, func(match string) string {
		separator := strings.IndexAny(match, ":=")
		if separator < 0 {
			return "[REDACTED]"
		}
		return strings.TrimSpace(match[:separator]) + "=[REDACTED]"
	})
	value = dsnCredential.ReplaceAllString(value, `${1}[REDACTED]@`)
	return value
}

func validateRequired(name, value string, maxBytes int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: %s is required", ErrInvalidDeliveryInput, name)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%w: %s is not valid UTF-8", ErrInvalidDeliveryInput, name)
	}
	if len(value) > maxBytes {
		return fmt.Errorf("%w: %s exceeds %d bytes", ErrInvalidDeliveryInput, name, maxBytes)
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return fmt.Errorf("%w: %s contains control characters", ErrInvalidDeliveryInput, name)
		}
	}
	return nil
}

func validateOptionalSHA256(name, value string) error {
	if value == "" {
		return nil
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("%w: %s must be a SHA-256 hex digest", ErrInvalidDeliveryInput, name)
	}
	return nil
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func sanitizeSummary(value string) string {
	value = redactSensitiveText(value)
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	return truncateUTF8Bytes(value, maxSummaryBytes)
}

func truncateUTF8Bytes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
