# Fraud Detection

> **Version:** sip-exporter v1.3.3+
>
> sip-exporter provides **signal-only** fraud detection. It does not block or
> intercept traffic. It exports Prometheus counter/gauge metrics that increment
> when suspicious patterns are detected. You configure alerts in AlertManager
> and enforce blocks externally (fail2ban, SBC rules, firewall).

## What It Detects

sip-exporter covers the top VoIP fraud categories — compromised PBX and identity
theft — with four detection signals:

| Signal | Metric | Type | What it detects |
|--------|--------|------|-----------------|
| Registration Scan | `register_scan_total` | counter | Account enumeration / compromised PBX |
| Registration Country Change | `register_country_change_total` | counter | Account takeover from new geography |
| INVITE Burst | `invite_burst_total` | counter | Toll-fraud onset / SIP flood DDoS |
| Sessions Utilization | `sessions_utilization` | gauge | Capacity exhaustion / contract breach |

All counters use `{carrier,source_country}` labels. The source IP is used
internally for threshold tracking but is **never exposed** as a Prometheus label.

---

## Metrics & Configuration

### Registration Scan

`sip_exporter_register_scan_total{carrier,source_country}` — counter

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

`sip_exporter_register_country_change_total{carrier,source_country}` — counter

Detects the same AOR re-registering from a different country — account takeover
signal. No configuration needed (uses existing GeoIP/carrier country config).

**Example:** `alice@example.com` registers from RU, then GE → counter increments.
Same AOR from GE again → no signal.

### INVITE Burst

`sip_exporter_invite_burst_total{carrier,source_country}` — counter

Detects a single IP sending initial INVITEs at abnormally high rate — toll-fraud
or SIP flood. Re-INVITEs within an existing dialog are excluded (counted
separately, don't trigger the detector).

| Env var | Default | Description |
|---------|---------|-------------|
| `SIP_EXPORTER_FRAUD_INVITE_BURST_THRESHOLD` | `100` | Initial INVITEs from one IP to trigger |
| `SIP_EXPORTER_FRAUD_INVITE_BURST_WINDOW` | `60s` | Sliding window duration |

**Example:** PBX at 198.51.100.10 makes 150 calls/min, threshold=100: INVITEs
1–99 → no signal; 100th → counter +1; 101–150 → +1 each.

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
  expr: sip_exporter_register_country_change_total > 0 unless on (carrier, source_country) (sip_exporter_register_country_change_total offset 5m > 0)
  for: 0m
  labels:
    severity: warning
  annotations:
    summary: "Registration country change detected"
    description: "A user re-registered from a different country on {{ $labels.carrier }}."

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

**Sessions utilization:**
- Capped at 100% — `active=300, limit=100` shows 100%. Monitor `sip_exporter_sessions` (raw gauge) for extreme oversubscription.
- `limit: 0` means "no limit" — carrier excluded from these metrics entirely.
