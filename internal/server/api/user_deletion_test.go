package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/signal-worker/pkg/proto"
)

type fakeDeletionWorker struct{ submitted *pb.SubmitUserDeletionRequest }

func (f *fakeDeletionWorker) Submit(_ context.Context, req *pb.SubmitUserDeletionRequest) (*pb.UserDeletionJob, error) {
	f.submitted = req
	return &pb.UserDeletionJob{Id: "11111111-1111-1111-1111-111111111111", UserId: req.UserId, SubjectName: req.SubjectName, Status: "queued", CurrentStep: "accepted", Progress: 5, RequestId: req.RequestId, CreatedAt: "2026-08-29T00:00:00Z", UpdatedAt: "2026-08-29T00:00:00Z", RowVersion: 2}, nil
}
func (*fakeDeletionWorker) Get(context.Context, string) (*pb.UserDeletionJob, error) { return nil, nil }
func (*fakeDeletionWorker) List(context.Context, []uint64) ([]*pb.UserDeletionJob, error) {
	return nil, nil
}
func (*fakeDeletionWorker) Retry(context.Context, *pb.RetryUserDeletionRequest) (*pb.UserDeletionJob, error) {
	return nil, nil
}

func TestCreateUserDeletionJobReturnsAccepted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.AuditLog{}))
	previous := db.DB
	db.DB = database
	t.Cleanup(func() { db.DB = previous })
	user := model.User{Name: "alice", Role: model.UserRoleClient, SecretHash: "hash", Enabled: true}
	require.NoError(t, database.Create(&user).Error)
	worker := &fakeDeletionWorker{}
	userAPI := NewUserAPI(&config.ServerConfig{})
	userAPI.SetDeletionWorker(worker)
	router := gin.New()
	router.POST("/users/:id/deletion-jobs", RequestMetadataMiddleware(), RequireIdempotencyKey(), userAPI.CreateDeletionJob)
	request := httptest.NewRequest(http.MethodPost, "/users/alice/deletion-jobs", nil)
	request.Header.Set(HeaderIdempotencyKey, "delete-alice-1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusAccepted, response.Code)
	var body Response
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.True(t, body.Success)
	require.NotNil(t, worker.submitted)
	require.Equal(t, user.ID, worker.submitted.UserId)
	require.Equal(t, "delete-alice-1", worker.submitted.IdempotencyKey)
}
