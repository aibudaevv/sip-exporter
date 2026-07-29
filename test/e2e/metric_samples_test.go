//go:build e2e

package e2e

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
