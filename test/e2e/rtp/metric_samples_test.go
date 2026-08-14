//go:build e2e

package rtp

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
			name:       "prefix collision",
			body:       "sip_exporter_sessions_limit 10\n",
			metricName: "sip_exporter_sessions",
		},
		{
			name:       "malformed row",
			body:       "sip_exporter_sessions{carrier=\"carrier-a\"} invalid\n",
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

	sampleLabels := map[string]string{
		"carrier":   "carrier-a",
		"direction": "inbound",
		"ua_type":   "phone",
	}
	tests := []struct {
		name   string
		filter string
		want   bool
	}{
		{name: "filter order is irrelevant", filter: `direction="inbound",carrier="carrier-a"`, want: true},
		{name: "subset labels match", filter: `carrier="carrier-a"`, want: true},
		{name: "label name is exact", filter: `type="phone"`, want: false},
		{name: "label value is exact", filter: `carrier="carrier"`, want: false},
		{name: "missing label does not match", filter: `iface="lo"`, want: false},
		{name: "missing empty label does not match", filter: `iface=""`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			filterLabels, ok := parseMetricLabels(tt.filter)
			require.True(t, ok)
			require.Equal(t, tt.want, matchMetricLabels(sampleLabels, filterLabels))
		})
	}
}

func TestMetricSamplesReadExactLabeledValue(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(
			"sip_exporter_rtcp_reports_total{report_type=\"rr\"} 9\n" +
				"sip_exporter_rtcp_reports_total{type=\"rr\"} 3\n",
		))
	}))
	t.Cleanup(server.Close)

	require.Equal(t, 3.0,
		getMetricByLabel(t, server.URL, "sip_exporter_rtcp_reports_total", `type="rr"`))
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
