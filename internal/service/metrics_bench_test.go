package service

import "testing"

func BenchmarkRequest_INVITE(b *testing.B) {
	m := NewTestMetricser().(*metrics)
	method := []byte("INVITE")

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.Request("carrier-a", "softphone", "RU", "RU", "192.168.1.1", "carrier.example.com", "", "", method)
	}
}

func BenchmarkRequest_REGISTER(b *testing.B) {
	m := NewTestMetricser().(*metrics)
	method := []byte("REGISTER")

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.Request("carrier-a", "softphone", "RU", "", "", "", "", "", method)
	}
}

func BenchmarkInvite200OK(b *testing.B) {
	m := NewTestMetricser().(*metrics)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.Invite200OK("carrier-a", "softphone", "RU", "RU", "192.168.1.1", "carrier.example.com", "", "")
	}
}

func BenchmarkRTPMetricsLegacy(b *testing.B) {
	m := NewTestMetricser()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.UpdateRTPPackets("carrier-a", "yealink", "PCMA", "RU", "inbound")
		m.UpdateRTPPDV("carrier-a", "yealink", "PCMA", "RU", "inbound", 1.5)
	}
}

func BenchmarkRTPMetricsBound(b *testing.B) {
	m := NewTestMetricser()
	rtpMetrics := m.BindRTPMetrics("carrier-a", "yealink", "PCMA", "RU", "inbound")

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		rtpMetrics.UpdateRTPPackets()
		rtpMetrics.UpdateRTPPDV(1.5)
	}
}
