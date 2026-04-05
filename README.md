# SIP-exporter
High-performance eBPF-based SIP monitoring service that captures and exports telephony metrics to Prometheus-compatible systems (Prometheus, VictoriaMetrics, etc.).
Designed for sub-microsecond packet processing with zero-copy capture directly in the Linux kernel.

[![Go Test](https://github.com/aibudaevv/sip-exporter/actions/workflows/go.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/go.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/aibudaevv/sip-exporter)](https://goreportcard.com/report/github.com/aibudaevv/sip-exporter)
[![License](https://img.shields.io/badge/license-AGPL--3.0-blue)](https://github.com/aibudaevv/sip-exporter/blob/main/LICENSE)
[![Issues](https://img.shields.io/github/issues/aibudaevv/sip-exporter)](https://github.com/aibudaevv/sip-exporter/issues)

## Key Features

- ⚡ **Sub-microsecond latency** — eBPF zero-copy packet capture in kernel space
- 🐳 **Single container deployment** — no external dependencies
- 🔧 **Configurable SIP ports** — monitor custom ports via environment variables
- 📈 **Prometheus native** — standard `/metrics` endpoint for scraping

## Quick Start

```yaml
# docker-compose.yml
services:
  sip-exporter:
    image: frzq/sip-exporter:0.5.0
    privileged: true
    network_mode: host
    environment:
      - SIP_EXPORTER_INTERFACE=eth0
```

```bash
docker-compose up -d
curl http://localhost:2112/metrics
```

Access metrics at `http://localhost:2112/metrics`.

## Core Technology

This service uses eBPF (extended Berkeley Packet Filter) attached to network sockets (XDP-like filtering) to
intercept SIP packets (UDP/5060-5061) at L4 without overhead of iptables/nftables or userspace daemons like tcpdump.

## Architecture
```
SIP Traffic → NIC → eBPF socket filter → ringbuf → Go poller → SIP parser → Prometheus
```

## Performance

Go benchmark results (Intel i7-8665U):

| Operation | Latency | Throughput | Memory |
|-----------|---------|------------|--------|
| Packet parsing (L2→SIP) | ~124 ns | 8M pkt/sec | 32 B/op |
| SIP header parsing | ~1.2 μs | 800k pkt/sec | 350 B/op |
| Full processing (with metrics) | ~3 μs | 300k pkt/sec | 1000 B/op |

*Note: Benchmarks measure userspace processing only. Actual latency depends on kernel eBPF overhead and system load.*

## Install

```bash
docker pull frzq/sip-exporter:0.5.0
```

### Configure
Environment variables:
* `SIP_EXPORTER_INTERFACE` - net interface (required)
* `SIP_EXPORTER_HTTP_PORT` - http port for prometheus (default 2112)
* `SIP_EXPORTER_LOGGER_LEVEL` - log level (default info)
* `SIP_EXPORTER_SIP_PORT` - SIP port (default 5060)
* `SIP_EXPORTER_SIPS_PORT` - SIPS port (default 5061)
* `SIP_EXPORTER_OBJECT_FILE_PATH` - path to eBPF object file (default /usr/local/bin/sip.o)

Start docker container in privileged mode is true and host mode.
## Metrics
### Generic SIP traffic metric
`sip_exporter_packets_total`: total number of parsed SIP packets (requests + responses).

### Session metric
`sip_exporter_sessions`: number of active SIP dialogs (RFC 3261).

**How dialogs are counted:**
- A dialog is created when a `200 OK` response is received for an `INVITE` request
- A dialog is identified by the tuple: `{Call-ID, From tag, To tag}`
- A dialog is terminated when a `200 OK` response is received for a `BYE` request
- Dialog ID format: `{call-id}:{min-tag}:{max-tag}` (tags sorted lexicographically)
- Dialogs are cleaned up every 1 second or when expired (based on `Session-Expires` header, default 30 min)

### SIP request metrics
`sip_exporter_publish_total`: total number of received SIP PUBLISH requests.  
`sip_exporter_prack_total`: total number of received SIP PRACK requests.  
`sip_exporter_notify_total`: total number of received SIP NOTIFY requests.  
`sip_exporter_subscribe_total`: total number of received SIP SUBSCRIBE requests.  
`sip_exporter_refer_total`: total number of received SIP REFER requests.  
`sip_exporter_info_total`: total number of received SIP INFO requests.  
`sip_exporter_update_total`: total number of received SIP UPDATE requests.  
`sip_exporter_register_total`: total number of received SIP REGISTER requests.  
`sip_exporter_options_total`: total number of received SIP OPTIONS requests.  
`sip_exporter_cancel_total`: total number of received SIP CANCEL requests.  
`sip_exporter_bye_total`: total number of received SIP BYE requests.  
`sip_exporter_ack_total`: total number of received SIP ACK requests.  
`sip_exporter_invite_total`: total number of received SIP INVITE requests.  
### SIP response metrics (by status code)
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
`sip_exporter_408_total`: total number of SIP 408 Request Timeout responses.  
`sip_exporter_480_total`: total number of SIP 480 Temporarily Unavailable responses.  
`sip_exporter_486_total`: total number of SIP 486 Busy Here responses.  
`sip_exporter_500_total`: total number of SIP 500 Server Internal Error responses.  
`sip_exporter_503_total`: total number of SIP 503 Service Unavailable responses.  
`sip_exporter_600_total`: total number of SIP 600 Busy Everywhere responses.  
`sip_exporter_603_total`: total number of SIP 603 Decline responses.  
### System metrics
`sip_exporter_system_error_total`: total number internal SIP exporter error.

### RFC 6076 Performance Metrics
Metrics defined in [RFC 6076 - Session Initiation Protocol (SIP) Performance Metrics](https://datatracker.ietf.org/doc/html/rfc6076):

#### Session Establishment Ratio (SER)
`sip_exporter_ser`: percentage of successfully established sessions relative to total INVITE attempts.

**Formula (RFC 6076):**
```
SER = (INVITE → 200 OK) / (Total INVITE - INVITE → 3xx) × 100
```

- 3xx responses (redirects) are **excluded from the denominator** — they are neither success nor failure, but a routing instruction
- A session is counted as established only when the originating UA receives `200 OK` for its INVITE
- Undefined when no INVITE requests have been received
- Undefined when all INVITEs received 3xx responses (denominator = 0)

**Important:** SER is a cumulative metric calculated over the entire runtime. Counters (`invite_total`, `200_total`) are never reset and accumulate over time. After sessions end, `sip_exporter_sessions` returns to 0, but SER retains its value based on all processed calls.

To calculate SER over a specific time window, use PromQL with `rate()` or `increase()`:
```promql
# SER over last 5 minutes
(
  increase(sip_exporter_200_total[5m])
  /
  (increase(sip_exporter_invite_total[5m]) - increase(sip_exporter_302_total[5m]))
) * 100
```

**Example values:**
- `100` — all non-redirect INVITEs succeeded
- `0` — all non-redirect INVITEs failed
- `undefined` — no INVITEs or all 3xx

## Development

### Requirements
- Go 1.24+
- Clang/LLVM (for eBPF compilation)
- Linux kernel with eBPF support
- Root privileges (required for eBPF and packet socket)

### Build
```bash
# Build eBPF and Go binary
make build

# Compile eBPF only
make ebpf_compile

# Build Go binary only
make go_build

# Run tests
make test
```

### Test Coverage

| Package | Coverage |
|---------|----------|
| `internal/config` | 100.0% |
| `pkg/log` | 95.5% |
| `internal/server` | 90.5% |
| `internal/service` | 86.5% |
| `internal/exporter` | 61.0% |

Run coverage report:
```bash
go test -cover ./...
```

### Docker
```bash
# Build image
make docker_build

# Run with Docker Compose
docker-compose up -d
```

## E2E Testing

E2E tests use [SIPp](https://sipp.sourceforge.net/) via [testcontainers-go](https://golang.testcontainers.org/) to generate real SIP traffic and verify metrics.

### Requirements
- Docker
- Root privileges (for eBPF and privileged containers)

### Run E2E tests
```bash
# Run all E2E tests
make test-e2e

# Run specific test
make test-e2e-run TEST=TestSER_AllScenarios/100_percent
```

### Test scenarios
| Test | Description | Expected SER |
|------|-------------|--------------|
| `TestSER_AllScenarios/100_percent` | 50 INVITE → 200 OK | 100% |
| `TestSER_AllScenarios/0_percent` | 50 INVITE → 486 Busy Here | 0% |
| `TestSER_AllScenarios/redirect` | 50 INVITE → 302 Redirect | 0% |
| `TestSER_AllScenarios/no_invite` | 50 OPTIONS (no INVITE) | 0% |
| `TestSER_Mixed` | 35 success + 15 rejected | 70% |
| `TestSER_Mixed3xx` | 25 redirect + 25 success | 100% |

All tests verify that `sip_exporter_sessions` returns to 0 after completion (all dialogs properly terminated).

### How it works
1. testcontainers-go builds exporter Docker image from Dockerfile
2. Starts exporter container with eBPF on loopback interface
3. Starts SIPp UAS and UAC containers with `--network=host`
4. SIPp generates SIP traffic through loopback (127.0.0.1:5060)
5. Exporter captures packets via eBPF and updates Prometheus metrics
6. Tests verify SER and sessions cleanup

## Integration

### Grafana Dashboard
Import the pre-built dashboard into your Grafana instance:

1. Open Grafana → Dashboards → Import
2. Upload `examples/grafana-dashboard.json` or copy the JSON content
3. Select your Prometheus datasource

The dashboard includes:
- 📊 Active SIP Sessions (gauge)
- 📈 SER (Session Establishment Ratio) — RFC 6076 metric
- 📈 SIP Packets Rate
- 📈 SIP Requests by Method (INVITE, BYE, REGISTER, etc.)
- 📈 SIP Responses by Status (1xx, 2xx, 4xx, 5xx, 6xx)
- 🚨 System Errors

Dashboard file: [`examples/grafana-dashboard.json`](examples/grafana-dashboard.json)

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