package main

import (
	"net"
	"os"
	"strings"
	"testing"

	"github.com/pkg/sftp"
)

func TestEndpointHandlesSFTPChildBeforeNormalStartup(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	content := string(source)
	childAt := strings.Index(content, "runEndpointChildIfRequested()")
	flagsAt := strings.Index(content, `configPath := flag.String`)
	if childAt < 0 || flagsAt < 0 || childAt > flagsAt {
		t.Fatal("Endpoint must dispatch the SFTP child before flags, banners, logs, and config startup")
	}
}

func TestEmbeddedSFTPProtocol(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	serverDone := make(chan error, 1)
	go func() { serverDone <- serveEmbeddedSFTP(serverConn) }()

	client, err := sftp.NewClientPipe(clientConn, clientConn)
	if err != nil {
		t.Fatalf("initialize embedded SFTP client: %v", err)
	}
	if _, err := client.Getwd(); err != nil {
		t.Fatalf("SFTP realpath: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close SFTP client: %v", err)
	}
	_ = clientConn.Close()
	if err := <-serverDone; err != nil {
		t.Fatalf("embedded SFTP server: %v", err)
	}
}
