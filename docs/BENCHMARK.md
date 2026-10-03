# RTP Load Benchmark

[Русский](BENCHMARK.ru.md)

Public benchmark results are published only for the same immutable release image that users can
pull from the installation instructions. Results from development images are kept in internal
engineering records and are not presented here as release capacity.

## Release results pending

The current release image has not yet been measured with the final RTP profile. Therefore this
page does not claim a supported CPS limit or publish measurements from another build.

After the release image is built from `develop`, it must be tested sequentially at 10, 30, 50,
and 80 CPS with complete SIP calls and bidirectional PCMA RTP, 20 ms packetization, and 18 seconds
of media per call. Every published result must record:

- the immutable image digest and source revision;
- actual CPS, RTP p95, CPU p95, and working-set p99;
- generator failures and retransmissions;
- exact SIP capture, SER, exporter errors, userspace RTP drops, and kernel socket drops;
- expected and observed RTP totals plus sequence-loss, duplicate, and out-of-order counters;
- host, container limits, capture topology, optional labels, and test duration.

A point may be marked PASS only when the generator and integrity gates complete successfully.
One short passing run is not a sustained production capacity limit; repeated runs and a longer
soak are required for such a claim.

Load tests must run separately from E2E and other RTP tests because they share capture resources.
Use a new absolute artifact directory and the immutable release tag:

```bash
make version=<release-tag> \
  ARTIFACT_DIR=/absolute/path/to/new-artifact-directory \
  test-load-targeted TEST='^TestLoadFullCallWithRTP$'
```

Do not increase exporter queues or relax integrity assertions to obtain PASS. Inspect every
ResultV2 scenario, generator CSV, before/after metrics, and resource sample before publishing it.
