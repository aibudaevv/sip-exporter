package exporter

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/aibudaevv/sip-exporter/internal/carriers"
	"github.com/aibudaevv/sip-exporter/internal/geoip"
)

// ==================== Benchmark for parseRawPacket ====================

func BenchmarkParseRawPacket_INVITE(b *testing.B) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
	}

	packet := buildTestPacket("INVITE sip:1001@192.168.0.89 SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 192.168.0.89:49375;rport\r\n" +
		"From: <sip:1000@192.168.0.89>;tag=21e4850e69de4f50a3f96a8051e1af35\r\n" +
		"To: <sip:1001@192.168.0.89>\r\n" +
		"Call-ID: 618e627cb7eb4275a9addb1c6b639656\r\n" +
		"CSeq: 9217 INVITE\r\n" +
		"Contact: <sip:1000@192.168.0.89:49375;ob>\r\n" +
		"Max-Forwards: 70\r\n" +
		"User-Agent: MicroSIP/3.22.3\r\n" +
		"Content-Type: application/sdp\r\n" +
		"\r\n")

	b.ResetTimer()
	for range b.N {
		_, _ = e.parseRawPacket(packet)
	}
}

func BenchmarkParseRawPacket_200OK(b *testing.B) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
	}

	packet := buildTestPacket("SIP/2.0 200 OK\r\n" +
		"Via: SIP/2.0/UDP 192.168.0.89:49375;rport=49375\r\n" +
		"From: <sip:1000@192.168.0.89>;tag=e2540aafe5474bd7a5f9059b0ffccfec\r\n" +
		"To: <sip:1000@192.168.0.89>;tag=8Xy7r28Ne5ZSQ\r\n" +
		"Call-ID: 583ce713cb324f27bd614e594db53cc2\r\n" +
		"CSeq: 6596 INVITE\r\n" +
		"Session-Expires: 1800;refresher=uac\r\n" +
		"User-Agent: MicroSIP/3.22.3\r\n" +
		"\r\n")

	b.ResetTimer()
	for range b.N {
		_, _ = e.parseRawPacket(packet)
	}
}

func BenchmarkParseRawPacket_BYE(b *testing.B) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
	}

	packet := buildTestPacket("BYE sip:1000@192.168.0.89:49375 SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 192.168.0.89:5060;rport\r\n" +
		"From: <sip:1001@192.168.0.89>;tag=abc123\r\n" +
		"To: <sip:1000@192.168.0.89>;tag=xyz789\r\n" +
		"Call-ID: test-call-123\r\n" +
		"CSeq: 1 BYE\r\n" +
		"\r\n")

	b.ResetTimer()
	for range b.N {
		_, _ = e.parseRawPacket(packet)
	}
}

func BenchmarkParseRawPacket_REGISTER(b *testing.B) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
	}

	packet := buildTestPacket("REGISTER sip:192.168.0.89:5060 SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 192.168.0.89:49375;rport\r\n" +
		"From: <sip:1000@192.168.0.89>;tag=e2540aafe5474bd7a5f9059b0ffccfec\r\n" +
		"To: <sip:1000@192.168.0.89>\r\n" +
		"Call-ID: 583ce713cb324f27bd614e594db53cc2\r\n" +
		"CSeq: 6596 REGISTER\r\n" +
		"User-Agent: MicroSIP/3.22.3\r\n" +
		"Expires: 3600\r\n" +
		"\r\n")

	b.ResetTimer()
	for range b.N {
		_, _ = e.parseRawPacket(packet)
	}
}

func BenchmarkParseRawPacket_401Unauthorized(b *testing.B) {
	e := &exporter{
		services: services{
			metricser: &mockMetricser{},
			dialoger:  &mockDialoger{},
		},
	}

	packet := buildTestPacket("SIP/2.0 401 Unauthorized\r\n" +
		"Via: SIP/2.0/UDP 192.168.0.89:49375;rport=49375\r\n" +
		"From: <sip:1000@192.168.0.89>;tag=e2540aafe5474bd7a5f9059b0ffccfec\r\n" +
		"To: <sip:1000@192.168.0.89>;tag=8Xy7r28Ne5ZSQ\r\n" +
		"Call-ID: 583ce713cb324f27bd614e594db53cc2\r\n" +
		"CSeq: 6596 REGISTER\r\n" +
		"WWW-Authenticate: Digest realm=\"asterisk\",nonce=\"abc123\"\r\n" +
		"\r\n")

	b.ResetTimer()
	for range b.N {
		_, _ = e.parseRawPacket(packet)
	}
}

// ==================== Benchmark for sipPacketParse ====================

func BenchmarkSIPPacketParse_INVITE(b *testing.B) {
	e := exporter{}

	input := []byte("INVITE sip:1001@192.168.0.89 SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 192.168.0.89:49375;rport\r\n" +
		"From: <sip:1000@192.168.0.89>;tag=21e4850e69de4f50a3f96a8051e1af35\r\n" +
		"To: <sip:1001@192.168.0.89>\r\n" +
		"Call-ID: 618e627cb7eb4275a9addb1c6b639656\r\n" +
		"CSeq: 9217 INVITE\r\n" +
		"Contact: <sip:1000@192.168.0.89:49375;ob>\r\n" +
		"Max-Forwards: 70\r\n" +
		"\r\n")

	b.ResetTimer()
	for range b.N {
		_, _ = e.sipPacketParse(input)
	}
}

func BenchmarkSIPPacketParse_200OK(b *testing.B) {
	e := exporter{}

	input := []byte("SIP/2.0 200 OK\r\n" +
		"Via: SIP/2.0/UDP 192.168.0.89:49375;rport=49375\r\n" +
		"From: <sip:1000@192.168.0.89>;tag=e2540aafe5474bd7a5f9059b0ffccfec\r\n" +
		"To: <sip:1000@192.168.0.89>;tag=8Xy7r28Ne5ZSQ\r\n" +
		"Call-ID: 583ce713cb324f27bd614e594db53cc2\r\n" +
		"CSeq: 6596 INVITE\r\n" +
		"Session-Expires: 1800;refresher=uac\r\n" +
		"\r\n")

	b.ResetTimer()
	for range b.N {
		_, _ = e.sipPacketParse(input)
	}
}

func BenchmarkSIPPacketParse_REGISTER(b *testing.B) {
	e := exporter{}

	input := []byte("REGISTER sip:192.168.0.89:5060 SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 192.168.0.89:49375;rport\r\n" +
		"From: <sip:1000@192.168.0.89>;tag=e2540aafe5474bd7a5f9059b0ffccfec\r\n" +
		"To: <sip:1000@192.168.0.89>\r\n" +
		"Call-ID: 583ce713cb324f27bd614e594db53cc2\r\n" +
		"CSeq: 6596 REGISTER\r\n" +
		"Expires: 3600\r\n" +
		"\r\n")

	b.ResetTimer()
	for range b.N {
		_, _ = e.sipPacketParse(input)
	}
}

// ==================== Benchmark for helper functions ====================

func BenchmarkNormalizeDialogID(b *testing.B) {
	callID := []byte("583ce713cb324f27bd614e594db53cc2")
	fromTag := []byte("e2540aafe5474bd7a5f9059b0ffccfec")
	toTag := []byte("8Xy7r28Ne5ZSQ")

	b.ResetTimer()
	for range b.N {
		_, _ = normalizeDialogID(callID, fromTag, toTag)
	}
}

func BenchmarkExtractTag(b *testing.B) {
	value := []byte("<sip:user@domain>;tag=abc123xyz;other=param")

	b.ResetTimer()
	for range b.N {
		_ = extractTag(value)
	}
}

func BenchmarkExtractCSeq(b *testing.B) {
	value := []byte("9217 INVITE")

	b.ResetTimer()
	for range b.N {
		_, _ = extractCSeq(value)
	}
}

func BenchmarkSplitHeader(b *testing.B) {
	line := []byte("Session-Expires: 1800;refresher=uac")

	b.ResetTimer()
	for range b.N {
		_, _ = splitHeader(line)
	}
}

func BenchmarkExtractSessionExpires(b *testing.B) {
	value := []byte("1800;refresher=uac")

	b.ResetTimer()
	for range b.N {
		_ = extractSessionExpires(value)
	}
}

// ==================== Benchmark for handleMessage ====================

func BenchmarkHandleMessage_INVITE(b *testing.B) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
	}

	input := []byte("INVITE sip:test SIP/2.0\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>\r\n" +
		"Call-ID: test\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"\r\n")

	b.ResetTimer()
	for range b.N {
		_ = e.handleMessage("other", "", input)
	}
}

func BenchmarkHandleMessage_200OK_INVITE(b *testing.B) {
	mm := &mockMetricser{}
	md := &mockDialoger{}

	e := &exporter{
		services: services{
			metricser: mm,
			dialoger:  md,
		},
	}

	input := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:user@domain>;tag=abc\r\n" +
		"To: <sip:other@domain>;tag=xyz\r\n" +
		"Call-ID: test-call\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Session-Expires: 3600\r\n" +
		"\r\n")

	b.ResetTimer()
	for range b.N {
		_ = e.handleMessage("other", "", input)
	}
}

// ==================== Benchmark for label resolution cost ====================

func BenchmarkParseRawPacket_INVITE_Labels(b *testing.B) {
	sipINVITE := "INVITE sip:+74951234567@carrier.example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 192.168.0.89:49375;rport\r\n" +
		"From: <sip:1000@192.168.1.1>;tag=21e4850e69de4f50a3f96a8051e1af35\r\n" +
		"To: <sip:+74951234567@carrier.example.com>\r\n" +
		"Call-ID: 618e627cb7eb4275a9addb1c6b639656\r\n" +
		"CSeq: 9217 INVITE\r\n" +
		"Contact: <sip:1000@192.168.0.89:49375;ob>\r\n" +
		"Max-Forwards: 70\r\n" +
		"User-Agent: MicroSIP/3.22.3\r\n" +
		"Content-Type: application/sdp\r\n" +
		"\r\n"

	resolverWithCountry, _ := carriers.NewResolver([]carriers.Carrier{
		{Name: "test-carrier", Country: "RU", CIDRs: []string{"192.168.0.0/16"}},
	})
	resolverNoCountry, _ := carriers.NewResolver([]carriers.Carrier{
		{Name: "test-carrier", CIDRs: []string{"81.0.0.0/8"}},
	})

	_, filename, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	dbPath := filepath.Join(repoRoot, "test", "e2e", "data", "GeoIP2-Country-Test.mmdb")
	geoipReader, err := geoip.New(dbPath)
	if err != nil {
		b.Skipf("GeoIP test DB not available: %v", err)
	}

	mock := &mockMetricser{}
	dlg := &mockDialoger{}

	newBenchExporter := func() *exporter {
		return &exporter{
			services:        services{metricser: mock, dialoger: dlg},
			inviteTracker:   make(map[string]inviteEntry),
			registerTracker: make(map[string]registerEntry),
			inviteSDP:       make(map[string]inviteSDPEntity),
			optionsTracker:  make(map[string]optionsEntry),
		}
	}

	type benchCase struct {
		name   string
		e      *exporter
		packet []byte
	}

	e1 := newBenchExporter()
	e2 := newBenchExporter()
	e2.carrierResolver = resolverWithCountry
	e2.localCountryCode = "RU"

	e3 := newBenchExporter()
	e3.carrierResolver = resolverNoCountry
	e3.geoip = geoipReader
	e3.localCountryCode = "RU"

	e4 := newBenchExporter()
	e4.carrierResolver = resolverWithCountry
	e4.geoip = geoipReader
	e4.localCountryCode = "RU"

	cases := []benchCase{
		{name: "NoResolver", e: e1, packet: buildTestPacket(sipINVITE)},
		{
			name:   "CarrierCountry",
			e:      e2,
			packet: buildTestPacketWithIPs(sipINVITE, [4]byte{192, 168, 1, 50}, [4]byte{10, 0, 0, 1}),
		},
		{
			name:   "GeoIPLookup",
			e:      e3,
			packet: buildTestPacketWithIPs(sipINVITE, [4]byte{81, 2, 69, 142}, [4]byte{10, 0, 0, 1}),
		},
		{
			name:   "CarrierCountry_GeoIPLoaded",
			e:      e4,
			packet: buildTestPacketWithIPs(sipINVITE, [4]byte{192, 168, 1, 50}, [4]byte{10, 0, 0, 1}),
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_, _ = tc.e.parseRawPacket(tc.packet)
			}
		})
	}
}

// ==================== Helper functions ====================

// buildTestPacket builds full Ethernet+IP+UDP+SIP packet for benchmarks.
func buildTestPacket(sip string) []byte {
	packet := make([]byte, 14+20+8+len(sip))

	// Ethernet header (14 bytes)
	packet[12] = 0x08 // IPv4
	packet[13] = 0x00

	ipOffset := 14

	// IP header (20 bytes)
	packet[ipOffset] = 0x45 // IPv4, IHL=5
	packet[ipOffset+1] = 0x00
	packet[ipOffset+2] = 0x00 // Total Length (will be updated)
	packet[ipOffset+9] = 17   // UDP

	udpOffset := ipOffset + 20

	// UDP header (8 bytes)
	packet[udpOffset] = 0x00
	packet[udpOffset+4] = 0x00 // Length (will be updated)
	packet[udpOffset+5] = byte(len(sip) + 8)
	packet[udpOffset+6] = 0x00 // Checksum
	packet[udpOffset+7] = 0x00

	// SIP payload
	copy(packet[udpOffset+8:], []byte(sip))

	return packet
}

func buildTestPacketWithIPs(sip string, srcIP, dstIP [4]byte) []byte {
	packet := buildTestPacket(sip)
	const ipOffset = 14
	copy(packet[ipOffset+12:ipOffset+16], srcIP[:])
	copy(packet[ipOffset+16:ipOffset+20], dstIP[:])
	return packet
}
