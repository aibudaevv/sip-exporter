package exporter

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// TestParseTimestampNS verifies the kernel SO_TIMESTAMPNS control message is
// parsed into the correct wall-clock time. The exporter's PDV computation
// depends on this timestamp being more accurate than a post-processing
// time.Now() call (S11-5 / F3).
func TestParseTimestampNS(t *testing.T) {
	wantSec := int64(1_700_000_000)
	wantNsec := int64(123_456_789)
	want := time.Unix(wantSec, wantNsec)

	// Build a SCM_TIMESTAMPNS cmsg payload: [Cmsghdr][Timespec{sec, nsec}].
	dataLen := tsCmsgLen
	oob := make([]byte, unix.CmsgSpace(dataLen))
	// Cmsghdr on linux amd64/arm64: Len uint64, Level int32, Type int32 (16 bytes).
	binary.NativeEndian.PutUint64(oob[0:8], uint64(unix.CmsgLen(dataLen)))
	binary.NativeEndian.PutUint32(oob[8:12], uint32(unix.SOL_SOCKET))
	binary.NativeEndian.PutUint32(oob[12:16], uint32(unix.SCM_TIMESTAMPNS))
	binary.NativeEndian.PutUint64(oob[16:24], uint64(wantSec))
	binary.NativeEndian.PutUint64(oob[24:32], uint64(wantNsec))

	got := parseTimestampNS(oob)
	require.True(t, got.Equal(want), "timestamp mismatch: want=%v got=%v", want, got)

	// Empty / malformed oob must yield zero time (caller falls back to time.Now()).
	require.True(t, parseTimestampNS(nil).IsZero(), "empty oob must be zero time")
	require.True(t, parseTimestampNS([]byte{0x01}).IsZero(), "garbage oob must be zero time")

	// A cmsg with a different type must be ignored (zero time).
	other := make([]byte, unix.CmsgSpace(dataLen))
	binary.NativeEndian.PutUint64(other[0:8], uint64(unix.CmsgLen(dataLen)))
	binary.NativeEndian.PutUint32(other[8:12], uint32(unix.SOL_SOCKET))
	binary.NativeEndian.PutUint32(other[12:16], uint32(0x9999)) // unrelated type
	require.True(t, parseTimestampNS(other).IsZero(), "non-timestamp cmsg must be zero time")
}
