# CHANGELOG

## 0.12.0
### Added
- 10 new SIP response status code counters: 181 (Call Is Being Forwarded), 182 (Queued), 405 (Method Not Allowed), 481 (Dialog/Transaction Does Not Exist), 487 (Request Terminated), 488 (Not Acceptable Here), 501 (Not Implemented), 502 (Bad Gateway), 604 (Does Not Exist Anywhere), 606 (Not Acceptable)
- E2E tests with SIPp scenarios for all 10 new status codes (including CANCEL flow for 487)
- E2E tests with carrier label filtering for 4 new status codes (181, 487, 502, 604)

### Changed
- Refactored `incrementStatusCodeCounter` from 30-case switch to map-based lookup (cyclomatic complexity 31→1)
- Grafana dashboard: added 10 new status codes to "SIP Responses Rate" panel in numeric order
- `docs/METRICS.md`: status codes reordered to numeric sort

## 0.11.0
### Added
- Per-carrier SIP metrics with CIDR-based resolution (`SIP_EXPORTER_CARRIERS_CONFIG`)
- `internal/carriers` package: YAML config loader with CIDR→carrier name mapping
- Carrier label on all SIP metrics: requests, responses, SER, SEER, ISA, SCR, ASR, NER, RRD, SPD, TTR, ORD, LRD, active sessions
- Carrier resolution: source IP → CIDR match → carrier name; destination IP fallback; `carrier="other"` for unmatched
- E2E tests for carrier: direction tests (carrier-A INVITE, carrier-B 200 OK → metrics to carrier-A), CIDR overlap, multi-carrier scenarios
- `examples/carriers.yaml` — example carrier configuration
- Grafana dashboard updated: carrier-filtered panels for all metrics
- CI: `govulncheck` workflow — Go dependency vulnerability scanning (push + daily schedule)
- CI: `trivy` workflow — Docker image vulnerability scanning with SARIF upload to GitHub Security tab
- `make vulncheck` — local Go vulnerability check via `govulncheck`
- `make trivy-fs` — local filesystem vulnerability scan via `trivy`
- `make trivy-image` — local Docker image vulnerability scan (builds image first)
- `make security` — runs `vulncheck` + `trivy-fs` (quick pre-push check)
- `docs/SECURITY.md` — security documentation (EN): why `--privileged` is required, attack surface analysis, eBPF code audit guide, industry analogs
- `docs/SECURITY.ru.md` — security documentation (RU)
- Badges in README: Go Vulncheck status, Container Scan status

### Changed
- `Dialoger` interface: `Create`, `Delete`, `Cleanup`, `Size` now accept/return carrier labels
- `Metricser` interface: all methods accept `carrier string` parameter
- `exporter.go`: carrier resolution on every packet, carrier passed through all tracker entries
- Go 1.25.8 → 1.25.9 (fixes CVE in crypto/x509, crypto/tls)
- Alpine 3.20 → 3.22 (fixes 18 CVE in openssl, musl, zlib)
- testcontainers-go v0.41.0 → v0.42.0 (fixes CVE-2026-34040, CVE-2026-33997 in docker/docker)
- `test/e2e/load/load_test.go`: imports migrated from `github.com/docker/docker` to `github.com/moby/moby`
- Dockerfile: `golang:1.25-alpine` → `golang:1.25.9-alpine`
- CI workflows: go-version updated to 1.25.9
- `docker-compose.yaml`: link to `docs/SECURITY.md` in privileged comment
- README.md / README.ru.md: carrier documentation, Security link in ToC and Install section

## 0.10.0
### Added
- NER (Network Effectiveness Ratio) metric per GSMA IR.42 (`sip_exporter_ner`)
- NER = 100 − ISA, reflects network quality including call termination
- ISS (Ineffective Session Severity) counter (`sip_exporter_iss_total`)
- ISS counts absolute number of INVITE→408/500/503/504 responses
- ORD (OPTIONS Response Delay) histogram (`sip_exporter_ord`)
- ORD measures delay from OPTIONS request to any response (p95, ms)
- LRD (Location Registration Delay) histogram (`sip_exporter_lrd`)
- LRD measures delay from REGISTER to 3xx redirect response (p95, ms)
- E2E tests for NER: AllScenarios, Mixed, Equals100MinusISA
- E2E tests for ISS: AllScenarios, Mixed
- E2E tests for ORD: OptionsPing, NoOptions, MixedWithOptions
- E2E tests for LRD: RegisterRedirect, Register200OK, RegisterError, Mixed
- SIPp scenarios for LRD: reg_uas_redirect, reg_uac_redirect (REGISTER→302)
- MC/DC unit tests for NER, ISS, ORD, LRD metric calculation
- Grafana dashboard: NER, ISS, ORD, LRD panels with thresholds

### Changed
- README updated: 55 E2E tests, updated dashboard panels, new metrics listed
- Grafana dashboard layout: new row with delay/ratio metrics, timeseries shifted

## 0.9.0
### Added
- ASR (Answer Seizure Ratio) metric per ITU-T E.411 (`sip_exporter_asr`)
- ASR tracks INVITE→200 OK ratio without excluding 3xx (difference from SER)
- SDC (Session Duration Counter) metric (`sip_exporter_sdc_total`)
- SDC exposes completed session count as Prometheus Counter for rate queries (`rate(sip_exporter_sdc_total[5m])`)
- SPD (Session Process Duration) metric per RFC 6076 §4.5 (`sip_exporter_spd`)
- SPD measures average session duration from INVITE 200 OK to BYE 200 OK (in seconds)
- SPD also tracks sessions that expire via Session-Expires timeout
- E2E tests for SPD: SuccessfulCalls, NoCompletedCalls, Mixed
- MC/DC unit tests for SPD metric calculation
- Load tests: baseline comparison system with `load_result.json` and `baseline.json` (k6 Thresholds model)
- Load tests: `make test-load`, `make test-load-run`, `make test-load-update-baseline`
- Load test metrics recording: each test writes structured metrics to `load_result.json`
- Load test summary: baseline comparison table with OK / REGRESSION / IMPROVEMENT status
- Metrics documentation: [docs/METRICS.md](docs/METRICS.md) — full reference for all metrics
- E2E tests for ASR: AllScenarios, Mixed, MixedWith3xx, Complex
- E2E tests for SDC: AllScenarios, Mixed, MixedWith3xx, Complex, SessionExpires
- MC/DC unit tests for ASR formula calculation and ASR ≤ SER invariant
- Unit tests for SDC Counter increment and nil-safety

### Changed
- `Dialoger.Create` now accepts `createdAt` parameter for session duration tracking
- `Dialoger.Delete` now returns `time.Duration` (session duration)
- `Dialoger.Cleanup` now returns `[]time.Duration` instead of `int`
- SCR e2e tests: expected values account for loopback duplication (SCR = theoretical/2)
- E2e and load tests run separately (`./test/e2e/` and `./test/e2e/load/...`)
- E2e tests use `-parallel 4` to avoid AF_PACKET socket contention on `lo`
- Load tests use SLO-based thresholds instead of exact value assertions:
  - `require.Equal(t, 100.0, ser)` → `require.GreaterOrEqual(t, ser, 99.0)`
  - `require.Equal(t, 0, errors)` → `require.LessOrEqual(t, errors, maxErrors)`
  - Warning logs replaced with `require.Less` SLO assertions
- RFC 6076 section numbering corrected across all files (code, docs, changelog)
- Metrics descriptions moved from README to [docs/METRICS.md](docs/METRICS.md)
- Grafana dashboard updated: added ASR, SDC, SPD panels; fixed RRD to use `histogram_quantile()` instead of broken bare metric expression
- `.gitignore` updated: `load_result.json` excluded, `baseline.json` tracked

## 0.8.0
### Added
- SCR (Session Completion Ratio) metric per RFC 6076 §4.9 (`sip_exporter_scr`)
- SCR tracks sessions completed with INVITE→200 OK→BYE→200 OK cycle
- RRD (Registration Request Delay) metric per RFC 6076 §4.1 (`sip_exporter_rrd`)
- RRD measures average delay between REGISTER request and 200 OK response
- Session-Expires timeout cleanup: dialogs exceeding timeout are counted as completed in SCR
- E2E tests for SCR: AllScenarios, Mixed, MixedWith3xx, Complex, SessionExpires
- MC/DC unit tests for SCR and RRD metric calculation

### Fixed
- Memory leak in registerTracker: TTL-based cleanup (60s) prevents unbounded growth
- Race condition in metrics: `ResponseWithMetrics()` ensures atomic SER/SEER counter updates
- SCR undefined behavior: returns 0 when no INVITEs received

## 0.7.0
### Added
- ISA (Ineffective Session Attempts) metric per RFC 6076 (`sip_exporter_isa`)
- ISA tracks server errors: 408, 500, 503, 504 (infrastructure failures)
- ISA panel to Grafana dashboard (thresholds: 0-5% green, 5-15% yellow, >15% red)
- E2E tests for ISA: all_500, all_503, all_200, Mixed
- SIPp scenarios for ISA: unavailable (503)
- MC/DC unit tests for ISA metric calculation

## 0.6.0
### Added
- SEER (Session Establishment Effectiveness Ratio) metric per RFC 6076 (`sip_exporter_seer`)
- SEER tracks effective responses: 200 OK, 480, 486, 600, 603 (clear user outcomes)
- SEER panel to Grafana dashboard
- E2E tests for SEER: all_200, all_486, all_480, all_603, all_500, redirect_only, MixedEffective, MixedWithErrors, Mixed3xx, Complex
- SIPp scenarios for SEER: busy (480), decline (603), server_error (500)

### Changed
- E2E tests use `require.Equal` instead of `require.InDelta` — metrics are deterministic on loopback
- Refactored `NewMetricser()`: extracted `initRequestCounters()`, `initStatusCounters()`, `initSystemCounters()`, `newSER()`, `newSEER()` for code clarity
- Replaced duplicate SEER switch in `Response()` with `isEffectiveResponse()` helper
- README updated with SEER documentation and e2e configuration guide
- E2E test verbosity control: `SIP_EXPORTER_E2E_SIPP_VERBOSE` and `SIP_EXPORTER_E2E_EXPORTER_VERBOSE` env vars (quiet by default)
- MC/DC unit tests for SEER metric calculation

## 0.5.0
### Added
- SER (Session Establishment Ratio) metric per RFC 6076 (`sip_exporter_ser`)
- E2E tests with SIPp via testcontainers-go (`make test-e2e`)
- SER test scenarios: 100%, 0%, redirect, mixed, no INVITE, mixed 3xx+200
- E2E tests verify `sip_exporter_sessions` returns to 0 after completion
- Comprehensive unit test coverage with MC/DC standard compliance
- Unit tests for all packages: config, dto, exporter, server, service, log

### Changed
- E2E tests use loopback interface automatically (no physical interface required)
- All comments in code translated to English

### Fixed
- SIPp scenarios: proper To tag in 200 OK responses to BYE for correct dialog termination

### Removed
- Absurd tests that tested Go language features instead of business logic
- `docker-compose.test.yml` — replaced by testcontainers-go
- Makefile targets `test-ser-*` — replaced by `make test-e2e` and `make test-e2e-run`

## 0.4.0
### Added
- `sip_exporter_sessions` - sip dialogs metrics

## 0.3.0
### Added
- Initial release with basic SIP metrics
