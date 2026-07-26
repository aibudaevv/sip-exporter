package rtcp

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// makeBlock builds a 24-byte RTCP report block (RFC 3550 §6.4.1).
func makeBlock(ssrc uint32, fracLost uint8, cumLost uint32, extSeq, jitter, lsr, dlsr uint32) []byte {
	b := make([]byte, 24)
	binary.BigEndian.PutUint32(b[0:4], ssrc)
	b[4] = fracLost
	b[5] = byte(cumLost >> 16) // 24-bit cumulative number of packets lost
	b[6] = byte(cumLost >> 8)
	b[7] = byte(cumLost)
	binary.BigEndian.PutUint32(b[8:12], extSeq)
	binary.BigEndian.PutUint32(b[12:16], jitter)
	binary.BigEndian.PutUint32(b[16:20], lsr)
	binary.BigEndian.PutUint32(b[20:24], dlsr)
	return b
}

// makeRR builds a Receiver Report (PT 201) with the given sender SSRC and blocks.
func makeRR(senderSSRC uint32, blocks ...[]byte) []byte {
	rc := len(blocks)
	pktLen := 4 + 4 + rc*24 // header + sender SSRC + blocks
	b := make([]byte, pktLen)
	b[0] = 0x80 | byte(rc) // V=2, P=0, RC=block count
	b[1] = PTReceiverReport
	binary.BigEndian.PutUint16(b[2:4], uint16(pktLen/4-1))
	binary.BigEndian.PutUint32(b[4:8], senderSSRC)
	off := 8
	for _, blk := range blocks {
		copy(b[off:], blk)
		off += 24
	}
	return b
}

// makeSR builds a Sender Report (PT 200) with sender info and blocks.
func makeSR(senderSSRC uint32, ntpTS uint64, rtpTS, pktCount, octCount uint32, blocks ...[]byte) []byte {
	rc := len(blocks)
	pktLen := 4 + 4 + 20 + rc*24 // header + sender SSRC + sender info + blocks
	b := make([]byte, pktLen)
	b[0] = 0x80 | byte(rc)
	b[1] = PTSenderReport
	binary.BigEndian.PutUint16(b[2:4], uint16(pktLen/4-1))
	binary.BigEndian.PutUint32(b[4:8], senderSSRC)
	binary.BigEndian.PutUint64(b[8:16], ntpTS)
	binary.BigEndian.PutUint32(b[16:20], rtpTS)
	binary.BigEndian.PutUint32(b[20:24], pktCount)
	binary.BigEndian.PutUint32(b[24:28], octCount)
	off := 28
	for _, blk := range blocks {
		copy(b[off:], blk)
		off += 24
	}
	return b
}

// makeRawPT builds an RTCP sub-packet with a given PT and body length, with dummy
// body bytes. Used to exercise skipping of non-SR/RR packet types (SDES/BYE/APP).
func makeRawPT(pt uint8, bodyWords int) []byte {
	pktLen := (bodyWords + 1) * 4
	b := make([]byte, pktLen)
	b[0] = 0x80 // V=2, P=0, RC=0
	b[1] = pt
	binary.BigEndian.PutUint16(b[2:4], uint16(pktLen/4-1))
	for i := 4; i < pktLen; i++ {
		b[i] = 0xAB // nonzero dummy body
	}
	return b
}

func TestParse_ReceiverReport(t *testing.T) {
	blk := makeBlock(0x11111111, 10, 1500, 0x2222, 250, 0x83B4A5C9, 0x00001000)
	reports, err := Parse(makeRR(0xDEADBEEF, blk))
	require.NoError(t, err)
	require.Len(t, reports, 1)

	r := reports[0]
	require.Equal(t, PTReceiverReport, r.Type)
	require.Len(t, r.Blocks, 1)

	b := r.Blocks[0]
	require.Equal(t, uint32(0x11111111), b.SSRC)
	require.Equal(t, uint8(10), b.FractionLost)
	require.Equal(t, uint32(1500), b.CumulativeLost, "24-bit cumulative lost")
	require.Equal(t, uint32(250), b.Jitter)
	require.Equal(t, uint32(0x83B4A5C9), b.LSR)
	require.Equal(t, uint32(0x00001000), b.DLSR)
}

func TestParse_SenderReport(t *testing.T) {
	blk := makeBlock(0x22222222, 5, 42, 0x3333, 90, 0, 0)
	reports, err := Parse(makeSR(0xCAFEBABE, 0x83B4A5C9E1D2C3F4, 123456, 99, 7000, blk))
	require.NoError(t, err)
	require.Len(t, reports, 1)

	r := reports[0]
	require.Equal(t, PTSenderReport, r.Type)
	require.Len(t, r.Blocks, 1, "SR carries its reception report blocks after sender info")
	require.Equal(t, uint32(0x22222222), r.Blocks[0].SSRC)
}

func TestParse_CompoundSRThenSDESSkipsNonReport(t *testing.T) {
	sr := makeSR(0xAAAA, 1, 1, 1, 1)
	sdes := makeRawPT(202, 3) // SDES, 3 words body — contents must be skipped, not parsed
	reports, err := Parse(append(sr, sdes...))
	require.NoError(t, err)
	require.Len(t, reports, 1, "only the SR must be reported; SDES skipped by length")
	require.Equal(t, PTSenderReport, reports[0].Type)
}

func TestParse_MultipleBlocks(t *testing.T) {
	b1 := makeBlock(0x100, 1, 10, 1000, 5, 0, 0)
	b2 := makeBlock(0x200, 2, 20, 2000, 6, 0x1111, 0x2222)
	reports, err := Parse(makeRR(0xBEEF, b1, b2))
	require.NoError(t, err)
	require.Len(t, reports, 1)
	require.Len(t, reports[0].Blocks, 2)
	require.Equal(t, uint32(0x100), reports[0].Blocks[0].SSRC)
	require.Equal(t, uint32(0x200), reports[0].Blocks[1].SSRC)
}

func TestParse_ZeroBlockCount(t *testing.T) {
	reports, err := Parse(makeRR(0x1)) // RR with RC=0
	require.NoError(t, err)
	require.Len(t, reports, 1)
	require.Empty(t, reports[0].Blocks)
}

func TestParse_SDESOnlyReturnsNoReports(t *testing.T) {
	reports, err := Parse(makeRawPT(202, 2))
	require.NoError(t, err, "a valid compound with no SR/RR is not an error")
	require.Empty(t, reports)
}

func TestParse_TruncatedDeclaredLength(t *testing.T) {
	// Declare a length (4 words = 16 bytes) but provide only 8 bytes.
	short := []byte{0x80, PTReceiverReport, 0x00, 0x04, 0x00, 0x00, 0x00, 0x01}
	_, err := Parse(short)
	require.ErrorIs(t, err, ErrTruncated)
}

func TestParse_TooShortForHeader(t *testing.T) {
	_, err := Parse([]byte{0x80, 0xC8, 0x00})
	require.ErrorIs(t, err, ErrInvalidRTCP)
}

func TestParse_EmptyPayload(t *testing.T) {
	_, err := Parse(nil)
	require.ErrorIs(t, err, ErrInvalidRTCP)
	_, err = Parse([]byte{})
	require.ErrorIs(t, err, ErrInvalidRTCP)
}

func TestParse_NotRTCPVersion(t *testing.T) {
	// V=0 in the first byte, but structurally a 4-byte header.
	_, err := Parse([]byte{0x00, PTReceiverReport, 0x00, 0x00})
	require.ErrorIs(t, err, ErrNotRTCP)
}

func TestParse_BlockCountLiesAboutBlocks(t *testing.T) {
	// RC=2 but only one block fits in the declared length. The parser must not
	// read past the sub-packet boundary; the second "block" must trigger ErrTruncated.
	oneBlock := makeBlock(0x1, 0, 0, 0, 0, 0, 0) // 24 bytes; second block missing → 8+24=32 declared
	pkt := make([]byte, 0, 4+4+24)
	pkt = append(pkt, 0x82, PTReceiverReport, 0x00, 0x07) // V=2, RC=2, length=7 (32 bytes total)
	pkt = append(pkt, 0, 0, 0, 0)                         // sender SSRC
	pkt = append(pkt, oneBlock...)                        // only one block; RC=2 lies
	_, err := Parse(pkt)
	require.ErrorIs(t, err, ErrTruncated, "lying RC must not read out of bounds")
}

func TestParse_TruncatedSRKeepsValidBlocks(t *testing.T) {
	// SR declares RC=2 but only 1 block fits — parseReport returns the partial
	// rep (1 valid block) plus ErrTruncated. Parse must salvage that block
	// instead of discarding it with the error.
	blk := makeBlock(0x12345678, 10, 500, 0x100, 200, 0xAABB, 0xCCDD)
	pkt := make([]byte, 0, srMinLen+24)
	pkt = append(pkt, 0xA2, PTSenderReport, 0x00, 0x0C) // V=2, RC=2, length=12 (52 bytes)
	pkt = append(pkt, 0, 0, 0, 0)                       // sender SSRC
	pkt = append(pkt, make([]byte, senderInfoLen)...)    // 20-byte sender info
	pkt = append(pkt, blk...)                           // 1 block; RC=2 lies
	reports, err := Parse(pkt)
	require.ErrorIs(t, err, ErrTruncated, "trailing truncation still reported")
	require.Len(t, reports, 1, "partial SR salvaged with valid blocks")
	require.Len(t, reports[0].Blocks, 1, "one valid block preserved")
	require.Equal(t, uint32(0x12345678), reports[0].Blocks[0].SSRC)
	require.Equal(t, uint32(200), reports[0].Blocks[0].Jitter)
}

func TestParse_CumulativeLostMaxPositive(t *testing.T) {
	// Maximum POSITIVE 24-bit signed value (0x7FFFFF = 8388607) round-trips.
	blk := makeBlock(0x1, 0, 0x7FFFFF, 0, 0, 0, 0)
	reports, err := Parse(makeRR(0x2, blk))
	require.NoError(t, err)
	require.Equal(t, uint32(0x7FFFFF), reports[0].Blocks[0].CumulativeLost)
}

func TestParse_NegativeCumulativeLostFlooredToZero(t *testing.T) {
	// RFC 3550 §6.4.1: cumulative number of packets lost is a SIGNED 24-bit field
	// (negative when duplicates exceed losses). A monitoring loss counter must
	// never go backwards, so negative values floor to zero instead of being
	// misread as huge positives (0xFFFFFF = -1 must NOT become 16777215).
	for _, tc := range []struct {
		name string
		raw  uint32
		want uint32
	}{
		{"max negative", 0xFFFFFF, 0}, // -1
		{"min negative", 0x800000, 0}, // -8388608
		{"max positive", 0x7FFFFF, 0x7FFFFF},
		{"zero", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blk := makeBlock(0x1, 0, tc.raw, 0, 0, 0, 0)
			reports, err := Parse(makeRR(0x2, blk))
			require.NoError(t, err)
			require.Equal(t, tc.want, reports[0].Blocks[0].CumulativeLost)
		})
	}
}

// FuzzParse ensures Parse never panics on arbitrary input AND that successful
// parses are deterministic and structurally valid. The caller's packet-consuming
// goroutine has no recover(); a panic would stall the whole exporter. Beyond the
// "no panic" contract, the fuzzer verifies: (1) parsing the same input twice
// yields identical results (rules out uninitialized-memory reads), and (2) on a
// successful parse every block's 24-bit cumulative-lost field fits in 24 bits
// (catches extraction off-by-ones that don't crash).
func FuzzParse(f *testing.F) {
	// Seeds: valid compounds, edge cases, and structural traps.
	f.Add(makeRR(0x1, makeBlock(0x2, 0, 0, 0, 0, 0, 0))) // 1-block RR
	f.Add(makeSR(0x1, 0, 0, 0, 0))                       // 0-block SR
	f.Add(append(makeSR(0x1, 0, 0, 0, 0), makeRawPT(202, 3)...))
	f.Add(makeRR(0x3, makeBlock(0x4, 0, 0xFFFFFF, 0, 0, 0, 0))) // negative cumulative (-1) → floored to 0
	f.Add(makeRR(0x5, maxReportBlocks(0x6)...))                 // RC=31 (5-bit max report count)
	f.Add(append(makeRR(0x7, makeBlock(0x8, 0, 0, 0, 0, 0, 0)),
		makeRR(0x9, makeBlock(0xA, 0, 0, 0, 0, 0, 0))...)) // multi-RR compound
	f.Add(append(append(makeRR(0xB, makeBlock(0xC, 0, 0, 0, 0, 0, 0)),
		makeRawPT(203, 2)...), makeRawPT(204, 2)...)) // RR+BYE+APP
	f.Add([]byte{0x80, 0xC8, 0x00, 0x04, 0x00, 0x00, 0x00, 0x01}) // truncated-declared-length RR
	// P=1 (padding bit set), RC=0: parser ignores P and advances by length.
	f.Add([]byte{0xA0, PTReceiverReport, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00, 0, 0, 0, 0})
	f.Add([]byte{0x80, PTReceiverReport, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}) // lengthWords=0 (pktLen=4)
	// Valid RR followed by a sub-packet with V=0 (parser does not re-check version
	// on trailing sub-packets — exercises iteration by length past non-V2 tails).
	f.Add(append(makeRR(0xD, makeBlock(0xE, 0, 0, 0, 0, 0, 0)),
		[]byte{0x00, 202, 0x00, 0x01, 0xAB, 0xAB, 0xAB, 0xAB}...))
	f.Add([]byte{0x00, 0x00, 0x00}) // short zeros
	f.Add([]byte{})                 // empty
	f.Fuzz(func(t *testing.T, data []byte) {
		r1, err1 := Parse(data)
		r2, err2 := Parse(data)
		// Determinism: the same input must produce the same outcome every time.
		require.Equal(t, err1 != nil, err2 != nil, "error-ness must be deterministic")
		require.Equal(t, r1, r2, "parsed reports must be deterministic")
		if err1 != nil {
			return
		}
		// Structural validity on successful parses: the cumulative-lost field
		// is a signed 24-bit value with negatives floored to 0, so it must lie
		// in [0, 0x7FFFFF] regardless of input bytes.
		for _, rep := range r1 {
			require.LessOrEqual(t, len(rep.Blocks), 31,
				"block count must not exceed 5-bit RC max")
			for _, blk := range rep.Blocks {
				require.LessOrEqual(t, blk.CumulativeLost, uint32(0x7FFFFF),
					"cumulative-lost must not exceed 0x7FFFFF (negative 24-bit floored to 0)")
			}
		}
	})
}

// maxReportBlocks returns 31 report blocks (the 5-bit RC maximum) for fuzz seeds.
func maxReportBlocks(ssrc uint32) [][]byte {
	blocks := make([][]byte, 31)
	for i := range blocks {
		blocks[i] = makeBlock(ssrc+uint32(i), 0, 0, 0, 0, 0, 0)
	}
	return blocks
}
