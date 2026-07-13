# Fraud Detection — How It Works

> **Version:** sip-exporter v1.3.3+
>
> sip-exporter provides **signal-only** fraud detection. It does not block or
> intercept traffic. Instead, it exports Prometheus counter metrics that
> increment when suspicious patterns are detected. You configure alerts in
> AlertManager and enforce blocks externally (fail2ban, SBC rules, firewall).

---

## 1. Overview

VoIP fraud costs the telecom industry over $15 billion annually (CFCA). The two
largest categories are:

| Category | Annual Loss | Detection Signal |
|----------|-------------|-----------------|
| Compromised PBX / voicemail | $4.96 B | Registration scan + INVITE burst |
| Subscription / identity theft | $4.32 B | Registration country change |

sip-exporter covers both categories with four detection signals:

| Signal | Metric | What it detects |
|--------|--------|-----------------|
| Registration Scan | `sip_exporter_register_scan_total` | Account enumeration / compromised PBX |
| Registration Country Change | `sip_exporter_register_country_change_total` | Account takeover from new geography |
| INVITE Burst | `sip_exporter_invite_burst_total` | Toll-fraud onset / SIP flood DDoS |
| Sessions Utilization | `sip_exporter_sessions_utilization` | Capacity exhaustion / contract breach |

**Important:** All signals use `{carrier,source_country}` labels. The source IP
address is used internally for threshold tracking but is **never exposed** as a
Prometheus label (cardinality safety).

---

## 2. Detection Signals

### 2.1 Registration Scan (Account Enumeration)

**Metric:** `sip_exporter_register_scan_total{carrier,source_country}` (counter)

**What it detects:** A single source IP successfully registers many distinct SIP
accounts (AORs) within a short time window — a strong indicator of a compromised
PBX enrolling extensions, an account farm, or an attacker using stolen credentials
for mass account takeover.

**How it works:**

1. Every successful REGISTER (200 OK) triggers a lookup of the original request's
   source IP (stored from the REGISTER request, not the response — the response
   comes from the server, not the client).
2. The AOR (`user@host`) is recorded for that source IP in a sliding window.
   Duplicate AORs (same user re-registering) do not increment the count.
3. When the number of **unique AORs** from one IP reaches the threshold, the
   counter increments **once** (signal deduplication).
4. Further registrations from the same IP do not increment the counter again.
5. When all AORs age out of the window, the signal resets — a new burst can
   trigger a new increment.

**Configuration:**

| Env var | Default | Description |
|---------|---------|-------------|
| `SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD` | `10` | Unique AORs from one IP to trigger |
| `SIP_EXPORTER_FRAUD_REGISTER_SCAN_WINDOW` | `60s` | Sliding window duration |

**Example scenario:** A compromised PBX at 203.0.113.5 registers 15 distinct
accounts in 30 seconds. With default threshold=10, window=60s:
- Registrations 1–9 → no signal (below threshold)
- At the 10th unique AOR → counter increments (+1)
- Registrations 11–15 → no additional increment (dedup)
- After 60 seconds with no new registrations → entries expire, signal resets
- If a new burst of 10+ unique AORs arrives → counter increments again

**Limitations:**
- **SBC round-robin:** An SBC or registrar proxy that round-robins registrations
  across many distinct extension AORs on behalf of healthy phones may trigger
  false positives. Raise the threshold for legitimate high-volume registrars.
- **Credential brute-force (401/403) is NOT detected by this metric.** This signal
  only tracks *successful* registrations. For brute-force detection, use
  `register_failure_total{code="401"}` with the pre-configured
  `SIPRegistrationBruteForce` alert (see `ALERTING.md`).
- SIP scanning with rotating source IPs (botnet) may not reach per-IP thresholds.
  Aggregate `register_scan_total` across all IPs in PromQL for network-wide detection.

---

### 2.2 Registration Country Change (Account Takeover)

**Metric:** `sip_exporter_register_country_change_total{carrier,source_country}` (counter)

**What it detects:** The same SIP user (AOR = `user@host`) successfully
re-registers from a different source country — a strong indicator of account
takeover or credential theft.

**How it works:**

1. On every successful REGISTER (200 OK), the exporter stores the registration
   with its `source_country` (resolved via GeoIP or carrier config).
2. Before overwriting the previous registration entry, the exporter compares the
   new `source_country` with the stored one.
3. If they differ (and the previous was non-empty) → counter increments.
4. The new country becomes the baseline for future comparisons.

**No configuration needed** — uses existing GeoIP/carrier setup from S003.

**Example scenario:** User `alice@example.com` registers from Russia (RU).
Later, the same AOR registers from Georgia (GE):
- `source_country` changes from `RU` to `GE` → counter increments
- If `alice` registers again from `GE` → no signal (same country)

**Limitations:**
- **Legitimate roaming** triggers false positives. A user traveling abroad will
  generate a signal. This is intentional — the operator investigates and
  dismisses if legitimate.
- **GeoIP disabled:** If no GeoIP database is configured and no carrier country
  is set, `source_country` is `"unknown"` for everyone → detection is a no-op
  (never triggers). This is correct behavior — without geographic data, country
  change cannot be detected.
- **Registration expired:** If the previous registration's TTL has expired
  (cleanup removed it), a new registration from a different country does not
  trigger (no baseline to compare). In-session takeover (refresh from new
  country while old registration still alive) is covered.

---

### 2.3 INVITE Burst (Toll-Fraud / DDoS)

**Metric:** `sip_exporter_invite_burst_total{carrier,source_country}` (counter)

**What it detects:** A single source IP sending INVITE requests at an abnormally
high rate — the onset of toll fraud (compromised PBX making expensive calls) or
SIP flood DDoS.

**How it works:**

1. Every initial INVITE request (not re-INVITE within an existing dialog) from a
   source IP is tracked in a sliding window (same algorithm as registration
   scan).
2. When the count in the window exceeds the threshold → counter increments once.
3. Signal resets when the window clears.

**Configuration:**

| Env var | Default | Description |
|---------|---------|-------------|
| `SIP_EXPORTER_FRAUD_INVITE_BURST_THRESHOLD` | `100` | Initial INVITEs from one IP to trigger |
| `SIP_EXPORTER_FRAUD_INVITE_BURST_WINDOW` | `60s` | Sliding window duration |

**Re-INVITE exclusion:** Mid-dialog re-INVITEs (session refresh, codec change,
call hold) are excluded from burst counting. Only initial INVITEs that start
new dialogs are counted. This prevents false positives from legitimate call
centers that frequently use re-INVITEs.

**Example scenario:** A compromised PBX at 198.51.100.10 starts making 150
calls/minute to premium-rate international numbers. With threshold=100:
- At the 100th INVITE → counter increments (+1)
- Further INVITEs in the same window → no additional increment
- After the window clears → a new burst triggers again

**SBC / high-volume sources:** A Session Border Controller, SIP gateway, or
large call center that multiplexes many subscribers through a single source IP
can legitimately exceed the default threshold (100 INVITE/min). In this case,
raise the threshold per source, or configure `SIP_EXPORTER_IGNORE_OUTGOING=true`
if the SBC is in your own network and only inbound fraud matters.

---

### 2.4 Sessions Utilization (Capacity Monitoring)

**Metrics:**
- `sip_exporter_sessions_limit{carrier}` (gauge)
- `sip_exporter_sessions_utilization{carrier}` (gauge)

**What it measures:** How close each carrier is to its configured concurrent
session limit. Not a fraud signal per se, but useful for capacity planning and
contract enforcement — a sudden spike in utilization may indicate fraud or a
misconfigured dialer.

**How it works:**

1. You configure per-carrier session limits in a YAML file.
2. The exporter exposes `sessions_limit{carrier}` as a constant gauge from
   config.
3. On every Prometheus scrape, the exporter computes
   `sessions_utilization = min(100, active_sessions / limit × 100)` per carrier.
4. Carriers without a configured limit do not emit these metrics.

**Capping tradeoff:** Utilization is capped at 100%. If `active_sessions = 300`
and `limit = 100`, the metric reports 100% — the same as 101%. This prevents
cardinality / alert storms, but hides the severity of overage. Monitor
`sip_exporter_sessions` (the raw gauge) alongside utilization to detect extreme
oversubscription.

**`limit: 0`:** Treated as "no limit" — the carrier is excluded from these
metrics entirely (same as not listing it). A value of 0 does **not** mean
"0% / blocked".

**Configuration:**

| Env var | Description |
|---------|-------------|
| `SIP_EXPORTER_SESSIONS_LIMITS` | Path to sessions limits YAML file |

**YAML format:**
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

## 3. Sliding Window Algorithm

Both rate-based signals use sliding window tracking, but with different internal
data structures optimized for their respective use cases.

### Registration Scan — AOR-Keyed Map

The scan tracker deduplicates by AOR: each unique `user@host` is recorded once
per source IP (re-registration of the same AOR updates the timestamp without
incrementing the count). Internally, it uses a map keyed by AOR.

```
REGISTER 200 OK from source IP X
  │
  ▼
Evict AORs with timestamps older than (now - window)
  │
  ▼
Already signaled for IP X? ─── Yes ──→ skip (memory cap)
  │
  No
  │
  ▼
Record AOR in the map (updates timestamp if already present)
  │
  ▼
unique AOR count >= threshold?
  │
  ├── Yes → signaled=true, fire signal (counter++)
  └── No  → wait for more events
```

Once signaled, new AORs are not recorded (memory cap = threshold entries). When
existing AORs age out and count drops below threshold, the signal resets — the
next registration resumes recording.

### INVITE Burst — Timestamp Slice

The burst tracker counts raw INVITE rate (no dedup needed — each INVITE is a
distinct event). Internally, it uses a sorted slice of timestamps, trimmed to the
last `threshold+1` entries.

```
Initial INVITE from source IP X
  │
  ▼
Evict timestamps older than (now - window)
  │
  ▼
Append now to slice
  │
  ▼
Trim slice to last threshold+1 entries (memory cap)
  │
  ▼
len(slice) >= threshold?
  │
  ├── Yes → signaled=true, fire signal (counter++)
  └── No  → wait for more events
```

**Memory:** Both trackers are bounded at O(threshold) entries per unique IP.
With default thresholds (scan=10, burst=100):
- Registration scan: ~10 AOR entries per IP → at most ~800 bytes/IP.
- INVITE burst: ~100 timestamps per IP → ~2.4 KB/IP.
- Even under a 10,000-IP DDoS, total memory is under 25 MB.

**Signal deduplication:** The counter increments at most once per "burst
episode." A burst episode ends when all entries age out of the window (via
per-call eviction and periodic cleanup every 1 second). This produces a clean
Prometheus counter suitable for `rate(metric[5m]) > 0` alerting.

---

## 4. Alerting Examples

These rules match `docs/ALERTING.md` §Fraud Detection Alerts. The canonical
source is ALERTING.md — any divergence should be resolved there.

> **Note on `rate()` window:** `rate(counter[5m]) > 0` stays true for ~5 minutes
> after a signal because the rate window includes the increment. The `for: 1m`
> clause adds an additional 1-minute persistence requirement, reducing noise from
> transient spikes. Using `increase(counter[5m]) > 0` is semantically equivalent
> for boolean alerting — both detect "did the counter increase within the window".

```yaml
# Registration scan (account enumeration) → credential stuffing investigation
- alert: SIPRegistrationScan
  expr: rate(sip_exporter_register_scan_total[5m]) > 0
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "Registration scan attack detected"
    description: "Single IP is registering many different accounts on {{ $labels.carrier }} from {{ $labels.source_country }}. Possible credential stuffing or enumeration."

# INVITE burst → toll-fraud investigation
- alert: SIPInviteBurst
  expr: rate(sip_exporter_invite_burst_total[5m]) > 0
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "INVITE burst detected"
    description: "Single IP is sending an unusually high rate of INVITEs on {{ $labels.carrier }} from {{ $labels.source_country }}. Possible toll fraud, DDoS, or traffic pump."

# Registration country change → account takeover
- alert: SIPRegistrationCountryChange
  expr: rate(sip_exporter_register_country_change_total[5m]) > 0
  for: 1m
  labels:
    severity: warning
  annotations:
    summary: "Registration country change detected"
    description: "A user re-registered from a different country on {{ $labels.carrier }}. Possible account takeover."

# Sessions capacity exhaustion
- alert: SIPSessionCapacityExhaustion
  expr: sip_exporter_sessions_utilization > 90
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Session capacity near exhaustion"
    description: "Carrier {{ $labels.carrier }} is at {{ $value | printf \"%.0f\" }}% of its configured session limit. Plan capacity expansion or investigate traffic surge."
```

---

## 5. Privacy & Cardinality

| Concern | Handling |
|---------|----------|
| Source IP in labels | **Never.** IP used internally only. Labels are `{carrier,source_country}`. |
| SIP URI / phone numbers | **Never** in metrics. AOR used as internal map key only. |
| `ua_type` | Excluded from fraud labels — attackers vary User-Agent, adding cardinality without value. |
| Unbounded IP map growth | Sliding window cap + TTL cleanup (1s tick). Idle IPs evicted when deque empties. |
| Unbounded AOR growth | Reuses `registerExpiryTracker` with existing TTL cleanup (Expires-based). |

---

## 6. Comparison with VoIPMonitor

| Feature | sip-exporter | VoIPMonitor |
|---------|-------------|-------------|
| Registration scan | Per-IP sliding window | Per-IP threshold |
| Country change | Per-AOR GeoIP | GeoIP + ASN |
| INVITE burst | Per-IP sliding window | Per-IP + per-destination |
| Toll-fraud (IRSF) | Not covered | Destination cost analysis |
| Bypass (SIM box) | Not covered | ACD/CLI heuristics |
| Enforcement | Signal only (Prometheus) | Optional blocking |
| Resource cost | ~2.4 MB/10K IPs | Full CDR capture |

sip-exporter covers the **top-2 fraud categories** (compromised PBX, identity
theft) with minimal overhead. IRSF and bypass fraud require CDR/cost data that
an eBPF packet monitor does not have — these remain a gap.
