package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"gorm.io/gorm"
)

// Deliberately tests DB-derived policy synchronization, not the lifecycle HTTP API.
// Cleanup completes before the report is aggregated, so cleanup errors cannot be hidden.
type tunnelProbeOps struct {
	Create     func() error
	Revoke     func() error
	Sync       func() error
	ReadPolicy func() (string, error)
	Cleanup    func() error
}

func runTunnelPolicyProbe(ops tunnelProbeOps, srcTag string, allowed map[string]bool) (result CheckResult) {
	result = CheckResult{ID: "D1-3", Name: "probe-revoke (DB + FullSync; API hooks not covered)"}
	defer func() {
		if err := ops.Cleanup(); err != nil {
			result.Passed = false
			result.Message += fmt.Sprintf("; cleanup FAIL: %v", err)
		} else {
			result.Message += "; cleanup PASS (DB residuals=0, policy verified)"
		}
	}()
	read := func() (*headscale.ACLPolicy, error) {
		raw, err := ops.ReadPolicy()
		if err != nil {
			return nil, fmt.Errorf("读取 ACL 策略失败: %w", err)
		}
		return parseVerifiedACL(raw)
	}
	id := tunnelACLIdentity{Selectors: map[string]bool{srcTag: true}}
	t0 := time.Now()
	if err := ops.Create(); err != nil {
		result.Message = fmt.Sprintf("创建探针失败: %v", err)
		return
	}
	if err := ops.Sync(); err != nil {
		result.Message = fmt.Sprintf("创建后同步失败: %v", err)
		return
	}
	policy, err := read()
	if err == nil {
		err = checkTunnelACL(policy, id, allowed)
	}
	if err != nil {
		result.Message = fmt.Sprintf("创建后核验失败: %v", err)
		return
	}
	created := time.Since(t0)
	t1 := time.Now()
	if err := ops.Revoke(); err != nil {
		result.Message = fmt.Sprintf("吊销探针失败: %v", err)
		return
	}
	if err := ops.Sync(); err != nil {
		result.Message = fmt.Sprintf("吊销后同步失败: %v", err)
		return
	}
	policy, err = read()
	if err == nil {
		err = checkTunnelACL(policy, id, map[string]bool{})
	}
	if err != nil {
		result.Message = fmt.Sprintf("吊销后核验失败: %v", err)
		return
	}
	result.Passed = true
	result.Message = fmt.Sprintf("create: %v, revoke: %v; lifecycle API hooks NOT COVERED", created.Round(time.Millisecond), time.Since(t1).Round(time.Millisecond))
	return
}

func verifyTunnelPolicyProbe(ctx context.Context, client *headscale.Client, syncer *headscale.ACLSyncService, agentName string) CheckResult {
	if client == nil {
		return CheckResult{ID: "D1-3", Name: "probe-revoke", Message: "Headscale 客户端不可用，未创建探针"}
	}
	tags, err := tunnelAgentTags(ctx, agentName)
	if err != nil {
		return CheckResult{ID: "D1-3", Name: "probe-revoke", Message: fmt.Sprintf("读取目标 Agent 失败: %v", err)}
	}
	name := fmt.Sprintf("verify-probe-%d", time.Now().UnixNano())
	user := model.User{Name: "svc-tunnel-" + name, Role: model.UserRoleClient, SecretHash: "probe-hash", Enabled: true}
	token := model.DeployToken{Name: name, TargetAgentName: agentName, Mode: "tunnel", Status: model.DeployTokenStatusBound, Token: "probe-token-" + name}
	srcTag := "tag:client-" + user.Name
	ops := tunnelProbeOps{
		Create: func() error {
			return db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if err := tx.Create(&user).Error; err != nil {
					return err
				}
				token.UserID = user.ID
				return tx.Create(&token).Error
			})
		},
		Revoke: func() error {
			r := db.DB.WithContext(ctx).Model(&model.DeployToken{}).Where("id = ? AND status = ?", token.ID, model.DeployTokenStatusBound).Update("status", model.DeployTokenStatusRevoked)
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return fmt.Errorf("吊销更新行数=%d，期望1", r.RowsAffected)
			}
			return nil
		},
		Sync:       func() error { return syncer.FullSync(ctx) },
		ReadPolicy: func() (string, error) { return client.GetPolicy(ctx) },
	}
	ops.Cleanup = func() error {
		// An independent bounded context also cleans up after an interrupted probe.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var errs []error
		if err := db.DB.WithContext(cleanupCtx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("name = ? AND token = ?", name, token.Token).Delete(&model.DeployToken{}).Error; err != nil {
				return err
			}
			return tx.Where("name = ?", user.Name).Delete(&model.User{}).Error
		}); err != nil {
			errs = append(errs, fmt.Errorf("清理数据库: %w", err))
		}
		for _, row := range []struct {
			model any
			name  string
		}{{&model.DeployToken{}, name}, {&model.User{}, user.Name}} {
			var count int64
			if err := db.DB.WithContext(cleanupCtx).Model(row.model).Where("name = ?", row.name).Count(&count).Error; err != nil {
				errs = append(errs, err)
			} else if count != 0 {
				errs = append(errs, fmt.Errorf("探针 %s 残留 %d 行", row.name, count))
			}
		}
		if err := syncer.FullSync(cleanupCtx); err != nil {
			errs = append(errs, fmt.Errorf("清理后同步: %w", err))
		}
		raw, err := client.GetPolicy(cleanupCtx)
		if err == nil {
			var policy *headscale.ACLPolicy
			policy, err = parseVerifiedACL(raw)
			if err == nil {
				err = checkTunnelACL(policy, tunnelACLIdentity{Selectors: map[string]bool{srcTag: true}}, map[string]bool{})
			}
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("清理后策略核验: %w", err))
		}
		return errors.Join(errs...)
	}
	return runTunnelPolicyProbe(ops, srcTag, tunnelAllowedDestinations(tags, false))
}
