# SIP-exporter
High-performance eBPF-based SIP monitoring service that captures and exports telephony metrics to Prometheus.
Designed for sub-microsecond packet processing with zero-copy capture directly in the Linux kernel.

[![Go Test](https://github.com/aibudaevv/sip-exporter/actions/workflows/go.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/go.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/aibudaevv/sip-exporter)](https://goreportcard.com/report/github.com/aibudaevv/sip-exporter)
[![License](https://img.shields.io/badge/license-AGPL--3.0-blue)](https://github.com/aibudaevv/sip-exporter/blob/main/LICENSE)
[![Issues](https://img.shields.io/github/issues/aibudaevv/sip-exporter)](https://github.com/aibudaevv/sip-exporter/issues)

### Core Technology: eBPF
This service uses eBPF (extended Berkeley Packet Filter) attached to network sockets (XDP-like filtering) to
intercept SIP packets (UDP/5060-5061) at L4 without overhead of iptables/nftables or userspace daemons like tcpdump.

### Architecture
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
`docker pull frzq/sip-exporter:0.4.0`
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
`sip_exporter_sessions`: active sip dialogs. (unique session it key call-id:from.tag:to.tag) 

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
The project has comprehensive MC/DC test coverage:

| Package | Coverage |
|---------|----------|
| `internal/config` | 100.0% |
| `internal/service` | 100.0% |
| `pkg/log` | 95.5% |
| `internal/server` | 90.5% |
| `internal/exporter` | 60.2% |

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

## Integration

### Grafana Dashboard
Import the pre-built dashboard into your Grafana instance:

1. Open Grafana → Dashboards → Import
2. Upload `examples/grafana-dashboard.json` or copy the JSON content
3. Select your Prometheus datasource

The dashboard includes:
- 📊 Active SIP Sessions (gauge)
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