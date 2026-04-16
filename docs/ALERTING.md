# SIP Exporter Alerting Guide

This guide provides pre-configured alerting examples for monitoring SIP infrastructure with the SIP Exporter.

## Overview

The SIP Exporter exposes metrics based on RFC 6076 (SIP Performance Metrics). Key metrics to alert on:

| Metric | Description | Alert When |
|--------|-------------|------------|
| `sip_exporter_ser` | Session Establishment Ratio | < 50% (warning), < 20% (critical) |
| `sip_exporter_isa` | Ineffective Sessions Attempts | High rate indicates DDoS or server issues |
| `sip_exporter_rrd` | Registration Request Delay | > 500ms indicates network/registrar issues |
| `sip_exporter_401_total` | Authentication failures | High rate indicates brute-force attacks |
| `sip_exporter_403_total` | Forbidden responses | High rate indicates authorization issues |

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
        expr: rate(sip_exporter_isa[1m]) > 50
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Possible DDoS attack detected"
          description: "ISA rate is {{ $value | printf \"%.2f\" }}/s. High rate of ineffective sessions (408/500/503/504) indicates server overload or DDoS attack."
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
        expr: sip_exporter_rrd > 500
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Slow SIP registration times"
          description: "Registration Request Delay is {{ $value | printf \"%.0f\" }}ms. Network or registrar performance issues."

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
          sip_exporter_sessions / sip_exporter_invite_total * 100 > 10
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "High ratio of active SIP dialogs"
          description: "{{ $value | printf \"%.1f\" }}% of INVITEs have active dialogs. Possible Session-Expires timeout issues or missing BYE messages. Check downstream servers."
```

## Grafana Dashboard

### Import Instructions

1. Open Grafana → Dashboards → Import
2. Paste the JSON below or upload as file
3. Select Prometheus datasource
4. Click "Import"

### Dashboard JSON

```json
{
  "annotations": {
    "list": []
  },
  "editable": true,
  "fiscalYearStartMonth": 0,
  "graphTooltip": 0,
  "id": null,
  "links": [],
  "liveNow": false,
  "panels": [
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [],
          "max": 100,
          "min": 0,
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "red",
                "value": null
              },
              {
                "color": "yellow",
                "value": 20
              },
              {
                "color": "green",
                "value": 50
              }
            ]
          },
          "unit": "percent"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 6,
        "w": 6,
        "x": 0,
        "y": 0
      },
      "id": 1,
      "options": {
        "orientation": "auto",
        "reduceOptions": {
          "values": false,
          "calcs": ["lastNotNull"],
          "fields": ""
        },
        "showThresholdLabels": false,
        "showThresholdMarkers": true
      },
      "title": "SER (Session Establishment Ratio)",
      "type": "gauge"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [],
          "max": 100,
          "min": 0,
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green",
                "value": null
              },
              {
                "color": "yellow",
                "value": 30
              },
              {
                "color": "red",
                "value": 50
              }
            ]
          },
          "unit": "percent"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 6,
        "w": 6,
        "x": 6,
        "y": 0
      },
      "id": 2,
      "options": {
        "orientation": "auto",
        "reduceOptions": {
          "values": false,
          "calcs": ["lastNotNull"],
          "fields": ""
        },
        "showThresholdLabels": false,
        "showThresholdMarkers": true
      },
      "title": "SEER (Effective Sessions)",
      "type": "gauge"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [],
          "max": 50,
          "min": 0,
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green",
                "value": null
              },
              {
                "color": "yellow",
                "value": 10
              },
              {
                "color": "red",
                "value": 25
              }
            ]
          },
          "unit": "percent"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 6,
        "w": 6,
        "x": 12,
        "y": 0
      },
      "id": 3,
      "options": {
        "orientation": "auto",
        "reduceOptions": {
          "values": false,
          "calcs": ["lastNotNull"],
          "fields": ""
        },
        "showThresholdLabels": false,
        "showThresholdMarkers": true
      },
      "title": "ISA (Ineffective Sessions)",
      "type": "gauge"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [],
          "max": 100,
          "min": 0,
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "red",
                "value": null
              },
              {
                "color": "yellow",
                "value": 30
              },
              {
                "color": "green",
                "value": 50
              }
            ]
          },
          "unit": "percent"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 6,
        "w": 6,
        "x": 18,
        "y": 0
      },
      "id": 4,
      "options": {
        "orientation": "auto",
        "reduceOptions": {
          "values": false,
          "calcs": ["lastNotNull"],
          "fields": ""
        },
        "showThresholdLabels": false,
        "showThresholdMarkers": true
      },
      "title": "SCR (Session Completion Ratio)",
      "type": "gauge"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green",
                "value": null
              },
              {
                "color": "yellow",
                "value": 100
              },
              {
                "color": "red",
                "value": 500
              }
            ]
          },
          "unit": "ms"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 4,
        "w": 4,
        "x": 0,
        "y": 6
      },
      "id": 5,
      "options": {
        "colorMode": "value",
        "graphMode": "area",
        "justifyMode": "auto",
        "orientation": "auto",
        "reduceOptions": {
          "values": false,
          "calcs": ["lastNotNull"],
          "fields": ""
        },
        "textMode": "auto"
      },
      "title": "RRD (Registration Delay)",
      "type": "stat"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "thresholds"
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green",
                "value": null
              },
              {
                "color": "yellow",
                "value": 100
              },
              {
                "color": "red",
                "value": 500
              }
            ]
          },
          "unit": "short"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 4,
        "w": 4,
        "x": 4,
        "y": 6
      },
      "id": 6,
      "options": {
        "colorMode": "value",
        "graphMode": "area",
        "justifyMode": "auto",
        "orientation": "auto",
        "reduceOptions": {
          "values": false,
          "calcs": ["lastNotNull"],
          "fields": ""
        },
        "textMode": "auto"
      },
      "title": "Active Sessions",
      "type": "stat"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "palette-classic"
          },
          "custom": {
            "hideFrom": {
              "tooltip": false,
              "viz": false,
              "legend": false
            }
          },
          "mappings": [],
          "unit": "short"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 8,
        "w": 16,
        "x": 8,
        "y": 6
      },
      "id": 7,
      "options": {
        "legend": {
          "displayMode": "table",
          "placement": "right",
          "values": ["value"]
        },
        "pieType": "pie",
        "reduceOptions": {
          "values": false,
          "calcs": ["lastNotNull"],
          "fields": ""
        },
        "tooltip": {
          "mode": "single"
        }
      },
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "expr": "sip_exporter_100_total",
          "legendFormat": "100 Trying"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "expr": "sip_exporter_180_total",
          "legendFormat": "180 Ringing"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "expr": "sip_exporter_200_total",
          "legendFormat": "200 OK"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "expr": "sip_exporter_401_total",
          "legendFormat": "401 Unauthorized"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "expr": "sip_exporter_403_total",
          "legendFormat": "403 Forbidden"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "expr": "sip_exporter_404_total",
          "legendFormat": "404 Not Found"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "expr": "sip_exporter_500_total",
          "legendFormat": "500 Server Error"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "expr": "sip_exporter_503_total",
          "legendFormat": "503 Service Unavailable"
        }
      ],
      "title": "Response Code Distribution",
      "type": "piechart"
    },
    {
      "datasource": {
        "type": "prometheus",
        "uid": "${datasource}"
      },
      "fieldConfig": {
        "defaults": {
          "color": {
            "mode": "palette-classic"
          },
          "custom": {
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "drawStyle": "line",
            "fillOpacity": 10,
            "gradientMode": "none",
            "hideFrom": {
              "tooltip": false,
              "viz": false,
              "legend": false
            },
            "lineInterpolation": "linear",
            "lineWidth": 1,
            "pointSize": 5,
            "scaleDistribution": {
              "type": "linear"
            },
            "showPoints": "never",
            "spanNulls": true
          },
          "mappings": [],
          "unit": "pps"
        },
        "overrides": []
      },
      "gridPos": {
        "h": 8,
        "w": 24,
        "x": 0,
        "y": 14
      },
      "id": 8,
      "options": {
        "legend": {
          "calcs": ["mean", "max"],
          "displayMode": "table",
          "placement": "bottom"
        },
        "tooltip": {
          "mode": "multi"
        }
      },
      "targets": [
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "expr": "rate(sip_exporter_packets_total[5m])",
          "legendFormat": "Total packets"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "expr": "rate(sip_exporter_invite_total[5m])",
          "legendFormat": "INVITE"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "expr": "rate(sip_exporter_register_total[5m])",
          "legendFormat": "REGISTER"
        },
        {
          "datasource": {
            "type": "prometheus",
            "uid": "${datasource}"
          },
          "expr": "rate(sip_exporter_bye_total[5m])",
          "legendFormat": "BYE"
        }
      ],
      "title": "SIP Traffic Rate",
      "type": "timeseries"
    }
  ],
  "refresh": "10s",
  "schemaVersion": 38,
  "style": "dark",
  "tags": ["sip", "voip", "monitoring"],
  "templating": {
    "list": [
      {
        "current": {
          "selected": false,
          "text": "Prometheus",
          "value": "Prometheus"
        },
        "hide": 0,
        "includeAll": false,
        "multi": false,
        "name": "datasource",
        "options": [],
        "query": "prometheus",
        "refresh": 1,
        "regex": "",
        "skipUrlSync": false,
        "type": "datasource"
      }
    ]
  },
  "time": {
    "from": "now-1h",
    "to": "now"
  },
  "timepicker": {},
  "timezone": "",
  "title": "SIP Exporter Dashboard",
  "uid": "sip-exporter",
  "version": 1,
  "weekStart": ""
}
```

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
        - targets: ['localhost:2119']
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
          - 'sip-exporter-1:2119'
          - 'sip-exporter-2:2119'
```

### Metric Cardinality

The SIP Exporter exposes ~50 metrics with low cardinality (no labels). This is suitable for long-term storage and efficient querying.

### Dashboard Organization

Organize dashboards by:
- **Overview**: High-level health (SER, SEER, ISA, SCR)
- **Traffic**: Request/response rates
- **Errors**: Error code breakdown
- **Performance**: RRD, latency metrics
