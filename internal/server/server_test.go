package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gitlab.com/sip-exporter/internal/config"
	"gitlab.com/sip-exporter/internal/exporter"
	"go.uber.org/zap"
)

// mockExporter for testing
type mockExporter struct {
	initializeCalled bool
	initializeErr    error
	closeCalled      bool
	isAlive          bool
}

func (m *mockExporter) Initialize(interfaceName string, path string, sipPort, sipsPort int) error {
	m.initializeCalled = true
	m.isAlive = true
	return m.initializeErr
}

func (m *mockExporter) Close() {
	m.closeCalled = true
	m.isAlive = false
}

func (m *mockExporter) IsAlive() bool {
	return m.isAlive
}

func TestNewServer(t *testing.T) {
	srv := NewServer(nil)
	require.NotNil(t, srv)

	s, ok := srv.(*server)
	require.True(t, ok)
	require.NotNil(t, s.exporter)
}

func TestServer_Run_Success(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	zap.ReplaceGlobals(logger)

	s := &server{
		exporter: &mockExporter{},
	}

	cfg := &config.App{
		Interface:     "lo",
		Port:          "2113",
		BPFBinaryPath: "/tmp/nonexistent.o",
		SIPPort:       5060,
		SIPSPort:      5061,
	}

	done := make(chan struct{})

	go func() {
		err := s.Run(cfg)
		if err != nil {
			t.Logf("Server error: %v", err)
		}
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)

	p, _ := os.FindProcess(os.Getpid())
	_ = p.Signal(syscall.SIGTERM)

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Server did not shutdown in time")
	}

	mExp := s.exporter.(*mockExporter)
	require.True(t, mExp.closeCalled)
}

func TestServer_Run_InitializeError(t *testing.T) {
	s := &server{
		exporter: &mockExporter{
			initializeErr: exporter.ErrUserNotRoot,
		},
	}

	cfg := &config.App{
		Interface: "lo",
		Port:      "2112",
	}

	err := s.Run(cfg)

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed initialized exporter")
}

func TestShutDownTimeout_Constant(t *testing.T) {
	require.Equal(t, 10*time.Second, shutDownTimeout)
}

func TestServer_Run_ContextDeadline(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	zap.ReplaceGlobals(logger)

	s := &server{
		exporter: &mockExporter{},
	}

	cfg := &config.App{
		Interface: "lo",
		Port:      "2114",
	}

	done := make(chan struct{})

	go func() {
		err := s.Run(cfg)
		if err != nil {
			t.Logf("Server error: %v", err)
		}
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)

	p, _ := os.FindProcess(os.Getpid())
	_ = p.Signal(syscall.SIGINT)

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Server did not shutdown in time")
	}
}

func TestServer_MetricsHandler(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
}
