package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

func TestPlatformSupplyGovernanceRejectsStalePermissionRevision(t *testing.T) {
	fixture := newManagementAuthorizationFixture(t)
	authorization, err := ResolveManagementContext(
		fixture.database, fixture.actor.ID, model.ManagementScopePlatform, "", fixture.now, false,
	)
	require.NoError(t, err)
	require.NoError(t, fixture.database.Model(&model.PlatformRoleMembership{}).
		Where("user_id = ?", fixture.actor.ID).
		Update("permission_revision", gorm.Expr("permission_revision + 1")).Error)

	governance := NewPlatformSupplyGovernanceService(fixture.database)
	governance.now = func() time.Time { return fixture.now }
	_, err = governance.ListSupplyConflicts(context.Background(), authorization, PlatformSupplyConflictListInput{})
	require.ErrorIs(t, err, ErrManagementPermissionDenied)
}
