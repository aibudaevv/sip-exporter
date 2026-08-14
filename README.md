# SIP-exporter

**[EN](README.md)** | **[RU](README.ru.md)**

High-performance eBPF-based SIP monitoring service that captures and exports telephony metrics to Prometheus-compatible systems (Prometheus, VictoriaMetrics, etc.).
Captures SIP packets directly in the Linux kernel using eBPF, minimizing userspace processing overhead.

[![Go Test](https://github.com/aibudaevv/sip-exporter/actions/workflows/go.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/go.yml)
[![Go Vulncheck](https://github.com/aibudaevv/sip-exporter/actions/workflows/vulncheck.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/vulncheck.yml)
[![Container Scan](https://github.com/aibudaevv/sip-exporter/actions/workflows/trivy.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/trivy.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/aibudaevv/sip-exporter)](https://goreportcard.com/report/github.com/aibudaevv/sip-exporter)
[![Docker Pulls](https://img.shields.io/docker/pulls/frzq/sip-exporter)](https://hub.docker.com/r/frzq/sip-exporter)
[![GitHub Release](https://img.shields.io/github/v/release/aibudaevv/sip-exporter)](https://github.com/aibudaevv/sip-exporter/releases)
[![License](https://img.shields.io/badge/license-AGPL--3.0-blue)](https://github.com/aibudaevv/sip-exporter/blob/main/LICENSE)
[![Issues](https://img.shields.io/github/issues/aibudaevv/sip-exporter)](https://github.com/aibudaevv/sip-exporter/issues)

## Table of Contents

- [Key Features](#key-features)
- [Quick Start](#quick-start)
- [Installation Verification](docs/INSTALLATION.md)
- [Core Technology](#core-technology)
- [Architecture](#architecture)
- [Performance](#performance)
- [Install](#install)
- [Deployment Topology](#deployment-topology)
- [Metrics](#metrics)
- [Fraud Detection](#fraud-detection)
- [Security](docs/SECURITY.md)
- [Development](#development)
- [Benchmark](#benchmark)
- [Alerting](#alerting)
- [Metrics Storage Compatibility](#metrics-storage-compatibility)
- [License](#license)
- [Changelog](#changelog)

## Key Features

- 🌐 **Multi-interface monitoring** — capture SIP/RTP from multiple NICs simultaneously, each tagged with an `iface` label
- ⚡ **Low overhead** — eBPF packet filtering in kernel space
- 🐳 **Single container deployment** — no external dependencies
- 🔧 **Configurable SIP ports** — monitor custom ports via environment variables
- 📈 **Prometheus native** — standard `/metrics` endpoint for scraping
- 🏷️ **Per-carrier metrics** — CIDR-based carrier resolution for all SIP metrics
- 🏷️ **Per-device-type metrics** — User-Agent classification for all SIP metrics
- 🌍 **Geo-enrichment** — `source_country` (GeoIP) and `destination_country` (E.164 prefix) labels on SIP metrics
- 🔀 **Traffic direction** — `inbound`/`outbound` label on SIP and RTP traffic metrics via kernel `pkttype`, zero-config
- 📞 **Voice quality (RFC 6035)** — MOS scores, jitter, packet loss from SIP PUBLISH/NOTIFY
- 🎧 **RTP media analysis** — jitter, packet loss, MOS (E-model G.107), and Packet Delay Variation (PDV, per-packet, VoIPMonitor-parity) from RTP streams correlated with SIP dialogs
- 📊 **RTCP endpoint-reported quality** — loss, jitter, and round-trip time (RTT) from RTCP SR/RR (RFC 3550), correlated by SSRC; supports rtcp-mux (RFC 5761), explicit `a=rtcp` (RFC 3605), and legacy port+1
- 🛡️ **Fraud detection** — registration scan, INVITE burst, account-takeover (country change), and False Answer Supervision (FAS) signals ([docs/fraud-detection.md](docs/fraud-detection.md))

## Quick Start

Copy the [pinned production Compose example](examples/docker-compose.production.yml) to your host.
Set `SIP_EXPORTER_INTERFACE` to the host interface that carries both SIP signaling and RTP media.

```bash
cp examples/docker-compose.production.yml docker-compose.yml
SIP_EXPORTER_INTERFACE=eth0 docker compose up -d
curl http://localhost:2112/metrics
```

The example includes a pinned release image, restart policy, healthcheck, read-only filesystem, and
every runtime setting listed below with its default value.

Access metrics at `http://localhost:2112/metrics`. A `/health` endpoint is also exposed (returns `200 OK` when alive, `503` otherwise) — used by the Dockerfile `HEALTHCHECK` and suitable for orchestrator liveness/readiness probes.

**First useful dashboard:** follow the [installation verification runbook](docs/INSTALLATION.md)
to check health, scrape status, SIP, SDP/RTP visibility and drops before importing Grafana.

## Core Technology

This service uses eBPF (extended Berkeley Packet Filter) attached to `AF_PACKET` sockets to
intercept IPv4 SIP packets over UDP (default port 5060) at L4 without overhead of iptables/nftables or userspace daemons like tcpdump. SIP over TCP or TLS is not captured.
Filtered packets are delivered to userspace via the socket for efficient Go processing.

## Architecture
```
SIP + RTP Traffic → NIC → eBPF socket filter → AF_PACKET socket → Go poller → SIP parser + RTP tracker → Prometheus
```

## Performance

Zero packet loss in the measured full SIP dialog lifecycle up to **1,800 CPS** (~21,200 PPS), at **<15% CPU** and **9–16 MB RAM**. GC stop-the-world pauses under **1 ms** in the measured workload, leaving substantial headroom relative to the socket buffer. Memory remained stable during the benchmark run.

Go micro-benchmarks:

| Operation | Latency | Memory |
|-----------|---------|--------|
| Parse BYE packet (L2→SIP) | ~860 ns | 712 B/op |
| Parse INVITE packet (L2→SIP) | ~1.1 μs | 808 B/op |
| Parse 200 OK packet (L2→SIP) | ~2.0 μs | 1176 B/op |

Full load test results: [docs/BENCHMARK.md](./docs/BENCHMARK.md).

## Install

```bash
docker pull frzq/sip-exporter:latest
```

### Configure
Environment variables:
* `SIP_EXPORTER_INTERFACE` - one or more network interfaces, comma-separated (required). Examples: `eth0`, `eth0,eth1,eth2`.
* `SIP_EXPORTER_HTTP_PORT` - http port for prometheus (default 2112)
* `SIP_EXPORTER_LOGGER_LEVEL` - log level (default info)
* `SIP_EXPORTER_SIP_PORTS` - one or more SIP ports, comma-separated (default 5060; up to 3 per interface). Use `;` for per-interface sets: `5060,5062;5060,5061`.
* `SIP_EXPORTER_OBJECT_FILE_PATH` - path to eBPF object file (default /usr/local/bin/sip.o)
* `SIP_EXPORTER_CARRIERS_CONFIG` - path to carriers YAML config (optional, see [`examples/carriers.yaml`](examples/carriers.yaml))
* `SIP_EXPORTER_USER_AGENTS_CONFIG` - path to user-agents YAML config (optional, see [`examples/user_agents.yaml`](examples/user_agents.yaml))
* `SIP_EXPORTER_RTP_STREAM_TTL` - idle RTP stream expiry, RFC 3550 §6.3.5 timeout (default 30s)
* `SIP_EXPORTER_IGNORE_OUTGOING` - loopback/test only: suppress duplicate TX packets on `lo` (default false, do NOT enable in production)
* `SIP_EXPORTER_GEOIP_COUNTRY_DB` - path to MaxMind GeoLite2-Country.mmdb for `source_country` label (optional)
* `SIP_EXPORTER_LOCAL_COUNTRY_CODE` - ISO alpha-2 country code for domestic phone-number fallback in `destination_country` (optional, e.g. `RU`)
* `SIP_EXPORTER_HOST_LABELS` - enable `caller_host`/`called_host` labels on INVITE metrics (default `false`; opt-in — unbounded cardinality, enable only on trusted/bounded deployments)
* `SIP_EXPORTER_SESSIONS_LIMITS` - path to sessions limits YAML config (optional, per-carrier concurrent-session caps and utilization metrics)
* `SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD` - registration scan: unique accounts (AoR) registered (200 OK) from one source IP to trigger the signal (default 10)
* `SIP_EXPORTER_FRAUD_REGISTER_SCAN_WINDOW` - registration scan rolling window (default 60s)
* `SIP_EXPORTER_FRAUD_INVITE_BURST_THRESHOLD` - INVITE burst fraud: INVITEs from one source to trigger the signal (default 100)
* `SIP_EXPORTER_FRAUD_INVITE_BURST_WINDOW` - INVITE burst rolling window (default 60s)
* `SIP_EXPORTER_FRAUD_FAS_THRESHOLD` - False Answer Supervision: seconds after a 200 OK with no observed RTP to trigger the signal (default 10s)
* `SIP_EXPORTER_TELEMETRY` - anonymous usage telemetry, opt-out with `false` (default true)

The container must run with `--privileged` and `--network host` (eBPF requires `CAP_BPF` and access to the network interface). See [Security](docs/SECURITY.md) for details on why this is safe.

> ⚠️ **Multi-interface caveat:** do not specify interfaces that see the same traffic (bond parent + child, bridge + member, VLAN parent + subinterface, duplicate SPAN ports). Doing so will double-count metrics. When in doubt, list only physical NICs.

## Deployment Topology

Install sip-exporter on the host where **both SIP signaling and RTP media pass through**. It captures packets from the network interface it is attached to, and the `direction` label relies on the kernel seeing packets as addressed to that host — so the host must own those IPs, not receive them via a mirror.

Coverage depends on what the host actually sees:

- **SIP only** (signaling passes through, media does not) → SIP metrics only; RTP metrics stay empty.
- **RTP only** (media passes through, signaling does not) → the exporter cannot correlate streams to dialogs, because it learns RTP endpoints from the SDP carried inside SIP messages. Place it where signaling is also visible.
- **SIP + RTP** → full metrics.

### Capture Support Matrix

| Scenario | Status | Operational requirement or limitation |
|----------|--------|--------------------------------------|
| SIP and RTP/RTCP over IPv4/UDP | Supported | The sensor must see signaling and both media directions on the same call path. |
| `rtcp-mux`, SDP `a=rtcp`, or legacy RTP/RTCP port+1 | Supported | The final IPv4 endpoint and port must be present in SDP visible to the exporter. |
| NAT/SBC with stable SDP-advertised media endpoints | Supported | Symmetric RTP source-port remapping is learned after destination-correlated RTP when the source IP still matches the SDP peer. Source-IP changes and ambiguous shared endpoints are not learned. |
| SIP over TCP/TLS, IPv6 SIP/SDP/media, or fragmented UDP | Unsupported | The capture and SDP path are IPv4/UDP-only and do not reassemble IP fragments. |
| RTP without visible SDP, or ICE/TURN endpoint changes after SDP | Unsupported | The kernel filter has no endpoint to register, so the media is dropped. |
| SPAN/TAP or other mirrored traffic | Not supported for QoE/direction | Packet collection may occur, but `direction` is not trustworthy because the sensor does not own the traffic IPs. Deploy on the forwarding host. |

## Metrics

All metrics are exposed at `/metrics` in Prometheus exposition format. Most SIP metrics include `carrier`, `ua_type`, and `source_country` labels for multi-dimensional analysis (exceptions: `sip_exporter_sessions_limit` carries only `carrier`; `sip_exporter_billable_seconds_total` carries `carrier`, `destination_country`, `direction`). INVITE metrics additionally carry an `iface` label identifying the network interface that captured the traffic. The exporter provides:

- **Traffic counters** — SIP request types (INVITE, re-INVITE, BYE, REGISTER, etc.) and response status codes (100–606)
- **Active sessions** — real-time count of active SIP dialogs
- **RFC 6076 performance metrics** — SER, SEER, ISA, SCR, ASR, NER, RRD, SPD, TTR, PDD, PBD
- **RFC 6035 voice quality metrics** — NLR, JDR, BLD, GLD, RTD, ESD, IAJ, MAJ, MOSLQ, MOSCQ, RLQ, RCQ, RERL
- **RTP media metrics** — `sip_exporter_rtp_packets_total`, `sip_exporter_rtp_packets_lost_total`, `sip_exporter_rtp_jitter_milliseconds`, `sip_exporter_rtp_pdv_milliseconds` (per-packet Packet Delay Variation), `sip_exporter_rtp_mos_score`, `sip_exporter_rtp_active_streams` (labels: `carrier,ua_type,codec,source_country,direction`)
- **RTCP quality metrics** — `sip_exporter_rtcp_reports_total`, `sip_exporter_rtcp_loss_fraction_percent`, `sip_exporter_rtcp_cumulative_loss_total`, `sip_exporter_rtcp_jitter_milliseconds`, `sip_exporter_rtcp_rtt_milliseconds`, `sip_exporter_rtcp_orphan_reports_total` (labels: `carrier,ua_type,codec,source_country,direction`)
- **Fraud signals** — `sip_exporter_fas_calls_total` (False Answer Supervision: 200 OK with no RTP within threshold), `sip_exporter_register_scan_total`, `sip_exporter_invite_burst_total`, `sip_exporter_register_country_change_total`
- **Diagnostics** — `sip_exporter_sip_retransmission_total` (SIP Timer A retransmissions), `sip_exporter_rtp_out_of_order_total` (out-of-sequence RTP packets), `sip_exporter_short_calls_total` (calls shorter than 20/60/180 seconds)

Full reference with formulas, examples, and RFC section mapping: [docs/METRICS.md](docs/METRICS.md)

### Per-Carrier Metrics

If your SIP infrastructure handles traffic from multiple operators (telecom providers, SIP trunks, PBX clusters), you need to see metrics **per operator**, not in aggregate.

The carrier feature solves this by mapping IP subnets to operator names. Every metric — INVITE count, SER, active sessions, RRD latency — gets a `carrier` label, so you can build separate Grafana dashboards and alerts for each operator.

**How it works:**

The exporter looks at the **source IP** of every SIP request and matches it against CIDR subnets in a YAML config. When UAC at `10.1.5.20` sends an INVITE, the exporter finds that `10.1.5.20` falls within `10.1.0.0/16` defined for carrier "telecom-alpha", and tags all metrics for this call — the INVITE itself, the 200 OK response, the BYE, even the dialog expiry — with `carrier="telecom-alpha"`.

This means:
- INVITE from `10.1.5.20` → metrics labeled `carrier="telecom-alpha"`
- INVITE from `192.168.11.3` → metrics labeled `carrier="telecom-beta"`
- INVITE from `8.8.8.8` (not in any subnet) → metrics labeled `carrier="other"`

**Setup:**

Add a read-only mount for your [carrier configuration](examples/carriers.yaml) to the production
Compose file and set `SIP_EXPORTER_CARRIERS_CONFIG=/etc/sip-exporter/carriers.yaml`.

```yaml
# carriers.yaml — map your operators' IP subnets
carriers:
  - name: "telecom-alpha"
    cidrs:
      - "10.1.0.0/16"
  - name: "telecom-beta"
    cidrs:
      - "192.168.10.0/24"
      - "192.168.11.0/24"
```

After that, metrics look like:

```
sip_exporter_invite_total{carrier="telecom-alpha",ua_type="other"}  1523
sip_exporter_ser{carrier="telecom-alpha",ua_type="other"}            95.2
sip_exporter_ser{carrier="telecom-beta",ua_type="other"}             87.4
sip_exporter_ser{carrier="other",ua_type="other"}                     0.0
```

**Things to know:**

- Carrier is determined at **request time** (INVITE/REGISTER/OPTIONS), not response time. If carrier-A sends INVITE and carrier-B answers 200 OK, all metrics still go to carrier-A — the operator who initiated the call
- If source IP doesn't match any CIDR, destination IP is tried. If neither matches → `carrier="other"`
- When CIDRs overlap, **first match wins** — list specific subnets before broad ones
- Without the config file, all metrics get `carrier="other"` — nothing breaks
- Each carrier can have multiple CIDRs, and multiple carriers can be defined
- CIDR notation is required — plain IPs without `/` are rejected. Use `/32` for a single host, e.g. `"10.226.97.5/32"` instead of `"10.226.97.5"`

Full config reference with examples: [`examples/carriers.yaml`](examples/carriers.yaml)

### Per-Device-Type Metrics (User-Agent Classification)

If you need to see metrics **per SIP device type** — IP phones vs softphones vs SBCs — the User-Agent classification feature adds a `ua_type` label to every metric.

The exporter reads the `User-Agent` SIP header from each request and matches it against regex patterns in a YAML config. Every metric — INVITE count, SER, active sessions, SPD duration — gets a `ua_type` label, so you can build separate Grafana dashboards and alerts for each device family.

**How it works:**

The exporter parses the `User-Agent` header of every SIP request and matches it against regex patterns in a YAML config. When a phone with `User-Agent: Yealink SIP-T46S 66.15.0.10` sends an INVITE, the exporter matches `^Yealink` and tags all metrics for this call with `ua_type="yealink"`.

This means:
- INVITE from Yealink phone → metrics labeled `ua_type="yealink"`
- INVITE from Grandstream phone → metrics labeled `ua_type="grandstream"`
- INVITE with unknown User-Agent → metrics labeled `ua_type="other"`

**Setup:**

Add a read-only mount for your [User-Agent configuration](examples/user_agents.yaml) to the
production Compose file and set `SIP_EXPORTER_USER_AGENTS_CONFIG=/etc/sip-exporter/user_agents.yaml`.

```yaml
# user_agents.yaml — map User-Agent patterns to device types
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
```

After that, metrics look like:

```
sip_exporter_invite_total{carrier="telecom-alpha",ua_type="yealink"}     1523
sip_exporter_ser{carrier="telecom-alpha",ua_type="yealink"}               95.2
sip_exporter_ser{carrier="telecom-alpha",ua_type="grandstream"}           87.4
sip_exporter_ser{carrier="telecom-alpha",ua_type="other"}                  0.0
```

**Things to know:**

- UA type is determined at **request time** (INVITE/REGISTER/OPTIONS), using the same tracker mechanism as carrier. Responses inherit `ua_type` from the request tracker, not from the response's own headers
- The `User-Agent` header is extracted from all SIP packets, but SIP responses typically use the `Server` header, so in practice only requests provide meaningful classification
- If no pattern matches → `ua_type="other"`
- When patterns overlap, **first match wins** — list specific patterns before broad ones
- Without the config file, all metrics get `ua_type="other"` — nothing breaks
- Patterns are case-insensitive when using `(?i)` prefix
- Works **together with carrier** — every metric has both `carrier` and `ua_type` labels for two-dimensional analysis

**Combined carrier + ua_type queries:**

```promql
# SER for Yealink phones on a specific carrier
sip_exporter_ser{carrier="telecom-alpha",ua_type="yealink"}

# Active sessions by device type (across all carriers)
sum by (ua_type) (sip_exporter_sessions)

# INVITE rate per carrier per device type
sum by (carrier, ua_type) (rate(sip_exporter_invite_total[5m]))
```

Full config reference with examples: [`examples/user_agents.yaml`](examples/user_agents.yaml)

### Geo-Enrichment Labels

The exporter adds geographic context to SIP metrics via two labels:

| Label | Method | Scope |
|-------|--------|-------|
| `source_country` | GeoIP lookup of source IP (MaxMind GeoLite2-Country) | All SIP + RTP metrics |
| `destination_country` | E.164 phone-number prefix (embedded, no DB needed) | INVITE metrics only |

**source_country resolution:**
1. `carrier.country` — optional field in `carriers.yaml`, overrides GeoIP (operator-curated)
2. `GeoIP(srcIP)` — MaxMind GeoLite2-Country database lookup
3. `"unknown"` — fallback when neither is available

**destination_country** requires **no database** — the prefix table is embedded in the binary (Google libphonenumber, Apache 2.0). Set `SIP_EXPORTER_LOCAL_COUNTRY_CODE` for domestic numbers without international prefix.

**caller_host / called_host** are **off by default** (`SIP_EXPORTER_HOST_LABELS=false`). They expose the host part of the SIP `From`/`To` URI on `invite_total` / `invite_200_total`. Since distinct endpoint identifiers are unbounded, they are opt-in: enable (`SIP_EXPORTER_HOST_LABELS=true`) only on trusted deployments where the endpoint count is bounded, otherwise they can grow Prometheus memory. See [Security > Data Exposed in Prometheus Labels](docs/SECURITY.md#data-exposed-in-prometheus-labels).

**Setup:**

Follow the [GeoIP setup guide](docs/geoip.md) to add the read-only database mount and
`SIP_EXPORTER_GEOIP_COUNTRY_DB` to the production Compose file.

Full reference with formulas and PromQL examples: [docs/METRICS.md > Geo-Enrichment Labels](docs/METRICS.md#geo-enrichment-labels)

Step-by-step setup (how to get and connect the MaxMind database): [`docs/geoip.md`](docs/geoip.md)

```promql
# SER for calls to Russia
sum(rate(sip_exporter_invite_200_total{destination_country="RU"}[5m]))
  / sum(rate(sip_exporter_invite_total{destination_country="RU"}[5m])) * 100

# INVITE rate by destination country
sum by (destination_country) (rate(sip_exporter_invite_total[5m]))
```

### RTP Media Analysis

In addition to SIP signaling, the exporter captures RTP media streams to estimate transport quality at the **capture point** (jitter, sequence gaps, and E-model MOS). RTP streams are **correlated with SIP dialogs**: when a `200 OK` to INVITE carries SDP, the exporter registers the negotiated media endpoints and tracks the matching RTP flows until BYE (or Session-Expires expiry). This means RTP metrics inherit the dialog's `carrier`, `ua_type`, and the negotiated `codec` labels.

Metrics produced:

| Metric | Type | Description |
|--------|------|-------------|
| `sip_exporter_rtp_packets_total` | counter | RTP packets observed |
| `sip_exporter_rtp_packets_lost_total` | counter | packets lost (RFC 3550 sequence-gap accounting) |
| `sip_exporter_rtp_jitter_milliseconds` | histogram | interarrival jitter (RFC 3550 A.8) |
| `sip_exporter_rtp_mos_score` | histogram | MOS-LQ via ITU-T G.107 E-model (1.0–4.5) |
| `sip_exporter_rtp_active_streams` | gauge | active RTP streams correlated with dialogs |

**Privacy:** RTP packets are copied to userspace in snapshots capped at 64 bytes, so a small prefix of payload can accompany the headers. The application parses only the fixed 12-byte RTP header and does not inspect or persist audio. Matched RTCP compounds are copied up to the Ethernet MTU so their report blocks can be parsed.

RTP capture is always enabled. RTP without a correlated SIP dialog (no SDP exchange seen) is dropped, so only media for monitored calls is counted.

The eBPF filter uses **SDP-driven RTP detection**: media endpoints (IP:port) learned from INVITE 200 OK SDP are inserted into a BPF LRU hash map. Only UDP packets matching a registered endpoint pass the kernel filter — all other UDP is dropped. This eliminates false positives from random UDP traffic on public IPs.

**How to interpret QoE:** RTP loss, jitter, PDV, and MOS are observations of packets that reached this sensor; they are not a subscriber's subjective score or proof of end-to-end impairment. RTCP SR/RR adds the receiver's own RTP statistics for a correlated SSRC, but still covers only reports and media visible to the sensor. Before acting on QoE alerts, verify `sip_exporter_socket_packets_dropped_total`, `sip_exporter_rtp_dropped_total`, and the deployment topology above.

```PromQL
# Average MOS over the last 5m (per codec)
sum by (codec) (rate(sip_exporter_rtp_mos_score_sum[5m]))
  / sum by (codec) (rate(sip_exporter_rtp_mos_score_count[5m]))

# Packet loss ratio by carrier
sum by (carrier) (rate(sip_exporter_rtp_packets_lost_total[5m]))
  / sum by (carrier) (rate(sip_exporter_rtp_packets_total[5m]))
```

See [docs/METRICS.md](docs/METRICS.md) for the full RTP reference, formulas, and label resolution.

## Fraud Detection

The exporter emits signals for common toll-fraud patterns, exposed as Prometheus counters/gauges:

- **Registration scan** — many unique accounts (AoR) registered (200 OK) from one source IP in a short window (tunable via `SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD` / `_WINDOW`)
- **INVITE burst** — abnormal INVITE rate from one source (tunable via `SIP_EXPORTER_FRAUD_INVITE_BURST_THRESHOLD` / `_WINDOW`)
- **Account takeover** — a subscriber places calls from a country different from previously observed

Full setup, metrics reference, and alerting guidance: [docs/fraud-detection.md](docs/fraud-detection.md)

## Development

### Requirements
- Go 1.26.6+
- Clang/LLVM (for eBPF compilation)
- golangci-lint v2.9.0 and goimports (for `make lint` / `make imports`)
- Linux kernel with eBPF support
- Root privileges (required for eBPF and packet socket)

### Test Coverage

| Package | Coverage |
|---------|----------|
| `internal/config` | 100.0% |
| `pkg/log` | 95.5% |
| `internal/server` | 90.5% |
| `internal/service` | 75.4% |
| `internal/exporter` | 64.0% |

Test suite:
- **Unit tests** — MC/DC-oriented coverage of business logic
- **Table-driven E2E tests** — real SIP traffic via SIPp + testcontainers-go, covering RFC 6076, RFC 6035, RTP, RTCP, fraud, and multi-interface behavior
- **Load tests** — PPS throughput, VQ reports, concurrent sessions, memory stability, GC pauses, and scrape latency

## Benchmark

Load testing results: **0% packet loss at 1,800 CPS (~21,200 PPS) for the measured full call flow**.

See [BENCHMARK.md](./docs/BENCHMARK.md) for detailed results, methodology, and optimization notes.

## Alerting

The repository includes a Grafana dashboard and documented Prometheus alert-rule examples.

**Grafana dashboard** — import manually:

1. Grafana → Dashboards → Import
2. Upload [`examples/grafana-dashboard.json`](examples/grafana-dashboard.json)
3. Select your Prometheus or VictoriaMetrics datasource

The dashboard includes: traffic counters, SIP request/response breakdowns, active sessions, RFC 6076 performance metrics (SER, SEER, ISA, SCR, NER), registrations (active count, success ratio, failures by code, fraud signals), RTP media analysis (active streams, packet rate, loss rate, MOS, jitter by codec), voice quality metrics (RFC 6035: MOS, jitter, packet loss), delay histograms (RRD, TTR, PDD, SPD, ORD, LRD, PBD), session quality metrics (ISS, ASR, SDC), diagnostics (SIP retransmissions, short calls), and system errors.

Full alerting guide with Prometheus rules, Alertmanager configs (Slack/PagerDuty/Email), and threshold tuning: [docs/ALERTING.md](docs/ALERTING.md)

## Metrics Storage Compatibility

SIP-Exporter exports metrics in Prometheus exposition format, compatible with:

- **Prometheus** — pull-based monitoring
- **VictoriaMetrics** — high-performance time-series database
- **Grafana Cloud** — cloud-based observability
- **Any Prometheus-compatible scraper** — the `/metrics` endpoint follows the standard format

## License
This project is licensed under the **GNU Affero General Public License v3.0 (AGPL-3.0)**.

See [LICENSE](LICENSE) for full text.

### Third-Party Data Licenses

- **MaxMind GeoLite2** (`source_country`) — users download the database separately. Use, attribution, redistribution, and update obligations are governed by the [GeoLite EULA](https://www.maxmind.com/en/geolite/eula) and the incorporated CC BY-SA 4.0 terms.
- **Google libphonenumber** (`destination_country`) — [Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0). E.164 prefix data embedded in the binary at compile time.

### Commercial Use
- ✅ Free for personal and educational use
- ✅ Free for commercial use with conditions
- ⚠️ If you modify the program and let users interact with that modified version over a network, AGPL-3.0 §13 requires offering its Corresponding Source to those users
- 📧 For commercial licensing without AGPL requirements, contact the author

## Changelog
See the [GitHub Releases](https://github.com/aibudaevv/sip-exporter/releases) for version history.
