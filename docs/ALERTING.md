# SIP Exporter Alerting Guide

This guide provides pre-configured alerting examples for monitoring SIP infrastructure with the SIP Exporter.

## Overview

The SIP Exporter exposes metrics based on RFC 6076 (SIP Performance Metrics) and RFC 6035 (Voice Quality Reporting). Key metrics to alert on:

| Metric | Description | Alert When |
|--------|-------------|------------|
| `sip_exporter_ser` | Session Establishment Ratio | < 50% (warning), < 20% (critical) |
| `sip_exporter_spd` | Session Process Duration (seconds) | p95 < 5s or p95 > 3600s |
| `sip_exporter_isa` | Ineffective Sessions Attempts | High rate indicates DDoS or server issues |
| `sip_exporter_rrd` | Registration Request Delay | p95 > 500ms indicates network/registrar issues |
| `sip_exporter_401_total` | Authentication failures | High rate indicates brute-force attacks |
| `sip_exporter_403_total` | Forbidden responses | High rate indicates authorization issues |
| `sip_exporter_vq_mos_lq` | MOS Listening Quality (RFC 6035) | avg < 3.5 (warning), < 3.0 (critical) |
| `sip_exporter_vq_nlr_percent` | Network Packet Loss Rate (RFC 6035) | avg > 2% (warning), > 5% (critical) |
| `sip_exporter_rtp_mos_score` | RTP MOS (E-model G.107) | avg < 3.5 (warning), < 3.0 (critical) |
| `sip_exporter_rtp_packets_lost_total` | RTP packet loss rate | > 5% (warning), > 10% (critical) |
| `sip_exporter_rtp_jitter_milliseconds` | RTP interarrival jitter (RFC 3550) | p95 > 50ms (warning) |
| `sip_exporter_register_country_change_total` | Registration country change | Any spike indicates compromised account |
| `sip_exporter_register_scan_total` | Registration scan detection | Any signal indicates enumeration attack |
| `sip_exporter_invite_burst_total` | INVITE burst detection | Any signal indicates toll fraud or DDoS |
| `sip_exporter_sessions_utilization` | Session capacity utilization | > 90% indicates capacity exhaustion |

## Quick Start

1. Copy Prometheus alert rules to your Prometheus configuration
2. Import Grafana dashboard JSON
3. Configure Alertmanager receiver (Slack/PagerDuty/Email)
4. Adjust thresholds based on your traffic patterns

## Prometheus Alert Rules

### Critical Alerts

```yaml
groups:
  - name: sip_exporter_critical
    interval: 30s
    rules:
      - alert: SIPExporterDown
        expr: up{job="sip-exporter"} == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "SIP Exporter is down"
          description: "SIP Exporter instance {{ $labels.instance }} has been down for more than 1 minute."

      - alert: SIPDDoSDetected
        expr: sip_exporter_isa > 50
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Possible DDoS attack detected"
          description: "ISA is {{ $value | printf \"%.2f\" }}%. High share of ineffective sessions (408/500/503/504) indicates server overload or DDoS attack."
          runbook_url: "https://wiki.example.com/runbooks/sip-ddos"

      - alert: SIPSessionEstablishmentCritical
        expr: sip_exporter_ser < 20
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "SER critically low"
          description: "Session Establishment Ratio is {{ $value | printf \"%.1f\" }}%. Most calls are failing to establish."
          runbook_url: "https://wiki.example.com/runbooks/sip-ser-low"
```

### Warning Alerts

```yaml
      - alert: SIPAuthFailuresHigh
        expr: rate(sip_exporter_401_total[5m]) > 10
        for: 3m
        labels:
          severity: warning
        annotations:
          summary: "High authentication failure rate"
          description: "401 Unauthorized rate is {{ $value | printf \"%.2f\" }}/s. Possible brute-force attack or misconfigured clients."

      - alert: SIPRegistrationSlow
        expr: histogram_quantile(0.95, sum(rate(sip_exporter_rrd_bucket[5m])) by (le)) > 500
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Slow SIP registration times"
          description: "95th percentile registration delay is {{ $value | printf \"%.0f\" }}ms. Network or registrar performance issues."

      - alert: SIPSessionEstablishmentLow
        expr: sip_exporter_ser < 50 and sip_exporter_ser >= 20
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "SER below threshold"
          description: "Session Establishment Ratio is {{ $value | printf \"%.1f\" }}%. Call quality may be degraded."

      - alert: SIPForbiddenHigh
        expr: rate(sip_exporter_403_total[5m]) > 5
        for: 3m
        labels:
          severity: warning
        annotations:
          summary: "High forbidden response rate"
          description: "403 Forbidden rate is {{ $value | printf \"%.2f\" }}/s. Check user permissions and ACL configurations."

      - alert: SIPServerErrorHigh
        expr: rate(sip_exporter_500_total[5m]) > 5
        for: 3m
        labels:
          severity: warning
        annotations:
          summary: "High server error rate"
          description: "500 Server Error rate is {{ $value | printf \"%.2f\" }}/s. SIP server may be overloaded or misconfigured."
      ```

### Registration Health Alerts

```yaml
      - alert: SIPRegistrationSuccessLow
        expr: sip_exporter_register_success_ratio < 80
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Low registration success ratio"
          description: "REGISTER success ratio is {{ $value | printf \"%.1f\" }}% (below 80%). Clients are failing to register — check registrar health, credentials, or ACLs. Note: 401/407 digest-auth challenges are excluded from the denominator."

      - alert: SIPRegistrationBruteForce
        expr: sum by (carrier, ua_type, source_country) (rate(sip_exporter_register_failure_total{code="401"}[5m])) > 10
        for: 3m
        labels:
          severity: warning
        annotations:
          summary: "Possible SIP registration brute-force"
          description: "REGISTER 401 Unauthorized rate is {{ $value | printf \"%.2f\" }}/s. A flood of failed authentications against the registrar — consider fail2ban or rate-limiting. (REGISTER-specific; distinct from the generic 401 alert.)"

      - alert: SIPRegistrationDrop
        expr: sip_exporter_active_registrations < 5
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Active registrations critically low"
          description: "Only {{ $value | printf \"%.0f\" }} active registrations. Mass deregistration may indicate registrar outage or network partition."
```

> **401 vs `register_failure_total{code="401"}`:** The generic `sip_exporter_401_total` counts 401s across **all** methods (INVITE auth challenges + REGISTER challenges). `sip_exporter_register_failure_total{code="401"}` is **REGISTER-only**, giving a cleaner brute-force signal. The success-ratio alert uses `register_success_ratio`, which excludes 401/407 from its denominator.

### Fraud Detection Alerts

These alerts detect suspicious SIP patterns: account takeover, registration enumeration, and INVITE flooding. See `docs/fraud-detection.md` for full configuration details.

```yaml
      - alert: SIPRegistrationCountryChange
        expr: rate(sip_exporter_register_country_change_total[5m]) > 0
        for: 1m
        labels:
          severity: warning
        annotations:
          summary: "Registration country change detected"
          description: "A user re-registered from a different country on {{ $labels.carrier }}. Possible account takeover."

      - alert: SIPRegistrationScan
        expr: rate(sip_exporter_register_scan_total[5m]) > 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Registration scan attack detected"
          description: "Single IP is registering many different accounts on {{ $labels.carrier }} from {{ $labels.source_country }}. Possible credential stuffing or enumeration."

      - alert: SIPInviteBurst
        expr: rate(sip_exporter_invite_burst_total[5m]) > 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "INVITE burst detected"
          description: "Single IP is sending an unusually high rate of INVITEs on {{ $labels.carrier }} from {{ $labels.source_country }}. Possible toll fraud, DDoS, or traffic pump."

      - alert: SIPSessionCapacityExhaustion
        expr: sip_exporter_sessions_utilization > 90
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Session capacity near exhaustion"
          description: "Carrier {{ $labels.carrier }} is at {{ $value | printf \"%.0f\" }}% of its configured session limit. Plan capacity expansion or investigate traffic surge."
```

### Voice Quality Alerts (RFC 6035)

These alerts monitor voice quality metrics extracted from SIP PUBLISH/NOTIFY with `Content-Type: application/vq-rtcpxr` (RFC 6035).

```yaml
      - alert: SIPVoiceQualityMOSLow
        expr: |
          rate(sip_exporter_vq_mos_lq_sum[5m]) / rate(sip_exporter_vq_mos_lq_count[5m]) < 3.5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Low voice quality (MOS)"
          description: "Average MOS Listening Quality is {{ $value | printf \"%.1f\" }}. Voice quality is degraded — check network, codec, or endpoint issues."

      - alert: SIPVoiceQualityMOSCritical
        expr: |
          rate(sip_exporter_vq_mos_lq_sum[5m]) / rate(sip_exporter_vq_mos_lq_count[5m]) < 3.0
        for: 3m
        labels:
          severity: critical
        annotations:
          summary: "Critical voice quality (MOS below 3.0)"
          description: "Average MOS Listening Quality is {{ $value | printf \"%.1f\" }}. Users are experiencing poor call quality. Investigate network packet loss, jitter, or codec mismatch immediately."

      - alert: SIPVoiceQualityPacketLossHigh
        expr: |
          rate(sip_exporter_vq_nlr_percent_sum[5m]) / rate(sip_exporter_vq_nlr_percent_count[5m]) > 2
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High network packet loss"
          description: "Average network packet loss rate is {{ $value | printf \"%.1f\" }}%. Packet loss above 2% degrades voice quality. Check network congestion and QoS configuration."

      - alert: SIPVoiceQualityPacketLossCritical
        expr: |
          rate(sip_exporter_vq_nlr_percent_sum[5m]) / rate(sip_exporter_vq_nlr_percent_count[5m]) > 5
        for: 3m
        labels:
          severity: critical
        annotations:
          summary: "Critical network packet loss"
          description: "Average network packet loss rate is {{ $value | printf \"%.1f\" }}%. Severe packet loss is causing unacceptable voice quality. Immediate network investigation required."

      - alert: SIPVoiceQualityJitterHigh
        expr: |
          histogram_quantile(0.95, sum(rate(sip_exporter_vq_iaj_ms_bucket[5m])) by (le)) > 50
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High interarrival jitter"
          description: "95th percentile interarrival jitter is {{ $value | printf \"%.1f\" }}ms. High jitter causes audio artifacts. Check network queueing and jitter buffer configuration."
      ```

### RTP Media Alerts

These alerts monitor real-time RTP stream quality (jitter, packet loss, MOS) measured passively from RTP headers (RFC 3550). Metrics are labeled by `carrier`, `ua_type`, `codec`, and `source_country`.

```yaml
      - alert: RTPPacketLossHigh
        expr: |
          sum by (carrier) (rate(sip_exporter_rtp_packets_lost_total[5m]))
          / sum by (carrier) (rate(sip_exporter_rtp_packets_total[5m])) * 100 > 5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "RTP packet loss above 5%"
          description: "RTP packet loss for carrier {{ $labels.carrier }} is {{ $value | printf \"%.1f\" }}%. Network congestion or QoS misconfiguration may be degrading voice quality."

      - alert: RTPPacketLossCritical
        expr: |
          sum by (carrier) (rate(sip_exporter_rtp_packets_lost_total[5m]))
          / sum by (carrier) (rate(sip_exporter_rtp_packets_total[5m])) * 100 > 10
        for: 3m
        labels:
          severity: critical
        annotations:
          summary: "Critical RTP packet loss above 10%"
          description: "RTP packet loss for carrier {{ $labels.carrier }} is {{ $value | printf \"%.1f\" }}%. Severe media degradation — users experience choppy audio or dropped calls."

      - alert: RTPMOSLow
        expr: |
          sum by (carrier) (rate(sip_exporter_rtp_mos_score_sum[5m]))
          / sum by (carrier) (rate(sip_exporter_rtp_mos_score_count[5m])) < 3.5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Low RTP MOS score"
          description: "Average RTP MOS for carrier {{ $labels.carrier }} is {{ $value | printf \"%.1f\" }}. E-model quality estimate below 3.5 indicates noticeable degradation."

      - alert: RTPJitterHigh
        expr: |
          histogram_quantile(0.95, sum by (le, carrier) (rate(sip_exporter_rtp_jitter_milliseconds_bucket[5m]))) > 50
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High RTP jitter"
          description: "95th percentile RTP jitter for carrier {{ $labels.carrier }} is {{ $value | printf \"%.1f\" }}ms. Jitter above 50ms causes audio artifacts and jitter buffer overflows."
      ```

### Info Alerts

```yaml
      - alert: SIPRedirectIncrease
        expr: rate(sip_exporter_302_total[10m]) > 2 and increase(sip_exporter_302_total[1h]) > 100
        for: 10m
        labels:
          severity: info
        annotations:
          summary: "Increased redirect responses"
          description: "302 Moved Temporarily rate increased. Users may be migrating to new endpoints."

      - alert: SIPSessionCompletionLow
        expr: sip_exporter_scr < 30 and sip_exporter_scr > 0
        for: 10m
        labels:
          severity: info
        annotations:
          summary: "Low session completion ratio"
          description: "SCR is {{ $value | printf \"%.1f\" }}%. Many sessions are not completing normally (INVITE without BYE)."

      - alert: SIPStaleDialogsHigh
        expr: |
          sip_exporter_sessions
            / on (carrier, ua_type, source_country)
              sum by (carrier, ua_type, source_country) (sip_exporter_invite_total)
            * 100 > 10
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "High ratio of active SIP dialogs"
          description: "{{ $value | printf \"%.1f\" }}% of INVITEs have active dialogs. Possible Session-Expires timeout issues or missing BYE messages. Check downstream servers."

      - alert: SIPSessionDurationTooShort
        expr: histogram_quantile(0.95, sum(rate(sip_exporter_spd_bucket[5m])) by (le)) > 0 and histogram_quantile(0.95, sum(rate(sip_exporter_spd_bucket[5m])) by (le)) < 5
        for: 10m
        labels:
          severity: info
        annotations:
          summary: "Very short call duration"
          description: "95th percentile session duration is {{ $value | printf \"%.1f\" }}s. Calls are terminating very quickly, possible media issues or misconfigured dial plans."
```

## Grafana Dashboard

### Import Instructions

1. Open Grafana → Dashboards → Import
2. Upload [`examples/grafana-dashboard.json`](../examples/grafana-dashboard.json) or paste its contents
3. Select your Prometheus or VictoriaMetrics datasource
4. Click "Import"

The dashboard includes `$carrier`, `$source_country`, and `$destination_country` variables for per-operator and geographic filtering, with panels for:
- **Overview** — Active sessions, packet rate, INVITE rate, completed sessions
- **RFC 6076 Ratios** — SER, SEER, ISA, SCR, ASR, NER gauges + trend graph
- **Latency Histograms** — RRD, TTR, SPD, ORD, LRD (p50/p95/p99 per carrier)
- **Voice Quality (RFC 6035)** — MOS Listening/Conversational Quality, packet loss (NLR), jitter (IAJ), round trip delay (RTD), R-factor (RLQ/RCQ)
- **RTP Media Analysis** — Active RTP streams, packet rate, packet loss rate, MOS (E-model), jitter p95 — all by codec
- **SIP Traffic** — Request/response rate breakdown by method and status code
- **System Health** — Error rate, ISS rate
- **Geographic Distribution** — Top source/destination countries by INVITE rate (GeoIP-enriched)

Dashboard file: [`examples/grafana-dashboard.json`](../examples/grafana-dashboard.json)

## Alertmanager Examples

### Slack Integration

```yaml
global:
  resolve_timeout: 5m

route:
  group_by: ['alertname', 'severity']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 4h
  receiver: 'sip-alerts'
  routes:
    - match:
        severity: critical
      receiver: 'sip-critical'

receivers:
  - name: 'sip-alerts'
    slack_configs:
      - channel: '#sip-monitoring'
        send_resolved: true
        title: '{{ .GroupLabels.alertname }}'
        text: |-
          {{ range .Alerts }}
          *Alert:* {{ .Annotations.summary }}
          *Description:* {{ .Annotations.description }}
          *Severity:* {{ .Labels.severity }}
          {{ end }}
        color: '{{ if eq .Status "firing" }}{{ if eq .CommonLabels.severity "critical" }}danger{{ else }}warning{{ end }}{{ else }}good{{ end }}'

  - name: 'sip-critical'
    slack_configs:
      - channel: '#sip-critical'
        send_resolved: true
        title: '🚨 {{ .GroupLabels.alertname }}'
        text: '{{ range .Alerts }}{{ .Annotations.description }}{{ end }}'
        color: 'danger'
```

### PagerDuty Integration

```yaml
receivers:
  - name: 'sip-pagerduty'
    pagerduty_configs:
      - service_key: '<your-pagerduty-integration-key>'
        severity: '{{ .CommonLabels.severity }}'
        description: '{{ .GroupLabels.alertname }}: {{ range .Alerts }}{{ .Annotations.summary }}{{ end }}'
        details:
          firing: '{{ template "pagerduty.default.instances" .Alerts.Firing }}'
          resolved: '{{ template "pagerduty.default.instances" .Alerts.Resolved }}'
          num_firing: '{{ .Alerts.Firing | len }}'
          num_resolved: '{{ .Alerts.Resolved | len }}'
```

### Email Integration

```yaml
global:
  smtp_smarthost: 'smtp.example.com:587'
  smtp_from: 'alertmanager@example.com'
  smtp_auth_username: 'alertmanager@example.com'
  smtp_auth_password: '<password>'

receivers:
  - name: 'sip-email'
    email_configs:
      - to: 'sip-team@example.com'
        send_resolved: true
        headers:
          Subject: '[SIP Alert] {{ .GroupLabels.alertname }}'
        html: |
          <h2>{{ .GroupLabels.alertname }}</h2>
          {{ range .Alerts }}
          <p><strong>Summary:</strong> {{ .Annotations.summary }}</p>
          <p><strong>Description:</strong> {{ .Annotations.description }}</p>
          <p><strong>Severity:</strong> {{ .Labels.severity }}</p>
          <hr>
          {{ end }}
```

## Best Practices

### Scrape Interval

- **Recommended**: 10-15 seconds for production
- **Minimum**: 5 seconds (may increase load)
- **Configuration**:
  ```yaml
  scrape_configs:
    - job_name: 'sip-exporter'
      scrape_interval: 10s
      static_configs:
        - targets: ['localhost:2112']
  ```

### Data Retention

- **Local Prometheus**: 15-30 days typical
- **Long-term storage**: Use Thanos, Cortex, or VictoriaMetrics
- **Configuration**:
  ```bash
  prometheus --storage.tsdb.retention.time=15d
  ```

### Alert Silences

Use alert silences during maintenance windows:

```bash
amtool silence add alertname=SIPDDoSDetected duration=2h comment="Planned maintenance"
```

### Threshold Tuning

1. **Baseline first**: Monitor metrics for 1-2 weeks before setting thresholds
2. **Traffic patterns**: Account for peak hours vs. off-hours
3. **Gradual tuning**: Start with wider thresholds, narrow over time
4. **Documentation**: Document why each threshold was chosen

### Runbook Integration

Link runbooks to alerts using `runbook_url` annotation:

```yaml
annotations:
  runbook_url: "https://wiki.example.com/runbooks/{{ .GroupLabels.alertname }}"
```

### Multiple Instances

For high availability, run multiple exporter instances:

```yaml
scrape_configs:
  - job_name: 'sip-exporter'
    static_configs:
      - targets:
          - 'sip-exporter-1:2112'
          - 'sip-exporter-2:2112'
```

### Metric Cardinality

The SIP Exporter exposes ~65 metrics with `carrier`, `ua_type`, and `source_country` labels. RTP metrics additionally carry a `codec` label (typically 3-8 codecs). Cardinality equals the number of configured carriers × UA types × source countries × (for RTP) active codecs. Without a carriers config and without GeoIP, `source_country="unknown"` (cardinality = 1); enabling GeoIP or setting `carrier.country` increases cardinality by the number of distinct source countries observed.

### Dashboard Organization

Organize dashboards by:
- **Overview**: High-level health (SER, SEER, ISA, SCR)
- **Traffic**: Request/response rates
- **Errors**: Error code breakdown
- **Performance**: RRD, latency metrics
- **Voice Quality**: MOS, packet loss, jitter, R-factor (RFC 6035)
- **RTP Media**: Active streams, packet rate, loss rate, MOS, jitter by codec
