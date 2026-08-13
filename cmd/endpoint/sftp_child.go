package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/pkg/sftp"
)

func runEndpointChildIfRequested() {
	if len(os.Args) < 2 || os.Args[1] != "be-child" {
		return
	}
	if len(os.Args) != 3 || os.Args[2] != "sftp" {
		fmt.Fprintln(os.Stderr, "unknown endpoint child mode")
		os.Exit(2)
	}
	if err := serveEmbeddedSFTP(stdioReadWriteCloser{}); err != nil {
		fmt.Fprintf(os.Stderr, "endpoint SFTP child failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func serveEmbeddedSFTP(rwc io.ReadWriteCloser) error {
	server, err := sftp.NewServer(rwc)
	if err != nil {
		return err
	}
	defer server.Close()
	if err := server.Serve(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

type stdioReadWriteCloser struct{}

func (stdioReadWriteCloser) Read(buffer []byte) (int, error)  { return os.Stdin.Read(buffer) }
func (stdioReadWriteCloser) Write(buffer []byte) (int, error) { return os.Stdout.Write(buffer) }
func (stdioReadWriteCloser) Close() error                     { return nil }
