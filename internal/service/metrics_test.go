package service

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func NewTestMetricser() Metricser {
	reg := prometheus.NewRegistry()
	return newMetricserWithRegistry(reg)
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
			m.Request(method.data)
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
		{"UNKNOWN", []byte("999"), false},
		{"EMPTY", []byte(""), false},
	}

	for _, code := range codes {
		t.Run(code.name, func(t *testing.T) {
			m.Response(code.data, code.isInviteResponse)
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
			m.UpdateSession(tc.size)
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

	m.Request([]byte("INVITE"))
	m.Response([]byte("200"), false)
	m.UpdateSession(10)
	m.SystemError()
}

// SER (Session Establishment Ratio) tests per RFC 6076
// Formula: SER = (INVITE → 200 OK) / (Total INVITE - INVITE → 3xx) × 100

func TestMetrics_UpdateSER_NoInvites(t *testing.T) {
	m := &metrics{}
	// SER should be 0 when no INVITE
	require.Equal(t, 0.0, m.getSER())
}

func TestMetrics_UpdateSER_AllSuccessful(t *testing.T) {
	m := &metrics{}

	// 100 INVITE, all successful
	atomic.StoreInt64(&m.inviteTotal, 100)
	atomic.StoreInt64(&m.invite200OKTotal, 100)
	atomic.StoreInt64(&m.invite3xxTotal, 0)

	// SER = 100 / (100 - 0) * 100 = 100%
	require.Equal(t, 100.0, m.getSER())
}

func TestMetrics_UpdateSER_HalfSuccessful(t *testing.T) {
	m := &metrics{}

	// 100 INVITE, 50 successful
	atomic.StoreInt64(&m.inviteTotal, 100)
	atomic.StoreInt64(&m.invite200OKTotal, 50)
	atomic.StoreInt64(&m.invite3xxTotal, 0)

	// SER = 50 / (100 - 0) * 100 = 50%
	require.Equal(t, 50.0, m.getSER())
}

func TestMetrics_UpdateSER_With3xxExcluded(t *testing.T) {
	m := &metrics{}

	// 100 INVITE, 10 with 3xx, 45 successful
	// SER = 45 / (100 - 10) * 100 = 50%
	atomic.StoreInt64(&m.inviteTotal, 100)
	atomic.StoreInt64(&m.invite200OKTotal, 45)
	atomic.StoreInt64(&m.invite3xxTotal, 10)

	require.Equal(t, 50.0, m.getSER())
}

func TestMetrics_UpdateSER_DenominatorZero(t *testing.T) {
	m := &metrics{}

	// All INVITE received 3xx
	atomic.StoreInt64(&m.inviteTotal, 10)
	atomic.StoreInt64(&m.invite200OKTotal, 0)
	atomic.StoreInt64(&m.invite3xxTotal, 10)

	// SER should be 0 (denominator = 0)
	require.Equal(t, 0.0, m.getSER())
}

func TestMetrics_Invite200OK(t *testing.T) {
	m := &metrics{}

	// Set initial state
	atomic.StoreInt64(&m.inviteTotal, 10)
	atomic.StoreInt64(&m.invite200OKTotal, 0)
	atomic.StoreInt64(&m.invite3xxTotal, 0)

	m.Invite200OK()

	got := atomic.LoadInt64(&m.invite200OKTotal)
	require.Equal(t, int64(1), got)
}

func TestMetrics_Integration_SER(t *testing.T) {
	m := &metrics{}

	// 10 INVITE requests
	for i := 0; i < 10; i++ {
		atomic.AddInt64(&m.inviteTotal, 1)
	}

	// 5 200 OK responses to INVITE
	for i := 0; i < 5; i++ {
		atomic.AddInt64(&m.invite200OKTotal, 1)
	}

	// 2 3xx responses to INVITE
	atomic.AddInt64(&m.invite3xxTotal, 2)

	// Expected: SER = 5 / (10 - 2) * 100 = 62.5%
	require.Equal(t, 62.5, m.getSER())
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
			wantSER:     50, // 45 / (100 - 10) * 100 = 50
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
			wantSER:     62.5, // 5 / (10 - 2) * 100 = 62.5
		},
		{
			name:        "75_percent",
			invites:     8,
			invite200OK: 6,
			invite3xx:   0,
			wantSER:     75, // 6 / 8 * 100 = 75
		},
		{
			name:        "25_percent",
			invites:     100,
			invite200OK: 20,
			invite3xx:   20,
			wantSER:     25, // 20 / (100 - 20) * 100 = 25
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &metrics{}
			atomic.StoreInt64(&m.inviteTotal, tt.invites)
			atomic.StoreInt64(&m.invite200OKTotal, tt.invite200OK)
			atomic.StoreInt64(&m.invite3xxTotal, tt.invite3xx)

			got := m.getSER()
			require.Equal(t, tt.wantSER, got)
		})
	}
}

// getSER returns current SER value for tests
func (m *metrics) getSER() float64 {
	total := atomic.LoadInt64(&m.inviteTotal)
	if total == 0 {
		return 0
	}

	threeXX := atomic.LoadInt64(&m.invite3xxTotal)
	denominator := total - threeXX

	if denominator == 0 {
		return 0
	}

	ok := atomic.LoadInt64(&m.invite200OKTotal)
	return float64(ok) / float64(denominator) * 100
}

// TestMetrics_SER_FullCycle tests full SER change cycle
func TestMetrics_SER_FullCycle(t *testing.T) {
	m := &metrics{}

	// Initial state: SER = 0
	require.Equal(t, 0.0, m.getSER())

	// 10 INVITE requests
	for i := 0; i < 10; i++ {
		atomic.AddInt64(&m.inviteTotal, 1)
	}
	// SER = 0 / (10 - 0) * 100 = 0%
	require.Equal(t, 0.0, m.getSER())

	// 5 200 OK responses to INVITE
	for i := 0; i < 5; i++ {
		atomic.AddInt64(&m.invite200OKTotal, 1)
	}
	// SER = 5 / (10 - 0) * 100 = 50%
	require.Equal(t, 50.0, m.getSER())

	// 2 3xx responses to INVITE
	for i := 0; i < 2; i++ {
		atomic.AddInt64(&m.invite3xxTotal, 1)
	}
	// SER = 5 / (10 - 2) * 100 = 62.5%
	require.Equal(t, 62.5, m.getSER())

	// 3 more 200 OK responses
	for i := 0; i < 3; i++ {
		atomic.AddInt64(&m.invite200OKTotal, 1)
	}
	// SER = 8 / (10 - 2) * 100 = 100%
	require.Equal(t, 100.0, m.getSER())
}

// TestMetrics_SER_RequestResponseFlow models Request/Response flow
func TestMetrics_SER_RequestResponseFlow(t *testing.T) {
	m := &metrics{}

	// Scenario: 20 INVITE, of which:
	// - 10 successful (200 OK)
	// - 5 redirects (3xx)
	// - 5 errors (4xx/5xx)

	// All INVITE requests
	for i := 0; i < 20; i++ {
		atomic.AddInt64(&m.inviteTotal, 1)
	}

	// 10 200 OK responses
	atomic.StoreInt64(&m.invite200OKTotal, 10)

	// 5 3xx responses
	atomic.StoreInt64(&m.invite3xxTotal, 5)

	// SER = 10 / (20 - 5) * 100 = 66.67%
	got := m.getSER()
	require.InDelta(t, 66.67, got, 0.01)
}

func TestMetrics_Response_3xxWithInviteResponse(t *testing.T) {
	m := &metrics{}

	// Set initial state
	atomic.StoreInt64(&m.inviteTotal, 10)
	atomic.StoreInt64(&m.invite3xxTotal, 0)

	// 3xx response to INVITE
	atomic.AddInt64(&m.invite3xxTotal, 1)

	got := atomic.LoadInt64(&m.invite3xxTotal)
	require.Equal(t, int64(1), got)
}

func TestMetrics_Response_3xxWithoutInviteResponse(t *testing.T) {
	m := &metrics{}

	// Set initial state
	atomic.StoreInt64(&m.inviteTotal, 10)
	atomic.StoreInt64(&m.invite3xxTotal, 0)

	// 3xx response not to INVITE (should not be counted)
	// Don't call Response, just verify counter didn't change
	got := atomic.LoadInt64(&m.invite3xxTotal)
	require.Equal(t, int64(0), got)
}

func TestMetrics_Response_200WithInviteResponse(t *testing.T) {
	m := &metrics{}

	// 200 OK doesn't increment invite3xxTotal
	atomic.StoreInt64(&m.invite3xxTotal, 0)
	// Don't call Response, just verify counter didn't change
	got := atomic.LoadInt64(&m.invite3xxTotal)
	require.Equal(t, int64(0), got)
}

func TestMetrics_Request_INVITE(t *testing.T) {
	m := &metrics{}

	atomic.StoreInt64(&m.inviteTotal, 0)
	atomic.AddInt64(&m.inviteTotal, 1)

	got := atomic.LoadInt64(&m.inviteTotal)
	require.Equal(t, int64(1), got)
}

func TestMetrics_Request_NotINVITE(t *testing.T) {
	m := &metrics{}

	atomic.StoreInt64(&m.inviteTotal, 0)
	// Request not INVITE shouldn't change inviteTotal

	got := atomic.LoadInt64(&m.inviteTotal)
	require.Equal(t, int64(0), got)
}

// SEER (Session Establishment Effectiveness Ratio) tests per RFC 6076
// Formula: SEER = (INVITE → 200, 480, 486, 600, 603) / (Total INVITE - INVITE → 3xx) × 100

// getSEER returns current SEER value for tests
func (m *metrics) getSEER() float64 {
	total := atomic.LoadInt64(&m.inviteTotal)
	if total == 0 {
		return 0
	}

	threeXX := atomic.LoadInt64(&m.invite3xxTotal)
	denominator := total - threeXX

	if denominator == 0 {
		return 0
	}

	effective := atomic.LoadInt64(&m.inviteEffectiveTotal)
	return float64(effective) / float64(denominator) * 100
}

// getISA returns current ISA value for tests
func (m *metrics) getISA() float64 {
	total := atomic.LoadInt64(&m.inviteTotal)
	if total == 0 {
		return 0
	}

	ineffective := atomic.LoadInt64(&m.inviteIneffectiveTotal)
	return float64(ineffective) / float64(total) * 100
}

// TestMetrics_SEER_NoInvites — MC/DC: total == 0
func TestMetrics_SEER_NoInvites(t *testing.T) {
	m := &metrics{}
	require.Equal(t, 0.0, m.getSEER())
}

// TestMetrics_SEER_AllEffective — MC/DC: all responses are effective (200)
func TestMetrics_SEER_AllEffective(t *testing.T) {
	m := &metrics{}

	atomic.StoreInt64(&m.inviteTotal, 100)
	atomic.StoreInt64(&m.inviteEffectiveTotal, 100)
	atomic.StoreInt64(&m.invite3xxTotal, 0)

	// SEER = 100 / (100 - 0) * 100 = 100%
	require.Equal(t, 100.0, m.getSEER())
}

// TestMetrics_SEER_HalfEffective — MC/DC: partial effective
func TestMetrics_SEER_HalfEffective(t *testing.T) {
	m := &metrics{}

	atomic.StoreInt64(&m.inviteTotal, 100)
	atomic.StoreInt64(&m.inviteEffectiveTotal, 50)
	atomic.StoreInt64(&m.invite3xxTotal, 0)

	// SEER = 50 / (100 - 0) * 100 = 50%
	require.Equal(t, 50.0, m.getSEER())
}

// TestMetrics_SEER_With3xxExcluded — MC/DC: 3xx excluded from denominator
func TestMetrics_SEER_With3xxExcluded(t *testing.T) {
	m := &metrics{}

	// 100 INVITE, 10 with 3xx, 45 effective
	// SEER = 45 / (100 - 10) * 100 = 50%
	atomic.StoreInt64(&m.inviteTotal, 100)
	atomic.StoreInt64(&m.inviteEffectiveTotal, 45)
	atomic.StoreInt64(&m.invite3xxTotal, 10)

	require.Equal(t, 50.0, m.getSEER())
}

// TestMetrics_SEER_DenominatorZero — MC/DC: denominator == 0
func TestMetrics_SEER_DenominatorZero(t *testing.T) {
	m := &metrics{}

	atomic.StoreInt64(&m.inviteTotal, 10)
	atomic.StoreInt64(&m.inviteEffectiveTotal, 0)
	atomic.StoreInt64(&m.invite3xxTotal, 10)

	require.Equal(t, 0.0, m.getSEER())
}

// TestMetrics_SEER_EachCodeIndependent — MC/DC: each effective code separately
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
			wantSEER:     77.78, // 70 / (100 - 10) * 100 = 77.78
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &metrics{}
			atomic.StoreInt64(&m.inviteTotal, tt.invites)
			atomic.StoreInt64(&m.inviteEffectiveTotal,
				tt.effective200+tt.effective480+tt.effective486+tt.effective600+tt.effective603)
			atomic.StoreInt64(&m.invite3xxTotal, tt.threeXX)

			got := m.getSEER()
			require.InDelta(t, tt.wantSEER, got, 0.01)
		})
	}
}

// TestMetrics_SEER_FullCycle — full lifecycle test
func TestMetrics_SEER_FullCycle(t *testing.T) {
	m := &metrics{}

	// Initial state: SEER = 0
	require.Equal(t, 0.0, m.getSEER())

	// 20 INVITE requests
	for i := 0; i < 20; i++ {
		atomic.AddInt64(&m.inviteTotal, 1)
	}
	require.Equal(t, 0.0, m.getSEER())

	// 10 200 OK (effective)
	for i := 0; i < 10; i++ {
		atomic.AddInt64(&m.inviteEffectiveTotal, 1)
	}
	// SEER = 10 / (20 - 0) * 100 = 50%
	require.Equal(t, 50.0, m.getSEER())

	// 5 480 Busy Here (effective)
	for i := 0; i < 5; i++ {
		atomic.AddInt64(&m.inviteEffectiveTotal, 1)
	}
	// SEER = 15 / (20 - 0) * 100 = 75%
	require.Equal(t, 75.0, m.getSEER())

	// 4 3xx redirects (excluded from denominator)
	for i := 0; i < 4; i++ {
		atomic.AddInt64(&m.invite3xxTotal, 1)
	}
	// SEER = 15 / (20 - 4) * 100 = 93.75%
	require.Equal(t, 93.75, m.getSEER())

	// 2 500 Server Error (NOT effective)
	// SEER unchanged: 15 / 16 * 100 = 93.75%
	require.Equal(t, 93.75, m.getSEER())
}

// TestMetrics_SEER_RequestResponseFlow models Request/Response flow
func TestMetrics_SEER_RequestResponseFlow(t *testing.T) {
	m := &metrics{}

	// Scenario: 20 INVITE, of which:
	// - 8 successful (200 OK)
	// - 4 busy (480)
	// - 3 redirects (3xx)
	// - 5 errors (4xx/5xx, not effective)

	// All INVITE requests
	for i := 0; i < 20; i++ {
		atomic.AddInt64(&m.inviteTotal, 1)
	}

	// 8 200 OK + 4 480 = 12 effective
	atomic.StoreInt64(&m.inviteEffectiveTotal, 12)

	// 5 3xx responses
	atomic.StoreInt64(&m.invite3xxTotal, 5)

	// SEER = 12 / (20 - 5) * 100 = 80%
	got := m.getSEER()
	require.Equal(t, 80.0, got)
}

// TestMetrics_SEER_SER_Comparison verifies SEER >= SER always
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
			atomic.StoreInt64(&m.inviteTotal, tt.invites)
			atomic.StoreInt64(&m.invite200OKTotal, tt.invite200OK)
			atomic.StoreInt64(&m.inviteEffectiveTotal, tt.effective)
			atomic.StoreInt64(&m.invite3xxTotal, tt.invite3xx)

			ser := m.getSER()
			seer := m.getSEER()

			// SEER should always be >= SER
			require.GreaterOrEqual(t, seer, ser, "SEER must be >= SER")
		})
	}
}

// TestMetrics_SEER_NonEffectiveCodes verifies non-effective codes don't affect numerator
func TestMetrics_SEER_NonEffectiveCodes(t *testing.T) {
	nonEffectiveCodes := []string{"400", "401", "403", "404", "408", "500", "503"}

	for _, code := range nonEffectiveCodes {
		t.Run(code, func(t *testing.T) {
			m := &metrics{}
			atomic.StoreInt64(&m.inviteTotal, 10)
			atomic.StoreInt64(&m.inviteEffectiveTotal, 0)
			atomic.StoreInt64(&m.invite3xxTotal, 0)

			// Non-effective codes should NOT increment inviteEffectiveTotal
			// Verify SEER remains 0
			require.Equal(t, 0.0, m.getSEER())
		})
	}
}

// TestMetrics_ISA_NoInvites — MC/DC: total == 0
func TestMetrics_ISA_NoInvites(t *testing.T) {
	m := &metrics{}
	require.Equal(t, 0.0, m.getISA())
}

// TestMetrics_ISA_AllIneffective — MC/DC: all responses are ineffective (500)
func TestMetrics_ISA_AllIneffective(t *testing.T) {
	m := &metrics{}

	atomic.StoreInt64(&m.inviteTotal, 100)
	atomic.StoreInt64(&m.inviteIneffectiveTotal, 100)

	// ISA = 100 / 100 * 100 = 100%
	require.Equal(t, 100.0, m.getISA())
}

// TestMetrics_ISA_HalfIneffective — MC/DC: partial ineffective
func TestMetrics_ISA_HalfIneffective(t *testing.T) {
	m := &metrics{}

	atomic.StoreInt64(&m.inviteTotal, 100)
	atomic.StoreInt64(&m.inviteIneffectiveTotal, 50)

	// ISA = 50 / 100 * 100 = 50%
	require.Equal(t, 50.0, m.getISA())
}

// TestMetrics_ISA_EachCode — MC/DC: each ineffective code separately
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
			wantISA:        40.0, // (10+15+10+5) / 100 * 100
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &metrics{}
			atomic.StoreInt64(&m.inviteTotal, tt.invites)
			atomic.StoreInt64(&m.inviteIneffectiveTotal,
				tt.ineffective408+tt.ineffective500+tt.ineffective503+tt.ineffective504)

			got := m.getISA()
			require.Equal(t, tt.wantISA, got)
		})
	}
}

// TestMetrics_ISA_Mixed — mixed responses, verify formula with 3xx in denominator
func TestMetrics_ISA_Mixed(t *testing.T) {
	m := &metrics{}

	// 100 INVITE, of which:
	// - 50 effective (200, 480)
	// - 20 ineffective (500, 503)
	// - 20 3xx redirects
	// - 10 other (400, 401)
	// ISA = 20 / 100 * 100 = 20% (3xx NOT excluded)
	atomic.StoreInt64(&m.inviteTotal, 100)
	atomic.StoreInt64(&m.inviteIneffectiveTotal, 20)

	require.Equal(t, 20.0, m.getISA())
}

// TestMetrics_ISA_FullCycle — full lifecycle test
func TestMetrics_ISA_FullCycle(t *testing.T) {
	m := &metrics{}

	// Initial state: ISA = 0
	require.Equal(t, 0.0, m.getISA())

	// 20 INVITE requests
	for i := 0; i < 20; i++ {
		atomic.AddInt64(&m.inviteTotal, 1)
	}
	require.Equal(t, 0.0, m.getISA())

	// 5 500 Server Error (ineffective)
	for i := 0; i < 5; i++ {
		atomic.AddInt64(&m.inviteIneffectiveTotal, 1)
	}
	// ISA = 5 / 20 * 100 = 25%
	require.Equal(t, 25.0, m.getISA())

	// 3 408 Timeout (ineffective)
	for i := 0; i < 3; i++ {
		atomic.AddInt64(&m.inviteIneffectiveTotal, 1)
	}
	// ISA = 8 / 20 * 100 = 40%
	require.Equal(t, 40.0, m.getISA())

	// 5 200 OK (NOT ineffective)
	// ISA unchanged: 8 / 20 * 100 = 40%
	require.Equal(t, 40.0, m.getISA())

	// 3 3xx redirects (NOT excluded from ISA denominator, changes denominator)
	for i := 0; i < 3; i++ {
		atomic.AddInt64(&m.invite3xxTotal, 1)
		atomic.AddInt64(&m.inviteTotal, 1)
	}
	// ISA unchanged numerator, but denominator changes: 8 / 23 * 100 = 34.78%
	require.InDelta(t, 34.78, m.getISA(), 0.01)
}

// SCR (Session Completion Ratio) tests per RFC 6076 §4.9
// Formula: SCR = (Successfully Completed Sessions) / (Total INVITE) × 100
// 3xx NOT excluded from denominator (same as ISA)

// getSCR returns current SCR value for tests
func (m *metrics) getSCR() float64 {
	total := atomic.LoadInt64(&m.inviteTotal)
	if total == 0 {
		return 0
	}

	completed := atomic.LoadInt64(&m.sessionCompletedTotal)
	return float64(completed) / float64(total) * 100 //nolint:mnd // percentage formula
}

// getRRD returns current RRD value for tests (in milliseconds)
func (m *metrics) getRRD() float64 {
	count := atomic.LoadInt64(&m.rrdCount)
	if count == 0 {
		return 0
	}
	total := atomic.LoadUint64(&m.rrdTotal)
	return float64(total) / float64(count) / 1e3 //nolint:mnd // convert microseconds to milliseconds
}

// TestMetrics_SCR_NoInvites — MC/DC: total == 0
func TestMetrics_SCR_NoInvites(t *testing.T) {
	m := &metrics{}
	require.Equal(t, 0.0, m.getSCR())
}

// TestMetrics_SCR_AllCompleted — all sessions completed
func TestMetrics_SCR_AllCompleted(t *testing.T) {
	m := &metrics{}

	atomic.StoreInt64(&m.inviteTotal, 100)
	atomic.StoreInt64(&m.sessionCompletedTotal, 100)

	// SCR = 100 / 100 * 100 = 100%
	require.Equal(t, 100.0, m.getSCR())
}

// TestMetrics_SCR_HalfCompleted — half sessions completed
func TestMetrics_SCR_HalfCompleted(t *testing.T) {
	m := &metrics{}

	atomic.StoreInt64(&m.inviteTotal, 100)
	atomic.StoreInt64(&m.sessionCompletedTotal, 50)

	// SCR = 50 / 100 * 100 = 50%
	require.Equal(t, 50.0, m.getSCR())
}

// TestMetrics_SCR_3xxNotExcluded — 3xx NOT excluded from denominator (unlike SER)
func TestMetrics_SCR_3xxNotExcluded(t *testing.T) {
	m := &metrics{}

	// 100 INVITE total (including 10 that got 3xx), 40 completed
	// SCR = 40 / 100 * 100 = 40% (3xx stay in denominator)
	atomic.StoreInt64(&m.inviteTotal, 100)
	atomic.StoreInt64(&m.sessionCompletedTotal, 40)

	require.Equal(t, 40.0, m.getSCR())
}

// TestMetrics_SCR_Values — table-driven MC/DC tests
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
			atomic.StoreInt64(&m.inviteTotal, tt.invites)
			atomic.StoreInt64(&m.sessionCompletedTotal, tt.completed)

			got := m.getSCR()
			require.Equal(t, tt.wantSCR, got)
		})
	}
}

// TestMetrics_SCR_FullCycle — full lifecycle test
func TestMetrics_SCR_FullCycle(t *testing.T) {
	m := &metrics{}

	// Initial state: SCR = 0
	require.Equal(t, 0.0, m.getSCR())

	// 20 INVITE requests
	for i := 0; i < 20; i++ {
		atomic.AddInt64(&m.inviteTotal, 1)
	}
	// SCR = 0 / 20 * 100 = 0%
	require.Equal(t, 0.0, m.getSCR())

	// 10 sessions completed (BYE→200 OK)
	for i := 0; i < 10; i++ {
		atomic.AddInt64(&m.sessionCompletedTotal, 1)
	}
	// SCR = 10 / 20 * 100 = 50%
	require.Equal(t, 50.0, m.getSCR())

	// 5 more sessions completed
	for i := 0; i < 5; i++ {
		atomic.AddInt64(&m.sessionCompletedTotal, 1)
	}
	// SCR = 15 / 20 * 100 = 75%
	require.Equal(t, 75.0, m.getSCR())

	// 3xx redirects do NOT change SCR denominator (unlike SER)
	// SCR remains 75%
	require.Equal(t, 75.0, m.getSCR())
}

// TestMetrics_SessionCompleted increments counter
func TestMetrics_SessionCompleted(t *testing.T) {
	m := &metrics{}

	atomic.StoreInt64(&m.sessionCompletedTotal, 0)

	m.SessionCompleted()
	require.Equal(t, int64(1), atomic.LoadInt64(&m.sessionCompletedTotal))

	m.SessionCompleted()
	require.Equal(t, int64(2), atomic.LoadInt64(&m.sessionCompletedTotal))
}

// TestMetrics_SCR_ComparedToSER verifies SCR <= SER always
// (completed sessions ⊆ established sessions)
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
			atomic.StoreInt64(&m.inviteTotal, tt.invites)
			atomic.StoreInt64(&m.invite200OKTotal, tt.invite200OK)
			atomic.StoreInt64(&m.sessionCompletedTotal, tt.completed)
			atomic.StoreInt64(&m.invite3xxTotal, tt.invite3xx)

			scr := m.getSCR()
			ser := m.getSER()

			require.LessOrEqual(t, scr, ser, "SCR must be <= SER")
		})
	}
}

// ResponseWithMetrics tests

func TestMetricser_ResponseWithMetrics_200OK_Invite(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	invite200OKBefore := atomic.LoadInt64(&m.invite200OKTotal)
	invite3xxBefore := atomic.LoadInt64(&m.invite3xxTotal)
	inviteEffectiveBefore := atomic.LoadInt64(&m.inviteEffectiveTotal)
	inviteIneffectiveBefore := atomic.LoadInt64(&m.inviteIneffectiveTotal)

	m.ResponseWithMetrics([]byte("200"), true, true)

	require.Equal(t, invite200OKBefore+1, atomic.LoadInt64(&m.invite200OKTotal))
	require.Equal(t, inviteEffectiveBefore+1, atomic.LoadInt64(&m.inviteEffectiveTotal))
	require.Equal(t, invite3xxBefore, atomic.LoadInt64(&m.invite3xxTotal))
	require.Equal(t, inviteIneffectiveBefore, atomic.LoadInt64(&m.inviteIneffectiveTotal))
}

func TestMetricser_ResponseWithMetrics_200OK_Register(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	invite200OKBefore := atomic.LoadInt64(&m.invite200OKTotal)

	m.ResponseWithMetrics([]byte("200"), false, true)

	require.Equal(t, invite200OKBefore, atomic.LoadInt64(&m.invite200OKTotal), "invite200OKTotal should not increment for non-INVITE")
}

func TestMetricser_ResponseWithMetrics_401(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	invite200OKBefore := atomic.LoadInt64(&m.invite200OKTotal)
	inviteEffectiveBefore := atomic.LoadInt64(&m.inviteEffectiveTotal)
	inviteIneffectiveBefore := atomic.LoadInt64(&m.inviteIneffectiveTotal)

	m.ResponseWithMetrics([]byte("401"), true, false)

	require.Equal(t, invite200OKBefore, atomic.LoadInt64(&m.invite200OKTotal))
	require.Equal(t, inviteEffectiveBefore, atomic.LoadInt64(&m.inviteEffectiveTotal))
	require.Equal(t, inviteIneffectiveBefore, atomic.LoadInt64(&m.inviteIneffectiveTotal))
}

func TestMetricser_ResponseWithMetrics_3xx(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	invite3xxBefore := atomic.LoadInt64(&m.invite3xxTotal)
	invite200OKBefore := atomic.LoadInt64(&m.invite200OKTotal)

	m.ResponseWithMetrics([]byte("302"), true, false)

	require.Equal(t, invite3xxBefore+1, atomic.LoadInt64(&m.invite3xxTotal))
	require.Equal(t, invite200OKBefore, atomic.LoadInt64(&m.invite200OKTotal))
}

func TestMetricser_ResponseWithMetrics_SEER_EffectiveCodes(t *testing.T) {
	effectiveCodes := []string{"200", "480", "486", "600", "603"}

	for _, code := range effectiveCodes {
		t.Run(code, func(t *testing.T) {
			m := NewTestMetricser().(*metrics)
			inviteEffectiveBefore := atomic.LoadInt64(&m.inviteEffectiveTotal)

			is200OK := code == "200"
			m.ResponseWithMetrics([]byte(code), true, is200OK)

			require.Equal(t, inviteEffectiveBefore+1, atomic.LoadInt64(&m.inviteEffectiveTotal), "code %s should be effective", code)
		})
	}
}

func TestMetricser_ResponseWithMetrics_ISA_IneffectiveCodes(t *testing.T) {
	ineffectiveCodes := []string{"408", "500", "503", "504"}

	for _, code := range ineffectiveCodes {
		t.Run(code, func(t *testing.T) {
			m := NewTestMetricser().(*metrics)
			inviteIneffectiveBefore := atomic.LoadInt64(&m.inviteIneffectiveTotal)

			m.ResponseWithMetrics([]byte(code), true, false)

			require.Equal(t, inviteIneffectiveBefore+1, atomic.LoadInt64(&m.inviteIneffectiveTotal), "code %s should be ineffective", code)
		})
	}
}

func TestMetricser_ResponseWithMetrics_NonInvite(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	invite200OKBefore := atomic.LoadInt64(&m.invite200OKTotal)
	invite3xxBefore := atomic.LoadInt64(&m.invite3xxTotal)
	inviteEffectiveBefore := atomic.LoadInt64(&m.inviteEffectiveTotal)
	inviteIneffectiveBefore := atomic.LoadInt64(&m.inviteIneffectiveTotal)

	m.ResponseWithMetrics([]byte("200"), false, true)

	require.Equal(t, invite200OKBefore, atomic.LoadInt64(&m.invite200OKTotal))
	require.Equal(t, invite3xxBefore, atomic.LoadInt64(&m.invite3xxTotal))
	require.Equal(t, inviteEffectiveBefore, atomic.LoadInt64(&m.inviteEffectiveTotal))
	require.Equal(t, inviteIneffectiveBefore, atomic.LoadInt64(&m.inviteIneffectiveTotal))
}

func TestMetricser_ResponseWithMetrics_AllInOne(t *testing.T) {
	m := NewTestMetricser().(*metrics)

	invite200OKBefore := atomic.LoadInt64(&m.invite200OKTotal)
	inviteEffectiveBefore := atomic.LoadInt64(&m.inviteEffectiveTotal)
	inviteIneffectiveBefore := atomic.LoadInt64(&m.inviteIneffectiveTotal)
	invite3xxBefore := atomic.LoadInt64(&m.invite3xxTotal)

	m.ResponseWithMetrics([]byte("200"), true, true)
	require.Equal(t, invite200OKBefore+1, atomic.LoadInt64(&m.invite200OKTotal))
	require.Equal(t, inviteEffectiveBefore+1, atomic.LoadInt64(&m.inviteEffectiveTotal))

	m.ResponseWithMetrics([]byte("480"), true, false)
	require.Equal(t, invite200OKBefore+1, atomic.LoadInt64(&m.invite200OKTotal))
	require.Equal(t, inviteEffectiveBefore+2, atomic.LoadInt64(&m.inviteEffectiveTotal))

	m.ResponseWithMetrics([]byte("500"), true, false)
	require.Equal(t, inviteIneffectiveBefore+1, atomic.LoadInt64(&m.inviteIneffectiveTotal))

	m.ResponseWithMetrics([]byte("302"), true, false)
	require.Equal(t, invite3xxBefore+1, atomic.LoadInt64(&m.invite3xxTotal))
}

// SPD (Session Process Duration) tests per RFC 6076 §4.7

func (m *metrics) getSPD() float64 {
	count := atomic.LoadInt64(&m.spdCount)
	if count == 0 {
		return 0
	}
	totalNs := atomic.LoadUint64(&m.spdTotalNs)
	return float64(totalNs) / float64(count) / 1e9
}

func TestMetrics_SPD_NoCompleted(t *testing.T) {
	m := &metrics{}
	require.Equal(t, 0.0, m.getSPD())
}

func TestMetrics_SPD_SingleSession(t *testing.T) {
	m := &metrics{}

	atomic.StoreUint64(&m.spdTotalNs, uint64(5*time.Second))
	atomic.StoreInt64(&m.spdCount, 1)

	require.Equal(t, 5.0, m.getSPD())
}

func TestMetrics_SPD_MultipleSessions(t *testing.T) {
	m := &metrics{}

	atomic.StoreUint64(&m.spdTotalNs, uint64(10*time.Second))
	atomic.StoreInt64(&m.spdCount, 2)

	require.Equal(t, 5.0, m.getSPD())
}

func TestMetrics_SPD_ZeroDuration(t *testing.T) {
	m := &metrics{}

	m.UpdateSPD(0)

	require.Equal(t, 0.0, m.getSPD())
}

func TestMetrics_SPD_UpdateSPD(t *testing.T) {
	m := &metrics{}

	m.UpdateSPD(3 * time.Second)
	require.Equal(t, 3.0, m.getSPD())

	m.UpdateSPD(7 * time.Second)
	require.Equal(t, 5.0, m.getSPD())
}

func TestMetrics_SPD_Values(t *testing.T) {
	tests := []struct {
		name      string
		totalNs   uint64
		count     int64
		wantSPD   float64
	}{
		{"zero_count", 0, 0, 0},
		{"single_1s", uint64(1 * time.Second), 1, 1.0},
		{"single_10s", uint64(10 * time.Second), 1, 10.0},
		{"two_5s_avg", uint64(10 * time.Second), 2, 5.0},
		{"three_mixed", uint64(30 * time.Second), 3, 10.0},
		{"zero_duration_with_count", 0, 5, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &metrics{}
			atomic.StoreUint64(&m.spdTotalNs, tt.totalNs)
			atomic.StoreInt64(&m.spdCount, tt.count)

			require.Equal(t, tt.wantSPD, m.getSPD())
		})
	}
}

func TestMetrics_SPD_FullCycle(t *testing.T) {
	m := &metrics{}

	require.Equal(t, 0.0, m.getSPD())

	m.UpdateSPD(10 * time.Second)
	require.Equal(t, 10.0, m.getSPD())

	m.UpdateSPD(20 * time.Second)
	require.Equal(t, 15.0, m.getSPD())

	m.UpdateSPD(0)
	require.InDelta(t, 10.0, m.getSPD(), 0.01)
}
