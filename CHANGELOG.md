# CHANGELOG

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
