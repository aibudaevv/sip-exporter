// Package rtcp parses RTCP compound packets (RFC 3550 §6) from raw UDP payloads
// for endpoint-reported quality metrics (jitter, loss, RTT). Only Sender Report
// (SR, PT 200) and Receiver Report (RR, PT 201) are parsed; SDES/BYE/APP and any
// other sub-packet types are iterated past by their length field without inspecting
// their contents. Parsing is bounds-checked at every step and never panics on
// truncated or malformed input — the caller (the single packet-consuming
// goroutine) has no recover(), so a panic would stall the whole exporter.
package rtcp

import (
	"encoding/binary"
	"errors"
)

// RTCP packet types (RFC 3550 §6.4-6.7).
const (
	PTSenderReport   uint8 = 200 // SR
	PTReceiverReport uint8 = 201 // RR
)

// Fixed field sizes (RFC 3550 §6).
const (
	versionShift   = 6
	version2       = 2
	reportCountBit = 0x1F // low 5 bits of the common header's first byte (RC)

	commonHeaderLen = 4  // V/P/RC | PT | length
	ptOffset        = 1  // packet-type byte within the common header
	lengthOffset    = 2  // 16-bit length field within the common header
	wordLen         = 4  // bytes per 32-bit word
	senderSSRCLen   = 4  // sender SSRC follows the common header
	senderInfoLen   = 20 // SR sender info: NTP(8) + RTP ts(4) + pkt count(4) + oct count(4)
	reportBlockLen  = 24 // one report block (RFC 3550 §6.4.1)

	rrMinLen = commonHeaderLen + senderSSRCLen                 // 8 (RR with 0 blocks)
	srMinLen = commonHeaderLen + senderSSRCLen + senderInfoLen // 28 (SR with 0 blocks)
)

var (
	// ErrInvalidRTCP is returned when the payload is too short or structurally
	// malformed (e.g. a length field that implies a sub-packet smaller than its
	// own header).
	ErrInvalidRTCP = errors.New("invalid RTCP: payload too short or malformed")
	// ErrNotRTCP is returned when the first sub-packet's version field is not 2.
	ErrNotRTCP = errors.New("not an RTCP packet: version is not 2")
	// ErrTruncated is returned when a declared sub-packet length exceeds the
	// remaining payload, or a report block overruns its sub-packet.
	ErrTruncated = errors.New("RTCP compound truncated: declared length exceeds payload")
)

// Report is one parsed SR or RR sub-packet. Blocks carries per-source reception
// statistics. The SR sender info (NTP/RTP timestamps, packet/octet counts) and
// the reporter SSRC are skipped — no current metric consumes them.
type Report struct {
	Type   uint8 // PTSenderReport or PTReceiverReport
	Blocks []ReportBlock
}

// ReportBlock is one reception report block (RFC 3550 §6.4.1) describing the
// reception of a single SSRC. SSRC identifies the source being reported on (the
// RTP stream we track); LSR+DLSR enable RTT computation; Jitter/FractionLost/
// CumulativeLost carry endpoint-observed quality.
type ReportBlock struct {
	SSRC           uint32
	FractionLost   uint8
	CumulativeLost int32 // signed 24-bit per RFC 3550 §6.4.1; negative when duplicates exceed losses
	Jitter         uint32
	LSR            uint32
	DLSR           uint32
}

// Parse decodes an RTCP compound payload into its SR/RR reports. Non-SR/RR
// sub-packets (SDES, BYE, APP, unknown) are skipped by length. The function
// validates bounds at every step and returns ErrInvalidRTCP, ErrNotRTCP, or
// ErrTruncated on malformed input without panicking. When a sub-packet is
// malformed, Parse salvages any valid reports already decoded and continues
// iterating subsequent sub-packets — the first error is returned but never
// suppresses later valid reports. A compound with no SR/RR (e.g. SDES-only)
// returns (nil, nil).
func Parse(payload []byte) ([]Report, error) {
	if len(payload) < commonHeaderLen {
		return nil, ErrInvalidRTCP
	}
	if payload[0]>>versionShift != version2 {
		return nil, ErrNotRTCP
	}

	var reports []Report
	var firstErr error
	off := 0
	for off < len(payload) {
		if off+commonHeaderLen > len(payload) {
			if firstErr == nil {
				firstErr = ErrTruncated
			}
			break
		}
		pt := payload[off+ptOffset]
		lengthWords := int(binary.BigEndian.Uint16(payload[off+lengthOffset : off+commonHeaderLen]))
		pktLen := (lengthWords + 1) * wordLen // RFC 3550: length is 32-bit words minus one
		if off+pktLen > len(payload) {
			if firstErr == nil {
				firstErr = ErrTruncated
			}
			break
		}

		if pt == PTSenderReport || pt == PTReceiverReport {
			rep, err := parseReport(payload[off:off+pktLen], pt)
			if err != nil {
				if len(rep.Blocks) > 0 {
					reports = append(reports, rep)
				}
				if firstErr == nil {
					firstErr = err
				}
			} else {
				reports = append(reports, rep)
			}
		}
		// SDES/BYE/APP and unknown PTs: skip — contents not needed.
		off += pktLen
	}
	return reports, firstErr
}

// parseReport decodes one SR or RR sub-packet already sliced to its declared
// length. RC (report count) bounds how many blocks are read; each block is also
// bounds-checked against the slice, so a lying RC cannot read out of bounds.
func parseReport(pkt []byte, pt uint8) (Report, error) {
	if len(pkt) < commonHeaderLen {
		return Report{}, ErrInvalidRTCP
	}
	rc := int(pkt[0] & reportCountBit)

	minLen := rrMinLen
	blockStart := rrMinLen
	if pt == PTSenderReport {
		minLen = srMinLen
		blockStart = srMinLen
	}
	if len(pkt) < minLen {
		return Report{}, ErrInvalidRTCP
	}

	rep := Report{
		Type:   pt,
		Blocks: make([]ReportBlock, 0, rc),
	}
	off := blockStart
	for range rc {
		if off+reportBlockLen > len(pkt) {
			return rep, ErrTruncated
		}
		rep.Blocks = append(rep.Blocks, parseReportBlock(pkt[off:off+reportBlockLen]))
		off += reportBlockLen
	}
	return rep, nil
}

// parseReportBlock decodes a 24-byte reception report block. Caller guarantees
// len(b) >= reportBlockLen.
func parseReportBlock(b []byte) ReportBlock {
	// RFC 3550 §6.4.1: cumulative number of packets lost is a SIGNED 24-bit
	// field (negative when duplicates exceed losses, e.g. 0xFFFFFF = -1).
	raw := uint32(b[5])<<16 | uint32(b[6])<<8 | uint32(b[7])
	cumLost := int32(raw)
	if raw&0x800000 != 0 {
		cumLost = int32(raw) - (1 << 24) // sign-extend 24-bit negative
	}
	return ReportBlock{
		SSRC:           binary.BigEndian.Uint32(b[0:4]),
		FractionLost:   b[4],
		CumulativeLost: cumLost,
		Jitter:         binary.BigEndian.Uint32(b[12:16]),
		LSR:            binary.BigEndian.Uint32(b[16:20]),
		DLSR:           binary.BigEndian.Uint32(b[20:24]),
	}
}
