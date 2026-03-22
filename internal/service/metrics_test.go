package service

import (
	"sync"
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
		name string
		data []byte
	}{
		{"100", []byte("100")},
		{"180", []byte("180")},
		{"183", []byte("183")},
		{"200", []byte("200")},
		{"202", []byte("202")},
		{"300", []byte("300")},
		{"302", []byte("302")},
		{"400", []byte("400")},
		{"401", []byte("401")},
		{"403", []byte("403")},
		{"404", []byte("404")},
		{"407", []byte("407")},
		{"408", []byte("408")},
		{"480", []byte("480")},
		{"486", []byte("486")},
		{"500", []byte("500")},
		{"503", []byte("503")},
		{"600", []byte("600")},
		{"603", []byte("603")},
		{"UNKNOWN", []byte("999")},
		{"EMPTY", []byte("")},
	}

	for _, code := range codes {
		t.Run(code.name, func(t *testing.T) {
			m.Response(code.data)
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
	m.Response([]byte("200"))
	m.UpdateSession(10)
	m.SystemError()
}
