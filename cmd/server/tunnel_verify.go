package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/headscale"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

type CheckResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

type TunnelVerifyReport struct {
	TunnelName string        `json:"tunnel_name"`
	Overall    string        `json:"overall"` // PASS or FAIL
	Checks     []CheckResult `json:"checks"`
	DurationMs int64         `json:"duration_ms"`
}

// RunTunnelVerify 执行 server tunnel-verify CLI
func RunTunnelVerify(args []string) int {
	return runTunnelVerifyWithOutput(args, os.Stdout, os.Stderr)
}

func runTunnelVerifyWithOutput(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tunnel-verify", flag.ContinueOnError)
	configPath := fs.String("c", "config/server.toml", "配置文件路径")
	tunnelName := fs.String("tunnel", "", "待验收的 Tunnel 名称")
	probeRevoke := fs.Bool("probe-revoke", false, "执行临时 Tunnel 注入与吊销耗时探测")
	jsonOutput := fs.Bool("json", false, "以 JSON 格式输出检测报告")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "解析命令行参数失败: %v\n", err)
		return 1
	}

	if strings.TrimSpace(*tunnelName) == "" {
		fmt.Fprintf(stderr, "必须通过 --tunnel 指定 Tunnel 名称\n")
		return 1
	}

	startTime := time.Now()
	report := TunnelVerifyReport{
		TunnelName: *tunnelName,
		Checks:     []CheckResult{},
	}

	// 1. 加载配置并初始化 DB
	cfg, err := config.LoadServerConfig(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "加载配置文件失败: %v\n", err)
		return 1
	}

	if err := db.InitDB(cfg.Database); err != nil {
		fmt.Fprintf(stderr, "初始化数据库连接失败: %v\n", err)
		return 1
	}

	ctx := context.Background()

	var hsClient *headscale.Client
	if cfg.Tailscale.HeadscaleURL != "" && cfg.Tailscale.HeadscaleAPIKey != "" {
		c, err := headscale.NewClient(headscale.Config{
			URL:    cfg.Tailscale.HeadscaleURL,
			APIKey: cfg.Tailscale.HeadscaleAPIKey,
		})
		if err == nil {
			hsClient = c
		}
	}

	aclSyncer := headscale.NewACLSyncService(hsClient)

	// ==================== [PRE] 检查 Feature Flag 与 Agent 心跳 ====================
	preCheck := CheckResult{ID: "PRE", Name: "feature flags / agent heartbeat"}
	var targetAgentName string
	var tok model.DeployToken

	{
		flagsOK := cfg.FeatureFlags.Enabled(config.FeatureManagementContextV2) &&
			cfg.FeatureFlags.Enabled(config.FeatureTenantResourceReadV2) &&
			cfg.FeatureFlags.Enabled(config.FeatureResourceModelWrite) &&
			cfg.FeatureFlags.Enabled(config.FeatureResourceReconciliation)

		if !flagsOK {
			preCheck.Passed = false
			preCheck.Message = "四个 Feature Flag (management_context_v2, tenant_resource_read_v2, resource_model_write, resource_reconciliation) 未全开启"
		} else if err := db.DB.WithContext(ctx).
			Scopes(model.ActiveTunnelTokenScope).
			Preload("User").
			Where("name = ?", *tunnelName).
			First(&tok).Error; err != nil {
			preCheck.Passed = false
			preCheck.Message = fmt.Sprintf("未在 DB 中找到活跃状态的 Tunnel (%s): %v", *tunnelName, err)
		} else {
			targetAgentName = tok.TargetAgentName
			var agentNode model.Node
			if err := db.DB.WithContext(ctx).
				Where("name = ? AND type = ?", targetAgentName, model.NodeTypeAgent).
				First(&agentNode).Error; err != nil {
				preCheck.Passed = false
				preCheck.Message = fmt.Sprintf("目标 Agent 节点 (%s) 不存在: %v", targetAgentName, err)
			} else if agentNode.LastHeartbeat == nil || time.Since(*agentNode.LastHeartbeat) > 15*time.Minute {
				preCheck.Passed = false
				if agentNode.LastHeartbeat == nil {
					preCheck.Message = fmt.Sprintf("目标 Agent 节点 (%s) 无心跳记录", targetAgentName)
				} else {
					preCheck.Message = fmt.Sprintf("目标 Agent 节点 (%s) 心跳超时: %v 前", targetAgentName, time.Since(*agentNode.LastHeartbeat))
				}
			} else {
				preCheck.Passed = true
			}
		}
	}
	report.Checks = append(report.Checks, preCheck)

	// ==================== [D1-1] 读取线上策略与 DB 派生规则对比 ====================
	d11Check := CheckResult{ID: "D1-1", Name: "tunnel ACL = agent:50051,6443, no '*'"}
	var livePolicy headscale.ACLPolicy

	if hsClient == nil {
		d11Check.Passed = false
		d11Check.Message = "未配置 Headscale 客户端，无法读取线上策略"
	} else {
		str, err := hsClient.GetPolicy(ctx)
		if err != nil {
			d11Check.Passed = false
			d11Check.Message = fmt.Sprintf("读取线上 ACL 策略失败: %v", err)
		} else {
			if err := json.Unmarshal([]byte(str), &livePolicy); err != nil {
				d11Check.Passed = false
				d11Check.Message = fmt.Sprintf("解析线上 ACL JSON 失败: %v", err)
			} else {
				// 解析端口配置以确定预期端口
				portsCfg, _ := service.ParseTunnelPortsConfig(tok.PortsConfig)
				srcTag := "tag:client-" + tok.Name
				if tok.User != nil && tok.User.Name != "" {
					srcTag = "tag:client-" + tok.User.Name
				} else {
					srcTag = "tag:client-svc-tunnel-" + tok.Name
				}
				targetTag := "tag:agent-" + targetAgentName

				hasWildcard := false
				has50051 := false
				has6443 := false

				for _, r := range livePolicy.ACLs {
					isSrc := false
					for _, s := range r.Src {
						if s == srcTag {
							isSrc = true
							break
						}
					}
					if !isSrc {
						continue
					}

					for _, d := range r.Dst {
						if d == targetTag+":*" || (strings.HasPrefix(d, "tag:agent-") && strings.HasSuffix(d, ":*")) {
							hasWildcard = true
						}
						if strings.HasSuffix(d, ":50051") {
							has50051 = true
						}
						if strings.HasSuffix(d, ":6443") {
							has6443 = true
						}
					}
				}

				if hasWildcard {
					d11Check.Passed = false
					d11Check.Message = fmt.Sprintf("线上策略中存在通配端口授权 (%s:*)", targetTag)
				} else if !has50051 {
					d11Check.Passed = false
					d11Check.Message = "线上策略缺少对 Agent 50051 端口的授权"
				} else if portsCfg.K8sAPIEnabled && !has6443 {
					d11Check.Passed = false
					d11Check.Message = "已启用 K8s API，但线上策略缺少对 6443 端口的授权"
				} else if !portsCfg.K8sAPIEnabled && has6443 {
					d11Check.Passed = false
					d11Check.Message = "未启用 K8s API，但线上策略冗余放行了 6443 端口"
				} else {
					d11Check.Passed = true
				}
			}
		}
	}
	report.Checks = append(report.Checks, d11Check)

	// ==================== [D1-2] 线上策略与 DB 派生策略哈希一致 ====================
	d12Check := CheckResult{ID: "D1-2", Name: "live policy == DB-derived policy"}
	{
		dbPolicy, err := aclSyncer.GenerateACLPolicy(ctx)
		if err != nil {
			d12Check.Passed = false
			d12Check.Message = fmt.Sprintf("由 DB 派生策略失败: %v", err)
		} else if !d11Check.Passed {
			d12Check.Passed = false
			d12Check.Message = "前置线上策略读取或校验未通过"
		} else {
			dbHash := canonicalPolicyHash(dbPolicy)
			liveHash := canonicalPolicyHash(&livePolicy)
			if dbHash == liveHash {
				d12Check.Passed = true
			} else {
				d12Check.Passed = false
				d12Check.Message = fmt.Sprintf("策略哈希不一致: live=%s, db=%s", liveHash[:8], dbHash[:8])
			}
		}
	}
	report.Checks = append(report.Checks, d12Check)

	// ==================== [D1-3] 临时 Tunnel 注入与吊销耗时探测 (可选) ====================
	if *probeRevoke {
		d13Check := CheckResult{ID: "D1-3", Name: "probe-revoke"}
		probeName := fmt.Sprintf("verify-probe-%d", time.Now().UnixNano())
		probeUser := model.User{
			Name:       "svc-tunnel-" + probeName,
			Role:       model.UserRoleClient,
			SecretHash: "probe-hash",
			Enabled:    true,
		}

		if err := db.DB.WithContext(ctx).Create(&probeUser).Error; err != nil {
			d13Check.Passed = false
			d13Check.Message = fmt.Sprintf("创建探针临时用户失败: %v", err)
		} else {
			defer func() {
				_ = db.DB.WithContext(ctx).Where("name = ?", probeUser.Name).Delete(&model.User{})
			}()

			probeToken := model.DeployToken{
				Name:            probeName,
				UserID:          probeUser.ID,
				TargetAgentName: targetAgentName,
				Mode:            "tunnel",
				Status:          model.DeployTokenStatusBound,
				Token:           "probe-token-" + probeName,
			}

			t0 := time.Now()
			if err := db.DB.WithContext(ctx).Create(&probeToken).Error; err != nil {
				d13Check.Passed = false
				d13Check.Message = fmt.Sprintf("写入探针 Token 失败: %v", err)
			} else {
				defer func() {
					_ = db.DB.WithContext(ctx).Where("id = ?", probeToken.ID).Delete(&model.DeployToken{})
				}()

				// 1. 同步后校验规则出现
				if err := aclSyncer.FullSync(ctx); err != nil {
					d13Check.Passed = false
					d13Check.Message = fmt.Sprintf("探针创建后 FullSync 失败: %v", err)
				} else {
					tCreate := time.Since(t0)
					newPolicyStr, _ := hsClient.GetPolicy(ctx)
					probeUserTag := "tag:client-" + probeUser.Name
					hasProbeRule := strings.Contains(newPolicyStr, probeUserTag)

					if !hasProbeRule {
						d13Check.Passed = false
						d13Check.Message = "探针创建并同步后，线上策略未检索到对应 tag:client 规则"
					} else {
						// 2. 注销并校验规则消失
						t1 := time.Now()
						_ = db.DB.WithContext(ctx).Model(&probeToken).Update("status", model.DeployTokenStatusRevoked)
						_ = db.DB.WithContext(ctx).Where("name = ?", probeUser.Name).Delete(&model.User{})
						if err := aclSyncer.FullSync(ctx); err != nil {
							d13Check.Passed = false
							d13Check.Message = fmt.Sprintf("探针吊销后 FullSync 失败: %v", err)
						} else {
							tRevoke := time.Since(t1)
							revokedPolicyStr, _ := hsClient.GetPolicy(ctx)
							hasProbeRuleAfter := strings.Contains(revokedPolicyStr, probeUserTag)

							if hasProbeRuleAfter {
								d13Check.Passed = false
								d13Check.Message = "探针吊销并同步后，线上策略仍残留该规则"
							} else {
								d13Check.Passed = true
								d13Check.Message = fmt.Sprintf("create: %v, revoke: %v", tCreate.Round(time.Millisecond), tRevoke.Round(time.Millisecond))
							}
						}
					}
				}
			}
		}
		report.Checks = append(report.Checks, d13Check)
	}

	// ==================== [D2-1] ports_config 完整性与候选集一致 ====================
	d21Check := CheckResult{ID: "D2-1", Name: "ports_config complete & matches inventory"}
	{
		portsCfg, err := service.ParseTunnelPortsConfig(tok.PortsConfig)
		if err != nil {
			d21Check.Passed = false
			d21Check.Message = fmt.Sprintf("解析 ports_config 失败: %v", err)
		} else {
			// 反查候选集
			candidates, candErr := service.QueryTunnelCandidateServices(ctx, db.DB, targetAgentName, time.Now().UTC())
			if candErr != nil {
				d21Check.Passed = false
				d21Check.Message = fmt.Sprintf("反查候选服务集失败: %v", candErr)
			} else {
				candMap := make(map[string]service.TunnelCandidateService)
				for _, c := range candidates {
					candMap[c.ResourceID] = c
				}

				allCompleteAndMatched := true
				var mismatchReason string

				if len(candidates) == 0 && len(portsCfg.Ports) > 0 {
					// 候选集为空时（如边缘集群未纳入中心治理资产），执行严格的元数据完整性校验
					for _, b := range portsCfg.Ports {
						if !b.Complete() {
							allCompleteAndMatched = false
							mismatchReason = fmt.Sprintf("条目 %s 未满足 Complete() 元数据完整性", b.ResourceID)
							break
						}
					}
					if allCompleteAndMatched {
						d21Check.Passed = true
						d21Check.Message = "条目完整性已满足；目标节点未接入集群治理资产，跳过候选集强一致校验"
					} else {
						d21Check.Passed = false
						d21Check.Message = mismatchReason
					}
				} else {
					for _, b := range portsCfg.Ports {
						if !b.Complete() {
							allCompleteAndMatched = false
							mismatchReason = fmt.Sprintf("条目 %s 未满足 Complete() 元数据完整性", b.ResourceID)
							break
						}
						c, exists := candMap[b.ResourceID]
						if !exists {
							allCompleteAndMatched = false
							mismatchReason = fmt.Sprintf("条目 %s 已不在目标 Agent 候选集中", b.ResourceID)
							break
						}
						if c.ServiceUID != b.ServiceUID {
							allCompleteAndMatched = false
							mismatchReason = fmt.Sprintf("条目 %s 的 ServiceUID (%s != %s) 与候选集不一致", b.ResourceID, b.ServiceUID, c.ServiceUID)
							break
						}
					}

					if allCompleteAndMatched {
						d21Check.Passed = true
					} else {
						d21Check.Passed = false
						d21Check.Message = mismatchReason
					}
				}
			}
		}
	}
	report.Checks = append(report.Checks, d21Check)

	// 计算总体状态
	allPassed := true
	for _, c := range report.Checks {
		if !c.Passed {
			allPassed = false
			break
		}
	}

	if allPassed {
		report.Overall = "PASS"
	} else {
		report.Overall = "FAIL"
	}
	report.DurationMs = time.Since(startTime).Milliseconds()

	// 打印输出
	if *jsonOutput {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		for _, c := range report.Checks {
			dots := strings.Repeat(".", max(2, 45-len(c.Name)-len(c.ID)))
			statusStr := "PASS"
			if !c.Passed {
				statusStr = "FAIL"
			}
			if c.Message != "" {
				fmt.Fprintf(stdout, "[%s] %s %s %s (%s)\n", c.ID, c.Name, dots, statusStr, c.Message)
			} else {
				fmt.Fprintf(stdout, "[%s] %s %s %s\n", c.ID, c.Name, dots, statusStr)
			}
		}
		fmt.Fprintf(stdout, "RESULT: %s\n", report.Overall)
	}

	if allPassed {
		return 0
	}
	return 1
}

func canonicalPolicyHash(p *headscale.ACLPolicy) string {
	if p == nil {
		return ""
	}
	// 规范化比较：序列化 ACL 规则列表
	type normRule struct {
		Action string   `json:"action"`
		Src    []string `json:"src"`
		Dst    []string `json:"dst"`
	}
	rules := make([]normRule, len(p.ACLs))
	for i, r := range p.ACLs {
		srcs := append([]string{}, r.Src...)
		dsts := append([]string{}, r.Dst...)
		sort.Strings(srcs)
		sort.Strings(dsts)
		rules[i] = normRule{Action: r.Action, Src: srcs, Dst: dsts}
	}
	sort.Slice(rules, func(i, j int) bool {
		s1 := strings.Join(rules[i].Src, ",")
		s2 := strings.Join(rules[j].Src, ",")
		if s1 != s2 {
			return s1 < s2
		}
		return strings.Join(rules[i].Dst, ",") < strings.Join(rules[j].Dst, ",")
	})

	data, _ := json.Marshal(rules)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
