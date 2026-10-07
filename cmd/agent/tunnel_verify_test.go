package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/open-beagle/awecloud-signaling-server/internal/agent"
)

func TestAgentTunnelVerify_Success(t *testing.T) {
	var k8sOKCount int64 = 0
	var svcOKCount int64 = 0

	// 启动两个真实的本地监听端口
	lK8s, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen for k8s-api mock: %v", err)
	}
	defer lK8s.Close()
	k8sPort := lK8s.Addr().(*net.TCPAddr).Port

	lSvc, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen for svc mock: %v", err)
	}
	defer lSvc.Close()
	svcPort := lSvc.Addr().(*net.TCPAddr).Port

	// 模拟接受探测连接并在收到首包探测字节后真实自增 ok 计数
	go func() {
		for {
			c, err := lK8s.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 16)
			_, _ = c.Read(buf)
			atomic.StoreInt64(&k8sOKCount, 1)
			_ = c.Close()
		}
	}()
	go func() {
		for {
			c, err := lSvc.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 16)
			_, _ = c.Read(buf)
			atomic.StoreInt64(&svcOKCount, 1)
			_ = c.Close()
		}
	}()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statuses := []*agent.TunnelResourceStatus{
			{
				ResourceID:          "k8s-api",
				Path:                "host_direct",
				LocalPort:           int32(k8sPort),
				SVCProxyOK:          atomic.LoadInt64(&k8sOKCount),
				SVCProxyRejected:    0,
				DirectDialViolation: 0,
			},
			{
				ResourceID:          "beagle-web-res",
				ServiceName:         "beagle-web",
				Namespace:           "beagle-core",
				PortNumber:          80,
				Path:                "svcproxy",
				LocalPort:           int32(svcPort),
				SVCProxyOK:          atomic.LoadInt64(&svcOKCount),
				SVCProxyRejected:    0,
				DirectDialViolation: 0,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(statuses)
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	code := runAgentTunnelVerifyWithOutput([]string{"-statusz-url", ts.URL}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s, stdout: %s", code, stderr.String(), stdout.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "[D2-3]") || !strings.Contains(out, "direct_dial_violation = 0") {
		t.Errorf("missing D2-3 in output: %s", out)
	}
	if !strings.Contains(out, "[D2-4]") || !strings.Contains(out, "k8s-api") {
		t.Errorf("missing D2-4 in output: %s", out)
	}
	if !strings.Contains(out, "[D2-2]") || !strings.Contains(out, "beagle-web.beagle-core") {
		t.Errorf("missing D2-2 in output: %s", out)
	}
	if !strings.Contains(out, "RESULT: PASS") {
		t.Errorf("expected RESULT: PASS, got:\n%s", out)
	}
}

// TestAgentTunnelVerify_RejectFail (R2 反例测试) 验证本地端口可连但被 Agent 拒绝时，必须判定为 FAIL 且退出码为 1
func TestAgentTunnelVerify_RejectFail(t *testing.T) {
	// 启动一个可正常拨通的本地监听端口
	lSvc, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen for svc mock: %v", err)
	}
	defer lSvc.Close()
	svcPort := lSvc.Addr().(*net.TCPAddr).Port

	var rejectedCount int64 = 0
	go func() {
		for {
			c, err := lSvc.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 16)
			_, _ = c.Read(buf)
			// 模拟 Agent 拒绝流，导致 rejected 自增而 ok 不自增
			atomic.StoreInt64(&rejectedCount, 1)
			_ = c.Close()
		}
	}()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statuses := []*agent.TunnelResourceStatus{
			{
				ResourceID:          "beagle-svc-reject",
				ServiceName:         "backend-svc",
				Namespace:           "prod",
				PortNumber:          8080,
				Path:                "svcproxy",
				LocalPort:           int32(svcPort),
				SVCProxyOK:          0, // ok 恒为 0
				SVCProxyRejected:    atomic.LoadInt64(&rejectedCount),
				LastSVCProxyError:   "permission denied: service revoked",
				DirectDialViolation: 0,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(statuses)
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	code := runAgentTunnelVerifyWithOutput([]string{"-statusz-url", ts.URL}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 when Agent rejects connection, got %d. stdout:\n%s", code, stdout.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "RESULT: FAIL") {
		t.Errorf("expected RESULT: FAIL, got:\n%s", out)
	}
	if !strings.Contains(out, "被对端 Agent 拒绝: permission denied: service revoked") {
		t.Errorf("expected rejection reason in output, got:\n%s", out)
	}
}

func TestAgentTunnelVerify_ViolationFail(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statuses := []*agent.TunnelResourceStatus{
			{
				ResourceID:          "bad-res",
				ServiceName:         "bad-svc",
				Namespace:           "default",
				Path:                "svcproxy",
				LocalPort:           0,
				DirectDialViolation: 2, // 违规直连计数 > 0
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(statuses)
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	code := runAgentTunnelVerifyWithOutput([]string{"-statusz-url", ts.URL}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}

	out := stdout.String()
	if !strings.Contains(out, "RESULT: FAIL") {
		t.Errorf("expected RESULT: FAIL, got:\n%s", out)
	}
	if !strings.Contains(out, "发生违规直连") {
		t.Errorf("expected violation message, got:\n%s", out)
	}
}

func TestAgentTunnelVerify_JSONOutput(t *testing.T) {
	var k8sOK int64 = 0
	lK8s, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer lK8s.Close()
	k8sPort := lK8s.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			c, err := lK8s.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 16)
			_, _ = c.Read(buf)
			atomic.StoreInt64(&k8sOK, 1)
			_ = c.Close()
		}
	}()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statuses := []*agent.TunnelResourceStatus{
			{
				ResourceID:          "k8s-api",
				Path:                "host_direct",
				LocalPort:           int32(k8sPort),
				SVCProxyOK:          atomic.LoadInt64(&k8sOK),
				SVCProxyRejected:    0,
				DirectDialViolation: 0,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(statuses)
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	code := runAgentTunnelVerifyWithOutput([]string{"-statusz-url", ts.URL, "-json"}, &stdout, &stderr)
	// B4：仅有 k8s-api、没有任何业务端口被探测时，D2-0 必须 FAIL，不得空集通过
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d. stderr: %s, stdout: %s", code, stderr.String(), stdout.String())
	}

	var report AgentTunnelVerifyReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("failed to decode JSON output: %v, raw:\n%s", err, stdout.String())
	}
	if report.Overall != "FAIL" {
		t.Errorf("expected report.Overall = FAIL, got: %s", report.Overall)
	}
	if len(report.Checks) != 3 { // D2-0, D2-3 and D2-4
		t.Fatalf("expected 3 checks, got %d", len(report.Checks))
	}
	if report.Checks[0].ID != "D2-0" || report.Checks[0].Passed {
		t.Errorf("expected D2-0 coverage check to fail, got %+v", report.Checks[0])
	}
	for _, c := range report.Checks[1:] {
		if !c.Passed {
			t.Errorf("expected %s to pass, got %+v", c.ID, c)
		}
	}
}

// TestAgentTunnelVerify_ZeroProbesFails (B4) statusz 为空 / 无 local_port 时必须 FAIL。
func TestAgentTunnelVerify_ZeroProbesFails(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statuses := []*agent.TunnelResourceStatus{{ResourceID: "res-studio", Path: "svcproxy", LocalPort: 0}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(statuses)
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	code := runAgentTunnelVerifyWithOutput([]string{"-statusz-url", ts.URL, "-json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d. stdout: %s", code, stdout.String())
	}
	var report AgentTunnelVerifyReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("failed to decode JSON output: %v", err)
	}
	if report.Overall != "FAIL" || report.Checks[0].ID != "D2-0" || report.Checks[0].Passed {
		t.Fatalf("expected D2-0 FAIL, got %+v", report)
	}
}
