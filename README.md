# SIP-exporter

**[EN](README.md)** | **[RU](README.ru.md)**

High-performance eBPF-based SIP monitoring service that captures and exports telephony metrics to Prometheus-compatible systems (Prometheus, VictoriaMetrics, etc.).
Captures SIP packets directly in the Linux kernel using eBPF, minimizing userspace processing overhead.

[![Go Test](https://github.com/aibudaevv/sip-exporter/actions/workflows/go.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/go.yml)
[![Go Vulncheck](https://github.com/aibudaevv/sip-exporter/actions/workflows/vulncheck.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/vulncheck.yml)
[![Container Scan](https://github.com/aibudaevv/sip-exporter/actions/workflows/trivy.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/trivy.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/aibudaevv/sip-exporter)](https://goreportcard.com/report/github.com/aibudaevv/sip-exporter)
[![License](https://img.shields.io/badge/license-AGPL--3.0-blue)](https://github.com/aibudaevv/sip-exporter/blob/main/LICENSE)
[![Issues](https://img.shields.io/github/issues/aibudaevv/sip-exporter)](https://github.com/aibudaevv/sip-exporter/issues)

## Table of Contents

- [Key Features](#key-features)
- [Quick Start](#quick-start)
- [Core Technology](#core-technology)
- [Architecture](#architecture)
- [Performance](#performance)
- [Install](#install)
- [Metrics](docs/METRICS.md)
- [Security](docs/SECURITY.md)
- [Development](#development)
- [Benchmark](#benchmark)
- [Integration](#integration)
- [License](#license)
- [Changelog](#changelog)

## Key Features

- ⚡ **Low overhead** — eBPF packet filtering in kernel space
- 🐳 **Single container deployment** — no external dependencies
- 🔧 **Configurable SIP ports** — monitor custom ports via environment variables
- 📈 **Prometheus native** — standard `/metrics` endpoint for scraping
- 🏷️ **Per-carrier metrics** — CIDR-based carrier resolution for all SIP metrics
- 🏷️ **Per-device-type metrics** — User-Agent classification for all SIP metrics

## Quick Start

```yaml
# docker-compose.yml
services:
  sip-exporter:
    image: frzq/sip-exporter:latest
    privileged: true
    network_mode: host
    environment:
      - SIP_EXPORTER_INTERFACE=eth0
      # Optional: carrier labels for per-provider metrics
      # - SIP_EXPORTER_CARRIERS_CONFIG=/etc/sip-exporter/carriers.yaml
      # Optional: user-agent labels for per-device-type metrics
      # - SIP_EXPORTER_USER_AGENTS_CONFIG=/etc/sip-exporter/user_agents.yaml
    # volumes:
    #   - ./examples/carriers.yaml:/etc/sip-exporter/carriers.yaml:ro
    #   - ./examples/user_agents.yaml:/etc/sip-exporter/user_agents.yaml:ro
```

```bash
docker compose up -d
curl http://localhost:2112/metrics
```

Access metrics at `http://localhost:2112/metrics`.

## Core Technology

This service uses eBPF (extended Berkeley Packet Filter) attached to `AF_PACKET` sockets to
intercept SIP packets (UDP/5060-5061) at L4 without overhead of iptables/nftables or userspace daemons like tcpdump.
Filtered packets are delivered to userspace via the socket for efficient Go processing.

## Architecture
```
SIP Traffic → NIC → eBPF socket filter → AF_PACKET socket → Go poller → SIP parser → Prometheus
```

## Performance

Zero packet loss up to **2,000 CPS** (~24,000 PPS) with full SIP dialog lifecycle, at **<15% CPU** and **~15 MB RAM**. GC stop-the-world pauses under **1 ms** — 400× smaller than socket buffer capacity, ensuring packets are never lost due to GC. Memory is stable under sustained load with no leaks detected.

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
* `SIP_EXPORTER_INTERFACE` - net interface (required)
* `SIP_EXPORTER_HTTP_PORT` - http port for prometheus (default 2112)
* `SIP_EXPORTER_LOGGER_LEVEL` - log level (default info)
* `SIP_EXPORTER_SIP_PORT` - SIP port (default 5060)
* `SIP_EXPORTER_SIPS_PORT` - SIPS port (default 5061)
* `SIP_EXPORTER_OBJECT_FILE_PATH` - path to eBPF object file (default /usr/local/bin/sip.o)
* `SIP_EXPORTER_CARRIERS_CONFIG` - path to carriers YAML config (optional, see [`examples/carriers.yaml`](examples/carriers.yaml))
* `SIP_EXPORTER_USER_AGENTS_CONFIG` - path to user-agents YAML config (optional, see [`examples/user_agents.yaml`](examples/user_agents.yaml))

The container must run with `--privileged` and `--network host` (eBPF requires `CAP_BPF` and access to the network interface). See [Security](docs/SECURITY.md) for details on why this is safe.

## Metrics

All metrics are exposed at `/metrics` in Prometheus exposition format. All SIP metrics include `carrier` and `ua_type` labels for multi-dimensional analysis. The exporter provides:

- **Traffic counters** — SIP request types (INVITE, BYE, REGISTER, etc.) and response status codes (100–606)
- **Active sessions** — real-time count of active SIP dialogs
- **RFC 6076 performance metrics** — SER, SEER, ISA, SCR, ASR, NER, RRD, SPD, TTR
- **Extended metrics** — ISS, SDC, ORD, LRD

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

```yaml
# docker-compose.yml
services:
  sip-exporter:
    image: frzq/sip-exporter:latest
    privileged: true
    network_mode: host
    environment:
      - SIP_EXPORTER_INTERFACE=eth0
      - SIP_EXPORTER_CARRIERS_CONFIG=/etc/sip-exporter/carriers.yaml
    volumes:
      - ./carriers.yaml:/etc/sip-exporter/carriers.yaml:ro
```

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

```yaml
# docker-compose.yml
services:
  sip-exporter:
    image: frzq/sip-exporter:latest
    privileged: true
    network_mode: host
    environment:
      - SIP_EXPORTER_INTERFACE=eth0
      - SIP_EXPORTER_USER_AGENTS_CONFIG=/etc/sip-exporter/user_agents.yaml
    volumes:
      - ./user_agents.yaml:/etc/sip-exporter/user_agents.yaml:ro
```

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

## Development

### Requirements
- Go 1.25+
- Clang/LLVM (for eBPF compilation)
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
- **Unit tests** — MC/DC standard, all business logic covered
- **55 E2E tests** — real SIP traffic via SIPp + testcontainers-go, validates all RFC 6076 metrics
- **8 load tests** — PPS throughput, concurrent sessions, memory stability, GC pauses, scrape latency

## Benchmark

Load testing results: **0% packet loss at 2,000 CPS (28,000 PPS)**.

See [BENCHMARK.md](./docs/BENCHMARK.md) for detailed results, methodology, and optimization notes.

## Integration

### Alerting

Pre-configured alerting examples are available in [ALERTING.md](./docs/ALERTING.md):

- **Prometheus alert rules** — Critical, warning, and info alerts for SER, ISA, RRD, and more
- **Grafana dashboard** — Ready-to-import JSON with carrier-filtered panels
- **Alertmanager examples** — Slack, PagerDuty, and Email integrations
- **Best practices** — Scrape intervals, retention, threshold tuning

### Grafana Dashboard
Import the pre-built dashboard into your Grafana instance:

1. Open Grafana → Dashboards → Import
2. Upload `examples/grafana-dashboard.json` or copy the JSON content
3. Select your Prometheus or VictoriaMetrics datasource

The dashboard includes all available metrics: traffic counters, SIP request/response breakdowns, active sessions, RFC 6076 performance metrics (SER, SEER, ISA, SCR, NER), delay histograms (RRD, TTR, SPD, ORD, LRD), session quality metrics (ISS, ASR, SDC), and system errors.

Dashboard file: [`examples/grafana-dashboard.json`](examples/grafana-dashboard.json)

### Metrics Storage Compatibility

SIP-Exporter exports metrics in Prometheus exposition format, compatible with:

- **Prometheus** — pull-based monitoring
- **VictoriaMetrics** — high-performance time-series database
- **Grafana Cloud** — cloud-based observability
- **Any Prometheus-compatible scraper** — the `/metrics` endpoint follows the standard format

## License
This project is licensed under the **GNU Affero General Public License v3.0 (AGPL-3.0)**.

See [LICENSE](LICENSE) for full text.

### Commercial Use
- ✅ Free for personal and educational use
- ✅ Free for commercial use with conditions
- ⚠️ If you modify and run as a public service, you must open-source your modifications
- 📧 For commercial licensing without AGPL requirements, contact the author

## Changelog
See [CHANGELOG.md](CHANGELOG.md) for version history.