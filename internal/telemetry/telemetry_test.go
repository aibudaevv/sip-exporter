package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestGetOrCreateIDLoadExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anon_id")
	expectedID := uuid.New().String()
	require.NoError(t, os.WriteFile(path, []byte(expectedID+"\n\ngarbage\n"), 0644))

	id := getOrCreateID(path)

	require.Equal(t, expectedID, id)
}

func TestGetOrCreateIDGenerateNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anon_id")

	require.NoFileExists(t, path)

	id := getOrCreateID(path)

	parsed, err := uuid.Parse(id)
	require.NoError(t, err, "generated ID must be a valid UUID")
	require.NotEqual(t, uuid.Nil, parsed)

	data, err := os.ReadFile(path)
	require.NoError(t, err, "ID file must be written")
	require.Contains(t, string(data), id)
	require.Contains(t, string(data), "anonymous ID used for telemetry")
}

func TestGetOrCreateIDGenerateNewCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "nested", "anon_id")

	require.NoDirExists(t, filepath.Join(dir, "subdir"))

	id := getOrCreateID(path)

	_, err := uuid.Parse(id)
	require.NoError(t, err)
	require.FileExists(t, path, "ID file must exist after generation")
}

func TestGetOrCreateIDEmptyPathEphemeral(t *testing.T) {
	id := getOrCreateID("")

	_, err := uuid.Parse(id)
	require.NoError(t, err, "empty path must still return a valid UUID")
}

func TestGetOrCreateIDInvalidContentOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anon_id")
	require.NoError(t, os.WriteFile(path, []byte("not-a-uuid\n"), 0644))

	id := getOrCreateID(path)

	_, err := uuid.Parse(id)
	require.NoError(t, err, "invalid content must be replaced with valid UUID")
	require.NotEqual(t, "not-a-uuid", id)
}

func TestGetOrCreateIDWriteFailEphemeral(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "anon_id")

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "subdir"), 0444))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "subdir"), 0755) })

	id := getOrCreateID(path)

	_, err := uuid.Parse(id)
	require.NoError(t, err, "write failure must fall back to ephemeral UUID")
}

func TestSendBeaconCorrectParams(t *testing.T) {
	var capturedQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := sendBeacon(t.Context(), server.URL, beaconData{
		anonID: "550e8400-e29b-41d4-a716-446655440000",
		uptime: 2 * time.Hour,
	})

	require.NoError(t, err)
	require.Equal(t, "550e8400-e29b-41d4-a716-446655440000", capturedQuery.Get("anon_id"))
	require.NotEmpty(t, capturedQuery.Get("version"))
	require.NotEmpty(t, capturedQuery.Get("os"))
	require.NotEmpty(t, capturedQuery.Get("arch"))
	require.Equal(t, "7200", capturedQuery.Get("uptime"))
}

func TestSendBeaconHTTPErrorNoCrash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	err := sendBeacon(t.Context(), server.URL, beaconData{
		anonID: uuid.New().String(),
		uptime: time.Minute,
	})

	require.NoError(t, err, "HTTP error status must not cause sendBeacon to fail")
}

func TestSendBeaconUnreachableURLNoCrash(t *testing.T) {
	_, cancel := context.WithCancel(t.Context())
	cancel()

	err := sendBeacon(t.Context(), "http://127.0.0.1:0/beacon", beaconData{
		anonID: uuid.New().String(),
		uptime: time.Minute,
	})

	require.Error(t, err, "unreachable URL must return error, not panic")
}

func TestRunDisabled(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	done := make(chan struct{})
	go func() {
		Run(t.Context(), Config{
			Enabled: false,
			URL:     server.URL,
		}, time.Now())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run with disabled telemetry must return immediately")
	}

	require.Equal(t, int32(0), requestCount.Load(), "no beacon must be sent when disabled")
}

func TestRunImmediateBeacon(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())

	go Run(ctx, Config{
		Enabled:  true,
		URL:      server.URL,
		IDFile:   filepath.Join(t.TempDir(), "anon_id"),
		Interval: time.Hour,
	}, time.Now())

	require.Eventually(t, func() bool {
		return requestCount.Load() >= 1
	}, time.Second, 50*time.Millisecond, "immediate beacon must be sent on startup")

	cancel()
}

func TestRunContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})
	go func() {
		Run(ctx, Config{
			Enabled:  true,
			URL:      server.URL,
			IDFile:   filepath.Join(t.TempDir(), "anon_id"),
			Interval: time.Hour,
		}, time.Now())
		close(done)
	}()

	require.Eventually(t, func() bool {
		return true
	}, 200*time.Millisecond, 50*time.Millisecond)

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run must return after context cancel")
	}
}
