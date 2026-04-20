# SIP-exporter
High-performance eBPF-based SIP monitoring service that captures and exports telephony metrics to Prometheus-compatible systems (Prometheus, VictoriaMetrics, etc.).
Captures SIP packets directly in the Linux kernel using eBPF, minimizing userspace processing overhead.

[![Go Test](https://github.com/aibudaevv/sip-exporter/actions/workflows/go.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/go.yml)
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
    # volumes:
    #   - ./examples/carriers.yaml:/etc/sip-exporter/carriers.yaml:ro
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

Start docker container in privileged mode is true and host mode.
## Metrics

All metrics are exposed at `/metrics` in Prometheus exposition format. All SIP metrics include a `carrier` label for per-provider breakdown (configurable via CIDR mapping). The exporter provides:

- **Traffic counters** — SIP request types (INVITE, BYE, REGISTER, etc.) and response status codes (100–603)
- **Active sessions** — real-time count of active SIP dialogs
- **RFC 6076 performance metrics** — SER, SEER, ISA, SCR, NER, RRD, SPD, TTR
- **Extended metrics** — ISS (ineffective session severity), ORD (OPTIONS response delay), LRD (location registration delay), ASR, SDC

Full reference with formulas, examples, and RFC section mapping: [docs/METRICS.md](docs/METRICS.md)

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
- **Grafana dashboard** — Ready-to-import JSON with 21 panels
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