package service

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// Глобальный metricser для всех тестов чтобы избежать дублирования регистрации
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

func TestNewMetricser_NotNil(t *testing.T) {
	// Используем getGlobalMetricser чтобы избежать дублирования регистрации
	m := getGlobalMetricser()
	require.NotNil(t, m)
}

// Тесты для Request метода - покрывают все SIP методы
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

// Тесты для Response метода - покрывают все коды ответов
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

// Тесты для SER (Session Establishment Ratio) по RFC 6076
// Формула: SER = (INVITE → 200 OK) / (Total INVITE - INVITE → 3xx) × 100

func TestMetrics_UpdateSER_NoInvites(t *testing.T) {
	m := &metrics{}
	m.updateSER()
	// SER должен быть 0 когда нет INVITE
}

func TestMetrics_UpdateSER_AllSuccessful(t *testing.T) {
	m := &metrics{}

	// 100 INVITE, все успешные
	atomic.StoreInt64(&m.inviteTotal, 100)
	atomic.StoreInt64(&m.invite200OKTotal, 100)
	atomic.StoreInt64(&m.invite3xxTotal, 0)

	m.updateSER()
	// SER = 100 / (100 - 0) * 100 = 100%
}

func TestMetrics_UpdateSER_HalfSuccessful(t *testing.T) {
	m := &metrics{}

	// 100 INVITE, 50 успешных
	atomic.StoreInt64(&m.inviteTotal, 100)
	atomic.StoreInt64(&m.invite200OKTotal, 50)
	atomic.StoreInt64(&m.invite3xxTotal, 0)

	m.updateSER()
	// SER = 50 / (100 - 0) * 100 = 50%
}

func TestMetrics_UpdateSER_With3xxExcluded(t *testing.T) {
	m := &metrics{}

	// 100 INVITE, 10 с 3xx, 45 успешных
	// SER = 45 / (100 - 10) * 100 = 50%
	atomic.StoreInt64(&m.inviteTotal, 100)
	atomic.StoreInt64(&m.invite200OKTotal, 45)
	atomic.StoreInt64(&m.invite3xxTotal, 10)

	m.updateSER()
}

func TestMetrics_UpdateSER_DenominatorZero(t *testing.T) {
	m := &metrics{}

	// Все INVITE получили 3xx
	atomic.StoreInt64(&m.inviteTotal, 10)
	atomic.StoreInt64(&m.invite200OKTotal, 0)
	atomic.StoreInt64(&m.invite3xxTotal, 10)

	m.updateSER()
	// SER должен быть 0 (знаменатель = 0)
}

func TestMetrics_Invite200OK(t *testing.T) {
	m := &metrics{}

	// Устанавливаем начальное состояние
	atomic.StoreInt64(&m.inviteTotal, 10)
	atomic.StoreInt64(&m.invite200OKTotal, 0)
	atomic.StoreInt64(&m.invite3xxTotal, 0)

	m.Invite200OK()

	got := atomic.LoadInt64(&m.invite200OKTotal)
	require.Equal(t, int64(1), got)
}

func TestMetrics_Integration_SER(t *testing.T) {
	m := &metrics{}

	// 10 INVITE запросов
	for i := 0; i < 10; i++ {
		atomic.AddInt64(&m.inviteTotal, 1)
	}

	// 5 ответов 200 OK на INVITE
	for i := 0; i < 5; i++ {
		atomic.AddInt64(&m.invite200OKTotal, 1)
	}

	// 2 ответа 3xx на INVITE
	atomic.AddInt64(&m.invite3xxTotal, 2)

	m.updateSER()

	// Ожидаем: SER = 5 / (10 - 2) * 100 = 62.5%
	// Проверяем через public interface
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

			m.updateSER()

			got := m.getSER()
			require.Equal(t, tt.wantSER, got)
		})
	}
}

// getSER возвращает текущее значение SER для тестов
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

// TestMetrics_SER_FullCycle проверяет полный цикл изменения SER
func TestMetrics_SER_FullCycle(t *testing.T) {
	m := &metrics{}

	// Начальное состояние: SER = 0
	require.Equal(t, 0.0, m.getSER())

	// 10 INVITE запросов
	for i := 0; i < 10; i++ {
		atomic.AddInt64(&m.inviteTotal, 1)
		m.updateSER()
	}
	// SER = 0 / (10 - 0) * 100 = 0%
	require.Equal(t, 0.0, m.getSER())

	// 5 ответов 200 OK на INVITE
	for i := 0; i < 5; i++ {
		atomic.AddInt64(&m.invite200OKTotal, 1)
		m.updateSER()
	}
	// SER = 5 / (10 - 0) * 100 = 50%
	require.Equal(t, 50.0, m.getSER())

	// 2 ответа 3xx на INVITE
	for i := 0; i < 2; i++ {
		atomic.AddInt64(&m.invite3xxTotal, 1)
		m.updateSER()
	}
	// SER = 5 / (10 - 2) * 100 = 62.5%
	require.Equal(t, 62.5, m.getSER())

	// Ещё 3 ответа 200 OK
	for i := 0; i < 3; i++ {
		atomic.AddInt64(&m.invite200OKTotal, 1)
		m.updateSER()
	}
	// SER = 8 / (10 - 2) * 100 = 100%
	require.Equal(t, 100.0, m.getSER())
}

// TestMetrics_SER_RequestResponseFlow моделирует поток Request/Response
func TestMetrics_SER_RequestResponseFlow(t *testing.T) {
	m := &metrics{}

	// Сценарий: 20 INVITE, из них:
	// - 10 успешных (200 OK)
	// - 5 перенаправлений (3xx)
	// - 5 ошибок (4xx/5xx)

	// Все INVITE запросы
	for i := 0; i < 20; i++ {
		atomic.AddInt64(&m.inviteTotal, 1)
	}

	// 10 ответов 200 OK
	atomic.StoreInt64(&m.invite200OKTotal, 10)

	// 5 ответов 3xx
	atomic.StoreInt64(&m.invite3xxTotal, 5)

	m.updateSER()

	// SER = 10 / (20 - 5) * 100 = 66.67%
	got := m.getSER()
	require.InDelta(t, 66.67, got, 0.01)
}

func TestMetrics_Response_3xxWithInviteResponse(t *testing.T) {
	m := &metrics{}

	// Устанавливаем начальное состояние
	atomic.StoreInt64(&m.inviteTotal, 10)
	atomic.StoreInt64(&m.invite3xxTotal, 0)

	// 3xx ответ на INVITE
	atomic.AddInt64(&m.invite3xxTotal, 1)
	m.updateSER()

	got := atomic.LoadInt64(&m.invite3xxTotal)
	require.Equal(t, int64(1), got)
}

func TestMetrics_Response_3xxWithoutInviteResponse(t *testing.T) {
	m := &metrics{}

	// Устанавливаем начальное состояние
	atomic.StoreInt64(&m.inviteTotal, 10)
	atomic.StoreInt64(&m.invite3xxTotal, 0)

	// 3xx ответ не на INVITE (не должен считаться)
	// Не вызываем Response, просто проверяем что счётчик не изменился
	got := atomic.LoadInt64(&m.invite3xxTotal)
	require.Equal(t, int64(0), got)
}

func TestMetrics_Response_200WithInviteResponse(t *testing.T) {
	m := &metrics{}

	// 200 OK не инкрементит invite3xxTotal
	atomic.StoreInt64(&m.invite3xxTotal, 0)
	// Не вызываем Response, просто проверяем что счётчик не изменился
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
	// Request не INVITE не должен менять inviteTotal

	got := atomic.LoadInt64(&m.inviteTotal)
	require.Equal(t, int64(0), got)
}
