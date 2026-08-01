package agent

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// containerShellOpener keeps the SSH bridge independent from the Kubernetes
// implementation and makes its request policy testable.
type containerShellOpener interface {
	OpenShell(context.Context, string, string, ContainerExecStream) error
}

// ServeContainerSSHSession accepts only an interactive SSH shell. Resource ID
// and user identity are supplied by the authenticated connection handler; no
// SSH request can replace them with arbitrary Kubernetes parameters.
func ServeContainerSSHSession(ctx context.Context, channel ssh.Channel, requests <-chan *ssh.Request, opener containerShellOpener, userName, resourceID string) error {
	if opener == nil || userName == "" || resourceID == "" {
		return fmt.Errorf("ContainerSSH session is not configured")
	}
	defer channel.Close()

	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	resize := make(chan ContainerTerminalSize, 1)
	defer close(resize)
	result := make(chan error, 1)
	rows, cols := uint16(24), uint16(80)
	shellStarted := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-result:
			sendContainerSSHExitStatus(channel, err)
			return err
		case request, ok := <-requests:
			if !ok {
				return fmt.Errorf("ContainerSSH session closed before shell completed")
			}
			switch request.Type {
			case "pty-req":
				if shellStarted {
					if request.WantReply {
						request.Reply(false, nil)
					}
					continue
				}
				parsedRows, parsedCols, ok := parsePTYSize(request.Payload)
				if !ok {
					if request.WantReply {
						request.Reply(false, nil)
					}
					continue
				}
				rows, cols = parsedRows, parsedCols
				if request.WantReply {
					request.Reply(true, nil)
				}
			case "shell":
				if shellStarted {
					if request.WantReply {
						request.Reply(false, nil)
					}
					continue
				}
				shellStarted = true
				initialRows, initialCols := rows, cols
				if request.WantReply {
					request.Reply(true, nil)
				}
				go func() {
					result <- opener.OpenShell(sessionCtx, userName, resourceID, ContainerExecStream{
						Stdin: channel, Stdout: channel, Stderr: channel.Stderr(), Rows: initialRows, Cols: initialCols, Resize: resize,
					})
				}()
			case "window-change":
				parsedRows, parsedCols, ok := parseWindowSize(request.Payload)
				if !ok || !shellStarted {
					if request.WantReply {
						request.Reply(false, nil)
					}
					continue
				}
				offerTerminalSize(resize, ContainerTerminalSize{Rows: parsedRows, Cols: parsedCols})
				if request.WantReply {
					request.Reply(true, nil)
				}
			default:
				if request.WantReply {
					request.Reply(false, nil)
				}
			}
		}
	}
}

type containerSSHExitError interface {
	ExitStatus() int
}

func sendContainerSSHExitStatus(channel ssh.Channel, err error) {
	status := uint32(0)
	if err != nil {
		status = 255
		var exitErr containerSSHExitError
		if errors.As(err, &exitErr) {
			code := exitErr.ExitStatus()
			if code >= 0 {
				status = uint32(code)
			}
		}
	}
	_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{Status: status}))
}

func offerTerminalSize(queue chan ContainerTerminalSize, size ContainerTerminalSize) {
	select {
	case queue <- size:
		return
	default:
	}
	select {
	case <-queue:
	default:
	}
	queue <- size
}

func parseWindowSize(payload []byte) (rows, cols uint16, ok bool) {
	if len(payload) < 8 {
		return 0, 0, false
	}
	columns := binary.BigEndian.Uint32(payload[:4])
	lineRows := binary.BigEndian.Uint32(payload[4:8])
	if columns == 0 || lineRows == 0 || columns > 65535 || lineRows > 65535 {
		return 0, 0, false
	}
	return uint16(lineRows), uint16(columns), true
}

// parsePTYSize extracts columns and rows from RFC 4254 pty-req payload.
func parsePTYSize(payload []byte) (rows, cols uint16, ok bool) {
	if len(payload) < 12 {
		return 0, 0, false
	}
	termLength := int(binary.BigEndian.Uint32(payload[:4]))
	offset := 4 + termLength
	if termLength < 0 || offset < 4 || len(payload) < offset+8 {
		return 0, 0, false
	}
	columns := binary.BigEndian.Uint32(payload[offset : offset+4])
	lineRows := binary.BigEndian.Uint32(payload[offset+4 : offset+8])
	if columns == 0 || lineRows == 0 || columns > 65535 || lineRows > 65535 {
		return 0, 0, false
	}
	return uint16(lineRows), uint16(columns), true
}
