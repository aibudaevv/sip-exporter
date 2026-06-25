package rtp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseHeader_Valid(t *testing.T) {
	// V=2, P=0, X=0, CC=0 | M=1, PT=8 (PCMA) | seq=0x1234 | ts=0x0A0B0C0D | ssrc=0x11223344
	data := []byte{
		0x80,       // V=2, P=0, X=0, CC=0
		0x88,       // M=1, PT=8
		0x12, 0x34, // seq = 4660
		0x0A, 0x0B, 0x0C, 0x0D, // timestamp
		0x11, 0x22, 0x33, 0x44, // SSRC
	}
	h, err := ParseHeader(data)

	require.NoError(t, err)
	require.Equal(t, uint8(2), h.Version)
	require.False(t, h.Padding)
	require.False(t, h.Extension)
	require.Equal(t, uint8(0), h.CSRCCount)
	require.True(t, h.Marker)
	require.Equal(t, uint8(8), h.PayloadType)
	require.Equal(t, uint16(0x1234), h.SequenceNumber)
	require.Equal(t, uint32(0x0A0B0C0D), h.Timestamp)
	require.Equal(t, uint32(0x11223344), h.SSRC)
}

func TestParseHeader_VersionNotTwo(t *testing.T) {
	// V=0
	data := []byte{0x00, 0x08, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	_, err := ParseHeader(data)
	require.ErrorIs(t, err, ErrNotRTP)

	// V=1
	data[0] = 0x40
	_, err = ParseHeader(data)
	require.ErrorIs(t, err, ErrNotRTP)

	// V=3
	data[0] = 0xC0
	_, err = ParseHeader(data)
	require.ErrorIs(t, err, ErrNotRTP)
}

func TestParseHeader_TooShort(t *testing.T) {
	data := []byte{0x80, 0x08, 0, 0}
	_, err := ParseHeader(data)
	require.ErrorIs(t, err, ErrInvalidRTP)
}

func TestParseHeader_PaddingExtensionCSRC(t *testing.T) {
	// V=2, P=1, X=1, CC=3 | PT=0
	data := []byte{
		0xB3, // V=2(0x80), P=1(0x20), X=1(0x10), CC=3(0x03) → 0xB3
		0x00, // M=0, PT=0 (PCMU)
		0x00, 0x01,
		0x00, 0x00, 0x00, 0x05,
		0xAA, 0xBB, 0xCC, 0xDD,
	}
	h, err := ParseHeader(data)
	require.NoError(t, err)
	require.True(t, h.Padding)
	require.True(t, h.Extension)
	require.Equal(t, uint8(3), h.CSRCCount)
	require.Equal(t, uint8(0), h.PayloadType)
}
