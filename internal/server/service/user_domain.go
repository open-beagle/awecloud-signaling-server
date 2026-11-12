package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var (
	ErrUserDomainLabelInvalid = errors.New("user domain label is invalid")
	ErrUserDomainLabelExists  = errors.New("user domain label already exists")

	configuredPublicURLsMu sync.RWMutex
	configuredPublicURLs   []string

	nonDNSLabelChars = regexp.MustCompile(`[^a-z0-9-]+`)
	digitsOnlyLabel  = regexp.MustCompile(`^[0-9]+$`)

	multiPartPublicTLDs = []string{
		".com.cn", ".net.cn", ".org.cn", ".gov.cn", ".ac.cn", ".edu.cn",
		".co.uk", ".org.uk", ".ac.uk",
		".co.jp", ".ne.jp", ".or.jp",
		".com.hk", ".com.tw", ".org.tw",
		".com.au", ".net.au", ".com.sg", ".com.my", ".co.kr", ".com.br",
	}

	internalHostSuffixes = []string{
		".local", ".internal", ".localhost", ".lan", ".arpa", ".svc", ".cluster.local",
	}
)

// SetConfiguredPublicURLs 注册当前 Server 配置的外部访问 URL 列表，用于运行时动态推导根品牌域名后缀
func SetConfiguredPublicURLs(urls ...string) {
	cleaned := make([]string, 0, len(urls))
	for _, raw := range urls {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	configuredPublicURLsMu.Lock()
	configuredPublicURLs = cleaned
	configuredPublicURLsMu.Unlock()
}

func getConfiguredPublicURLs() []string {
	configuredPublicURLsMu.RLock()
	defer configuredPublicURLsMu.RUnlock()
	out := make([]string, len(configuredPublicURLs))
	copy(out, configuredPublicURLs)
	return out
}

// DeriveBrandDomainSuffix 从控制面公网 URL 推导根品牌域名后缀（如 https://signal.acme.io -> .acme）
// 若输入为空、IP 地址、localhost 或内网后缀则返回空字符串，由上层回退默认后缀。
func DeriveBrandDomainSuffix(rawURL string) string {
	host := extractHostnameForBrand(rawURL)
	if host == "" || host == "localhost" || net.ParseIP(host) != nil {
		return ""
	}
	for _, internalSuffix := range internalHostSuffixes {
		if strings.HasSuffix(host, internalSuffix) {
			return ""
		}
	}

	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return ""
	}

	stripCount := 1
	for _, multiTLD := range multiPartPublicTLDs {
		if strings.HasSuffix(host, multiTLD) {
			tldLabels := strings.Count(multiTLD, ".")
			if len(labels) > tldLabels {
				stripCount = tldLabels
			}
			break
		}
	}
	if len(labels) <= stripCount {
		return ""
	}

	brand := labels[len(labels)-1-stripCount]
	if !providerDomainLabelPattern.MatchString(brand) || digitsOnlyLabel.MatchString(brand) {
		return ""
	}
	return "." + brand
}

func extractHostnameForBrand(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return ""
		}
		s = u.Hostname()
	} else {
		if slash := strings.IndexByte(s, '/'); slash >= 0 {
			s = s[:slash]
		}
		if h, _, err := net.SplitHostPort(s); err == nil {
			s = h
		}
	}
	s = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
	s = strings.Trim(s, "[]")
	return s
}

func normalizeDomainSuffixValue(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	v = strings.TrimRight(v, ".")
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, ".") {
		v = "." + v
	}
	label := strings.TrimPrefix(v, ".")
	if !providerDomainLabelPattern.MatchString(label) || digitsOnlyLabel.MatchString(label) {
		return ""
	}
	return v
}

// EffectiveDomainSuffix 计算当前生效的根域名后缀：
// 1. 若 system_config.domain_suffix 显式配置了非默认后缀（!= .beagle），优先采用；
// 2. 否则尝试从已配置的外部访问 URL（或数据库中保存的 headscale_public_url / client_download_url）动态推导品牌后缀；
// 3. 兜底回退 model.DefaultDomainSuffix（.beagle）。
func EffectiveDomainSuffix(ctx context.Context, database *gorm.DB) string {
	var dbDomainSuffix string
	var dbPublicURLs []string

	if database != nil {
		if ctx == nil {
			ctx = context.Background()
		}
		var configs []model.SystemConfig
		if err := database.WithContext(ctx).Where("key IN ?", []string{
			model.ConfigDomainSuffix,
			model.ConfigHeadscalePublicURL,
			model.ConfigClientDownloadURL,
		}).Find(&configs).Error; err == nil {
			for _, cfg := range configs {
				switch cfg.Key {
				case model.ConfigDomainSuffix:
					dbDomainSuffix = normalizeDomainSuffixValue(cfg.Value)
				case model.ConfigHeadscalePublicURL, model.ConfigClientDownloadURL:
					if strings.TrimSpace(cfg.Value) != "" {
						dbPublicURLs = append(dbPublicURLs, cfg.Value)
					}
				}
			}
		}
	}

	if dbDomainSuffix != "" && dbDomainSuffix != model.DefaultDomainSuffix {
		return dbDomainSuffix
	}

	for _, candidateURL := range append(getConfiguredPublicURLs(), dbPublicURLs...) {
		if derived := DeriveBrandDomainSuffix(candidateURL); derived != "" {
			return derived
		}
	}

	if dbDomainSuffix != "" {
		return dbDomainSuffix
	}
	return model.DefaultDomainSuffix
}

// NormalizeUserDomainLabel 校验并规范化个人域名中段标识（如 mengk）
func NormalizeUserDomainLabel(value string) (string, error) {
	return normalizeDomainLabel(value, ErrUserDomainLabelInvalid)
}

// SanitizeDNSLabel 将任意用户名或设备名安全清洗为合法 DNS 标签；无法清洗时返回空字符串
func SanitizeDNSLabel(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = nonDNSLabelChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	if len(s) > 63 {
		s = strings.TrimRight(s[:63], "-")
	}
	if s == "" || !providerDomainLabelPattern.MatchString(s) {
		return ""
	}
	if _, reserved := providerDomainLabelReservedSet[s]; reserved {
		return ""
	}
	return s
}

// EffectiveUserDomainLabel 返回用户当前生效的个人域名中段标识（优先自定义 domain_label，否则回退规范化用户名）
func EffectiveUserDomainLabel(user model.User) string {
	if custom, err := NormalizeUserDomainLabel(user.DomainLabel); err == nil && custom != "" {
		return custom
	}
	if fromName := SanitizeDNSLabel(user.Name); fromName != "" {
		return fromName
	}
	if user.ID > 0 {
		return fmt.Sprintf("u-%d", user.ID)
	}
	return "user"
}

// CheckUserDomainLabelConflict 检查候选个人域名标识是否与全局其他用户、具名 Provider 或根域 Agent 冲突
func CheckUserDomainLabelConflict(ctx context.Context, database *gorm.DB, excludeUserID uint64, label string) error {
	if database == nil {
		return ErrUserDomainLabelInvalid
	}
	label = strings.ToLower(strings.TrimSpace(label))

	// 1. 与其他 User 的显式 domain_label 或默认生效用户名标识去重
	var otherUsers []model.User
	if err := database.WithContext(ctx).Select("id", "name", "domain_label").Where("id <> ?", excludeUserID).Find(&otherUsers).Error; err != nil {
		return err
	}
	for _, u := range otherUsers {
		if strings.EqualFold(strings.TrimSpace(u.DomainLabel), label) || EffectiveUserDomainLabel(u) == label {
			return ErrUserDomainLabelExists
		}
	}

	// 2. 与具名 ResourceProvider 的 domain_label 去重，避免与 <agent>.<provider>.<brand> 撞名
	if database.Migrator().HasTable(&model.ResourceProvider{}) {
		var providerCount int64
		if err := database.WithContext(ctx).Model(&model.ResourceProvider{}).
			Where("domain_scope = ? AND lower(domain_label) = ?", model.ProviderDomainNamed, label).
			Count(&providerCount).Error; err != nil {
			return err
		}
		if providerCount > 0 {
			return ErrUserDomainLabelExists
		}
	}

	// 3. 与根作用域 Provider 下 Agent 的 domain_label 去重，避免与 <host>.<agent>.<brand> 撞名
	if database.Migrator().HasTable(&model.TechnicalResource{}) && database.Migrator().HasTable(&model.ResourceProvider{}) {
		var rootAgentCount int64
		if err := database.WithContext(ctx).Table("technical_resource AS agent").
			Joins("JOIN resource_provider AS provider ON provider.id = agent.provider_id").
			Where("agent.type = ? AND agent.deleted_at IS NULL AND provider.domain_scope = ? AND lower(agent.domain_label) = ?",
				model.TechnicalResourceAgent, model.ProviderDomainRoot, label).
			Count(&rootAgentCount).Error; err != nil {
			return err
		}
		if rootAgentCount > 0 {
			return ErrUserDomainLabelExists
		}
	}

	return nil
}

// UpdateUserDomainLabel 更新指定用户的个人域名中段标识（传空字符串表示恢复默认用户名标识）
func UpdateUserDomainLabel(ctx context.Context, database *gorm.DB, userID uint64, rawLabel string) (string, error) {
	if database == nil || userID == 0 {
		return "", ErrUserDomainLabelInvalid
	}
	var user model.User
	if err := database.WithContext(ctx).First(&user, userID).Error; err != nil {
		return "", err
	}

	trimmed := strings.TrimSpace(rawLabel)
	var persistValue string
	var effectiveLabel string
	if trimmed == "" {
		persistValue = ""
		tempUser := user
		tempUser.DomainLabel = ""
		effectiveLabel = EffectiveUserDomainLabel(tempUser)
	} else {
		normalized, err := NormalizeUserDomainLabel(trimmed)
		if err != nil {
			return "", err
		}
		persistValue = normalized
		effectiveLabel = normalized
	}

	if err := CheckUserDomainLabelConflict(ctx, database, user.ID, effectiveLabel); err != nil {
		return "", err
	}

	if err := database.WithContext(ctx).Model(&model.User{}).Where("id = ?", user.ID).Update("domain_label", persistValue).Error; err != nil {
		return "", err
	}
	return effectiveLabel, nil
}

// EffectiveDesktopHostDomainLabel 返回 Desktop 设备当前生效的主机域名前缀标识（如 home / work）
func EffectiveDesktopHostDomainLabel(node model.Node) string {
	if custom, err := NormalizeHostDomainLabel(node.HostDomainLabel); err == nil && custom != "" {
		return custom
	}
	if fromName := SanitizeDNSLabel(node.Name); fromName != "" {
		return fromName
	}
	if fromHost := SanitizeDNSLabel(node.Hostname); fromHost != "" {
		return fromHost
	}
	if node.ID > 0 {
		return fmt.Sprintf("host-%d", node.ID)
	}
	return "host"
}

// EnsureDesktopNodeHostDomainLabel 确保 Desktop 节点拥有在该用户名下无冲突的持久化 host_domain_label
func EnsureDesktopNodeHostDomainLabel(ctx context.Context, database *gorm.DB, node *model.Node) string {
	if node == nil {
		return ""
	}
	if existing, err := NormalizeHostDomainLabel(node.HostDomainLabel); err == nil && existing != "" {
		node.HostDomainLabel = existing
		return existing
	}

	base := EffectiveDesktopHostDomainLabel(*node)
	if database == nil || node.UserID == 0 {
		node.HostDomainLabel = base
		return base
	}

	var siblings []model.Node
	_ = database.WithContext(ctx).Select("id", "name", "hostname", "host_domain_label").
		Where("user_id = ? AND id <> ?", node.UserID, node.ID).
		Order("id ASC").
		Find(&siblings).Error

	used := make(map[string]struct{}, len(siblings))
	for _, sib := range siblings {
		if l, err := NormalizeHostDomainLabel(sib.HostDomainLabel); err == nil && l != "" {
			used[l] = struct{}{}
		}
	}

	chosen := base
	for seq := 2; ; seq++ {
		if _, taken := used[chosen]; !taken {
			break
		}
		suffix := fmt.Sprintf("-%d", seq)
		maxBase := 63 - len(suffix)
		prefix := base
		if len(prefix) > maxBase {
			prefix = strings.TrimRight(prefix[:maxBase], "-")
		}
		chosen = prefix + suffix
	}

	node.HostDomainLabel = chosen
	if node.ID != 0 {
		_ = database.WithContext(ctx).Model(&model.Node{}).Where("id = ?", node.ID).Update("host_domain_label", chosen).Error
	}
	return chosen
}

// UpdateDesktopNodeHostDomainLabel 更新本人名下某台 Desktop 设备的主机域名前缀标识（如 home / work / qnap）
func UpdateDesktopNodeHostDomainLabel(ctx context.Context, database *gorm.DB, userID, nodeID uint64, rawLabel string) (string, error) {
	if database == nil || userID == 0 || nodeID == 0 {
		return "", ErrHostDomainLabelInvalid
	}
	normalized, err := NormalizeHostDomainLabel(rawLabel)
	if err != nil {
		return "", err
	}

	var siblings []model.Node
	if err := database.WithContext(ctx).Select("id", "name", "hostname", "host_domain_label").
		Where("user_id = ? AND id <> ?", userID, nodeID).
		Find(&siblings).Error; err != nil {
		return "", err
	}
	for _, sib := range siblings {
		sibLabel := strings.ToLower(strings.TrimSpace(sib.HostDomainLabel))
		if sibLabel == "" {
			sibLabel = EffectiveDesktopHostDomainLabel(sib)
		}
		if sibLabel == normalized {
			return "", ErrHostDomainLabelExists
		}
	}

	if err := database.WithContext(ctx).Model(&model.Node{}).
		Where("id = ? AND user_id = ? AND type = ?", nodeID, userID, model.NodeTypeDesktop).
		Update("host_domain_label", normalized).Error; err != nil {
		return "", err
	}
	return normalized, nil
}

// BuildPersonalDeviceDomain 拼接三段式个人主机完整域名：<host-label>.<user-label><effective-brand-suffix>
func BuildPersonalDeviceDomain(ctx context.Context, database *gorm.DB, node model.Node, owner model.User) string {
	hostLabel := EffectiveDesktopHostDomainLabel(node)
	userLabel := EffectiveUserDomainLabel(owner)
	suffix := EffectiveDomainSuffix(ctx, database)
	return fmt.Sprintf("%s.%s%s", hostLabel, userLabel, suffix)
}
