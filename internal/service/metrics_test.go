package service

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"gitlab.com/sip-exporter/internal/vq"
)

func NewTestMetricser() Metricser {
	reg := prometheus.NewRegistry()
	return newMetricserWithRegistry(reg)
}

func (m *metrics) getSER(carrier string, uaType string) float64 {
	key := carrier + "\x00" + uaType
	val, ok := m.carrierCounters.Load(key)
	if !ok {
		return 0
	}
	counters := val.(*carrierAtomicCounters)
	total := counters.inviteTotal.Load()
	if total == 0 {
		return 0
	}
	threeXX := counters.invite3xxTotal.Load()
	denominator := total - threeXX
	if denominator == 0 {
		return 0
	}
	ok200 := counters.invite200OKTotal.Load()
	return float64(ok200) / float64(denominator) * 100 //nolint:mnd // percentage
}

func (m *metrics) getSEER(carrier string, uaType string) float64 {
	key := carrier + "\x00" + uaType
	val, ok := m.carrierCounters.Load(key)
	if !ok {
		return 0
	}
	counters := val.(*carrierAtomicCounters)
	total := counters.inviteTotal.Load()
	if total == 0 {
		return 0
	}
	threeXX := counters.invite3xxTotal.Load()
	denominator := total - threeXX
	if denominator == 0 {
		return 0
	}
	effective := counters.inviteEffectiveTotal.Load()
	return float64(effective) / float64(denominator) * 100 //nolint:mnd
}

func (m *metrics) getISA(carrier string, uaType string) float64 {
	key := carrier + "\x00" + uaType
	val, ok := m.carrierCounters.Load(key)
	if !ok {
		return 0
	}
	counters := val.(*carrierAtomicCounters)
	total := counters.inviteTotal.Load()
	if total == 0 {
		return 0
	}
	ineffective := counters.inviteIneffectiveTotal.Load()
	return float64(ineffective) / float64(total) * 100 //nolint:mnd
}

func (m *metrics) getASR(carrier string, uaType string) float64 {
	key := carrier + "\x00" + uaType
	val, ok := m.carrierCounters.Load(key)
	if !ok {
		return 0
	}
	counters := val.(*carrierAtomicCounters)
	total := counters.inviteTotal.Load()
	if total == 0 {
		return 0
	}
	ok200 := counters.invite200OKTotal.Load()
	return float64(ok200) / float64(total) * 100 //nolint:mnd
}

func (m *metrics) getNER(carrier string, uaType string) float64 {
	key := carrier + "\x00" + uaType
	val, ok := m.carrierCounters.Load(key)
	if !ok {
		return 0
	}
	counters := val.(*carrierAtomicCounters)
	total := counters.inviteTotal.Load()
	if total == 0 {
		return 0
	}
	ineffective := counters.inviteIneffectiveTotal.Load()
	return float64(total-ineffective) / float64(total) * 100 //nolint:mnd
}

func (m *metrics) getSCR(carrier string, uaType string) float64 {
	key := carrier + "\x00" + uaType
	val, ok := m.carrierCounters.Load(key)
	if !ok {
		return 0
	}
	counters := val.(*carrierAtomicCounters)
	total := counters.inviteTotal.Load()
	if total == 0 {
		return 0
	}
	completed := counters.sessionCompletedTotal.Load()
	return float64(completed) / float64(total) * 100 //nolint:mnd
}

func (m *metrics) getRRDFromHistogram(carrier string, uaType string) (sum float64, count uint64) {
	if m.rrd == nil {
		return 0, 0
	}
	hist, ok := m.rrd.WithLabelValues(carrier, uaType).(prometheus.Histogram)
	if !ok {
		return 0, 0
	}
	var dtoMetric dto.Metric
	if err := hist.Write(&dtoMetric); err != nil {
		return 0, 0
	}
	h := dtoMetric.GetHistogram()
	return h.GetSampleSum(), h.GetSampleCount()
}

func (m *metrics) getTTRFromHistogram(carrier string, uaType string) (sum float64, count uint64) {
	if m.ttr == nil {
		return 0, 0
	}
	hist, ok := m.ttr.WithLabelValues(carrier, uaType).(prometheus.Histogram)
	if !ok {
		return 0, 0
	}
	var dtoMetric dto.Metric
	if err := hist.Write(&dtoMetric); err != nil {
		return 0, 0
	}
	h := dtoMetric.GetHistogram()
	return h.GetSampleSum(), h.GetSampleCount()
}

func (m *metrics) getORDFromHistogram(carrier string, uaType string) (sum float64, count uint64) {
	if m.ord == nil {
		return 0, 0
	}
	hist, ok := m.ord.WithLabelValues(carrier, uaType).(prometheus.Histogram)
	if !ok {
		return 0, 0
	}
	var dtoMetric dto.Metric
	if err := hist.Write(&dtoMetric); err != nil {
		return 0, 0
	}
	h := dtoMetric.GetHistogram()
	return h.GetSampleSum(), h.GetSampleCount()
}

func (m *metrics) getLRDFromHistogram(carrier string, uaType string) (sum float64, count uint64) {
	if m.lrd == nil {
		return 0, 0
	}
	hist, ok := m.lrd.WithLabelValues(carrier, uaType).(prometheus.Histogram)
	if !ok {
		return 0, 0
	}
	var dtoMetric dto.Metric
	if err := hist.Write(&dtoMetric); err != nil {
		return 0, 0
	}
	h := dtoMetric.GetHistogram()
	return h.GetSampleSum(), h.GetSampleCount()
}

func (m *metrics) getSPDFromHistogram(carrier string, uaType string) (sum float64, count uint64) {
	if m.spd == nil {
		return 0, 0
	}
	hist, ok := m.spd.WithLabelValues(carrier, uaType).(prometheus.Histogram)
	if !ok {
		return 0, 0
	}
	var dtoMetric dto.Metric
	if err := hist.Write(&dtoMetric); err != nil {
		return 0, 0
	}
	h := dtoMetric.GetHistogram()
	return h.GetSampleSum(), h.GetSampleCount()
}

func (m *metrics) getSDCFromCounter(carrier string, uaType string) float64 {
	if m.sdc == nil {
		return 0
	}
	var dtoMetric dto.Metric
	if err := m.sdc.WithLabelValues(carrier, uaType).Write(&dtoMetric); err != nil {
		return 0
	}
	return dtoMetric.GetCounter().GetValue()
}

func (m *metrics) getISSFromCounter(carrier string, uaType string) float64 {
	if m.iss == nil {
		return 0
	}
	var dtoMetric dto.Metric
	if err := m.iss.WithLabelValues(carrier, uaType).Write(&dtoMetric); err != nil {
		return 0
	}
	return dtoMetric.GetCounter().GetValue()
}

func TestMetricser_Request_AllMethodsSingleRun(t *testing.T) {
	m := NewTestMetricser()
	require.NotNil(t, m)

	methods := []struct {
		name string
		data []byte
	}{
		{"INVITE", []byte("INVITE")},
		{"ACK", []byte("ACK")},
		{"BYE", []byte("BYE")},
		{"CANCEL", []byte("CANCEL")},
		{"OPTIONS", []byte("OPTIONS")},
		{"REGISTER", []byte("REGISTER")},
		{"UPDATE", []byte("UPDATE")},
		{"INFO", []byte("INFO")},
		{"REFER", []byte("REFER")},
		{"SUBSCRIBE", []byte("SUBSCRIBE")},
		{"NOTIFY", []byte("NOTIFY")},
		{"PRACK", []byte("PRACK")},
		{"PUBLISH", []byte("PUBLISH")},
		{"MESSAGE", []byte("MESSAGE")},
		{"UNKNOWN", []byte("UNKNOWN_METHOD")},
		{"EMPTY", []byte("")},
	}

	for _, method := range methods {
		t.Run(method.name, func(t *testing.T) {
			m.Request("", "", method.data)
		})
	}
}

func TestMetricser_Response_AllCodesSingleRun(t *testing.T) {
	m := NewTestMetricser()
	require.NotNil(t, m)

	codes := []struct {
		name             string
		data             []byte
		isInviteResponse bool
	}{
		{"100", []byte("100"), false},
		{"180", []byte("180"), false},
		{"183", []byte("183"), false},
		{"200", []byte("200"), false},
		{"202", []byte("202"), false},
		{"300", []byte("300"), false},
		{"302", []byte("302"), false},
		{"400", []byte("400"), false},
		{"401", []byte("401"), false},
		{"403", []byte("403"), false},
		{"404", []byte("404"), false},
		{"407", []byte("407"), false},
		{"408", []byte("408"), false},
		{"480", []byte("480"), false},
		{"486", []byte("486"), false},
		{"500", []byte("500"), false},
		{"503", []byte("503"), false},
		{"600", []byte("600"), false},
		{"603", []byte("603"), false},
		{"181", []byte("181"), false},
		{"182", []byte("182"), false},
		{"405", []byte("405"), false},
		{"481", []byte("481"), false},
		{"487", []byte("487"), false},
		{"488", []byte("488"), false},
		{"501", []byte("501"), false},
		{"502", []byte("502"), false},
		{"604", []byte("604"), false},
		{"606", []byte("606"), false},
		{"UNKNOWN", []byte("999"), false},
		{"EMPTY", []byte(""), false},
	}

	for _, code := range codes {
		t.Run(code.name, func(t *testing.T) {
			m.Response("", "", code.data, code.isInviteResponse)
		})
	}
}

func TestMetricser_UpdateSession_VariousValues(t *testing.T) {
	m := NewTestMetricser()
	require.NotNil(t, m)

	testCases := []struct {
		name string
		size int
	}{
		{"zero", 0},
		{"small", 5},
		{"medium", 100},
		{"large", 10000},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m.UpdateSession("", "", tc.size)
		})
	}
}

func TestMetricser_SystemError_Multiple(t *testing.T) {
	m := NewTestMetricser()
	require.NotNil(t, m)

	for i := 0; i < 5; i++ {
		t.Run(string(rune('0'+i)), func(t *testing.T) {
			m.SystemError()
		})
	}
}

func TestMetricser_Combined(t *testing.T) {
	m := NewTestMetricser()
	require.NotNil(t, m)

	m.Request("", "", []byte("INVITE"))
	m.Response("", "", []byte("200"), false)
	m.UpdateSession("", "", 10)
	m.SystemError()
}

func TestMetrics_UpdateSER_NoInvites(t *testing.T) {
	m := &metrics{}
	require.Equal(t, 0.0, m.getSER("", ""))
}

func TestMetrics_UpdateSER_AllSuccessful(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(100)
	counters.invite200OKTotal.Store(100)
	counters.invite3xxTotal.Store(0)

	require.Equal(t, 100.0, m.getSER("", ""))
}

func TestMetrics_UpdateSER_HalfSuccessful(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(100)
	counters.invite200OKTotal.Store(50)
	counters.invite3xxTotal.Store(0)

	require.Equal(t, 50.0, m.getSER("", ""))
}

func TestMetrics_UpdateSER_With3xxExcluded(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(100)
	counters.invite200OKTotal.Store(45)
	counters.invite3xxTotal.Store(10)

	require.Equal(t, 50.0, m.getSER("", ""))
}

func TestMetrics_UpdateSER_DenominatorZero(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(10)
	counters.invite200OKTotal.Store(0)
	counters.invite3xxTotal.Store(10)

	require.Equal(t, 0.0, m.getSER("", ""))
}

func TestMetrics_Invite200OK(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(10)
	counters.invite200OKTotal.Store(0)
	counters.invite3xxTotal.Store(0)

	m.Invite200OK("", "")

	require.Equal(t, int64(1), counters.invite200OKTotal.Load())
}

func TestMetrics_Integration_SER(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")

	for i := 0; i < 10; i++ {
		counters.inviteTotal.Add(1)
	}

	for i := 0; i < 5; i++ {
		counters.invite200OKTotal.Add(1)
	}

	counters.invite3xxTotal.Add(2)

	require.Equal(t, 62.5, m.getSER("", ""))
}

func TestMetrics_SER_Values(t *testing.T) {
	tests := []struct {
		name        string
		invites     int64
		invite200OK int64
		invite3xx   int64
		wantSER     float64
	}{
		{
			name:        "zero_invites",
			invites:     0,
			invite200OK: 0,
			invite3xx:   0,
			wantSER:     0,
		},
		{
			name:        "all_successful",
			invites:     100,
			invite200OK: 100,
			invite3xx:   0,
			wantSER:     100,
		},
		{
			name:        "half_successful",
			invites:     100,
			invite200OK: 50,
			invite3xx:   0,
			wantSER:     50,
		},
		{
			name:        "with_3xx_excluded",
			invites:     100,
			invite200OK: 45,
			invite3xx:   10,
			wantSER:     50,
		},
		{
			name:        "denominator_zero",
			invites:     10,
			invite200OK: 0,
			invite3xx:   10,
			wantSER:     0,
		},
		{
			name:        "62.5_percent",
			invites:     10,
			invite200OK: 5,
			invite3xx:   2,
			wantSER:     62.5,
		},
		{
			name:        "75_percent",
			invites:     8,
			invite200OK: 6,
			invite3xx:   0,
			wantSER:     75,
		},
		{
			name:        "25_percent",
			invites:     100,
			invite200OK: 20,
			invite3xx:   20,
			wantSER:     25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &metrics{}
			counters := m.getOrCreateCarrierCounters("", "")
			counters.inviteTotal.Store(tt.invites)
			counters.invite200OKTotal.Store(tt.invite200OK)
			counters.invite3xxTotal.Store(tt.invite3xx)

			got := m.getSER("", "")
			require.Equal(t, tt.wantSER, got)
		})
	}
}

func TestMetrics_SER_FullCycle(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")

	require.Equal(t, 0.0, m.getSER("", ""))

	for i := 0; i < 10; i++ {
		counters.inviteTotal.Add(1)
	}
	require.Equal(t, 0.0, m.getSER("", ""))

	for i := 0; i < 5; i++ {
		counters.invite200OKTotal.Add(1)
	}
	require.Equal(t, 50.0, m.getSER("", ""))

	for i := 0; i < 2; i++ {
		counters.invite3xxTotal.Add(1)
	}
	require.Equal(t, 62.5, m.getSER("", ""))

	for i := 0; i < 3; i++ {
		counters.invite200OKTotal.Add(1)
	}
	require.Equal(t, 100.0, m.getSER("", ""))
}

func TestMetrics_SER_RequestResponseFlow(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")

	for i := 0; i < 20; i++ {
		counters.inviteTotal.Add(1)
	}

	counters.invite200OKTotal.Store(10)
	counters.invite3xxTotal.Store(5)

	got := m.getSER("", "")
	require.InDelta(t, 66.67, got, 0.01)
}

func TestMetrics_Response_3xxWithInviteResponse(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(10)
	counters.invite3xxTotal.Store(0)

	counters.invite3xxTotal.Add(1)

	require.Equal(t, int64(1), counters.invite3xxTotal.Load())
}

func TestMetrics_Response_3xxWithoutInviteResponse(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(10)
	counters.invite3xxTotal.Store(0)

	require.Equal(t, int64(0), counters.invite3xxTotal.Load())
}

func TestMetrics_Response_200WithInviteResponse(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.invite3xxTotal.Store(0)

	require.Equal(t, int64(0), counters.invite3xxTotal.Load())
}

func TestMetrics_Request_INVITE(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(0)
	counters.inviteTotal.Add(1)

	require.Equal(t, int64(1), counters.inviteTotal.Load())
}

func TestMetrics_Request_NotINVITE(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(0)

	require.Equal(t, int64(0), counters.inviteTotal.Load())
}

func TestMetrics_SEER_NoInvites(t *testing.T) {
	m := &metrics{}
	require.Equal(t, 0.0, m.getSEER("", ""))
}

func TestMetrics_SEER_AllEffective(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(100)
	counters.inviteEffectiveTotal.Store(100)
	counters.invite3xxTotal.Store(0)

	require.Equal(t, 100.0, m.getSEER("", ""))
}

func TestMetrics_SEER_HalfEffective(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(100)
	counters.inviteEffectiveTotal.Store(50)
	counters.invite3xxTotal.Store(0)

	require.Equal(t, 50.0, m.getSEER("", ""))
}

func TestMetrics_SEER_With3xxExcluded(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(100)
	counters.inviteEffectiveTotal.Store(45)
	counters.invite3xxTotal.Store(10)

	require.Equal(t, 50.0, m.getSEER("", ""))
}

func TestMetrics_SEER_DenominatorZero(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(10)
	counters.inviteEffectiveTotal.Store(0)
	counters.invite3xxTotal.Store(10)

	require.Equal(t, 0.0, m.getSEER("", ""))
}

func TestMetrics_SEER_EachCodeIndependent(t *testing.T) {
	tests := []struct {
		name         string
		invites      int64
		effective200 int64
		effective480 int64
		effective486 int64
		effective600 int64
		effective603 int64
		threeXX      int64
		wantSEER     float64
	}{
		{
			name:         "only_200",
			invites:      100,
			effective200: 50,
			threeXX:      0,
			wantSEER:     50.0,
		},
		{
			name:         "only_480",
			invites:      100,
			effective480: 30,
			threeXX:      0,
			wantSEER:     30.0,
		},
		{
			name:         "only_486",
			invites:      100,
			effective486: 20,
			threeXX:      0,
			wantSEER:     20.0,
		},
		{
			name:         "only_600",
			invites:      100,
			effective600: 10,
			threeXX:      0,
			wantSEER:     10.0,
		},
		{
			name:         "only_603",
			invites:      100,
			effective603: 5,
			threeXX:      0,
			wantSEER:     5.0,
		},
		{
			name:         "all_codes_combined",
			invites:      100,
			effective200: 40,
			effective480: 10,
			effective486: 10,
			effective600: 5,
			effective603: 5,
			threeXX:      10,
			wantSEER:     77.78,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &metrics{}
			counters := m.getOrCreateCarrierCounters("", "")
			counters.inviteTotal.Store(tt.invites)
			counters.inviteEffectiveTotal.Store(
				tt.effective200 + tt.effective480 + tt.effective486 + tt.effective600 + tt.effective603)
			counters.invite3xxTotal.Store(tt.threeXX)

			got := m.getSEER("", "")
			require.InDelta(t, tt.wantSEER, got, 0.01)
		})
	}
}

func TestMetrics_SEER_FullCycle(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")

	require.Equal(t, 0.0, m.getSEER("", ""))

	for i := 0; i < 20; i++ {
		counters.inviteTotal.Add(1)
	}
	require.Equal(t, 0.0, m.getSEER("", ""))

	for i := 0; i < 10; i++ {
		counters.inviteEffectiveTotal.Add(1)
	}
	require.Equal(t, 50.0, m.getSEER("", ""))

	for i := 0; i < 5; i++ {
		counters.inviteEffectiveTotal.Add(1)
	}
	require.Equal(t, 75.0, m.getSEER("", ""))

	for i := 0; i < 4; i++ {
		counters.invite3xxTotal.Add(1)
	}
	require.Equal(t, 93.75, m.getSEER("", ""))

	require.Equal(t, 93.75, m.getSEER("", ""))
}

func TestMetrics_SEER_RequestResponseFlow(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")

	for i := 0; i < 20; i++ {
		counters.inviteTotal.Add(1)
	}

	counters.inviteEffectiveTotal.Store(12)
	counters.invite3xxTotal.Store(5)

	got := m.getSEER("", "")
	require.Equal(t, 80.0, got)
}

func TestMetrics_SEER_SER_Comparison(t *testing.T) {
	tests := []struct {
		name        string
		invites     int64
		invite200OK int64
		effective   int64
		invite3xx   int64
	}{
		{"equal_when_only_200", 100, 50, 50, 10},
		{"seer_higher_with_480", 100, 40, 60, 10},
		{"seer_higher_with_603", 100, 30, 50, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &metrics{}
			counters := m.getOrCreateCarrierCounters("", "")
			counters.inviteTotal.Store(tt.invites)
			counters.invite200OKTotal.Store(tt.invite200OK)
			counters.inviteEffectiveTotal.Store(tt.effective)
			counters.invite3xxTotal.Store(tt.invite3xx)

			ser := m.getSER("", "")
			seer := m.getSEER("", "")

			require.GreaterOrEqual(t, seer, ser, "SEER must be >= SER")
		})
	}
}

func TestMetrics_SEER_NonEffectiveCodes(t *testing.T) {
	nonEffectiveCodes := []string{"400", "401", "403", "404", "408", "500", "503"}

	for _, code := range nonEffectiveCodes {
		t.Run(code, func(t *testing.T) {
			m := &metrics{}
			counters := m.getOrCreateCarrierCounters("", "")
			counters.inviteTotal.Store(10)
			counters.inviteEffectiveTotal.Store(0)
			counters.invite3xxTotal.Store(0)

			require.Equal(t, 0.0, m.getSEER("", ""))
		})
	}
}

func TestMetrics_ASR_NoInvites(t *testing.T) {
	m := &metrics{}
	require.Equal(t, 0.0, m.getASR("", ""))
}

func TestMetrics_ASR_AllSuccessful(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(100)
	counters.invite200OKTotal.Store(100)

	require.Equal(t, 100.0, m.getASR("", ""))
}

func TestMetrics_ASR_HalfSuccessful(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(100)
	counters.invite200OKTotal.Store(50)

	require.Equal(t, 50.0, m.getASR("", ""))
}

func TestMetrics_ASR_3xxNotExcluded(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(100)
	counters.invite200OKTotal.Store(45)
	counters.invite3xxTotal.Store(10)

	require.Equal(t, 45.0, m.getASR("", ""))
}

func TestMetrics_ASR_Values(t *testing.T) {
	tests := []struct {
		name        string
		invites     int64
		invite200OK int64
		wantASR     float64
	}{
		{
			name:        "zero_invites",
			invites:     0,
			invite200OK: 0,
			wantASR:     0,
		},
		{
			name:        "all_successful",
			invites:     100,
			invite200OK: 100,
			wantASR:     100,
		},
		{
			name:        "half_successful",
			invites:     100,
			invite200OK: 50,
			wantASR:     50,
		},
		{
			name:        "one_of_ten",
			invites:     10,
			invite200OK: 1,
			wantASR:     10,
		},
		{
			name:        "75_percent",
			invites:     8,
			invite200OK: 6,
			wantASR:     75,
		},
		{
			name:        "zero_200ok",
			invites:     50,
			invite200OK: 0,
			wantASR:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &metrics{}
			counters := m.getOrCreateCarrierCounters("", "")
			counters.inviteTotal.Store(tt.invites)
			counters.invite200OKTotal.Store(tt.invite200OK)

			got := m.getASR("", "")
			require.Equal(t, tt.wantASR, got)
		})
	}
}

func TestMetrics_ASR_FullCycle(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")

	require.Equal(t, 0.0, m.getASR("", ""))

	for i := 0; i < 20; i++ {
		counters.inviteTotal.Add(1)
	}
	require.Equal(t, 0.0, m.getASR("", ""))

	for i := 0; i < 10; i++ {
		counters.invite200OKTotal.Add(1)
	}
	require.Equal(t, 50.0, m.getASR("", ""))

	for i := 0; i < 5; i++ {
		counters.invite3xxTotal.Add(1)
	}
	require.Equal(t, 50.0, m.getASR("", ""))
}

func TestMetrics_ASR_ComparedToSER(t *testing.T) {
	tests := []struct {
		name        string
		invites     int64
		invite200OK int64
		invite3xx   int64
	}{
		{"equal_when_no_3xx", 100, 50, 0},
		{"asr_lower_with_3xx", 100, 50, 20},
		{"all_3xx_asr_zero", 100, 0, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &metrics{}
			counters := m.getOrCreateCarrierCounters("", "")
			counters.inviteTotal.Store(tt.invites)
			counters.invite200OKTotal.Store(tt.invite200OK)
			counters.invite3xxTotal.Store(tt.invite3xx)

			asr := m.getASR("", "")
			ser := m.getSER("", "")

			require.LessOrEqual(t, asr, ser, "ASR must be <= SER")
		})
	}
}

func TestMetrics_NER_NoInvites(t *testing.T) {
	m := &metrics{}
	require.Equal(t, 0.0, m.getNER("", ""))
}

func TestMetrics_NER_AllEffective(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(100)
	counters.inviteIneffectiveTotal.Store(0)
	require.Equal(t, 100.0, m.getNER("", ""))
}

func TestMetrics_NER_AllIneffective(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(100)
	counters.inviteIneffectiveTotal.Store(100)
	require.Equal(t, 0.0, m.getNER("", ""))
}

func TestMetrics_NER_HalfIneffective(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(100)
	counters.inviteIneffectiveTotal.Store(30)
	require.Equal(t, 70.0, m.getNER("", ""))
}

func TestMetrics_NER_Equals100MinusISA(t *testing.T) {
	tests := []struct {
		name        string
		invites     int64
		ineffective int64
	}{
		{"all_effective", 100, 0},
		{"half_ineffective", 100, 50},
		{"all_ineffective", 100, 100},
		{"partial", 200, 15},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &metrics{}
			counters := m.getOrCreateCarrierCounters("", "")
			counters.inviteTotal.Store(tt.invites)
			counters.inviteIneffectiveTotal.Store(tt.ineffective)

			ner := m.getNER("", "")
			isa := m.getISA("", "")
			require.InDelta(t, 100.0-isa, ner, 0.01, "NER must equal 100 - ISA")
		})
	}
}

func TestMetrics_NER_Values(t *testing.T) {
	tests := []struct {
		name        string
		invites     int64
		ineffective int64
		wantNER     float64
	}{
		{"zero_invites", 0, 0, 0},
		{"all_effective", 100, 0, 100},
		{"30_percent_ineffective", 100, 30, 70},
		{"one_of_ten_ineffective", 10, 1, 90},
		{"zero_ineffective", 50, 0, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &metrics{}
			counters := m.getOrCreateCarrierCounters("", "")
			counters.inviteTotal.Store(tt.invites)
			counters.inviteIneffectiveTotal.Store(tt.ineffective)
			require.Equal(t, tt.wantNER, m.getNER("", ""))
		})
	}
}

func TestMetrics_NER_GreaterOrEqualSEER(t *testing.T) {
	tests := []struct {
		name        string
		invites     int64
		ineffective int64
		effective   int64
		invite3xx   int64
	}{
		{"equal_no_3xx", 100, 10, 90, 0},
		{"ner_higher_with_3xx", 100, 10, 60, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &metrics{}
			counters := m.getOrCreateCarrierCounters("", "")
			counters.inviteTotal.Store(tt.invites)
			counters.inviteIneffectiveTotal.Store(tt.ineffective)
			counters.inviteEffectiveTotal.Store(tt.effective)
			counters.invite3xxTotal.Store(tt.invite3xx)

			ner := m.getNER("", "")
			seer := m.getSEER("", "")
			require.GreaterOrEqual(t, ner, seer, "NER must be >= SEER")
		})
	}
}

func TestMetrics_ISA_NoInvites(t *testing.T) {
	m := &metrics{}
	require.Equal(t, 0.0, m.getISA("", ""))
}

func TestMetrics_ISA_AllIneffective(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(100)
	counters.inviteIneffectiveTotal.Store(100)

	require.Equal(t, 100.0, m.getISA("", ""))
}

func TestMetrics_ISA_HalfIneffective(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(100)
	counters.inviteIneffectiveTotal.Store(50)

	require.Equal(t, 50.0, m.getISA("", ""))
}

func TestMetrics_ISA_EachCode(t *testing.T) {
	tests := []struct {
		name           string
		invites        int64
		ineffective408 int64
		ineffective500 int64
		ineffective503 int64
		ineffective504 int64
		wantISA        float64
	}{
		{
			name:           "only_408",
			invites:        100,
			ineffective408: 40,
			wantISA:        40.0,
		},
		{
			name:           "only_500",
			invites:        100,
			ineffective500: 30,
			wantISA:        30.0,
		},
		{
			name:           "only_503",
			invites:        100,
			ineffective503: 20,
			wantISA:        20.0,
		},
		{
			name:           "only_504",
			invites:        100,
			ineffective504: 10,
			wantISA:        10.0,
		},
		{
			name:           "all_codes_combined",
			invites:        100,
			ineffective408: 10,
			ineffective500: 15,
			ineffective503: 10,
			ineffective504: 5,
			wantISA:        40.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &metrics{}
			counters := m.getOrCreateCarrierCounters("", "")
			counters.inviteTotal.Store(tt.invites)
			counters.inviteIneffectiveTotal.Store(
				tt.ineffective408 + tt.ineffective500 + tt.ineffective503 + tt.ineffective504)

			got := m.getISA("", "")
			require.Equal(t, tt.wantISA, got)
		})
	}
}

func TestMetrics_ISA_Mixed(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(100)
	counters.inviteIneffectiveTotal.Store(20)

	require.Equal(t, 20.0, m.getISA("", ""))
}

func TestMetrics_ISA_FullCycle(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")

	require.Equal(t, 0.0, m.getISA("", ""))

	for i := 0; i < 20; i++ {
		counters.inviteTotal.Add(1)
	}
	require.Equal(t, 0.0, m.getISA("", ""))

	for i := 0; i < 5; i++ {
		counters.inviteIneffectiveTotal.Add(1)
	}
	require.Equal(t, 25.0, m.getISA("", ""))

	for i := 0; i < 3; i++ {
		counters.inviteIneffectiveTotal.Add(1)
	}
	require.Equal(t, 40.0, m.getISA("", ""))

	require.Equal(t, 40.0, m.getISA("", ""))

	for i := 0; i < 3; i++ {
		counters.invite3xxTotal.Add(1)
		counters.inviteTotal.Add(1)
	}
	require.InDelta(t, 34.78, m.getISA("", ""), 0.01)
}

func TestMetrics_SCR_NoInvites(t *testing.T) {
	m := &metrics{}
	require.Equal(t, 0.0, m.getSCR("", ""))
}

func TestMetrics_SCR_AllCompleted(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(100)
	counters.sessionCompletedTotal.Store(100)

	require.Equal(t, 100.0, m.getSCR("", ""))
}

func TestMetrics_SCR_HalfCompleted(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(100)
	counters.sessionCompletedTotal.Store(50)

	require.Equal(t, 50.0, m.getSCR("", ""))
}

func TestMetrics_SCR_3xxNotExcluded(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteTotal.Store(100)
	counters.sessionCompletedTotal.Store(40)

	require.Equal(t, 40.0, m.getSCR("", ""))
}

func TestMetrics_SCR_Values(t *testing.T) {
	tests := []struct {
		name      string
		invites   int64
		completed int64
		wantSCR   float64
	}{
		{
			name:      "zero_invites",
			invites:   0,
			completed: 0,
			wantSCR:   0,
		},
		{
			name:      "all_completed",
			invites:   100,
			completed: 100,
			wantSCR:   100,
		},
		{
			name:      "half_completed",
			invites:   100,
			completed: 50,
			wantSCR:   50,
		},
		{
			name:      "one_of_ten",
			invites:   10,
			completed: 1,
			wantSCR:   10,
		},
		{
			name:      "75_percent",
			invites:   8,
			completed: 6,
			wantSCR:   75,
		},
		{
			name:      "25_percent",
			invites:   200,
			completed: 50,
			wantSCR:   25,
		},
		{
			name:      "zero_completed",
			invites:   50,
			completed: 0,
			wantSCR:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &metrics{}
			counters := m.getOrCreateCarrierCounters("", "")
			counters.inviteTotal.Store(tt.invites)
			counters.sessionCompletedTotal.Store(tt.completed)

			got := m.getSCR("", "")
			require.Equal(t, tt.wantSCR, got)
		})
	}
}

func TestMetrics_SCR_FullCycle(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")

	require.Equal(t, 0.0, m.getSCR("", ""))

	for i := 0; i < 20; i++ {
		counters.inviteTotal.Add(1)
	}
	require.Equal(t, 0.0, m.getSCR("", ""))

	for i := 0; i < 10; i++ {
		counters.sessionCompletedTotal.Add(1)
	}
	require.Equal(t, 50.0, m.getSCR("", ""))

	for i := 0; i < 5; i++ {
		counters.sessionCompletedTotal.Add(1)
	}
	require.Equal(t, 75.0, m.getSCR("", ""))

	require.Equal(t, 75.0, m.getSCR("", ""))
}

func TestMetrics_SessionCompleted(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	counters := m.getOrCreateCarrierCounters("", "")

	m.SessionCompleted("", "")
	require.Equal(t, int64(1), counters.sessionCompletedTotal.Load())

	m.SessionCompleted("", "")
	require.Equal(t, int64(2), counters.sessionCompletedTotal.Load())
}

func TestMetrics_SDC_IncrementOnSessionCompleted(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	counters := m.getOrCreateCarrierCounters("", "")

	require.Equal(t, 0.0, m.getSDCFromCounter("", ""))
	require.Equal(t, int64(0), counters.sessionCompletedTotal.Load())

	m.SessionCompleted("", "")
	require.Equal(t, 1.0, m.getSDCFromCounter("", ""))
	require.Equal(t, int64(1), counters.sessionCompletedTotal.Load())

	m.SessionCompleted("", "")
	require.Equal(t, 2.0, m.getSDCFromCounter("", ""))
	require.Equal(t, int64(2), counters.sessionCompletedTotal.Load())
}

func TestMetrics_ISS_IncrementOnIneffective(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	ineffectiveCodes := []string{"408", "500", "503", "504"}
	for _, code := range ineffectiveCodes {
		before := m.getISSFromCounter("", "")
		m.ResponseWithMetrics("", "", []byte(code), true, false)
		require.Equal(t, before+1, m.getISSFromCounter("", ""), "ISS should increment on %s", code)
	}
	require.Equal(t, 4.0, m.getISSFromCounter("", ""))
}

func TestMetrics_ISS_NotIncrementOnEffective(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	effectiveCodes := []string{"200", "480", "486", "600", "603"}
	for _, code := range effectiveCodes {
		is200OK := code == "200"
		m.ResponseWithMetrics("", "", []byte(code), true, is200OK)
	}
	require.Equal(t, 0.0, m.getISSFromCounter("", ""))
}

func TestMetrics_ISS_NotIncrementOnNonInvite(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	m.ResponseWithMetrics("", "", []byte("500"), false, false)
	require.Equal(t, 0.0, m.getISSFromCounter("", ""))
}

func TestMetrics_ISS_NilSafe(t *testing.T) {
	m := &metrics{}
	counters := m.getOrCreateCarrierCounters("", "")
	counters.inviteIneffectiveTotal.Add(1)
	require.Equal(t, int64(1), counters.inviteIneffectiveTotal.Load())
	require.Equal(t, 0.0, m.getISSFromCounter("", ""))
}

func TestMetrics_SCR_ComparedToSER(t *testing.T) {
	tests := []struct {
		name        string
		invites     int64
		invite200OK int64
		completed   int64
		invite3xx   int64
	}{
		{"equal_when_all_completed", 100, 50, 50, 0},
		{"scr_lower_when_some_terminated", 100, 80, 60, 10},
		{"scr_zero_when_none_completed", 100, 50, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &metrics{}
			counters := m.getOrCreateCarrierCounters("", "")
			counters.inviteTotal.Store(tt.invites)
			counters.invite200OKTotal.Store(tt.invite200OK)
			counters.sessionCompletedTotal.Store(tt.completed)
			counters.invite3xxTotal.Store(tt.invite3xx)

			scr := m.getSCR("", "")
			ser := m.getSER("", "")

			require.LessOrEqual(t, scr, ser, "SCR must be <= SER")
		})
	}
}

func TestMetricser_ResponseWithMetrics_200OK_Invite(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	counters := m.getOrCreateCarrierCounters("", "")

	invite200OKBefore := counters.invite200OKTotal.Load()
	invite3xxBefore := counters.invite3xxTotal.Load()
	inviteEffectiveBefore := counters.inviteEffectiveTotal.Load()
	inviteIneffectiveBefore := counters.inviteIneffectiveTotal.Load()

	m.ResponseWithMetrics("", "", []byte("200"), true, true)

	require.Equal(t, invite200OKBefore+1, counters.invite200OKTotal.Load())
	require.Equal(t, inviteEffectiveBefore+1, counters.inviteEffectiveTotal.Load())
	require.Equal(t, invite3xxBefore, counters.invite3xxTotal.Load())
	require.Equal(t, inviteIneffectiveBefore, counters.inviteIneffectiveTotal.Load())
}

func TestMetricser_ResponseWithMetrics_200OK_Register(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	counters := m.getOrCreateCarrierCounters("", "")

	invite200OKBefore := counters.invite200OKTotal.Load()

	m.ResponseWithMetrics("", "", []byte("200"), false, true)

	require.Equal(t, invite200OKBefore, counters.invite200OKTotal.Load(), "invite200OKTotal should not increment for non-INVITE")
}

func TestMetricser_ResponseWithMetrics_401(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	counters := m.getOrCreateCarrierCounters("", "")

	invite200OKBefore := counters.invite200OKTotal.Load()
	inviteEffectiveBefore := counters.inviteEffectiveTotal.Load()
	inviteIneffectiveBefore := counters.inviteIneffectiveTotal.Load()

	m.ResponseWithMetrics("", "", []byte("401"), true, false)

	require.Equal(t, invite200OKBefore, counters.invite200OKTotal.Load())
	require.Equal(t, inviteEffectiveBefore, counters.inviteEffectiveTotal.Load())
	require.Equal(t, inviteIneffectiveBefore, counters.inviteIneffectiveTotal.Load())
}

func TestMetricser_ResponseWithMetrics_3xx(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	counters := m.getOrCreateCarrierCounters("", "")

	invite3xxBefore := counters.invite3xxTotal.Load()
	invite200OKBefore := counters.invite200OKTotal.Load()

	m.ResponseWithMetrics("", "", []byte("302"), true, false)

	require.Equal(t, invite3xxBefore+1, counters.invite3xxTotal.Load())
	require.Equal(t, invite200OKBefore, counters.invite200OKTotal.Load())
}

func TestMetricser_ResponseWithMetrics_SEER_EffectiveCodes(t *testing.T) {
	effectiveCodes := []string{"200", "480", "486", "600", "603"}

	for _, code := range effectiveCodes {
		t.Run(code, func(t *testing.T) {
			m := NewTestMetricser().(*metrics)
			counters := m.getOrCreateCarrierCounters("", "")
			inviteEffectiveBefore := counters.inviteEffectiveTotal.Load()

			is200OK := code == "200"
			m.ResponseWithMetrics("", "", []byte(code), true, is200OK)

			require.Equal(t, inviteEffectiveBefore+1, counters.inviteEffectiveTotal.Load(), "code %s should be effective", code)
		})
	}
}

func TestMetricser_ResponseWithMetrics_ISA_IneffectiveCodes(t *testing.T) {
	ineffectiveCodes := []string{"408", "500", "503", "504"}

	for _, code := range ineffectiveCodes {
		t.Run(code, func(t *testing.T) {
			m := NewTestMetricser().(*metrics)
			counters := m.getOrCreateCarrierCounters("", "")
			inviteIneffectiveBefore := counters.inviteIneffectiveTotal.Load()

			m.ResponseWithMetrics("", "", []byte(code), true, false)

			require.Equal(t, inviteIneffectiveBefore+1, counters.inviteIneffectiveTotal.Load(), "code %s should be ineffective", code)
		})
	}
}

func TestMetricser_ResponseWithMetrics_NonInvite(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	counters := m.getOrCreateCarrierCounters("", "")

	invite200OKBefore := counters.invite200OKTotal.Load()
	invite3xxBefore := counters.invite3xxTotal.Load()
	inviteEffectiveBefore := counters.inviteEffectiveTotal.Load()
	inviteIneffectiveBefore := counters.inviteIneffectiveTotal.Load()

	m.ResponseWithMetrics("", "", []byte("200"), false, true)

	require.Equal(t, invite200OKBefore, counters.invite200OKTotal.Load())
	require.Equal(t, invite3xxBefore, counters.invite3xxTotal.Load())
	require.Equal(t, inviteEffectiveBefore, counters.inviteEffectiveTotal.Load())
	require.Equal(t, inviteIneffectiveBefore, counters.inviteIneffectiveTotal.Load())
}

func TestMetricser_ResponseWithMetrics_AllInOne(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	counters := m.getOrCreateCarrierCounters("", "")

	invite200OKBefore := counters.invite200OKTotal.Load()
	inviteEffectiveBefore := counters.inviteEffectiveTotal.Load()
	inviteIneffectiveBefore := counters.inviteIneffectiveTotal.Load()
	invite3xxBefore := counters.invite3xxTotal.Load()

	m.ResponseWithMetrics("", "", []byte("200"), true, true)
	require.Equal(t, invite200OKBefore+1, counters.invite200OKTotal.Load())
	require.Equal(t, inviteEffectiveBefore+1, counters.inviteEffectiveTotal.Load())

	m.ResponseWithMetrics("", "", []byte("480"), true, false)
	require.Equal(t, invite200OKBefore+1, counters.invite200OKTotal.Load())
	require.Equal(t, inviteEffectiveBefore+2, counters.inviteEffectiveTotal.Load())

	m.ResponseWithMetrics("", "", []byte("500"), true, false)
	require.Equal(t, inviteIneffectiveBefore+1, counters.inviteIneffectiveTotal.Load())

	m.ResponseWithMetrics("", "", []byte("302"), true, false)
	require.Equal(t, invite3xxBefore+1, counters.invite3xxTotal.Load())
}

func TestMetrics_SPD_Histogram(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	sum, count := m.getSPDFromHistogram("", "")
	require.Equal(t, 0.0, sum)
	require.Equal(t, uint64(0), count)

	m.UpdateSPD("", "", 5*time.Second)

	sum, count = m.getSPDFromHistogram("", "")
	require.Equal(t, 5.0, sum)
	require.Equal(t, uint64(1), count)

	m.UpdateSPD("", "", 15*time.Second)

	sum, count = m.getSPDFromHistogram("", "")
	require.Equal(t, 20.0, sum)
	require.Equal(t, uint64(2), count)
}

func TestMetrics_SPD_Histogram_NegativeIgnored(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	m.UpdateSPD("", "", -1*time.Second)

	sum, count := m.getSPDFromHistogram("", "")
	require.Equal(t, 0.0, sum)
	require.Equal(t, uint64(0), count)
}

func TestMetrics_SPD_Histogram_ZeroDuration(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	m.UpdateSPD("", "", 0)

	sum, count := m.getSPDFromHistogram("", "")
	require.Equal(t, 0.0, sum)
	require.Equal(t, uint64(1), count)
}

func TestMetrics_RRD_Histogram(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	sum, count := m.getRRDFromHistogram("", "")
	require.Equal(t, 0.0, sum)
	require.Equal(t, uint64(0), count)

	m.UpdateRRD("", "", 100.5)

	sum, count = m.getRRDFromHistogram("", "")
	require.InDelta(t, 100.5, sum, 0.01)
	require.Equal(t, uint64(1), count)

	m.UpdateRRD("", "", 200.5)

	sum, count = m.getRRDFromHistogram("", "")
	require.InDelta(t, 301.0, sum, 0.01)
	require.Equal(t, uint64(2), count)
}

func TestMetrics_RRD_Histogram_MultipleObservations(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	for i := range 100 {
		m.UpdateRRD("", "", float64(i)*10.0)
	}

	sum, count := m.getRRDFromHistogram("", "")
	require.Equal(t, uint64(100), count)
	require.InDelta(t, 49500.0, sum, 1.0)
}

func TestMetrics_TTR_Histogram(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	sum, count := m.getTTRFromHistogram("", "")
	require.Equal(t, 0.0, sum)
	require.Equal(t, uint64(0), count)

	m.UpdateTTR("", "", 50.0)

	sum, count = m.getTTRFromHistogram("", "")
	require.InDelta(t, 50.0, sum, 0.01)
	require.Equal(t, uint64(1), count)

	m.UpdateTTR("", "", 150.0)

	sum, count = m.getTTRFromHistogram("", "")
	require.InDelta(t, 200.0, sum, 0.01)
	require.Equal(t, uint64(2), count)
}

func TestMetrics_TTR_Histogram_MultipleObservations(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	for i := range 100 {
		m.UpdateTTR("", "", float64(i)*10.0)
	}

	sum, count := m.getTTRFromHistogram("", "")
	require.Equal(t, uint64(100), count)
	require.InDelta(t, 49500.0, sum, 1.0)
}

func TestMetrics_TTR_Histogram_ZeroValue(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	m.UpdateTTR("", "", 0.0)

	sum, count := m.getTTRFromHistogram("", "")
	require.InDelta(t, 0.0, sum, 0.01)
	require.Equal(t, uint64(1), count)
}

func TestMetrics_TTR_Histogram_LargeValue(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	m.UpdateTTR("", "", 5000.0)

	sum, count := m.getTTRFromHistogram("", "")
	require.InDelta(t, 5000.0, sum, 0.01)
	require.Equal(t, uint64(1), count)
}

func TestMetrics_ORD_Observe(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateORD("", "", 10.5)

	m.UpdateORD("", "", 25.0)

	_, count := m.getORDFromHistogram("", "")
	require.Equal(t, uint64(2), count)
}

func TestMetrics_LRD_Observe(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateLRD("", "", 5.5)

	m.UpdateLRD("", "", 15.0)

	_, count := m.getLRDFromHistogram("", "")
	require.Equal(t, uint64(2), count)
}

func getCounterValue(cv *prometheus.CounterVec, carrier string, uaType string) float64 {
	var dtoMetric dto.Metric
	if err := cv.WithLabelValues(carrier, uaType).Write(&dtoMetric); err != nil {
		return 0
	}
	return dtoMetric.GetCounter().GetValue()
}

func TestMetrics_NewStatusCodes_IncrementCounters(t *testing.T) {
	newCodes := []struct {
		code   string
		metric string
	}{
		{"181", "sip_exporter_181_total"},
		{"182", "sip_exporter_182_total"},
		{"405", "sip_exporter_405_total"},
		{"481", "sip_exporter_481_total"},
		{"487", "sip_exporter_487_total"},
		{"488", "sip_exporter_488_total"},
		{"501", "sip_exporter_501_total"},
		{"502", "sip_exporter_502_total"},
		{"604", "sip_exporter_604_total"},
		{"606", "sip_exporter_606_total"},
	}

	for _, tc := range newCodes {
		t.Run(tc.code, func(t *testing.T) {
			m := NewTestMetricser().(*metrics)

			before := getCounterValue(m.statusCounters[tc.code], "other", "other")
			require.Equal(t, 0.0, before)

			m.Response("other", "other", []byte(tc.code), false)

			after := getCounterValue(m.statusCounters[tc.code], "other", "other")
			require.Equal(t, 1.0, after, "counter for status %s should increment", tc.code)
		})
	}
}

func TestMetrics_NewStatusCodes_MultipleIncrements(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	for range 5 {
		m.Response("other", "other", []byte("487"), false)
	}

	require.Equal(t, 5.0, getCounterValue(m.statusCounters["487"], "other", "other"))
}

func TestMetrics_NewStatusCodes_DifferentCarriers(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	m.Response("carrier-a", "other", []byte("181"), false)
	m.Response("carrier-b", "other", []byte("181"), false)
	m.Response("carrier-a", "other", []byte("181"), false)

	require.Equal(t, 2.0, getCounterValue(m.statusCounters["181"], "carrier-a", "other"))
	require.Equal(t, 1.0, getCounterValue(m.statusCounters["181"], "carrier-b", "other"))
}

func TestMetrics_NewStatusCodes_DoNotAffectExistingCounters(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	counters := m.getOrCreateCarrierCounters("other", "other")

	m.Response("other", "other", []byte("487"), true)

	require.Equal(t, int64(0), counters.invite200OKTotal.Load())
	require.Equal(t, int64(0), counters.inviteEffectiveTotal.Load())
	require.Equal(t, int64(0), counters.inviteIneffectiveTotal.Load())
	require.Equal(t, int64(0), counters.invite3xxTotal.Load())
}

func (m *metrics) getVQHistogram(hv *prometheus.HistogramVec, carrier, uaType string) (sum float64, count uint64) {
	if hv == nil {
		return 0, 0
	}
	hist, ok := hv.WithLabelValues(carrier, uaType).(prometheus.Histogram)
	if !ok {
		return 0, 0
	}
	var dtoMetric dto.Metric
	if err := hist.Write(&dtoMetric); err != nil {
		return 0, 0
	}
	h := dtoMetric.GetHistogram()
	return h.GetSampleSum(), h.GetSampleCount()
}

func (m *metrics) getVQCounter(cv *prometheus.CounterVec, carrier, uaType string) float64 {
	if cv == nil {
		return 0
	}
	var dtoMetric dto.Metric
	if err := cv.WithLabelValues(carrier, uaType).Write(&dtoMetric); err != nil {
		return 0
	}
	return dtoMetric.GetCounter().GetValue()
}

func TestVQ_NLR_Observe(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateVQReport("carrier-a", "yealink", &vq.SessionReport{
		NLR:     1.5,
		Present: map[string]bool{"NLR": true},
	})
	sum, count := m.getVQHistogram(m.vqNLR, "carrier-a", "yealink")
	require.InDelta(t, 1.5, sum, 0.01)
	require.Equal(t, uint64(1), count)
}

func TestVQ_JDR_Observe(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateVQReport("carrier-a", "yealink", &vq.SessionReport{
		JDR:     2.3,
		Present: map[string]bool{"JDR": true},
	})
	sum, count := m.getVQHistogram(m.vqJDR, "carrier-a", "yealink")
	require.InDelta(t, 2.3, sum, 0.01)
	require.Equal(t, uint64(1), count)
}

func TestVQ_BLD_Observe(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateVQReport("carrier-a", "yealink", &vq.SessionReport{
		BLD:     0.8,
		Present: map[string]bool{"BLD": true},
	})
	sum, count := m.getVQHistogram(m.vqBLD, "carrier-a", "yealink")
	require.InDelta(t, 0.8, sum, 0.01)
	require.Equal(t, uint64(1), count)
}

func TestVQ_GLD_Observe(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateVQReport("carrier-a", "yealink", &vq.SessionReport{
		GLD:     0.15,
		Present: map[string]bool{"GLD": true},
	})
	sum, count := m.getVQHistogram(m.vqGLD, "carrier-a", "yealink")
	require.InDelta(t, 0.15, sum, 0.01)
	require.Equal(t, uint64(1), count)
}

func TestVQ_RTD_Observe(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateVQReport("carrier-a", "yealink", &vq.SessionReport{
		RTD:     45.5,
		Present: map[string]bool{"RTD": true},
	})
	sum, count := m.getVQHistogram(m.vqRTD, "carrier-a", "yealink")
	require.InDelta(t, 45.5, sum, 0.01)
	require.Equal(t, uint64(1), count)
}

func TestVQ_ESD_Observe(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateVQReport("carrier-a", "yealink", &vq.SessionReport{
		ESD:     20.3,
		Present: map[string]bool{"ESD": true},
	})
	sum, count := m.getVQHistogram(m.vqESD, "carrier-a", "yealink")
	require.InDelta(t, 20.3, sum, 0.01)
	require.Equal(t, uint64(1), count)
}

func TestVQ_IAJ_Observe(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateVQReport("carrier-a", "yealink", &vq.SessionReport{
		IAJ:     5.2,
		Present: map[string]bool{"IAJ": true},
	})
	sum, count := m.getVQHistogram(m.vqIAJ, "carrier-a", "yealink")
	require.InDelta(t, 5.2, sum, 0.01)
	require.Equal(t, uint64(1), count)
}

func TestVQ_MAJ_Observe(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateVQReport("carrier-a", "yealink", &vq.SessionReport{
		MAJ:     3.1,
		Present: map[string]bool{"MAJ": true},
	})
	sum, count := m.getVQHistogram(m.vqMAJ, "carrier-a", "yealink")
	require.InDelta(t, 3.1, sum, 0.01)
	require.Equal(t, uint64(1), count)
}

func TestVQ_MOSLQ_Observe(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateVQReport("carrier-a", "yealink", &vq.SessionReport{
		MOSLQ:   4.5,
		Present: map[string]bool{"MOSLQ": true},
	})
	sum, count := m.getVQHistogram(m.vqMOSLQ, "carrier-a", "yealink")
	require.InDelta(t, 4.5, sum, 0.01)
	require.Equal(t, uint64(1), count)
}

func TestVQ_MOSCQ_Observe(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateVQReport("carrier-a", "yealink", &vq.SessionReport{
		MOSCQ:   4.2,
		Present: map[string]bool{"MOSCQ": true},
	})
	sum, count := m.getVQHistogram(m.vqMOSCQ, "carrier-a", "yealink")
	require.InDelta(t, 4.2, sum, 0.01)
	require.Equal(t, uint64(1), count)
}

func TestVQ_RLQ_Observe(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateVQReport("carrier-a", "yealink", &vq.SessionReport{
		RLQ:     92.0,
		Present: map[string]bool{"RLQ": true},
	})
	sum, count := m.getVQHistogram(m.vqRLQ, "carrier-a", "yealink")
	require.InDelta(t, 92.0, sum, 0.01)
	require.Equal(t, uint64(1), count)
}

func TestVQ_RCQ_Observe(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateVQReport("carrier-a", "yealink", &vq.SessionReport{
		RCQ:     88.0,
		Present: map[string]bool{"RCQ": true},
	})
	sum, count := m.getVQHistogram(m.vqRCQ, "carrier-a", "yealink")
	require.InDelta(t, 88.0, sum, 0.01)
	require.Equal(t, uint64(1), count)
}

func TestVQ_RERL_Observe(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateVQReport("carrier-a", "yealink", &vq.SessionReport{
		RERL:    55.0,
		Present: map[string]bool{"RERL": true},
	})
	sum, count := m.getVQHistogram(m.vqRERL, "carrier-a", "yealink")
	require.InDelta(t, 55.0, sum, 0.01)
	require.Equal(t, uint64(1), count)
}

func TestVQ_ReportsTotal_Increment(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	report := &vq.SessionReport{Present: map[string]bool{}}
	m.UpdateVQReport("carrier-a", "yealink", report)
	m.UpdateVQReport("carrier-b", "grandstream", report)
	require.Equal(t, 1.0, m.getVQCounter(m.vqReports, "carrier-a", "yealink"))
	require.Equal(t, 1.0, m.getVQCounter(m.vqReports, "carrier-b", "grandstream"))
	require.Equal(t, 0.0, m.getVQCounter(m.vqReports, "carrier-c", "other"))
}

func TestVQ_AbsentFieldNotObserved(t *testing.T) {
	m := NewTestMetricser().(*metrics)
	m.UpdateVQReport("carrier-a", "yealink", &vq.SessionReport{
		MOSLQ:   4.5,
		Present: map[string]bool{"MOSLQ": true},
	})
	_, nlRCount := m.getVQHistogram(m.vqNLR, "carrier-a", "yealink")
	require.Equal(t, uint64(0), nlRCount)
	sum, count := m.getVQHistogram(m.vqMOSLQ, "carrier-a", "yealink")
	require.InDelta(t, 4.5, sum, 0.01)
	require.Equal(t, uint64(1), count)
}
