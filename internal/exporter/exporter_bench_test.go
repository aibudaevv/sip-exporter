package exporter

import (
	"testing"
)

// ==================== Benchmark для parseRawPacket ====================

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
	for i := 0; i < b.N; i++ {
		_ = e.parseRawPacket(packet)
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
	for i := 0; i < b.N; i++ {
		_ = e.parseRawPacket(packet)
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
	for i := 0; i < b.N; i++ {
		_ = e.parseRawPacket(packet)
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
	for i := 0; i < b.N; i++ {
		_ = e.parseRawPacket(packet)
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
	for i := 0; i < b.N; i++ {
		_ = e.parseRawPacket(packet)
	}
}

// ==================== Benchmark для sipPacketParse ====================

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
	for i := 0; i < b.N; i++ {
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
	for i := 0; i < b.N; i++ {
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
	for i := 0; i < b.N; i++ {
		_, _ = e.sipPacketParse(input)
	}
}

// ==================== Benchmark для вспомогательных функций ====================

func BenchmarkNormalizeDialogID(b *testing.B) {
	callID := []byte("583ce713cb324f27bd614e594db53cc2")
	fromTag := []byte("e2540aafe5474bd7a5f9059b0ffccfec")
	toTag := []byte("8Xy7r28Ne5ZSQ")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = normalizeDialogID(callID, fromTag, toTag)
	}
}

func BenchmarkExtractTag(b *testing.B) {
	value := []byte("<sip:user@domain>;tag=abc123xyz;other=param")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = extractTag(value)
	}
}

func BenchmarkExtractCSeq(b *testing.B) {
	value := []byte("9217 INVITE")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = extractCSeq(value)
	}
}

func BenchmarkSplitHeader(b *testing.B) {
	line := []byte("Session-Expires: 1800;refresher=uac")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = splitHeader(line)
	}
}

func BenchmarkExtractSessionExpires(b *testing.B) {
	value := []byte("1800;refresher=uac")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = extractSessionExpires(value)
	}
}

// ==================== Benchmark для handleMessage ====================

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
	for i := 0; i < b.N; i++ {
		_ = e.handleMessage(input)
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
	for i := 0; i < b.N; i++ {
		_ = e.handleMessage(input)
	}
}

// ==================== Helper functions ====================

// buildTestPacket строит полный Ethernet+IP+UDP+SIP пакет для бенчмарков
func buildTestPacket(sipPayload string) []byte {
	// Ethernet header (14 байт)
	// Destination MAC (6) + Source MAC (6) + EtherType (2 = 0x0800 IPv4)
	packet := make([]byte, 14+20+8+len(sipPayload))

	// Ethernet: EtherType = IPv4 (0x0800)
	packet[12] = 0x08
	packet[13] = 0x00

	// IP header (20 байт)
	ipOffset := 14
	packet[ipOffset] = 0x45    // Version + IHL
	packet[ipOffset+1] = 0x00  // DSCP + ECN
	packet[ipOffset+2] = 0x00  // Total Length (будет обновлён)
	packet[ipOffset+3] = 0x00  // Identification
	packet[ipOffset+4] = 0x00  // Flags + Fragment Offset
	packet[ipOffset+5] = 0x00  // TTL
	packet[ipOffset+8] = 0x11  // Protocol = UDP (17)
	packet[ipOffset+10] = 0x00 // Header checksum
	packet[ipOffset+11] = 0x00 // Header checksum
	packet[ipOffset+12] = 0x7f // Source IP (127.0.0.1)
	packet[ipOffset+13] = 0x00
	packet[ipOffset+14] = 0x00
	packet[ipOffset+15] = 0x01
	packet[ipOffset+16] = 0x7f // Dest IP (127.0.0.1)
	packet[ipOffset+17] = 0x00
	packet[ipOffset+18] = 0x00
	packet[ipOffset+19] = 0x01

	// UDP header (8 байт)
	udpOffset := ipOffset + 20
	packet[udpOffset] = 0x13 // Source Port (5060)
	packet[udpOffset+1] = 0x88
	packet[udpOffset+2] = 0x13 // Dest Port (5060)
	packet[udpOffset+3] = 0x88
	packet[udpOffset+4] = 0x00 // Length (будет обновлён)
	packet[udpOffset+5] = byte(len(sipPayload) + 8)
	packet[udpOffset+6] = 0x00 // Checksum
	packet[udpOffset+7] = 0x00

	// SIP payload
	copy(packet[udpOffset+8:], []byte(sipPayload))

	return packet
}
