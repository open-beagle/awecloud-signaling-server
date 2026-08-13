package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

const SSHBannerSocketPath = "/run/awecloud-signaling/ssh-banner.sock"

type SSHBannerInfo struct {
	RemoteUser       string
	DisplayName      string
	RemoteIP         string
	RemoteDeviceName string
	RemoteDeviceOS   string
}

func BuildSSHBanner(info SSHBannerInfo) string {
	var banner strings.Builder
	banner.WriteString("================================================================\r\n")
	banner.WriteString("           AWECloud Signaling - SSH Access\r\n")
	banner.WriteString("================================================================\r\n")
	banner.WriteString(fmt.Sprintf("  Version:      %s\r\n", version))
	banner.WriteString(fmt.Sprintf("  Build Date:   %s\r\n", buildDate))
	banner.WriteString(fmt.Sprintf("  Connect Time: %s\r\n", time.Now().Format("2006-01-02 15:04:05")))
	banner.WriteString("----------------------------------------------------------------\r\n")
	if info.DisplayName != "" {
		banner.WriteString(fmt.Sprintf("  Remote User:   %s , %s\r\n", info.RemoteUser, info.DisplayName))
	} else {
		banner.WriteString(fmt.Sprintf("  Remote User:   %s\r\n", info.RemoteUser))
	}
	deviceParts := []string{info.RemoteIP}
	if info.RemoteDeviceName != "" {
		deviceParts = append(deviceParts, info.RemoteDeviceName)
	}
	if info.RemoteDeviceOS != "" {
		deviceParts = append(deviceParts, info.RemoteDeviceOS)
	}
	banner.WriteString(fmt.Sprintf("  Remote Device: %s\r\n", strings.Join(deviceParts, " , ")))
	banner.WriteString("================================================================\r\n")
	return banner.String()
}

type SSHBannerServer struct {
	tsManager *TailscaleManager
	grpc      pb.AgentServiceClient
	listener  net.Listener
	mu        sync.Mutex
}

func NewSSHBannerServer(tsManager *TailscaleManager, grpcClient pb.AgentServiceClient) *SSHBannerServer {
	return &SSHBannerServer{tsManager: tsManager, grpc: grpcClient}
}

func (s *SSHBannerServer) Start() error {
	if err := os.MkdirAll(filepath.Dir(SSHBannerSocketPath), 0o755); err != nil {
		return err
	}
	_ = os.Remove(SSHBannerSocketPath)
	listener, err := net.Listen("unix", SSHBannerSocketPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(SSHBannerSocketPath, 0o666); err != nil {
		listener.Close()
		return err
	}
	s.listener = listener
	go s.acceptLoop(listener)
	return nil
}

func (s *SSHBannerServer) Stop() {
	s.mu.Lock()
	listener := s.listener
	s.listener = nil
	s.mu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
	_ = os.Remove(SSHBannerSocketPath)
}

func (s *SSHBannerServer) acceptLoop(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *SSHBannerServer) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
	line, err := bufio.NewReader(io.LimitReader(conn, 256)).ReadString('\n')
	if err != nil {
		return
	}
	remoteAddr := strings.TrimSpace(line)
	remoteHost, _, err := net.SplitHostPort(remoteAddr)
	if err != nil || net.ParseIP(remoteHost) == nil {
		return
	}
	info := SSHBannerInfo{RemoteIP: remoteHost}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	localClient, err := s.tsManager.LocalClient()
	if err == nil {
		if whois, whoisErr := localClient.WhoIs(ctx, remoteAddr); whoisErr == nil && whois.UserProfile != nil {
			info.RemoteUser, _ = parseHeadscaleUserName(whois.UserProfile.LoginName)
		}
	}
	if info.RemoteUser != "" && s.grpc != nil {
		if device, queryErr := s.grpc.GetUserDeviceInfo(ctx, &pb.GetUserDeviceInfoRequest{UserName: info.RemoteUser, DeviceIp: remoteHost}); queryErr == nil {
			info.DisplayName = device.DisplayName
			info.RemoteDeviceName = device.DeviceName
			info.RemoteDeviceOS = device.DeviceOs
		}
	}
	if info.RemoteUser == "" {
		info.RemoteUser = "unknown"
	}
	_, _ = conn.Write([]byte(BuildSSHBanner(info)))
}

func RequestSSHBanner(socketPath, remoteIP, remotePort string) (string, error) {
	address := net.JoinHostPort(strings.TrimSpace(remoteIP), strings.TrimSpace(remotePort))
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
	if _, err := fmt.Fprintln(conn, address); err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(conn, 16*1024))
	return string(data), err
}
