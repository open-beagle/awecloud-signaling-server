package main

import (
	"io"
	"os"
	"os/exec"
	"sync"
	"testing"

	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
	"google.golang.org/protobuf/proto"
)

type fakeShellDataStream struct {
	mu       sync.Mutex
	received []*pb.ShellData
	sent     []*pb.ShellData
}

func (s *fakeShellDataStream) Send(message *pb.ShellData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, proto.Clone(message).(*pb.ShellData))
	return nil
}

func (s *fakeShellDataStream) Recv() (*pb.ShellData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.received) == 0 {
		return nil, io.EOF
	}
	message := s.received[0]
	s.received = s.received[1:]
	return message, nil
}

func TestNonPTYShellProcessPreservesStandardStreamsAndExitCode(t *testing.T) {
	if os.Getenv("BEAGLE_NON_PTY_HELPER") == "1" {
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(2)
		}
		_, _ = os.Stdout.Write(input)
		_, _ = os.Stdout.Write([]byte("stdout"))
		_, _ = os.Stderr.Write([]byte("stderr"))
		os.Exit(37)
	}

	input := []byte{0x00, 0x03, 0x7f, 'a', '\n'}
	stream := &fakeShellDataStream{received: []*pb.ShellData{
		{Data: input},
		{IsClose: true},
	}}
	cmd := exec.Command(os.Args[0], "-test.run=TestNonPTYShellProcessPreservesStandardStreamsAndExitCode", "--")
	cmd.Env = append(os.Environ(), "BEAGLE_NON_PTY_HELPER=1")

	handleNonPTYShellProcess(cmd, stream, &pb.ShellRequest{SessionId: "test", Login: "test", Command: "cat"})

	var stdout, stderr []byte
	var closeMessage *pb.ShellData
	for _, message := range stream.sent {
		if message.IsClose {
			closeMessage = message
			continue
		}
		if message.IsStderr {
			stderr = append(stderr, message.Data...)
		} else {
			stdout = append(stdout, message.Data...)
		}
	}
	wantStdout := append(append([]byte(nil), input...), []byte("stdout")...)
	if string(stdout) != string(wantStdout) {
		t.Fatalf("stdout = %v, want %v", stdout, wantStdout)
	}
	if string(stderr) != "stderr" {
		t.Fatalf("stderr = %q, want %q", stderr, "stderr")
	}
	if closeMessage == nil || closeMessage.ExitCode != 37 {
		t.Fatalf("close message = %#v, want exit code 37", closeMessage)
	}
}

func TestShouldAllocateShellPTY(t *testing.T) {
	for _, test := range []struct {
		name string
		req  *pb.ShellRequest
		want bool
	}{
		{name: "interactive shell", req: &pb.ShellRequest{}, want: true},
		{name: "exec without PTY", req: &pb.ShellRequest{Command: "true"}, want: false},
		{name: "exec with PTY", req: &pb.ShellRequest{Command: "true", Rows: 24, Cols: 80}, want: true},
		{name: "explicit pipes", req: &pb.ShellRequest{Mode: pb.ShellMode_SHELL_MODE_PIPES}, want: false},
		{name: "explicit PTY", req: &pb.ShellRequest{Command: "true", Mode: pb.ShellMode_SHELL_MODE_PTY}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldAllocateShellPTY(test.req); got != test.want {
				t.Fatalf("shouldAllocateShellPTY() = %t, want %t", got, test.want)
			}
		})
	}
}
