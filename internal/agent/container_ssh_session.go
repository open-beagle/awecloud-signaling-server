package agent

import (
	"context"
	"encoding/binary"
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

	rows, cols := uint16(24), uint16(80)
	for request := range requests {
		switch request.Type {
		case "pty-req":
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
			if request.WantReply {
				request.Reply(true, nil)
			}
			return opener.OpenShell(ctx, userName, resourceID, ContainerExecStream{
				Stdin: channel, Stdout: channel, Stderr: channel.Stderr(), Rows: rows, Cols: cols,
			})
		case "window-change":
			// The initial PTY size is applied by the current remotecommand stream.
			// A later revision will pass resize events through a TerminalSizeQueue.
			if request.WantReply {
				request.Reply(true, nil)
			}
		default:
			if request.WantReply {
				request.Reply(false, nil)
			}
		}
	}
	return fmt.Errorf("ContainerSSH session closed before shell request")
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
