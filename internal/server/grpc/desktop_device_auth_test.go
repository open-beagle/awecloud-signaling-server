package grpc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func TestDesktopDeviceMutationsRequireCurrentDeviceSecret(t *testing.T) {
	original := db.DB
	t.Cleanup(func() { db.DB = original })
	database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:desktop-device-auth-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.Node{}))
	db.DB = database

	user := model.User{Name: "member", Role: model.UserRoleClient, SecretHash: "user", Enabled: true}
	otherUser := model.User{Name: "other", Role: model.UserRoleClient, SecretHash: "other", Enabled: true}
	require.NoError(t, database.Create(&user).Error)
	require.NoError(t, database.Create(&otherUser).Error)
	currentHash, err := bcrypt.GenerateFromPassword([]byte("current-secret"), bcrypt.MinCost)
	require.NoError(t, err)
	now := time.Now().UTC()
	current := model.Node{UserID: user.ID, Name: "current", Type: model.NodeTypeDesktop, SecretHash: string(currentHash), LastHeartbeat: &now}
	target := model.Node{UserID: user.ID, Name: "target", Type: model.NodeTypeDesktop, SecretHash: "target", LastHeartbeat: &now}
	foreign := model.Node{UserID: otherUser.ID, Name: "foreign", Type: model.NodeTypeDesktop, SecretHash: "foreign", LastHeartbeat: &now}
	require.NoError(t, database.Create(&current).Error)
	require.NoError(t, database.Create(&target).Error)
	require.NoError(t, database.Create(&foreign).Error)
	server := &DesktopServiceServer{connections: make(map[uint64]*DesktopConnection)}

	for _, secret := range []string{"", "wrong-secret"} {
		offline, err := server.OfflineDevice(context.Background(), &pb.OfflineDeviceRequest{
			DesktopId: current.ID, DeviceToken: fmt.Sprintf("%d:***", target.ID), Secret: secret,
		})
		require.NoError(t, err)
		require.False(t, offline.Success)
		require.Equal(t, "当前设备认证失败", offline.Message)
		deleted, err := server.DeleteDevice(context.Background(), &pb.DeleteDeviceRequest{
			DesktopId: current.ID, DeviceToken: fmt.Sprintf("%d:***", target.ID), Secret: secret,
		})
		require.NoError(t, err)
		require.False(t, deleted.Success)
		require.Equal(t, "当前设备认证失败", deleted.Message)
	}
	var unchanged model.Node
	require.NoError(t, database.First(&unchanged, target.ID).Error)
	require.NotNil(t, unchanged.LastHeartbeat)

	foreignResult, err := server.DeleteDevice(context.Background(), &pb.DeleteDeviceRequest{
		DesktopId: current.ID, DeviceToken: fmt.Sprintf("%d:***", foreign.ID), Secret: "current-secret",
	})
	require.NoError(t, err)
	require.False(t, foreignResult.Success)
	require.Equal(t, "无权操作该设备", foreignResult.Message)

	deleted, err := server.DeleteDevice(context.Background(), &pb.DeleteDeviceRequest{
		DesktopId: current.ID, DeviceToken: fmt.Sprintf("%d:***", target.ID), Secret: "current-secret",
	})
	require.NoError(t, err)
	require.True(t, deleted.Success)
	require.ErrorIs(t, database.First(&model.Node{}, target.ID).Error, gorm.ErrRecordNotFound)
}

func TestDesktopDataSnapshotRequiresAssembler(t *testing.T) {
	server := NewDesktopServiceServer(&config.ServerConfig{})
	data, err := server.GetDataSnapshotREST(context.Background(), 1)
	require.EqualError(t, err, "DesktopDataAssembler 未初始化")
	require.Nil(t, data)
}

func TestDesktopCredentialVerificationIsReadOnlyAndAuthenticateUpdatesHeartbeat(t *testing.T) {
	original := db.DB
	t.Cleanup(func() { db.DB = original })
	database, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:desktop-credential-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.Node{}))
	db.DB = database

	user := model.User{Name: "credential-member", Role: model.UserRoleClient, SecretHash: "user", Enabled: true}
	require.NoError(t, database.Create(&user).Error)
	hash, err := bcrypt.GenerateFromPassword([]byte("current-secret"), bcrypt.MinCost)
	require.NoError(t, err)
	originalHeartbeat := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	node := model.Node{UserID: user.ID, Name: "credential-desktop", Type: model.NodeTypeDesktop, SecretHash: string(hash), LastHeartbeat: &originalHeartbeat}
	require.NoError(t, database.Create(&node).Error)
	server := NewDesktopServiceServer(&config.ServerConfig{})

	verifiedNode, verifiedUser, _, ok := server.VerifyCredential(context.Background(), node.ID, "current-secret")
	require.True(t, ok)
	require.Equal(t, node.ID, verifiedNode.ID)
	require.Equal(t, user.ID, verifiedUser.ID)
	var stored model.Node
	require.NoError(t, database.First(&stored, node.ID).Error)
	require.WithinDuration(t, originalHeartbeat, *stored.LastHeartbeat, time.Millisecond)

	_, _, _, ok = server.VerifyCredential(context.Background(), node.ID, "wrong-secret")
	require.False(t, ok)
	response, err := server.Authenticate(context.Background(), &pb.DesktopAuthenticateRequest{DesktopId: node.ID, Secret: "current-secret"})
	require.NoError(t, err)
	require.True(t, response.Success)
	require.NoError(t, database.First(&stored, node.ID).Error)
	require.True(t, stored.LastHeartbeat.After(originalHeartbeat))
}
