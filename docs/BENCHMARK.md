# Benchmark Results

Load testing results for sip-exporter, measuring packet capture reliability under high SIP traffic.

## Test Environment

| Parameter | Value |
|-----------|-------|
| OS | Linux (Debian 12 bookworm) |
| Kernel | Linux 6.x (eBPF enabled) |
| Docker | 29.3.1 |
| SIPp | pbertera/sipp:latest |
| Interface | loopback (`lo`) |
| Socket buffer | 4 MB (`SO_RCVBUFFORCE`, falls back to `SO_RCVBUF` without `CAP_NET_ADMIN`) |
| Go | 1.25.11 |

## Methodology

- Tests use [SIPp](https://sipp.sourceforge.net/) via [testcontainers-go](https://golang.testcontainers.org/) to generate real SIP traffic
- Exporter runs as Docker container (`--privileged --network host`) with eBPF on `lo`
- On loopback each packet is captured twice (send + receive)
- Packet loss is calculated as: `1 - (captured / expected) × 100%`
- Each test runs sequentially (no parallel execution) to ensure isolated measurements
- 3 consecutive runs per configuration, 0% loss required on all runs

## Results: Full Call Flow (FullCallFlow)

Complete SIP dialog lifecycle: INVITE → 100 → 180 → 200 → ACK → BYE → 200 (14 packets per call on loopback).

| Rate (CPS) | PPS (actual) | CPU avg | CPU peak | RAM | Loss | Stable |
|------------|-------------|---------|----------|-----|------|--------|
| 100 | ~1,190 | 0.8-1.1% | 1.0-1.6% | 8.6-12.7 MB | 0.00% | 3/3 |
| 500 | ~5,880 | 3.4-4.2% | 4.9-5.4% | 9.5-12.6 MB | 0.00% | 3/3 |
| 1,000 | ~11,800 | 5.5-7.0% | 7.7-9.5% | 8.3-14.6 MB | 0.00% | 3/3 |
| 1,200 | ~14,100 | 4.4-8.3% | 7.5-11.5% | 8.6-14.6 MB | 0.00% | 3/3 |
| 1,400 | ~16,500 | 7.0-10.7% | 9.5-12.7% | 9.1-14.7 MB | 0.00% | 3/3 |
| 1,600 | ~18,800 | 8.2-11.8% | 11.0-13.4% | 8.4-16.7 MB | 0.00% | 3/3 |
| 1,800 | ~21,200 | 7.5-11.0% | 9.4-14.6% | 9.0-16.5 MB | 0.00% | 3/3 |
| 2,000 | ~23,600 | 6.0-11.3% | 9.6-15.1% | 8.6-16.6 MB | 0.00% | 3/3 |

## Results: INVITE Flood (raw PPS)

INVITE-only flood without responses. Tests maximum throughput of packet capture and parsing pipeline (2 packets per call on loopback).

| Rate (CPS) | PPS (actual) | CPU avg | CPU peak | RAM | Loss |
|------------|-------------|---------|----------|-----|------|
| 100 | ~190 | 0.4% | 0.5% | 9.0-9.4 MB | 0.00% |
| 500 | ~960 | 0.7-0.9% | 0.8-1.0% | 12.1-14.3 MB | 0.00% |
| 1,000 | ~1,900 | 1.1-1.5% | 1.3-1.5% | 12.3 MB | 0.00% |
| 2,000 | ~3,850 | 2.0-2.9% | 2.1-3.6% | 12.3-12.8 MB | 0.00% |
| 5,000 | ~9,450 | 6.0-6.3% | 6.3-6.6% | 12.4-16.0 MB | 0.00% |

## Results: Concurrent Sessions

Many simultaneous SIP dialogs with low call rate. Tests dialog map scalability and mutex contention.

| Concurrent | Calls | Duration | CPU avg | CPU peak | RAM | Loss |
|------------|-------|----------|---------|----------|-----|------|
| 500 | 1,000 | ~66s | 0.2% | 1.2-1.5% | 9.8-12.2 MB | 0.00% |
| 1,000 | 2,000 | ~71s | 0.4% | 1.2-1.6% | 11.2-13.8 MB | 0.00% |
| 2,000 | 4,000 | ~81s | 0.6-0.8% | 1.2-2.2% | 12.2-13.7 MB | 0.00% |

## Results: VQ Report Flood

VQ PUBLISH flood without responses. Tests VQ report parsing throughput (2 packets per report on loopback).

| Rate (CPS) | PPS (actual) | CPU avg | CPU peak | RAM | Loss |
|------------|-------------|---------|----------|-----|------|
| 100 | ~190 | 0.52% | 0.68% | 14.4 MB | 0.00% |
| 500 | ~960 | 1.03% | 1.32% | 13.5 MB | 0.00% |
| 1,000 | ~1,930 | 2.16% | 2.92% | 16.0 MB | 0.00% |
| 2,000 | ~3,840 | 3.28% | 3.54% | 13.0 MB | 0.00% |

## Results: VQ High Rate with Response

VQ PUBLISH with 200 OK responses. Tests VQ report parsing under bidirectional traffic (4 packets per call on loopback).

| Rate (CPS) | PPS (actual) | CPU avg | CPU peak | RAM | Loss |
|------------|-------------|---------|----------|-----|------|
| 100 | ~340 | 0.60% | 0.80% | 15.0 MB | 0.00% |
| 500 | ~1,710 | 1.56% | 2.36% | 15.4 MB | 0.00% |
| 1,000 | ~3,420 | 2.99% | 4.22% | 14.1 MB | 0.00% |

## Results: Full Call with VQ Report

Complete SIP dialog lifecycle + VQ PUBLISH after BYE: INVITE → 100 → 180 → 200 → ACK → BYE → 200 → PUBLISH → 200 (18 packets per call on loopback).

| Rate (CPS) | PPS (actual) | CPU avg | CPU peak | RAM | Loss | SER |
|------------|-------------|---------|----------|-----|------|-----|
| 100 | ~1,530 | 1.49% | 2.04% | 12.7 MB | 0.00% | 100% |
| 500 | ~7,590 | 3.98% | 6.57% | 15.3 MB | 0.00% | 100% |
| 1,000 | ~15,270 | 6.11% | 8.45% | 15.6 MB | 0.00% | 100% |

## Results: Full Call with RTP Media

Complete SIP dialog + 4s G.711a RTP in both directions (INVITE → 100 → 200 → ACK → RTP → BYE → 200). Each call generates ~6 SIP packets + ~400 RTP packets. Rates are 10× lower than SIP-only due to RTP volume.

| Rate (CPS) | SIP PPS | RTP Packets | CPU avg | CPU peak | RAM | SIP Loss | SER |
|------------|---------|-------------|---------|----------|-----|----------|-----|
| 10 | ~30 | ~20K | 0.86% | 1.91% | 12.4 MB | 0.00% | 100% |
| 25 | ~76 | ~50K | 1.50% | 3.23% | 11.7 MB | 0.00% | 100% |
| 50 | ~151 | ~100K | 2.20% | 4.92% | 13.0 MB | 0.00% | 100% |
| 100 | ~302 | ~199K | 4.79% | 8.57% | 12.0 MB | 0.00% | 100% |

RTP processing adds minimal CPU overhead. At 100 CPS with ~200K RTP packets, CPU stays under 5% avg. SIP metrics (SER, packet loss) are unaffected by RTP capture.

## Results: Multi-Interface Scaling (N=1/2/3)

Verifies that scaling from 1 to N network interfaces is **linear** on packet throughput and **sub-linear** on CPU/memory (the shared channel, pool, and trackers do not become a bottleneck). Each subtest runs N parallel SIPp UAC flood scenarios (`flood_uac.xml`, 1 INVITE per call, `callCount=1000`, `rate=500` per UAC). The exporter listens on `lo` + (N-1) veth pairs with one AF_PACKET socket per interface; all sockets feed a single Go channel.

| N interfaces | Actual PPS | Packets received | CPU avg | CPU peak | RAM | Loss | Errors |
|---|---|---|---|---|---|---|---|
| 1 (lo) | 373 | 1,000 | 0.51% | 0.77% | 15.82 MB | 0.00% | 0 |
| 2 (lo + veth0a) | 870 | 2,000 | 0.78% | 0.83% | 14.99 MB | 0.00% | 0 |
| 3 (lo + veth0a + veth1a) | 1,043 | 3,000 | 0.76% | 1.23% | 17.13 MB | 0.00% | 0 |

**Analysis:**

- **Packets received** scales **exactly linearly**: 1,000 → 2,000 → 3,000. Each interface delivers its 1,000 INVITEs independently with zero cross-interface loss.
- **Actual PPS** scales **near-linearly**: N=2 = 2.33× N=1, N=3 = 2.80× N=1. The sub-1.0× ratio at N=3 reflects SIPp container startup amortisation, not exporter saturation (CPU stays under 1.3% peak).
- **CPU** scales **sub-linearly**: 0.51% → 0.78% → 0.76% avg (N=3 ≈ N=2 due to measurement granularity). The shared parser/channel/pipeline amortises across sockets — one BPF program, one Go channel, one tracker map set.
- **Memory** stays flat at ~15-17 MB across all N (kernel-side receive buffers add ~4 MiB per socket, but userspace RSS does not grow proportionally).
- **Zero packet loss, zero errors** at every N — the AF_PACKET → channel → parser pipeline has no contention point at N≤3.

**Conclusion:** multi-interface capture adds ~0.25% CPU and ~1 MiB RAM per additional NIC. The shared infrastructure (single BPF collection, single Go channel, single set of trackers) is **not** a bottleneck — sub-linear CPU/memory scaling is preserved up to N=3.

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
| 2,000 | ~23,600 | 5.0-7.1% | 7.1-9.2% | 7.6-12.1 MB | 0.00% | 3/3 |

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
| 2,000 | ~23,600 | 6.7-8.6% | 10.2-12.2% | 11.9-15.3 MB | 0.00% | 3/3 |

### Summary

| Metric | GOMAXPROCS=1 | GOMAXPROCS=8 |
|--------|-------------|-------------|
| Max stable CPS | 1,600 (2/3 at 1800) | 2,000 (3/3 all rates) |
| CPU avg @ 2000 CPS | 5.0-7.1% | 6.7-8.6% |
| CPU peak @ 2000 CPS | 7.1-9.2% | 10.2-12.2% |
| RAM @ 2000 CPS | 7.6-12.1 MB | 11.9-15.3 MB |
| RAM overhead | baseline | +50-60% |

Single-core uses less RAM and CPU (no synchronization overhead between goroutines), but is less stable at high rates (1800+ CPS). Multi-core provides stable 0% loss at all rates up to 2000 CPS at the cost of higher resource usage.

## Scrape Performance Under Load

HTTP GET `/metrics` response time while processing 2000 CPS (14,000 PPS). 50 sequential scrapes at 100ms intervals.

| Metric | Value |
|--------|-------|
| Min | 1.7 ms |
| Avg | 4.2 ms |
| P95 | 6.4 ms |
| Max | 8.4 ms |

Scrape does not interfere with packet processing. Safe to scrape every 5-10 seconds even at maximum load.

## Memory Stability

2-minute continuous run at 500 CPS (7,000 PPS). Measures memory growth over time.

| Metric | Value |
|--------|-------|
| Duration | 2 min |
| Packets processed | 840,000 |
| CPU avg | 4.6% |
| CPU peak | 5.9% |
| Memory min | 11.6 MB |
| Memory max | 14.4 MB |
| Memory first sample | 12.8 MB |
| Memory last sample | 12.6 MB |
| Growth rate | -0.09 MB/min (stable) |

No memory leaks detected. Memory stabilizes after initial warmup and remains flat throughout the run.

## GC Impact

Go GC stop-the-world pauses measured at 2000 CPS (14,000 PPS). 85 GC cycles observed during ~5 seconds of traffic.

| Metric | Value |
|--------|-------|
| GC cycles | 85 |
| Min STW | 0.047 ms |
| Avg STW | 0.149 ms |
| P95 STW | 0.264 ms |
| Max STW | 0.970 ms |

Maximum STW pause is **< 1 ms**. With `SO_RCVBUFFORCE = 4 MB` (~420 ms buffer at 28K PPS), GC pauses are 400× smaller than the socket buffer capacity — packets are never lost due to GC.

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

Per-dialog overhead is within GC measurement noise. Even 4,000+ active dialogs add < 7 MB to total memory. The theoretical per-dialog cost is ~112 bytes per `dialogEntry` (two `time.Time` + four `string` fields + map bucket overhead), but container-level memory measurement includes Go runtime overhead that obscures per-entry costs.

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

Same pattern as dialogs: container-level memory measurement includes Go runtime overhead that dominates at low counts. The theoretical per-stream cost is ~96 bytes for the `StreamState` struct plus ~130 bytes for the `streamEntry` wrapper and map overhead. Streams expire after the configured TTL (default 30s), bounding memory under SSRC reuse.

**Practical conclusion:** RTP stream storage is negligible. Even 1,000+ active streams add < 7 MB to total memory.

## RTP Media Processing Micro-Benchmarks

Per-packet performance of the RTP processing pipeline (header parse + media tracker observe). Measured with `go test -bench` on Intel i7-8665U, 3 runs.

| Benchmark | Time | Allocs | Description |
|-----------|------|--------|-------------|
| `BenchmarkParseHeader` | ~5 ns/op | 0 | RTP header parse (12 bytes → struct) |
| `BenchmarkTracker_Observe_1000Streams` | ~203 ns/op | 0 | Per-packet Observe across 1000 concurrent streams (worst case) |
| `BenchmarkTracker_Snapshot_1000Streams` | ~66 µs/op | 1 (128 KB) | Periodic metrics export (Snapshot over 1000 streams) |

### Throughput Estimate

At ~210 ns/packet end-to-end (parse + observe), the theoretical capacity is ~4.7M RTP pps on a single core. In practice, the SIP/RTP shared channel (10K buffer) and the 1-second snapshot loop are the bottlenecks, not the per-packet cost.

With SIP-vs-RTP channel priority (RTP uses non-blocking send), RTP packets are dropped under extreme load without affecting SIP processing.

### Memory Per RTP Stream

Each active RTP stream stores a `StreamState` struct (~96 bytes) plus `streamEntry` wrapper and map overhead. The Snapshot call allocates a `[]StreamStats` slice proportional to the number of active streams (128 KB for 1000 streams — one allocation).

| Active Streams | Snapshot Allocation | Per-Stream Cost |
|---------------|--------------------:|----------------:|
| 100 | ~13 KB | ~128 bytes |
| 1,000 | ~128 KB | ~128 bytes |
| 10,000 | ~1.3 MB | ~128 bytes |

Streams expire after the configured TTL (default 30s, `SIP_EXPORTER_RTP_STREAM_TTL`), bounding memory under SSRC reuse.

## Geo-Enrichment & Label Resolution Micro-Benchmarks

Per-packet cost of label resolution (`carrier`, `ua_type`, `source_country`, `destination_country`, `caller_host`, `called_host`) including the MaxMind GeoIP lookup. Measured with `go test -bench` on the same i7-8665U (8 logical cores), Go 1.25.11, 3 runs, using the MaxMind GeoLite2-Country **test** database (`test/e2e/data/GeoIP2-Country-Test.mmdb`).

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
```

## Minimum System Requirements

Based on all benchmark results:

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

## How to Run

```bash
# Build Docker image
make docker_build

# Run all load tests (sequential, ~5 min)
SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(cat VERSION) \
  go test -tags=e2e -v -count=1 -timeout 30m -run 'TestLoad' ./test/e2e/load/...

# Run specific test
SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(cat VERSION) \
  go test -tags=e2e -v -count=1 -timeout 5m -run 'TestLoad_FullCallFlow/rate_2000' ./test/e2e/load/...

# Run with single core (test scheduler sensitivity)
SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(cat VERSION) SIP_EXPORTER_E2E_GOMAXPROCS=1 \
  go test -tags=e2e -v -count=1 -timeout 30m -run 'TestLoad' ./test/e2e/load/...

# Run with GC trace
SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(cat VERSION) SIP_EXPORTER_E2E_GODEBUG=gctrace=1 \
  go test -tags=e2e -v -count=1 -timeout 5m -run 'TestLoad_FullCallFlow/rate_2000' ./test/e2e/load/...
```
