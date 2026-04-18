# AGENTS.md

## Critical Rules

- **NEVER push to git without explicit user request**
- **NEVER commit without explicit user request**
- **Communicate in Russian language with the user**
- **Design tests using MC/DC approach** — each test must verify real behavior, no absurd/meaningless tests
- **NEVER modify eBPF code when developing tests** — eBPF code is correct and tested
- **NEVER use loopback (`lo`) for integration tests** — packets are duplicated (sent + received), causing incorrect metrics. Exception: e2e tests use `lo` by design (they account for duplication)
- **NEVER create shell scripts (`.sh`) for running tests** — use Go test framework directly
- **NEVER modify service code to make integration tests pass** — tests must adapt to the service, not vice versa
- **NEVER rebuild eBPF program (`make ebpf_compile`, `make build`) without explicit user permission** — use existing `bin/sip.o`. Only `make go_build` / `make docker_build` are allowed freely
- **NEVER run e2e and load tests together** — they must run separately. E2e tests (`./test/e2e/`) and load tests (`./test/e2e/load/...`) both create AF_PACKET sockets on `lo`; running them concurrently causes packet loss/duplication
- **NEVER modify eBPF code (`internal/bpf/`) under any circumstances** — eBPF code is correct and tested

## Build & Run

```bash
make build            # Full: eBPF compile + Go binary
make ebpf_compile     # clang -O2 -target bpf -c internal/bpf/sip.c -o bin/sip.o -g -fno-stack-protector
make go_build         # go build -o bin/main cmd/main.go
make docker_build     # Default Make target
make ebpf_log         # sudo cat /sys/kernel/debug/tracing/trace_pipe
```

Runtime requires root (eBPF needs `CAP_BPF`). Docker must run `--privileged --network host`.
Version is in `VERSION` file, read by Makefile.

## Test Commands

```bash
make test             # go test -v ./...
go test -v ./internal/service/...                              # Single package
go test -v -run TestName ./internal/service/...                # Single test
go test -bench=. -benchmem ./internal/exporter/...             # Benchmarks
```

### E2E Tests

E2E tests use **SIPp** via **testcontainers-go** to generate real SIP traffic. Requires Docker + root.

```bash
make test-e2e                                                    # All e2e tests (excludes load)
make test-e2e-run TEST=TestSER_AllScenarios                      # Specific test
make test-e2e-run TEST=TestSER_AllScenarios/100_percent          # Subtest
```

E2E tests use build tag `//go:build e2e` — they are **excluded from `make test`**.

Key details:
- Exporter image is built from Dockerfile, runs privileged with host network on `lo`
- SIPp UAS/UAC containers also use `--network=host`
- Run with `-parallel 4` to avoid kernel AF_PACKET socket contention on `lo`
- SCR expected values account for loopback duplication: `inviteTotal` doubles but `sessionCompletedTotal` does not (dialog map deduplicates), so SCR = theoretical/2
- Verbose mode: `SIP_EXPORTER_E2E_SIPP_VERBOSE=true SIP_EXPORTER_E2E_EXPORTER_VERBOSE=true`

CI runs only unit tests: `go test -v -coverprofile=coverage.out ./internal/... ./pkg/...` (Go 1.24 in CI, go.mod says 1.25.8 — mismatch exists).

### Load Tests

Load tests are in a separate package `test/e2e/load/`. Same build tag `//go:build e2e`. Measures PPS, drain time, CPU/memory via `docker stats`.

**IMPORTANT**: Load tests must run **separately** from e2e tests — both create AF_PACKET sockets on `lo`.

```bash
make test-load                                                         # All load tests
make test-load-run TEST=TestLoad_INVITEFlood/rate_1000                 # Specific test
make test-load-update-baseline                                         # Save current results as baseline
```

Load tests: `TestLoad_INVITEFlood` (raw PPS), `TestLoad_FullCallFlow` (full SIP dialog lifecycle), `TestLoad_ConcurrentSessions` (dialog map scalability), `TestBenchmark_MemoryStability` (memory growth under sustained load), `TestBenchmark_ScrapeLatencyUnderLoad` (P95 /metrics latency), `TestBenchmark_GCPauseDuration` (GC STW pauses), `TestBenchmark_MemoryPerDialog` (memory per active dialog).

**Load test methodology**: thresholds follow k6 Thresholds model (SLO-based, not exact values). Each run produces `test/e2e/load/load_result.json` with all metrics. Results are compared against `test/e2e/load/baseline.json` — if any metric regresses beyond tolerance (defined in baseline), the test run fails. After intentional changes, run `make test-load-update-baseline` to update the reference.

## Lint

```bash
make lint             # golangci-lint run (runs vet + imports first)
make vet              # go vet -unsafeptr ./...
make imports          # goimports -l -w . (depends on vet)
```

**Important**: `make lint` runs `vet → imports → golangci-lint` in sequence.

### Linter Gotchas

- **`testpackage`** linter is enabled but **excluded for test files** — same-package tests (`package service` in `*_test.go`) are fine.
- **`sloglint: no-global: all`** is enabled but project uses `zap.L()` (global zap logger), not slog. No conflict since sloglint only checks slog usage.
- **`nolint` directives** must include specific linter name and explanation: `//nolint:funlen // complex function with many cases`
- **`nakedret`** is set to `max-func-lines: 0` — ALL naked returns are flagged, even in short functions.
- Key limits: line length 120, funlen 100 lines/50 statements, cyclop 30, gocognit 20.
- Tests excluded from linting (`run.tests: false`) + many linters relaxed for `*_test.go`.

## Architecture

```
SIP Traffic → NIC → eBPF socket filter (AF_PACKET) → ringbuf → Go poller → SIP parser → Prometheus
```

- `cmd/main.go` → `config.GetConfig()` → `pkgLog.Verbosity()` → `server.NewServer().Run(cfg)`
- `internal/exporter` — eBPF integration, raw packet parsing (L2→SIP), feeds `Metricser` + `Dialoger`
- `internal/service/metrics` — Prometheus counters/gauges/histograms (including RFC 6076: SER, SEER, ISA, SCR, RRD, SPD)
- `internal/service/dialogs` — SIP dialog tracking (created on 200 OK to INVITE, removed on 200 OK to BYE)
- `internal/server` — HTTP server with `/metrics` endpoint
- `internal/config` — cleanenv-based config from env vars
- `internal/dto` — SIP packet data transfer objects
- `pkg/log` — Zap logger setup

### Data Flow (goroutines)

```
Initialize()
├── go readSocket()        → reads from AF_PACKET raw socket → copies to messages channel (10K buffer)
├── go readPackets()       → consumes from messages channel → parseRawPacket() → handleMessage()
└── go sipDialogMetricsUpdate() → 1s ticker: Cleanup() expired dialogs, cleanup trackers, update sessions gauge
```

`readSocket()` → `messages` chan → `readPackets()` → `parseRawPacket()` → `handleMessage()` → `sipPacketParse()`

### Packet Parsing Pipeline

1. **L2**: Ethernet header (14 bytes), check VLAN 802.1Q tag → adjust offset
2. **L3**: IPv4 only (ethertype 0x0800), extract IHL
3. **L4**: UDP only (protocol 17), skip 8-byte header
4. **SIP**: minimum 50 bytes, must start with known SIP method or `SIP/2.0`
5. **Parse**: `sipPacketParse()` extracts request method / response status, From tag, To tag, Call-ID, CSeq (ID + method), Session-Expires

### Interfaces

```go
// Metricser (internal/service/metrics.go) — all Prometheus metrics
Request(in []byte)
Response(in []byte, isInviteResponse bool)
ResponseWithMetrics(status []byte, isInviteResponse, is200OK bool)
Invite200OK()
SessionCompleted()
UpdateRRD(delayMs float64)
UpdateSPD(duration time.Duration)
UpdateTTR(delayMs float64)
UpdateSession(size int)
SystemError()

// Dialoger (internal/service/dialogs.go) — dialog lifecycle
Create(dialogID string, expiresAt time.Time, createdAt time.Time)
Delete(dialogID string) time.Duration
Size() int
Cleanup() []time.Duration

// Exporter (internal/exporter/exporter.go) — eBPF packet capture
Initialize(interfaceName string, path string, sipPort, sipsPort int) error
IsAlive() bool
Close()

// Server (internal/server/server.go) — HTTP server
Run(cfg *config.App) error
```

### Metrics Registration

- **Production** (`NewMetricser()`): calls `newMetricserWithRegistry(nil)` → uses `promauto` (auto-registers to global Prometheus registry)
- **Tests** (`NewTestMetricser()`): creates `prometheus.NewRegistry()` → passes to `newMetricserWithRegistry(reg)` → manually creates and registers metrics in isolated registry
- Pattern: `newCounterWithRegistry()`, `newHistogramWithRegistry()`, `newSessionsGaugeWithRegistry()` check `reg != nil` to decide `promauto` vs manual registration
- **NEVER use `promauto` in tests** — causes "duplicate metrics registration" panic

### Metrics Types

| Type | Metrics |
|---|---|
| Counter | 14 request counters, 19+ status code counters, `systemErrorTotal`, `sipPacketsTotal` |
| Gauge | `sessions` (active dialogs) |
| GaugeFunc | `ser`, `seer`, `isa`, `scr` (computed on scrape from atomic counters) |
| Histogram | `rrd` (ms, buckets: 1,5,10,25,50,100,250,500,1000,5000), `spd` (sec, buckets: 1,5,10,30,60,300,600,1800,3600), `ttr` (ms, buckets: same as RRD) |
| Deprecated GaugeFunc | `rrdAvg`, `spdAvg` (kept for backward compatibility) |

### Tracker Pattern

The exporter uses timestamp tracker maps for measuring delays. Pattern:

1. **Store**: on request arrival, save `time.Now()` in map keyed by Call-ID
2. **Measure**: on response, calculate `time.Since(startTime)`, observe into histogram, remove entry
3. **Remove**: on non-success response, just remove entry (no measurement)
4. **Cleanup**: background goroutine removes entries older than TTL (60s)

Current trackers:
- `registerTracker` — stores REGISTER timestamp, measures RRD on 200 OK REGISTER
- `inviteTracker` — stores INVITE timestamp, measures TTR on first 1xx provisional response

### Dialog Lifecycle

- **Create**: `200 OK` to `INVITE` → `dialoger.Create(dialogID, expiresAt, createdAt)` — only if dialog doesn't already exist (`if !exists`)
- **Delete**: `200 OK` to `BYE` → `dialoger.Delete(dialogID)` → returns duration → `UpdateSPD()` + `SessionCompleted()`
- **Cleanup**: `sipDialogMetricsUpdate()` runs every 1s → `dialoger.Cleanup()` removes expired dialogs (Session-Expires) → each expired dialog counts as `SessionCompleted()` + `UpdateSPD()`
- **Dialog ID**: `{call-id}:{min-tag}:{max-tag}` (tags sorted lexicographically)
- Default expiry: 1800s (30 min) if Session-Expires header absent

### Handle Response Flow

```
handleResponse(packet)
├── Determine: isInviteResponse, isRegisterResponse, is200OK
├── TTR logic (INVITE responses only):
│   ├── status[0] == '1' → measureTTR(callID), UpdateTTR()
│   └── status[0] != '1' → removeInviteTime(callID)
├── metricser.ResponseWithMetrics(status, isInviteResponse, is200OK)
├── if is200OK:
│   ├── handle200OKResponse()
│   │   ├── CSeq INVITE → handleInvite200OK() → dialoger.Create()
│   │   ├── CSeq BYE → handleBye200OK() → dialoger.Delete() + UpdateSPD() + SessionCompleted()
│   │   └── REGISTER → handleRegister200OK() → UpdateRRD()
│   └── ...
└── if !is200OK && REGISTER → removeRegisterTime()
```

### Handle Request Flow

```
handleMessage(rawPacket) → sipPacketParse(raw)
├── if response → handleResponse(packet)
└── if request:
    ├── metricser.Request(method) — increments request counter (+ inviteTotal for INVITE)
    ├── if INVITE → storeInviteTime(callID)
    └── if REGISTER → storeRegisterTime(callID)
```

## Config (env vars)

| Variable | Default | Required |
|---|---|---|
| `SIP_EXPORTER_INTERFACE` | — | yes |
| `SIP_EXPORTER_HTTP_PORT` | `2112` | no |
| `SIP_EXPORTER_LOGGER_LEVEL` | `info` | no |
| `SIP_EXPORTER_OBJECT_FILE_PATH` | `/usr/local/bin/sip.o` | no |
| `SIP_EXPORTER_SIP_PORT` | `5060` | no |
| `SIP_EXPORTER_SIPS_PORT` | `5061` | no |

## Domain Knowledge

### SIP

- Reference **RFC 6076** for SIP Performance Metrics
- Dialog ID: `{call-id}:{min-tag}:{max-tag}` (tags sorted lexicographically)
- Dialogs created on `200 OK` to `INVITE`, removed on `200 OK` to `BYE`
- Standard ports: 5060 (UDP/TCP), 5061 (TLS)
- Session-Expires (RFC 4028): `Dialoger.Cleanup()` removes stale dialogs; each removed counts as a completed session for SCR

### RFC 6076 Metric Formulas

- **SER** = `(INVITE→200 OK) / (Total INVITE - INVITE→3xx) × 100` — 3xx excluded from denominator
- **SEER** = `(INVITE→200,480,486,600,603) / (Total INVITE - INVITE→3xx) × 100` — effective response codes: 200, 480, 486, 600, 603
- **ISA** = `(INVITE→408,500,503,504) / Total INVITE × 100` — 3xx NOT excluded from denominator
- **SCR** = `(Completed Sessions) / (Total INVITE) × 100` — 3xx NOT excluded; completed = INVITE→200 OK + BYE→200 OK
- SEER ≥ SER always (SEER numerator is a superset); SCR ≤ SER (completed ⊆ established)
- All are cumulative over runtime; undefined when no INVITEs

### eBPF

- Socket filter via `AF_PACKET`, handles VLAN-tagged and regular Ethernet frames
- Zero-copy kernel→userspace via ringbuf
- **Known limitation**: Packet copying in 64-byte blocks due to eBPF verifier constraints

## Code Style

### Imports

Three groups: stdlib / external / local (`gitlab.com/sip-exporter/...`).

### Conventions

- Interfaces: `-er` suffix (`Metricser`, `Dialoger`, `Exporter`, `Server`)
- Constructors: `New` prefix (`NewMetricser()`, `NewExporter()`)
- Private structs: lowercase (`metrics`, `exporter`, `dialogs`)
- Sentinel errors: `Err` prefix (`ErrUserNotRoot`)
- Group related types with `type (...)` block syntax
- Logger: `zap.L()` (global), never `log` or `slog` in non-main code (depguard enforces this)
- Config: `env` tags with cleanenv
- Error wrapping: `fmt.Errorf("context: %w", err)`
- **NEVER** use `if err == nil` — always `if err != nil`

## Testing Best Practices

### Prometheus Metrics in Tests

- **NEVER use `promauto`** in tests — it registers metrics in global registry, causing "duplicate metrics registration" panic on repeated test runs
- **Each test must create its own `*metrics` instance** with isolated `prometheus.NewRegistry()`
- Use `NewTestMetricser()` factory (in `metrics_test.go`) to create isolated metrics instances
- Test absolute values, not deltas (before/after) — this is cleaner and more reliable

### Interface Consistency

- If a method is used by both tests and production code (e.g., `ResponseWithMetrics()`), it **must** be in the interface
- Avoid adding test-only methods to interfaces — keep them as unexported methods on the concrete type

## Known Issues (Pre-existing Lint)

These are **NOT** caused by recent changes and should not block PRs:

- `gosec G115` — integer overflow warnings (expected in network code)
- `intrange` — suggestions to use integer range syntax
- `mnd` — magic numbers in various files

## Requirements

- Go 1.25+ (go.mod specifies 1.25.8)
- Clang/LLVM for eBPF compilation
- golangci-lint, goimports
- Root privileges at runtime

## Session Learnings

### SCR on Loopback

On loopback (`lo`) each packet is captured twice (send + receive). For SCR this causes a mismatch:
- `inviteTotal` **doubles** (each INVITE seen twice)
- `sessionCompletedTotal` does **NOT** double (`dialogs.Create()` uses `if !exists`, second INVITE→200 OK is ignored; `Delete()` returns 0 on second BYE→200 OK)
- Result: SCR on loopback = theoretical/2. All SCR e2e tests expect halved values.

SER/SEER/ISA are unaffected because both numerator and denominator double equally.

### Go Test Pitfalls

- `TestMain` MUST be in a `*_test.go` file — Go's test runner ignores it in plain `.go` files
- `go test` runs from a temporary directory — use `projectRoot` (computed via `runtime.Caller`) for file paths, not relative paths
- `go test -c` compiles a test binary — useful for debugging TestMain issues (`go tool nm <binary> | grep TestMain`)

### AF_PACKET Contention

Multiple containers with AF_PACKET sockets on `lo` cause packet loss/duplication. Symptoms:
- SER/SEER values > 100% (impossible — means extra responses counted)
- SER/SEER values slightly below expected (a few packets lost)
- Solution: `-parallel 4` for e2e, separate run for load tests

### Load Test Architecture

- SLO-based thresholds (k6 model): `require.GreaterOrEqual(t, ser, 99.0)` not `require.Equal(t, 100.0, ser)`
- Baseline comparison: `load_result.json` (current run) vs `baseline.json` (committed reference)
- Regression detection: each metric compared with configurable tolerance (e.g., cpu_peak ±50%, actual_pps ±15%)
- `make test-load-update-baseline` copies current results to baseline — use after intentional changes
- `load_result.json` is in `.gitignore`, `baseline.json` is committed

### RFC 6076 Section Mapping

Correct numbering (many files had wrong numbers before):
- §4.1 RRD, §4.5 SDT (called SPD in codebase), §4.6 SER, §4.7 SEER, §4.8 ISA, §4.9 SCR

### Docs Structure

- `docs/METRICS.md` — full metric reference (all metrics, formulas, examples)
- `docs/BENCHMARK.md` — load test results and methodology
- `docs/ALERTING.md` — pre-configured alerting examples
- README contains only brief overview + links to docs/
