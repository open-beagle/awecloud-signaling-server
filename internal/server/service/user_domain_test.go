package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func openUserDomainTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "user_domain.db")
	database, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, database.AutoMigrate(
		&model.SystemConfig{},
		&model.User{},
		&model.Node{},
		&model.ResourceProvider{},
		&model.TechnicalResource{},
	))
	return database
}

func TestDeriveBrandDomainSuffixAndPrecedence(t *testing.T) {
	t.Cleanup(func() { SetConfiguredPublicURLs() })

	require.Equal(t, ".acme", DeriveBrandDomainSuffix("https://signal.acme.io"))
	require.Equal(t, ".nebula", DeriveBrandDomainSuffix("https://auth.nebula.com.cn/auth/desktop/callback"))
	require.Equal(t, ".beagle", DeriveBrandDomainSuffix("https://signal.beagle.org.uk:8443"))
	require.Equal(t, "", DeriveBrandDomainSuffix("https://127.0.0.1:8080"))
	require.Equal(t, "", DeriveBrandDomainSuffix("http://localhost:8080"))
	require.Equal(t, "", DeriveBrandDomainSuffix("http://signal-server.signal-system.svc.cluster.local:8080"))

	database := openUserDomainTestDB(t)
	ctx := context.Background()

	// 未配置公网 URL 时默认回退 .beagle
	SetConfiguredPublicURLs()
	require.Equal(t, ".beagle", EffectiveDomainSuffix(ctx, database))

	// 配置公网 URL 且 system_config 为空（或保留默认 .beagle）时，自动推导控制面品牌后缀
	SetConfiguredPublicURLs("http://headscale-internal:8080", "https://signal.acme.io")
	require.Equal(t, ".acme", EffectiveDomainSuffix(ctx, database))

	require.NoError(t, database.Create(&model.SystemConfig{Key: model.ConfigDomainSuffix, Value: ".beagle"}).Error)
	require.Equal(t, ".acme", EffectiveDomainSuffix(ctx, database))

	// 管理员显式指定非默认后缀时优先覆盖
	require.NoError(t, database.Model(&model.SystemConfig{}).Where("key = ?", model.ConfigDomainSuffix).Update("value", ".corp-net").Error)
	require.Equal(t, ".corp-net", EffectiveDomainSuffix(ctx, database))
}

func TestThreeSegmentPersonalHostDomainsAndCollisionChecks(t *testing.T) {
	t.Cleanup(func() { SetConfiguredPublicURLs() })
	SetConfiguredPublicURLs("https://signal.acme.io")

	database := openUserDomainTestDB(t)
	ctx := context.Background()

	u1 := model.User{Name: "mengsk_dev", Role: model.UserRoleClient, SecretHash: "h", Enabled: true}
	u2 := model.User{Name: "teammate", Role: model.UserRoleClient, SecretHash: "h", Enabled: true}
	require.NoError(t, database.Create(&u1).Error)
	require.NoError(t, database.Create(&u2).Error)

	require.NoError(t, database.Create(&model.ResourceProvider{
		ID:          "prov-named",
		Key:         "shanghai",
		DisplayName: "Shanghai",
		Status:      model.ProviderStatusActive,
		DomainScope: model.ProviderDomainNamed,
		DomainLabel: "shanghai",
	}).Error)

	// 用户名自定义域名不得与具名 Provider 或其他用户冲突
	_, err := UpdateUserDomainLabel(ctx, database, u1.ID, "shanghai")
	require.ErrorIs(t, err, ErrUserDomainLabelExists)
	_, err = UpdateUserDomainLabel(ctx, database, u1.ID, "teammate")
	require.ErrorIs(t, err, ErrUserDomainLabelExists)

	userLabel, err := UpdateUserDomainLabel(ctx, database, u1.ID, "mengk")
	require.NoError(t, err)
	require.Equal(t, "mengk", userLabel)
	require.NoError(t, database.First(&u1, u1.ID).Error)

	homeNode := model.Node{UserID: u1.ID, Name: "HOME-PC", Type: model.NodeTypeDesktop, SecretHash: "s1"}
	workNode := model.Node{UserID: u1.ID, Name: "WORK-PC", Type: model.NodeTypeDesktop, SecretHash: "s2"}
	require.NoError(t, database.Create(&homeNode).Error)
	require.NoError(t, database.Create(&workNode).Error)

	EnsureDesktopNodeHostDomainLabel(ctx, database, &homeNode)
	EnsureDesktopNodeHostDomainLabel(ctx, database, &workNode)
	require.Equal(t, "home-pc.mengk.acme", BuildPersonalDeviceDomain(ctx, database, homeNode, u1))

	_, err = UpdateDesktopNodeHostDomainLabel(ctx, database, u1.ID, homeNode.ID, "home")
	require.NoError(t, err)
	_, err = UpdateDesktopNodeHostDomainLabel(ctx, database, u1.ID, workNode.ID, "work")
	require.NoError(t, err)
	_, err = UpdateDesktopNodeHostDomainLabel(ctx, database, u1.ID, workNode.ID, "home")
	require.ErrorIs(t, err, ErrHostDomainLabelExists)

	require.NoError(t, database.First(&homeNode, homeNode.ID).Error)
	require.NoError(t, database.First(&workNode, workNode.ID).Error)
	require.Equal(t, "home.mengk.acme", BuildPersonalDeviceDomain(ctx, database, homeNode, u1))
	require.Equal(t, "work.mengk.acme", BuildPersonalDeviceDomain(ctx, database, workNode, u1))
}
