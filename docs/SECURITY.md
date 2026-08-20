# Security

## Why `--privileged` Is Required

SIP-exporter uses **eBPF** (extended Berkeley Packet Filter) attached to `AF_PACKET` sockets to capture SIP traffic directly in the Linux kernel. This requires three specific capabilities:

| Capability | Why it's needed | Code reference |
|---|---|---|
| `CAP_BPF` (or `CAP_SYS_ADMIN`) | Load eBPF program into kernel via `bpf()` syscall | `internal/exporter/exporter.go` — `ebpf.LoadCollection()` |
| `CAP_NET_RAW` | Create `AF_PACKET` raw socket (`SOCK_RAW`) | `internal/exporter/exporter.go` — `unix.Socket(AF_PACKET, SOCK_RAW, ...)` |
| `CAP_NET_ADMIN` | Request a forced socket receive buffer | `internal/exporter/exporter.go` — `SO_RCVBUFFORCE` with `SO_RCVBUF` fallback |

These capabilities are only available to root (`UID 0`), hence `--privileged`.

### Why `network_mode: host`

Packet capture via `AF_PACKET` requires direct access to the host's network interface. Bridge networking would only see the container's virtual interface, not the actual SIP traffic on the physical NIC. There is no workaround for this — it's a fundamental requirement of passive network monitoring.

## What the Container Does with Privileges

The container performs **read-only packet inspection**:

1. **Loads** an eBPF socket filter program into the kernel (once, at startup)
2. **Creates** an `AF_PACKET` raw socket bound to the specified network interface
3. **Reads** packets from the socket into a Go channel (10,000 buffer)
4. **Parses** SIP headers (method, status, Call-ID, From/To tags, CSeq, Session-Expires), fixed RTP headers (12 bytes: version, payload type, sequence, timestamp, SSRC), and RTCP report blocks
5. **Exports** metrics to Prometheus via `/metrics` HTTP endpoint

That's it. No packet modification, no packet injection, no network redirection, no iptables/nftables rules. (The exporter does make one outbound HTTPS telemetry beacon and write one telemetry ID file — see [Telemetry](#telemetry) below.)

> **Multi-interface note:** A separate BPF collection is loaded per interface at startup; each program is attached to its own `AF_PACKET` socket via `SO_ATTACH_BPF`. Maps `sip_ports` and `rtp_endpoints` are per-collection — each interface has independent filtering rules.

## What the Container Does NOT Do

- Does **not** modify or drop packets — the eBPF filter is *passive* (read-only)
- Does **not** send any SIP traffic — purely passive listener
- Does **not** persist packet data in application-managed files. The production Compose example writes only the anonymous telemetry ID to the `sip-exporter-state` volume; configuration mounts are `:ro` (read-only). Container logging can persist sensitive diagnostics; see [Logging and Sensitive Identifiers](#logging-and-sensitive-identifiers)
- Does **not** access other containers, processes, or system resources
- Does **not** open any inbound ports except the `/metrics` and `/health` endpoints on the configured HTTP port (default 10047)
- Does **not** make outbound network connections **except** the optional telemetry beacon (see [Telemetry](#telemetry))

## Telemetry

By default, SIP-exporter sends an **anonymous telemetry beacon** to the project maintainers so installations can be counted. This is **enabled by default** (`SIP_EXPORTER_TELEMETRY=true`) and runs as a background goroutine started unconditionally at startup (`cmd/main.go`).

- **Endpoint:** HTTPS GET to `https://telemetry.sip-exporter.com/v1/beacon` (`SIP_EXPORTER_TELEMETRY_URL`).
- **Frequency:** once at startup, then every **24 hours** (`internal/telemetry/telemetry.go`).
- **Payload** (URL query parameters, `internal/telemetry/beacon.go`):
  - `anon_id` — a persistent random UUID v4 (see below), **not** derived from any host, user, or network identifier
  - `version` — exporter build version
  - `os` — `runtime.GOOS` (e.g. `linux`)
  - `arch` — `runtime.GOARCH` (e.g. `amd64`)
  - `uptime` — process uptime in seconds
- **Persistent ID file:** the anonymous ID is stored at `/var/lib/sip-exporter/anon_id` (`SIP_EXPORTER_TELEMETRY_ID_FILE`), mode `0600`, and reused across restarts. The file contains only the UUID plus a short explanatory comment; deleting it regenerates a new random ID. No phone numbers, IPs, hostnames, or SIP/RTP data are ever sent.

**To disable telemetry entirely:** `SIP_EXPORTER_TELEMETRY=false`. To redirect beacons to your own endpoint (e.g. for air-gapped auditing), set `SIP_EXPORTER_TELEMETRY_URL`. To change the ID file location, set `SIP_EXPORTER_TELEMETRY_ID_FILE`.

## Logging and Sensitive Identifiers

The default `info` level does not log raw packet payloads, but FAS warnings include the SIP Call-ID. At `debug`, diagnostic records include raw SIP message data and may include Call-IDs and media endpoint IP addresses and ports. These records go to stdout/stderr rather than the `sip-exporter-state` volume, but Docker or an orchestrator logging driver may retain them.

In production, use `SIP_EXPORTER_LOGGER_LEVEL=info` (the default) or `error`, restrict access to collected logs, and apply an appropriate retention policy. Enable `debug` only in a controlled troubleshooting window. The accepted values are `error`, `info`, and `debug`; any other value currently selects debug logging.

## Data Exposed in Prometheus Labels

No **phone numbers** ever reach Prometheus labels. The privacy-relevant packet and infrastructure labels are listed below; see the [Metrics label matrix](METRICS.md#labels) for the complete schema of every metric family.

| Label | Derived from | Scope | Example |
|---|---|---|---|
| `carrier` | CIDR match of source IP against `carriers.yaml` | Most SIP, RTP, and correlated RTCP metrics | `telecom-alpha` |
| `ua_type` | User-Agent classification (`user_agents.yaml`) | Base/call-level SIP, RTP, and correlated RTCP metrics | `yealink` |
| `source_country` | `carrier.country` → MaxMind GeoIP(src IP) → `unknown` | Base/call-level SIP, RTP, and correlated RTCP metrics | `RU` |
| `destination_country` | E.164 prefix of the called number (embedded table) | INVITE metrics only | `US` |
| `caller_host`, `called_host` | Host part of the From/To SIP URI | INVITE metrics only (**opt-in**, default off) | `10.0.0.5`, `sip.example.com` |
| `codec` | RTP payload type / SDP `a=rtpmap` | RTP and correlated RTCP quality metrics | `G.711` |
| `direction` | Kernel packet type | SIP and RTP traffic metrics | `inbound` |
| `iface` | Configured capture-interface name | Raw INVITE and socket self-monitoring metrics | `ens3` |

**Infrastructure identifiers, opt-in by default:** `caller_host`/`called_host` are hostnames or IP addresses extracted from the SIP `From`/`To` URI — they identify network endpoints, not subscribers. Because the number of distinct endpoints is unbounded, these labels are **disabled by default** (`SIP_EXPORTER_HOST_LABELS=false`): when off, they collapse to the empty value, so they add **zero cardinality** and leak no endpoint identifiers. Enable them (`SIP_EXPORTER_HOST_LABELS=true`) only on trusted, bounded deployments — otherwise a flood of spoofed `From`/`To` hosts could grow Prometheus memory; if you enable them in a less-trusted environment, monitor `prometheus_tsdb_symbol_table_size`.

**GeoIP is optional:** without `SIP_EXPORTER_GEOIP_COUNTRY_DB` (and no `carrier.country` configured), `source_country` is `"unknown"` — zero additional cardinality, no data leaves the host. `destination_country` needs **no database**: the E.164 prefix table is embedded in the binary at compile time.

## Minimal Attack Surface

| Layer | Details |
|---|---|
| Base image | `alpine:3.22` — minimal (~5 MB) |
| Runtime packages | `libelf`, `bash`, `libssl3`, and `libcrypto3`; the healthcheck executes BusyBox `wget` |
| Application | Single statically linked Go binary plus the eBPF object file |
| Volumes | Writable `sip-exporter-state:/var/lib/sip-exporter` for the telemetry ID; optional configuration and timezone mounts are read-only |
| Network | Inbound `/metrics` and `/health` HTTP endpoints (default port 10047); optional outbound telemetry |
| Processes | A single application process; no shell or daemon process is started |

> **Security note — unauthenticated endpoints:** `/metrics` and `/health` are registered without any authentication or authorization middleware (`internal/server/server.go:102-103`). Anyone who can reach port `10047` can read all exported metrics. The current service listens on all interfaces and does not provide a bind-address setting; on untrusted networks, firewall port `10047` or place a reverse proxy with authentication in front of it.

## eBPF Code Audit

The eBPF program is [~166 lines of C](../internal/bpf/sip.c). It does two things: (1) passes through IPv4 UDP traffic on the configured SIP port (default 5060), and (2) passes through RTP/RTCP packets from media endpoints learned via SDP signaling (INVITE 200 OK).

**What the eBPF program does:**
1. Checks Ethernet header — skips non-Ethernet frames
2. Handles VLAN 802.1Q tags — adjusts offset if present
3. Filters for IPv4 only (`ethertype 0x0800`)
4. Validates IP header length (IHL)
5. Filters for UDP only (`protocol 17`)
6. Reads source and destination ports
7. Passes through packets where src or dst port matches a configured SIP port
8. For matching media endpoints, returns up to 64 bytes for RTP and up to 1518 bytes for RTCP compounds; non-matching UDP is not copied to userspace
9. Returns `skb->len` (SIP pass), a capped media snapshot, or `0` (drop from buffer — the packet still reaches its destination, it's just not copied to userspace)

**RTP privacy:** The application parses only the 12-byte fixed RTP header (version, PT, sequence, timestamp, SSRC), but the 64-byte capture snapshot can include a small payload prefix. It does not inspect or persist audio. RTCP compounds are copied up to 1518 bytes to parse report blocks. RTP metrics expose only `carrier`, `ua_type`, `codec`, `source_country`, and `direction` labels — no phone numbers, raw IPs, or call identifiers appear in RTP metrics. Privacy-relevant labels are documented in [Data Exposed in Prometheus Labels](#data-exposed-in-prometheus-labels); the complete per-family schema is in the [Metrics label matrix](METRICS.md#labels).

**Critical point:** The eBPF filter is a *socket filter*, not a *tc/XDP filter*. It only controls which packets are copied to the application's socket buffer. Dropped packets are **not** lost — they continue through the normal network stack to their destination. The filter cannot modify or block traffic.

## Industry Standard

Running privileged for eBPF-based observability is standard practice:

| Project | What it does | Privileged? |
|---|---|---|
| [Cilium](https://github.com/cilium/cilium) | eBPF networking & security | Yes |
| [Falco](https://github.com/falcosecurity/falco) | eBPF system call monitoring | Yes |
| [Pixie](https://github.com/pixie-io/pixie) | eBPF Kubernetes observability | Yes |
| [kubectl-trace](https://github.com/iovisor/kubectl-trace) | eBPF tracing | Yes |
| [Parca](https://github.com/parca-dev/parca) | eBPF continuous profiling | Yes |
| **SIP-exporter** | eBPF SIP traffic monitoring | Yes |

All eBPF-based tools require `CAP_BPF` / `CAP_SYS_ADMIN` to load programs into the kernel. This is a kernel-level security boundary, not a container-level one.

## Automated Vulnerability Scanning

All code and container images are automatically scanned for known vulnerabilities:

| Scanner | What it checks | Frequency |
|---|---|---|
| [Go Vulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) | Go dependencies against Go Vulnerability Database | Every push + daily |
| [Trivy](https://trivy.dev) | Container image (OS packages + Go binaries) against CVE databases | Every push + daily |

Results are uploaded to the [GitHub Security tab](https://github.com/aibudaevv/sip-exporter/security).

### Local Scanning

Run security checks before pushing:

```bash
make security      # govulncheck + trivy filesystem scan (fast, no Docker build)
make trivy-image   # full container image scan (requires Docker build)
```

**Prerequisites:** `govulncheck` (`go install golang.org/x/vuln/cmd/govulncheck@latest`) and `trivy` (see [installation guide](https://trivy.dev/latest/getting-started/installation/)).

## Source Code

The project is open-source under [AGPL-3.0](../LICENSE). Every line of code — including the eBPF kernel program — is available for audit:

- eBPF filter: [`internal/bpf/sip.c`](../internal/bpf/sip.c)
- Packet parsing: [`internal/exporter/exporter.go`](../internal/exporter/exporter.go)
- All source: [GitHub repository](https://github.com/aibudaevv/sip-exporter)
