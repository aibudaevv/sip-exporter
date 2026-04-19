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
| Socket buffer | 4 MB (`SO_RCVBUF`) |
| Go | 1.25.8 |

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

Maximum STW pause is **< 1 ms**. With `SO_RCVBUF = 4 MB` (~420 ms buffer at 28K PPS), GC pauses are 400× smaller than the socket buffer capacity — packets are never lost due to GC.

## Memory Per Dialog

Memory overhead per active SIP dialog. Dialog map stores `map[string]time.Time` — dialog ID as key, expiration timestamp as value.

| Active Dialogs | Total RAM | Delta from Baseline | Bytes/Dialog |
|---------------|-----------|--------------------:|-------------:|
| 0 (baseline) | 9.9 MB | — | — |
| 100 | 12.8 MB | 2.8 MB | ~29 KB |
| 403 | 16.6 MB | 6.7 MB | ~17 KB |
| 813 | 14.9 MB | 5.0 MB | ~6 KB |
| 1,627 | 14.9 MB | 5.0 MB | ~3 KB |
| 4,064 | 12.5 MB | 2.5 MB | < 1 KB |

Per-dialog overhead is within GC measurement noise. Even 4,000+ active dialogs add < 7 MB to total memory. The theoretical per-dialog cost is ~100-200 bytes (string key + time.Time value + map overhead), but container-level memory measurement includes Go runtime overhead that obscures per-entry costs.

**Practical conclusion:** dialog storage is negligible. Plan for ~10 MB base + 1-2 MB per 1,000 active dialogs as a conservative estimate.

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
- **RAM:** 10-15 MB base + ~1 MB per 1,000 active dialogs
- **Network:** eBPF socket filter adds zero latency to SIP traffic (filters in kernel)
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
