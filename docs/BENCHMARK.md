# RTP Load Benchmark

[Русский](BENCHMARK.ru.md)

Public benchmark results are published only for the same immutable release image that users can
pull from the installation instructions. Results from development images are kept in internal
engineering records and are not presented here as release capacity.

## Release results pending

The current release image has not yet been measured with the final RTP profile. Therefore this
page does not claim a supported CPS limit or publish measurements from another build.

After the release image is built from `develop`, run the release matrix sequentially: mixed SIP+RTP
nominal at 50 CPS, peak at 80 CPS, and a 50-CPS soak after warmup, alongside INVITE flood,
concurrent-dialog, multi-interface, and VQ scenarios. RTP media is bidirectional PCMA, 20 ms
packetization, and 18 seconds per call. Every published result must record:

- the immutable image digest and source revision;
- actual CPS, RTP p95, CPU p95, and working-set p99;
- generator failures and retransmissions;
- exact SIP capture, SER, exporter errors, userspace RTP drops, and kernel socket drops;
- expected and observed RTP totals plus sequence-loss, duplicate, and out-of-order counters;
- host, container limits, capture topology, optional labels, and test duration.

A release artifact may be marked PASS only when all scenario-level generator, integrity and resource
gates complete successfully. The report validates one exact release matrix ResultV2 artifact; it does
not use candidate-baseline promotion or a relative median comparison. A passing release artifact is
not by itself a supported production capacity claim; the matrix includes a soak, and any published
capacity claim needs its own evidence.

Load tests must run separately from E2E and other RTP tests because they share capture resources.
Use a new absolute artifact directory and the immutable release tag:

```bash
make version=<release-tag> \
  ARTIFACT_DIR=/absolute/path/to/new-artifact-directory \
  test-load-release
```

To regenerate a report from an existing `run-1/result.json` without sending traffic, run:

```bash
make ARTIFACT_DIR=/absolute/path/to/artifact-directory test-load-report
```

Do not increase exporter queues or relax integrity assertions to obtain PASS. Inspect every
ResultV2 scenario, generator CSV, before/after metrics, and resource sample before publishing it.
