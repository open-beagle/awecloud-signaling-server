package grpc

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/signal-worker/pkg/proto"
)

func TestUserDeletionMarkerAndConditionalFinalize(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.User{}))
	previous := db.DB
	db.DB = database
	t.Cleanup(func() { db.DB = previous })
	user := model.User{Name: "alice", Role: model.UserRoleClient, SecretHash: "hash", Enabled: true}
	require.NoError(t, database.Create(&user).Error)
	service := NewUserDeletionServiceServer(nil, nil)
	command := &pb.UserDeletionCommand{UserId: user.ID, JobId: "11111111-1111-1111-1111-111111111111", CommandId: "mark-1", SubjectName: user.Name, SubjectRole: string(user.Role)}
	_, err = service.MarkUserDeleting(context.Background(), command)
	require.NoError(t, err)
	var marked model.User
	require.NoError(t, database.First(&marked, user.ID).Error)
	require.False(t, marked.Enabled)
	require.NotNil(t, marked.DeletionJobID)
	require.Equal(t, command.JobId, *marked.DeletionJobID)
	other := &pb.UserDeletionCommand{UserId: user.ID, JobId: "22222222-2222-2222-2222-222222222222", CommandId: "mark-2", SubjectName: user.Name, SubjectRole: string(user.Role)}
	_, err = service.MarkUserDeleting(context.Background(), other)
	require.Equal(t, codes.AlreadyExists, grpcstatus.Code(err))
	_, err = service.FinalizeUserDeletion(context.Background(), other)
	require.Equal(t, codes.FailedPrecondition, grpcstatus.Code(err))
	_, err = service.FinalizeUserDeletion(context.Background(), command)
	require.NoError(t, err)
	var count int64
	require.NoError(t, database.Model(&model.User{}).Where("id = ?", user.ID).Count(&count).Error)
	require.Zero(t, count)
	_, err = service.FinalizeUserDeletion(context.Background(), command)
	require.NoError(t, err)
}
