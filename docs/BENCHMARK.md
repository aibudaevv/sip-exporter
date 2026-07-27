# Benchmark Results

Load testing results for sip-exporter, measuring packet capture reliability under high SIP traffic.

## TL;DR

- **Zero packet loss** up to 1,800 CPS (~21,200 PPS) with the measured full SIP dialog lifecycle
- **< 15% CPU** and **9–16 MB RAM** at the measured full-call maximum
- **GC stop-the-world pauses < 1 ms** in the measured workload, leaving substantial headroom relative to the 4 MB socket buffer
- **Multi-NIC:** ~0.25% CPU and ~1 MiB RAM per additional interface
- **Minimum:** 1 core / 128 MB for ≤ 1,000 CPS; 2 cores / 256 MB for ≤ 2,000 CPS

## System Requirements

| Traffic Level | Min CPU | Min RAM | GOMAXPROCS | Notes |
|--------------|---------|---------|------------|-------|
| ≤ 500 CPS | 1 core | 128 MB | 1 | Single-core sufficient |
| ≤ 1,000 CPS | 1 core | 128 MB | 1 | Stable on single core |
| ≤ 2,000 CPS | 2 cores | 256 MB | 2 | Multi-core recommended for stability |
| > 2,000 CPS | 4 cores | 512 MB | 4 | Not tested, conservative estimate |

Key parameters for sizing:

- **CPU:** ~8% of one core at 2,000 CPS on i7-8665U (multi-core)
- **RAM:** 10-15 MB base + ~1 MB per 1,000 active dialogs + ~128 bytes per active RTP stream
- **Network:** eBPF socket filter adds zero latency to SIP/RTP traffic (filters in kernel)
- **Scrape interval:** 5-10 seconds recommended (scrape takes < 10 ms even at max load)

## Reliability: Measured Zero Packet Loss

All scenarios tested with 3 consecutive runs per configuration; **0% loss required on every run**. On loopback each packet is captured twice (send + receive).

| Scenario | What it tests | Max tested | PPS | CPU avg | RAM | Loss |
|----------|---------------|------------|-----|---------|-----|------|
| Full Call Flow | INVITE → 100 → 180 → 200 → ACK → BYE → 200 (14 pkts/call) | 1,800 CPS | ~21,200 | 7.5-11.0% | 9-16 MB | 0.00% |
| INVITE Flood | Raw capture/parse throughput, INVITE-only (2 pkts/call) | 5,000 CPS | ~9,450 | 6.0-6.3% | 12-16 MB | 0.00% |
| Concurrent Sessions | Dialog map scalability & mutex contention | 2,000 dialogs | — | 0.6-0.8% | 12-14 MB | 0.00% |
| VQ Report Flood | VQ PUBLISH parsing throughput (2 pkts/report) | 2,000 CPS | ~3,840 | 3.3% | 13 MB | 0.00% |
| VQ + Response | VQ PUBLISH with 200 OK, bidirectional (4 pkts/report) | 1,000 CPS | ~3,420 | 3.0% | 14 MB | 0.00% |
| Full Call + VQ | Full lifecycle + VQ PUBLISH after BYE (18 pkts/call) | 1,000 CPS | ~15,270 | 6.1% | 16 MB | 0.00% |
| Full Call + RTP | SIP dialog + 4s G.711a RTP both directions | 100 CPS | SIP ~302 / RTP ~199K | 4.8% | 12 MB | 0.00% |

SER (Session Establishment Rate) is 100% in every scenario that completes a full dialog (Full Call Flow, Full Call + VQ, Full Call + RTP). RTP processing adds minimal overhead: at 100 CPS with ~200K RTP packets, CPU stays under 5% avg and SIP metrics are unaffected.

## Methodology

| Parameter | Value |
|-----------|-------|
| OS | Linux (Debian 12 bookworm) |
| Kernel | Linux 6.x (eBPF enabled) |
| Docker | 29.3.1 |
| SIPp | pbertera/sipp:latest |
| Interface | loopback (`lo`) |
| Socket buffer | 4 MB (`SO_RCVBUFFORCE`, falls back to `SO_RCVBUF` without `CAP_NET_ADMIN`) |
| Go | 1.25.12 |

- Tests use [SIPp](https://sipp.sourceforge.net/) via [testcontainers-go](https://golang.testcontainers.org/) to generate real SIP traffic
- Exporter runs as Docker container (`--privileged --network host`) with eBPF on `lo`
- Packet loss is calculated as: `1 - (captured / expected) × 100%`
- Each test runs sequentially (no parallel execution); 3 consecutive runs per configuration, 0% loss required on all runs

## Resource Usage

### CPU & Memory by Rate (Full Call Flow)

| Rate (CPS) | PPS (actual) | CPU avg | CPU peak | RAM | Loss | Stable |
|------------|-------------|---------|----------|-----|------|--------|
| 100 | ~1,190 | 0.8-1.1% | 1.0-1.6% | 8.6-12.7 MB | 0.00% | 3/3 |
| 500 | ~5,880 | 3.4-4.2% | 4.9-5.4% | 9.5-12.6 MB | 0.00% | 3/3 |
| 1,000 | ~11,800 | 5.5-7.0% | 7.7-9.5% | 8.3-14.6 MB | 0.00% | 3/3 |
| 1,200 | ~14,100 | 4.4-8.3% | 7.5-11.5% | 8.6-14.6 MB | 0.00% | 3/3 |
| 1,400 | ~16,500 | 7.0-10.7% | 9.5-12.7% | 9.1-14.7 MB | 0.00% | 3/3 |
| 1,600 | ~18,800 | 8.2-11.8% | 11.0-13.4% | 8.4-16.7 MB | 0.00% | 3/3 |
| 1,800 | ~21,200 | 7.5-11.0% | 9.4-14.6% | 9.0-16.5 MB | 0.00% | 3/3 |

### Scrape Performance Under Load

HTTP GET `/metrics` response time while processing 2,000 CPS (14,000 PPS). 50 scrapes at 100 ms spacing.

| Metric | Value |
|--------|-------|
| Min | 1.7 ms |
| Avg | 4.2 ms |
| P95 | 6.4 ms |
| Max | 8.4 ms |

Scrape does not interfere with packet processing. Safe to scrape every 5-10 seconds even at maximum load.

### Memory Stability & GC

**Memory:** 2-minute continuous run at 500 CPS (7,000 PPS), 840,000 packets processed. Memory starts at 12.8 MB, peaks at 14.4 MB, ends at 12.6 MB — growth rate **-0.09 MB/min (stable)**, CPU avg 4.6% / peak 5.9%. This run shows stable memory after warmup; it is not a proof of leak absence under every workload.

**GC:** Stop-the-world pauses at 2,000 CPS (14,000 PPS), 85 GC cycles over ~5 s of traffic.

| Metric | Value |
|--------|-------|
| Min STW | 0.047 ms |
| Avg STW | 0.149 ms |
| P95 STW | 0.264 ms |
| Max STW | 0.970 ms |

Maximum STW pause is **< 1 ms** in this benchmark. With `SO_RCVBUFFORCE = 4 MB`, this leaves substantial measured headroom relative to the socket buffer; it does not prove that GC can never contribute to packet loss in another deployment.

## Multi-Interface Scaling

Each subtest runs N parallel SIPp UAC flood scenarios (`flood_uac.xml`, 1 INVITE per call, `callCount=1000`, `rate=500` per UAC). The exporter listens on `lo` + (N-1) veth pairs with one AF_PACKET socket per interface; all sockets feed a single Go channel.

| N interfaces | Actual PPS | Packets received | CPU avg | CPU peak | RAM | Loss | Errors |
|---|---|---|---|---|---|---|---|
| 1 (lo) | 373 | 1,000 | 0.51% | 0.77% | 15.82 MB | 0.00% | 0 |
| 2 (lo + veth0a) | 870 | 2,000 | 0.78% | 0.83% | 14.99 MB | 0.00% | 0 |
| 3 (lo + veth0a + veth1a) | 1,043 | 3,000 | 0.76% | 1.23% | 17.13 MB | 0.00% | 0 |

- **Packets scale linearly**: 1,000 → 2,000 → 3,000, zero cross-interface loss. CPU scales sub-linearly (0.51% → 0.78% → 0.76% avg) — the shared parser/channel/pipeline amortises across sockets.
- **Per-NIC cost:** ~0.25% CPU and ~1 MiB RAM per additional interface. No bottleneck up to N=3.

## Reproducing Benchmarks

```bash
# Build Docker image
make docker_build

# Run all load tests (whole package, sequential — includes TestBenchmark_* tests)
SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(cat VERSION) \
  go test -tags=e2e -v -count=1 -timeout 30m ./test/e2e/load/...

# Run specific test
SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(cat VERSION) \
  go test -tags=e2e -v -count=1 -timeout 5m -run 'TestLoad_FullCallFlow/rate_1800' ./test/e2e/load/...

# Run with single core (test scheduler sensitivity)
SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(cat VERSION) SIP_EXPORTER_E2E_GOMAXPROCS=1 \
  go test -tags=e2e -v -count=1 -timeout 30m -run 'TestLoad' ./test/e2e/load/...

# Run with GC trace
SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(cat VERSION) SIP_EXPORTER_E2E_GODEBUG=gctrace=1 \
  go test -tags=e2e -v -count=1 -timeout 5m -run 'TestLoad_FullCallFlow/rate_1800' ./test/e2e/load/...
```

---

<details>
<summary><b>Appendix: Micro-Benchmarks & Detailed Analysis</b></summary>

Developer-focused data: per-operation costs, memory-per-entry breakdowns, and the GOMAXPROCS detail tables.

## GOMAXPROCS Comparison: 1 Core vs 8 Cores

Full Call Flow benchmark comparing single-core vs multi-core execution. 3 runs per configuration.

### GOMAXPROCS=1 (single core)

| Rate (CPS) | PPS (actual) | CPU avg | CPU peak | RAM | Loss | Stable |
|------------|-------------|---------|----------|-----|------|--------|
| 100 | ~1,170 | 0.9-1.1% | 1.1-1.6% | 7.6-12.0 MB | 0.00% | 3/3 |
| 500 | ~5,880 | 2.5-2.8% | 3.5-4.0% | 7.7-11.5 MB | 0.00% | 3/3 |
| 1,000 | ~11,800 | 4.5-5.5% | 6.1-6.6% | 7.5-11.7 MB | 0.00% | 3/3 |
| 1,200 | ~14,100 | 3.4-5.8% | 6.3-7.6% | 7.7-11.6 MB | 0.00% | 3/3 |
| 1,400 | ~16,400 | 5.6-6.4% | 7.2-8.5% | 7.7-9.5 MB | 0.00% | 3/3 |
| 1,600 | ~18,900 | 5.8-6.5% | 8.2-8.9% | 7.5-9.7 MB | 0.00% | 3/3 |
| 1,800 | ~21,000 | 5.5-7.2% | 7.8-9.6% | 7.5-11.6 MB | 0.00% | 2/3 |

### GOMAXPROCS=8 (all cores)

| Rate (CPS) | PPS (actual) | CPU avg | CPU peak | RAM | Loss | Stable |
|------------|-------------|---------|----------|-----|------|--------|
| 100 | ~1,160 | 1.0-1.1% | 1.6-1.9% | 12.0-14.2 MB | 0.00% | 3/3 |
| 500 | ~5,830 | 3.3-3.8% | 4.7-5.7% | 12.4-14.4 MB | 0.00% | 3/3 |
| 1,000 | ~11,800 | 5.9-6.2% | 8.5-8.7% | 12.5-13.1 MB | 0.00% | 3/3 |
| 1,200 | ~14,200 | 5.8-6.5% | 8.4-9.3% | 13.0-17.4 MB | 0.00% | 3/3 |
| 1,400 | ~16,400 | 6.4-7.1% | 9.0-9.8% | 11.7-16.9 MB | 0.00% | 3/3 |
| 1,600 | ~18,800 | 6.3-7.8% | 10.8-11.3% | 12.3-16.0 MB | 0.00% | 3/3 |
| 1,800 | ~21,300 | 6.5-8.9% | 9.9-10.5% | 11.1-16.4 MB | 0.00% | 3/3 |

### Summary

| Metric | GOMAXPROCS=1 | GOMAXPROCS=8 |
|--------|-------------|-------------|
| Max stable CPS | 1,600 (2/3 at 1800) | 1,800 (3/3 all rates) |
| CPU avg @ 1800 CPS | 5.5-7.2% | 6.5-8.9% |
| CPU peak @ 1800 CPS | 7.8-9.6% | 9.9-10.5% |
| RAM @ 1800 CPS | 7.5-11.6 MB | 11.1-16.4 MB |
| RAM overhead | baseline | +50-60% |

Single-core uses less RAM and CPU (no synchronization overhead between goroutines), but is less stable at high rates (1800+ CPS). Multi-core provides stable 0% loss at all rates up to 1800 CPS at the cost of higher resource usage.

## Memory Per Dialog

Memory overhead per active SIP dialog. Dialog map stores `map[string]dialogEntry` — each entry holds `expiresAt`/`createdAt` timestamps plus label metadata (carrier, UA type, source country, Call-ID).

| Active Dialogs | Total RAM | Delta from Baseline | Bytes/Dialog |
|---------------|-----------|--------------------:|-------------:|
| 0 (baseline) | 9.9 MB | — | — |
| 100 | 12.8 MB | 2.8 MB | ~29 KB |
| 403 | 16.6 MB | 6.7 MB | ~17 KB |
| 813 | 14.9 MB | 5.0 MB | ~6 KB |
| 1,627 | 14.9 MB | 5.0 MB | ~3 KB |
| 4,064 | 12.5 MB | 2.5 MB | < 1 KB |

Per-dialog overhead is within GC measurement noise. Even 4,000+ active dialogs add < 7 MB to total memory. The theoretical per-dialog cost is ~144 bytes per `dialogEntry` (two `time.Time` + six `string` fields incl. `destination_country` and `direction` + map bucket overhead), but container-level memory measurement includes Go runtime overhead that obscures per-entry costs.

**Practical conclusion:** dialog storage is negligible. Plan for ~10 MB base + 1-2 MB per 1,000 active dialogs as a conservative estimate.

## Memory Per RTP Stream

Memory overhead per active RTP stream. Each stream stores a `StreamState` struct (jitter, loss, sequence state) wrapped in a `streamEntry` with correlation labels, keyed by media endpoint IP:port + SSRC.

| Active Streams | Total RAM | Delta from Baseline | Bytes/Stream |
|---------------|-----------|--------------------:|-------------:|
| 0 (baseline) | 7.3 MB | — | — |
| 98 | 14.3 MB | 7.0 MB | ~75 KB |
| 204 | 12.4 MB | 5.1 MB | ~26 KB |
| 413 | 14.7 MB | 7.3 MB | ~19 KB |
| 1,030 | 12.2 MB | 4.9 MB | ~5 KB |

Same pattern as dialogs: container-level memory measurement includes Go runtime overhead that dominates at low counts. The theoretical per-stream cost is ~136 bytes for the `StreamState` struct plus ~130 bytes for the `streamEntry` wrapper and map overhead. Streams expire after the configured TTL (default 30s), bounding memory under SSRC reuse.

**Practical conclusion:** RTP stream storage is negligible. Even 1,000+ active streams add < 7 MB to total memory.

## RTP Media Processing Micro-Benchmarks

Per-packet performance of the RTP processing pipeline (header parse + media tracker observe). Measured with `go test -bench` on Intel i7-8665U, 3 runs.

| Benchmark | Time | Allocs | Description |
|-----------|------|--------|-------------|
| `BenchmarkParseHeader` | ~5 ns/op | 0 | RTP header parse (12 bytes → struct) |
| `BenchmarkTracker_Observe_1000Streams` | ~203 ns/op | 0 | Per-packet Observe across 1000 concurrent streams (worst case) |
| `BenchmarkTracker_Snapshot_1000Streams` | ~66 µs/op | 1 (227 KB) | Periodic metrics export (Snapshot over 1000 streams) |

### Throughput Estimate

At ~210 ns/packet end-to-end (parse + observe), the theoretical capacity is ~4.7M RTP pps on a single core. In practice, the SIP/RTP shared channel (10K buffer) and the 1-second snapshot loop are the bottlenecks, not the per-packet cost.

With SIP-vs-RTP channel priority (RTP uses non-blocking send), RTP packets are dropped under extreme load without affecting SIP processing.

### Memory Per RTP Stream

Each active RTP stream stores a `StreamState` struct (~136 bytes) plus `streamEntry` wrapper and map overhead. The Snapshot call allocates a `[]StreamStats` slice proportional to the number of active streams (227 KB for 1000 streams — one allocation).

| Active Streams | Snapshot Allocation | Per-Stream Cost |
|---------------|--------------------:|----------------:|
| 100 | ~23 KB | ~232 bytes |
| 1,000 | ~227 KB | ~232 bytes |
| 10,000 | ~2.3 MB | ~232 bytes |

Streams expire after the configured TTL (default 30s, `SIP_EXPORTER_RTP_STREAM_TTL`), bounding memory under SSRC reuse.

## Geo-Enrichment & Label Resolution Micro-Benchmarks

Per-packet cost of label resolution (`carrier`, `ua_type`, `source_country`, `destination_country`, `caller_host`, `called_host`) including the MaxMind GeoIP lookup. Measured with `go test -bench` on the same i7-8665U (8 logical cores), Go 1.25.12, 3 runs, using the MaxMind GeoLite2-Country **test** database (`test/e2e/data/GeoIP2-Country-Test.mmdb`).

### Packet parse + label resolution (`BenchmarkParseRawPacket_INVITE_Labels`)

Full L2 (Ethernet) → L3 (IPv4) → L4 (UDP) → SIP parse of an INVITE plus label resolution, varying the enrichment path:

| Scenario | Time | Allocs | Memory | Description |
|----------|------|--------|--------|-------------|
| `NoResolver` | ~2.6 µs/op | 11 | 1024 B | Baseline: no carrier, no GeoIP — `source_country="unknown"` |
| `CarrierCountry` | ~2.2 µs/op | 13 | 1056 B | `carrier.country="RU"` set — source resolved from config, no DB lookup |
| `GeoIPLookup` | ~3.4 µs/op | 17 | 1104 B | GeoIP DB lookup of a public IP (`81.2.69.142` → GB) |
| `CarrierCountry_GeoIPLoaded` | ~2.4 µs/op | 13 | 1056 B | `carrier.country` set **and** DB loaded — carrier wins, lookup skipped |

**GeoIP lookup overhead:** ~1.0 µs and +4 allocs per packet when a lookup is actually performed — compare `GeoIPLookup` vs `CarrierCountry_GeoIPLoaded`, which run the identical pipeline and differ only in whether the DB is queried. When `carrier.country` is set, GeoIP is never consulted, so enabling the DB adds **zero** cost for those carriers.

### Prometheus counter cost (`BenchmarkRequest_*`)

Steady-state cost of incrementing the raw INVITE / REGISTER counters (label-value tuples are cached by Prometheus after first use):

| Benchmark | Labels | Time | Allocs | Description |
|-----------|--------|------|--------|-------------|
| `BenchmarkRequest_INVITE` | 6 | ~310 ns/op | 0 | `invite_total` (carrier, ua_type, source_country, destination_country, caller_host, called_host) |
| `BenchmarkRequest_REGISTER` | 3 | ~126 ns/op | 0 | `register_total` (carrier, ua_type, source_country) |
| `BenchmarkInvite200OK` | 6 | ~188 ns/op | 0 | `invite_200_total` |

**Zero allocations** in steady state — Prometheus caches label-value entries. The 3 extra INVITE labels add ~50 ns over the 3-label REGISTER path. At 2,000 CPS this is < 0.1% of one core: the Prometheus layer is not a bottleneck.

### Throughput Estimate

At ~3.4 µs per INVITE (full parse + GeoIP lookup), the exporter's parse/label pipeline alone handles ~290K INVITE/s on a single core. At the tested maximum of 2,000 CPS, label resolution consumes < 1% of one core; enabling GeoIP adds ~0.2% CPU over the `carrier.country` fast path. The real bottlenecks remain the SIP/RTP shared channel and the 1-second snapshot loop, not label resolution.

### How to run

```bash
# Packet parse + GeoIP lookup (skips automatically if the test DB is absent)
go test -run='^$' -bench='BenchmarkParseRawPacket_INVITE_Labels' -benchmem ./internal/exporter/

# Prometheus counter cost
go test -run='^$' -bench='BenchmarkRequest_INVITE|BenchmarkRequest_REGISTER|BenchmarkInvite200OK' \
  -benchmem ./internal/service/

# RTP header parse
go test -run='^$' -bench='BenchmarkParseHeader' -benchmem ./internal/rtp/

# RTP media tracker (observe + snapshot)
go test -run='^$' -bench='BenchmarkTracker_Observe_1000Streams|BenchmarkTracker_Snapshot_1000Streams' \
  -benchmem ./internal/mediatracker/
```

</details>
