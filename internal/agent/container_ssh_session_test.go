package agent

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParsePTYSize(t *testing.T) {
	payload := make([]byte, 4+5+8)
	binary.BigEndian.PutUint32(payload[:4], 5)
	copy(payload[4:], "xterm")
	binary.BigEndian.PutUint32(payload[9:13], 132)
	binary.BigEndian.PutUint32(payload[13:17], 43)

	rows, cols, ok := parsePTYSize(payload)
	require.True(t, ok)
	require.Equal(t, uint16(43), rows)
	require.Equal(t, uint16(132), cols)
}

func TestParsePTYSizeRejectsMalformedOrOversizedRequest(t *testing.T) {
	_, _, ok := parsePTYSize([]byte{0, 0, 0, 5})
	require.False(t, ok)
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[:4], 0)
	binary.BigEndian.PutUint32(payload[4:8], 70000)
	binary.BigEndian.PutUint32(payload[8:12], 24)
	_, _, ok = parsePTYSize(payload)
	require.False(t, ok)
}

func TestParseWindowSize(t *testing.T) {
	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[:4], 160)
	binary.BigEndian.PutUint32(payload[4:], 50)
	rows, cols, ok := parseWindowSize(payload)
	require.True(t, ok)
	require.Equal(t, uint16(50), rows)
	require.Equal(t, uint16(160), cols)
}

func TestParseWindowSizeRejectsMalformedOrOversizedRequest(t *testing.T) {
	_, _, ok := parseWindowSize([]byte{0, 0, 0, 1})
	require.False(t, ok)
	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[:4], 70000)
	binary.BigEndian.PutUint32(payload[4:], 24)
	_, _, ok = parseWindowSize(payload)
	require.False(t, ok)
}
