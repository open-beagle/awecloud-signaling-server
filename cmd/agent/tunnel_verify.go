package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/open-beagle/awecloud-signaling-server/internal/agent"
)

type AgentCheckResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

type AgentTunnelVerifyReport struct {
	Overall    string             `json:"overall"` // PASS or FAIL
	Checks     []AgentCheckResult `json:"checks"`
	DurationMs int64              `json:"duration_ms"`
}

// RunAgentTunnelVerify 执行 signal_agent tunnel-verify CLI
func RunAgentTunnelVerify(args []string) int {
	return runAgentTunnelVerifyWithOutput(args, os.Stdout, os.Stderr)
}

func runAgentTunnelVerifyWithOutput(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tunnel-verify", flag.ContinueOnError)
	statusURL := fs.String("statusz-url", "http://127.0.0.1:19090/statusz", "/statusz 接口地址")
	jsonOutput := fs.Bool("json", false, "以 JSON 格式输出检测报告")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "解析命令行参数失败: %v\n", err)
		return 1
	}

	startTime := time.Now()
	report := AgentTunnelVerifyReport{
		Checks: []AgentCheckResult{},
	}

	client := &http.Client{Timeout: 3 * time.Second}

	// 1. 读取初始状态
	resp, err := client.Get(*statusURL)
	if err != nil {
		fmt.Fprintf(stderr, "获取 Tunnel /statusz 失败 (%s): %v\n", *statusURL, err)
		report.Overall = "FAIL"
		report.Checks = append(report.Checks, AgentCheckResult{
			ID:      "INIT",
			Name:    "query /statusz",
			Passed:  false,
			Message: fmt.Sprintf("无法连接状态端点: %v", err),
		})
		outputReport(report, *jsonOutput, stdout)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "Tunnel /statusz 返回非 200: %d\n", resp.StatusCode)
		report.Overall = "FAIL"
		report.Checks = append(report.Checks, AgentCheckResult{
			ID:      "INIT",
			Name:    "query /statusz",
			Passed:  false,
			Message: fmt.Sprintf("状态码: %d", resp.StatusCode),
		})
		outputReport(report, *jsonOutput, stdout)
		return 1
	}

	var initialStatuses []*agent.TunnelResourceStatus
	if err := json.NewDecoder(resp.Body).Decode(&initialStatuses); err != nil {
		fmt.Fprintf(stderr, "解析 /statusz 失败: %v\n", err)
		return 1
	}

	initialMap := make(map[string]*agent.TunnelResourceStatus)
	for _, s := range initialStatuses {
		initialMap[s.ResourceID] = s
	}

	// 2. 对每个 local_port > 0 的资源进行一次主动 TCP 探测
	probeResults := make(map[string]error)
	for _, s := range initialStatuses {
		if s.LocalPort <= 0 {
			continue
		}
		targetAddr := fmt.Sprintf("127.0.0.1:%d", s.LocalPort)
		conn, err := net.DialTimeout("tcp", targetAddr, 2*time.Second)
		if err != nil {
			probeResults[s.ResourceID] = err
			continue
		}
		// 写入一字节探测首包，促发 SVCProxy / TCP 桥接，再关闭
		_ = conn.SetDeadline(time.Now().Add(1 * time.Second))
		_, _ = conn.Write([]byte{0x00})
		buf := make([]byte, 64)
		_, _ = conn.Read(buf)
		_ = conn.Close()
		probeResults[s.ResourceID] = nil
	}

	// 3. 轮询读取更新状态，直到所有被探测的资源计数产生变化（ok 增加或 rejected 增加），上限 6 秒
	pollDeadline := time.Now().Add(6 * time.Second)
	var afterStatuses []*agent.TunnelResourceStatus
	var afterMap map[string]*agent.TunnelResourceStatus

	for {
		resp2, err := client.Get(*statusURL)
		if err == nil && resp2.StatusCode == http.StatusOK {
			var curStatuses []*agent.TunnelResourceStatus
			if err := json.NewDecoder(resp2.Body).Decode(&curStatuses); err == nil {
				_ = resp2.Body.Close()
				curMap := make(map[string]*agent.TunnelResourceStatus, len(curStatuses))
				for _, s := range curStatuses {
					curMap[s.ResourceID] = s
				}

				allUpdated := true
				for _, s := range initialStatuses {
					if s.LocalPort <= 0 {
						continue
					}
					after := curMap[s.ResourceID]
					if after == nil {
						allUpdated = false
						break
					}
					// 判定该资源计数是否已产生变化（ok 增加或 rejected 增加）
					if after.SVCProxyOK == s.SVCProxyOK && after.SVCProxyRejected == s.SVCProxyRejected {
						allUpdated = false
						break
					}
				}

				afterStatuses = curStatuses
				afterMap = curMap

				if allUpdated || time.Now().After(pollDeadline) {
					break
				}
			} else {
				_ = resp2.Body.Close()
			}
		} else if resp2 != nil {
			_ = resp2.Body.Close()
		}

		if time.Now().After(pollDeadline) {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}

	if afterMap == nil {
		fmt.Fprintf(stderr, "探测后未能获取有效的 /statusz 状态\n")
		return 1
	}

	// 4. 执行校验项：D2-2, D2-3, D2-4
	// D2-3 守卫计数：所有资源 direct_dial_violation == 0
	hasViolation := false
	var violationDetail string
	for _, s := range afterStatuses {
		if s.DirectDialViolation > 0 {
			hasViolation = true
			violationDetail = fmt.Sprintf("资源 %s 发生违规直连，计数: %d", s.ResourceID, s.DirectDialViolation)
			break
		}
	}
	report.Checks = append(report.Checks, AgentCheckResult{
		ID:      "D2-3",
		Name:    "direct_dial_violation = 0",
		Passed:  !hasViolation,
		Message: violationDetail,
	})

	for _, s := range initialStatuses {
		if s.LocalPort <= 0 {
			continue
		}

		after := afterMap[s.ResourceID]
		probeErr := probeResults[s.ResourceID]

		if s.ResourceID == "k8s-api" {
			// D2-4: k8s-api path = host_direct 且探测成功 (必须满足: svcproxy_ok 增加且 rejected 不增加)
			passed := s.Path == "host_direct" && probeErr == nil && after != nil && after.SVCProxyOK > s.SVCProxyOK && after.SVCProxyRejected == s.SVCProxyRejected
			msg := ""
			if s.Path != "host_direct" {
				msg = fmt.Sprintf("路径非 host_direct (实际为 %s)", s.Path)
			} else if probeErr != nil {
				msg = fmt.Sprintf("本地端口探测失败: %v", probeErr)
			} else if after == nil || after.SVCProxyOK <= s.SVCProxyOK {
				msg = "探测超时: svcproxy_ok 未增加"
			} else if after.SVCProxyRejected > s.SVCProxyRejected {
				msg = fmt.Sprintf("拨号被拒绝: %s", after.LastSVCProxyError)
			}
			report.Checks = append(report.Checks, AgentCheckResult{
				ID:      "D2-4",
				Name:    "k8s-api  path=host_direct  probe ok",
				Passed:  passed,
				Message: msg,
			})
		} else {
			// D2-2: 普通容器服务 path = svcproxy 且探测成功 (必须满足: svcproxy_ok 增加且 svcproxy_rejected 不增加)
			svcDesc := fmt.Sprintf("%s.%s:%d", s.ServiceName, s.Namespace, s.LocalPort)
			checkName := fmt.Sprintf("%-28s path=svcproxy  probe ok", svcDesc)
			passed := s.Path == "svcproxy" && probeErr == nil && after != nil && after.SVCProxyOK > s.SVCProxyOK && after.SVCProxyRejected == s.SVCProxyRejected
			msg := ""
			if s.Path != "svcproxy" {
				msg = fmt.Sprintf("路径非 svcproxy (实际为 %s)", s.Path)
			} else if probeErr != nil {
				msg = fmt.Sprintf("本地端口拨号失败: %v", probeErr)
			} else if after != nil && after.SVCProxyRejected > s.SVCProxyRejected {
				msg = fmt.Sprintf("被对端 Agent 拒绝: %s", after.LastSVCProxyError)
			} else if after == nil || after.SVCProxyOK <= s.SVCProxyOK {
				msg = "探测超时: svcproxy_ok 未增加"
			}
			report.Checks = append(report.Checks, AgentCheckResult{
				ID:      "D2-2",
				Name:    checkName,
				Passed:  passed,
				Message: msg,
			})
		}
	}

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

	outputReport(report, *jsonOutput, stdout)

	if allPassed {
		return 0
	}
	return 1
}

func outputReport(report AgentTunnelVerifyReport, jsonOutput bool, stdout io.Writer) {
	if jsonOutput {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		for _, c := range report.Checks {
			dots := strings.Repeat(".", max(2, 52-len(c.Name)-len(c.ID)))
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
}
