//go:build e2e

package load

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type closeErrorBody struct {
	io.Reader
}

func (closeErrorBody) Close() error {
	return errors.New("close")
}

type scrapeRoundTripFunc func(*http.Request) (*http.Response, error)

func (f scrapeRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestScrapeOnceRequiresStatusAndCompleteBody(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
		wantLen int64
	}{
		{name: "complete", handler: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("metric 1\n"))
		}, wantLen: 9},
		{name: "non 200", handler: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}, wantErr: true},
		{name: "truncated", handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "20")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("short"))
		}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			observation := scrapeOnce(t.Context(), server.Client(), server.URL)

			if tt.wantErr {
				require.NotEmpty(t, observation.Err)
				return
			}
			require.Empty(t, observation.Err)
			require.Equal(t, http.StatusOK, observation.StatusCode)
			require.Equal(t, tt.wantLen, observation.BodyBytes)
			require.Positive(t, observation.Duration)
		})
	}
}

func TestScrapeOnceHonorsCanceledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintln(w, "metric 1")
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	observation := scrapeOnce(ctx, server.Client(), server.URL)

	require.NotEmpty(t, observation.Err)
}

func TestScrapeOnceReportsBodyCloseFailure(t *testing.T) {
	client := &http.Client{Transport: scrapeRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       closeErrorBody{Reader: strings.NewReader("metric 1\n")},
		}, nil
	})}

	observation := scrapeOnce(t.Context(), client, "http://metrics.test")

	require.Contains(t, observation.Err, "close")
}

func TestSummarizeScrapesUsesCompleteInPhaseObservations(t *testing.T) {
	start := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Second)
	observations := []scrapeObservation{
		{StartedAt: start.Add(-time.Nanosecond), Duration: time.Second, StatusCode: 200, BodyBytes: 1},
		{StartedAt: start, Duration: 10 * time.Millisecond, StatusCode: 200, BodyBytes: 1},
		{StartedAt: start.Add(time.Nanosecond), Duration: 20 * time.Millisecond, StatusCode: 200, BodyBytes: 1},
		{StartedAt: end, Duration: time.Second, StatusCode: 200, BodyBytes: 1},
	}

	got, err := summarizeScrapes(observations, start, end)

	require.NoError(t, err)
	require.Equal(t, 2, got.Count)
	require.InDelta(t, 15, got.P50MS, 0.000001)
	require.InDelta(t, 19.5, got.P95MS, 0.000001)
	require.InDelta(t, 19.9, got.P99MS, 0.000001)
}

func TestSummarizeScrapesFailsClosed(t *testing.T) {
	start := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Second)
	tests := []struct {
		name         string
		observations []scrapeObservation
	}{
		{name: "empty"},
		{name: "transport error", observations: []scrapeObservation{{StartedAt: start, Err: "failed"}}},
		{name: "wrong status", observations: []scrapeObservation{{StartedAt: start, StatusCode: 500, BodyBytes: 1}}},
		{name: "empty body", observations: []scrapeObservation{{StartedAt: start, StatusCode: 200}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := summarizeScrapes(tt.observations, start, end)
			require.Error(t, err)
		})
	}
}

func TestValidateScrapeGatesUsesStrictBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		summary ScrapeSummary
		wantErr bool
	}{
		{name: "below", summary: ScrapeSummary{Count: 1, P95MS: 99.999999, P99MS: 199.999999}},
		{name: "p95 equal", summary: ScrapeSummary{Count: 1, P95MS: 100, P99MS: 199}, wantErr: true},
		{name: "p95 above", summary: ScrapeSummary{Count: 1, P95MS: 100.000001, P99MS: 199}, wantErr: true},
		{name: "p99 equal", summary: ScrapeSummary{Count: 1, P95MS: 99, P99MS: 200}, wantErr: true},
		{name: "p99 above", summary: ScrapeSummary{Count: 1, P95MS: 99, P99MS: 200.000001}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateScrapeGates(tt.summary)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
