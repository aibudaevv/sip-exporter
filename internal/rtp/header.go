// Package rtp parses RTP (RFC 3550) fixed headers from raw packet bytes.
package rtp

import (
	"encoding/binary"
	"errors"
)

var (
	// ErrInvalidRTP is returned when a packet is too short for an RTP header.
	ErrInvalidRTP = errors.New("invalid RTP header: too short")
	// ErrNotRTP is returned when the RTP version field is not 2.
	ErrNotRTP = errors.New("not an RTP packet: version is not 2")
)

const (
	rtpVersion2     = 2
	minRTPHeaderLen = 12

	versionShift = 6
	paddingMask  = 1 << 5
	extMask      = 1 << 4
	csrcMask     = 0x0F
	markerMask   = 1 << 7
	payloadMask  = 0x7F
)

// Header is the parsed minimal RTP header (RFC 3550). Payload is not captured.
type Header struct {
	Version        uint8
	Padding        bool
	Extension      bool
	CSRCCount      uint8
	Marker         bool
	PayloadType    uint8
	SequenceNumber uint16
	Timestamp      uint32
	SSRC           uint32
}

// ParseHeader parses the 12-byte fixed RTP header (RFC 3550 §5.1).
// Returns ErrNotRTP if the version field is not 2, ErrInvalidRTP if data is too short.
func ParseHeader(data []byte) (Header, error) {
	if len(data) < minRTPHeaderLen {
		return Header{}, ErrInvalidRTP
	}

	if data[0]>>versionShift != rtpVersion2 {
		return Header{}, ErrNotRTP
	}

	return Header{
		Version:        rtpVersion2,
		Padding:        data[0]&paddingMask != 0,
		Extension:      data[0]&extMask != 0,
		CSRCCount:      data[0] & csrcMask,
		Marker:         data[1]&markerMask != 0,
		PayloadType:    data[1] & payloadMask,
		SequenceNumber: binary.BigEndian.Uint16(data[2:4]),
		Timestamp:      binary.BigEndian.Uint32(data[4:8]),
		SSRC:           binary.BigEndian.Uint32(data[8:12]),
	}, nil
}
