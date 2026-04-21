# Выбор следующей фичи

**Session ID:** ses_2570f0a15ffezZDRY8JLmkd1Sj
**Created:** 4/20/2026, 6:30:57 AM
**Updated:** 4/20/2026, 9:04:11 AM

---

## User

какую фичу возмём следующей

---

## Assistant (Plan · GLM-5.1 · 280.8s)

_Thinking:_

The user is asking "какую фичу возьмём следующей" (what feature should we take next) in Russian. This is a brainstorming/creative question about what to build next. Let me look at the current state of the project to understand what's already implemented and what could be improved or added.

Let me first explore the codebase to understand the current feature set, and then I can suggest potential next features.

Давай посмотрю текущее состояние проекта и пойму, что уже есть, чтобы предложить варианты.

**Tool: task**

**Input:**
```json
{
  "description": "Explore project features",
  "subagent_type": "explore",
  "prompt": "Explore the sip-exporter project at /home/aleksey/go/src/github.com/sip-exporter. I need a comprehensive understanding of:\n\n1. What features are already implemented (metrics, exporters, etc.)\n2. What's in the TODO/FUTURE/ROADMAP if any\n3. What metrics are exported (check internal/service/metrics/ and docs/METRICS.md)\n4. What tests exist and their coverage areas\n5. Any open issues or known limitations mentioned in code comments\n6. The current state of e2e tests and load tests\n7. Check git log for recent activity to understand development direction\n\nReturn a structured summary of all findings."
}
```

**Output:**
```
task_id: ses_2570ed051ffeV5ZdoN3AeeaK3y (for resuming to continue this task if needed)

<task_result>
Now I have a thorough picture. Here is the comprehensive structured summary:

---

# SIP Exporter Project — Comprehensive Analysis

**Repository:** `/home/aleksey/go/src/github.com/sip-exporter`
**Current version:** 0.10.0 | **Total commits:** 84 | **Total tracked files:** 94
**Branches:** `master`, `develop` (active), `ab/feature/rfc-new-feature`
**License:** AGPL-3.0 | **Module path:** `gitlab.com/sip-exporter`
**Go version:** 1.25.8

---

## 1. Implemented Features

### Core Architecture
```
SIP Traffic → NIC → eBPF socket filter (AF_PACKET) → ringbuf → Go poller → SIP parser → Prometheus
```

- **eBPF packet capture** (`internal/bpf/sip.c`, 100 lines): kernel-level socket filter on AF_PACKET, handles VLAN 802.1Q, IPv4 + UDP only, configurable SIP/SIPS ports via BPF map
- **Raw packet parsing** (`internal/exporter/exporter.go`, 724 lines): L2 (Ethernet) → L3 (IPv4, IHL-aware) → L4 (UDP) → SIP parsing; extracts From tag, To tag, Call-ID, CSeq, Session-Expires
- **SIP dialog tracking** (`internal/service/dialogs.go`, 79 lines): map-based dialog store, creates on 200 OK INVITE, deletes on 200 OK BYE or Session-Expires timeout; default 1800s expiry
- **Prometheus metrics** (`internal/service/metrics.go`, 653 lines): dual registry pattern (production uses `promauto`, tests use isolated `prometheus.NewRegistry`)
- **HTTP server** (`internal/server/server.go`, 86 lines): `/metrics` (Prometheus handler), `/health` (200 OK / 503 based on eBPF IsAlive), graceful shutdown on SIGTERM (10s timeout)
- **Configuration** (`internal/config/config.go`, 30 lines): cleanenv-based, environment variables only, 6 configurable params
- **Logging** (`pkg/log/`): Zap-based, configurable verbosity

### 13 RFC 6076 / Telecom Metrics (All Implemented)

| Metric | Type | Prometheus Name | Standard | Since |
|--------|------|-----------------|----------|-------|
| SER | GaugeFunc | `sip_exporter_ser` | RFC 6076 S4.6 | v0.5 |
| SEER | GaugeFunc | `sip_exporter_seer` | RFC 6076 S4.7 | v0.6 |
| ISA | GaugeFunc | `sip_exporter_isa` | RFC 6076 S4.8 | v0.7 |
| SCR | GaugeFunc | `sip_exporter_scr` | RFC 6076 S4.9 | v0.8 |
| RRD | Histogram | `sip_exporter_rrd` | RFC 6076 S4.1 | v0.8 |
| SPD | Histogram | `sip_exporter_spd` | RFC 6076 S4.5 | v0.9 |
| ASR | GaugeFunc | `sip_exporter_asr` | ITU-T E.411 | v0.9 |
| SDC | Counter | `sip_exporter_sdc_total` | — | v0.9 |
| TTR | Histogram | `sip_exporter_ttr` | — | v0.10 |
| NER | GaugeFunc | `sip_exporter_ner` | GSMA IR.42 | v0.10 |
| ISS | Counter | `sip_exporter_iss_total` | — | v0.10 |
| ORD | Histogram | `sip_exporter_ord` | — | v0.10 |
| LRD | Histogram | `sip_exporter_lrd` | — | v0.10 |

### Additional Metrics (Counters/Gauges)
- **14 request counters**: INVITE, ACK, BYE, CANCEL, OPTIONS, REGISTER, UPDATE, INFO, REFER, SUBSCRIBE, NOTIFY, PRACK, PUBLISH, MESSAGE
- **20 response status counters**: 100, 180, 183, 200, 202, 300, 302, 400, 401, 403, 404, 407, 408, 480, 486, 500, 503, 504, 600, 603
- **1 gauge**: `sip_exporter_sessions` (active SIP dialogs)
- **2 system counters**: `sip_exporter_system_error_total`, `sip_exporter_packets_total`
- **2 deprecated GaugeFuncs** (backward compat): `sip_exporter_rrd_average`, `sip_exporter_spd_average`

### Tracker Pattern (Timestamp Maps)
Three trackers with 60s TTL cleanup:
- `registerTracker`: REGISTER timestamp → measures RRD (200 OK) or LRD (3xx)
- `inviteTracker`: INVITE timestamp → measures TTR (first 1xx)
- `optionsTracker`: OPTIONS timestamp → measures ORD (any response)

### Infrastructure
- **Docker** (`Dockerfile`, `docker-compose.yaml`): runs `--privileged --network host`
- **Grafana dashboard** (`examples/grafana-dashboard.json`): 21 panels
- **CI** (`.github/workflows/go.yml`): unit tests only on push/PR (Go 1.24 in CI vs 1.25.8 in go.mod — known mismatch)
- **Lint**: golangci-lint with strict rules (line 120, funlen 100/50, cyclop 30, gocognit 20, nakedret 0, testpackage, sloglint)
- **Makefile**: `build`, `docker_build`, `ebpf_compile`, `go_build`, `test`, `test-e2e`, `test-load`, `lint`

---

## 2. Roadmap / Future Plans (from ISSUES.md)

### Detailed version roadmap defined:

| Version | Planned Features | Status |
|---------|-----------------|--------|
| **v0.11** | Per-Carrier Labels + Self-Monitoring | Not started |
| **v0.12** | SIP User-Agent Classification | Not started |
| **v0.13** | TCP Support (TLS offload) | Not started |
| **v0.14** | RTCP Quality Metrics + MOS | Not started |
| **v0.15** | CDR Export | Not started |
| **v0.16** | OpenTelemetry + Helm Chart | Not started |
| **v0.17** | Hot-reload + IPv6 + Multi-interface | Not started |
| **v1.0** | Retransmission Detection + GeoIP + HOMER/HEP | Not started |

### Top-priority unimplemented features:

1. **P0: Per-IP / Per-Carrier metrics** — carrier label via CIDR groups, requires YAML config, eBPF changes, Metricser rework
2. **P1: TCP/TLS support** — eBPF only captures UDP (line 306: `if ipHeader[9] != 17`), needs TCP stream reassembly
3. **P1: RTP/RTCP Quality Metrics** — MOS, jitter, packet loss via RTCP parsing
4. **P2: OpenTelemetry export** — OTLP protocol support
5. **P2: Self-Monitoring Metrics** — uptime, parse errors, channel capacity, ring buffer drops
6. **P2: CDR Export** — Call Detail Records (JSON Lines, webhook, Kafka)
7. **P2: SIP User-Agent Classification** — parse User-Agent header, regex-based labeling
8. **P3: Multi-interface, IPv6, retransmission detection, GeoIP, HOMER/HEP, Helm chart, hot-reload**

### Distribution/Marketing plan (in ISSUES.md)
A detailed 5-phase marketing plan exists covering GitHub setup, account warming on Reddit/HN/Twitter/LinkedIn/Dev.to/Habr, launch strategy, content marketing, and ecosystem integration.

---

## 3. Metrics Exported (Complete Reference)

Full documentation in `/home/aleksey/go/src/github.com/sip-exporter/docs/METRICS.md` (489 lines).

See the 13 performance metrics table above plus the ~37 additional counters/gauges. Key formulas:

- **SER** = `(INVITE→200 OK) / (Total INVITE - INVITE→3xx) × 100`
- **SEER** = `(INVITE→200,480,486,600,603) / (Total INVITE - INVITE→3xx) × 100`
- **ISA** = `(INVITE→408,500,503,504) / Total INVITE × 100` (3xx NOT excluded)
- **SCR** = `(Completed Sessions) / Total INVITE × 100` (3xx NOT excluded)
- **ASR** = `(INVITE→200 OK) / Total INVITE × 100` (3xx NOT excluded)
- **NER** = `100 - ISA`
- **RRD** = histogram of REGISTER→200 OK delay (ms)
- **SPD** = histogram of session duration (sec)
- **TTR** = histogram of INVITE→first 1xx delay (ms)
- **ORD** = histogram of OPTIONS→any response delay (ms)
- **LRD** = histogram of REGISTER→3xx delay (ms)
- **ISS** = counter of INVITE→408/500/503/504
- **SDC** = counter of completed sessions

---

## 4. Tests — Coverage and Structure

### Unit Tests (4,912 lines, run via `make test`)

| File | Lines | Coverage Area |
|------|-------|---------------|
| `internal/service/metrics_test.go` | 1,904 | All 13 metrics (SER, SEER, ISA, SCR, ASR, NER, RRD, SPD, TTR, ORD, LRD, ISS, SDC), MC/DC approach, Request/Response methods, UpdateSession, isolated registry per test |
| `internal/exporter/exporter_test.go` | 2,132 | parseRawPacket (L2→SIP), sipPacketParse, handleMessage, handleResponse, handle200OKResponse, handleInvite200OK, handleBye200OK, handleRegister200OK, handleRegisterNon200Response, normalizeDialogID, extractTag, extractCSeq, extractSessionExpires, tracker operations |
| `internal/exporter/exporter_bench_test.go` | 316 | Go micro-benchmarks: parse BYE (~860ns), parse INVITE (~1.1us), parse 200 OK (~2.0us) |
| `internal/service/dialogs_test.go` | 212 | Create, Delete, Size, Cleanup, concurrent access, edge cases |
| `internal/config/config_test.go` | 189 | Environment variable parsing, defaults, validation |
| `internal/server/server_test.go` | 159 | HTTP server, /metrics, /health, graceful shutdown |
| `pkg/log/logger_test.go` | — | Zap logger setup, verbosity |

Package coverage (from README):
- `internal/config`: 100%
- `pkg/log`: 95.5%
- `internal/server`: 90.5%
- `internal/service`: 75.4%
- `internal/exporter`: 64.0%

### E2E Tests (1,990 lines, 14 test files, build tag `//go:build e2e`)

Located in `test/e2e/`. Use SIPp via testcontainers-go, run on loopback (`lo`) with `-parallel 4`.

| Test File | Lines | Scenarios Covered |
|-----------|-------|--------------------|
| `ser_test.go` | 99 | 100%, 0%, redirect, mixed, no INVITE, mixed 3xx+200 |
| `seer_test.go` | 151 | all_200, all_486, all_480, all_603, all_500, redirect_only, MixedEffective, MixedWithErrors, Mixed3xx, Complex |
| `isa_test.go` | 114 | all_500, all_503, all_200, Mixed |
| `scr_test.go` | 165 | AllScenarios, Mixed, MixedWith3xx, Complex, SessionExpires |
| `rrd_test.go` | 150 | Multiple scenarios |
| `spd_test.go` | 53 | SuccessfulCalls, NoCompletedCalls, Mixed |
| `asr_test.go` | 129 | AllScenarios, Mixed, MixedWith3xx, Complex |
| `sdc_test.go` | 150 | AllScenarios, Mixed, MixedWith3xx, Complex, SessionExpires |
| `ttr_test.go` | 153 | Multiple scenarios with 1xx provisional responses |
| `ner_test.go` | 104 | AllScenarios, Mixed, Equals100MinusISA |
| `iss_test.go` | 89 | AllScenarios, Mixed |
| `ord_test.go` | 54 | OptionsPing, NoOptions, MixedWithOptions |
| `lrd_test.go` | 63 | RegisterRedirect, Register200OK, RegisterError, Mixed |
| `e2e_test.go` | 516 | Test infrastructure: testEnv, port allocation, container management, metric parsing |

Supporting files: 30 SIPp XML scenarios in `test/e2e/sipp/` (UAS/UAC for each scenario type).

### Load Tests (1,597 lines, 7 test files, build tag `//go:build e2e`)

Located in `test/e2e/load/`. Separate from e2e tests (AF_PACKET contention on `lo`).

| Test File | Lines | Purpose |
|-----------|-------|---------|
| `load_test.go` | 614 | `TestLoad_INVITEFlood` (raw PPS), `TestLoad_FullCallFlow` (full dialog lifecycle), `TestLoad_ConcurrentSessions` |
| `benchmark_test.go` | 128 | `TestBenchmark_MemoryStability`, `TestBenchmark_ScrapeLatencyUnderLoad` |
| `gc_pause_test.go` | 119 | `TestBenchmark_GCPauseDuration` |
| `dialog_memory_test.go` | 139 | `TestBenchmark_MemoryPerDialog` |
| `scrape_latency_test.go` | 123 | P95 /metrics latency under load |
| `stability_test.go` | 132 | Sustained load memory growth |
| `results_test.go` | 342 | Baseline comparison (load_result.json vs baseline.json) |

Baseline: `test/e2e/load/baseline.json` tracks PPS, CPU, RAM, loss_rate, SER at 1800 CPS and 100 CPS.

---

## 5. Known Issues and Limitations

### From AGENTS.md (acknowledged, not blocking)
- **`gosec G115`** — integer overflow warnings (expected in network code)
- **`intrange`** — suggestions to use integer range syntax
- **`mnd`** — magic numbers in various files

### From ISSUES.md and code analysis
- **UDP only** — eBPF filter (`internal/bpf/sip.c:66`) and Go parser (`exporter.go:306`) only handle UDP (protocol 17). TCP/TLS not supported.
- **IPv4 only** — eBPF checks `eth_type != 0x0800` (IPv4), no IPv6 (0x
</task_result>
```

**Tool: bash**

**Input:**
```json
{
  "command": "cd /home/aleksey/go/src/github.com/sip-exporter && git log --oneline -20",
  "description": "Recent git commits"
}
```

**Output:**
```
ad70c5c docs: mark NER, ISS, ORD, LRD as implemented in ISSUES.md
c2ecffd feat: add NER, ISS, ORD, LRD metrics (v0.10.0)
71838c4 style: fix formatting in load test files
20bdb66 feat: add ASR (Answer Seizure Ratio) and SDC (Session Duration Counter) metrics
3fe8854 chore: remove AGENTS.md and ISSUES.md from git index
40fd60e feat: add TTR histogram metric, /health endpoint, Grafana TTR panel
ef3034b feat: replace SPD/RRD GaugeFunc with Prometheus Histogram
5a1763a refactor: update SPD e2e tests to use newTestEnv and t.Parallel
250a7b2 feat: load test baseline system, SCR loopback fix, RFC 6076 docs restructure
1c88e93 Merge branch 'ab/feature/rfc-new-feature' into develop
bd43a73 feat: add production benchmark suite and update README
f8bfcb1 docs: add roadmap with proposed metrics and product features
d7213c9 feat: add SPD (Session Process Duration) metric per RFC 6076 §4.7
6d3b6e3 docs: add RRD metric to CHANGELOG v0.8.0
ef4ab90 chore: remove SESSION_NOTES.md and TODO.md from git tracking
82267c0 docs: update documentation for v0.8.0 features
0840b9b docs: move ALERTING.md to docs/, update .gitignore
c4c751f chore: ignore test binaries and session notes
10cfb7a feat: add SCR Session-Expires support, fix race condition in metrics, refactor tests
ace9541 feat(grafana): add RRD panel to dashboard

```

---

## User

какую фичу возмём следующей

---

## Assistant (Plan · GLM-5.1 · 64.5s)

_Thinking:_

The user is asking what feature to pick next. Based on the project analysis, I can see the roadmap in ISSUES.md. The current version is v0.10.0, and the next planned version is v0.11 with "Per-Carrier Labels + Self-Monitoring". Let me check the ISSUES.md for more details about what's planned.

Судя по анализу проекта, текущая версия **v0.10.0** реализовала все основные RFC 6076 метрики. По роадмапу из ISSUES.md следующая — **v0.11: Per-Carrier Labels + Self-Monitoring**.

Вот варианты, что можно взять:

**Tool: question**

**Input:**
```json
{
  "questions": [
    {
      "question": "Какую фичу берём следующей?",
      "header": "Следующая фича",
      "options": [
        {
          "label": "v0.11: Self-Monitoring",
          "description": "Метрики самого экспортера: uptime, parse errors, channel capacity, ring buffer drops, goroutine count. Простой скоуп, не требует eBPF/парсер изменений."
        },
        {
          "label": "v0.11: Per-IP/Per-Carrier labels",
          "description": "Разделять метрики по IP/CIDR-группам (carrier label). Требует YAML конфиг, реворк Metricser, возможные изменения eBPF. Крупная фича."
        },
        {
          "label": "v0.12: SIP User-Agent классификация",
          "description": "Парсить заголовок User-Agent, regex-лейблы для классификации устройств/софтфонов."
        },
        {
          "label": "v0.14: RTCP Quality Metrics + MOS",
          "description": "Парсинг RTCP-пакетов, метрики джиттера/потерь/MOS. Требует расширения eBPF фильтра."
        },
        {
          "label": "v0.15: CDR Export",
          "description": "Экспорт Call Detail Records (JSON Lines, webhook, Kafka). Практичная фича для телекома."
        },
        {
          "label": "v0.16: OpenTelemetry + Helm Chart",
          "description": "OTLP экспорт + Kubernetes Helm chart для деплоя."
        }
      ]
    },
    {
      "question": "Какой приоритет: быстрые инкременты или крупные фичи?",
      "header": "Подход",
      "options": [
        {
          "label": "Быстрые инкременты",
          "description": "Self-Monitoring или небольшая фича за 1-2 сессии"
        },
        {
          "label": "Крупная фича",
          "description": "Per-Carrier Labels или CDR — полноценный спринт"
        }
      ]
    }
  ]
}
```

**Output:**
```
User has answered your questions: "Какую фичу берём следующей?"="отсортируй по приоритетам", "Какой приоритет: быстрые инкременты или крупные фичи?"="Крупная фича". You can now continue with the user's answers in mind.
```

---

## Assistant (Plan · GLM-5.1 · 15.6s)

_Thinking:_

The user wants me to sort by priority and is interested in a large feature. Let me read the ISSUES.md to get the full roadmap and detailed descriptions, then provide a prioritized list.

Понял, хочешь крупную фичу и приоритизированный список. Давай посмотрю детали роадмапа:

**Tool: glob**

**Input:**
```json
{
  "pattern": "**/ISSUES.md"
}
```

**Output:**
```
/home/aleksey/go/src/github.com/sip-exporter/ISSUES.md
```

---

## Assistant (Plan · GLM-5.1 · 6.9s)

**Tool: read**

**Input:**
```json
{
  "filePath": "/home/aleksey/go/src/github.com/sip-exporter/ISSUES.md"
}
```

**Output:**
```
<path>/home/aleksey/go/src/github.com/sip-exporter/ISSUES.md</path>
<type>file</type>
<content>
1: # SIP Exporter — Roadmap & Feature Proposals
2: 
3: ## Реализованные метрики (v0.5–0.10)
4: 
5: | Метрика | RFC 6076 | Описание | Версия |
6: |---------|----------|----------|--------|
7: | SER | §4.6 | Session Establishment Ratio | 0.5.0 |
8: | SEER | §4.7 | Session Establishment Effectiveness Ratio | 0.6.0 |
9: | ISA | §4.8 | Ineffective Session Attempts | 0.7.0 |
10: | SCR | §4.9 | Session Completion Ratio | 0.8.0 |
11: | RRD | §4.1 | Registration Request Delay (histogram) | 0.8.0 |
12: | SPD | §4.5 | Session Process Duration (histogram) | 0.9.0 |
13: | TTR | — | Time to First Response (histogram) | 0.10.0 |
14: | NER | — | Network Effectiveness Ratio (GSMA IR.42) | 0.10.0 |
15: | ISS | — | Ineffective Session Severity (counter) | 0.10.0 |
16: | ORD | — | OPTIONS Response Delay (histogram) | 0.10.0 |
17: | LRD | — | Location Registration Delay (histogram) | 0.10.0 |
18: 
19: ---
20: 
21: ## Предлагаемые метрики RFC 6076
22: 
23: ### ~~P0: TTR — Time to First Response~~ ✅ Реализовано (v0.10.0)
24: 
25: **Описание:** время от INVITE до первого provisional response (100/180/183). Не определена в RFC 6076, но является полезной операционной метрикой.
26: 
27: **Зачем:** позволяет отслеживать задержки на стороне сервера (медленные SIP-прокси). Непосредственно влияет на пользовательский опыт — «сколько времени сервер думает перед ответом».
28: 
29: **Реализация:** отдельный `inviteTracker` map (по аналогии с `registerTracker`), замер при получении первого 1xx ответа на INVITE. Histogram `sip_exporter_ttr` с бакетами `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` ms.
30: 
31: **Формула:**
32: ```
33: TTR = Average(Time of first 1xx response - Time of INVITE request)
34: ```
35: 
36: **Почему P0:** естественно расширяет текущую архитектуру с `dialogEntry`, даёт операционную видимость.
37: 
38: ---
39: 
40: ### ~~P1: SDC — Session Duration Counter~~ ✅ Реализовано
41: 
42: **Описание:** общее количество завершённых сессий (BYE 200 OK). Не определена в RFC 6076, но является полезным дополнением.
43: 
44: **Зачем:** простой счётчик, дополняет SPD для понимания «сколько сессий в среднем за период». Позволяет строить rate-графики (`rate(sip_exporter_sdc_total[5m])`).
45: 
46: **Реализация:** `prometheus.Counter` `sip_exporter_sdc_total` с инкрементом в `SessionCompleted()` (вызывается из `handleBye200OK` и `sipDialogMetricsUpdate`). Unit-тесты + e2e (`sdc_test.go`).
47: 
48: ---
49: 
50: ### ~~P1: ASR — Answer Seizure Ratio~~ ✅ Реализовано
51: 
52: **Описание:** классика телефонии — ratio ответов (200 OK на INVITE) к общему числу INVITE.
53: 
54: **Формула:**
55: ```
56: ASR = (INVITE → 200 OK) / Total INVITE × 100
57: ```
58: 
59: **Отличие от SER:** не исключает 3xx из знаменателя. Проще интерпретировать, стандартная метрика в телекоме (ITU-T E.411).
60: 
61: **Реализация:** `prometheus.GaugeFunc` `sip_exporter_asr`, unit-тесты + e2e (`asr_test.go`).
62: 
63: ---
64: 
65: ### ~~P2: NER — Network Effectiveness Ratio~~ ✅ Реализовано (v0.10.0)
66: 
67: **Описание:** NER = 100 − ISA, показывает долю вызовов без сетевых проблем. GSMA IR.42.
68: 
69: **Реализация:** `prometheus.GaugeFunc` `sip_exporter_ner`, unit-тесты + e2e (`ner_test.go`).
70: 
71: ---
72: 
73: ### ~~P3: LRD — Location Registration Delay~~ ✅ Реализовано (v0.10.0)
74: 
75: **Описание:** задержка между REGISTER и 3xx redirect. Histogram `sip_exporter_lrd` с бакетами `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` ms.
76: 
77: **Реализация:** переиспользует `registerTracker` (аналог RRD), измеряет REGISTER→3xx. E2e тесты с SIPp redirect-сценариями (`reg_uas_redirect.xml`, `reg_uac_redirect.xml`).
78: 
79: ---
80: 
81: ### ~~P3: ISS — Ineffective Session Severity~~ ✅ Реализовано (v0.10.0)
82: 
83: **Описание:** счётчик INVITE→408/500/503/504 ответов. `prometheus.Counter` `sip_exporter_iss_total`.
84: 
85: **Реализация:** инкремент в `isIneffectiveResponse()` (переиспользует коды ISA). Полезен для alerting на абсолютный объём ошибок.
86: 
87: ---
88: 
89: ## Предлагаемые продуктыые фичи
90: 
91: ### P0: Per-IP / Per-Carrier метрики (дифференциатор)
92: 
93: **Проблема:** все метрики — глобальные счётчики без labels. Оператор не может ответить на вопрос «какой carrier/транк деградирует?».
94: 
95: **Решение:**
96: - Добавить `carrier` label через конфигурируемые CIDR-группы
97: - Парсить source/destination IP из пакета (уже есть в L2→L3 парсере)
98: - Новые метрики: `sip_exporter_ser{carrier="provider-a"}`, `sip_exporter_isa{carrier="provider-b"}`
99: 
100: **Конфигурация:**
101: ```yaml
102: carriers:
103:   - name: "provider-a"
104:     cidr: "10.0.1.0/24"
105:   - name: "provider-b"
106:     cidr: "10.0.2.0/24"
107:   - name: "internal"
108:     cidr: "192.168.0.0/16"
109: ```
110: 
111: **Почему P0:** ни один open-source SIP exporter такого не делает. Это главный дифференциатор для enterprise-заказчиков.
112: 
113: **Сложность:** средняя. Требует:
114: - Изменить конфигурацию (YAML вместо env-only)
115: - Изменить eBPF packet parsing — извлечь source/destination IP
116: - Переработать `Metricser` — добавить label dimension
117: - Обновить Grafana dashboard
118: 
119: ---
120: 
121: ### ~~P0: Histogram вместо среднего для SPD/RRD~~ ✅ Реализовано (v0.10.0)
122: 
123: **Проблема:** SPD и RRD — средние значения за всё время работы. Если первые 100 звонков были по 1 секунде, а потом пошли по 30 минут — среднее долго не отреагирует. Бесполезно для alerting.
124: 
125: **Решение:** заменить `prometheus.GaugeFunc` на `prometheus.Histogram` с бакетами.
126: 
127: **RRD бакеты (ms):**
128: ```
129: [1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]
130: ```
131: 
132: **SPD бакеты (sec):**
133: ```
134: [1, 5, 10, 30, 60, 300, 600, 1800, 3600]
135: ```
136: 
137: **Преимущества:**
138: - Prometheus автоматически даст p50, p90, p99 через `histogram_quantile()`
139: - Индустриальный стандарт для latency-метрик
140: - Можно строить SLO на основе percentile-based alerting
141: 
142: **PromQL примеры:**
143: ```promql
144: # 95-й перцентиль задержки регистрации
145: histogram_quantile(0.95, rate(sip_exporter_rrd_bucket[5m]))
146: 
147: # 99-й перцентиль длительности сессии
148: histogram_quantile(0.99, rate(sip_exporter_spd_bucket[5m]))
149: ```
150: 
151: **Обратная совместимость:** оставить старые GaugeFunc как `sip_exporter_spd_average` / `sip_exporter_rrd_average` на переходный период.
152: 
153: ---
154: 
155: ### P1: TCP/TLS поддержка
156: 
157: **Проблема:** eBPF-фильтр захватывает только UDP (`exporter.go:249`: `if ipHeader[9] != 17 // UDP`). В продакшене SIP-инфраструктура почти всегда использует TCP и TLS (SIPS port 5061).
158: 
159: **Решение:**
160: - Расширить eBPF-парсер для TCP-потоковой реассемблизации
161: - Использовать Content-Length header для определения границ SIP-сообщения в TCP-потоке
162: - Для TLS — варианты:
163:   1. SSL/TLS offload (расшифровка на termination point, мониторинг plaintext после него)
164:   2. SPAN-port подход (зеркалирование трафика на decryption point)
165:   3. eBPF uprobe на OpenSSL/BoringSSL для перехвата decrypted данных
166: 
167: **Сложность:** высокая. TCP реассемблизация — нетривиальная задача.
168: 
169: ---
170: 
171: ### ~~P1: Health endpoint + Graceful Shutdown~~ ✅ Реализовано (v0.10.0)
172: 
173: **Описание:** `/health` endpoint для Kubernetes probes + `IsAlive()` на Exporter.
174: 
175: **Реализация:**
176: - `GET /health` → 200 OK (экспортер инициализирован) / 503 Service Unavailable
177: - `Exporter.IsAlive()` через `atomic.Bool`
178: - Graceful shutdown уже был реализован (SIGTERM → `exporter.Close()` → `http.Server.Shutdown(10s)`)
179: 
180: **Kubernetes probes:**
181: ```yaml
182: livenessProbe:
183:   httpGet:
184:     path: /health
185:     port: 2112
186: readinessProbe:
187:   httpGet:
188:     path: /health
189:     port: 2112
190: ```
191: 
192: ---
193: 
194: ### ~~P2: SIP OPTIONS Ping Monitoring~~ ✅ Реализовано (v0.10.0)
195: 
196: **Проблема:** SIP trunk-провайдеры используют OPTIONS ping для проверки alive-статуса. Мониторинг ответов на OPTIONS — критичная фича для NOC-команд.
197: 
198: **Реализация:** `sip_exporter_ord` histogram — задержка от OPTIONS до любого ответа. `optionsTracker` map по Call-ID с TTL cleanup (60s). Аналогична RRD, но для OPTIONS. E2e тесты + unit-тесты.
199: 
200: ---
201: 
202: ### P2: OpenTelemetry Export
203: 
204: **Проблема:** мир движется к OTel. Ограничение только Prometheus exposition format сужает рынок интеграции.
205: 
206: **Решение:**
207: - Добавить `SIP_EXPORTER_OTEL_ENDPOINT` env var
208: - Экспортировать те же метрики через OTLP protocol
209: - Поддержка: Datadog, New Relic, Splunk, Honeycomb, Grafana Tempo
210: - Библиотека `go.opentelemetry.io/otel` уже в зависимостях (indirect)
211: 
212: **Конфигурация:**
213: ```yaml
214: SIP_EXPORTER_OTEL_ENDPOINT=otel-collector:4317
215: SIP_EXPORTER_OTEL_PROTOCOL=grpc  # grpc | http
216: ```
217: 
218: **Сложность:** средняя.
219: 
220: ---
221: 
222: ### P3: Multi-interface мониторинг
223: 
224: **Проблема:** `SIP_EXPORTER_INTERFACE` принимает один интерфейс. На border-серверах обычно 2+ NIC (internal + external).
225: 
226: **Решение:**
227: ```yaml
228: SIP_EXPORTER_INTERFACES=eth0,eth1
229: ```
230: 
231: Отдельный eBPF socket на каждый интерфейс.
232: 
233: **Workaround:** запустить несколько инстансов экспортера.
234: 
235: **Сложность:** средняя.
236: 
237: ---
238: 
239: ## Новые фичи — анализ конкурентного преимущества
240: 
241: ### Конкурентный ландшафт
242: 
243: | Проект | Звёзды | Подход | Слабости |
244: |--------|--------|--------|----------|
245: | HOMER (sipcapture/homer) | 1.9k | Полная платформа захвата (HEP), UI, ClickHouse | Тяжёлый стек, не Prometheus-native |
246: | kamailio_exporter | 62 | BINRPC к Kamailio | Только Kamailio, нужен прямой доступ |
247: | KRAM-PRO/SIP_Exporter | 2 | OPTIONS ping (Python) | Примитивный скрипт |
248: 
249: **Текущая позиция sip-exporter:** технический лидер в нише «passive SIP Prometheus exporter». Единственный eBPF-based, самая полная реализация RFC 6076, лучший test coverage. Для перехода от «лучший экспортер» к «must-have инструмент» нужны продуктовые фичи.
250: 
251: ---
252: 
253: ### Tier 1: Enterprise Must-Have
254: 
255: #### P0: Per-IP / Per-Carrier метрики (дифференциатор)
256: 
257: **Проблема:** все метрики — глобальные счётчики без labels. Оператор не может ответить на вопрос «какой carrier/транк деградирует?».
258: 
259: **Решение:**
260: - Добавить `carrier` label через конфигурируемые CIDR-группы
261: - Парсить source/destination IP из пакета (уже есть в L2→L3 парсере)
262: - Новые метрики: `sip_exporter_ser{carrier="provider-a"}`, `sip_exporter_isa{carrier="provider-b"}`
263: 
264: **Конфигурация:**
265: ```yaml
266: carriers:
267:   - name: "provider-a"
268:     cidr: "10.0.1.0/24"
269:   - name: "provider-b"
270:     cidr: "10.0.2.0/24"
271:   - name: "internal"
272:     cidr: "192.168.0.0/16"
273: ```
274: 
275: **Почему P0:** ни один open-source SIP exporter такого не делает. Это главный дифференциатор для enterprise-заказчиков.
276: 
277: **Сложность:** средняя. Требует:
278: - Изменить конфигурацию (YAML вместо env-only)
279: - Изменить eBPF packet parsing — извлечь source/destination IP
280: - Переработать `Metricser` — добавить label dimension
281: - Обновить Grafana dashboard
282: 
283: ---
284: 
285: #### P1: TCP/TLS поддержка
286: 
287: **Проблема:** eBPF-фильтр захватывает только UDP. В продакшене ~40-60% SIP трафика — TCP/TLS (SIPS port 5061).
288: 
289: **Решение:**
290: - Расширить eBPF-парсер для TCP-потоковой реассемблизации
291: - Использовать Content-Length header для определения границ SIP-сообщения в TCP-потоке
292: - Для TLS — варианты:
293:   1. SSL/TLS offload (расшифровка на termination point, мониторинг plaintext после него) — самый практичный
294:   2. SPAN-port подход (зеркалирование трафика на decryption point)
295:   3. eBPF uprobe на OpenSSL/BoringSSL для перехвата decrypted данных
296: 
297: **Сложность:** высокая. TCP реассемблизация — нетривиальная задача.
298: 
299: **Почему P1:** без этого продукт отсекает 40-60% production-сценариев.
300: 
301: ---
302: 
303: #### P1: RTP/RTCP Quality Metrics (главный новый дифференциатор)
304: 
305: **Проблема:** мониторинг SIP signaling без quality-метрик — только половина картины. VoIP-оператору нужен MOS, jitter, packet loss.
306: 
307: **Решение:** парсить RTCP Receiver Reports (RFC 3550) параллельно с SIP:
308: 
309: **Метрики:**
310: ```promql
311: sip_exporter_rtp_packet_loss_percent{call_id="..."}   # % потерянных RTP-пакетов
312: sip_exporter_rtp_jitter_ms{call_id="..."}              # джиттер в ms (histogram)
313: sip_exporter_rtp_mos_estimate{call_id="..."}           # синтетический MOS score (1-5)
314: sip_exporter_rtp_r_factor{call_id="..."}               # R-factor по ITU-T G.107
315: sip_exporter_rtcp_reports_total                         # количество обработанных RTCP отчетов
316: ```
317: 
318: **MOS calculation:** ITU-T G.107 E-Model → R-factor → MOS:
319: ```
320: R = 94.2 - Ie - Id - Is
321: MOS = 1 + (0.035 * R) + (7 * 10^-6 * R * (R - 60) * (100 - R))
322: ```
323: 
324: Где `Ie` (equipment impairment) из таблицыcodec-specific (G.113), `Id` — delay impairment, `Is` — simultaneous impairment.
325: 
326: **Архитектура:**
327: - eBPF-фильтр: добавить RTCP (UDP, не только SIP-порты) — конфигурируемый RTP port range
328: - Новый интерфейс `Qualityer` с методами `UpdateJitter()`, `UpdatePacketLoss()`, `UpdateMOS()`
329: - Correlate RTCP с SIP dialog через SDP negotiation (порт → Call-ID mapping)
330: - Histogram бакеты jitter: `[0.1, 0.5, 1, 5, 10, 20, 50, 100, 200, 500]` ms
331: - Histogram бакеты MOS: `[1.0, 1.5, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5.0]`
332: 
333: **Почему P1:** HOMER делает это, но как тяжёлая платформа. Лёгкий eBPF-based RTP мониторинг — уникальное предложение на рынке. Превращает product из «SIP метрик» в «полный VoIP quality мониторинг».
334: 
335: **Сложность:** высокая. Требует:
336: - Новый eBPF-фильтр для RTCP (отдельный от SIP)
337: - SDP-парсинг для port→Call-ID correlation
338: - E-Model реализация для MOS/R-factor
339: - Значительное расширение тестов
340: 
341: ---
342: 
343: ### Tier 2: Growth Features
344: 
345: #### P2: OpenTelemetry Export
346: 
347: **Проблема:** мир движется к OTel. Ограничение только Prometheus exposition format сужает рынок интеграции.
348: 
349: **Решение:**
350: - Добавить `SIP_EXPORTER_OTEL_ENDPOINT` env var
351: - Экспортировать те же метрики через OTLP protocol
352: - Поддержка: Datadog, New Relic, Splunk, Honeycomb, Grafana Tempo
353: - Библиотека `go.opentelemetry.io/otel` уже в зависимостях (indirect)
354: 
355: **Конфигурация:**
356: ```yaml
357: SIP_EXPORTER_OTEL_ENDPOINT=otel-collector:4317
358: SIP_EXPORTER_OTEL_PROTOCOL=grpc  # grpc | http
359: ```
360: 
361: **Сложность:** средняя.
362: 
363: **Почему P2:** OTel = доступ к рынку enterprise observability (Datadog, New Relic, Splunk).
364: 
365: ---
366: 
367: #### P2: Self-Monitoring Metrics
368: 
369: **Проблема:** оператор не знает, работает ли экспортер корректно. Нет видимости внутренних проблем (ring buffer overflow, parse errors, channel congestion).
370: 
371: **Решение:**
372: 
373: ```promql
374: sip_exporter_uptime_seconds                              # время работы (gauge)
375: sip_exporter_packets_captured_total                      # захвачено пакетов (counter)
376: sip_exporter_packets_dropped_total                       # потеряно на ring buffer (counter)
377: sip_exporter_parse_errors_total                          # невалидные SIP-пакеты (counter)
378: sip_exporter_channel_capacity_ratio                      # заполненность messages channel (gauge, 0-1)
379: sip_exporter_ebpf_events_lost_total                      # потеряно на eBPF level (counter)
380: sip_exporter_active_trackers{type="register|invite|options"}  # размер tracker maps (gauge)
381: sip_exporter_active_dialogs                               # alias для sessions (gauge)
382: sip_exporter_config_reload_total                          # количество hot-reload (counter)
383: ```
384: 
385: Go runtime metrics через стандартный `promhttp.Handler()`:
386: ```go
387: mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
388: ```
389: 
390: **Сложность:** низкая. Большинство данных уже есть в структуре `exporter`.
391: 
392: **Почему P2:** без self-monitoring невозможно отличить «трафик упал» от «экспортер сломался».
393: 
394: ---
395: 
396: #### P2: CDR Export (Call Detail Records)
397: 
398: **Проблема:** каждый VoIP-оператор нуждается в CDR для биллинга и отчётности. Сейчас sip-exporter видит все данные для генерации CDR, но не экспортирует их.
399: 
400: **Решение:**
401: 
402: Формат CDR записи:
403: ```json
404: {
405:   "call_id": "abc123@10.0.1.5",
406:   "from_uri": "sip:user1@example.com",
407:   "to_uri": "sip:user2@example.com",
408:   "from_tag": "abc",
409:   "to_tag": "xyz",
410:   "invite_time": "2026-04-20T10:30:00Z",
411:   "answer_time": "2026-04-20T10:30:02Z",
412:   "end_time": "2026-04-20T10:30:47Z",
413:   "duration_ms": 45000,
414:   "setup_duration_ms": 2000,
415:   "end_reason": "bye",
416:   "status_code": 200,
417:   "source_ip": "10.0.1.5",
418:   "destination_ip": "10.0.2.10",
419:   "carrier": "provider-a"
420: }
421: ```
422: 
423: Каналы экспорта:
424: - **Файл:** JSON Lines `/var/log/sip-exporter/cdr.jsonl` с rotation
425: - **HTTP webhook:** POST на конфигурируемый URL
426: - **Kafka/NATS:** push в message queue (опционально)
427: - **Stdout:** для container-native (docker logs → Fluentd/Loki)
428: 
429: **Конфигурация:**
430: ```yaml
431: cdr:
432:   enabled: true
433:   output: "file"  # file | webhook | stdout
434:   path: "/var/log/sip-exporter/cdr.jsonl"
435:   webhook_url: "http://billing-service/api/cdr"
436: ```
437: 
438: **Архитектура:**
439: - Новый интерфейс `CdrWriter` с методами `Write(record CdrRecord)`
440: - CDR генерируется в `handleBye200OK()` и `sipDialogMetricsUpdate()` (expired dialogs)
441: - Асинхронная запись через buffered channel
442: - Requires: парсинг From/To URI (уже есть в dto.Packet)
443: 
444: **Почему P2:** CDR = биллинг + отчётность. Это то, за что платят деньги.
445: 
446: **Сложность:** средняя.
447: 
448: ---
449: 
450: #### P2: SIP User-Agent Classification
451: 
452: **Проблема:** оператор не видит, какие устройства/серверы генерируют ошибки. «Проблема с Yealink T46S» vs «проблема с Asterisk» — разная реакция.
453: 
454: **Решение:**
455: - Парсить `User-Agent` и `Server` заголовки из SIP-пакетов
456: - Классифицировать по встроенной базе паттернов
457: - Добавить label `ua_type` к существующим метрикам:
458:   ```
459:   sip_exporter_invite_total{ua_type="yealink"}
460:   sip_exporter_response_total{status="503",ua_type="asterisk"}
461:   ```
462: 
463: **База паттернов (расширяемая через конфигурацию):**
464: ```yaml
465: user_agents:
466:   - pattern: "Yealink.*"
467:     label: "yealink"
468:   - pattern: "Grandstream.*"
469:     label: "grandstream"
470:   - pattern: "Cisco/SPA.*"
471:     label: "cisco_spa"
472:   - pattern: "Kamailio.*"
473:     label: "kamailio"
474:   - pattern: "OpenSIPS.*"
475:     label: "opensips"
476:   - pattern: "Asterisk.*"
477:     label: "asterisk"
478:   - pattern: "FreeSWITCH.*"
479:     label: "freeswitch"
480:   - pattern: "Linphone.*"
481:     label: "linphone"
482:   - pattern: "MicroSIP.*"
483:     label: "microsip"
484:   - pattern: "Zoiper.*"
485:     label: "zoiper"
486:   - default: "other"
487: ```
488: 
489: **Архитектура:**
490: - Парсинг `User-Agent`/`Server` заголовка в `sipPacketParse()` — добавить поле `UserAgent []byte` в `dto.Packet`
491: - Классификация через regex match (pre-compiled при загрузке конфигурации)
492: - Добавить `ua_type` label dimension к `Metricser`
493: 
494: **Сложность:** средняя. Требует переработки `Metricser` (добавление label).
495: 
496: **Почему P2:** бесценно для troubleshooting — «какие телефоны генерируют больше ошибок».
497: 
498: ---
499: 
500: #### P3: Hot-Reload Configuration
501: 
502: **Проблема:** рестарт для смены конфигурации = риск потери пакетов в 24/7 NOC.
503: 
504: **Решение:**
505: - SIGHUP → перечитывание конфигурации без остановки
506: - Применимо к: порты, carrier CIDR-группы, UA-паттерны
507: - Не применимо к: интерфейс (требует пересоздания AF_PACKET socket)
508: 
509: **Сложность:** низкая.
510: 
511: ---
512: 
513: ### Tier 3: Completeness Features
514: 
515: #### P3: Multi-interface мониторинг
516: 
517: **Проблема:** `SIP_EXPORTER_INTERFACE` принимает один интерфейс. На border-серверах обычно 2+ NIC (internal + external).
518: 
519: **Решение:**
520: ```yaml
521: SIP_EXPORTER_INTERFACES=eth0,eth1
522: ```
523: 
524: Отдельный eBPF socket на каждый интерфейс.
525: 
526: **Workaround:** запустить несколько инстансов экспортера.
527: 
528: **Сложность:** средняя.
529: 
530: ---
531: 
532: #### P3: IPv6 Support
533: 
534: **Проблема:** eBPF-фильтр обрабатывает только IPv4 (ethertype 0x0800). В современных сетях растёт доля IPv6.
535: 
536: **Решение:**
537: - Добавить обработку ethertype 0x86DD (IPv6) в eBPF-фильтр
538: - IPv6 header: 40 байт fixed, Next Header вместо Protocol
539: - Обновить L3→L4 парсинг в `parseRawPacket()`
540: 
541: **Сложность:** средняя. Требует изменения eBPF C-кода + Go парсера.
542: 
543: ---
544: 
545: #### P3: SIP Retransmission Detection
546: 
547: **Проблема:** SIP retransmissions (duplicate INVITE/BYE с тем же Call-ID + CSeq) — индикатор потери пакетов или перегрузки сервера. RFC 3261 §17.
548: 
549: **Решение:**
550: - Детекция: пакет с тем же Call-ID + CSeq (ID + Method) что уже был обработан
551: - Метрика: `sip_exporter_retransmissions_total{method="INVITE"}` (counter)
552: - Ограничение: tracker с TTL (как registerTracker), хранить хэш последних N Call-ID+CSeq
553: 
554: **Сложность:** низкая. Переиспользует архитектуру tracker maps.
555: 
556: ---
557: 
558: #### P3: GeoIP Enrichment
559: 
560: **Проблема:** IP-адреса без гео-контекста менее информативны для дашбордов.
561: 
562: **Решение:**
563: - Интеграция MaxMind GeoLite2 (бесплатная база)
564: - Embed в бинарник или загружать по пути
565: - Добавить labels: `sip_exporter_invite_total{country="RU", city="Moscow"}`
566: - Требует: Per-Carrier Labels (P0) как prerequisite
567: 
568: **Конфигурация:**
569: ```yaml
570: geoip:
571:   enabled: true
572:   db_path: "/usr/local/share/GeoLite2-City.mmdb"
573: ```
574: 
575: **Сложность:** средняя.
576: 
577: ---
578: 
579: #### P3: HOMER/HEP Protocol Export
580: 
581: **Проблема:** многие операторы уже используют HOMER для packet capture. sip-exporter не интегрирован с этой экосистемой.
582: 
583: **Решение:**
584: - Генерация HEPv3 (Homer Encapsulation Protocol) пакетов
585: - Экспорт захваченных SIP-пакетов в HOMER/heplify-server
586: - sip-exporter как lightweight capture agent → HOMER как full troubleshooting UI
587: - Конфигурация: `SIP_EXPORTER_HEP_ENDPOINT=heplify-server:9060`
588: 
589: **Почему P3:** мост между лёгким мониторингом (sip-exporter) и полной платформой (HOMER). Не all-or-nothing.
590: 
591: **Сложность:** средняя. HEPv3 — простой binary protocol.
592: 
593: ---
594: 
595: #### P3: Kubernetes Helm Chart
596: 
597: **Проблема:** нет стандартного способа деплоя в Kubernetes.
598: 
599: **Решение:**
600: - Helm chart с `values.yaml`
601: - `ServiceMonitor` для Prometheus Operator
602: - `PodMonitor` для VictoriaMetrics Operator
603: - ConfigMap для carrier-конфигурации
604: - `PriorityClass: system-node-critical` (monitoring не должен вытесняться)
605: 
606: **Сложность:** низкая. Стандартная задача.
607: 
608: ---
609: 
610: ## Приоритет реализации (обновлённый)
611: 
612: ### Реализованные фичи
613: 
614: | Приоритет | Фича | Версия |
615: |-----------|------|--------|
616: | ~~P0~~ | Histogram для SPD/RRD | ✅ v0.10.0 |
617: | ~~P0~~ | TTR (Time to First Response) | ✅ v0.10.0 |
618: | ~~P1~~ | Health + graceful shutdown | ✅ v0.10.0 |
619: | ~~P1~~ | SDC (Session Duration Counter) | ✅ v0.9.0 |
620: | ~~P1~~ | ASR (Answer Seizure Ratio) | ✅ v0.9.0 |
621: | ~~P2~~ | NER (Network Effectiveness Ratio) | ✅ v0.10.0 |
622: | ~~P2~~ | SIP OPTIONS monitoring (ORD) | ✅ v0.10.0 |
623: | ~~P3~~ | ISS (Ineffective Session Severity) | ✅ v0.10.0 |
624: | ~~P3~~ | LRD (Location Registration Delay) | ✅ v0.10.0 |
625: 
626: ### Roadmap по версиям
627: 
628: | Версия | Фичи | Обоснование |
629: |--------|-------|-------------|
630: | **v0.11** | Per-Carrier Labels + Self-Monitoring | Enterprise не купится без multi-tenant видимости; self-monitoring для отличия «трафик упал» от «экспортер сломался» |
631: | **v0.12** | SIP User-Agent Classification | Troubleshooting по типам устройств — топ-2 запрос NOC после per-carrier |
632: | **v0.13** | TCP Support (TLS offload) | Открытие 40-60% рынка production-сценариев |
633: | **v0.14** | RTCP Quality Metrics + MOS | Уникальный дифференциатор — «полный VoIP quality мониторинг», никто в OSS этого не делает на eBPF |
634: | **v0.15** | CDR Export | Монетизация, биллинг, отчётность |
635: | **v0.16** | OpenTelemetry + Helm Chart | Расширение рынка интеграции + упрощение деплоя |
636: | **v0.17** | Hot-reload + IPv6 + Multi-interface | Production completeness |
637: | **v1.0** | Retransmission Detection + GeoIP + HOMER/HEP | Полнота продукта для GA |
638: 
639: ### Итог
640: 
641: Для перехода от «лучший экспортер» к «must-have инструмент №1» критичны 3 вещи:
642: 
643: 1. **Per-carrier labels** — оператор должен видеть каждый транк отдельно
644: 2. **RTP/RTCP quality metrics** — мониторинг без quality = полдела
645: 3. **TCP/TLS** — без этого продукт отсекает 40-60% production-сценариев
646: 
647: ---
648: 
649: ## Дистрибуция — роадмап распространения
650: 
651: ### Текущее состояние
652: 
653: - GitHub: 10 звёзд, 0 фолловеров
654: - Docker Hub: `frzq/sip-exporter:latest`
655: - Документация: README + docs/ (хорошее состояние)
656: - Нет: блога, social media, conference talks, community presence
657: - Целевая аудитория: VoIP-операторы, NOC-инженеры, DevOps/SRE в телекоме, Kamailio/OpenSIPS/Asterisk администраторы
658: 
659: ### Целевые метрики
660: 
661: | Метрика | Текущее | +3 мес | +6 мес | +12 мес |
662: |---------|---------|--------|--------|---------|
663: | GitHub Stars | 10 | 100 | 300 | 1000 |
664: | Docker Hub pulls | — | 500+ | 2K+ | 10K+ |
665: | Known production users | 0 | 3-5 | 10+ | 30+ |
666: | Внешние contributions | 0 | 1-2 | 5+ | 15+ |
667: | Articles/talks mentioning | 0 | 3-5 | 10+ | 20+ |
668: 
669: ---
670: 
671: ### Фаза 1: Фундамент (Недели 1-2)
672: 
673: Цель: подготовить все точки касания к привлечению пользователей.
674: 
675: #### 1.1 GitHub Profile Package
676: 
677: - [ ] **README.md** — добавить badge: Docker Pulls, GitHub Releases, Go Reference
678: - [ ] **Screenshots/GIF** — добавить анимацию в README: Grafana dashboard в действии (SER gauge, traffic graph)
679: - [ ] **CONTRIBUTING.md** — правила контрибуции (как запускать тесты, стиль кода, PR process)
680: - [ ] **GitHub Topics** — добавить: `sip`, `voip`, `ebpf`, `prometheus-exporter`, `telecom`, `monitoring`, `sip-monitoring`, `voip-monitoring`, `kamailio`, `opensips`
681: - [ ] **GitHub Releases** — опубликовать v0.10.0 release с бинарниками (linux/amd64, linux/arm64) через GoReleaser + GitHub Actions
682: - [ ] **GitHub Discussions** — включить, создать категории: Q&A, Ideas, Show & Tell
683: - [ ] **Repo description** — убедиться что SEO-friendly: «High-performance eBPF-based SIP/VoIP monitoring exporter for Prometheus»
684: 
685: #### 1.2 Docker Hub
686: 
687: - [ ] **Automated builds** — GitHub Actions → Docker Hub push на каждый tag
688: - [ ] **Multi-arch** — amd64 + arm64 (телеком железо часто ARM)
689: - [ ] **Tags strategy** — `latest`, `0.10.0`, `0.10`, `0` (semver ranges)
690: - [ ] **Description** — заполнить overview на Docker Hub с примером docker-compose
691: 
692: #### 1.3 Документация
693: 
694: - [ ] **Quick Start Guide** — 5-минутный туториал: `docker compose up` → Grafana → видеть метрики (можно как отдельный docs/QUICKSTART.md)
695: - [ ] **Comparison table** — docs/COMPARISON.md: sip-exporter vs HOMER vs kamailio_exporter vs KRAM-PRO/SIP_Exporter
696: - [ ] **Architecture diagram** — визуальная схема (Mermaid или ASCII) в README/документации
697: - [ ] **Use Cases page** — docs/USE_CASES.md: «SIP trunk monitoring», «carrier SLA», «NOC dashboard», «debugging call quality»
698: 
699: ---
700: 
701: ### Фаза 2: Подготовка аккаунтов (Недели 3-4)
702: 
703: Цель: создать и «прогреть» аккаунты на целевых площадках. Нельзя прийти с нуля и начать постить свои проекты — удалят или проигнорируют.
704: 
705: #### 2.1 Реалии площадок — барьеры входа
706: 
707: | Площадка | Барьер | Что нужно ДО любого поста | Время подготовки |
708: |----------|--------|---------------------------|------------------|
709: | **Hacker News** | Низкий. Show HN специально для своих проектов. Нужен аккаунт >1 дня. | Зарегистрироваться. 1-2 дня просто почитать/проголосовать. Понять стиль «Show HN». | 2-3 дня |
710: | **Reddit** | Высокий. Karma-требования, self-promotion rules (10:1 ratio), AutoModerator. | Зарегистрировать аккаунт. Нарастить ~100-200 karma: отвечать на вопросы в r/voip, r/selfhosted, r/devops, r/kubernetes. Помогать людям. | 2-4 недели |
711: | **Twitter/X** | Средний. 0 followers = 0 охват. Алгоритм не показывает новые аккаунты. | Создать профиль. 2-4 недели писать полезные треды про VoIP/eBPF/Prometheus, комментировать чужие, нарастить 200-500 followers. | 3-6 недель |
712: | **LinkedIn** | Средний. Зависит от числа connections. | Расширить сеть: подключиться к VoIP/SRE/DevOps людям. Писать «insights» и «posts» (не просто ссылки). | 2-4 недели |
713: | **Dev.to** | Низкий. Любой может публиковать. Доброжелательны к self-promotion. | Зарегистрироваться. Понять формат (практические туториалы, не PR-посты). | 1-2 дня |
714: | **Medium** | Низкий. Любой может публиковать. | Зарегистрироваться. Писать в publications (Better Programming, Towards DevOps) для большего охвата. | 1-2 дня |
715: | **Habr** | Средний. Для публикации в хабах нужна карма ≥5 или заявка в песочницу. | Зарегистрироваться. Написать 1-2 статьи в песочницу (хабы: Linux, Сетевые технологии). Комментировать чужие статьи. | 2-4 недели |
716: | **Telegram группы** | Низкий. Любой может писать. | Вступить в группы (@voip_dev, @sip_ru, @telecom_ru, @ebpf_ru, @prometheus_ru). Неделю пообщаться, помочь людям, затем аккуратно упомянуть проект. | 1 неделя |
717: | **Mailing lists** | Низкий. Подписался — пишешь. | Подписаться на sr-users@lists.kamailio.org и аналогичные. Понять tone и format (технический, без маркетинга). | Несколько дней |
718: 
719: #### 2.2 План подготовки аккаунтов
720: 
721: **Неделя 3:**
722: 
723: | День | Действие |
724: |------|----------|
725: | Пн | Зарегистрировать аккаунты: Hacker News, Reddit, Dev.to, Medium, Twitter/X |
726: | Вт | Reddit: подписаться на r/voip, r/selfhosted, r/devops, r/kubernetes, r/PrometheusMonitoring. Ответить на 5-10 вопросов. |
727: | Ср | Twitter/X: follow 50-100 людей (VoIP, eBPF, Prometheus, Go). Ответить на 5-10 тредов содержательно. |
728: | Чт | LinkedIn: подключиться к 50 людям (Kamailio, OpenSIPS, Asterisk, Prometheus, eBPF — поиск по ключевым словам). |
729: | Пт | Dev.to: написать первую статью (не про sip-exporter, а образовательную — «Understanding RFC 6076 SIP Metrics»). Это прогрев + karma. |
730: | Сб | Вступить в Telegram группы (@voip_dev, @sip_ru, @prometheus_ru, @ebpf_ru, @k8s_ru). Начать общаться. |
731: | Вс | Подписаться на mailing lists: Kamailio (sr-users), OpenSIPS, FreeSWITCH. Прочитать последние 20 писем для понимания тона. |
732: 
733: **Неделя 4:**
734: 
735: | День | Действие |
736: |------|----------|
737: | Пн-Ср | Reddit: продолжать наращивать karma. Писать полезные комментарии (10-20 в день). Цель: ~100 karma. |
738: | Чт | Habr: написать статью в песочницу («Мониторинг SIP в 2026: что доступно open-source»). |
739: | Пт | Twitter/X: опубликовать образовательный тред (не про свой проект, а про RFC 6076 или eBPF). |
740: | Сб | LinkedIn: опубликовать insight post (не ссылку, а мнение/наблюдение про VoIP мониторинг). |
741: | Вс | Проверить все аккаунты: karma/followers/connections готовы? Если нет — ещё неделя прогрева. |
742: 
743: #### 2.3 Критерии готовности к launch
744: 
745: Не переходить к Фазе 3 пока:
746: 
747: - [ ] Reddit аккаунт: karma ≥ 50, возраст ≥ 2 недели
748: - [ ] Twitter/X: ≥ 100 followers, 5-10 опубликованных тредов
749: - [ ] LinkedIn: ≥ 50 connections в VoIP/DevOps/SRE
750: - [ ] Dev.to: ≥ 1 опубликованная статья (не про sip-exporter)
751: - [ ] Habr: карма ≥ 5 (или статья прошла модерацию из песочницы)
752: - [ ] Telegram: активно общаетесь в 3+ группах минимум неделю
753: 
754: ---
755: 
756: ### Фаза 3: Launch (Недели 5-6)
757: 
758: Цель: максимальный охват за 48 часов. Постить только когда аккаунты прогреты.
759: 
760: #### 3.1 Порядок публикации (важно!)
761: 
762: Запуск не одновременный, а каскадный — каждая площадка имеет свой «пиковый час»:
763: 
764: | День | Время UTC | Площадка | Контент | Почему этот порядок |
765: |------|-----------|----------|---------|---------------------|
766: | День 1, утро | 08:00-10:00 | **Dev.to** | Полная статья (2000+ слов): «sip-exporter: eBPF-based SIP monitoring for Prometheus» | Dev.to толерантен к self-promo, статья индексируется Google, будет ссылкой для остальных постов |
767: | День 1, утро | 10:00-12:00 | **Hacker News** | «Show HN: sip-exporter — eBPF-based SIP monitoring with Prometheus (github.com/aibudaevv/sip-exporter)» | Show HN — формат для своих проектов. Ссылка на Dev.to или GitHub. Пиковый трафик HN — US утро. |
768: | День 1, день | 12:00-14:00 | **Reddit r/selfhosted** | Пост с описанием + docker-compose пример | r/selfhosted самый толерантный к self-promo. Обязательно с практическим примером. |
769: | День 1, день | 14:00-16:00 | **Reddit r/voip** | Пост: «Open-source passive SIP monitoring with RFC 6076 metrics» | Прямая аудитория. Акцент на техническую глубину, не маркетинг. |
770: | День 1, вечер | 16:00-18:00 | **Twitter/X** | Thread (10-15 твитов): «I built a SIP monitoring tool using eBPF...» | Ссылки на GitHub + Dev.to. Хештеги: #eBPF #Prometheus #VoIP #SIP #Golang |
771: | День 2, утро | 08:00-10:00 | **LinkedIn** | Статья/Post: профессиональный рассказ про eBPF + VoIP | LinkedIn algorithm любит «insight» формат — не «look at my project», а «here's what I learned building...» |
772: | День 2, день | 10:00-12:00 | **Reddit r/devops** + **r/PrometheusMonitoring** | Cross-post с адаптацией под каждую аудиторию | r/devops: акцент на Prometheus + eBPF. r/PrometheusMonitoring: акцент на RFC 6076 metrics |
773: | День 2, вечер | 16:00-18:00 | **Medium** | Cross-post Dev.to статьи (canonical URL на Dev.to) | SEO: Medium хорошо индексируется |
774: | День 3 | В течение дня | **Telegram группы** | Написать в @voip_dev, @sip_ru, @telecom_ru — но от первого лица, как «смотрите что сделал, буду рад фидбеку» | Не как реклама, а как «коллеги, сделал инструмент для нашей сферы» |
775: | День 3-4 | В течение дня | **Mailing lists** | Kamailio sr-users, OpenSIPS users — email с описанием проблемы и решения | Технический tone, без маркетинга. «I needed X, couldn't find it, built Y.» |
776: 
777: #### 3.2 Шаблоны контента
778: 
779: **Hacker News (Show HN) — формат:**
780: 
781: ```
782: Show HN: sip-exporter – Passive SIP monitoring with eBPF for Prometheus
783: 
784: I built this because I couldn't find a lightweight SIP traffic monitor 
785: that works with Prometheus without running a full HOMER stack or needing 
786: direct access to Kamailio internals.
787: 
788: It uses eBPF socket filters to capture SIP packets at kernel level — 
789: zero packet loss at 2,000 CPS (~24K PPS), <15% CPU, ~15MB RAM.
790: 
791: Implements full RFC 6076 metrics: SER, SEER, ISA, SCR, RRD, SPD + 
792: extended metrics (TTR, NER, ORD, LRD, ASR). Single Docker container, 
793: no dependencies.
794: 
795: 55 E2E tests with real SIP traffic (SIPp + testcontainers-go).
796: 
797: Would love feedback from anyone running SIP infrastructure.
798: ```
799: 
800: **Reddit — важно: не копировать HN текст. Reddit ненавидит копипасту.**
801: 
802: r/selfhosted (практический акцент):
803: ```
804: Заголовок: I made a SIP monitoring tool that runs in Docker and 
805: outputs Prometheus metrics
806: 
807: Текст: [docker-compose пример] [скриншот Grafana] [2-3 предложения 
808: о проблеме/решении] [ссылка на GitHub]
809: ```
810: 
811: r/voip (технический акцент):
812: ```
813: Заголовок: Open-source passive SIP traffic monitor with RFC 6076 
814: metrics (SER, SEER, ISA, SCR)
815: 
816: Текст: [описание RFC 6076 реализации] [какие метрики] 
817: [архитектура: eBPF → AF_PACKET → Go] [ссылка]
818: ```
819: 
820: **Twitter/X thread:**
821: 
822: ```
823: 1/ I built a SIP monitoring tool using eBPF 🧵
824: 
825: Problem: VoIP operators need SER, SEER, ISA metrics per RFC 6076. 
826: Existing options = full HOMER stack (heavy) or kamailio_exporter 
827: (Kamailio-only).
828: 
829: 2/ Solution: passive packet capture at kernel level. 
830: No agent on the SIP server. No module changes.
831: Just point at a network interface.
832: 
833: 3/ Architecture:
834: SIP Traffic → NIC → eBPF socket filter → ringbuf → Go → Prometheus
835: 
836: Zero-copy kernel→userspace. No tcpdump overhead.
837: 
838: 4/ Numbers from load testing:
839: ✅ 2,000 CPS (24K PPS) — zero packet loss
840: ✅ <15% CPU, ~15MB RAM  
841: ✅ GC pauses <1ms (400x smaller than socket buffer)
842: 
843: 5/ Full RFC 6076 implementation:
844: SER (Session Establishment Ratio)
845: SEER (Session Establishment Effectiveness Ratio)
846: ISA (Ineffective Session Attempts)
847: SCR (Session Completion Ratio)
848: RRD, SPD, TTR — histograms with percentiles
849: 
850: 6/ 55 end-to-end tests with real SIP traffic via SIPp.
851: 8 load tests benchmarking PPS, memory, GC pauses.
852: 
853: Open source, AGPL-3.0:
854: github.com/aibudaevv/sip-exporter
855: ```
856: 
857: #### 3.3 Что делать ПОСЛЕ launch
858: 
859: Launch — это 20% работы. 80% — это то, что происходит после:
860: 
861: - [ ] **Отвечать на каждый комментарий** на HN/Reddit в первые 24-48 часов. Это определяет ранг поста.
862: - [ ] **HN:** быть готовым к вопросам про eBPF verifer, AF_PACKET vs XDP, why Go not Rust. Техническая глубина = уважение.
863: - [ ] **Reddit:** следить за upvote/downvote ratio. Если пост уходит в минус — проанализировать почему (обычно: слишком промо, мало практической ценности).
864: - [ ] **Twitter/X:** отвечать на каждый reply и retweet. Это повышает видимость в алгоритме.
865: - [ ] **Collect feedback:** записывать все вопросы и замечания — это input для будущих статей и фич.
866: 
867: ---
868: 
869: ### Фаза 4: Контент-маркетинг (Месяцы 2-3)
870: 
871: Цель: SEO-позиционирование + репутация эксперта. Каждый материал = постоянный источник трафика.
872: 
873: #### 4.1 Технические статьи (на английском)
874: 
875: | Приоритет | Статья | Площадка | Хук | Барьер |
876: |-----------|--------|----------|-----|--------|
877: | P0 | «eBPF for SIP Monitoring: Zero-Overhead Packet Capture» | Dev.to → cross-post Medium | Уникальный eBPF use case | Нет — Dev.to открыт |
878: | P0 | «RFC 6076 SIP Performance Metrics in Prometheus» | Dev.to → cross-post Medium | Образовательный + SEO | Нет |
879: | P1 | «SIP Monitoring: HOMER vs sip-exporter vs kamailio_exporter» | Dev.to | Comparison SEO-трафик | Нет |
880: | P1 | «Monitoring Kamailio with Prometheus — No Module Changes Needed» | Kamailio wiki / blog | Прямая аудитория | Нужен контакт с Kamailio community |
881: | P2 | «Building a Grafana Dashboard for VoIP Quality» | Grafana community blog | Grafana ecosystem | Guest post — нужен pitch |
882: | P2 | «Benchmarking: 2000 CPS SIP Processing in Go» | Dev.to | Performance angle | Нет |
883: | P3 | «eBPF Socket Filters: Practical Guide for Network Monitoring» | Dev.to + Lobste.rs | Техническая глубина | Lobste.rs нужен инвайт |
884: 
885: #### 4.2 Контент на русском
886: 
887: | Приоритет | Статья | Площадка | Барьер |
888: |-----------|--------|----------|--------|
889: | P0 | «Мониторинг SIP-трафика на eBPF: SER, SEER, SCR для Grafana» | Habr (Сетевые технологии) | Карма ≥ 5 или песочница |
890: | P1 | «VoIP-мониторинг в Kubernetes: Prometheus + eBPF» | Habr (DevOps) | Карма ≥ 5 |
891: | P2 | «RFC 6076: метрики SIP-качества — от формул до Grafana» | Habr (Телефония) | Карма ≥ 5 |
892: 
893: #### 4.3 Визуальный контент
894: 
895: - [ ] **YouTube видео** (10-15 мин): «Deploy sip-exporter + Grafana in 5 minutes» — screen recording полного цикла
896: - [ ] **YouTube shorts / TikTok** (30-60 сек): «SIP monitoring with eBPF — zero packet loss at 2000 CPS» — Grafana dashboard в действии
897: - [ ] **Архитектурная схема** (SVG/Mermaid) для README и статей
898: 
899: #### 4.4 Stack Overflow стратегия
900: 
901: Не спамить ссылкой на проект. Вместо этого:
902: - Искать вопросы: «sip monitoring prometheus», «sip traffic grafana», «kamailio prometheus metrics», «voip monitoring tools»
903: - Давать развёрнутый ответ (5-10 предложений)
904: - В конце: «For a comprehensive solution, you might also look at sip-exporter which captures SIP traffic via eBPF...»
905: - Цель: ~10 качественных ответов за 2 месяца. Каждый = постоянный трафик.
906: 
907: ---
908: 
909: ### Фаза 5: Экосистемная интеграция (Месяцы 3-4)
910: 
911: Цель: попасть в «официальные» списки и каталоги. Это PR, не постинг — правила другие.
912: 
913: #### 5.1 Prometheus Ecosystem
914: 
915: - [ ] **awesome-prometheus-exporters** — PR в github.com/prometheus-community/awesome-prometheus-exporters. Формат: одна строка в markdown. Нужен meaningful description. Барьер: нужен аппрув мейнтейнера.
916: - [ ] **Prometheus.io exporters page** — PR в github.com/prometheus/docs. Добавить в таблицу exporters. Барьер: нужно соответствовать критериям (open-source, работает, документация).
917: - [ ] **Grafana dashboards marketplace** — опубликовать на grafana.com/grafana/dashboards/. Нужен аккаунт Grafana Cloud (бесплатный). Загрузить JSON через форму.
918: - [ ] **Artifact Hub** — опубликовать Helm chart (когда будет готов). Формат: стандартный Helm chart + annotation.
919: 
920: #### 5.2 VoIP Ecosystem
921: 
922: - [ ] **voip-info.org** — wiki-style сайт. Любой может создать страницу. Написать статью с примерами docker-compose и PromQL.
923: - [ ] **Kamailio wiki cookbook** — предложить рецепт через mailing list. Барьер: нужно быть частью community.
924: - [ ] **Asterisk wiki** — аналогично
925: - [ ] **FreeSWITCH wiki** — аналогично
926: 
927: #### 5.3 eBPF Ecosystem
928: 
929: - [ ] **ebpf.io/applications** — PR в github.com/ebpf-io/ebpf.io. Добавить в список applications.
930: - [ ] **Cilium / eBPF Slack** — зарегистрироваться на slack.cilium.io, написать в #general или #showcase
931: - [ ] **ebpf-forum** — опубликовать use case
932: 
933: #### 5.4 Package Managers
934: 
935: - [ ] **GoReleaser** — автоматические GitHub Releases с бинарниками для linux/amd64, linux/arm64

(Output capped at 50 KB. Showing lines 1-935. Use offset=936 to continue.)
</content>
```

---

## Assistant (Plan · GLM-5.1 · 23.2s)

_Thinking:_

Now I have a comprehensive view of the project. Let me summarize the prioritized features for the user, since they want a sorted priority list and prefer large features.

Based on the ISSUES.md roadmap:
- v0.11: Per-Carrier Labels + Self-Monitoring
- v0.12: SIP User-Agent Classification
- v0.13: TCP Support (TLS offload)
- v0.14: RTCP Quality Metrics + MOS
- v0.15: CDR Export
- v0.16: OpenTelemetry + Helm Chart
- v0.17: Hot-reload + IPv6 + Multi-interface
- v1.0: Retransmission Detection + GeoIP + HOMER/HEP

The user said they want a "large feature" and asked me to sort by priority. Let me present the sorted priority list with my reasoning.

Вот приоритизированный список крупных фич с обоснованием:

---

## Приоритизированный список (крупные фичи)

### 1. Per-IP / Per-Carrier метрики (v0.11) — **Рекомендую**

**Почему первая:** главный дифференциатор. Ни один open-source SIP exporter не даёт метрики по транкам. Enterprise-оператор не купится на глобальные счётчики — ему нужно «какой carrier деградирует?».

**Что включает:**
- YAML конфиг с CIDR-группами (`carriers:`)
- Извлечение src/dst IP из L3-парсера (уже есть)
- Переработка `Metricser` — добавить `carrier` label
- Обновление Grafana dashboard

**Сложность:** средняя. eBPF-код **не трогаем** — IP уже парсится в Go на L3.

---

### 2. CDR Export (v0.15) — **Пропустил бы вперёд в роадмапе**

**Почему вторая:** CDR = биллинг + отчётность = то, за что платят. Данные для генерации CDR у нас уже есть (Call-ID, From/To, timestamps, duration). Асинхронная запись через buffered channel.

**Сложность:** средняя. Не трогает eBPF.

---

### 3. RTCP Quality Metrics + MOS (v0.14)

**Почему третья:** уникальный дифференциатор — никто в OSS не делает eBPF-based RTP/RTCP мониторинг. Но высокая сложность: новый eBPF фильтр, SDP-парсинг, E-Model реализация.

**Сложность:** высокая.

---

### 4. TCP/TLS поддержка (v0.13)

**Почему четвёртая:** открывает 40-60% production-сценариев, но TCP stream reassembly — нетривиальная задача. Логичнее сделать после Per-Carrier.

**Сложность:** высокая.

---

### 5. OpenTelemetry + Helm Chart (v0.16)

**Сложность:** средняя. OTLP экспорт + Kubernetes Helm chart.

---

**Моя рекомендация:** начать с **Per-Carrier Labels** — это наибольший product-impact при средней сложности, и не требует изменений eBPF. Какую берём?

---

## User

формирование CDR исключи, это функционал другого ПО. Мы мониторинг

---

## Assistant (Plan · GLM-5.1 · 15.7s)

_Thinking:_

The user wants to exclude CDR Export from the roadmap since this is monitoring software, not billing software. They want to focus on monitoring features. Let me update the prioritized list accordingly.

Since we're in plan mode, I should only analyze and plan, not make any changes.

Понял, CDR — не наш скоуп. Обновлённый приоритет:

## Крупные фичи для мониторинга (без CDR)

| # | Фича | Impact | Сложность | Почему |
|---|------|--------|-----------|--------|
| **1** | **Per-IP / Per-Carrier Labels** | Критичный | Средняя | Главный дифференциатор. eBPF не трогаем. |
| **2** | **RTCP Quality Metrics + MOS** | Высокий | Высокая | Уникально для OSS. Но нужен новый eBPF фильтр. |
| **3** | **TCP/TLS поддержка** | Высокий | Высокая | 40-60% production-сценариев. TCP reassembly сложен. |
| **4** | **OpenTelemetry + Helm Chart** | Средний | Средняя | Расширение рынка интеграции. |
| **5** | **SIP User-Agent Classification** | Средний | Средняя | Troubleshooting по типам устройств. |
| **6** | **Hot-reload + IPv6 + Multi-interface** | Низкий | Средний | Production completeness. |
| **7** | **Retransmission Detection + GeoIP + HOMER/HEP** | Низкий | Средний | Полнота для GA. |

**Рекомендация:** начинаем с **Per-Carrier Labels** (v0.11) — максимальный impact при средней сложности. Берём её?

---

## User

Да. Проанализируй как устроено у конкурентов. Как это будет использоваться. Как использовать это удобно. Примеры использования.

---

## Assistant (Plan · GLM-5.1 · 172.6s)

_Thinking:_

The user wants me to analyze how per-carrier/per-IP metrics work in competitor products, how they'd be used in practice, and what would be the most convenient UX. Let me research competitors and think through the design.

Let me look at how HOMER, kamailio_exporter, and other monitoring tools handle per-carrier/per-IP labeling.

Давай исследую конкурентов и продумаю UX. Запущу параллельный анализ.

**Tool: task**

**Input:**
```json
{
  "description": "Research HOMER per-carrier",
  "prompt": "Research how HOMER (sipcapture/homer) handles per-IP, per-carrier, multi-tenant SIP monitoring. Look at:\n1. How does HOMER partition metrics by carrier/trunk/IP?\n2. What labels/dimensions does it use?\n3. How is configuration done (IP groups, aliases)?\n4. What does the UI/API look like for filtering by carrier?\n5. Check their GitHub repo at https://github.com/sipcapture/homer and documentation\n\nReturn a detailed analysis of their approach to per-carrier/per-IP monitoring.",
  "subagent_type": "general"
}
```

**Output:**
```
task_id: ses_257078f12ffePlrQfYoJo5YXFY (for resuming to continue this task if needed)

<task_result>
Вот детальный анализ подхода HOMER к мониторингу per-IP/per-carrier:

---

## Архитектура HOMER: Per-IP/Per-Carrier мониторинг

### 1. Разделение метрик по carrier/trunk/IP

HOMER использует модель **IP-to-Name mapping** через конфигурацию `heplify-server`. Это простая, но эффективная схема:

**Конфигурация** (в `heplify-server.toml`):
```toml
PromTargetIP   = "10.1.2.111,10.1.2.4,10.1.2.5,10.1.2.6,10.12.44.222"
PromTargetName = "sbc_access,sbc_core,kamailio,asterisk,pstn_gateway"
```

При получении HEP-пакета сервер сравнивает SrcIP/DstIP с `TargetMap` (map[string]string). Если IP найден:
- SrcIP совпадает → метрика получает label `target_name=<name>`, `direction="src"`
- DstIP совпадает → метрика получает label `target_name=<name>`, `direction="dst"`
- Ни один не совпадает → `target_name="unknown"`

**Горячая перезагрузка**: `killall -HUP heplify-server` — перечитывает `PromTargetIP`/`PromTargetName` без рестарта (`metric/reload.go`).

### 2. Labels/Dimensions (Prometheus метрики)

Ключевая метрика SIP — `heplify_method_response`:

| Label | Описание | Пример |
|-------|----------|--------|
| `target_name` | Имя таргета из PromTargetName | `sbc_access`, `pstn_gateway` |
| `direction` | Направление относительно таргета | `src`, `dst` |
| `node_id` | Имя/ID HEP capture node (NodeName/NodeID) | `100`, `sbc1` |
| `response` | SIP метод/status первой строки | `INVITE`, `200`, `180`, `480` |
| `method` | CSeq метод | `INVITE`, `BYE`, `REGISTER` |

Другие метрики и их labels:

- **`heplify_kpi_srd`** (Session Request Delay): `target_name`, `node_id`
- **`heplify_kpi_rrd`** (Registration Request Delay): `target_name`, `node_id`
- **`heplify_reason_isup_total`** (Q.850 cause codes): `target_name`, `cause`, `method`
- **RTCP метрики**: `target_name`, `direction`, `node_id`
- **horaclifix метрики** (RTP stats): `sbc_name`, `direction`, `inc_realm`, `out_realm`

### 3. Конфигурация (IP Groups, Aliases)

**HEP-протокол** обеспечивает транспортный слой идентификации:
- **NodeID** (HEP chunk 0x000c) — числовой ID capture agent'а
- **NodeName** (HEP chunk 0x0013) — строковое имя узла
- **SrcIP/DstIP/SrcPort/DstPort** — извлекаются из HEP envelope

**Способы конфигурации**:
1. **TOML-файл** (`heplify-server.toml`) — основной метод
2. **CLI флаги** / **env vars**
3. **Web-редактор** (`ConfigHTTPAddr = "0.0.0.0:9876"`) — UI для редактирования конфигурации

**Multi-Node**: homer-app поддерживает несколько БД-узлов (data nodes) для масштабирования:
```json
"database_data": {
  "LocalNode": { "host": "127.0.0.1" },
  "RemoteNode": { "host": "127.0.0.2" }
}
```

**Нет формальной модели «tenants» или «IP groups»** — HOMER не имеет встроенной multi-tenancy в открытом коде. Вместо этого:
- Разделение на уровне IP → label mapping
- Разделение на уровне Grafana dashboards (фильтрация по `target_name`)
- Для полноценной multi-tenancy используется коммерческая версия или ручная настройка Prometheus + Grafana с tenant labels

### 4. UI/API для фильтрации

**HOMER 7** (homer-app + homer-ui):
- Встроенный UI с поиском по SIP-пакетам
- Фильтрация по: `node`, `from/to IP`, `call-id`, `method`, `status code`
- API: RESTful (`/api/v1/...`) с Swagger документацией
- Prometheus endpoint: `heplify-server:9096/metrics`

**HOMER 10** (новый подход):
- «Headless observability» — без собственного UI
- Использует **qryn** (PolyLog/Loki-compatible) + **Grafana**
- Пакеты трансформируются в стандартные logs/metrics/traces
- Фильтрация полностью через Grafana (LogQL/PromQL)
- Нативная интеграция с observability stack

### 5. Сравнение подходов

| Аспект | HOMER/heplify-server | sip-exporter (наш) |
|--------|---------------------|---------------------|
| Модель | IP → Name mapping (config) | eBPF capture, нет labels |
| Labels | `target_name`, `direction`, `node_id` | Нет (все метрики глобальные) |
| Tenant | Нет встроенной модели | Нет |
| Транспорт | HEP протокол (агенты на каждом узле) | AF_PACKET (passive capture) |
| Масштабирование | Много capture nodes | Один интерфейс |
| Конфигурация | TOML файл + hot reload | Env vars |
| KPI | SRD, RRD, Q.850 cause, RTCP/XR | SER, SEER, ISA, SCR, RRD, SPD, TTR |

### Ключевые выводы для sip-exporter

HOMER'овский подход к per-IP мониторингу — это **статический IP→Name mapping на уровне capture server**, а не на уровне меток в пакете. Это простая модель, которая:

1. **Не требует модификации capture pipeline** — mapping происходит при экспорте в Prometheus
2. **Позволяет группировать** несколько IP под одним логическим именем (carrier/trunk)
3. **Direction-aware** — знает, является ли IP источником или получателем
4. **Hot-reloadable** — можно добавлять/менять mapping без рестарта

Для sip-exporter этот паттерн был бы наиболее практичным: добавить конфигурацию `SIP_EXPORTER_TARGET_MAP` с IP→label mapping и прикреплять `target` label к метрикам при парсинге.
</task_result>
```

**Tool: task**

**Input:**
```json
{
  "description": "Research kamailio_exporter labels",
  "prompt": "Research how kamailio_exporter and similar Prometheus SIP/VoIP exporters handle per-IP or per-carrier labeling. Search for:\n1. kamailio_exporter on GitHub - what labels does it expose?\n2. Any Prometheus VoIP/SIP exporters that have carrier/trunk/IP labels\n3. How do telekom NOC teams typically label SIP metrics in Prometheus (search for examples, blog posts, Grafana dashboards)\n4. Best practices for multi-tenant/multi-carrier Prometheus exporters\n\nReturn a detailed analysis of competitor approaches and industry best practices for labeling SIP metrics.",
  "subagent_type": "general"
}
```

**Output:**
```
task_id: ses_257077b75ffeXtrdpYl3gypMM3 (for resuming to continue this task if needed)

<task_result>
Вот детальный анализ конкурентов и практик маркировки SIP-метрик:

---

## 1. kamailio_exporter — три реализации

### florentchauveau/kamailio_exporter (Callr, 62 stars)
Самый популярный. Использует BINRPC. **Никаких per-IP/carrier лейблов**. Все метрики агрегатные:

```
kamailio_tm_stats_codes_total{code="2xx"} 2214
kamailio_sl_stats_codes_total{code="200"} 1089737
kamailio_core_rcv_request_total{method="invite"} 0
kamailio_dispatcher_list_target{destination="sip:172.16.105.138:5070"} 1
```

Единственный «идентифицирующий» лейбл — `code` (статус) и `method` (SIP метод). Dispatcher имеет `destination` (URI SIP-таргета), но **не имя carrier/trunk**.

### voiplens/kamailio_exporter (форк pascomnet, самый функциональный)
**Единственный, кто решает задачу per-carrier маркировки.** Ключевые лейблы:

| Метрика | Лейблы | Источник |
|---|---|---|
| `kamailio_dispatcher_list_target` | `destination`, `set_id`, **`set_name`** | `dispatcher.list` + CLI маппинг |
| `kamailio_dispatcher_list_target_latency_avg` | `destination`, `set_id`, `set_name` | latency probing |
| `kamailio_dlg_profile_get_size_dialog` | **`profile`** | dialog profiles |
| `kamailio_rtpengine_enabled` | `url`, `set`, `index`, `weight` | rtpengine.show |
| `kamailio_htable_items_total` | **`name`** | htable.listTables |

**Ключевой механизм** — CLI-флаг для маппинга dispatcher set ID → имя:
```bash
kamailio_exporter --collector.dispatcher.mapping="200:Carrier 1" --collector.dispatcher.mapping="400:Carrier 2"
```

Результат:
```
kamailio_dispatcher_list_target{destination="sip:10.0.0.1:5060",set_id="200",set_name="Carrier 1"} 1
```

Dialog profiles для per-carrier сессий:
```bash
kamailio_exporter --collector.dialog.profiles="PROVIDER_A_IN" --collector.dialog.profiles="PROVIDER_A_OUT"
```
```
kamailio_dlg_profile_get_size_dialog{profile="PROVIDER_A_IN"} 42
```

---

## 2. Kamailio xhttp_prom (нативный модуль)

Это встроенный механизм Kamailio для создания **произвольных метрик с лейблами** — до 3 лейблов на метрику. Пример конфигурации:

```kamailio
modparam("xhttp_prom", "prom_counter", "name=sip_invite_total; label=method:carrier; help='Total INVITEs per carrier'")
```

В routing-логике Kamailio:
```kamailio
prom_counter_inc("sip_invite_total", "1", "INVITE", "$var(carrier_name)");
```

Результат:
```
kamailio_sip_invite_total{method="INVITE", carrier="provider_a"} 150
```

**Именно так NOC-команды делают per-carrier метрики**: Kamailio script сам определяет carrier (по source IP, R-URI, или dispatch logic) и инкрементит labeled counter. pascomnet и voiplens kamailio_exporter могут забирать эти метрики через `--customKamailioMetricsURL`.

---

## 3. FreeSWITCH exporter (Callr)

Предельно простой — 15 метрик, **никаких лейблов для идентификации carrier/IP**. Только gauge/counter:
```
freeswitch_current_calls 42
freeswitch_sessions_total 150
freeswitch_time_synced 1
```

---

## 4. Отсутствующие на рынке SIP-экспортеры

На GitHub **нет** open-source SIP/VoIP экспортеров, которые:
- Декодируют SIP-пакеты на сетевом уровне (как sip-exporter через eBPF)
- Автоматически извлекают source/destination IP и создают per-IP лейблы
- Предоставляют per-carrier labeling без участия Kamailio/OpenSIPS routing layer

Все существующие решения — это **RPC-клиенты к SIP-серверам**, которые забирают агрегатные счётчики. Multitenancy делается на уровне Kamailio script (xhttp_prom + dialog profiles).

---

## 5. Best practices для multi-tenant/multi-carrier SIP метрик

### Prometheus naming (официальные guidelines)
- Лейблы — для различения характеристик измеряемого объекта
- **Не более 10 уникальных значений лейбла** на метрику в идеале (cardinality budget)
- `source_ip` с тысячами уников = cardinality explosion → нужна агрегация
- Не использовать `type` как имя лейбла (слишком generic)
- Target-лейблы (`region`, `env`) — через Prometheus scrape config, не через экспортер

### Telekom/VoIP NOC практика (на основе xhttp_prom и voiplens)

**Подход 1: Carrier через Dispatcher Set (voiplens)**
```
sip_dispatcher_status{carrier="T-Mobile", destination="sip:10.0.0.1:5060"} 1
sip_dispatcher_latency_ms{carrier="T-Mobile"} 12.5
```
Плюсы: автоматически из Kamailio dispatcher. Минусы: только для outbound.

**Подход 2: Carrier через Dialog Profile**
```
sip_active_sessions{carrier="T-Mobile", direction="inbound"} 42
sip_active_sessions{carrier="Vodafone", direction="outbound"} 18
```
Плюсы: per-carrier active calls. Минусы: нужна Kamailio конфигурация.

**Подход 3: xhttp_prom custom counters (наиболее гибкий)**
```
kamailio_sip_response_total{method="INVITE", status="200", carrier="T-Mobile"} 150
kamailio_sip_invite_duration_seconds{carrier="T-Mobile"} 0.5
```

**Подход 4: IP-to-carrier mapping через конфигурационный файл (для пассивного мониторинга)**
```
sip_invite_total{carrier="T-Mobile", source_ip="10.0.0.1"} 100
sip_invite_total{carrier="Vodafone", source_ip="10.0.0.2"} 50
```
Это то, что **sip-exporter мог бы сделать уникально** — mapping source/dest IP → carrier name через конфиг, без участия SIP-сервера.

### Cardinality management

Критически важные правила:
- **Никогда не использовать raw IP как лейбл** без mapping — в телекоме тысячи IP
- Использовать **именованные группы** (carrier/trunk name) с конфигурационным mapping
- Если нужен per-IP drill-down — использовать **Grafana+Loki для логов**, а не Prometheus метрики
- Типичный бюджет: 10-50 carrier'ов × 6 methods × ~10 status codes = ~3000 series — приемлемо
- Per-IP без маппинга: 10,000 IP × 6 methods × 10 status codes = **600,000 series** — неприемлемо

---

## 6. Вывод: уникальное позиционирование sip-exporter

| Возможность | kamailio_exporter | xhttp_prom | **sip-exporter (eBPF)** |
|---|---|---|---|
| Per-carrier labels | Только dispatcher set_name | Произвольные (ручные) | **Может автоматически по IP mapping** |
| Пассивный мониторинг | Нет (нужен Kamailio) | Нет (нужен Kamailio) | **Да (eBPF, независим от SIP server)** |
| Per-IP drill-down | Нет | Нет | **Потенциально да** |
| Source IP → carrier mapping | Нет | Ручной script | **Может через config file** |
| RFC 6076 метрики (SER, SCR) | Нет | Нет | **Да** |

**Уникальная ниша sip-exporter**: per-carrier labeling через конфигурируемый IP-to-carrier mapping **без зависимости от SIP-сервера**, с пассивным мониторингом на уровне сети. Ни один существующий экспортер этого не делает.

### Рекомендуемый набор лейблов для sip-exporter:
```
sip_request_total{method="INVITE", carrier="T-Mobile", direction="inbound"}
sip_response_total{method="INVITE", status="200", carrier="T-Mobile"}
sip_sessions{carrier="T-Mobile", state="active"}
sip_ser{carrier="T-Mobile"} 95.5
sip_rrd_seconds{carrier="T-Mobile"} 0.045
```

Где `direction` = inbound/outbound (определяется по порту назначения), `carrier` = mapping из конфига (source/dest IP → carrier name). Если IP не в маппинге — лейбл `carrier="unknown"` или метрика без лейбла (для контроля cardinality).
</task_result>
```

**Tool: task**

**Input:**
```json
{
  "description": "Research Prometheus labeling UX",
  "prompt": "Research best practices for Prometheus exporter labeling that involves IP-to-group mapping. Specifically:\n\n1. Search for examples of Prometheus exporters that map IP addresses/CIDRs to labels (e.g., blackbox_exporter, snmp_exporter, any network monitoring exporter)\n2. How do exporters like node_exporter, cAdvisor, or kube-state-metrics handle multi-dimensional labeling?\n3. What are the Prometheus best practices for label cardinality? (search prometheus.io docs)\n4. How do operators typically use PromQL with carrier/trunk labels? Show example queries.\n5. What's the standard approach: labels on every metric vs separate \"info\" metric with grouping?\n6. Search for \"Prometheus relabeling\" approaches for IP-to-carrier mapping as an alternative to doing it in the exporter itself.\n\nReturn a comprehensive analysis with concrete PromQL examples and recommended patterns.",
  "subagent_type": "general"
}
```

**Output:**
```
task_id: ses_2570758a6ffe1IQWYYfxdhI0mR (for resuming to continue this task if needed)

<task_result>
# Prometheus Exporter Labeling: IP-to-Group Mapping Research

## 1. Exporters That Map IPs to Labels

**blackbox_exporter** is the canonical example. It uses the **multi-target exporter pattern** where:
- Targets are defined in Prometheus `scrape_configs`, not in the exporter itself
- Prometheus passes `__param_target` to the exporter via relabeling
- The `instance` label is set to the actual target, not the exporter's address

```yaml
relabel_configs:
  - source_labels: [__address__]
    target_label: __param_target
  - source_labels: [__param_target]
    target_label: instance
  - target_label: __address__
    replacement: localhost:9115
```

**snmp_exporter** uses the same pattern — targets with IPs are mapped in Prometheus config, and the exporter just queries the given target.

**Key takeaway**: Neither exporter does internal IP→label mapping. The mapping is done in **Prometheus relabel_configs** or **service discovery**.

## 2. Multi-Dimensional Labeling in Popular Exporters

| Exporter | Pattern | Labels per Metric |
|----------|---------|-------------------|
| **node_exporter** | Single-instance, minimal labels | `device`, `fstype`, `mountpoint` (low cardinality) |
| **cAdvisor** | Per-container metrics | `container`, `image`, `name` (bounded by container count) |
| **kube-state-metrics** | Uses `*_info` pattern | `kube_pod_info{uid, node, created_by_kind}` — metadata separated from counters |

**kube-state-metrics** is the gold standard:
```promql
# Info metric pattern — metadata stored separately
kube_pod_info{uid="abc123", node="node1", created_by_kind="Deployment"}

# Joining info to counters in PromQL
kube_pod_status_phase * on (namespace, pod) group_left(node) kube_pod_info
```

## 3. Label Cardinality Best Practices (from prometheus.io)

From the official instrumentation docs:

> **Keep cardinality below 10** for most metrics. A **handful of metrics** can exceed that. Cardinality **over 100** needs alternate solutions.

**Concrete guidelines:**

| Cardinality | Verdict | Example |
|-------------|---------|---------|
| < 10 | Fine | `method="INVITE\|REGISTER\|BYE"` (7 values) |
| 10–100 | Acceptable if necessary | `status_code="100\|180\|200\|...40x..."` (~30 values) |
| 100–1000 | Danger zone | IP addresses in a small network |
| > 1000 | **Avoid** — move to alternate approach | Individual IP addresses, Call-IDs |

**For SIP carrier/trunk labels:**
- If you have <50 carriers and <200 trunks → labels are fine
- If IPs are unbounded → use a separate `sip_source_info` info metric (1 series per IP) or don't label by IP at all, label by carrier group only

## 4. PromQL with Carrier/Trunk Labels — Example Queries

### Scenario: Labels `carrier`, `trunk_group`, `source_ip` on metrics

```promql
# SER per carrier
sip_ser{carrier="carrier_A"} * 100

# INVITE rate per trunk group
sum by (trunk_group) (rate(sip_invite_total[5m]))

# Top 5 carriers by failed INVITEs
topk(5, sum by (carrier) (rate(sip_invite_failed_total[5m])))

# Compare carrier response time (P99)
histogram_quantile(0.99, 
  sum by (carrier, le) (rate(sip_ttr_duration_seconds_bucket[5m]))
)

# Cross-carrier SCR comparison
sip_scr * on (instance) group_left(carrier) sip_carrier_info

# INVITE flood detection per source IP
sum by (source_ip) (rate(sip_invite_total[1m])) > 1000

# Session duration by carrier
histogram_quantile(0.95,
  sum by (carrier, le) (rate(sip_spd_duration_seconds_bucket[5m]))
)
```

### Using `* on() group_left()` to join carrier info to metrics:

```promql
# Add carrier label to all SIP metrics
rate(sip_invite_total[5m]) 
  * on (source_ip) group_left(carrier, trunk_group) 
  sip_source_info

# Then aggregate by carrier:
sum by (carrier) (
  rate(sip_invite_total[5m]) 
    * on (source_ip) group_left(carrier) 
    sip_source_info
)
```

## 5. The Standard Approach: Info Metric Pattern

**This is the recommended pattern** (from Brian Brazil / Robust Perception / kube-state-metrics):

### Pattern: Separate `*_info` metric with grouping labels

```
# Info metric — 1 time series per source IP (Gauge, value=1)
sip_source_info{source_ip="10.0.1.1", carrier="carrier_A", trunk_group="east"} 1
sip_source_info{source_ip="10.0.1.2", carrier="carrier_A", trunk_group="east"} 1
sip_source_info{source_ip="10.0.2.1", carrier="carrier_B", trunk_group="west"} 1

# Regular metrics — NO carrier/trunk labels
sip_invite_total{source_ip="10.0.1.1"} 1500
sip_invite_total{source_ip="10.0.1.2"} 3200
sip_invite_total{source_ip="10.0.2.1"} 890
```

### Advantages:

1. **Low cardinality on counters**: `sip_invite_total` only has `source_ip` label (not multiplied by carrier × trunk)
2. **Changing carrier mapping** doesn't break counter continuity — only the info metric changes
3. **Join is explicit** — users control when/where they need the join:
   ```promql
   sum by (carrier) (
     rate(sip_invite_total[5m]) 
       * on (source_ip) group_left(carrier) sip_source_info
   )
   ```
4. **Info metric is easily updated** without resetting all counters

### Anti-Pattern: Labels on Every Metric

```
# BAD — cardinality explosion, carrier changes break series
sip_invite_total{source_ip="10.0.1.1", carrier="carrier_A", trunk_group="east"} 1500
```

If carrier_A reassigns an IP, **all counters reset** because the label changed.

### When to put labels directly on metrics?

Per the official docs: only when **virtually all users** will need the label on every query. For SIP: `method` and `status_code` qualify — carrier/trunk does **not**.

## 6. Prometheus Relabeling for IP-to-Carrier Mapping

This is the **"do it in Prometheus, not the exporter"** approach. Two methods:

### Method A: Static relabeling via `relabel_configs`

```yaml
scrape_configs:
  - job_name: 'sip-exporter'
    static_configs:
      - targets: ['localhost:2112']
        labels:
          # No carrier label here — it comes from the metrics themselves
    metric_relabel_configs:
      # Map source_ip to carrier using a relabeling rule
      # This is limited — can only copy/transform existing labels
```

**Limitation**: Prometheus relabeling can only transform existing labels (regex, replace, hashmod). It **cannot** do CIDR matching or IP→carrier lookup. You'd need the mapping as a label already.

### Method B: File-based Service Discovery with labels

```yaml
scrape_configs:
  - job_name: 'sip-exporter'
    file_sd_configs:
      - files:
        - 'sip_targets.json'
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
```

`sip_targets.json`:
```json
[{
  "targets": ["10.0.1.1:5060", "10.0.1.2:5060"],
  "labels": {"carrier": "carrier_A", "trunk_group": "east"}
}, {
  "targets": ["10.0.2.1:5060"],
  "labels": {"carrier": "carrier_B", "trunk_group": "west"}
}]
```

### Method C: Consul/HTTP SD with external IP database

Use an external service that maps IPs to carriers and exposes them as SD targets with labels.

## Recommendation Matrix for sip-exporter

| Approach | Pros | Cons | Best For |
|----------|------|------|----------|
| **Info metric in exporter** | CIDR matching, hot-reload, single binary | More code in exporter | **Recommended** — sip-exporter already sees the IPs |
| **Prometheus relabeling** | Standard, no exporter changes | No CIDR, manual mapping | Small, static deployments |
| **File SD + labels** | Declarative, version-controlled | Still no CIDR matching | When IP list is static |
| **Labels on every metric** | Simplest queries | Cardinality explosion, breaks on remap | Only if carrier count is tiny and static |

## Final Recommendation for sip-exporter

**Use the `*_info` metric pattern in the exporter:**

```
# Gauge, value=1, maps source IP to carrier/trunk via CIDR lookup
sip_source_info{source_ip="10.0.1.5", carrier="carrier_A", trunk_group="east"} 1
```

**Configuration** (YAML with CIDRs):
```yaml
carrier_groups:
  - name: carrier_A
    trunk_group: east
    cidrs: ["10.0.1.0/24", "192.168.100.0/24"]
  - name: carrier_B
    trunk_group: west
    cidrs: ["10.0.2.0/24"]
```

**Why this is best:**
1. sip-exporter already parses every packet and knows source/dest IPs — it can do the CIDR lookup once and export the info metric
2. Counters stay low-cardinality (no carrier label multiplication)
3. CIDR matching is impossible in Prometheus relabeling alone
4. Carrier reassignments only affect the info metric, not counter continuity
5. Users join explicitly with `* on (source_ip) group_left(carrier) sip_source_info`
</task_result>
```

**Tool: read**

**Input:**
```json
{
  "filePath": "/home/aleksey/go/src/github.com/sip-exporter/internal/exporter/exporter.go",
  "offset": 240,
  "limit": 80
}
```

**Output:**
```
<path>/home/aleksey/go/src/github.com/sip-exporter/internal/exporter/exporter.go</path>
<type>file</type>
<content>
240: 			e.services.metricser.SystemError()
241: 			zap.L().Error("parse err", zap.Error(err))
242: 		}
243: 	}
244: }
245: 
246: func (e *exporter) readSocket() {
247: 	buf := make([]byte, readBufSize)
248: 
249: 	for {
250: 		n, err := unix.Read(e.sock, buf)
251: 		if err != nil {
252: 			if err == unix.EINTR {
253: 				continue
254: 			}
255: 			zap.L().Error("socket read error", zap.Error(err))
256: 			e.services.metricser.SystemError()
257: 			continue
258: 		}
259: 
260: 		if n == 0 {
261: 			continue
262: 		}
263: 
264: 		packet := make([]byte, n)
265: 		copy(packet, buf[:n])
266: 
267: 		zap.L().Debug("packet from socket", zap.Int("len", n))
268: 
269: 		e.messages <- packet
270: 	}
271: }
272: 
273: // parseRawPacket parses raw L2 packet.
274: func (e *exporter) parseRawPacket(packet []byte) error {
275: 	if len(packet) < 42 {
276: 		return fmt.Errorf("wrong len packet %d", len(packet))
277: 	}
278: 
279: 	// Parse Ethernet header (14 bytes)
280: 	ethType := packet[12:14]
281: 	ipOffset := 14
282: 
283: 	// VLAN (802.1Q)
284: 	if ethType[0] == 0x81 && ethType[1] == 0x00 {
285: 		if len(packet) < 18 {
286: 			return fmt.Errorf("wrong len packet %d", len(packet))
287: 		}
288: 		ethType = packet[16:18]
289: 		ipOffset = 18
290: 	}
291: 
292: 	// Only IPv4
293: 	if ethType[0] != 0x08 || ethType[1] != 0x00 {
294: 		return errors.New("not IPv4 packet")
295: 	}
296: 
297: 	// IP header
298: 	if len(packet) < ipOffset+20 {
299: 		return errors.New("ip header too short")
300: 	}
301: 
302: 	ipHeader := packet[ipOffset : ipOffset+20]
303: 	ihl := ipHeader[0] & 0x0F
304: 	ipHeaderLen := int(ihl) * 4
305: 
306: 	if ipHeader[9] != 17 { // UDP
307: 		return errors.New("not UDP packet")
308: 	}
309: 
310: 	// UDP header (8 bytes)
311: 	udpOffset := ipOffset + ipHeaderLen
312: 	if len(packet) < udpOffset+8 {
313: 		return errors.New("udp header too short")
314: 	}
315: 
316: 	// SIP data after UDP header
317: 	sipOffset := udpOffset + 8
318: 	if sipOffset >= len(packet) {
319: 		return errors.New("no SIP payload")

(Showing lines 240-319 of 724. Use offset=320 to continue.)
</content>
```

**Tool: read**

**Input:**
```json
{
  "filePath": "/home/aleksey/go/src/github.com/sip-exporter/internal/service/metrics.go",
  "offset": 1,
  "limit": 80
}
```

**Output:**
```
<path>/home/aleksey/go/src/github.com/sip-exporter/internal/service/metrics.go</path>
<type>file</type>
<content>
1: package service
2: 
3: import (
4: 	"bytes"
5: 	"sync/atomic"
6: 	"time"
7: 
8: 	"github.com/prometheus/client_golang/prometheus"
9: 	"github.com/prometheus/client_golang/prometheus/promauto"
10: 	"go.uber.org/zap"
11: )
12: 
13: type (
14: 	metrics struct {
15: 		proxyAuthenticationRequired       prometheus.Counter
16: 		statusBusyHereTotal               prometheus.Counter
17: 		statusTemporarilyUnavailableTotal prometheus.Counter
18: 		statusOKTotal                     prometheus.Counter
19: 		statusNotFoundTotal               prometheus.Counter
20: 		statusSessionProgressTotal        prometheus.Counter
21: 		statusRingingTotal                prometheus.Counter
22: 		statusTryingTotal                 prometheus.Counter
23: 		statusAcceptedTotal               prometheus.Counter
24: 		statusMultipleChoiceTotal         prometheus.Counter
25: 		statusMovedTemporarilyTotal       prometheus.Counter
26: 		statusBadRequestTotal             prometheus.Counter
27: 		statusUnauthorizedTotal           prometheus.Counter
28: 		statusForbiddenTotal              prometheus.Counter
29: 		statusRequestTimeoutTotal         prometheus.Counter
30: 		statusServerInternalTotal         prometheus.Counter
31: 		statusServiceUnavailableTotal     prometheus.Counter
32: 		statusServerTimeoutTotal          prometheus.Counter
33: 		statusBusyEverywhereTotal         prometheus.Counter
34: 		statusDeclineTotal                prometheus.Counter
35: 		requestInviteTotal                prometheus.Counter
36: 		requestACKTotal                   prometheus.Counter
37: 		requestByeTotal                   prometheus.Counter
38: 		requestCancelTotal                prometheus.Counter
39: 		requestOptionsTotal               prometheus.Counter
40: 		requestRegisterTotal              prometheus.Counter
41: 		requestUpdateTotal                prometheus.Counter
42: 		requestInfoTotal                  prometheus.Counter
43: 		requestReferTotal                 prometheus.Counter
44: 		requestSubscribeTotal             prometheus.Counter
45: 		requestNotifyTotal                prometheus.Counter
46: 		requestPrackTotal                 prometheus.Counter
47: 		requestPublishTotal               prometheus.Counter
48: 		requestMessageTotal               prometheus.Counter
49: 		systemErrorTotal                  prometheus.Counter
50: 		sipPacketsTotal                   prometheus.Counter
51: 		sessions                          prometheus.Gauge
52: 
53: 		// SER metrics (RFC 6076)
54: 		ser              prometheus.GaugeFunc
55: 		inviteTotal      int64
56: 		invite3xxTotal   int64
57: 		invite200OKTotal int64
58: 
59: 		// SEER metrics (RFC 6076)
60: 		seer                 prometheus.GaugeFunc
61: 		inviteEffectiveTotal int64
62: 
63: 		// ISA metric (RFC 6076)
64: 		isa                    prometheus.GaugeFunc
65: 		inviteIneffectiveTotal int64
66: 
67: 		// SCR metric (RFC 6076)
68: 		scr                   prometheus.GaugeFunc
69: 		sessionCompletedTotal int64
70: 
71: 		// ASR metric (ITU-T E.411)
72: 		asr prometheus.GaugeFunc
73: 
74: 		// NER metric (GSMA IR.42)
75: 		ner prometheus.GaugeFunc
76: 
77: 		// SDC metric — completed sessions counter
78: 		sdc prometheus.Counter
79: 
80: 		// ISS metric — ineffective session severity counter

(Showing lines 1-80 of 653. Use offset=81 to continue.)
</content>
```

---

## Assistant (Plan · GLM-5.1 · 16.1s)

_Thinking:_

Now I have a comprehensive picture. Let me also look at the current metrics structure and how IP is parsed to understand what we have to work with.

Let me also read the dto.Packet structure and the current metrics interface to understand the full picture before making recommendations.

Отличные данные. Давай ещё посмотрю текущую структуру пакета и интерфейса Metricser:

**Tool: read**

**Input:**
```json
{
  "filePath": "/home/aleksey/go/src/github.com/sip-exporter/internal/dto/dto.go"
}
```

**Error:**
```
File not found: /home/aleksey/go/src/github.com/sip-exporter/internal/dto/dto.go
```

**Tool: read**

**Input:**
```json
{
  "filePath": "/home/aleksey/go/src/github.com/sip-exporter/internal/service/metrics.go",
  "offset": 80,
  "limit": 120
}
```

**Output:**
```
<path>/home/aleksey/go/src/github.com/sip-exporter/internal/service/metrics.go</path>
<type>file</type>
<content>
80: 		// ISS metric — ineffective session severity counter
81: 		iss prometheus.Counter
82: 
83: 		// RRD metrics (RFC 6076 for REGISTER)
84: 		rrdTotal uint64               // deprecated: суммарная задержка для расчёта среднего (микросекунды)
85: 		rrdCount int64                // deprecated: количество измерений для среднего
86: 		rrd      prometheus.Histogram // Метрика RRD histogram (ms)
87: 		rrdAvg   prometheus.GaugeFunc // deprecated: среднее RRD (ms)
88: 
89: 		// SPD metrics (RFC 6076 §4.5)
90: 		spdTotalNs uint64               // deprecated: суммарная длительность сессий (наносекунды)
91: 		spdCount   int64                // deprecated: количество завершённых сессий
92: 		spd        prometheus.Histogram // Метрика SPD histogram (sec)
93: 		spdAvg     prometheus.GaugeFunc // deprecated: среднее SPD (sec)
94: 
95: 		// TTR metrics (Time to First Response)
96: 		ttr prometheus.Histogram // Метрика TTR histogram (ms)
97: 
98: 		// ORD metrics (OPTIONS Response Delay)
99: 		ord prometheus.Histogram
100: 
101: 		// LRD metrics (Location Registration Delay)
102: 		lrd prometheus.Histogram
103: 	}
104: 
105: 	Metricser interface {
106: 		Request(in []byte)
107: 		Response(in []byte, isInviteResponse bool)
108: 		ResponseWithMetrics(status []byte, isInviteResponse, is200OK bool)
109: 		Invite200OK()
110: 		SessionCompleted()
111: 		UpdateRRD(delayMs float64)
112: 		UpdateSPD(duration time.Duration)
113: 		UpdateTTR(delayMs float64)
114: 		UpdateORD(delayMs float64)
115: 		UpdateLRD(delayMs float64)
116: 		UpdateSession(size int)
117: 		SystemError()
118: 	}
119: )
120: 
121: func NewMetricser() Metricser {
122: 	return newMetricserWithRegistry(nil)
123: }
124: 
125: func newMetricserWithRegistry(reg *prometheus.Registry) Metricser {
126: 	m := &metrics{
127: 		sessions: newSessionsGaugeWithRegistry(reg),
128: 	}
129: 	initRequestCountersWithRegistry(m, reg)
130: 	initStatusCountersWithRegistry(m, reg)
131: 	initSystemCountersWithRegistry(m, reg)
132: 
133: 	m.ser = newSERWithRegistry(m, reg)
134: 	m.seer = newSEERWithRegistry(m, reg)
135: 	m.isa = newISAWithRegistry(m, reg)
136: 	m.scr = newSCRWithRegistry(m, reg)
137: 	m.asr = newASRWithRegistry(m, reg)
138: 	m.ner = newNERWithRegistry(m, reg)
139: 	m.sdc = newCounterWithRegistry("sip_exporter_sdc_total", "Total number of completed SIP sessions", reg)
140: 	m.iss = newCounterWithRegistry("sip_exporter_iss_total", "Total number of ineffective INVITE responses (408, 500, 503, 504) per RFC 6076 §4.8", reg)
141: 	m.rrd, m.rrdAvg = newRRDWithRegistry(m, reg)
142: 	m.spd, m.spdAvg = newSPDWithRegistry(m, reg)
143: 	m.ttr = newTTRWithRegistry(reg)
144: 	m.ord = newHistogramWithRegistry(prometheus.HistogramOpts{
145: 		Name:    "sip_exporter_ord",
146: 		Help:    "OPTIONS Response Delay in milliseconds (RFC 3261 §11.1)",
147: 		Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000},
148: 	}, reg)
149: 	m.lrd = newHistogramWithRegistry(prometheus.HistogramOpts{
150: 		Name:    "sip_exporter_lrd",
151: 		Help:    "Location Registration Delay in milliseconds: delay between REGISTER and 3xx redirect response",
152: 		Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000},
153: 	}, reg)
154: 
155: 	return m
156: }
157: 
158: func initRequestCountersWithRegistry(m *metrics, reg *prometheus.Registry) {
159: 	m.requestMessageTotal = newCounterWithRegistry("sip_exporter_message_total", "Total number of MESSAGE requests", reg)
160: 	m.requestPublishTotal = newCounterWithRegistry("sip_exporter_publish_total", "Total number of PUBLISH requests", reg)
161: 	m.requestPrackTotal = newCounterWithRegistry("sip_exporter_prack_total", "Total number of PRACK requests", reg)
162: 	m.requestNotifyTotal = newCounterWithRegistry("sip_exporter_notify_total", "Total number of NOTIFY requests", reg)
163: 	m.requestSubscribeTotal = newCounterWithRegistry("sip_exporter_subscribe_total", "Total number of SUBSCRIBE requests", reg)
164: 	m.requestReferTotal = newCounterWithRegistry("sip_exporter_refer_total", "Total number of REFER requests", reg)
165: 	m.requestInfoTotal = newCounterWithRegistry("sip_exporter_info_total", "Total number of INFO requests", reg)
166: 	m.requestUpdateTotal = newCounterWithRegistry("sip_exporter_update_total", "Total number of UPDATE requests", reg)
167: 	m.requestRegisterTotal = newCounterWithRegistry("sip_exporter_register_total", "Total number of REGISTER requests", reg)
168: 	m.requestOptionsTotal = newCounterWithRegistry("sip_exporter_options_total", "Total number of OPTIONS requests", reg)
169: 	m.requestCancelTotal = newCounterWithRegistry("sip_exporter_cancel_total", "Total number of CANCEL requests", reg)
170: 	m.requestByeTotal = newCounterWithRegistry("sip_exporter_bye_total", "Total number of BYE requests", reg)
171: 	m.requestACKTotal = newCounterWithRegistry("sip_exporter_ack_total", "Total number of ACK requests", reg)
172: 	m.requestInviteTotal = newCounterWithRegistry("sip_exporter_invite_total", "Total number of INVITE requests", reg)
173: }
174: 
175: func initStatusCountersWithRegistry(m *metrics, reg *prometheus.Registry) {
176: 	m.statusDeclineTotal = newCounterWithRegistry("sip_exporter_603_total", "Total number of 603 Decline responses", reg)
177: 	m.statusBusyEverywhereTotal = newCounterWithRegistry("sip_exporter_600_total", "Total number of 600 Busy Everywhere responses", reg)
178: 	m.statusServiceUnavailableTotal = newCounterWithRegistry(
179: 		"sip_exporter_503_total",
180: 		"Total number of 503 Service Unavailable responses",
181: 		reg,
182: 	)
183: 	m.statusServerTimeoutTotal = newCounterWithRegistry("sip_exporter_504_total", "Total number of 504 Server Time-out responses", reg)
184: 	m.statusServerInternalTotal = newCounterWithRegistry(
185: 		"sip_exporter_500_total",
186: 		"Total number of 500 Server Internal Error responses",
187: 		reg,
188: 	)
189: 	m.statusRequestTimeoutTotal = newCounterWithRegistry("sip_exporter_408_total", "Total number of 408 Request Timeout responses", reg)
190: 	m.statusForbiddenTotal = newCounterWithRegistry("sip_exporter_403_total", "Total number of 403 Forbidden responses", reg)
191: 	m.statusUnauthorizedTotal = newCounterWithRegistry("sip_exporter_401_total", "Total number of 401 Unauthorized responses", reg)
192: 	m.statusBadRequestTotal = newCounterWithRegistry("sip_exporter_400_total", "Total number of 400 Bad Request responses", reg)
193: 	m.statusMovedTemporarilyTotal = newCounterWithRegistry(
194: 		"sip_exporter_302_total",
195: 		"Total number of 302 Moved Temporarily responses",
196: 		reg,
197: 	)
198: 	m.statusMultipleChoiceTotal = newCounterWithRegistry("sip_exporter_300_total", "Total number of 300 Multiple Choices responses", reg)
199: 	m.statusAcceptedTotal = newCounterWithRegistry("sip_exporter_202_total", "Total number of 202 Accepted responses", reg)

(Showing lines 80-199 of 653. Use offset=200 to continue.)
</content>
```

**Tool: read**

**Input:**
```json
{
  "filePath": "/home/aleksey/go/src/github.com/sip-exporter/internal/exporter/exporter.go",
  "offset": 320,
  "limit": 100
}
```

**Output:**
```
<path>/home/aleksey/go/src/github.com/sip-exporter/internal/exporter/exporter.go</path>
<type>file</type>
<content>
320: 	}
321: 
322: 	sipData := packet[sipOffset:]
323: 
324: 	// Minimum SIP packet should be at least 50 bytes
325: 	if len(sipData) < 50 {
326: 		return fmt.Errorf("packet too small for SIP: %d", len(sipData))
327: 	}
328: 
329: 	// Check if this is a SIP packet (starts with SIP method or SIP/2.0)
330: 	if !bytes.HasPrefix(sipData, []byte("INVITE")) &&
331: 		!bytes.HasPrefix(sipData, []byte("ACK")) &&
332: 		!bytes.HasPrefix(sipData, []byte("BYE")) &&
333: 		!bytes.HasPrefix(sipData, []byte("CANCEL")) &&
334: 		!bytes.HasPrefix(sipData, []byte("OPTIONS")) &&
335: 		!bytes.HasPrefix(sipData, []byte("REGISTER")) &&
336: 		!bytes.HasPrefix(sipData, []byte("SUBSCRIBE")) &&
337: 		!bytes.HasPrefix(sipData, []byte("NOTIFY")) &&
338: 		!bytes.HasPrefix(sipData, []byte("PUBLISH")) &&
339: 		!bytes.HasPrefix(sipData, []byte("INFO")) &&
340: 		!bytes.HasPrefix(sipData, []byte("PRACK")) &&
341: 		!bytes.HasPrefix(sipData, []byte("UPDATE")) &&
342: 		!bytes.HasPrefix(sipData, []byte("MESSAGE")) &&
343: 		!bytes.HasPrefix(sipData, []byte("REFER")) &&
344: 		!bytes.HasPrefix(sipData, []byte("SIP/2.0")) {
345: 		return errors.New("not a SIP packet")
346: 	}
347: 
348: 	zap.L().Debug("packet raw", zap.ByteString("sip_data", sipData))
349: 
350: 	if err := e.handleMessage(sipData); err != nil {
351: 		return err
352: 	}
353: 
354: 	return nil
355: }
356: 
357: func (e *exporter) sipPacketParse(raw []byte) (dto.Packet, error) {
358: 	lines := bytes.Split(raw, []byte("\r\n"))
359: 	if len(lines) == 0 {
360: 		return dto.Packet{}, fmt.Errorf("split return empty result, raw: %b", raw)
361: 	}
362: 
363: 	p := dto.Packet{}
364: 	if bytes.HasPrefix(lines[0], []byte("SIP/2.0")) {
365: 		p.IsResponse = true
366: 		p.ResponseStatus = bytes.TrimPrefix(lines[0], []byte("SIP/2.0 "))[:3]
367: 	} else {
368: 		parts := bytes.SplitN(lines[0], []byte(" "), 3)
369: 		if len(parts) >= 2 {
370: 			p.IsResponse = false
371: 			p.Method = bytes.TrimSpace(parts[0])
372: 		}
373: 	}
374: 
375: 	for i, line := range lines {
376: 		if i == 0 {
377: 			continue
378: 		}
379: 
380: 		header, value := splitHeader(line)
381: 
382: 		switch {
383: 		case bytes.Equal(header, []byte("From")):
384: 			tag := extractTag(value)
385: 			if tag == nil {
386: 				return dto.Packet{}, fmt.Errorf("fail extract tag from '%b'", value)
387: 			}
388: 
389: 			p.From.Tag = tag
390: 		case bytes.Equal(header, []byte("To")):
391: 			p.To.Tag = extractTag(value)
392: 		case bytes.Equal(header, []byte("Call-ID")):
393: 			p.CallID = value
394: 		case bytes.Equal(header, []byte("CSeq")):
395: 			id, method := extractCSeq(value)
396: 			if id == nil || method == nil {
397: 				return dto.Packet{}, fmt.Errorf("fail extract CSeq from '%b'", value)
398: 			}
399: 
400: 			p.CSeq.Method = method
401: 			p.CSeq.ID = id
402: 		case bytes.Equal(header, []byte("Session-Expires")):
403: 			p.SessionExpires = extractSessionExpires(value)
404: 		}
405: 	}
406: 
407: 	return p, nil
408: }
409: 
410: func (e *exporter) handleMessage(rawPacket []byte) error {
411: 	packet, err := e.sipPacketParse(rawPacket)
412: 	if err != nil {
413: 		return fmt.Errorf("parse SIP packet: %w", err)
414: 	}
415: 
416: 	zap.L().Debug("parsed packet", zap.Any("packet", packet))
417: 
418: 	if packet.IsResponse {
419: 		e.handleResponse(packet)

(Showing lines 320-419 of 724. Use offset=420 to continue.)
</content>
```

---

## Assistant (Plan · GLM-5.1 · 16.7s)

**Tool: glob**

**Input:**
```json
{
  "pattern": "**/dto/**"
}
```

**Output:**
```
/home/aleksey/go/src/github.com/sip-exporter/internal/dto/sip.go
```

**Tool: read**

**Input:**
```json
{
  "filePath": "/home/aleksey/go/src/github.com/sip-exporter/internal/exporter/exporter.go",
  "limit": 50,
  "offset": 270
}
```

**Output:**
```
<path>/home/aleksey/go/src/github.com/sip-exporter/internal/exporter/exporter.go</path>
<type>file</type>
<content>
270: 	}
271: }
272: 
273: // parseRawPacket parses raw L2 packet.
274: func (e *exporter) parseRawPacket(packet []byte) error {
275: 	if len(packet) < 42 {
276: 		return fmt.Errorf("wrong len packet %d", len(packet))
277: 	}
278: 
279: 	// Parse Ethernet header (14 bytes)
280: 	ethType := packet[12:14]
281: 	ipOffset := 14
282: 
283: 	// VLAN (802.1Q)
284: 	if ethType[0] == 0x81 && ethType[1] == 0x00 {
285: 		if len(packet) < 18 {
286: 			return fmt.Errorf("wrong len packet %d", len(packet))
287: 		}
288: 		ethType = packet[16:18]
289: 		ipOffset = 18
290: 	}
291: 
292: 	// Only IPv4
293: 	if ethType[0] != 0x08 || ethType[1] != 0x00 {
294: 		return errors.New("not IPv4 packet")
295: 	}
296: 
297: 	// IP header
298: 	if len(packet) < ipOffset+20 {
299: 		return errors.New("ip header too short")
300: 	}
301: 
302: 	ipHeader := packet[ipOffset : ipOffset+20]
303: 	ihl := ipHeader[0] & 0x0F
304: 	ipHeaderLen := int(ihl) * 4
305: 
306: 	if ipHeader[9] != 17 { // UDP
307: 		return errors.New("not UDP packet")
308: 	}
309: 
310: 	// UDP header (8 bytes)
311: 	udpOffset := ipOffset + ipHeaderLen
312: 	if len(packet) < udpOffset+8 {
313: 		return errors.New("udp header too short")
314: 	}
315: 
316: 	// SIP data after UDP header
317: 	sipOffset := udpOffset + 8
318: 	if sipOffset >= len(packet) {
319: 		return errors.New("no SIP payload")

(Showing lines 270-319 of 724. Use offset=320 to continue.)
</content>
```

---

## Assistant (Plan · GLM-5.1 · 9.5s)

**Tool: read**

**Input:**
```json
{
  "filePath": "/home/aleksey/go/src/github.com/sip-exporter/internal/dto/sip.go"
}
```

**Output:**
```
<path>/home/aleksey/go/src/github.com/sip-exporter/internal/dto/sip.go</path>
<type>file</type>
<content>
1: package dto
2: 
3: type (
4: 	Packet struct {
5: 		IsResponse     bool
6: 		ResponseStatus []byte
7: 		Method         []byte
8: 		From           From
9: 		To             To
10: 		CallID         []byte
11: 		CSeq           CSeq
12: 		SessionExpires int // seconds
13: 	}
14: 	From struct {
15: 		Addr []byte
16: 		Tag  []byte
17: 	}
18: 	To struct {
19: 		Addr []byte
20: 		Tag  []byte
21: 	}
22: 	CSeq struct {
23: 		ID     []byte
24: 		Method []byte
25: 	}
26: )

(End of file - total 26 lines)
</content>
```

**Tool: read**

**Input:**
```json
{
  "filePath": "/home/aleksey/go/src/github.com/sip-exporter/internal/config/config.go"
}
```

**Output:**
```
<path>/home/aleksey/go/src/github.com/sip-exporter/internal/config/config.go</path>
<type>file</type>
<content>
1: package config
2: 
3: import (
4: 	"fmt"
5: 
6: 	"github.com/ilyakaznacheev/cleanenv"
7: )
8: 
9: type (
10: 	App struct {
11: 		LogLevel      string `env:"SIP_EXPORTER_LOGGER_LEVEL" env-default:"info"`
12: 		Port          string `env:"SIP_EXPORTER_HTTP_PORT" env-default:"2112"`
13: 		Interface     string `env:"SIP_EXPORTER_INTERFACE" env-required:"true"`
14: 		BPFBinaryPath string `env:"SIP_EXPORTER_OBJECT_FILE_PATH" env-default:"/usr/local/bin/sip.o"`
15: 		SIPPort       int    `env:"SIP_EXPORTER_SIP_PORT" env-default:"5060"`
16: 		SIPSPort      int    `env:"SIP_EXPORTER_SIPS_PORT" env-default:"5061"`
17: 	}
18: )
19: 
20: func GetConfig() (*App, error) {
21: 	cfg := &App{}
22: 
23: 	if err := cleanenv.ReadEnv(cfg); err != nil {
24: 		helpText := "error read env"
25: 		help, _ := cleanenv.GetDescription(cfg, &helpText)
26: 		return nil, fmt.Errorf("err: %s, info: %s", err.Error(), help)
27: 	}
28: 
29: 	return cfg, nil
30: }

(End of file - total 30 lines)
</content>
```

---

## Assistant (Plan · GLM-5.1 · 158.0s)

_Thinking:_

Now I have a comprehensive picture. Let me synthesize all the research into a clear analysis for the user. Here's what I've found:

## Current state of the codebase:

1. **IP addresses are already parsed** in `parseRawPacket()` — the IP header is at `ipHeader = packet[ipOffset : ipOffset+20]`, which contains src IP at bytes 12-15 and dst IP at bytes 16-19. But currently the IP addresses are **discarded** — only used for protocol check (UDP).

2. **dto.Packet** has no IP fields — only SIP-level data (From/To tags, Call-ID, CSeq, etc.)

3. **Metricser interface** is all global counters — no labels at all

4. **Config** is env-only, no YAML support

## Key architectural decisions to make:

### Approach A: Labels on every metric (anti-pattern per Prometheus best practices)
- Every metric gets a `carrier` label
- Cardinality explosion: 50 carriers × 14 request methods × 20 status codes = 14,000 series
- Carrier reassignment = counter reset
- Simplest PromQL queries

### Approach B: Info metric pattern (recommended by Brian Brazil / Robust Perception)
- Export `sip_source_info{source_ip="10.0.1.5", carrier="provider-a"}` as a separate info metric
- Regular metrics get only a `source_ip` label (not carrier)
- Users join with `* on (source_ip) group_left(carrier) sip_source_info`
- Lower cardinality, carrier changes don't reset counters
- But: source_ip as a label is still high cardinality if there are many IPs

### Approach C: Direct carrier label on metrics (HOMER approach)
- HOMER uses `target_name` label directly on metrics
- Simple IP → name mapping at export time
- If an IP doesn't match, it gets `target_name="unknown"`
- Cardinality bounded by number of carrier groups, not individual IPs
- This is what makes most sense for our use case

### My recommendation: Hybrid approach

Given the specific use case (SIP monitoring with known carrier IP ranges):

1. **Don't expose individual source_ip as a label** — cardinality explosion in production
2. **Add a `carrier` label directly on metrics** — via CIDR lookup at parse time
3. **Unknown IPs get `carrier=""` (empty) or skip the label** — keeps cardinality bounded
4. **This is exactly what HOMER does** — and it works well because the number of carriers is bounded (typically 5-50)

Actually, wait. Let me think about this more carefully.

The info metric pattern is the "correct" Prometheus approach, but it has a significant UX downside: the PromQL join is complex and confusing for most users. `sum by (carrier) (rate(sip_invite_total[5m]) * on (source_ip) group_left(carrier) sip_source_info)` is not intuitive.

For HOMER's approach (direct label), the cardinality is bounded by the number of carrier groups in the config (not by individual IPs), so it's safe. With 50 carriers, we'd have 50 × 14 request methods = 700 series for requests, 50 × 20 status codes = 1000 for responses. That's ~2000 total, which is well within Prometheus limits.

The key question is: should we use source_ip as a label or carrier as a label?

Option 1: carrier label only (no source_ip)
- sip_invite_total{carrier="provider-a"}
- Cardinality: carriers × methods = manageable
- Lost: can't drill down to individual IP
- Simplest

Option 2: carrier + source_ip labels
- sip_invite_total{carrier="provider-a", source_ip="10.0.1.5"}
- Cardinality: individual IPs × methods = potentially huge
- More granular but dangerous

Option 3: carrier label on metrics + separate info metric for IP→carrier mapping
- sip_invite_total{carrier="provider-a"}
- sip_carrier_ip_info{source_ip="10.0.1.5", carrier="provider-a"} 1
- Best of both worlds: metrics stay low-cardinality, but info metric provides IP drill-down

I think Option 3 is actually the best:
- Most queries will just use carrier label → simple PromQL
- Info metric allows joining when needed
- No source_ip on counters → no cardinality explosion
- Carrier changes only affect info metric

But actually, for this use case, I think the HOMER approach is most practical. Let me think about what the user actually needs...

In a typical SIP NOC:
- Operators want to see SER per carrier → `sip_ser{carrier="provider-a"}`
- They DON'T need per-IP drill-down in Prometheus (they use Grafana/Loki for packet-level analysis)
- The carrier count is bounded and configured by the operator

So the simplest approach that works:
1. YAML config with carrier CIDR groups
2. At parse time, look up src/dst IP → carrier name
3. Add `carrier` label to all metrics
4. If IP doesn't match any CIDR → `carrier="unknown"` or `carrier="other"`

This is exactly what HOMER does and it works great.

But wait, there's a subtlety. For SIP metrics, should we use source IP or destination IP to determine the carrier? In a typical setup:
- SIP traffic flows through the exporter's interface
- Source IP = who sent the packet (could be carrier A's SBC)
- Destination IP = who receives it (could be our SBC)

For per-carrier analysis, the relevant IP depends on direction:
- Inbound traffic: source IP identifies the carrier
- Outbound traffic: destination IP identifies the carrier

This is where HOMER's `direction` label becomes useful. But for sip-exporter, we can simplify:
- The carrier is determined by the "far end" IP
- If our SIP port is the destination → inbound, carrier = source IP
- If our SIP port is the source → outbound, carrier = destination IP

Actually, let me think about this differently. The exporter is a passive monitor. It sees all traffic on an interface. The operator configures which IP ranges belong to which carrier. When a packet arrives:
- Source IP matches carrier A → this packet involves carrier A as sender
- Destination IP matches carrier B → this packet involves carrier B as receiver

For SER/SEER/ISA/SCR, the carrier should be identified by who sent the INVITE (source IP of INVITE request). For responses, the carrier is identified by the original INVITE's source.

Hmm, this is getting complex. Let me simplify for the plan:

**Phase 1 (v0.11): Simple carrier label based on source IP**
- Config: CIDR groups
- At parse time: lookup source IP → carrier name
- Add `carrier` label to all metrics
- Simple, works for the common case (monitoring inbound traffic from carriers)

**Future enhancement**: direction awareness, bidirectional carrier tracking

Let me now structure the analysis for the user.

Actually, I realize there's a more fundamental question: should we have two labels or one?

Looking at the research:
- HOMER uses `target_name` + `direction` — but this creates 2× cardinality
- voiplens uses just carrier name from dispatcher set
- Our case: CIDR matching on IPs

I think the cleanest approach for sip-exporter is:

1. When a SIP packet arrives, we have source IP and destination IP
2. We look up BOTH against the carrier CIDR config
3. If source IP matches carrier A → the packet is FROM carrier A
4. If destination IP matches carrier B → the packet is TO carrier B
5. We add a single `carrier` label based on the "remote" IP

But "remote" is relative... On a border SBC with two NICs:
- eth0 (external): carrier traffic
- eth1 (internal): our PBX traffic

The exporter monitors one interface. If on eth0:
- Source IP = carrier (inbound INVITE from carrier)
- Destination IP = our SBC (inbound to us)
- For outbound: source = our SBC, dest = carrier

So the carrier is always the IP that is NOT in our own network range.

But we don't know what "our network range" is...

Actually, the simplest and most practical approach used in production:
- Config has carrier groups with CIDRs
- Source IP is matched against carrier CIDRs
- If match → carrier label = that carrier's name
- If no match → carrier label = "unknown" (or empty)

This works because:
- Inbound INVITE from carrier → source IP = carrier → correct
- Outbound INVITE to carrier → source IP = our SBC → unknown → still OK because we're primarily interested in tracking carrier performance from their responses

Wait, but we also want to track outbound calls to carriers... In that case:
- Outbound INVITE → dest IP = carrier, source IP = us
- Response to our INVITE → source IP = carrier, dest IP = us

So for INVITE tracking, we should match the DESTINATION IP for outbound and SOURCE IP for inbound? But we don't know which is which without more context...

Actually, the simplest approach that works for 90% of use cases:
- Match both source and destination IPs against carrier CIDRs
- Use the one that matches as the carrier
- If both match (inter-carrier traffic) → use source IP
- If neither matches → "unknown"

This is what HOMER essentially does. Let me go with this approach.

OK let me write up the comprehensive analysis now.

Теперь у меня полная картина. Вот анализ:

---

## Анализ: Per-Carrier Labels для sip-exporter

### Как это делают конкуренты

**HOMER (heplify-server)** — самый близкий аналог:
```toml
# Конфиг: статический IP → Name mapping
PromTargetIP   = "10.1.2.111,10.1.2.4"
PromTargetName = "sbc_access,kamailio"
```
- Labels: `target_name` + `direction` (src/dst) на каждой метрике
- IP не совпал → `target_name="unknown"`
- Hot-reload через SIGHUP
- Simple matching (exact IP, не CIDR)

**voiplens/kamailio_exporter** — единственный с per-carrier:
```bash
--collector.dispatcher.mapping="200:Carrier 1"
```
- Label: `set_name` на dispatcher-метриках
- Только для Kamailio dispatcher, не для общего трафика

**Другие** (FreeSWITCH exporter, florentchauveau/kamailio_exporter) — **никаких carrier labels вообще**.

**Вывод:** Никто в OSS не делает CIDR-based carrier mapping на уровне passive capture. Наша уникальная ниша.

---

### Как это будет использоваться (сценарии)

**Сценарий 1: NOC дежурный — деградация транка**
```
 sip_ser{carrier="provider-a"} → 45%  ← УПАЛ!
 sip_ser{carrier="provider-b"} → 98%  ← Норма
```
→ Алерт: "Provider-A SER ниже 80%" → NOC звонит провайдеру

**Сценарий 2: SRE — анализ задержек**
```promql
histogram_quantile(0.95, sum by(carrier,le)(rate(sip_exporter_ttr_bucket[5m])))
```
→ График: P95 TTR по каждому carrier — кто тормозит?

**Сценарий 3: Capacity planning — нагрузка по транкам**
```promql
sum by(carrier)(rate(sip_exporter_invite_total[5m]))
```
→ Сколько CPS генерит каждый carrier

**Сценарий 4: SCR мониторинг — кто не завершает звонки**
```promql
sip_exporter_scr{carrier="provider-a"}
```
→ Provider-A много незавершённых сессий

---

### Два архитектурных подхода

#### Подход A: Carrier label напрямую на метриках (как HOMER)
```
sip_exporter_invite_total{carrier="provider-a"} 1500
sip_exporter_ser{carrier="provider-a"} 95.5
sip_exporter_ttr_bucket{carrier="provider-a",le="50"} 42
```

| Плюсы | Минусы |
|-------|--------|
| Простой PromQL: `sip_ser{carrier="X"}` | Смена IP у carrier = reset счётчиков |
| Не нужен join | Cardinality: carriers × metrics |
| Как HOMER — привычно | |

#### Подход B: Info metric pattern (Prometheus best practice)
```
# Info metric — mapping IP→carrier
sip_carrier_info{source_ip="10.0.1.5",carrier="provider-a"} 1

# Основные метрики — только source_ip
sip_exporter_invite_total{source_ip="10.0.1.5"} 1500
```
Join в PromQL:
```promql
sum by(carrier)(rate(sip_exporter_invite_total[5m]) 
  * on(source_ip) group_left(carrier) sip_carrier_info)
```

| Плюсы | Минусы |
|-------|--------|
| Cardinality: IP × metrics, не carrier × metrics | Сложный PromQL с join |
| Смена carrier — не сбрасывает счётчики | source_ip на метриках = тысячи series в проде |
| Prometheus-idiomatic | Неудобно для NOC |

---

### Моя рекомендация: Подход A (прямой label)

Причины:

1. **Cardinality контролируема** — количество carriers задаёт оператор в конфиге (типично 5-50). 50 carriers × 14 request counters × 20 response counters ≈ 1700 series — это норма для Prometheus

2. **source_ip как label — опасно** (тысячи IP в проде), а carrier — bounded по определению

3. **HOMER делает так же** и это работает у тысяч пользователей

4. **PromQL тривиальный** — NOC-инженер пишет `sip_ser{carrier="provider-a"}`, а не join на 3 строки

5. **Смена IP у carrier редка** — происходит раз в месяцы/годы, reset счётчиков не критичен

---

### Как определить carrier из пакета

Текущий код в `parseRawPacket()` (`exporter.go:302`):
```go
ipHeader := packet[ipOffset : ipOffset+20]
// byte 12-15 = source IP, byte 16-19 = dest IP
```

IP-адреса уже доступны, но **не извлекаются**. Алгоритм:

1. Извлечь source IP и destination IP из IPv4 header
2. Матчить **оба** IP против CIDR-групп из конфига
3. Если source IP совпал → carrier = matched group
4. Если dest IP совпал, source нет → carrier = matched group
5. Если оба/ни один → `carrier=""` (пустой, без label)

---

### Конфигурация

Сейчас конфиг — env-only (`cleanenv`). Нужно добавить YAML с CIDR-группами. Вариант:

```yaml
# carriers.yaml
carriers:
  - name: "provider-a"
    cidrs:
      - "10.0.1.0/24"
      - "192.168.100.0/24"
  - name: "provider-b"
    cidrs:
      - "10.0.2.0/24"
  - name: "internal"
    cidrs:
      - "10.10.0.0/16"
```

Env var: `SIP_EXPORTER_CARRIERS_CONFIG=/etc/sip-exporter/carriers.yaml`

Если файл не задан — метрики без carrier label (обратная совместимость).

---

### Что думаешь по подходу A vs B? И по формату конфига — YAML файл через env var?

---

## User

то есть label будет у каждой метрики ?

---

## Assistant (Plan · GLM-5.1 · 82.6s)

_Thinking:_

The user is asking whether the carrier label will be on every metric. This is an important design question. Let me think about this carefully.

Currently the metrics are:
1. **Counters** (14 request counters, 20+ status code counters, systemErrorTotal, sipPacketsTotal, sdc, iss)
2. **Gauges/GaugeFuncs** (sessions, ser, seer, isa, scr, asr, ner, rrdAvg, spdAvg)
3. **Histograms** (rrd, spd, ttr, ord, lrd)

The question is: should ALL of these get a `carrier` label?

Let me think about which ones make sense:

**Make sense with carrier label:**
- Request counters (invite_total, bye_total, etc.) — "how many INVITEs from carrier X"
- Response status counters (200_total, 503_total, etc.) — "how many 503 from carrier X"
- SER, SEER, ISA, SCR, ASR, NER — "what's the SER for carrier X" — THIS IS THE KEY USE CASE
- Histograms (rrd, spd, ttr, ord, lrd) — "what's the P95 TTR for carrier X"

**Don't make sense with carrier label:**
- systemErrorTotal — this is an exporter-level metric, not carrier-specific
- sipPacketsTotal — all packets, not per-carrier
- sessions gauge — could be per-carrier (active dialogs per carrier), but the dialog tracking would need to know the carrier

Actually, let me think more carefully. The key insight is:

For SER/SEER/ISA/SCR to be per-carrier, we need per-carrier counters for:
- inviteTotal per carrier
- invite3xxTotal per carrier
- invite200OKTotal per carrier
- inviteEffectiveTotal per carrier
- inviteIneffectiveTotal per carrier
- sessionCompletedTotal per carrier

These are currently atomic counters (int64) that feed into GaugeFunc calculations. To make them per-carrier, they'd need to be maps or labeled counters.

There are two approaches:

### Approach 1: Labeled Prometheus counters for everything
Every counter gets a `carrier` label. SER/SEER/ISA/SCR become GaugeFuncs that iterate over labeled counter values.

Problem: SER/SEER/ISA/SCR are GaugeFuncs — they compute from atomic counters. To make them per-carrier, we'd need to either:
a) Create a GaugeFunc per carrier (needs dynamic registration — messy)
b) Use labeled counters and compute per-carrier ratios in the GaugeFunc

Actually, Prometheus has `prometheus.GaugeFunc` but it can't have labels that vary. So we'd need to switch from GaugeFunc to something else, or use a different approach.

Wait, let me think about this differently. The current architecture:
- Atomic counters: inviteTotal, invite3xxTotal, invite200OKTotal, etc.
- GaugeFunc reads these atomics and computes SER = invite200OKTotal / (inviteTotal - invite3xxTotal) * 100

For per-carrier, we'd need:
- Per-carrier atomic counters: inviteTotal[carrier], invite3xxTotal[carrier], etc.
- Per-carrier GaugeFunc: SER[carrier] = invite200OKTotal[carrier] / (inviteTotal[carrier] - invite3xxTotal[carrier]) * 100

The simplest implementation would be:
- Use `prometheus.CounterVec` instead of `prometheus.Counter` for request/status counters
- For SER/SEER/ISA/SCR, use a map of atomic counters per carrier, and a GaugeFunc per carrier

Actually, a cleaner approach would be:
- Track the raw counters per carrier in a map: `map[string]*carrierCounters`
- Each carrier has its own inviteTotal, invite200OKTotal, etc.
- SER/SEER/ISA/SCR are computed from these per-carrier counters
- Export as labeled GaugeFunc or custom Collector

Hmm, but GaugeFunc doesn't support labels well. We'd need to use a custom Collector or switch to a different approach.

Actually, the standard pattern in Prometheus Go client for labeled gauges with computed values is to use `prometheus.GaugeVec` and update them periodically, or implement a custom `prometheus.Collector`.

Let me think about what's simplest and most maintainable...

### Simplest approach: CounterVec + GaugeVec

1. Replace `prometheus.Counter` with `prometheus.CounterVec` for all request/status counters
2. Replace the atomic int64 counters (inviteTotal, etc.) with `map[string]*int64` or `sync.Map`
3. For SER/SEER/ISA/SCR: instead of GaugeFunc, use `prometheus.GaugeVec` and update values periodically (e.g., in the existing `sipDialogMetricsUpdate()` goroutine that runs every 1s)

Wait, but GaugeFunc is nice because it computes on scrape. If we switch to GaugeVec + periodic update, we'd have stale values between updates.

Alternative: implement a custom Collector that computes SER per carrier on scrape. This is what `promauto` GaugeFunc does under the hood anyway.

Actually, the cleanest approach:

```go
type carrierMetrics struct {
    inviteTotal      atomic.Int64
    invite3xxTotal   atomic.Int64
    invite200OKTotal atomic.Int64
    // etc.
}

type metrics struct {
    // existing fields...
    
    // Per-carrier counters
    carriers sync.Map // map[string]*carrierMetrics
    
    // Labeled Prometheus metrics
    requestInviteTotal *prometheus.CounterVec
    statusOKTotal      *prometheus.CounterVec
    // etc.
    
    // Per-carrier SER/SEER/ISA/SCR — custom collector
    serVec *prometheus.GaugeVec
}
```

When a packet arrives:
1. Look up carrier from IP
2. Increment CounterVec with carrier label
3. Increment atomic counter in carrierMetrics map
4. SER GaugeVec is updated on scrape from carrierMetrics atomics

This is a significant rework of the metrics layer but keeps the same general architecture.

### But does it need to be on EVERY metric?

Let me categorize:

**Definitely per-carrier:**
- All request counters (invite, bye, register, etc.)
- All response status counters (200, 408, 503, etc.)
- SER, SEER, ISA, SCR, ASR, NER (the RFC 6076 metrics — THE MAIN REASON for this feature)
- RRD, SPD, TTR, ORD, LRD histograms (latency per carrier)
- sessions gauge (active dialogs per carrier)
- SDC, ISS counters

**NOT per-carrier (exporter-level):**
- systemErrorTotal — exporter health
- sipPacketsTotal — total packets processed

So yes, carrier label would be on essentially every SIP metric. That's the whole point of the feature.

Let me answer the user's question clearly.

Да, по сути на каждой SIP-метрике. Это и есть смысл фичи — без label на метрике невозможно группировать.

Конкретно по категориям:

| Метрики | carrier label | Почему |
|---------|:---:|--------|
| 14 request counters (invite_total, bye_total, ...) | Да | "Сколько INVITE от provider-a" |
| 20 response counters (200_total, 503_total, ...) | Да | "Сколько 503 от provider-a" |
| SER, SEER, ISA, SCR, ASR, NER | Да | **Главная цель** — SER per carrier |
| RRD, SPD, TTR, ORD, LRD (histograms) | Да | "P95 TTR provider-a vs provider-b" |
| sessions gauge | Да | "Активные диалоги provider-a" |
| sdc_total, iss_total | Да | "Завершённые сессии provider-a" |
| system_error_total | **Нет** | Это здоровье экспортера, не carrier |
| sip_packets_total | **Нет** | Общий счётчик пакетов |

**Cardinality:** при 10 carriers → ~10 × 50 метрик = **~500 series**. Нормально для Prometheus (лимит обычно 10K+).

Это потребует переработки `Metricser` — все `Counter` станут `CounterVec`, `GaugeFunc` (SER/SEER/ISA/SCR) — вычисляться per-carrier. Основной объём работы, но архитектура остаётся та же.

Норм, переходим к детальному плану?

---

## User

а общие метрики останутся ? Допустим пользователь хочет видеть значение с лейблом и без

---

## Assistant (Plan · GLM-5.1 · 27.0s)

_Thinking:_

The user is asking whether the global (unlabeled) metrics will remain alongside the per-carrier labeled metrics. This is an important UX question.

The answer is: they should be able to see both. There are two approaches:

1. **Keep both** — global metrics (no label) + per-carrier metrics (with carrier label). This means `sip_exporter_invite_total` (global) AND `sip_exporter_invite_total{carrier="provider-a"}` (per-carrier).

2. **Only per-carrier** — all metrics have carrier label. Global aggregation via PromQL: `sum(sip_exporter_invite_total)`.

Approach 2 is cleaner because:
- No duplicate metrics
- `sum()` in PromQL gives the same result as the global counter
- Less cardinality
- Standard Prometheus pattern

Approach 1 means:
- Double the metrics
- User sees both in /metrics endpoint
- Simpler for users who don't care about per-carrier

Actually, the most practical approach is:
- If carriers config is NOT provided → metrics without carrier label (backward compatible)
- If carriers config IS provided → metrics WITH carrier label only
- Global aggregation via `sum by()()` or just `sum()` in PromQL

This way:
- Backward compatible (no config = old behavior)
- When carriers are configured, user can always get global via `sum(sip_exporter_invite_total)`
- No duplication

Let me present this clearly to the user.

Хороший вопрос. Есть два варианта:

### Вариант 1: Только per-carrier, глобальные через PromQL
```
sip_exporter_invite_total{carrier="provider-a"} 100
sip_exporter_invite_total{carrier="provider-b"} 50
sip_exporter_invite_total{carrier="unknown"}     30
```
Глобальное значение: `sum(sip_exporter_invite_total)` = 180

### Вариант 2: И те, и другие одновременно
```
sip_exporter_invite_total                       180   ← глобальная (без label)
sip_exporter_invite_total{carrier="provider-a"} 100   ← per-carrier
sip_exporter_invite_total{carrier="provider-b"} 50
sip_exporter_invite_total{carrier="unknown"}     30
```

### Вариант 3: Условно — зависит от конфига

```
# Нет carriers.yaml → метрики без label (как сейчас, backward compatible)
sip_exporter_invite_total  180

# Есть carriers.yaml → метрики с carrier label
sip_exporter_invite_total{carrier="provider-a"} 100
sip_exporter_invite_total{carrier="provider-b"} 50
```

Глобальное значение в любом случае через `sum()`.

---

**Моя рекомендация — Вариант 3:**

- **Нет конфига** = текущее поведение, ноль изменений для существующих пользователей
- **Есть конфиг** = carrier label на метриках
- Глобальное значение всегда доступно через `sum()` — это стандартный Prometheus-паттерн
- Нет дублирования метрик (не раздуваем /metrics)
- `carrier=""` (пустой) для unmatched IP — чтобы `sum()` давал точный результат

А если пользователь хочет видеть и то и другое на дашборде — в Grafana два запроса:
```promql
# Per-carrier
sip_exporter_invite_total

# Global
sum(sip_exporter_invite_total)
```

Какой вариант предпочитаешь?

---

## User

не очень понял. Приведи пример объявления метрики с label в голанге

---

## Assistant (Plan · GLM-5.1 · 21.3s)

Вот пример. Сейчас:

```go
// Без label — обычный Counter
requestInviteTotal prometheus.Counter

// Создание:
m.requestInviteTotal = promauto.NewCounter(prometheus.CounterOpts{
    Name: "sip_exporter_invite_total",
    Help: "Total number of INVITE requests",
})

// Использование:
m.requestInviteTotal.Inc()
```

С carrier label:

```go
// С label — CounterVec
requestInviteTotal *prometheus.CounterVec

// Создание:
m.requestInviteTotal = promauto.NewCounterVec(prometheus.CounterOpts{
    Name: "sip_exporter_invite_total",
    Help: "Total number of INVITE requests",
}, []string{"carrier"})

// Использование:
m.requestInviteTotal.WithLabelValues("provider-a").Inc()
m.requestInviteTotal.WithLabelValues("provider-b").Inc()
```

Результат в `/metrics`:
```
sip_exporter_invite_total{carrier="provider-a"} 100
sip_exporter_invite_total{carrier="provider-b"} 50
```

---

Для SER/SEER/ISA/SCR сложнее — сейчас это `GaugeFunc`, которая читает atomic-счётчики и вычисляет ratio. С carrier label понадобится **отдельный набор atomic-счётчиков на каждый carrier**:

```go
// Сейчас:
ser              prometheus.GaugeFunc
inviteTotal      int64  // atomic
invite200OKTotal int64  // atomic
// GaugeFunc: SER = invite200OKTotal / (inviteTotal - invite3xxTotal) * 100

// С carrier label:
ser          *prometheus.GaugeVec
carrierData  sync.Map // map[string]*carrierCounters

type carrierCounters struct {
    inviteTotal      atomic.Int64
    invite3xxTotal   atomic.Int64
    invite200OKTotal atomic.Int64
    // ...
}
```

Или можно сделать custom `Collector`, который при scrape вычисляет SER per carrier из `CounterVec`-значений — но Prometheus Go client не даёт читать значение Counter напрямую. Поэтому atomic-счётчики per carrier + GaugeVec — более надёжный путь.

Понятно? Переходим к плану?

---

