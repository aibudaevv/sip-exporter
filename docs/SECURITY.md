# Security

## Why `--privileged` Is Required

SIP-exporter uses **eBPF** (extended Berkeley Packet Filter) attached to `AF_PACKET` sockets to capture SIP traffic directly in the Linux kernel. This requires three specific capabilities:

| Capability | Why it's needed | Code reference |
|---|---|---|
| `CAP_BPF` (or `CAP_SYS_ADMIN`) | Load eBPF program into kernel via `bpf()` syscall | `exporter.go:120` — `ebpf.LoadCollection()` |
| `CAP_NET_RAW` | Create `AF_PACKET` raw socket (`SOCK_RAW`) | `exporter.go:148` — `unix.Socket(AF_PACKET, SOCK_RAW, ...)` |
| `CAP_NET_ADMIN` | Attach eBPF filter to socket, set socket buffer size | `exporter.go:186` — `SO_ATTACH_BPF`, `exporter.go:155` — `SO_RCVBUF` |

These capabilities are only available to root (`UID 0`), hence `--privileged`.

### Why `network_mode: host`

Packet capture via `AF_PACKET` requires direct access to the host's network interface. Bridge networking would only see the container's virtual interface, not the actual SIP traffic on the physical NIC. There is no workaround for this — it's a fundamental requirement of passive network monitoring.

## What the Container Does with Privileges

The container performs **read-only packet inspection**:

1. **Loads** an eBPF socket filter program into the kernel (once, at startup)
2. **Creates** an `AF_PACKET` raw socket bound to the specified network interface
3. **Reads** packets from the socket into a Go channel (10,000 buffer)
4. **Parses** SIP headers (method, status, Call-ID, From/To tags, CSeq, Session-Expires)
5. **Exports** metrics to Prometheus via `/metrics` HTTP endpoint

That's it. No packet modification, no packet injection, no network redirection, no iptables/nftables rules, no filesystem writes (except stdout/stderr for logs).

## What the Container Does NOT Do

- Does **not** modify or drop packets — the eBPF filter is *passive* (read-only)
- Does **not** send any SIP traffic — purely passive listener
- Does **not** write to the host filesystem — volumes are `:ro` (read-only)
- Does **not** access other containers, processes, or system resources
- Does **not** open any inbound ports except `/metrics` on the configured HTTP port (default 2112)
- Does **not** make outbound network connections

## Minimal Attack Surface

| Layer | Details |
|---|---|
| Base image | `alpine:3.22` — minimal (~5 MB) |
| Runtime dependencies | `libelf` (for eBPF), `bash` (for healthcheck) |
| Application | Single statically-linked Go binary |
| Volumes | `/etc/localtime:ro`, `/etc/timezone:ro` — read-only timezone files |
| Network | Only `/metrics` HTTP endpoint (default port 2112) |
| Processes | Single process, no shell, no daemon |

## eBPF Code Audit

The entire eBPF program is [100 lines of C](../internal/bpf/sip.c). It does one thing: filters packets to only pass through UDP traffic on the configured SIP ports (default 5060/5061).

**What the eBPF program does:**
1. Checks Ethernet header — skips non-Ethernet frames
2. Handles VLAN 802.1Q tags — adjusts offset if present
3. Filters for IPv4 only (`ethertype 0x0800`)
4. Validates IP header length (IHL)
5. Filters for UDP only (`protocol 17`)
6. Reads source and destination ports
7. Passes through **only** packets where src or dst port matches SIP/SIPS port
8. Returns `skb->len` (pass) or `0` (drop from buffer — the packet still reaches its destination, it's just not copied to userspace)

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
