# Release-Verified Load Envelope

This report documents the verified load envelope for sip-exporter at commit `0bed331`. It is derived
from five fresh, sequential candidate processes on one developer workstation. Every scenario used a
fresh exporter container. The resulting BaselineV2 was owner-accepted for release comparison.

## Verified Envelope

Resource figures are medians of five processes. CPU is container CPU p95; memory is container
working-set p99. MiB values use 1,048,576 bytes.

| Scenario | Offered workload | Limit | Generator median | CPU p95 | Working set p99 | Integrity |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Full call, nominal | 1,000 CPS for 30 s | 1 CPU / 128 MiB | 998.469 CPS | 66.48% | 35.44 MiB | exact 210,000 SIP packets; SER 100% |
| Full call + scrape, peak | 1,800 CPS for 30 s | 2 CPU / 256 MiB | 1,795.390 CPS | 54.19% | 60.59 MiB | exact 378,000 SIP packets; SER 100% |
| INVITE flood | 5,000 CPS for 30 s | 2 CPU / 256 MiB | 4,985.870 CPS | 22.50% | 130.28 MiB | exact 150,000 INVITEs |
| Concurrent dialogs | 2,000 dialogs, held 30 s | 2 CPU / 256 MiB | 99.649 CPS ramp | 26.01% | 28.06 MiB | exact 14,000 SIP packets; 2,000 INVITEs and peak sessions |
| Carrier and UA labels | 1,800 aggregate CPS for 30 s | 2 CPU / 256 MiB | 1,797.274 CPS | 53.01% | 56.62 MiB | exact 378,000 SIP packets; 27,000 INVITEs and SER 100% per label |
| Three interfaces | 3 × 500 CPS for 30 s | 2 CPU / 256 MiB | 1,474.986 CPS | 13.48% | 55.02 MiB | exact 45,000 INVITEs; no cross-interface series |
| Ten-minute soak | 500 CPS for 10 min | 1 CPU / 128 MiB | 499.976 CPS | 41.57% | 39.13 MiB | exact 2,100,000 SIP packets; SER 100% |
| Mixed VQ | 1,000 aggregate CPS for 30 s | 2 CPU / 256 MiB | 999.489 CPS | 9.50% | 14.14 MiB | exact 30,000 SIP packets; reports/MOS-LQ/RLQ 20,000 each; NLR/malformed errors 10,000 each |

All five candidate processes reported zero socket and RTP drops. Median throttling was zero in every
profile except full-call nominal, where it was 0.34%, below the 1% gate. Scenarios expecting ordinary
SIP traffic reported zero unexpected system errors. The mixed VQ profile intentionally reports
10,000 parser/system errors for its 10,000 malformed reports and verifies that value exactly.

## Sustained-Load and Scrape Gates

The soak working-set medians were 35.20 MiB in the first measured minute and 36.40 MiB in the last;
median growth was 1.15 MiB. The gate permits no more than the larger of 10% of the first-minute
median or 8 MiB. All five processes also drained `channel_length`, active dialogs and active media
trackers to zero after exact capture.

At 1,800 CPS, concurrent Prometheus scrapes had a 22.22 ms p99 median. The release gate requires
p95 below 100 ms and p99 below 200 ms while traffic is active.

## Methodology

| Parameter | Baseline value |
| --- | --- |
| OS / architecture | Linux / amd64 |
| Kernel | 6.12.94+deb12-amd64 |
| Go | 1.26.6 |
| Docker | 29.5.3 |
| Exporter commit | `0bed331` |
| Accepted BaselineV2 SHA-256 | `dbc0da32b7a76a2e04d38b9f166d38b5f38538f2a37e150292abaa8ee0786b69` |
| Baseline candidate processes | 5, sequential |
| Exporter topology | privileged Docker container, host network, capture on loopback or isolated test interfaces |

SIPp produces phase-aware generator CSV and logs. ResultV2 records achieved rate, exact capture,
business outcomes, cgroup resource samples, GC evidence and Prometheus snapshots. Candidate
aggregation validates identical environment fingerprints and exact scenario/metric inventories
before calculating medians.

The tests are local acceptance evidence, not a universal hardware capacity guarantee. Results apply
to the declared profiles, limits, environment and exporter revision. They do not benchmark network
latency, production call servers, RTP packet throughput, storage backends or distributed capture.

## Reproducing the Acceptance Run

Never overlap load tests with main or RTP E2E tests. Use a unique image tag and artifact directory:

```bash
make version=<unique-tag> ARTIFACT_DIR=<candidate-artifacts> test-load-candidate
```

After an owner accepts the generated BaselineV2, release comparison uses one fresh process:

```bash
make version=<unique-tag> ARTIFACT_DIR=<release-artifacts> \
  BASELINE=<accepted-baseline-v2.json> test-load-release
```

The candidate baseline is deliberately separate from the accepted baseline; candidate generation
never promotes or overwrites release policy.
