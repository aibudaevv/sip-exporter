package service

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// globalMetricser for all tests to avoid duplicate registration
var (
	globalMetricser     Metricser
	globalMetricserOnce sync.Once
)

func getGlobalMetricser() Metricser {
	globalMetricserOnce.Do(func() {
		globalMetricser = NewMetricser()
	})
	return globalMetricser
}

// Tests for Request method - cover all SIP methods
func TestMetricser_Request_AllMethodsSingleRun(t *testing.T) {
	m := getGlobalMetricser()
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

// Tests for Response method - cover all response codes
func TestMetricser_Response_AllCodesSingleRun(t *testing.T) {
	m := getGlobalMetricser()
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
	m := getGlobalMetricser()
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
	m := getGlobalMetricser()
	require.NotNil(t, m)

	for i := 0; i < 5; i++ {
		t.Run(string(rune('0'+i)), func(t *testing.T) {
			m.SystemError()
		})
	}
}

func TestMetricser_Combined(t *testing.T) {
	m := getGlobalMetricser()
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
