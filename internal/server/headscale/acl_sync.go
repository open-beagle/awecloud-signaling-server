// Package headscale 提供 Headscale API 客户端和 ACL 同步功能
package headscale

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

// ACLSyncService ACL 同步服务
// 负责将数据库中的权限配置同步到 Headscale ACL
type ACLSyncService struct {
	client *Client
	mutex  sync.Mutex
}

// NewACLSyncService 创建 ACL 同步服务
func NewACLSyncService(client *Client) *ACLSyncService {
	return &ACLSyncService{
		client: client,
	}
}

// SyncACL 同步 ACL 规则到 Headscale
// 根据数据库中的权限配置生成 ACL 规则
// 失败时会自动重试最多 3 次
func (s *ACLSyncService) SyncACL(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger.Info("开始同步 ACL 规则到 Headscale")

	// 生成 ACL 规则
	rules, err := s.generateACLRules()
	if err != nil {
		return fmt.Errorf("生成 ACL 规则失败: %w", err)
	}

	// 构建 ACL 策略
	policy := &ACLPolicy{
		ACLs: rules,
	}

	// 设置 ACL 策略，带重试机制
	var lastErr error
	for i := 0; i < 3; i++ {
		err := s.client.SetACLPolicy(ctx, policy)
		if err == nil {
			logger.Infof("ACL 规则同步完成，共 %d 条规则", len(rules))
			return nil
		}

		lastErr = err
		logger.Warnf("ACL 同步失败 (尝试 %d/3): %v", i+1, err)

		// 等待后重试，使用指数退避
		if i < 2 {
			waitTime := time.Duration(i+1) * time.Second
			time.Sleep(waitTime)
		}
	}

	logger.Errorf("ACL 同步最终失败: %v", lastErr)
	return fmt.Errorf("设置 ACL 策略失败（已重试 3 次）: %w", lastErr)
}

// generateACLRules 根据数据库配置生成 ACL 规则
// TODO: 需要根据新的数据模型重新实现 ACL 规则生成逻辑
func (s *ACLSyncService) generateACLRules() ([]ACLRule, error) {
	var rules []ACLRule

	// 临时实现：返回空规则列表，避免编译错误
	// 完整的 ACL 规则生成需要在后续任务中实现
	logger.Warn("ACL 规则生成功能暂未实现，返回空规则列表")

	return rules, nil
}

// appendPort 为 IP 列表添加端口
func appendPort(ips []string, port string) []string {
	result := make([]string, len(ips))
	for i, ip := range ips {
		result[i] = fmt.Sprintf("%s:%s", ip, port)
	}
	return result
}

// AddServicePermission 添加服务权限并同步 ACL
func (s *ACLSyncService) AddServicePermission(ctx context.Context, serviceID, clientID, grantedBy int64, expiresAt *time.Time) error {
	// 创建权限记录
	perm := &model.ServicePermission{
		ServiceID: serviceID,
		ClientID:  clientID,
		GrantedBy: grantedBy,
		GrantedAt: time.Now(),
		ExpiresAt: expiresAt,
	}

	if err := db.DB.Create(perm).Error; err != nil {
		return fmt.Errorf("创建权限记录失败: %w", err)
	}

	logger.Infof("添加服务权限: service_id=%d, client_id=%d", serviceID, clientID)

	// 同步 ACL（带重试）
	if err := s.SyncACL(ctx); err != nil {
		logger.Errorf("同步 ACL 失败: %v", err)
		// 不回滚权限记录，下次同步会修复
		return fmt.Errorf("权限已添加但 ACL 同步失败: %w", err)
	}

	return nil
}

// RemoveServicePermission 删除服务权限并同步 ACL
func (s *ACLSyncService) RemoveServicePermission(ctx context.Context, permissionID int64) error {
	// 删除权限记录
	if err := db.DB.Delete(&model.ServicePermission{}, permissionID).Error; err != nil {
		return fmt.Errorf("删除权限记录失败: %w", err)
	}

	logger.Infof("删除服务权限: permission_id=%d", permissionID)

	// 同步 ACL（带重试）
	if err := s.SyncACL(ctx); err != nil {
		logger.Errorf("同步 ACL 失败: %v", err)
		return fmt.Errorf("权限已删除但 ACL 同步失败: %w", err)
	}

	return nil
}

// AddAgentServicePermission 添加 Agent 服务权限并同步 ACL
func (s *ACLSyncService) AddAgentServicePermission(ctx context.Context, agentID, serviceID, grantedBy int64) error {
	// 创建权限记录
	perm := &model.AgentServicePermission{
		AgentID:   agentID,
		ServiceID: serviceID,
		GrantedBy: grantedBy,
		GrantedAt: time.Now(),
	}

	if err := db.DB.Create(perm).Error; err != nil {
		return fmt.Errorf("创建权限记录失败: %w", err)
	}

	logger.Infof("添加 Agent 服务权限: agent_id=%d, service_id=%d", agentID, serviceID)

	// 同步 ACL（带重试）
	if err := s.SyncACL(ctx); err != nil {
		logger.Errorf("同步 ACL 失败: %v", err)
		return fmt.Errorf("权限已添加但 ACL 同步失败: %w", err)
	}

	return nil
}

// RemoveAgentServicePermission 删除 Agent 服务权限并同步 ACL
func (s *ACLSyncService) RemoveAgentServicePermission(ctx context.Context, permissionID int64) error {
	// 删除权限记录
	if err := db.DB.Delete(&model.AgentServicePermission{}, permissionID).Error; err != nil {
		return fmt.Errorf("删除权限记录失败: %w", err)
	}

	logger.Infof("删除 Agent 服务权限: permission_id=%d", permissionID)

	// 同步 ACL（带重试）
	if err := s.SyncACL(ctx); err != nil {
		logger.Errorf("同步 ACL 失败: %v", err)
		return fmt.Errorf("权限已删除但 ACL 同步失败: %w", err)
	}

	return nil
}

// UpdateServiceAccessType 更新服务访问类型并同步 ACL
// TODO: 需要根据新的服务模型重新实现
func (s *ACLSyncService) UpdateServiceAccessType(ctx context.Context, serviceID int64, accessType string, groupID *int64) error {
	logger.Warn("UpdateServiceAccessType 功能暂未实现，需要根据新的服务模型重新实现")
	return fmt.Errorf("UpdateServiceAccessType 功能暂未实现")
}

// UpdateAgentGroup 更新 Agent 分组并重新生成 ACL
// TODO: 需要根据新的分组模型重新实现
func (s *ACLSyncService) UpdateAgentGroup(ctx context.Context, agentID int64, groupName string) error {
	logger.Warn("UpdateAgentGroup 功能暂未实现，需要根据新的分组模型重新实现")
	return fmt.Errorf("UpdateAgentGroup 功能暂未实现")
}

// StartPeriodicSync 启动定时全量同步
// 每 5 分钟全量同步一次 ACL，确保 ACL 规则与数据库状态一致
func (s *ACLSyncService) StartPeriodicSync(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	logger.Info("启动 ACL 定时同步任务，间隔 5 分钟")

	// 立即执行一次同步
	if err := s.SyncACL(ctx); err != nil {
		logger.Errorf("初始 ACL 同步失败: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info("ACL 定时同步任务已停止")
			return
		case <-ticker.C:
			logger.Debug("执行定时 ACL 全量同步")
			if err := s.SyncACL(ctx); err != nil {
				logger.Errorf("定时 ACL 同步失败: %v", err)
			}
		}
	}
}
