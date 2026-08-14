//go:build e2e

package e2e

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMetricSamplesRequireExactName(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte("sip_exporter_sessions_limit{carrier=\"test\"} 10\n"))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	require.False(t, metricExists(t, server.URL, "sip_exporter_sessions"))
}

func TestAssertDialogTeardownAllowsMissingSessionsAfterSuccessfulInvite(t *testing.T) {
	t.Parallel()

	const minimumSnapshotDelay = 1100 * time.Millisecond
	const healthyMetrics = "sip_exporter_socket_packets_received_total{iface=\"lo\"} 1\n" +
		"sip_exporter_socket_packets_dropped_total{iface=\"lo\"} 0\n" +
		"sip_exporter_channel_length 0\n" +
		"sip_exporter_channel_capacity 10000\n" +
		"sip_exporter_active_dialogs 0\n" +
		"sip_exporter_active_trackers{type=\"invite\"} 0\n"

	tests := []struct {
		name string
		body string
		run  func(*testing.T, string)
	}{
		{
			name: "without carrier",
			body: "sip_exporter_invite_200_total 1\n" + healthyMetrics,
			run:  assertDialogTeardown,
		},
		{
			name: "with carrier",
			body: "sip_exporter_invite_200_total{carrier=\"carrier-a\"} 1\n" + healthyMetrics,
			run: func(t *testing.T, endpoint string) {
				env := testEnv{endpoint: endpoint, carrier: "carrier-a"}
				env.assertDialogTeardownByCarrier(t)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var requestMu sync.Mutex
			var requestTimes []time.Time
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requestMu.Lock()
				requestTimes = append(requestTimes, time.Now())
				requestMu.Unlock()
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(server.Close)

			tt.run(t, server.URL)

			requestMu.Lock()
			times := append([]time.Time(nil), requestTimes...)
			requestMu.Unlock()
			require.GreaterOrEqual(t, len(times), 2)
			require.GreaterOrEqual(t, times[1].Sub(times[0]), minimumSnapshotDelay,
				"self-monitoring scrape must wait for a post-flow snapshot")
		})
	}
}

func TestMetricSamples(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		metricName string
		want       []metricSample
	}{
		{
			name:       "exact scalar",
			body:       "sip_exporter_packets_total 12\n",
			metricName: "sip_exporter_packets_total",
			want: []metricSample{{
				name: "sip_exporter_packets_total", labels: map[string]string{}, value: 12,
			}},
		},
		{
			name:       "exact scalar with timestamp",
			body:       "sip_exporter_packets_total 12 1750000000000\n",
			metricName: "sip_exporter_packets_total",
			want: []metricSample{{
				name: "sip_exporter_packets_total", labels: map[string]string{}, value: 12,
			}},
		},
		{
			name:       "exact labeled sample",
			body:       "sip_exporter_sessions{carrier=\"carrier-a\",direction=\"inbound\"} 3\n",
			metricName: "sip_exporter_sessions",
			want: []metricSample{{
				name: "sip_exporter_sessions",
				labels: map[string]string{
					"carrier":   "carrier-a",
					"direction": "inbound",
				},
				value: 3,
			}},
		},
		{
			name:       "quoted label values",
			body:       "sip_exporter_sessions{carrier=\"carrier a\",note=\"quoted \\\"value\\\"\"} 4\n",
			metricName: "sip_exporter_sessions",
			want: []metricSample{{
				name: "sip_exporter_sessions",
				labels: map[string]string{
					"carrier": "carrier a",
					"note":    `quoted "value"`,
				},
				value: 4,
			}},
		},
		{
			name:       "escaped trailing backslash",
			body:       `sip_exporter_sessions{path="C:\\"} 5` + "\n",
			metricName: "sip_exporter_sessions",
			want: []metricSample{{
				name: "sip_exporter_sessions", labels: map[string]string{"path": `C:\`}, value: 5,
			}},
		},
		{
			name:       "absent sample",
			body:       "sip_exporter_packets_total 12\n",
			metricName: "sip_exporter_sessions",
		},
		{
			name: "help and type only",
			body: "# HELP sip_exporter_sessions Number of sessions\n" +
				"# TYPE sip_exporter_sessions gauge\n",
			metricName: "sip_exporter_sessions",
		},
		{
			name: "multiple label sets",
			body: "sip_exporter_sessions{carrier=\"carrier-a\"} 1\n" +
				"sip_exporter_sessions{carrier=\"carrier-b\"} 2\n",
			metricName: "sip_exporter_sessions",
			want: []metricSample{
				{name: "sip_exporter_sessions", labels: map[string]string{"carrier": "carrier-a"}, value: 1},
				{name: "sip_exporter_sessions", labels: map[string]string{"carrier": "carrier-b"}, value: 2},
			},
		},
		{
			name:       "malformed value",
			body:       "sip_exporter_sessions{carrier=\"carrier-a\"} invalid\n",
			metricName: "sip_exporter_sessions",
		},
		{
			name:       "malformed trailing label comma",
			body:       "sip_exporter_sessions{carrier=\"carrier-a\",} 1\n",
			metricName: "sip_exporter_sessions",
		},
		{
			name:       "malformed duplicate label",
			body:       "sip_exporter_sessions{carrier=\"carrier-a\",carrier=\"carrier-b\"} 1\n",
			metricName: "sip_exporter_sessions",
		},
		{
			name:       "malformed label escape",
			body:       `sip_exporter_sessions{carrier="bad\tvalue"} 1` + "\n",
			metricName: "sip_exporter_sessions",
		},
		{
			name:       "malformed extra field",
			body:       "sip_exporter_sessions 1 garbage\n",
			metricName: "sip_exporter_sessions",
		},
		{
			name:       "malformed infinity",
			body:       "sip_exporter_sessions Infinity\n",
			metricName: "sip_exporter_sessions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(server.Close)

			require.Equal(t, tt.want, readMetricSamples(t, server.URL, tt.metricName))
		})
	}
}

func TestMetricSamplesMatchLabelsExactly(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(
			"sip_exporter_sessions{carrier=\"carrier-a\",direction=\"inbound\",ua_type=\"phone\"} 3\n",
		))
	}))
	t.Cleanup(server.Close)

	tests := []struct {
		name        string
		labelFilter string
		want        bool
	}{
		{name: "filter order is irrelevant", labelFilter: `direction="inbound",carrier="carrier-a"`, want: true},
		{name: "extra sample labels are allowed", labelFilter: `carrier="carrier-a"`, want: true},
		{name: "label value is exact", labelFilter: `carrier="carrier"`, want: false},
		{name: "missing label does not match", labelFilter: `iface="lo"`, want: false},
		{name: "missing empty label does not match", labelFilter: `iface=""`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want,
				metricWithLabelExists(t, server.URL, "sip_exporter_sessions", tt.labelFilter))
		})
	}
}

func TestMetricSamplesReadExactLabeledValue(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(
			"sip_exporter_sessions{carrier=\"carrier-a\",direction=\"inbound\",ua_type=\"phone\"} 3\n",
		))
	}))
	t.Cleanup(server.Close)

	require.Equal(t, 3.0, getMetricWithLabel(t, server.URL, "sip_exporter_sessions",
		`direction="inbound",carrier="carrier-a"`))
}

func TestMetricSamplesSumExplicitly(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(
			"sip_exporter_socket_packets_received_total{iface=\"lo\"} 3\n" +
				"sip_exporter_socket_packets_received_total{iface=\"eth0\"} 4\n",
		))
	}))
	t.Cleanup(server.Close)

	require.Equal(t, 7.0, getMetricSum(t, server.URL, "sip_exporter_socket_packets_received_total"))
}

func TestMetricSamplesRequireSingleValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		samples []metricSample
		want    float64
		wantErr bool
	}{
		{name: "zero matches", wantErr: true},
		{name: "one match", samples: []metricSample{{value: 3}}, want: 3},
		{name: "multiple matches", samples: []metricSample{{value: 3}, {value: 4}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := singleMetricValue(tt.samples)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMetricSamplesRequireAllZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		metricName string
		samples    []metricSample
		wantError  string
	}{
		{
			name:       "missing sessions",
			metricName: "sip_exporter_sessions",
			wantError:  "sip_exporter_sessions has no samples",
		},
		{
			name:       "one active session",
			metricName: "sip_exporter_sessions",
			samples: []metricSample{{
				name: "sip_exporter_sessions", labels: map[string]string{"carrier": "carrier-a"}, value: 1,
			}},
			wantError: `sip_exporter_sessions has non-zero samples: sip_exporter_sessions{carrier="carrier-a"}=1`,
		},
		{
			name:       "second session series is active",
			metricName: "sip_exporter_sessions",
			samples: []metricSample{
				{name: "sip_exporter_sessions", labels: map[string]string{"carrier": "carrier-a"}, value: 0},
				{name: "sip_exporter_sessions", labels: map[string]string{"carrier": "carrier-b"}, value: 1},
			},
			wantError: `sip_exporter_sessions has non-zero samples: sip_exporter_sessions{carrier="carrier-b"}=1`,
		},
		{
			name:       "missing active dialog gauge",
			metricName: "sip_exporter_active_dialogs",
			wantError:  "sip_exporter_active_dialogs has no samples",
		},
		{
			name:       "second interface has drops",
			metricName: "sip_exporter_socket_packets_dropped_total",
			samples: []metricSample{
				{name: "sip_exporter_socket_packets_dropped_total", labels: map[string]string{"iface": "lo"}, value: 0},
				{name: "sip_exporter_socket_packets_dropped_total", labels: map[string]string{"iface": "eth0"}, value: 2},
			},
			wantError: "sip_exporter_socket_packets_dropped_total has non-zero samples: " +
				`sip_exporter_socket_packets_dropped_total{iface="eth0"}=2`,
		},
		{
			name:       "every series is zero",
			metricName: "sip_exporter_active_trackers",
			samples: []metricSample{
				{name: "sip_exporter_active_trackers", labels: map[string]string{"type": "invite"}, value: 0},
				{name: "sip_exporter_active_trackers", labels: map[string]string{"type": "rtp"}, value: 0},
			},
		},
		{
			name:       "diagnostic contains every non-zero series with sorted labels",
			metricName: "sip_exporter_sessions",
			samples: []metricSample{
				{
					name: "sip_exporter_sessions",
					labels: map[string]string{
						"direction": "inbound",
						"carrier":   "carrier-a",
					},
					value: 1,
				},
				{name: "sip_exporter_sessions", labels: map[string]string{"carrier": "carrier-b"}, value: 2},
			},
			wantError: "sip_exporter_sessions has non-zero samples: " +
				`sip_exporter_sessions{carrier="carrier-a",direction="inbound"}=1, ` +
				`sip_exporter_sessions{carrier="carrier-b"}=2`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := zeroMetricSamplesError(tt.metricName, tt.samples)
			if tt.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tt.wantError)
		})
	}
}
