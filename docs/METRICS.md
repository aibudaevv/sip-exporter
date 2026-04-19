# Metrics Reference

All metrics are exposed at `/metrics` endpoint in Prometheus exposition format.

## SIP traffic

`sip_exporter_packets_total`: total number of parsed SIP packets (requests + responses).

## Active sessions

`sip_exporter_sessions`: number of active SIP dialogs (RFC 3261).

**How dialogs are counted:**
- A dialog is created when a `200 OK` response is received for an `INVITE` request
- A dialog is identified by the tuple: `{Call-ID, From tag, To tag}`
- A dialog is terminated when a `200 OK` response is received for a `BYE` request
- Dialog ID format: `{call-id}:{min-tag}:{max-tag}` (tags sorted lexicographically)
- Dialogs are cleaned up when:
  - `200 OK` received for `BYE` request (normal termination)
  - Session-Expires timeout reached (RFC 4028)
- Default timeout: 1800 seconds (30 min) if `Session-Expires` header not present
- Cleanup runs every 1 second

## SIP request metrics

`sip_exporter_invite_total`: total number of received SIP INVITE requests.  
`sip_exporter_register_total`: total number of received SIP REGISTER requests.  
`sip_exporter_options_total`: total number of received SIP OPTIONS requests.  
`sip_exporter_cancel_total`: total number of received SIP CANCEL requests.  
`sip_exporter_bye_total`: total number of received SIP BYE requests.  
`sip_exporter_ack_total`: total number of received SIP ACK requests.  
`sip_exporter_publish_total`: total number of received SIP PUBLISH requests.  
`sip_exporter_prack_total`: total number of received SIP PRACK requests.  
`sip_exporter_notify_total`: total number of received SIP NOTIFY requests.  
`sip_exporter_subscribe_total`: total number of received SIP SUBSCRIBE requests.  
`sip_exporter_refer_total`: total number of received SIP REFER requests.  
`sip_exporter_info_total`: total number of received SIP INFO requests.  
`sip_exporter_update_total`: total number of received SIP UPDATE requests.

## SIP response metrics (by status code)

`sip_exporter_100_total`: total number of SIP 100 Trying responses.  
`sip_exporter_180_total`: total number of SIP 180 Ringing responses.  
`sip_exporter_183_total`: total number of SIP 183 Session Progress responses.  
`sip_exporter_200_total`: total number of SIP 200 OK responses.  
`sip_exporter_202_total`: total number of SIP 202 Accepted responses.  
`sip_exporter_300_total`: total number of SIP 300 Multiple Choices responses.  
`sip_exporter_302_total`: total number of SIP 302 Moved Temporarily responses.  
`sip_exporter_400_total`: total number of SIP 400 Bad Request responses.  
`sip_exporter_401_total`: total number of SIP 401 Unauthorized responses.  
`sip_exporter_403_total`: total number of SIP 403 Forbidden responses.  
`sip_exporter_404_total`: total number of SIP 404 Not Found responses.  
`sip_exporter_proxy_authentication_required_total`: total number of SIP 407 Proxy Authentication Required responses.  
`sip_exporter_408_total`: total number of SIP 408 Request Timeout responses.  
`sip_exporter_480_total`: total number of SIP 480 Temporarily Unavailable responses.  
`sip_exporter_486_total`: total number of SIP 486 Busy Here responses.  
`sip_exporter_500_total`: total number of SIP 500 Server Internal Error responses.  
`sip_exporter_503_total`: total number of SIP 503 Service Unavailable responses.  
`sip_exporter_504_total`: total number of SIP 504 Server Time-out responses.  
`sip_exporter_600_total`: total number of SIP 600 Busy Everywhere responses.  
`sip_exporter_603_total`: total number of SIP 603 Decline responses.

## System metrics

`sip_exporter_system_error_total`: total number internal SIP exporter errors.

## RFC 6076 Performance Metrics

Metrics defined in [RFC 6076](https://datatracker.ietf.org/doc/html/rfc6076):

### Dialog Lifecycle

```
INVITE → 200 OK → Dialog Created
                     │
                     ├──→ BYE → 200 OK → Dialog Deleted → SCR +1, SPD updated
                     │
                     └──→ [Session-Expires timeout] → Dialog Expired → SCR +1, SPD updated
```

Dialogs are tracked with Session-Expires (RFC 4028). If no BYE is received before timeout, the dialog is cleaned up and counted as "completed" in SCR.

| Metric | RFC 6076 Section | Description |
|--------|------------------|-------------|
| SER | §4.6 | Session Establishment Ratio |
| SEER | §4.7 | Session Establishment Effectiveness Ratio |
| ISA | §4.8 | Ineffective Session Attempts |
| SCR | §4.9 | Session Completion Ratio |
| RRD | §4.1 | Registration Request Delay |
| SPD | §4.5 | Session Process Duration |
| TTR | — | Time to First Response |
| ASR | — | Answer Seizure Ratio (ITU-T E.411) |
| SDC | — | Session Duration Counter |
| NER | — | Network Effectiveness Ratio (GSMA IR.42) |
| ISS | — | Ineffective Session Severity |
| ORD | — | OPTIONS Response Delay |
| LRD | — | Location Registration Delay |

---

### Session Establishment Ratio (SER)

`sip_exporter_ser`: percentage of successfully established sessions relative to total INVITE attempts.

**Formula (RFC 6076 §4.6):**
```
SER = (INVITE → 200 OK) / (Total INVITE - INVITE → 3xx) × 100
```

- 3xx responses (redirects) are **excluded from the denominator** — they are neither success nor failure, but a routing instruction
- A session is counted as established only when the originating UA receives `200 OK` for its INVITE
- Undefined when no INVITE requests have been received
- Undefined when all INVITEs received 3xx responses (denominator = 0)

**Important:** SER is a cumulative metric calculated over the entire runtime.

---

### Session Establishment Effectiveness Ratio (SEER)

`sip_exporter_seer`: percentage of "effective" INVITE responses relative to total non-redirected INVITE attempts.

**Formula (RFC 6076 §4.7):**
```
SEER = (INVITE → 200, 480, 486, 600, 603) / (Total INVITE - INVITE → 3xx) × 100
```

- 3xx responses (redirects) are **excluded from the denominator** — same as SER
- Numerator includes responses that represent a clear outcome from the end user:
  - `200 OK` — session established
  - `480 Temporarily Unavailable` — user temporarily unavailable
  - `486 Busy Here` — user busy
  - `600 Busy Everywhere` — user busy everywhere
  - `603 Decline` — user declined the call
- Responses like `400`, `404`, `500`, `503` are **not** counted as effective — they indicate infrastructure or routing problems
- Undefined when no INVITE requests have been received
- Undefined when all INVITEs received 3xx responses (denominator = 0)

**Important:** Like SER, SEER is cumulative.

**Relationship between SER and SEER:** SEER is always >= SER, since SEER's numerator includes all responses counted by SER plus additional "effective" failure codes (480, 486, 600, 603). The gap between them indicates the proportion of calls that received a definitive user-level outcome rather than a session establishment.

**Example values:**
- `100` — all non-redirect INVITEs received a clear outcome (success or explicit decline)
- `0` — all non-redirect INVITEs received infrastructure errors
- `undefined` — no INVITEs received or all were 3xx redirects

---

### Ineffective Session Attempts (ISA)

`sip_exporter_isa`: percentage of INVITE requests that resulted in server error or timeout responses.

**Formula (RFC 6076 §4.8):**
```
ISA % = (INVITE → 408, 500, 503, 504) / Total INVITE × 100
```

- Unlike SER/SEER, 3xx responses are **NOT excluded from the denominator** — ISA measures infrastructure reliability
- Numerator includes server-side failures that indicate system overload or malfunction:
  - `408 Request Timeout` — downstream server did not respond
  - `500 Server Internal Error` — internal server failure
  - `503 Service Unavailable` — service temporarily unavailable (overload)
  - `504 Server Time-out` — server gateway timeout
- Responses like `400`, `401`, `403`, `404` are **not** counted — they indicate client-side issues, not server failures
- Undefined when no INVITE requests have been received

**Important:** ISA is cumulative over the entire runtime.

**Relationship with SER/SEER:** ISA measures infrastructure health, while SER/SEER measure session establishment success. A rising ISA typically indicates overloaded or failing downstream servers. Unlike SER (which excludes 3xx), ISA includes all INVITEs in the denominator.

**Example values:**
- `0` — no server errors or timeouts detected
- `5` — 5% of INVITEs resulted in server failures (monitoring threshold)
- `>15` — significant infrastructure issues requiring immediate attention

#### Understanding ISA

ISA measures infrastructure health, not user experience. Unlike SER/SEER which measure session establishment success, ISA tracks server-side failures that indicate system problems.

| ISA Trend | What It Means | Likely Causes |
|-----------|---------------|---------------|
| **ISA rising** | Infrastructure is degrading | Server overload, network packet loss, failing dependencies (DB, cache), misconfigured load balancers |
| **ISA falling** | Infrastructure is stabilizing | Servers recovering, errors decreasing, system returning to healthy state |
| **ISA 0-5%** | Healthy system | Normal operations, no action needed |
| **ISA 5-15%** | Warning zone | Investigate emerging issues before they escalate |
| **ISA >15%** | Critical | Immediate diagnostics required — servers or network are failing |

---

### Session Completion Ratio (SCR)

`sip_exporter_scr`: percentage of INVITE sessions that were fully completed (established and terminated) relative to total INVITE attempts.

**Formula (RFC 6076 §4.9):**
```
SCR = (Completed Sessions) / Total INVITE × 100
```

- Unlike SER/SEER, 3xx responses are **NOT excluded from the denominator** — SCR measures end-to-end session completion
- A session is counted as "completed" when:
  1. `200 OK` received for `BYE` (normal termination), **OR**
  2. Dialog expired via Session-Expires timeout (RFC 4028)
- Expired dialogs are counted as completed to prevent SCR inflation from "hanging" sessions
- Default Session-Expires: 1800 seconds (30 minutes), configurable via SIP `Session-Expires` header
- Undefined when no INVITE requests have been received

**Important:** SCR is cumulative over the entire runtime.

**Relationship with SER:** SCR is always <= SER, since only a subset of established sessions are fully completed. The gap indicates sessions still active or abandoned without BYE.

**Example values:**
- `100` — all INVITEs resulted in fully completed sessions (INVITE→200 OK + BYE→200 OK)
- `50` — half of all INVITEs resulted in complete call cycles
- `0` — no sessions were fully completed (either no answers or no BYE sent)

---

### Registration Request Delay (RRD)

`sip_exporter_rrd`: histogram of delays in milliseconds between sending a REGISTER request and receiving a 200 OK response.

**Formula (RFC 6076 §4.1):**
```
RRD = Average(Time of 200 OK - Time of REGISTER request)
```

- Measures the round-trip time for SIP registration transactions
- Only successful registrations (200 OK responses) are measured
- Exposed as a Prometheus Histogram with buckets: `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` ms
- Use `histogram_quantile()` for percentile-based alerting

**PromQL examples:**
```promql
# 95th percentile registration delay
histogram_quantile(0.95, rate(sip_exporter_rrd_bucket[5m]))

# Average registration delay
rate(sip_exporter_rrd_sum[5m]) / rate(sip_exporter_rrd_count[5m])
```

**Important:** RRD measures registration latency, not call setup latency. Use SER/SEER for call establishment metrics.

**Example values:**
- `< 100 ms` — excellent registration performance (local network)
- `100-500 ms` — acceptable performance (typical WAN)
- `> 1000 ms` — potential issues (network congestion, server overload)

**Deprecated metric:** `sip_exporter_rrd_average` — cumulative average (will be removed in next major version)

---

### Session Process Duration (SPD)

`sip_exporter_spd`: histogram of completed SIP session durations in seconds.

**Formula (RFC 6076 §4.5):**
```
SPD = (Cumulative Session Duration) / (Completed Session Count)
```

- Measures the time from session establishment (`200 OK` to `INVITE`) to session termination (`200 OK` to `BYE` or Session-Expires timeout)
- A session begins when the dialog is created upon receiving `200 OK` for `INVITE`
- A session ends when:
  1. `200 OK` received for `BYE` (normal termination), **OR**
  2. Dialog expires via Session-Expires timeout (RFC 4028)
- Exposed as a Prometheus Histogram with buckets: `[1, 5, 10, 30, 60, 300, 600, 1800, 3600]` seconds
- Use `histogram_quantile()` for percentile-based alerting

**PromQL examples:**
```promql
# 99th percentile session duration
histogram_quantile(0.99, rate(sip_exporter_spd_bucket[5m]))

# Average session duration
rate(sip_exporter_spd_sum[5m]) / rate(sip_exporter_spd_count[5m])
```

**Important:** SPD measures actual session duration, not setup latency. Use SER/SEER for establishment metrics.

**Example values:**
- `< 30 s` — short calls (IVR, voicemail)
- `180 s` — typical voice call (~3 minutes)
- `> 3600 s` — long-duration sessions (conferences, held calls)

**Deprecated metric:** `sip_exporter_spd_average` — cumulative average (will be removed in next major version)

---

### Time to First Response (TTR)

`sip_exporter_ttr`: histogram of delays in milliseconds between an INVITE request and the first provisional (1xx) response.

**Formula:**
```
TTR = Time of first 1xx response - Time of INVITE request
```

- Not defined in RFC 6076, but is a useful operational metric for detecting slow SIP servers
- Measures the time from INVITE to the **first** provisional response (100 Trying, 180 Ringing, 183 Session Progress)
- Only the first 1xx response is measured — subsequent provisional responses are ignored
- If no provisional response is received (e.g., INVITE → 200 OK directly), TTR is not measured
- Exposed as a Prometheus Histogram with buckets: `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` ms
- Use `histogram_quantile()` for percentile-based alerting

**PromQL examples:**
```promql
# 95th percentile time to first response
histogram_quantile(0.95, sum(rate(sip_exporter_ttr_bucket[5m])) by (le))

# Average time to first response
rate(sip_exporter_ttr_sum[5m]) / rate(sip_exporter_ttr_count[5m])
```

**Example values:**
- `< 50 ms` — excellent server responsiveness
- `100-500 ms` — acceptable (typical for loaded servers)
- `> 1000 ms` — potential issues (server overload, network latency)

---

### Answer Seizure Ratio (ASR)

`sip_exporter_asr`: percentage of INVITE requests that received a 200 OK response.

**Formula (ITU-T E.411):**
```
ASR = (INVITE → 200 OK) / Total INVITE × 100
```

- Classic telephony metric defined in ITU-T E.411, referenced by RFC 6076 §4.6
- Unlike SER, 3xx responses are **NOT excluded from the denominator**
- Undefined when no INVITE requests have been received

**Relationship with SER:** ASR is always <= SER. When 3xx responses are present, SER excludes them from the denominator, making SER higher. ASR keeps all INVITEs in the denominator.

**PromQL examples:**
```promql
# Current ASR
sip_exporter_asr

# Compare with SER to detect redirect volume
sip_exporter_ser - sip_exporter_asr
```

**Example values:**
- `100` — all INVITEs received 200 OK
- `50` — half of INVITEs received 200 OK
- `0` — no INVITEs received 200 OK

---

### Session Duration Counter (SDC)

`sip_exporter_sdc_total`: total number of completed SIP sessions (Prometheus Counter).

- Counts sessions that ended via:
  1. `200 OK` received for `BYE` (normal termination), **OR**
  2. Dialog expired via Session-Expires timeout (RFC 4028)
- Same events counted by SCR numerator, but exposed as a Counter for rate queries

**PromQL examples:**
```promql
# Session completion rate (sessions per second)
rate(sip_exporter_sdc_total[5m])

# Session completion rate per minute
rate(sip_exporter_sdc_total[1m]) * 60
```

---

### Network Effectiveness Ratio (NER)

`sip_exporter_ner`: percentage of INVITE requests that did **not** result in ineffective (infrastructure failure) responses.

**Formula (GSMA IR.42):**
```
NER = (Total INVITE - INVITE → 408, 500, 503, 504) / Total INVITE × 100
NER = 100 - ISA
```

- Defined in GSMA IR.42 (not RFC 6076), widely used in mobile operator networks
- 3xx responses are **NOT excluded from the denominator**
- Measures network quality including call termination — higher NER means fewer infrastructure failures
- Undefined when no INVITE requests have been received

**Relationship with ISA:** NER = 100 − ISA. Always use together: ISA for failure percentage, NER for success percentage.

**PromQL examples:**
```promql
# Current NER
sip_exporter_ner

# Verify NER = 100 - ISA
sip_exporter_ner + sip_exporter_isa
```

**Example values:**
- `100` — no infrastructure failures (all INVITEs resolved without 408/500/503/504)
- `95` — 5% of INVITEs hit server errors or timeouts
- `0` — all INVITEs resulted in infrastructure failures

---

### Ineffective Session Severity (ISS)

`sip_exporter_iss_total`: total number of INVITE requests that resulted in ineffective responses (Prometheus Counter).

- Counts INVITE responses with status codes: `408`, `500`, `503`, `504`
- Same codes used by ISA numerator, but exposed as an absolute Counter
- Useful for alerting on absolute volume of infrastructure failures (unlike ISA which is a percentage)

**PromQL examples:**
```promql
# Ineffective sessions per second
rate(sip_exporter_iss_total[5m])

# Total ineffective sessions since start
sip_exporter_iss_total

# Alert: more than 20 ineffective sessions per second
rate(sip_exporter_iss_total[5m]) > 20
```

**Example values:**
- `0` — no infrastructure failures detected
- `rate > 5/sec` — elevated error rate, investigate
- `rate > 20/sec` — critical, immediate attention required

---

### OPTIONS Response Delay (ORD)

`sip_exporter_ord`: histogram of delays in milliseconds between sending an OPTIONS request and receiving any response.

**Formula:**
```
ORD = Time of OPTIONS response - Time of OPTIONS request
```

- Measures round-trip time for SIP OPTIONS-pong transactions
- Any response is measured (not only 200 OK) — OPTIONS is used for keepalive/health-check
- OPTIONS requests are tracked by Call-ID with TTL-based cleanup (60s)
- Exposed as a Prometheus Histogram with buckets: `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` ms

**PromQL examples:**
```promql
# 95th percentile OPTIONS response delay
histogram_quantile(0.95, sum(rate(sip_exporter_ord_bucket[5m])) by (le))

# Average OPTIONS response delay
rate(sip_exporter_ord_sum[5m]) / rate(sip_exporter_ord_count[5m])
```

**Example values:**
- `< 50 ms` — excellent SIP server responsiveness
- `100-500 ms` — acceptable (typical for WAN or loaded servers)
- `> 1000 ms` — potential issues (server overload, network latency)

---

### Location Registration Delay (LRD)

`sip_exporter_lrd`: histogram of delays in milliseconds between sending a REGISTER request and receiving a 3xx redirect response.

**Formula:**
```
LRD = Time of REGISTER 3xx response - Time of REGISTER request
```

- Measures delay for registration redirect scenarios (e.g., SIP load balancer redirecting to another registrar)
- Only 3xx responses to REGISTER are measured (200 OK is measured by RRD)
- Reuses the same `registerTracker` as RRD — REGISTER→3xx triggers LRD, REGISTER→200 OK triggers RRD
- Exposed as a Prometheus Histogram with buckets: `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` ms

**PromQL examples:**
```promql
# 95th percentile location registration delay
histogram_quantile(0.95, sum(rate(sip_exporter_lrd_bucket[5m])) by (le))

# Average location registration delay
rate(sip_exporter_lrd_sum[5m]) / rate(sip_exporter_lrd_count[5m])
```

**Example values:**
- `< 50 ms` — fast redirect processing
- `100-500 ms` — acceptable (redirect involves DNS or database lookup)
- `> 1000 ms` — potential issues (slow redirect server, DNS resolution delays)
