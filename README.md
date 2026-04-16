# SIP-exporter
High-performance eBPF-based SIP monitoring service that captures and exports telephony metrics to Prometheus-compatible systems (Prometheus, VictoriaMetrics, etc.).
Captures SIP packets directly in the Linux kernel using eBPF, minimizing userspace processing overhead.

[![Go Test](https://github.com/aibudaevv/sip-exporter/actions/workflows/go.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/go.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/aibudaevv/sip-exporter)](https://goreportcard.com/report/github.com/aibudaevv/sip-exporter)
[![License](https://img.shields.io/badge/license-AGPL--3.0-blue)](https://github.com/aibudaevv/sip-exporter/blob/main/LICENSE)
[![Issues](https://img.shields.io/github/issues/aibudaevv/sip-exporter)](https://github.com/aibudaevv/sip-exporter/issues)

## Key Features

- ⚡ **Low overhead** — eBPF packet filtering in kernel space
- 🐳 **Single container deployment** — no external dependencies
- 🔧 **Configurable SIP ports** — monitor custom ports via environment variables
- 📈 **Prometheus native** — standard `/metrics` endpoint for scraping

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
```

```bash
docker-compose up -d
curl http://localhost:2112/metrics
```

Access metrics at `http://localhost:2112/metrics`.

## Core Technology

This service uses eBPF (extended Berkeley Packet Filter) attached to network sockets to
intercept SIP packets (UDP/5060-5061) at L4 without overhead of iptables/nftables or userspace daemons like tcpdump.
Packets are transferred to userspace via Linux ringbuf for efficient processing.

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

**Important:** SER is a cumulative metric calculated over the entire runtime.

#### Session Establishment Effectiveness Ratio (SEER)
`sip_exporter_seer`: percentage of "effective" INVITE responses relative to total non-redirected INVITE attempts.

**Formula (RFC 6076):**
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

#### Ineffective Session Attempts (ISA)
`sip_exporter_isa`: percentage of INVITE requests that resulted in server error or timeout responses.

**Formula (RFC 6076, Section 4.8):**
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

#### Session Completion Ratio (SCR)
`sip_exporter_scr`: percentage of INVITE sessions that were fully completed (established and terminated) relative to total INVITE attempts.

**Formula (RFC 6076):**
```
SCR = (Completed Sessions) / Total INVITE × 100
```

- Unlike SER/SEER, 3xx responses are **NOT excluded from the denominator** — SCR measures end-to-end session completion
- A session is counted as "completed" only when:
  1. `200 OK` was received for an `INVITE` (session established)
  2. `200 OK` was received for a `BYE` (session terminated)
- 3xx responses are counted as non-completed — they never result in a session
- Undefined when no INVITE requests have been received

**Important:** SCR is cumulative over the entire runtime.

**Relationship with SER:** SCR is always <= SER, since only a subset of established sessions are fully completed (terminated with BYE). The gap between SER and SCR indicates the proportion of established sessions that are still active or were abandoned without BYE.

**Example values:**
- `100` — all INVITEs resulted in fully completed sessions (INVITE→200 OK + BYE→200 OK)
- `50` — half of all INVITEs resulted in complete call cycles
- `0` — no sessions were fully completed (either no answers or no BYE sent)

#### Registration Request Delay (RRD)
`sip_exporter_rrd`: average delay in milliseconds between sending a REGISTER request and receiving a 200 OK response.

**Formula (RFC 6076, Section 4.1):**
```
RRD = Average(Time of 200 OK - Time of REGISTER request)
```

- Measures the round-trip time for SIP registration transactions
- Only successful registrations (200 OK responses) are measured
- Returns 0 when no successful registrations have occurred
- The metric is cumulative — it shows the average delay over the entire runtime

**Important:** RRD measures registration latency, not call setup latency. Use SER/SEER for call establishment metrics.

**Example values:**
- `< 100 ms` — excellent registration performance (local network)
- `100-500 ms` — acceptable performance (typical WAN)
- `> 1000 ms` — potential issues (network congestion, server overload)

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
# Build Docker image and run all E2E tests
make test-e2e

# Run specific test (image is built automatically)
make test-e2e-run TEST=TestISA_AllScenarios
make test-e2e-run TEST=TestSCR_AllScenarios
make test-e2e-run TEST=TestSEER_AllScenarios
make test-e2e-run TEST=TestSER_AllScenarios

# Run specific subtest
make test-e2e-run TEST=TestSER_AllScenarios/100_percent
```

### Configuration
| Variable | Default | Description |
|----------|---------|-------------|
| `SIP_EXPORTER_E2E_SIPP_VERBOSE` | `false` | Show SIPp container output |
| `SIP_EXPORTER_E2E_EXPORTER_VERBOSE` | `false` | Show exporter debug logs |

```bash
# Quiet mode (default) — one line per test
make test-e2e

# Verbose mode — full SIPp + exporter logs
SIP_EXPORTER_E2E_SIPP_VERBOSE=true SIP_EXPORTER_E2E_EXPORTER_VERBOSE=true make test-e2e
```

### How it works
1. Makefile builds exporter Docker image from Dockerfile (dependency: `docker_build`)
2. Image name is passed to tests via `SIP_EXPORTER_E2E_IMAGE` environment variable
3. Tests start exporter container with eBPF on loopback interface (`--privileged --network host`)
4. SIPp UAS and UAC containers are started with `--network=host`
5. SIPp generates SIP traffic through loopback (127.0.0.1:5060)
6. Exporter captures packets via eBPF and updates Prometheus metrics
7. Tests verify ISA/SCR/SEER/SER metrics and sessions cleanup

## Integration

### Alerting

Pre-configured alerting examples are available in [ALERTING.md](./ALERTING.md):

- **Prometheus alert rules** — Critical, warning, and info alerts for SER, ISA, RRD, and more
- **Grafana dashboard** — Ready-to-import JSON with 8 panels
- **Alertmanager examples** — Slack, PagerDuty, and Email integrations
- **Best practices** — Scrape intervals, retention, threshold tuning

### Grafana Dashboard
Import the pre-built dashboard into your Grafana instance:

1. Open Grafana → Dashboards → Import
2. Upload `examples/grafana-dashboard.json` or copy the JSON content
3. Select your Prometheus or VictoriaMetrics datasource

The dashboard includes:
- 📊 Active SIP Sessions (gauge)
- 📈 SER (Session Establishment Ratio) — RFC 6076 metric
- 📈 SEER (Session Establishment Effectiveness Ratio) — RFC 6076 metric
- 📈 ISA (Ineffective Session Attempts) — RFC 6076 metric
- 📈 SCR (Session Completion Ratio) — RFC 6076 metric
- 📈 SIP Packets Rate
- 📈 SIP Requests by Method (INVITE, BYE, REGISTER, etc.)
- 📈 SIP Responses by Status (1xx, 2xx, 4xx, 5xx, 6xx)
- 🚨 System Errors

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