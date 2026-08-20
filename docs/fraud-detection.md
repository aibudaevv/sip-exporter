# Fraud Detection

> **Version:** sip-exporter v1.9.0
>
> sip-exporter provides **signal-only** fraud detection. It does not block or
> intercept traffic. It exports Prometheus counter/gauge metrics that increment
> when suspicious patterns are detected. You configure alerts in AlertManager
> and enforce blocks externally (fail2ban, SBC rules, firewall).

## What It Detects

sip-exporter covers the top VoIP fraud categories — compromised PBX and identity
theft — with five detection signals:

| Signal | Metric | Type | What it detects |
|--------|--------|------|-----------------|
| Registration Scan | `register_scan_total` | counter | Account enumeration / compromised PBX |
| Registration Country Change | `register_country_change_total` | counter | Account takeover from new geography |
| INVITE Burst | `invite_burst_total` | counter | Toll-fraud onset / SIP flood DDoS |
| False Answer Supervision | `fas_calls_total` | counter | Answered, media-bearing call with no answer-side RTP |
| Sessions Utilization | `sessions_utilization` | gauge | Capacity exhaustion / contract breach |

The three signaling counters use `{carrier,source_country,direction}` labels. FAS is a
call-level signal and additionally carries `ua_type`. The source IP is used internally
for threshold tracking but is **never exposed** as a Prometheus label.

---

## Metrics & Configuration

### Registration Scan

`sip_exporter_register_scan_total{carrier,source_country,direction}` — counter

Detects a single source IP registering many unique SIP accounts (AORs) within a
sliding window. Catches compromised PBX enrolling extensions, account farms, or
credential stuffing with successful registrations.

| Env var | Default | Description |
|---------|---------|-------------|
| `SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD` | `10` | Unique AORs from one IP to trigger |
| `SIP_EXPORTER_FRAUD_REGISTER_SCAN_WINDOW` | `60s` | Sliding window duration |

**Example:** PBX at 203.0.113.5 registers 15 accounts in 30s, threshold=10:
registrations 1–9 → no signal; 10th unique AOR → counter +1; 11–15 → +1 each.

### Registration Country Change

`sip_exporter_register_country_change_total{carrier,source_country,direction}` — counter

Detects the same AOR re-registering from a different country — account takeover
signal. No configuration needed (uses existing GeoIP/carrier country config).

**Example:** `alice@example.com` registers from RU, then GE → counter increments.
Same AOR from GE again → no signal.

### INVITE Burst

`sip_exporter_invite_burst_total{carrier,source_country,direction}` — counter

Detects a single IP sending initial INVITEs at abnormally high rate — toll-fraud
or SIP flood. Re-INVITEs within an existing dialog are excluded (counted
separately, don't trigger the detector).

| Env var | Default | Description |
|---------|---------|-------------|
| `SIP_EXPORTER_FRAUD_INVITE_BURST_THRESHOLD` | `100` | Initial INVITEs from one IP to trigger |
| `SIP_EXPORTER_FRAUD_INVITE_BURST_WINDOW` | `60s` | Sliding window duration |

**Example:** PBX at 198.51.100.10 makes 150 calls/min, threshold=100: INVITEs
1–99 → no signal; 100th → counter +1; 101–150 → +1 each.

### False Answer Supervision

`sip_exporter_fas_calls_total{carrier,ua_type,source_country,direction}` — counter

Detects an answered, media-bearing call that carries no answer-side RTP. The signal
has two paths: a periodic sweep after the configured threshold (plus a 15s grace for
DTLS-SRTP), or BYE teardown after an independent 3s floor. It is distinct from
`sessions_missing_rtp_total`, which is evaluated when the dialog ends.

| Env var | Default | Description |
|---------|---------|-------------|
| `SIP_EXPORTER_FRAUD_FAS_THRESHOLD` | `10s` | Base wait for the periodic sweep path; does not change the 3s BYE floor |

FAS depends on complete RTP capture. Check `sip_exporter_rtp_dropped_total` before
acting on the signal. See [Metrics — FAS limitations](METRICS.md#fas-limitations) for
side detection, short calls, NAT and expected one-way-media false positives.

### Sessions Utilization

- `sip_exporter_sessions_utilization{carrier}` — gauge (% of limit)
- `sip_exporter_sessions_limit{carrier}` — gauge (configured limit)

Shows how close each carrier is to its concurrent session limit. Useful for
capacity planning — a sudden spike may indicate fraud or a misconfigured dialer.
Utilization is capped at 100%.

| Env var | Description |
|---------|-------------|
| `SIP_EXPORTER_SESSIONS_LIMITS` | Path to sessions limits YAML file |

```yaml
sessions_limits:
  - carrier: "beeline"
    limit: 500
  - carrier: "mts"
    limit: 200
  - carrier: "other"
    limit: 1000
```

---

## Alerts

> **Note on `rate()`:** `rate(counter[5m]) > 0` stays true for ~5 minutes after
> a signal. The `for: 1m` clause reduces noise from transient spikes.
> The country-change expression combines `increase()` for repeat events with an exact-label
> new-series branch, so the first event and later increments are both detected per direction.

```yaml
# Registration scan → credential stuffing investigation
- alert: SIPRegistrationScan
  expr: rate(sip_exporter_register_scan_total[5m]) > 0
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "Registration scan attack detected"
    description: "Single IP is registering many different accounts on {{ $labels.carrier }} from {{ $labels.source_country }}."

# INVITE burst → toll-fraud investigation
- alert: SIPInviteBurst
  expr: rate(sip_exporter_invite_burst_total[5m]) > 0
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "INVITE burst detected"
    description: "Single IP is sending an unusually high rate of INVITEs on {{ $labels.carrier }} from {{ $labels.source_country }}."

# Registration country change → account takeover
- alert: SIPRegistrationCountryChange
  expr: |
    increase(sip_exporter_register_country_change_total[5m]) > 0
    or
    (sip_exporter_register_country_change_total > 0
      unless sip_exporter_register_country_change_total offset 5m)
  for: 0m
  labels:
    severity: warning
  annotations:
    summary: "Registration country change detected"
    description: "A user re-registered from a different country on {{ $labels.carrier }}."

# False Answer Supervision → billing-fraud investigation
- alert: SIPFalseAnswerSupervision
  expr: |
    (rate(sip_exporter_fas_calls_total[5m]) > 0)
    unless on()
    (rate(sip_exporter_rtp_dropped_total[5m]) > 100)
  for: 2m
  labels:
    severity: warning
  annotations:
    summary: "False Answer Supervision suspected"
    description: "Answered calls on {{ $labels.carrier }} carried no answer-side RTP. Check RTP drops and expected one-way-media endpoints before acting."

# Sessions capacity exhaustion
- alert: SIPSessionCapacityExhaustion
  expr: sip_exporter_sessions_utilization > 90
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Session capacity near exhaustion"
    description: "Carrier {{ $labels.carrier }} is at {{ $value | printf \"%.0f\" }}% of its configured session limit."
```

---

## Limitations

**Register scan:**
- Only tracks *successful* (200 OK) registrations. For brute-force (401/403), use `register_failure_total{code="401"}` with `SIPRegistrationBruteForce` alert.
- SBC/proxy round-robining registrations across extensions may trigger false positives. Raise threshold.
- Rotating-source botnets may not reach per-IP threshold. Aggregate across IPs in PromQL.

**Country change:**
- Legitimate roaming triggers a signal — intentional, operator investigates.
- If GeoIP is disabled and carrier country is unset → `source_country="unknown"` for all → detection is a no-op.
- If previous registration TTL expired before re-registration from a new country → no signal (no baseline).

**INVITE burst:**
- SBC/gateway multiplexing many subscribers through one IP may exceed threshold=100. Raise threshold for that source.

**False Answer Supervision:**
- Incomplete RTP capture can produce false positives; correlate with socket and userspace drop metrics.
- Voicemail, IVR, paging and announcement endpoints that do not send answer-side RTP trigger the heuristic by design.
- See the [complete FAS limitations](METRICS.md#fas-limitations) before using this signal for enforcement.

**Sessions utilization:**
- Capped at 100% — `active=300, limit=100` shows 100%. Monitor `sip_exporter_sessions` (raw gauge) for extreme oversubscription.
- `limit: 0` means "no limit" — carrier excluded from these metrics entirely.
