# Metrics Reference

All metrics are exposed at `/metrics` endpoint in Prometheus exposition format.

## Contents

- [Labels](#labels)
  - [Carrier Label](#carrier-label)
  - [User-Agent Type Label](#user-agent-type-label)
  - [Geo-Enrichment Labels](#geo-enrichment-labels)
- [System Metrics](#system-metrics) — `packets_total`, `system_error_total`
- [Active Sessions](#active-sessions) — `sessions`
- [SIP Request Metrics](#sip-request-metrics) — `invite_total`, `reinvite_total`, `register_total`, `bye_total`, etc.
- [SIP Response Metrics](#sip-response-metrics-by-status-code) — `100_total`…`606_total`
- [Registration Health](#registration-health) — `register_success_total`, `register_failure_total`, `register_success_ratio`, `active_registrations`
- [Fraud Detection](#fraud-detection) — `register_scan_total`, `invite_burst_total`, `register_country_change_total`, `fas_calls_total`
- [Capacity Monitoring](#capacity-monitoring) — `sessions_limit`, `sessions_utilization`
- [Traffic Minutes by Destination](#traffic-minutes-by-destination) — `billable_seconds_total`
- [RTP Media Metrics](#rtp-media-metrics) — `rtp_packets_total`, `rtp_mos_score`, `rtp_jitter_milliseconds`, `rtp_pdv_milliseconds`, etc.
- [RTCP Endpoint-Reported Metrics](#rtcp-endpoint-reported-metrics) — `rtcp_jitter_milliseconds`, `rtcp_rtt_milliseconds`, `rtcp_loss_fraction_percent`, `rtcp_cumulative_loss_total`, `rtcp_reports_total`
- [Self-Monitoring Metrics](#self-monitoring-metrics) — `socket_packets_*`, `rtp_dropped_total`, `channel_*`, `parse_errors_total`, `active_trackers`, `active_dialogs`, `build_info`
- [RFC 6076 Performance Metrics](#rfc-6076-performance-metrics)
  - [SER](#session-establishment-ratio-ser) — Session Establishment Ratio
  - [SEER](#session-establishment-effectiveness-ratio-seer) — Session Establishment Effectiveness Ratio
  - [ISA](#ineffective-session-attempts-isa) — Ineffective Session Attempts
  - [SCR](#session-completion-ratio-scr) — Session Completion Ratio
  - [ASR](#answer-seizure-ratio-asr) — Answer Seizure Ratio
  - [NER](#network-effectiveness-ratio-ner) — Network Effectiveness Ratio
  - [SDC](#session-duration-counter-sdc) — Session Duration Counter
  - [ISS](#ineffective-session-severity-iss) — Ineffective Session Severity
  - [RRD](#registration-request-delay-rrd) — Registration Request Delay
  - [SPD](#session-process-duration-spd) — Session Process Duration
  - [TTR](#time-to-first-response-ttr) — Time to First Response
  - [PDD](#post-dial-delay-pdd) — Post Dial Delay
  - [PBD](#post-bye-delay-pbd) — Post Bye Delay
  - [ORD](#options-response-delay-ord) — OPTIONS Response Delay
  - [LRD](#location-registration-delay-lrd) — Location Registration Delay
- [Voice Quality Metrics (RFC 6035)](#voice-quality-metrics-rfc-6035) — NLR, JDR, BLD, GLD, RTD, ESD, IAJ, MAJ, MOSLQ, MOSCQ, RLQ, RCQ, RERL
- [Alerts](#alerts) — pre-configured alert rules and threshold recommendations

## Labels

SIP metrics use a multi-layer label model. Most metrics include **three base labels**; INVITE-related raw counters add **three more**:

| Label | Scope | Value | Description |
|-------|-------|-------|-------------|
| `carrier` | Base (all SIP) | Carrier name from config | Source IP → CIDR mapping, resolved at request time |
| `ua_type` | Base (all SIP) | UA type from config | `User-Agent` header → regex mapping, resolved at request time |
| `source_country` | Base (all SIP) | ISO 3166-1 alpha-2 | Country of the calling device. See [Geo-Enrichment Labels](#geo-enrichment-labels) |
| `direction` | Base (all SIP) | `inbound` or `outbound` | Traffic direction from the server's perspective. See [Direction Label](#direction-label) |
| `destination_country` | INVITE raw only | ISO alpha-2 or `"unknown"` | Destination country from E.164 phone-number prefix. See [Geo-Enrichment Labels](#geo-enrichment-labels) |
| `caller_host` | INVITE raw only (**opt-in**) | IP or domain | Host part of the `From` SIP URI |
| `called_host` | INVITE raw only (**opt-in**) | IP or domain | Host part of the `To` SIP URI |

> **Note:** Individual metric signatures below show `{carrier="...",ua_type="..."}` for brevity. Use this table to determine the full label set for any metric:
>
> | Tier | Metrics | Full label set |
> |------|---------|----------------|
> | **System** | `packets_total`, `system_error_total`, self-monitoring | *(none)* |
> | **Base** | All SIP requests, SER/SEER/ISA/SCR/ASR/NER, RRD/SPD/TTR/PDD/ORD/LRD/PBD, VQ reports, sessions, `reinvite_total`, registration health (`register_success_total`, `register_success_ratio`, `active_registrations`) | `carrier, ua_type, source_country, direction` |
> | **Reg failure** | `register_failure_total` | `carrier, ua_type, source_country, direction, code` |
> | **Retransmission** | `sip_retransmission_total` | `carrier, ua_type, source_country, direction, method` |
> | **RTP** | `rtp_packets_total`, `rtp_packets_lost_total`, `rtp_duplicate_packets_total`, `rtp_out_of_order_total`, `rtp_jitter_milliseconds`, `rtp_pdv_milliseconds`, `rtp_mos_score`, `rtp_mos_f1`, `rtp_mos_f2`, `rtp_mos_adaptive`, `rtp_r_factor`, `rtp_burst_loss_density`, `rtp_gap_loss_density`, `rtp_active_streams` | `carrier, ua_type, codec, source_country, direction` |
> | **RTP dialog** | `rtp_oneway_calls_total`, `sessions_missing_rtp_total` | `carrier, ua_type, source_country, direction` |
> | **INVITE raw** | `invite_total`, `invite_200_total` | `carrier, ua_type, source_country, direction, destination_country, caller_host, called_host, iface` |
> | **INVITE redirects** | `invite_3xx_total` | `carrier, ua_type, source_country, direction` |
> | **Fraud** | `register_country_change_total`, `register_scan_total`, `invite_burst_total` | `carrier, source_country, direction` |
> | **Fraud (call-level)** | `fas_calls_total` | `carrier, ua_type, source_country, direction` |
> | **Short calls** | `short_calls_total` | `carrier, ua_type, source_country, direction, threshold` |
> | **Traffic** | `billable_seconds_total` | `carrier, destination_country, direction` |
> | **Capacity** | `sessions_limit`, `sessions_utilization` | `carrier` |

`carrier` and `ua_type` default to `"other"` when not configured or when no pattern matches. `source_country` defaults to `"unknown"` when neither carrier country nor GeoIP DB is available.

**Example:**
```
sip_exporter_invite_total{carrier="carrier-a",ua_type="yealink",source_country="RU",direction="inbound",destination_country="US",caller_host="10.1.5.20",called_host="sip.example.com",iface="ens3"} 1523
sip_exporter_200_total{carrier="carrier-a",ua_type="yealink",source_country="RU",direction="inbound"} 847
sip_exporter_ser{carrier="carrier-a",ua_type="yealink",source_country="RU",direction="inbound"} 95.2
```

### Metrics WITHOUT `carrier` and `ua_type` labels

The following metrics are system-level and do not include either label:

- `sip_exporter_system_error_total` — internal exporter errors (not SIP traffic)
- `sip_exporter_packets_total` — counts all parsed SIP packets regardless of source

### Default behavior

- If no carriers config is provided (`SIP_EXPORTER_CARRIERS_CONFIG` not set), all SIP metrics use `carrier="other"`.
- If no user-agents config is provided (`SIP_EXPORTER_USER_AGENTS_CONFIG` not set), all SIP metrics use `ua_type="other"`.

### Carrier Label

The `carrier` label identifies the network operator that **initiated** the SIP transaction. It is resolved from the source IP address of the **request** (INVITE, REGISTER, OPTIONS) and propagated to all related responses and dialog lifecycle events via tracker.

### Configuration

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `SIP_EXPORTER_CARRIERS_CONFIG` | — | no | Path to YAML file with CIDR-to-carrier mapping |
| `SIP_EXPORTER_USER_AGENTS_CONFIG` | — | no | Path to YAML file with User-Agent-to-type mapping |

**Config file format:**
```yaml
carriers:
  - name: "mobile-operator-a"
    cidrs:
      - "10.1.0.0/16"
      - "10.2.0.0/16"

  - name: "sip-trunk-provider"
    cidrs:
      - "192.168.10.0/24"
      - "192.168.11.0/24"

  - name: "enterprise-pbx"
    cidrs:
      - "172.16.5.0/24"
```

See [`examples/carriers.yaml`](../examples/carriers.yaml) for a complete example.

#### How to fill the configuration

- `name` — arbitrary string used as the `carrier` label value in Prometheus. Avoid spaces and special characters.
- `cidrs` — list of IPv4 subnets in CIDR notation. Specify subnets of **devices that send SIP requests** (phones, SBCs, PBXs, SIP proxies).
- **Order matters**: first matching CIDR wins (first match wins).
- IPs not matching any CIDR → `carrier="other"`.
- If `SIP_EXPORTER_CARRIERS_CONFIG` is not set → all metrics use `carrier="other"`.

**Recommendations:**
- Group CIDRs by **logical owner** (operator, client, VLAN).
- For per-client quality monitoring: each client = own carrier with their subnets.
- For per-upstream monitoring: each upstream provider = own carrier.
- Avoid overlapping CIDRs across carriers — order may cause unexpected results.

### Resolution algorithm

Carrier is determined at **request time** and inherited by all responses in the same transaction:

```
1. SIP request arrives (INVITE/REGISTER/OPTIONS):
   - Source IP extracted from IPv4 header
   - IP matched against configured CIDRs (first match wins)
   - If no match — destination IP is checked as fallback
   - If neither matches → carrier="other"
   - Carrier saved in tracker by Call-ID

2. SIP response arrives:
   - Carrier retrieved from tracker (by Call-ID), NOT from response IP
   - INVITE responses → carrier from inviteTracker
   - REGISTER responses → carrier from registerTracker
   - OPTIONS responses → carrier from optionsTracker
   - If tracker entry expired (TTL 60s) → falls back to response packet IP

3. Dialog lifecycle:
   - Dialog created with carrier from INVITE tracker
   - BYE 200 OK → carrier from dialog entry
   - Session-Expires expiry → carrier from dialog entry
```

### Carrier semantics per metric

| Metric | Carrier source | Meaning |
|--------|---------------|---------|
| `invite_total{carrier}` | INVITE sender IP | How many calls this carrier initiated |
| `200_total{carrier}` | Request tracker | How many 200 OK for this carrier's transactions |
| `sessions{carrier}` | INVITE tracker → dialog | Active dialogs initiated by this carrier |
| SER, SEER, ISA, SCR, ASR, NER | INVITE tracker | Quality of calls initiated by this carrier |
| RRD | Register tracker | Registration delay for this carrier |
| TTR | Invite tracker | Time to first response for this carrier's INVITEs |
| PDD | Invite tracker | Post dial delay (INVITE → 180 Ringing) for this carrier's INVITEs |
| SPD | Dialog (INVITE carrier) | Duration of sessions initiated by this carrier |
| ORD | Options tracker | OPTIONS response delay for this carrier |
| LRD | Register tracker | Registration redirect delay for this carrier |
| `system_error_total` | No carrier | System-level errors |
| `packets_total` | No carrier | All SIP packets |

### Example scenario

```
Configuration:
  carrier-A: 10.0.1.0/24  (mobile operator, sends INVITE)
  carrier-B: 10.0.2.0/24  (SIP platform, responds 200 OK)

Scenario: carrier-A subscriber calls through carrier-B platform

Packet                  | Source IP  | Carrier resolved  | Source
INVITE                  | 10.0.1.5   | carrier-A         | IP → tracker
100 Trying              | 10.0.2.5   | carrier-A         | inviteTracker
200 OK                  | 10.0.2.5   | carrier-A         | inviteTracker
ACK                     | 10.0.1.5   | carrier-A         | IP (request)
BYE                     | 10.0.1.5   | carrier-A         | IP (request)
200 OK to BYE           | 10.0.2.5   | carrier-A         | dialog entry

Result:
  invite_total{carrier="carrier-A",ua_type="yealink"} += 1
  invite_200_total{carrier="carrier-A",ua_type="yealink"} += 1
  sessions{carrier="carrier-A",ua_type="yealink"} = N
  sdc_total{carrier="carrier-A",ua_type="yealink"} += 1
  SER{carrier="carrier-A",ua_type="yealink"} is correct

Carrier-B metrics: only response counters for non-tracked packets (if any)
```

**Analytical meaning:** `carrier` represents the **call initiator**. This aligns with:
- Billing model (caller pays)
- RFC 6076 metrics (all tied to INVITE initiator)
- Capacity planning ("how many sessions is this carrier generating?")

---

## User-Agent Type Label

The `ua_type` label identifies the **type of SIP device** that sent the request, based on the `User-Agent` header. It is resolved from the header value using regex patterns defined in a YAML config, and propagated to all related responses and dialog lifecycle events via the same tracker mechanism as `carrier`.

**Why it matters:**
- Different SIP devices have different failure patterns — IP phones fail differently than softphones or SBCs
- Troubleshooting: "Yealink phones get 408 timeouts, but Grandstream phones don't" — impossible to see without `ua_type`
- Capacity planning: "how many calls come from mobile clients vs desk phones?"

### Default behavior

If no user-agents config is provided (`SIP_EXPORTER_USER_AGENTS_CONFIG` not set), all SIP metrics use `ua_type="other"`.

### Configuration

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `SIP_EXPORTER_USER_AGENTS_CONFIG` | — | no | Path to YAML file with User-Agent regex patterns |

**Config file format:**
```yaml
user_agents:
  - regex: '(?i)^Yealink'
    label: yealink

  - regex: '(?i)^Grandstream'
    label: grandstream

  - regex: '(?i)^Cisco/SPA'
    label: cisco_spa

  - regex: '(?i)^Kamailio'
    label: kamailio

  - regex: '(?i)^Asterisk'
    label: asterisk

  - regex: '(?i)^FreeSWITCH'
    label: freeswitch
```

See [`examples/user_agents.yaml`](../examples/user_agents.yaml) for a complete example with 11 patterns.

#### How to fill the configuration

- `regex` — Go-compatible regular expression matched against the full `User-Agent` header value. Use `(?i)` prefix for case-insensitive matching, or anchoring like `^Yealink` to match from the start.
- `label` — arbitrary string used as the `ua_type` label value in Prometheus. Avoid spaces and special characters.
- **Order matters**: first matching regex wins (first match wins).
- `User-Agent` values not matching any regex → `ua_type="other"`.
- If `SIP_EXPORTER_USER_AGENTS_CONFIG` is not set → all metrics use `ua_type="other"`.

**Recommendations:**
- Group patterns by **device family** (Yealink SIP-T46S, SIP-T42S → both match `^Yealink`).
- Use broad patterns for device families, specific patterns for problematic models.
- Typical categories: IP phones (Yealink, Grandstream, Cisco), softphones (MicroSIP, Zoiper, Linphone), servers (Asterisk, Kamailio, FreeSWITCH), SBCs.
- The `User-Agent` header is extracted from all SIP packets (requests and responses). However, SIP responses typically use the `Server` header instead of `User-Agent`, so in practice only requests provide meaningful classification.

### Resolution algorithm

UA type is determined at **request time** and inherited by all responses in the same transaction, using the same tracker mechanism as `carrier`:

```
1. SIP request arrives (INVITE/REGISTER/OPTIONS):
   - User-Agent header extracted from SIP packet
   - Header value matched against configured regex patterns (first match wins)
   - If no match → ua_type="other"
   - ua_type saved in tracker by Call-ID (alongside carrier)

2. SIP response arrives:
   - ua_type retrieved from tracker (by Call-ID), NOT from response packet
   - INVITE responses → ua_type from inviteTracker
   - REGISTER responses → ua_type from registerTracker
   - OPTIONS responses → ua_type from optionsTracker
   - If tracker entry expired (TTL 60s) → falls back to response packet's User-Agent
   - If response has no User-Agent → ua_type="other"

3. Dialog lifecycle:
   - Dialog created with ua_type from INVITE tracker
   - BYE 200 OK → ua_type from dialog entry
   - Session-Expires expiry → ua_type from dialog entry
```

### ua_type semantics per metric

| Metric | UA type source | Meaning |
|--------|---------------|---------|
| `invite_total{ua_type}` | INVITE User-Agent header | How many calls this device type initiated |
| `200_total{ua_type}` | Request tracker | How many 200 OK for this device type's transactions |
| `sessions{ua_type}` | INVITE tracker → dialog | Active dialogs from this device type |
| SER, SEER, ISA, SCR, ASR, NER | INVITE tracker | Quality of calls from this device type |
| RRD | Register tracker | Registration delay for this device type |
| TTR | Invite tracker | Time to first response for this device type |
| PDD | Invite tracker | Post dial delay for this device type |
| SPD | Dialog (INVITE ua_type) | Duration of sessions from this device type |
| ORD | Options tracker | OPTIONS response delay for this device type |
| LRD | Register tracker | Registration redirect delay for this device type |
| `system_error_total` | No ua_type | System-level errors |
| `packets_total` | No ua_type | All SIP packets |

### Example scenario

```
Configuration:
  ua_type patterns:
    - regex: '(?i)^Yealink'     → label: yealink
    - regex: '(?i)^Grandstream'  → label: grandstream

Scenario: Yealink phone calls through SIP platform

Packet                      | User-Agent              | ua_type resolved | Source
INVITE                      | Yealink SIP-T46S 66.15  | yealink          | header → tracker
100 Trying                  | (none / Server header)  | yealink          | inviteTracker
200 OK                      | (none / Server header)  | yealink          | inviteTracker
ACK                         | Yealink SIP-T46S 66.15  | yealink          | header (request)
BYE                         | Yealink SIP-T46S 66.15  | yealink          | header (request)
200 OK to BYE               | (none / Server header)  | yealink          | dialog entry

Result:
  invite_total{carrier="...",ua_type="yealink"} += 1
  sessions{carrier="...",ua_type="yealink"} = N
  SER{carrier="...",ua_type="yealink"} is correct
  SPD{carrier="...",ua_type="yealink"} observed

Grandstream phone INVITE with "Grandstream GXP2160" → ua_type="grandstream"
Unknown device with "SomeUnknownClient/1.0"         → ua_type="other"
No User-Agent header at all                         → ua_type="other"
```

**Analytical meaning:** `ua_type` represents the **device type of the call initiator**. This aligns with:
- Troubleshooting by device family ("Yealink phones have low SER")
- Firmware issue detection ("specific model gets 503 errors")
- Capacity planning per device type ("how many calls from mobile softphones?")

### Combined carrier + ua_type analysis

Both labels work together for two-dimensional analysis:

```promql
# SER per carrier AND device type
sip_exporter_ser

# SER for Yealink phones on carrier-a
sip_exporter_ser{carrier="carrier-a",ua_type="yealink"}

# Compare Yealink vs Grandstream on same carrier
sip_exporter_ser{carrier="carrier-a",ua_type="yealink"}
  - sip_exporter_ser{carrier="carrier-a",ua_type="grandstream"}

# Active sessions by device type (across all carriers)
sum by (ua_type) (sip_exporter_sessions)

# INVITE rate per carrier per device type
sum by (carrier, ua_type) (rate(sip_exporter_invite_total[5m]))
```

---

## Geo-Enrichment Labels

SIP Exporter enriches metrics with geographic context using a **two-method model**:

| Dimension | Method | Based on | Labels |
|-----------|--------|----------|--------|
| **Source country** (where the call originates) | GeoIP lookup of source IP | MaxMind GeoLite2-Country DB | `source_country` |
| **Destination country** (where the call goes) | E.164 phone-number prefix | Embedded prefix table (Google libphonenumber, Apache 2.0) | `destination_country` |

GeoIP for source IP, phone-number prefix for destination — two independent methods, each optimal for its task.

### source_country

**Resolution precedence:**

```
1. carrier.country  →  if the carrier has a "country" field in carriers.yaml, it takes priority
2. GeoIP(srcIP)     →  MaxMind GeoLite2-Country lookup of the source IP
3. "unknown"        →  fallback when neither is available
```

- **carrier.country** (operator-curated, authoritative): set `country: "RU"` on a carrier in `carriers.yaml` — overrides GeoIP for all IPs in that carrier's CIDRs
- **GeoIP**: requires `GeoLite2-Country.mmdb` (download from [maxmind.com](https://www.maxmind.com)). Private IPs (RFC 1918: `10.x`, `172.16-31.x`, `192.168.x`) return no result from MaxMind — use `carrier.country` for enterprise/contact-center deployments
- **Without GeoIP DB**: all `source_country` labels are `"unknown"` unless `carrier.country` is set — zero additional cardinality

**Config:**

| Variable | Default | Description |
|----------|---------|-------------|
| `SIP_EXPORTER_GEOIP_COUNTRY_DB` | (empty = disabled) | Path to `GeoLite2-Country.mmdb` |

**Cardinality impact:** ~250 ISO alpha-2 codes. With GeoIP disabled and no `carrier.country`, every metric has `source_country="unknown"` — the same cardinality as without the label.

### destination_country

**Resolution logic:**

```
1. Number starts with "+" or "00"  →  E.164 longest-prefix match (e.g. "+7495..." → RU)
2. Otherwise, LOCAL_COUNTRY_CODE set →  use that code (domestic number fallback)
3. Otherwise                        →  "unknown"
```

- **E.164 table** is embedded in the binary (generated from Google libphonenumber `PhoneNumberMetadata.xml`, Apache 2.0). **No database download required** — unlike GeoIP
- Correctly handles multi-national codes: `+1212...`→US, `+1416...`→CA (Toronto), `+7727...`→KZ (Almaty), `+7495...`→RU
- **INVITE-only**: `destination_country` appears only on `invite_total` and `invite_200_total` (not on response counters, SER/SCR, RTP, etc.)

**Config:**

| Variable | Default | Description |
|----------|---------|-------------|
| `SIP_EXPORTER_LOCAL_COUNTRY_CODE` | (empty = off) | ISO alpha-2 code for domestic numbers without international prefix (e.g. `"RU"`, `"US"`) |

**Example:**
```
# INVITE to +74951234567 (Moscow) with GeoIP enabled
sip_exporter_invite_total{carrier="carrier-a",ua_type="yealink",source_country="RU",destination_country="RU",caller_host="10.1.5.20",called_host="sip.operator.com"} 100

# INVITE to +442071838750 (London)
sip_exporter_invite_total{...,destination_country="GB"} 50
```

### caller_host / called_host

The host part of the `From` and `To` SIP URIs, respectively. Extracted during packet parsing from `<sip:user@host:port>`.

- Can be an IP address (`10.1.5.20`) or a domain name (`sip.example.com`)
- **INVITE-only**: appears only on `invite_total` and `invite_200_total`
- **Opt-in** (`SIP_EXPORTER_HOST_LABELS`, default `false`): disabled by default because distinct endpoint identifiers are unbounded. When off, both labels are empty (zero cardinality). Enable only on trusted deployments where the endpoint count is bounded — see [Security](SECURITY.md#data-exposed-in-prometheus-labels).

**Config:**

| Variable | Default | Description |
|----------|---------|-------------|
| `SIP_EXPORTER_HOST_LABELS` | `false` | Enable `caller_host`/`called_host` on `invite_total` / `invite_200_total`. Off by default (unbounded cardinality). |

### iface

The network interface name (e.g. `ens3`, `tun0`, `lo`) on which the packet was captured. Populated from `SIP_EXPORTER_INTERFACE` — each monitored NIC produces separate metric series.

- **Metrics**: `invite_total`, `invite_200_total`, `socket_packets_received_total`, `socket_packets_dropped_total`
- **Always on** (no config toggle): the label is populated whenever the exporter captures traffic
- **Cardinality**: +1 series per NIC per metric (negligible — typically 1–3 NICs)
- **Use case**: per-NIC anomaly detection (drop rate on one interface, INVITE flood on another), aggregation across IPs on the same NIC

**PromQL examples:**
```promql
# INVITE rate by interface
sum by (iface) (rate(sip_exporter_invite_total[5m]))

# Drop rate on a specific NIC
rate(sip_exporter_socket_packets_dropped_total{iface="ens3"}[5m])
```

### Direction Label

The `direction` label classifies traffic as `inbound` or `outbound` from the server's perspective. It is determined automatically from the kernel's `pkttype` field — **zero configuration required**.

**How it works:**

The Linux kernel assigns each packet a `pkttype` when delivering it to an AF_PACKET socket:

| pkttype | Meaning | Request direction | Response direction |
|---------|---------|-------------------|--------------------|
| `PACKET_HOST` (0) | Packet arriving at our interface (RX) | `inbound` | `outbound` |
| `PACKET_OUTGOING` (4) | Packet sent by us (TX) | `outbound` | `inbound` |

For responses, the direction is inverted: a response arriving at our interface means it's a response to our outbound request, and vice versa. INVITE responses inherit the direction from the original INVITE via the invite tracker, ensuring consistency within a call.

**Semantics:**

- `inbound` — someone is calling us (incoming calls, registrations from devices, OPTIONS from peers)
- `outbound` — we are calling out (outgoing calls, outgoing registrations, outgoing OPTIONS)

All packets within the same call carry the same `direction` value.

**Requirements:**

- `SIP_EXPORTER_IGNORE_OUTGOING=false` (default) — both RX and TX packets must be captured. With `IgnoreOutgoing=true`, TX packets are filtered and `direction` collapses to `inbound` for all calls (only responses appear as `outbound` due to inversion).
- On loopback (`lo`), `IgnoreOutgoing=true` is typically used to prevent packet duplication, which means `direction` cannot distinguish inbound/outbound. For meaningful direction analysis, use a physical NIC or veth pair.

**PromQL examples:**
```promql
# ASR separately for inbound vs outbound calls
sum by (direction) (sip_exporter_asr)

# INVITE rate by direction
sum by (direction) (rate(sip_exporter_invite_total[5m]))

# Inbound call failure rate
sum(rate(sip_exporter_480_total{direction="inbound"}[5m]))
  / sum(rate(sip_exporter_invite_total{direction="inbound"}[5m]))
```

**Cardinality impact:** +1 label × 2 values = 2× combinations. Typical: 5 carriers × 3 UA types × 10 countries × 2 directions = 300 series per metric.

### SER by Destination (PromQL)

Ratio metrics (SER, SEER, ISA, SCR) carry `source_country` but **not** `destination_country` (cardinality control). To calculate SER for a specific destination, use PromQL on the raw INVITE counters:

```promql
# SER for calls to Russia = successful / total INVITE
# (approximation: 3xx not excluded — see note below)
sum(rate(sip_exporter_invite_200_total{destination_country="RU"}[5m]))
  / sum(rate(sip_exporter_invite_total{destination_country="RU"}[5m])) * 100

# INVITE rate by destination country
sum by (destination_country) (rate(sip_exporter_invite_total[5m]))

# Top 10 destination countries by call volume
topk(10, sum by (destination_country) (rate(sip_exporter_invite_total[5m])))
```

> **Why no 3xx exclusion?** Strict SER (per the formula in [Session Establishment Ratio (SER)](#session-establishment-ratio-ser)) subtracts 3xx responses from the denominator. But `invite_3xx_total` does **not** carry `destination_country`, so redirects cannot be partitioned by destination in PromQL. The approximation above (200 OK / total INVITE) is used instead; for most deployments the 3xx share is small and the difference is negligible.

---

`sip_exporter_packets_total`: total number of parsed SIP packets (requests + responses). **No `carrier` or `ua_type` label.**

## Active sessions

`sip_exporter_sessions{carrier="...",ua_type="..."}`: number of active SIP dialogs (RFC 3261).

> Note: Actual metric includes `source_country` and `direction` labels. See [Labels](#labels) table for the full label set per metric tier.

**How dialogs are counted:**
- A dialog is created when a `200 OK` response is received for an `INVITE` request
- A dialog is identified by the tuple: `{Call-ID, From tag, To tag}`
- A dialog is terminated when a `200 OK` response is received for a `BYE` request
- Dialog ID format: `{call-id}:{min-tag}:{max-tag}` (tags sorted lexicographically)
- A re-INVITE (INVITE within an existing dialog) **refreshes** the dialog's Session-Expires timer without creating a new dialog or incrementing `invite_total`
- Dialogs are cleaned up when:
  - `200 OK` received for `BYE` request (normal termination)
  - Session-Expires timeout reached (RFC 4028)
- Default timeout: 1800 seconds (30 min) if `Session-Expires` header not present
- Cleanup runs every 1 second

## SIP request metrics

`sip_exporter_invite_total{carrier="...",ua_type="...",...,iface="ens3"}`: total number of received SIP INVITE requests. Re-INVITEs (INVITE within an existing dialog) are **excluded** — see `reinvite_total` below. Carries the `iface` label (network interface name, e.g. `ens3`, `tun0`).
`sip_exporter_reinvite_total{carrier="...",ua_type="..."}`: total number of re-INVITE requests (INVITE sent within an already established dialog, RFC 3261 §14). Re-INVITEs refresh the dialog's Session-Expires timer without creating a new dialog or contaminating SER/SCR/ASR ratios.  
`sip_exporter_register_total{carrier="...",ua_type="..."}`: total number of received SIP REGISTER requests.  
`sip_exporter_options_total{carrier="...",ua_type="..."}`: total number of received SIP OPTIONS requests.  
`sip_exporter_cancel_total{carrier="...",ua_type="..."}`: total number of received SIP CANCEL requests.  
`sip_exporter_bye_total{carrier="...",ua_type="..."}`: total number of received SIP BYE requests.  
`sip_exporter_ack_total{carrier="...",ua_type="..."}`: total number of received SIP ACK requests.  
`sip_exporter_publish_total{carrier="...",ua_type="..."}`: total number of received SIP PUBLISH requests.  
`sip_exporter_prack_total{carrier="...",ua_type="..."}`: total number of received SIP PRACK requests.  
`sip_exporter_notify_total{carrier="...",ua_type="..."}`: total number of received SIP NOTIFY requests.  
`sip_exporter_subscribe_total{carrier="...",ua_type="..."}`: total number of received SIP SUBSCRIBE requests.  
`sip_exporter_refer_total{carrier="...",ua_type="..."}`: total number of received SIP REFER requests.  
`sip_exporter_info_total{carrier="...",ua_type="..."}`: total number of received SIP INFO requests.  
`sip_exporter_update_total{carrier="...",ua_type="..."}`: total number of received SIP UPDATE requests.  
`sip_exporter_message_total{carrier="...",ua_type="..."}`: total number of received SIP MESSAGE requests.

`sip_exporter_sip_retransmission_total{carrier="...",ua_type="...",direction="...",method="INVITE"}` *(counter)*: total number of retransmitted SIP requests detected via Timer A (RFC 3261 §17.1.1.2). A retransmission is identified when a duplicate INVITE with the same Call-ID arrives within the invite tracker TTL window (60s) without an active dialog. Currently INVITE-only; the `method` label is reserved for future generalization to REGISTER/OPTIONS.

`sip_exporter_invite_200_total{carrier,ua_type,source_country,destination_country,caller_host,called_host,iface}`: total number of `200 OK` responses to INVITE requests (successful call establishments). This is the numerator for [SER-by-destination](#ser-by-destination-promql) PromQL calculations. Carries the full 8-label set — same as `invite_total`, including `iface`.

`sip_exporter_invite_3xx_total{carrier,ua_type,source_country,direction}`: total number of accepted first-final `3xx` responses to initial INVITE transactions. Retransmitted/forked final responses, re-INVITEs, and responses to other SIP methods are excluded. Use this counter with `invite_total` and `invite_200_total` to calculate windowed SER with the same redirect semantics as `sip_exporter_ser`.

## SIP response metrics (by status code)

`sip_exporter_100_total{carrier="...",ua_type="..."}`: total number of SIP 100 Trying responses.  
`sip_exporter_180_total{carrier="...",ua_type="..."}`: total number of SIP 180 Ringing responses.  
`sip_exporter_181_total{carrier="...",ua_type="..."}`: total number of SIP 181 Call Is Being Forwarded responses.  
`sip_exporter_182_total{carrier="...",ua_type="..."}`: total number of SIP 182 Queued responses.  
`sip_exporter_183_total{carrier="...",ua_type="..."}`: total number of SIP 183 Session Progress responses.  
`sip_exporter_200_total{carrier="...",ua_type="..."}`: total number of SIP 200 OK responses.  
`sip_exporter_202_total{carrier="...",ua_type="..."}`: total number of SIP 202 Accepted responses.  
`sip_exporter_300_total{carrier="...",ua_type="..."}`: total number of SIP 300 Multiple Choices responses.  
`sip_exporter_302_total{carrier="...",ua_type="..."}`: total number of SIP 302 Moved Temporarily responses.  
`sip_exporter_400_total{carrier="...",ua_type="..."}`: total number of SIP 400 Bad Request responses.  
`sip_exporter_401_total{carrier="...",ua_type="..."}`: total number of SIP 401 Unauthorized responses.  
`sip_exporter_403_total{carrier="...",ua_type="..."}`: total number of SIP 403 Forbidden responses.  
`sip_exporter_404_total{carrier="...",ua_type="..."}`: total number of SIP 404 Not Found responses.  
`sip_exporter_405_total{carrier="...",ua_type="..."}`: total number of SIP 405 Method Not Allowed responses.  
`sip_exporter_proxy_authentication_required_total{carrier="...",ua_type="..."}`: total number of SIP 407 Proxy Authentication Required responses.  
`sip_exporter_408_total{carrier="...",ua_type="..."}`: total number of SIP 408 Request Timeout responses.  
`sip_exporter_480_total{carrier="...",ua_type="..."}`: total number of SIP 480 Temporarily Unavailable responses.  
`sip_exporter_481_total{carrier="...",ua_type="..."}`: total number of SIP 481 Dialog/Transaction Does Not Exist responses.  
`sip_exporter_486_total{carrier="...",ua_type="..."}`: total number of SIP 486 Busy Here responses.  
`sip_exporter_487_total{carrier="...",ua_type="..."}`: total number of SIP 487 Request Terminated responses.  
`sip_exporter_488_total{carrier="...",ua_type="..."}`: total number of SIP 488 Not Acceptable Here responses.  
`sip_exporter_500_total{carrier="...",ua_type="..."}`: total number of SIP 500 Server Internal Error responses.  
`sip_exporter_501_total{carrier="...",ua_type="..."}`: total number of SIP 501 Not Implemented responses.  
`sip_exporter_502_total{carrier="...",ua_type="..."}`: total number of SIP 502 Bad Gateway responses.  
`sip_exporter_503_total{carrier="...",ua_type="..."}`: total number of SIP 503 Service Unavailable responses.  
`sip_exporter_504_total{carrier="...",ua_type="..."}`: total number of SIP 504 Server Time-out responses.  
`sip_exporter_600_total{carrier="...",ua_type="..."}`: total number of SIP 600 Busy Everywhere responses.  
`sip_exporter_603_total{carrier="...",ua_type="..."}`: total number of SIP 603 Decline responses.  
`sip_exporter_604_total{carrier="...",ua_type="..."}`: total number of SIP 604 Does Not Exist Anywhere responses.  
`sip_exporter_606_total{carrier="...",ua_type="..."}`: total number of SIP 606 Not Acceptable responses.  

## Registration Health

Registration metrics track the full lifecycle of SIP registrations (RFC 3261 §10): success/failure outcomes, a computed success ratio, and the count of currently active registrations. All are scoped per `carrier,ua_type,source_country,direction`.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `sip_exporter_register_success_total` | counter | `carrier,ua_type,source_country,direction` | REGISTER responses with status `200 OK` |
| `sip_exporter_register_failure_total` | counter | `carrier,ua_type,source_country,direction,code` | REGISTER responses with status `3xx/4xx/5xx/6xx`, by code |
| `sip_exporter_register_success_ratio` | gauge | `carrier,ua_type,source_country,direction` | `200 OK / (200 OK + terminal failures) × 100` |
| `sip_exporter_active_registrations` | gauge | `carrier,ua_type,source_country,direction` | Currently active registrations (Expires-TTL tracked) |

### register_success_ratio

```
register_success_ratio = (REGISTER → 200 OK) / (REGISTER → 200 OK + terminal failures) × 100
```

- **Terminal failures** = non-200 responses **excluding** `401 Unauthorized` and `407 Proxy Authentication Required` (these are digest-auth challenges — a normal part of the registration handshake, not genuine failures) and `3xx` redirects.
- Excluding challenges keeps the ratio meaningful on systems using SIP digest authentication: a healthy auth flow (`REGISTER → 401 → REGISTER+creds → 200 OK`) yields a ratio near 100%, not ~50%.
- Undefined (emits `0`) when no successful or terminal-failed registrations have been observed.

> **Note:** `register_failure_total{code}` counts **all** non-1xx/non-2xx responses including `401`/`407`. The sum across codes therefore exceeds the failure count used in the ratio denominator. Use `register_failure_total{code="401"}` for brute-force detection (see [ALERTING.md](ALERTING.md)), and `register_success_ratio` for overall registration health.

### active_registrations

- Incremented on each `REGISTER → 200 OK`, keyed by the Address-of-Record (`user@host` parsed from the `From` URI).
- Each entry has a TTL from the `Expires` header (RFC 3261 §20.19); default **3600 s** when absent.
- A **refresh** (same AOR, new `200 OK`) updates the TTL — it does **not** create a duplicate.
- Entries are removed by a background cleanup (every 1 s) once their TTL expires; the gauge is updated accordingly.

**PromQL examples:**
```promql
# Registration success ratio per carrier
sip_exporter_register_success_ratio

# Active registrations over time (rate of churn)
rate(sip_exporter_register_success_total[5m])

# Top failing status codes
topk(5, sum by (code) (rate(sip_exporter_register_failure_total[5m])))
```

## Fraud Detection

Fraud signals detect suspicious patterns: registration scanning (one IP registering many accounts), geographic impossibility (same account from different countries), INVITE flooding (one IP sending a burst of calls), and False Answer Supervision (answered calls that never carry media). The signaling heuristics are scoped per `carrier,source_country,direction` — `ua_type` is intentionally omitted because attackers vary their User-Agent. **FAS is the exception**: it is a call-level signal measured at the 200 OK, where the answering endpoint's `ua_type` is meaningful, so it carries the full `carrier,ua_type,source_country,direction` set.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `sip_exporter_register_country_change_total` | counter | `carrier,source_country,direction` | Times a user re-registered from a different country (account-takeover signal) |
| `sip_exporter_register_scan_total` | counter | `carrier,source_country,direction` | Registration scan signals (one IP registering N+ unique AORs within a time window) |
| `sip_exporter_invite_burst_total` | counter | `carrier,source_country,direction` | INVITE burst signals (one IP sending N+ INVITEs within a time window) |
| `sip_exporter_fas_calls_total` | counter | `carrier,ua_type,source_country,direction` | Suspected False Answer Supervision: 200 OK on a media-bearing dialog with no RTP within the threshold |

### register_country_change_total

Incremented when a successful REGISTER 200 OK arrives from a **different source country** than the previous registration for the same AOR (Address-of-Record). The AOR is derived from the `From` header URI.

- First registration of an AOR does **not** trigger a signal (no baseline)
- Refresh from the same country does **not** trigger a signal
- Empty previous country (GeoIP disabled at first registration) does **not** trigger a signal

```promql
# Account-takeover spikes
sip_exporter_register_country_change_total > 0 unless on (carrier, source_country) (sip_exporter_register_country_change_total offset 5m > 0)
```

### register_scan_total

Incremented for each registration event from a single source IP when the count of unique AORs (Address of Record) within the configured `window` reaches or exceeds `threshold`. The counter increases continuously during a scan attack, making `rate()` effective for alerting.

| Config | Env var | Default |
|--------|---------|---------|
| Threshold | `SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD` | `10` |
| Window | `SIP_EXPORTER_FRAUD_REGISTER_SCAN_WINDOW` | `60s` |

```promql
# Registration enumeration attacks
rate(sip_exporter_register_scan_total[5m])
```

### invite_burst_total

Incremented for each **INVITE request** (excluding re-INVITEs) from a single source IP when the count of INVITEs within the configured `window` is at or above `threshold`. The counter increases continuously during a burst, making `rate()` effective for alerting.

| Config | Env var | Default |
|--------|---------|---------|
| Threshold | `SIP_EXPORTER_FRAUD_INVITE_BURST_THRESHOLD` | `100` |
| Window | `SIP_EXPORTER_FRAUD_INVITE_BURST_WINDOW` | `60s` |

```promql
# Toll-fraud or DDoS via INVITE flood
rate(sip_exporter_invite_burst_total[5m])
```

### fas_calls_total

Incremented when a call is **answered** (non-re-INVITE 200 OK on a dialog that registered media endpoints from SDP) but **no RTP is observed within the threshold window** — a real-time False Answer Supervision signal. The answering side starts billing without delivering media.

This is distinct from `sessions_missing_rtp_total` (a **teardown** metric, evaluated at BYE/expiry and may arrive much later): FAS is an **early, real-time** signal `threshold` seconds after answer.

- Pending entry is created at the 200 OK (only when SDP registered ≥1 media endpoint; held SDP `c=0.0.0.0` is excluded — no media is expected).
- It is cleared once **≥2 forward RTP packets** reach the dialog's media endpoint (a single stray/spoofed packet must not mask a media-less call).
- It is also cleared at dialog teardown (BYE / Session-Expires expiry), so a short call that never had media is not misreported.

| Config | Env var | Default |
|--------|---------|---------|
| Threshold | `SIP_EXPORTER_FRAUD_FAS_THRESHOLD` | `10s` |

```promql
# False Answer Supervision — answered calls that never carried media
rate(sip_exporter_fas_calls_total[5m])
```

#### FAS limitations

This is a **signaling-only heuristic**. True FAS detection requires decoding the audio stream and recognizing ringing/two-tone patterns in the "answered" media (e.g. Europe/UK ringing tones) — out of scope for a Prometheus exporter (won't fix). Known limitations:

- **Adversarial defeat is inherent.** A party that controls the answering endpoint can send a short RTP burst (≥2 packets) to cancel the signal. The ≥2-packet gate only raises the bar against accidental/coincidental clears, not a determined adversary.
- **Side-gated clearing (answer-side media required).** FAS is cleared only when media from the *answering* side is observed (it arrives at the offer/caller endpoint). Media from the *calling* side does **not** defeat FAS — a fraudster's false 200 OK can no longer be masked by the victim's own upstream RTP. This requires the INVITE offer SDP to have been cached; if the INVITE was not seen (exporter started mid-call, late offer), the originating side cannot be determined and FAS falls back to any-media-clears. Under NAT that remaps the answering side's source port, side detection degrades gracefully to that same fallback.
- **DTLS-SRTP grace (sweep path only).** When the answer SDP carries `a=fingerprint` (RFC 4572) or `a=crypto:` (RFC 4568 SDES), the sweep deadline receives a per-call grace (`fasSRTPGrace`, 15 s) on top of the base threshold, so ICE/DTLS setup does not false-positive on WebRTC-trunked calls. The BYE path uses `fasByeFloor` (3 s) regardless of SRTP — a fake `a=fingerprint` must not let a fraudster evade detection by hanging up within the grace window. Tradeoff: a WebRTC call that fails ICE and hangs up after 3 s may false-positive.
- **Short dead-air calls reported at BYE.** A call that answered with no answer-side RTP and ends via BYE before the sweep threshold is reported at teardown when answer→BYE ≥ `fasByeFloor` (3 s), covering the common FAS pattern where the caller hangs up on dead air. Shorter calls (immediate abandonment) are excluded.
- **Re-INVITE un-hold blind spot.** If the initial 200 OK carried held SDP (`c=0.0.0.0`, no pending entry created) and a later re-INVITE offers real media, the call is never FAS-tracked. Rare (hold→unhold without any prior RTP).
- **No audio-content analysis.** A fraudster streaming silence or comfort noise passes the media-established check.
- **RTP capture dependency.** FAS reliability equals RTP capture completeness. SIP packets use a blocking channel send (never dropped); RTP packets use a non-blocking send and are dropped when the channel is full (`rtp_dropped_total`). Correlate `rate(sip_exporter_rtp_dropped_total[5m])` with `fas_calls_total` before alerting — a spike in drops during high traffic can cause false FAS positives by losing the answer-side RTP.
- **One-way media false positives.** Servers that never send answer-side RTP (voicemail, IVR, paging, announcement playback) will trigger FAS by design. Tune the threshold or exclude these endpoints via alerting rules.

## Capacity Monitoring

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `sip_exporter_sessions_limit` | gauge | `carrier` | Configured concurrent session limit per carrier (from YAML config) |
| `sip_exporter_sessions_utilization` | gauge | `carrier` | Session utilization as percentage of configured limit (0–100, capped) |

### Configuration

Create a YAML file and set its path via `SIP_EXPORTER_SESSIONS_LIMITS`:

```yaml
sessions_limits:
  - carrier: "beeline"
    limit: 500
  - carrier: "mts"
    limit: 1000
```

- Utilization is computed on every scrape: `active_sessions(carrier) / limit × 100`
- Capped at 100 (over-limit shown as 100, not >100) — this hides severity of oversubscription; use the raw `sip_exporter_sessions` gauge to detect extreme overage
- Carriers without a configured limit are omitted (gauge not emitted)
- Carriers with `limit: 0` are also omitted (treated as "no limit", not "0% / blocked")
- Carriers with 0 active sessions show 0% utilization

```promql
# Capacity headroom per carrier
100 - sip_exporter_sessions_utilization

# Carriers at or near capacity
sip_exporter_sessions_utilization > 90
```

## Short call counters

`sip_exporter_short_calls_total{carrier="...",ua_type="...",direction="...",threshold="20|60|180"}` *(counter)*: completed sessions with duration shorter than the threshold (20, 60, or 180 seconds). A single session can increment multiple thresholds (e.g., a 15-second call increments `threshold="20"`, `"60"`, and `"180"`). Short calls indicate abandoned calls, poor quality, or potential toll fraud.

**PromQL examples:**
```promql
# Short call rate (< 20s) as percentage of completed sessions
rate(sip_exporter_short_calls_total{threshold="20"}[5m]) / rate(sip_exporter_sdc_total[5m]) * 100

# Absolute count of sub-60s calls per carrier
sum by (carrier) (rate(sip_exporter_short_calls_total{threshold="60"}[1h]))
```

## Traffic Minutes by Destination

`sip_exporter_billable_seconds_total{carrier="...",destination_country="..."}` *(counter)*: total seconds of completed sessions by destination country, accumulated at session teardown (BYE 200 OK or Session-Expires expiry). Only sessions with `Duration > 0` contribute; clock-skew dialogs (instant teardown) are skipped. `destination_country` is resolved from the called number (E.164 prefix matching) at INVITE 200 OK and stored for the lifetime of the dialog.

**Cardinality:** ~50 carriers × ~250 countries ≈ 12.5K series (same tier as INVITE-raw).

**PromQL examples:**
```promql
# Traffic minutes/min by destination (top 10)
topk(10, sum by (destination_country) (rate(sip_exporter_billable_seconds_total[5m]) / 60))

# ACD (average call duration) by destination in minutes
sum by (destination_country) (rate(sip_exporter_billable_seconds_total[15m])) / 60
  / clamp_min(sum by (destination_country) (rate(sip_exporter_invite_200_total[15m])), 1)

# Traffic minutes per carrier per hour
sum by (carrier) (increase(sip_exporter_billable_seconds_total[1h])) / 3600
```

## RTP media metrics

RTP media metrics are derived from RTP packets captured by the eBPF filter and
correlated with SIP dialogs via SDP (media IP:port → carrier/ua_type/call-id).
RTP stream metrics carry the `carrier`, `ua_type`, `codec`, `source_country`, and `direction` labels.
Only RTP that
belongs to an established SIP dialog (after a 200 OK to INVITE with SDP) is
counted; RTP without a correlated dialog is dropped.

> **Measurement semantics:** RTP loss, jitter, PDV, and E-model MOS describe
> packets observed at this capture point. Sequence gaps and arrival variation
> can indicate a transport problem upstream of the sensor, but they are not a
> subscriber's subjective score or proof of end-to-end impairment. Treat a
> rising `sip_exporter_socket_packets_dropped_total` or
> `sip_exporter_rtp_dropped_total` rate as a data-quality failure before using
> these metrics for QoE alerting.

> **RTP capture (SDP-driven mode):** The eBPF filter passes UDP only from
> endpoints learned via SDP signaling. Media endpoints (IP:port) from INVITE
> 200 OK SDP are inserted into a BPF LRU hash map (`rtp_endpoints`, 65536
> entries). UDP packets matching a registered endpoint (destination or source)
> pass the filter; all other UDP is dropped at the kernel level.
>
> Entries are removed on BYE 200 OK or Session-Expires cleanup. On restart the
> map starts empty — RTP is not captured until new SDP is learned from
> subsequent calls.

`{carrier="...",ua_type="...",codec="...",source_country="..."}` — `codec` is the RTP payload-type name resolved from SDP `a=rtpmap` (e.g. `PCMU`, `PCMA`, `opus`) with a static fallback table (RFC 3551). `source_country` is inherited from the SIP dialog (resolved at INVITE time).

`sip_exporter_rtp_packets_total{carrier,ua_type,codec,source_country,direction}` *(counter)*: total number of RTP packets observed.

`sip_exporter_rtp_packets_lost_total{carrier,ua_type,codec,source_country,direction}` *(counter)*: packets detected as lost via RTP sequence-number gaps.

`sip_exporter_rtp_duplicate_packets_total{carrier,ua_type,codec,source_country,direction}` *(counter)*: duplicate RTP packets detected (same sequence number as the previous packet, indicating retransmission or media loop).

`sip_exporter_rtp_out_of_order_total{carrier,ua_type,codec,source_country,direction}` *(counter)*: out-of-order RTP packets detected (sequence number less than maxSeq, not a duplicate). High values indicate network reordering that can overwhelm jitter buffers.

`sip_exporter_rtp_jitter_milliseconds{carrier,ua_type,codec,source_country,direction}` *(histogram, buckets 0.1..500 ms)*: smoothed interarrival jitter (RFC 3550 A.8).

`sip_exporter_rtp_pdv_milliseconds{carrier,ua_type,codec,source_country,direction}` *(histogram, buckets 1..500 ms)*: Packet Delay Variation — the **raw** per-packet deviation `|arrivalDelta − tsDelta|` (unsmoothed), **observed per RTP packet** (parity with VoIPMonitor, which buckets each packet's deviation from the expected 20 ms spacing). Unlike `rtp_jitter_milliseconds` (an EWMA that smooths over spikes), PDV is the instantaneous per-packet deviation, so its histogram captures the true delay-variation distribution including transient spikes. Only forward (counted) packets contribute; reorder/duplicate do not (their timestamp delta is not a forward delta). The first packet of each stream (and after a stream restart) is skipped — it has no baseline to compute a delta. The arrival timestamp is the kernel `SO_TIMESTAMPNS` receive time (captured on the AF_PACKET socket), so Go scheduler/GC processing delay does not affect the measurement; if the timestamp is absent, `rtp_kernel_timestamp_missing_total` is incremented. Because the formula uses consecutive-packet deltas `(Rj−Ri)−(Sj−Si)` (the same drift-cancelling form as RFC 3550 jitter), it is immune to sender/receiver clock drift. The `direction` label reflects the SIP dialog direction (inbound/outbound), not the media direction — both forward and reverse streams of a call are aggregated into the same histogram (asymmetric delay is averaged out).

`sip_exporter_rtp_mos_score{carrier,ua_type,codec,source_country,direction}` *(histogram, buckets 1.0..5.0)*: MOS-LQ estimated via the ITU-T G.107 E-model with a 60 ms jitter buffer assumption.

`sip_exporter_rtp_mos_f1{carrier,ua_type,codec,source_country,direction}` *(histogram, buckets 1.0..5.0)*: MOS-LQ with a strict jitter buffer (50 ms) — models low-latency endpoints that tolerate less jitter.

`sip_exporter_rtp_mos_f2{carrier,ua_type,codec,source_country,direction}` *(histogram, buckets 1.0..5.0)*: MOS-LQ with a generous jitter buffer (200 ms) — models managed endpoints with deeper buffers.

`sip_exporter_rtp_mos_adaptive{carrier,ua_type,codec,source_country,direction}` *(histogram, buckets 1.0..5.0)*: MOS-LQ with an adaptive jitter buffer (500 ms) — models adaptive endpoints that absorb significant jitter.

`sip_exporter_rtp_r_factor{carrier,ua_type,codec,source_country,direction}` *(histogram, buckets 10..100)*: E-model R-factor (ITU-T G.107), range 0–100. Underlying quality score before the R→MOS transform.

`sip_exporter_rtp_burst_loss_density{carrier,ua_type,codec,source_country,direction}` *(histogram, buckets 10..100)*: percentage of lost packets that occurred in burst runs (≥ 3 consecutive losses), range 0–100.

`sip_exporter_rtp_gap_loss_density{carrier,ua_type,codec,source_country,direction}` *(histogram, buckets 10..100)*: percentage of lost packets that occurred in isolated gaps (< 3 consecutive losses), range 0–100.

`sip_exporter_rtp_active_streams{carrier,ua_type,codec,source_country,direction}` *(gauge)*: number of active RTP streams. Sampled once per second; idle streams expire after 30 s.

`sip_exporter_rtp_endpoint_mismatch_total{carrier,direction,type}` *(counter)*: source-port aliases
learned for symmetric RTP. `type` is currently always `source_port`; the counter increments once for
each successfully learned alias and never exposes an observed IP address or port as a label.

`sip_exporter_rtp_alias_active{carrier,direction}` *(gauge)*: active learned source-port aliases.
It increases when an alias is learned and decreases when its dialog media revision is replaced,
the call ends, or the dialog expires.

> MOS and R-factor are sampled per stream once per second; the E-model uses G.113 codec Ie/Bpl
> factors. Unknown codecs get a conservative default (Ie=10). Burst/gap density uses a simplified
> heuristic inspired by RFC 3611: consecutive loss runs of ≥ 3 packets are classified as burst,
> shorter runs as gap.

> **Correlation limitation:** RTP first matches an SDP-advertised endpoint by destination,
> then by source fallback. A destination-correlated packet may learn one source-port alias for
> its SDP peer only when its source IP matches that peer and ownership is unambiguous. This supports
> symmetric RTP/NAT source-port remapping, but never learns a source-IP change, ICE/TURN endpoint
> changes, or arbitrary UDP traffic. Learned aliases are scoped to the dialog media revision and are
> removed on re-INVITE replacement, BYE, or expiry; SIP signaling metrics are unaffected by a
> rejected alias.

### RTP Dialog Quality Metrics

These counters are evaluated at dialog teardown (BYE 200 OK or Session-Expires expiry) and carry `carrier, ua_type, source_country, direction` (no `codec` label — they describe the dialog, not a single stream).

`sip_exporter_rtp_oneway_calls_total{carrier,ua_type,source_country}` *(counter)*: dialogs where 2+ media endpoints were registered (SDP from both parties) but RTP was observed in only one direction.

`sip_exporter_sessions_missing_rtp_total{carrier,ua_type,source_country}` *(counter)*: dialogs with SDP media endpoints but no RTP observed at all.

> Both metrics rely on a persistent per-dialog RTP record that survives stream TTL expiry,
> ensuring accurate detection even when RTP streams were cleaned up before dialog teardown.

## RTCP Endpoint-Reported Metrics

RTCP metrics are derived from RTCP Sender/Receiver Reports (RFC 3550 §6) captured by the eBPF filter and correlated to RTP streams by SSRC. They report the **receiver's RTP statistics** for that stream, complementing passive RTP metrics observed at the capture point. They are not a subjective call-quality score and exist only when the endpoint, SBC, or mixer emits SR/RR that the sensor can see; do not assume every deployment produces them.

> **Capture:** RTCP shares V=2 and (for rtcp-mux, RFC 5761) the RTP port with RTP; the eBPF filter distinguishes them by the 8-bit packet-type byte (200–204 for RTCP) and passes the **full** RTCP compound to userspace (RTP keeps a 64-byte header-only snapshot). rtcp-mux works with no extra configuration. Non-mux RTCP on a separate port is captured when the SDP declares it via `a=rtcp` (RFC 3605) — both the port and an optional unicast address (`a=rtcp:<port> IN IP4 <addr>`, RTCP on a host other than `c=`) are honoured (IPv4 only); absent both `a=rtcp-mux` and `a=rtcp`, legacy RTCP on `port+1` (RFC 3550 §9) is registered automatically.

All quality metrics inherit the labels of the correlated RTP stream: `carrier, ua_type, codec, source_country, direction` (identical to the RTP tier — `direction` is `inbound`/`outbound`). An RTCP report block whose SSRC does not match a tracked RTP stream is dropped, consistent with RTP correlation.

`{carrier="...",ua_type="...",codec="...",source_country="...",direction="..."}`

`sip_exporter_rtcp_jitter_milliseconds{carrier,ua_type,codec,source_country,direction}` *(histogram, buckets 0.1..500 ms)*: interarrival jitter reported by the receiver (RR block), converted from RTP timestamp units to milliseconds via the stream's clock rate.

`sip_exporter_rtcp_loss_fraction_percent{carrier,ua_type,codec,source_country,direction}` *(histogram, buckets 0..100)*: fraction of RTP packets lost since the previous RR, as a percent (0–100). The RR field is an 8-bit ratio (N/256).

`sip_exporter_rtcp_cumulative_loss_total{carrier,ua_type,codec,source_country,direction}` *(counter)*: cumulative packets lost reported by the receiver. The exporter diffs consecutive reports per SSRC: the **first** report for an SSRC establishes the baseline and emits no delta (so the counter reflects only losses observed since the exporter began tracking the stream, not the absolute cumulative total the endpoint carries), and a session reset or 24-bit wrap yields the current value as the delta. The counter is monotonic and `rate()`-able.

`sip_exporter_rtcp_rtt_milliseconds{carrier,ua_type,codec,source_country,direction}` *(histogram, buckets 1..5000 ms)*: network round-trip time computed from the RR's LSR/DLSR fields: `RTT = (now_NTP32 − LSR − DLSR) × 1000 / 65536` (RFC 3550 §6.4.1). Skipped when `LSR` or `DLSR` is zero (no prior SR) or the result is negative (clock skew). Requires the host clock to be roughly NTP-synchronised.

`sip_exporter_rtcp_reports_total{carrier,ua_type,source_country,direction,type}` *(counter)*: number of RTCP reception report blocks processed for tracked streams. `type` is `sr` (Sender Report, PT 200) or `rr` (Receiver Report, PT 201). Note: no `codec` label (a report describes reception, not a single codec); counted per report block.

`sip_exporter_rtcp_orphan_reports_total` *(counter, label-less)*: RTCP reception report blocks whose SSRC could not be correlated to a tracked RTP stream (the SSRC is unknown, or two streams collide on the SSRC and the report's endpoints match neither). A non-zero rate indicates SDP/SSRC registration gaps or RTCP arriving for calls the exporter does not track. Label-less because the stream — and thus its carrier/codec context — is unknown.

> **RTT vs everything else:** RTT has no passive-RTP equivalent — a single sniffer point cannot measure end-to-end RTT from RTP alone. RTCP-derived RTT is the only source of network round-trip latency in sip-exporter. Use it to investigate routing, queuing, and conversational delay; it does not by itself diagnose acoustic echo.

> **Correlation by SSRC:** a report block is matched to the RTP stream sending with its SSRC (destination-first, NAT-robust). On rare SSRC collision (two streams reuse an SSRC within the TTL window) the endpoint hints disambiguate; the loss delta and the labels always attribute to the same stream.

## System metrics

`sip_exporter_system_error_total`: total number internal SIP exporter errors. **No `carrier` or `ua_type` label.**

## Self-Monitoring Metrics

Self-monitoring metrics provide visibility into the exporter's internal health. All self-monitoring metrics have **no `carrier` or `ua_type` label** — they measure the exporter itself, not SIP traffic.

| Metric | Type | Description |
|--------|------|-------------|
| `sip_exporter_socket_packets_received_total{iface}` | CounterVec | Total packets received from kernel AF_PACKET socket per interface |
| `sip_exporter_socket_packets_dropped_total{iface}` | CounterVec | Total packets dropped by kernel due to socket receive buffer overflow per interface |
| `sip_exporter_rtp_dropped_total` | Counter | Total RTP packets dropped in userspace when the internal messages channel is full |
| `sip_exporter_rtp_kernel_timestamp_missing_total` | Counter | RTP packets where the kernel `SO_TIMESTAMPNS` was absent and PDV fell back to processing time (a growing rate means unreliable PDV readings) |
| `sip_exporter_channel_length` | Gauge | Current number of packets in the internal messages channel buffer |
| `sip_exporter_channel_capacity` | Gauge | Capacity of the internal messages channel buffer (constant: 10000) |
| `sip_exporter_parse_errors_total{type="..."}` | CounterVec | Total packet parse errors by type |
| `sip_exporter_active_trackers{type="..."}` | GaugeVec | Current number of entries in tracker maps |
| `sip_exporter_active_dialogs` | Gauge | Current number of active SIP dialogs |
| `sip_exporter_build_info{version="..."}` | GaugeFunc | Build information; constant `version` label, value always `1`. Useful for `count by (version) (sip_exporter_build_info)` inventory queries |

### AF_PACKET Socket Statistics

`sip_exporter_socket_packets_received_total` and `sip_exporter_socket_packets_dropped_total` are read from the kernel's `PACKET_STATISTICS` via `getsockopt()` every second. The kernel resets counters after each read, so values are accumulated in the exporter. Both carry an `iface` label (network interface name) for per-NIC visibility.

**PromQL examples:**
```promql
# Packet drop rate per interface (packets/sec)
rate(sip_exporter_socket_packets_dropped_total{iface="ens3"}[5m])

# Drop ratio across all interfaces (percentage of received packets dropped)
sum(rate(sip_exporter_socket_packets_dropped_total[5m]))
  / sum(rate(sip_exporter_socket_packets_received_total[5m])) * 100

# Packets received by interface
sum by (iface) (rate(sip_exporter_socket_packets_received_total[5m]))
```

### Parse Errors

`sip_exporter_parse_errors_total{type="..."}` counts parse failures by protocol layer:

| Type | Layer | Description |
|------|-------|-------------|
| `l2` | Ethernet | Packet too short for Ethernet/VLAN header |
| `l3` | IPv4 | Not IPv4 packet or IP header too short |
| `l4` | UDP | Not UDP packet or UDP header too short |
| `sip` | SIP | No SIP payload, packet too small, or unrecognized method |
| `vq` | Voice Quality | Failed to parse RFC 6035 VQ report body |
| `rtcp` | RTCP | Malformed/truncated RTCP compound; valid SR/RR prefix before the error is still salvaged |

**PromQL examples:**
```promql
# Parse error rate by type
sum by (type) (rate(sip_exporter_parse_errors_total[5m]))

# Total parse errors
sum(rate(sip_exporter_parse_errors_total[5m]))
```

### Channel Buffer

`sip_exporter_channel_length` shows how many packets are buffered in the internal channel between the socket reader and the SIP parser. If this approaches `channel_capacity` (10000), the exporter cannot keep up with packet arrival rate and may lose packets at the kernel level.

**PromQL examples:**
```promql
# Channel utilization (percentage)
sip_exporter_channel_length / sip_exporter_channel_capacity * 100

# Alert if channel is filling up
sip_exporter_channel_length / sip_exporter_channel_capacity > 0.8
```

### Active Trackers

`sip_exporter_active_trackers{type="register|invite|options|bye|rtp"}` shows the number of entries in each tracker map. The `register`/`invite`/`options`/`bye` trackers store timestamps for measuring round-trip delays (RRD, TTR, ORD, LRD, PBD) and are cleaned up after 60 seconds. The `rtp` tracker holds active RTP media streams (correlated with SIP dialogs) and expires idle streams after 30 seconds.

**PromQL examples:**
```promql
# Active trackers by type
sip_exporter_active_trackers

# High INVITE tracker count indicates many pending calls
sip_exporter_active_trackers{type="invite"}
```

### Active Dialogs

`sip_exporter_active_dialogs` shows the total number of active SIP dialogs (same value as `sum(sip_exporter_sessions)` but without label cardinality). Useful for quick health checks.

**PromQL examples:**
```promql
# Current active dialogs
sip_exporter_active_dialogs

# Alert: too many active dialogs
sip_exporter_active_dialogs > 10000
```

## RFC 6076 Performance Metrics

All RFC 6076 metrics are **scoped per carrier, ua_type, and source_country** — each ratio/histogram is computed independently for each label combination. This allows comparing SER, SEER, ISA, SCR, ASR, NER across carriers, device types, and source countries in a single Prometheus query.

**Example:**
```promql
# SER per carrier
sip_exporter_ser

# Compare SER across carriers for a specific device type
sip_exporter_ser{ua_type="yealink"}

# Compare SER across carriers
sip_exporter_ser{carrier="carrier-a"} - sip_exporter_ser{carrier="carrier-b"}
```

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
| PDD | — | Post Dial Delay |
| ASR | — | Answer Seizure Ratio (ITU-T E.411) |
| SDC | — | Session Duration Counter |
| NER | — | Network Effectiveness Ratio (GSMA IR.42) |
| ISS | — | Ineffective Session Severity |
| ORD | — | OPTIONS Response Delay |
| LRD | — | Location Registration Delay |

---

### Session Establishment Ratio (SER)

`sip_exporter_ser{carrier="...",ua_type="..."}`: percentage of successfully established sessions relative to total INVITE attempts.

**Formula (RFC 6076 §4.6):**
```
SER = (INVITE → 200 OK) / (Total INVITE - INVITE → 3xx) × 100
```

- **Re-INVITEs are excluded** from both numerator and denominator — they are not new session attempts and are tracked separately in `reinvite_total`
- 3xx responses (redirects) are **excluded from the denominator** — they are neither success nor failure, but a routing instruction
- A session is counted as established only when the originating UA receives `200 OK` for its INVITE
- Undefined when no INVITE requests have been received
- Undefined when all INVITEs received 3xx responses (denominator = 0)

**Important:** SER is a cumulative metric calculated over the entire runtime.

---

### Session Establishment Effectiveness Ratio (SEER)

`sip_exporter_seer{carrier="...",ua_type="..."}`: percentage of "effective" INVITE responses relative to total non-redirected INVITE attempts.

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

`sip_exporter_isa{carrier="...",ua_type="..."}`: percentage of INVITE requests that resulted in server error or timeout responses.

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

`sip_exporter_scr{carrier="...",ua_type="..."}`: percentage of INVITE sessions that were fully completed (established and terminated) relative to total INVITE attempts.

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

`sip_exporter_rrd{carrier="...",ua_type="..."}`: histogram of delays in milliseconds between sending a REGISTER request and receiving a 200 OK response.

**Formula (RFC 6076 §4.1):**
```
RRD = Time of 200 OK response - Time of REGISTER request
```

- Measures the round-trip time for SIP registration transactions
- Only successful registrations (200 OK responses) are measured
- Exposed as a Prometheus Histogram with buckets: `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` ms
- Use `histogram_quantile()` for percentile-based alerting

**PromQL examples:**
```promql
# 95th percentile registration delay (all carriers)
histogram_quantile(0.95, sum(rate(sip_exporter_rrd_bucket[5m])) by (le))

# 95th percentile registration delay (specific carrier and device type)
histogram_quantile(0.95, sum(rate(sip_exporter_rrd_bucket{carrier="carrier-a",ua_type="yealink"}[5m])) by (le))

# Average registration delay
rate(sip_exporter_rrd_sum[5m]) / rate(sip_exporter_rrd_count[5m])
```

**Important:** RRD measures registration latency, not call setup latency. Use SER/SEER for call establishment metrics.

**Example values:**
- `< 100 ms` — excellent registration performance (local network)
- `100-500 ms` — acceptable performance (typical WAN)
- `> 1000 ms` — potential issues (network congestion, server overload)

**Note:** Use `histogram_quantile()` or `rate(sum)/rate(count)` for averages — the histogram provides full distribution, not just an average.

---

### Session Process Duration (SPD)

`sip_exporter_spd{carrier="...",ua_type="..."}`: histogram of completed SIP session durations in seconds.

**Formula (RFC 6076 §4.5):**
```
SPD = Time of session end - Time of session start (200 OK to INVITE)
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
# 99th percentile session duration (all carriers)
histogram_quantile(0.99, sum(rate(sip_exporter_spd_bucket[5m])) by (le))

# 99th percentile session duration (specific carrier and device type)
histogram_quantile(0.99, sum(rate(sip_exporter_spd_bucket{carrier="carrier-a",ua_type="yealink"}[5m])) by (le))

# Average session duration
rate(sip_exporter_spd_sum[5m]) / rate(sip_exporter_spd_count[5m])
```

**Important:** SPD measures actual session duration, not setup latency. Use SER/SEER for establishment metrics.

**Example values:**
- `< 30 s` — short calls (IVR, voicemail)
- `180 s` — typical voice call (~3 minutes)
- `> 3600 s` — long-duration sessions (conferences, held calls)

**Note:** Use `histogram_quantile()` or `rate(sum)/rate(count)` for averages — the histogram provides full distribution, not just an average.

---

### Time to First Response (TTR)

`sip_exporter_ttr{carrier="...",ua_type="..."}`: histogram of delays in milliseconds between an INVITE request and the first provisional (1xx) response.

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
# 95th percentile time to first response (all carriers)
histogram_quantile(0.95, sum(rate(sip_exporter_ttr_bucket[5m])) by (le))

# 95th percentile time to first response (specific carrier and device type)
histogram_quantile(0.95, sum(rate(sip_exporter_ttr_bucket{carrier="carrier-a",ua_type="yealink"}[5m])) by (le))

# Average time to first response
rate(sip_exporter_ttr_sum[5m]) / rate(sip_exporter_ttr_count[5m])
```

**Example values:**
- `< 50 ms` — excellent server responsiveness
- `100-500 ms` — acceptable (typical for loaded servers)
- `> 1000 ms` — potential issues (server overload, network latency)

---

### Post Dial Delay (PDD)

`sip_exporter_pdd{carrier="...",ua_type="..."}`: histogram of delays in milliseconds between an INVITE request and the 180 Ringing response.

**Formula:**
```
PDD = Time of 180 Ringing response - Time of INVITE request
```

- Measures the time from INVITE to the **180 Ringing** response specifically (not other 1xx responses)
- 100 Trying and 183 Session Progress do **not** trigger PDD measurement
- If no 180 Ringing is received (e.g., INVITE → 200 OK directly, or only 100 Trying), PDD is not measured
- Exposed as a Prometheus Histogram with buckets: `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` ms
- Use `histogram_quantile()` for percentile-based alerting

**PromQL examples:**
```promql
# 95th percentile post dial delay (all carriers)
histogram_quantile(0.95, sum(rate(sip_exporter_pdd_bucket[5m])) by (le))

# 95th percentile post dial delay (specific carrier and device type)
histogram_quantile(0.95, sum(rate(sip_exporter_pdd_bucket{carrier="carrier-a",ua_type="yealink"}[5m])) by (le))

# Average post dial delay
rate(sip_exporter_pdd_sum[5m]) / rate(sip_exporter_pdd_count[5m])
```

**Example values:**
- `< 100 ms` — excellent (fast call setup)
- `100-500 ms` — acceptable (typical for inter-carrier calls)
- `> 3000 ms` — potential issues (routing delays, server overload)

---

### Post Bye Delay (PBD)

`sip_exporter_pbd{carrier="...",ua_type="...",source_country="..."}` *(histogram, buckets 1..5000 ms)*: delay in milliseconds between a BYE request and the corresponding `200 OK BYE` response. Measures how quickly the endpoint tears down the session after hang-up. High PBD indicates SBC/media gateway processing delays or resource contention.

**Buckets:** 1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000 ms.

**PromQL examples:**
```promql
# Average PBD
rate(sip_exporter_pbd_sum[5m]) / rate(sip_exporter_pbd_count[5m])

# 95th percentile
histogram_quantile(0.95, rate(sip_exporter_pbd_bucket[5m]))
```

---

### Answer Seizure Ratio (ASR)

`sip_exporter_asr{carrier="...",ua_type="..."}`: percentage of INVITE requests that received a 200 OK response.

**Formula (ITU-T E.411):**
```
ASR = (INVITE → 200 OK) / Total INVITE × 100
```

- Classic telephony metric defined in ITU-T E.411; related to (but distinct from) SER defined in RFC 6076 §4.6
- Unlike SER, 3xx responses are **NOT excluded from the denominator**
- Undefined when no INVITE requests have been received

**Relationship with SER:** ASR is always <= SER. When 3xx responses are present, SER excludes them from the denominator, making SER higher. ASR keeps all INVITEs in the denominator.

**PromQL examples:**
```promql
# Current ASR (per carrier)
sip_exporter_asr

# Compare with SER to detect redirect volume
sip_exporter_ser - sip_exporter_asr
```

**Carrier-scoped queries:**
```promql
# ASR for a specific carrier and device type
sip_exporter_asr{carrier="carrier-a",ua_type="yealink"}

# Compare ASR across all carriers
sip_exporter_asr
```

**Example values:**
- `100` — all INVITEs received 200 OK
- `50` — half of INVITEs received 200 OK
- `0` — no INVITEs received 200 OK

---

### Session Duration Counter (SDC)

`sip_exporter_sdc_total{carrier="...",ua_type="..."}`: total number of completed SIP sessions (Prometheus Counter).

- Counts sessions that ended via:
  1. `200 OK` received for `BYE` (normal termination), **OR**
  2. Dialog expired via Session-Expires timeout (RFC 4028)
- Same events counted by SCR numerator, but exposed as a Counter for rate queries

**PromQL examples:**
```promql
# Session completion rate (sessions per second, per carrier)
rate(sip_exporter_sdc_total[5m])

# Session completion rate for specific carrier and device type
rate(sip_exporter_sdc_total{carrier="carrier-a",ua_type="yealink"}[5m])

# Session completion rate per minute
rate(sip_exporter_sdc_total[1m]) * 60
```

---

### Network Effectiveness Ratio (NER)

`sip_exporter_ner{carrier="...",ua_type="..."}`: percentage of INVITE requests that did **not** result in ineffective (infrastructure failure) responses.

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
# Current NER (per carrier)
sip_exporter_ner

# Verify NER = 100 - ISA
sip_exporter_ner + sip_exporter_isa

# NER for a specific carrier and device type
sip_exporter_ner{carrier="carrier-a",ua_type="yealink"}
```

**Example values:**
- `100` — no infrastructure failures (all INVITEs resolved without 408/500/503/504)
- `95` — 5% of INVITEs hit server errors or timeouts
- `0` — all INVITEs resulted in infrastructure failures

---

### Ineffective Session Severity (ISS)

`sip_exporter_iss_total{carrier="...",ua_type="..."}`: total number of INVITE requests that resulted in ineffective responses (Prometheus Counter).

- Counts INVITE responses with status codes: `408`, `500`, `503`, `504`
- Same codes used by ISA numerator, but exposed as an absolute Counter
- Useful for alerting on absolute volume of infrastructure failures (unlike ISA which is a percentage)

**PromQL examples:**
```promql
# Ineffective sessions per second (per carrier)
rate(sip_exporter_iss_total[5m])

# Total ineffective sessions since start
sip_exporter_iss_total

# Alert: more than 20 ineffective sessions per second for any carrier
rate(sip_exporter_iss_total[5m]) > 20
```

**Example values:**
- `0` — no infrastructure failures detected
- `rate > 5/sec` — elevated error rate, investigate
- `rate > 20/sec` — critical, immediate attention required

---

### OPTIONS Response Delay (ORD)

`sip_exporter_ord{carrier="...",ua_type="..."}`: histogram of delays in milliseconds between sending an OPTIONS request and receiving any response.

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
# 95th percentile OPTIONS response delay (all carriers)
histogram_quantile(0.95, sum(rate(sip_exporter_ord_bucket[5m])) by (le))

# 95th percentile OPTIONS response delay (specific carrier and device type)
histogram_quantile(0.95, sum(rate(sip_exporter_ord_bucket{carrier="carrier-a",ua_type="yealink"}[5m])) by (le))

# Average OPTIONS response delay
rate(sip_exporter_ord_sum[5m]) / rate(sip_exporter_ord_count[5m])
```

**Example values:**
- `< 50 ms` — excellent SIP server responsiveness
- `100-500 ms` — acceptable (typical for WAN or loaded servers)
- `> 1000 ms` — potential issues (server overload, network latency)

---

### Location Registration Delay (LRD)

`sip_exporter_lrd{carrier="...",ua_type="..."}`: histogram of delays in milliseconds between sending a REGISTER request and receiving a 3xx redirect response.

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
# 95th percentile location registration delay (all carriers)
histogram_quantile(0.95, sum(rate(sip_exporter_lrd_bucket[5m])) by (le))

# 95th percentile location registration delay (specific carrier and device type)
histogram_quantile(0.95, sum(rate(sip_exporter_lrd_bucket{carrier="carrier-a",ua_type="yealink"}[5m])) by (le))

# Average location registration delay
rate(sip_exporter_lrd_sum[5m]) / rate(sip_exporter_lrd_count[5m])
```

**Example values:**
- `< 50 ms` — fast redirect processing
- `100-500 ms` — acceptable (redirect involves DNS or database lookup)
- `> 1000 ms` — potential issues (slow redirect server, DNS resolution delays)

---

## Voice Quality Metrics (RFC 6035)

Voice quality metrics are extracted from SIP PUBLISH and NOTIFY requests carrying `Content-Type: application/vq-rtcpxr` bodies per RFC 6035 (RTCP XR SIP Package). The exporter parses `VQSessionReport: CallTerm` report blocks and exposes each metric field as a labeled Prometheus histogram.

### How It Works

**Data source:** SIP endpoints (IP phones, PBXs, SBCs, media gateways) generate voice quality reports after each call completes. These reports contain RTP-level statistics measured during the call — packet loss, jitter, delay, MOS scores, R-factor values. Reports are sent as SIP PUBLISH or NOTIFY requests with `Content-Type: application/vq-rtcpxr` to a collector endpoint.

**Capture path:**
```
SIP Endpoint → PUBLISH/NOTIFY → Network → eBPF capture → SIP parser → VQ parser → Prometheus histogram
```

The exporter does **not** generate or calculate voice quality metrics itself. It only parses and exports the values reported by SIP endpoints. The accuracy of metrics depends entirely on the reporting endpoint's RTP statistics implementation.

**PUBLISH vs NOTIFY:**
- **PUBLISH** — endpoint sends VQ report to a dedicated collector URI (e.g., `sip:collector@example.com`). Most common method in enterprise and carrier environments.
- **NOTIFY** — endpoint sends VQ report via a SIP event package subscription. Used when the collector subscribes to `vq-rtcpxr` events from the endpoint.

Both methods produce identical VQ report bodies. The exporter treats them equivalently — the only difference is the SIP method counter (`sip_exporter_publish_total` vs `sip_exporter_notify_total`).

### Detection

The exporter detects VQ reports when:
1. A SIP request is received (typically PUBLISH or NOTIFY, though the Content-Type check is method-agnostic)
2. The `Content-Type` header contains `application/vq-rtcpxr`
3. The body starts with `VQSessionReport`

If the body fails to parse, the exporter increments `sip_exporter_system_error_total` and `sip_exporter_parse_errors_total{type="vq"}`, and skips the report.

### Label Resolution

All VQ metrics include `carrier`, `ua_type`, `source_country`, and `direction` labels, resolved from the SIP PUBLISH/NOTIFY packet itself:

| Label | Resolution |
|-------|-----------|
| `carrier` | Source IP of the PUBLISH/NOTIFY request → CIDR matching against `carriers.yaml` |
| `ua_type` | `User-Agent` header of the PUBLISH/NOTIFY request → regex matching against `user_agents.yaml` |
| `source_country` | Source IP → GeoIP/carrier.country lookup (same precedence as all other metrics) |
| `direction` | `inbound`/`outbound` from the `pkttype` of the PUBLISH/NOTIFY request (same as other metrics) |

Unlike call setup metrics (INVITE/BYE) where carrier is resolved from the INVITE and propagated through trackers, VQ metrics use the carrier/ua_type of the PUBLISH/NOTIFY request directly. This is because VQ reports are sent **after** the call ends and may come from a different device than the one that initiated the call (e.g., an SBC generating reports for all calls it proxies).

### What Each Metric Measures

The 13 VQ metrics correspond to fields in the RFC 6035 `VQSessionReport` block. Each field is reported by the SIP endpoint based on its RTP statistics:

| Category | Metrics | Source |
|----------|---------|--------|
| **Packet loss** | NLR, JDR, BLD, GLD | RTP packet loss statistics (RTCP XR blocks) |
| **Delay** | RTD, ESD | Round trip time measurement + endpoint processing delay |
| **Jitter** | IAJ, MAJ | RTP timestamp interarrival variance |
| **MOS scores** | MOSLQ, MOSCQ | Estimated by endpoint based on ITU-T G.107 (E-Model) |
| **R-factor** | RLQ, RCQ | ITU-T G.107 E-Model output (0–120 scale) |
| **Echo** | RERL | Echo return loss after echo cancellation |

**Important:** Not all endpoints report all 13 metrics. The exporter only observes histograms for fields present in the report body. Absent fields are silently skipped (see [Partial Reports](#partial-reports) below).

### Voice Quality Reports Total

`sip_exporter_vq_reports_total{carrier="...",ua_type="..."}`: total number of VQ session reports successfully processed (Prometheus Counter).

**PromQL examples:**
```promql
# VQ report processing rate
rate(sip_exporter_vq_reports_total[5m])

# VQ reports per carrier
sip_exporter_vq_reports_total{carrier="carrier-a"}
```

### Network Loss Rate (NLR)

`sip_exporter_vq_nlr_percent{carrier="...",ua_type="..."}`: histogram of network packet loss rate percentage.

- Measures the percentage of RTP packets lost in the network (not recovered by jitter buffer)
- Range: 0–100%
- Buckets: `[0, 0.1, 0.5, 1, 2, 5, 10, 20, 50, 100]`

**PromQL examples:**
```promql
# Average NLR per carrier
rate(sip_exporter_vq_nlr_percent_sum[5m]) / rate(sip_exporter_vq_nlr_percent_count[5m])

# 95th percentile packet loss
histogram_quantile(0.95, sum(rate(sip_exporter_vq_nlr_percent_bucket[5m])) by (le))
```

### Jitter Buffer Discard Rate (JDR)

`sip_exporter_vq_jdr_percent{carrier="...",ua_type="..."}`: histogram of jitter buffer discard rate percentage.

- Measures the percentage of RTP packets discarded by the jitter buffer (too late or early)
- Range: 0–100%
- Buckets: `[0, 0.1, 0.5, 1, 2, 5, 10, 20, 50, 100]`

### Burst Loss Density (BLD)

`sip_exporter_vq_bld_percent{carrier="...",ua_type="..."}`: histogram of burst loss density percentage.

- Measures the density of packet loss during burst periods
- Range: 0–100%
- Buckets: `[0, 0.1, 0.5, 1, 2, 5, 10, 20, 50, 100]`

### Gap Loss Density (GLD)

`sip_exporter_vq_gld_percent{carrier="...",ua_type="..."}`: histogram of gap loss density percentage.

- Measures the density of packet loss during gap (non-burst) periods
- Range: 0–100%
- Buckets: `[0, 0.1, 0.5, 1, 2, 5, 10, 20, 50, 100]`

### Round Trip Delay (RTD)

`sip_exporter_vq_rtd_ms{carrier="...",ua_type="..."}`: histogram of round trip delay in milliseconds.

- Measures the network round trip time for the RTP stream
- Buckets: `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` ms

**PromQL examples:**
```promql
# 95th percentile round trip delay
histogram_quantile(0.95, sum(rate(sip_exporter_vq_rtd_ms_bucket[5m])) by (le))
```

### End System Delay (ESD)

`sip_exporter_vq_esd_ms{carrier="...",ua_type="..."}`: histogram of end system delay in milliseconds.

- Measures the total delay added by the end system (jitter buffer, codec, etc.)
- Buckets: `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` ms

### Interarrival Jitter (IAJ)

`sip_exporter_vq_iaj_ms{carrier="...",ua_type="..."}`: histogram of interarrival jitter in milliseconds.

- Measures the statistical variance of RTP packet interarrival time
- Buckets: `[0.1, 0.5, 1, 5, 10, 20, 50, 100, 200, 500]` ms

### Mean Absolute Jitter (MAJ)

`sip_exporter_vq_maj_ms{carrier="...",ua_type="..."}`: histogram of mean absolute jitter in milliseconds.

- Measures the mean absolute value of jitter
- Buckets: `[0.1, 0.5, 1, 5, 10, 20, 50, 100, 200, 500]` ms

### MOS Listening Quality (MOSLQ)

`sip_exporter_vq_mos_lq{carrier="...",ua_type="..."}`: histogram of MOS Listening Quality score.

- Estimated MOS score for one-way listening quality (no echo consideration)
- Range: 1.0–4.9
- Buckets: `[1.0, 1.5, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5.0]`

**PromQL examples:**
```promql
# Average MOS Listening Quality
rate(sip_exporter_vq_mos_lq_sum[5m]) / rate(sip_exporter_vq_mos_lq_count[5m])

# Percentage of calls with MOS below 3.0
sum(rate(sip_exporter_vq_mos_lq_bucket{le="2.5"}[5m]))
  / sum(rate(sip_exporter_vq_mos_lq_count[5m])) * 100
```

**Example values:**
- `4.0–4.9` — excellent quality
- `3.5–4.0` — good quality
- `3.0–3.5` — acceptable quality
- `1.0–3.0` — poor quality, investigate

### MOS Conversational Quality (MOSCQ)

`sip_exporter_vq_mos_cq{carrier="...",ua_type="..."}`: histogram of MOS Conversational Quality score.

- Estimated MOS score including both directions and echo effects
- Range: 1.0–4.9
- Buckets: `[1.0, 1.5, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5.0]`

### R-factor Listening Quality (RLQ)

`sip_exporter_vq_rlq{carrier="...",ua_type="..."}`: histogram of R-factor Listening Quality.

- R-factor for one-way listening quality (ITU-T G.107)
- Range: 0–120 (typically 0–100)
- Buckets: `[0, 10, 20, 30, 50, 60, 70, 80, 90, 100, 120]`

**Example values:**
- `90–100` — excellent
- `80–90` — good
- `70–80` — acceptable
- `< 70` — poor

### R-factor Conversational Quality (RCQ)

`sip_exporter_vq_rcq{carrier="...",ua_type="..."}`: histogram of R-factor Conversational Quality.

- R-factor including both directions and echo effects
- Range: 0–120
- Buckets: `[0, 10, 20, 30, 50, 60, 70, 80, 90, 100, 120]`

### Residual Echo Return Loss (RERL)

`sip_exporter_vq_rerl_db{carrier="...",ua_type="..."}`: histogram of residual echo return loss in dB.

- Measures the echo return loss after echo cancellation
- Higher values mean less echo
- Buckets: `[0, 5, 10, 15, 20, 30, 40, 50, 60, 80, 100]` dB

### Partial Reports

Not all VQ session reports contain all 13 metrics. The exporter only observes histograms for fields that are present in the report body. Absent fields are silently skipped. This means histogram `*_count` values may differ across VQ metrics.

### VQ Report Format

Expected body format (RFC 6035):
```
VQSessionReport: CallTerm
CallID: <call-id>
LocalID: <sip-uri>
RemoteID: <sip-uri>
NLR=0.50 JDR=1.20 BLD=0.30 GLD=0.10
RTD=45.5 ESD=20.3 IAJ=5.2 MAJ=3.1
MOSLQ=4.5 MOSCQ=4.2 RLQ=92.0 RCQ=88.0
RERL=55.0
```

Multiple metrics can appear on the same line (space-separated) or on separate lines. Header lines (containing `:` but not `=`) are ignored.

---

## Alerts

The repository includes pre-configured alert rules and dashboards so monitoring works out-of-the-box.

### What's Included

| Component | File | Description |
|-----------|------|-------------|
| Grafana dashboard | [`examples/grafana-dashboard.json`](../examples/grafana-dashboard.json) | Full production dashboard (variables, all metric types) |
| Alerting guide | [`docs/ALERTING.md`](ALERTING.md) | Complete reference: rules, Alertmanager configs (Slack/PagerDuty/Email), threshold tuning |

### Alert Summary by Category

#### Fraud Detection

| Alert | Metric | Trigger | Severity |
|-------|--------|---------|----------|
| `SIPRegistrationScan` | `register_scan_total` | rate > 0 (one IP registering many accounts) | critical |
| `SIPInviteBurst` | `invite_burst_total` | rate > 0 (one IP flooding INVITEs) | critical |
| `SIPRegistrationCountryChange` | `register_country_change_total` | counter > 0 and was 0/absent 5m ago | warning |
| `SIPSessionCapacityExhaustion` | `sessions_utilization` | > 90% for 5m | warning |

#### SIP Health

| Alert | Metric | Trigger | Severity |
|-------|--------|---------|----------|
| `SIPExporterDown` | `up` | == 0 for 1m | critical |
| `SIPHighServerErrorRate` | `isa` | avg > 50% for 1m | critical |
| `SIPSessionEstablishmentCritical` | `ser` | < 20% for 2m | critical |
| `SIPSessionEstablishmentLow` | `ser` | < 50% for 5m | warning |
| `SIPRegistrationSlow` | `rrd` | p95 > 500ms for 5m | warning |
| `SIPPacketDropHigh` | `socket_packets_dropped_total` | > 10/s for 2m | warning |

#### Voice Quality (RTP)

| Alert | Metric | Trigger | Severity |
|-------|--------|---------|----------|
| `RTPMOSLow` | `rtp_mos_score` | avg < 3.5 for 5m | warning |
| `RTPPacketLossHigh` | `rtp_packets_lost_total` / `rtp_packets_total` | > 5% for 5m | warning |
| `RTPJitterHigh` | `rtp_jitter_milliseconds` | p95 > 50ms for 5m | warning |

### Threshold Recommendations

| Metric | Green | Yellow | Red |
|--------|-------|--------|-----|
| SER | ≥ 95% | 80–95% | < 80% |
| SEER | ≥ 95% | 80–95% | < 80% |
| ISA | < 5% | 5–15% | > 15% |
| SCR | ≥ 80% | 60–80% | < 60% |
| RRD p95 | < 100ms | 100–500ms | > 500ms |
| RTP MOS | ≥ 4.0 | 3.5–4.0 | < 3.5 |
| RTP Jitter p95 | < 30ms | 30–50ms | > 50ms |
| RTP Packet Loss | < 2% | 2–5% | > 5% |

Full alerting guide with Prometheus rules, Alertmanager configs, and best practices: [`docs/ALERTING.md`](ALERTING.md).
