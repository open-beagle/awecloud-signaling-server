package grpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	pb "github.com/open-beagle/signal-worker/pkg/proto"
)

type UserDeletionServiceServer struct {
	pb.UnimplementedServerServiceServer
	agent   *AgentServiceServer
	desktop *DesktopServiceServer
}

func NewUserDeletionServiceServer(agent *AgentServiceServer, desktop *DesktopServiceServer) *UserDeletionServiceServer {
	return &UserDeletionServiceServer{agent: agent, desktop: desktop}
}

func validateDeletionCommand(req *pb.UserDeletionCommand) error {
	if req.UserId == 0 || req.JobId == "" || req.CommandId == "" || req.SubjectName == "" || (req.SubjectRole != "agent" && req.SubjectRole != "client") {
		return status.Error(codes.InvalidArgument, "invalid user deletion command")
	}
	return nil
}

func (s *UserDeletionServiceServer) MarkUserDeleting(ctx context.Context, req *pb.UserDeletionCommand) (*pb.CommandResult, error) {
	if err := validateDeletionCommand(req); err != nil {
		return nil, err
	}
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.First(&user, req.UserId).Error; err != nil {
			return err
		}
		if user.Name != req.SubjectName || string(user.Role) != req.SubjectRole {
			return status.Error(codes.FailedPrecondition, "user snapshot mismatch")
		}
		if user.DeletionJobID != nil {
			if *user.DeletionJobID == req.JobId {
				return nil
			}
			return status.Error(codes.AlreadyExists, "user is bound to another deletion job")
		}
		now := time.Now().UTC()
		result := tx.Model(&model.User{}).Where("id = ? AND deletion_job_id IS NULL", req.UserId).Updates(map[string]any{"deletion_job_id": req.JobId, "deletion_requested_at": now, "enabled": false})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return status.Error(codes.Aborted, "user deletion marker changed concurrently")
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	if err != nil {
		return nil, err
	}
	return &pb.CommandResult{Completed: true}, nil
}

func deletionUser(ctx context.Context, req *pb.UserDeletionCommand) (*model.User, error) {
	var user model.User
	err := db.DB.WithContext(ctx).First(&user, req.UserId).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if user.DeletionJobID == nil || *user.DeletionJobID != req.JobId {
		return nil, status.Error(codes.FailedPrecondition, "user deletion marker mismatch")
	}
	return &user, nil
}

func (s *UserDeletionServiceServer) DisconnectUserConnections(ctx context.Context, req *pb.UserDeletionCommand) (*pb.CommandResult, error) {
	if err := validateDeletionCommand(req); err != nil {
		return nil, err
	}
	user, err := deletionUser(ctx, req)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return &pb.CommandResult{Completed: true}, nil
	}
	var nodes []model.Node
	if err := db.DB.WithContext(ctx).Where("user_id = ?", user.ID).Find(&nodes).Error; err != nil {
		return nil, err
	}
	for _, node := range nodes {
		if node.Type == model.NodeTypeDesktop && s.desktop != nil {
			s.desktop.DisconnectDesktop(node.ID)
		}
		if node.Type == model.NodeTypeAgent && s.agent != nil {
			s.agent.DisconnectNode(node.ID)
		}
	}
	if err := db.DB.WithContext(ctx).Model(&model.Node{}).Where("user_id = ?", user.ID).Updates(map[string]any{"last_heartbeat": nil, "ip": ""}).Error; err != nil {
		return nil, err
	}
	return &pb.CommandResult{Completed: true}, nil
}

func (s *UserDeletionServiceServer) CleanupUserResources(ctx context.Context, req *pb.UserDeletionCommand) (*pb.CommandResult, error) {
	if err := validateDeletionCommand(req); err != nil {
		return nil, err
	}
	user, err := deletionUser(ctx, req)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return &pb.CommandResult{Completed: true}, nil
	}
	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		deletes := []struct {
			value any
			query string
		}{
			{&model.AclGroupUserPermission{}, "user_id = ?"},
			{&model.AclServiceUserPermission{}, "user_id = ?"},
			{&model.AclSSHUserPermission{}, "user_id = ? OR target_user_id = ?"},
			{&model.AclSSHGroupPermission{}, "target_user_id = ?"},
			{&model.AclK8SUserPermission{}, "user_id = ? OR target_user_id = ?"},
			{&model.AclK8SGroupPermission{}, "target_user_id = ?"},
			{&model.AclUserUserPermission{}, "granted_user_id = ? OR target_user_id = ?"},
			{&model.AclUserGroupPermission{}, "target_user_id = ?"},
			{&model.AccessGrant{}, "subject_user_id = ?"},
			{&model.GroupMember{}, "user_id = ?"},
			{&model.TenantMembership{}, "user_id = ?"},
			{&model.UserTenantManagementMembership{}, "user_id = ? OR created_by_user_id = ?"},
			{&model.UserSimulationSession{}, "actor_user_id = ? OR effective_user_id = ?"},
			{&model.PlatformRoleMembership{}, "user_id = ? OR created_by_user_id = ?"},
			{&model.UserAuthenticationLink{}, "user_id = ?"},
			{&model.UserIdentityProfile{}, "user_id = ?"},
			{&model.DeviceToken{}, "client_id = ?"},
			{&model.DesktopLoginSession{}, "user_id = ?"},
			{&model.DeployToken{}, "user_id = ?"},
			{&model.PortForward{}, "user_id = ?"},
			{&model.ProxyService{}, "user_id = ?"},
			{&model.Node{}, "user_id = ?"},
		}
		for _, item := range deletes {
			args := []any{user.ID}
			if item.query == "user_id = ? OR target_user_id = ?" || item.query == "granted_user_id = ? OR target_user_id = ?" || item.query == "user_id = ? OR created_by_user_id = ?" || item.query == "actor_user_id = ? OR effective_user_id = ?" {
				args = append(args, user.ID)
			}
			if err := tx.Where(item.query, args...).Delete(item.value).Error; err != nil {
				return fmt.Errorf("cleanup %T: %w", item.value, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &pb.CommandResult{Completed: true}, nil
}

func (s *UserDeletionServiceServer) FinalizeUserDeletion(ctx context.Context, req *pb.UserDeletionCommand) (*pb.CommandResult, error) {
	if err := validateDeletionCommand(req); err != nil {
		return nil, err
	}
	var count int64
	if err := db.DB.WithContext(ctx).Model(&model.User{}).Where("id = ?", req.UserId).Count(&count).Error; err != nil {
		return nil, err
	}
	if count == 0 {
		return &pb.CommandResult{Completed: true}, nil
	}
	result := db.DB.WithContext(ctx).Where("id = ? AND deletion_job_id = ?", req.UserId, req.JobId).Delete(&model.User{})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, status.Error(codes.FailedPrecondition, "user deletion marker mismatch")
	}
	return &pb.CommandResult{Completed: true}, nil
}
