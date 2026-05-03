# SIP Exporter — Roadmap & Feature Proposals

## Реализованные метрики (v0.5–0.10)

| Метрика | RFC 6076 | Описание | Версия |
|---------|----------|----------|--------|
| SER | §4.6 | Session Establishment Ratio | 0.5.0 |
| SEER | §4.7 | Session Establishment Effectiveness Ratio | 0.6.0 |
| ISA | §4.8 | Ineffective Session Attempts | 0.7.0 |
| SCR | §4.9 | Session Completion Ratio | 0.8.0 |
| RRD | §4.1 | Registration Request Delay (histogram) | 0.8.0 |
| SPD | §4.5 | Session Process Duration (histogram) | 0.9.0 |
| TTR | — | Time to First Response (histogram) | 0.10.0 |
| PDD | — | Post-Dial Delay (histogram) | 0.15.0 |
| NER | — | Network Effectiveness Ratio (GSMA IR.42) | 0.10.0 |
| ISS | — | Ineffective Session Severity (counter) | 0.10.0 |
| ORD | — | OPTIONS Response Delay (histogram) | 0.10.0 |
| LRD | — | Location Registration Delay (histogram) | 0.10.0 |

---

## Предлагаемые метрики RFC 6076

### ~~P0: PDD — Post-Dial Delay~~ ✅ Реализовано (v0.15.0)

**Описание:** время от INVITE до первого 180 Ringing / 183 Session Progress (исключая 100 Trying). Не определена в RFC 6076, но является стандартной индустриальной метрикой (ITU-T E.411, GSMA IR.42).

**Зачем:** показывает реальную задержку до начала звонка на стороне callee. В отличие от TTR (который измеряет INVITE → любой 1xx, включая 100 Trying), PDD отсекает «phantom delay» от промежуточных прокси/SBC, которые отправляют 100 Trying мгновенно. PDD = «сколько времени ждёт звонящий, пока телефон абонента зазвонит».

**Отличие от TTR:**
- **TTR** (`sip_exporter_ttr`) = INVITE → первый 1xx (включая 100 Trying). Задержка до первого хопа.
- **PDD** = INVITE → первый 180/183. Задержка до дальнего конца (callee ringing).
- Разница может быть от миллисекунд до секунд. 100 Trying — это ACK от первого попавшегося SIP-элемента, 180/183 — ответ от дальнего конца.

**Реализация:** расширить существующий `inviteEntry` флагом `pddMeasured bool`. В `handleInviteResponse()` при получении 180/183, если `pddMeasured == false`, измерить задержку и установить флаг. Не требует нового трекера — переиспользует `inviteTracker`. Histogram `sip_exporter_pdd` с бакетами `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` ms. Labels: `carrier`, `ua_type`.

**Формула:**
```
PDD = Time of first 180/183 response - Time of INVITE request
```

**Почему P0:** запрошено пользователем. PDD — стандартная метрика в телекоме, используется NOC-командами для отслеживания качества звонков. Естественно расширяет текущую архитектуру с `inviteTracker`.

---

### ~~P0: TTR — Time to First Response~~ ✅ Реализовано (v0.10.0)

**Описание:** время от INVITE до первого provisional response (100/180/183). Не определена в RFC 6076, но является полезной операционной метрикой.

**Зачем:** позволяет отслеживать задержки на стороне сервера (медленные SIP-прокси). Непосредственно влияет на пользовательский опыт — «сколько времени сервер думает перед ответом».

**Реализация:** отдельный `inviteTracker` map (по аналогии с `registerTracker`), замер при получении первого 1xx ответа на INVITE. Histogram `sip_exporter_ttr` с бакетами `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` ms.

**Формула:**
```
TTR = Average(Time of first 1xx response - Time of INVITE request)
```

**Почему P0:** естественно расширяет текущую архитектуру с `dialogEntry`, даёт операционную видимость.

---

### ~~P1: SDC — Session Duration Counter~~ ✅ Реализовано

**Описание:** общее количество завершённых сессий (BYE 200 OK). Не определена в RFC 6076, но является полезным дополнением.

**Зачем:** простой счётчик, дополняет SPD для понимания «сколько сессий в среднем за период». Позволяет строить rate-графики (`rate(sip_exporter_sdc_total[5m])`).

**Реализация:** `prometheus.Counter` `sip_exporter_sdc_total` с инкрементом в `SessionCompleted()` (вызывается из `handleBye200OK` и `sipDialogMetricsUpdate`). Unit-тесты + e2e (`sdc_test.go`).

---

### ~~P1: ASR — Answer Seizure Ratio~~ ✅ Реализовано

**Описание:** классика телефонии — ratio ответов (200 OK на INVITE) к общему числу INVITE.

**Формула:**
```
ASR = (INVITE → 200 OK) / Total INVITE × 100
```

**Отличие от SER:** не исключает 3xx из знаменателя. Проще интерпретировать, стандартная метрика в телекоме (ITU-T E.411).

**Реализация:** `prometheus.GaugeFunc` `sip_exporter_asr`, unit-тесты + e2e (`asr_test.go`).

---

### ~~P2: NER — Network Effectiveness Ratio~~ ✅ Реализовано (v0.10.0)

**Описание:** NER = 100 − ISA, показывает долю вызовов без сетевых проблем. GSMA IR.42.

**Реализация:** `prometheus.GaugeFunc` `sip_exporter_ner`, unit-тесты + e2e (`ner_test.go`).

---

### ~~P3: LRD — Location Registration Delay~~ ✅ Реализовано (v0.10.0)

**Описание:** задержка между REGISTER и 3xx redirect. Histogram `sip_exporter_lrd` с бакетами `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` ms.

**Реализация:** переиспользует `registerTracker` (аналог RRD), измеряет REGISTER→3xx. E2e тесты с SIPp redirect-сценариями (`reg_uas_redirect.xml`, `reg_uac_redirect.xml`).

---

### ~~P3: ISS — Ineffective Session Severity~~ ✅ Реализовано (v0.10.0)

**Описание:** счётчик INVITE→408/500/503/504 ответов. `prometheus.Counter` `sip_exporter_iss_total`.

**Реализация:** инкремент в `isIneffectiveResponse()` (переиспользует коды ISA). Полезен для alerting на абсолютный объём ошибок.

---

## Предлагаемые продуктыые фичи

### ~~P0: Per-IP / Per-Carrier метрики (дифференциатор)~~ ✅ Реализовано (v0.11.0)

**Реализация:** `carrier` label через CIDR-маппинг на всех SIP-метриках. Конфигурация через `SIP_EXPORTER_CARRIERS_CONFIG`. Полная документация в README и docs/METRICS.md.

---

### ~~P0: Histogram вместо среднего для SPD/RRD~~ ✅ Реализовано (v0.10.0)

**Проблема:** SPD и RRD — средние значения за всё время работы. Если первые 100 звонков были по 1 секунде, а потом пошли по 30 минут — среднее долго не отреагирует. Бесполезно для alerting.

**Решение:** заменить `prometheus.GaugeFunc` на `prometheus.Histogram` с бакетами.

**RRD бакеты (ms):**
```
[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]
```

**SPD бакеты (sec):**
```
[1, 5, 10, 30, 60, 300, 600, 1800, 3600]
```

**Преимущества:**
- Prometheus автоматически даст p50, p90, p99 через `histogram_quantile()`
- Индустриальный стандарт для latency-метрик
- Можно строить SLO на основе percentile-based alerting

**PromQL примеры:**
```promql
# 95-й перцентиль задержки регистрации
histogram_quantile(0.95, rate(sip_exporter_rrd_bucket[5m]))

# 99-й перцентиль длительности сессии
histogram_quantile(0.99, rate(sip_exporter_spd_bucket[5m]))
```

**Обратная совместимость:** оставить старые GaugeFunc как `sip_exporter_spd_average` / `sip_exporter_rrd_average` на переходный период.

---

---

### ~~P1: Health endpoint + Graceful Shutdown~~ ✅ Реализовано (v0.10.0)

**Описание:** `/health` endpoint для Kubernetes probes + `IsAlive()` на Exporter.

**Реализация:**
- `GET /health` → 200 OK (экспортер инициализирован) / 503 Service Unavailable
- `Exporter.IsAlive()` через `atomic.Bool`
- Graceful shutdown уже был реализован (SIGTERM → `exporter.Close()` → `http.Server.Shutdown(10s)`)

**Kubernetes probes:**
```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 2112
readinessProbe:
  httpGet:
    path: /health
    port: 2112
```

---

### ~~P2: SIP OPTIONS Ping Monitoring~~ ✅ Реализовано (v0.10.0)

**Проблема:** SIP trunk-провайдеры используют OPTIONS ping для проверки alive-статуса. Мониторинг ответов на OPTIONS — критичная фича для NOC-команд.

**Реализация:** `sip_exporter_ord` histogram — задержка от OPTIONS до любого ответа. `optionsTracker` map по Call-ID с TTL cleanup (60s). Аналогична RRD, но для OPTIONS. E2e тесты + unit-тесты.

---

## Новые фичи — анализ конкурентного преимущества

### Конкурентный ландшафт

| Проект | Звёзды | Подход | Слабости |
|--------|--------|--------|----------|
| HOMER (sipcapture/homer) | 1.9k | Полная платформа захвата (HEP), UI, ClickHouse | Тяжёлый стек, не Prometheus-native |
| kamailio_exporter | 62 | BINRPC к Kamailio | Только Kamailio, нужен прямой доступ |
| KRAM-PRO/SIP_Exporter | 2 | OPTIONS ping (Python) | Примитивный скрипт |

**Текущая позиция sip-exporter:** технический лидер в нише «passive SIP Prometheus exporter». Единственный eBPF-based, самая полная реализация RFC 6076, лучший test coverage. Для перехода от «лучший экспортер» к «must-have инструмент» нужны продуктовые фичи.

---

### Tier 1: Enterprise Must-Have

#### ~~P0: Per-IP / Per-Carrier метрики~~ ✅ Реализовано (v0.11.0)

**Реализация:** `carrier` label через CIDR-маппинг. Подробности выше.

---

### Tier 2: Growth Features

#### ~~P1: RFC 6035 Voice Quality Reporting (vq-rtcpxr)~~ ✅ Реализовано (v0.14.0)

**Описание:** парсинг SIP PUBLISH/NOTIFY с `Content-Type: application/vq-rtcpxr` для получения метрик качества голоса из RTCP Extended Reports (RFC 3611), передаваемых через SIP signaling.

**RFC 6035** определяет SIP event package `vq-rtcpxr` для передачи VoIP quality-метрик от User Agent (REPORTER) к collection server (COLLECTOR). SIP-устройства (Yealink, Grandstream, Cisco, Kamailio, Asterisk) отправляют отчёты о качестве голоса в теле SIP PUBLISH или NOTIFY сообщений по окончании сессии или при деградации качества.

**Зачем:** вместо прямого захвата RTCP-пакетов (что требует отдельного eBPF-фильтра, SDP-парсинга и port correlation), RFC 6035 позволяет получить те же метрики **из SIP signaling** — данные уже приезжают в SIP-пакетах, которые sip-exporter и так захватывает. Никаких изменений в eBPF-коде не требуется.

**Метрики (из RFC 6035 §4.6.1 ABNF):**

**Session Description:**
- `PayloadType` (PT) — codec type
- `SampleRate` (SR) — sample rate (Hz)
- `FrameDuration` (FD) — frame duration (ms)

**Jitter Buffer:**
- `JitterBufferAdaptive` (JBA) — adaptive/static/unknown
- `JitterBufferNominal` (JBN) — nominal jitter buffer size (ms)
- `JitterBufferMax` (JBM) — max jitter buffer size (ms)

**Packet Loss:**
- `NetworkPacketLossRate` (NLR) — network packet loss (%)
- `JitterBufferDiscardRate` (JDR) — jitter buffer discard rate (%)

**Burst/Gap Loss:**
- `BurstLossDensity` (BLD) — burst loss density (%)
- `BurstDuration` (BD) — burst duration (ms)
- `GapLossDensity` (GLD) — gap loss density (%)
- `GapDuration` (GD) — gap duration (ms)

**Delay:**
- `RoundTripDelay` (RTD) — round trip delay (ms)
- `EndSystemDelay` (ESD) — end system delay (ms)
- `OneWayDelay` (OWD) — one way delay (ms)
- `SymmOneWayDelay` (SOWD) — symmetric one way delay (ms)
- `InterarrivalJitter` (IAJ) — interarrival jitter (ms, RFC 3550)
- `MeanAbsoluteJitter` (MAJ) — mean absolute jitter (ms, ITU-T G.1020 MAPDV)

**Signal:**
- `SignalLevel` (SL) — signal level (dB)
- `NoiseLevel` (NL) — noise level (dB)
- `ResidualEchoReturnLoss` (RERL) — residual echo return loss (dB)

**Quality Estimates:**
- `ListeningQualityR` (RLQ) — R-factor listening quality (0-120)
- `ConversationalQualityR` (RCQ) — R-factor conversational quality (0-120)
- `MOS-LQ` (MOSLQ) — MOS listening quality (1.0-4.9)
- `MOS-CQ` (MOSCQ) — MOS conversational quality (1.0-4.9)
- `ExternalR-In` (EXTRI) — external R-factor inbound (0-120)
- `ExternalR-Out` (EXTRO) — external R-factor outbound (0-120)

**Prometheus метрики:**

```promql
sip_exporter_vq_rtd_ms{carrier="...",ua_type="..."}              # Round Trip Delay histogram
sip_exporter_vq_nlr_percent{carrier="...",ua_type="..."}         # Network Packet Loss Rate histogram
sip_exporter_vq_jdr_percent{carrier="...",ua_type="..."}         # Jitter Buffer Discard Rate histogram
sip_exporter_vq_iaj_ms{carrier="...",ua_type="..."}              # Interarrival Jitter histogram
sip_exporter_vq_maj_ms{carrier="...",ua_type="..."}              # Mean Absolute Jitter histogram
sip_exporter_vq_mos_lq{carrier="...",ua_type="..."}              # MOS Listening Quality histogram
sip_exporter_vq_mos_cq{carrier="...",ua_type="..."}              # MOS Conversational Quality histogram
sip_exporter_vq_rlq{carrier="...",ua_type="..."}                 # R-factor Listening Quality histogram
sip_exporter_vq_rcq{carrier="...",ua_type="..."}                 # R-factor Conversational Quality histogram
sip_exporter_vq_rerl_db{carrier="...",ua_type="..."}             # Residual Echo Return Loss histogram
sip_exporter_vq_reports_total{carrier="...",ua_type="...",type="session|interval|alert"}  # Total reports counter
sip_exporter_vq_esd_ms{carrier="...",ua_type="..."}              # End System Delay histogram
sip_exporter_vq_bld_percent{carrier="...",ua_type="..."}         # Burst Loss Density histogram
sip_exporter_vq_gld_percent{carrier="...",ua_type="..."}         # Gap Loss Density histogram
```

**Архитектура:**

1. **Обнаружение vq-rtcpxr:** при парсинге SIP-пакета определять `Content-Type: application/vq-rtcpxr` в PUBLISH/NOTIFY
2. **Парсинг тела:** parse `VQSessionReport` / `VQIntervalReport` / `VQAlertReport` по ABNF из RFC 6035 §4.6.1
3. **Correlation:** использовать `CallID` из отчёта для привязки к carrier/ua_type через tracker (или fallback на IP из `LocalAddr`/`RemoteAddr`)
4. **Экспорт:** histograms + counters для каждой метрики, с `carrier` и `ua_type` labels

**Ключевое преимущество перед прямым RTCP capture:**
- **Не требует изменений eBPF** — данные уже в SIP-пакетах
- **Не требует SDP-парсинга** — correlation через Call-ID
- **Не требует отдельного RTP port range** — всё через SIP signaling
- **Поддерживается оборудованием** — Yealink, Grandstream, Cisco, Kamailio, Asterisk, FreeSWITCH отправляют vq-rtcpxr отчёты

**Поддержка устройств:**

| Device/Server | RFC 6035 Support | Notes |
|---------------|-----------------|-------|
| Yealink SIP-T4xS | Да | Включается в web UI: Features > SIP > RTP |
| Grandstream GXP21xx | Да | В settings: SIP > Advanced > RTCP-XR |
| Cisco SPA5xx | Да | Via admin XML config |
| Kamailio | Да | Модуль `rtcpaxreport` |
| Asterisk | Да | `res_rtp_asterisk` + `cdr_csv` |
| FreeSWITCH | Да | mod_sofia + verto |

**Сложность:** средняя. Парсинг текстового формата ABNF (не бинарный RTCP). Основная работа — в SIP-парсере и новых Prometheus метриках.

**Почему P1:** голосовое качество — это то, за что платят деньги. RFC 6035 — стандартный механизм, поддерживаемый производителями оборудования. В отличие от самостоятельного RTCP-capture (требующего eBPF-изменений), RFC 6035 работает поверх существующего SIP-capture инфраструктуры.

---



---

#### P2: Self-Monitoring Metrics

**Проблема:** оператор не знает, работает ли экспортер корректно. Нет видимости внутренних проблем (ring buffer overflow, parse errors, channel congestion).

**Решение:**

```promql
sip_exporter_uptime_seconds                              # время работы (gauge)
sip_exporter_packets_captured_total                      # захвачено пакетов (counter)
sip_exporter_packets_dropped_total                       # потеряно на ring buffer (counter)
sip_exporter_parse_errors_total                          # невалидные SIP-пакеты (counter)
sip_exporter_channel_capacity_ratio                      # заполненность messages channel (gauge, 0-1)
sip_exporter_ebpf_events_lost_total                      # потеряно на eBPF level (counter)
sip_exporter_active_trackers{type="register|invite|options"}  # размер tracker maps (gauge)
sip_exporter_active_dialogs                               # alias для sessions (gauge)
sip_exporter_config_reload_total                          # количество hot-reload (counter)
```

Go runtime metrics через стандартный `promhttp.Handler()`:
```go
mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
```

**Сложность:** низкая. Большинство данных уже есть в структуре `exporter`.

**Почему P2:** без self-monitoring невозможно отличить «трафик упал» от «экспортер сломался».

---

### В разработке (v0.15)

| Приоритет | Фича | Статус |
|-----------|------|--------|
| ~~P2~~ | Self-Monitoring Metrics (7 метрик) | ✅ Закоммичено (`6975848`) |

**TODO (завтра): стабилизация e2e тестов**
- **E2E loopback flakiness (~33% FAIL rate)** — AF_PACKET на loopback теряет ±1-2 пакета из 200 на suite из 50+ тестов
- Root cause: kernel AF_PACKET packet delivery timing на `lo` при 30+ последовательных контейнерах
- Проявляется: `SER=102` (2 INVITE потеряны), `SER=69.31` (1 лишний), `SEER=99.01` (1 response потерян)
- Pre-existing — на чистом `develop` тоже падают 4 PDD теста
- Варианты: (1) `require.InDelta ±1%` для ratio-метрик, (2) veth pairs вместо `lo`, (3) network namespace per test

---

### Tier 3: Completeness Features

#### ~~P2: SIP User-Agent Classification~~ ✅ Реализовано (v0.13.0)

**Описание:** классификация SIP-устройств по заголовку `User-Agent` с regex-паттернами из YAML-конфигурации. Лейбл `ua_type` на всех метриках.

**Реализация:**
- Парсинг `User-Agent` заголовка в `sipPacketParse()` — поле `UserAgent []byte` в `dto.Packet`
- `internal/ua` пакет: regex-based классификатор, first-match-wins, YAML конфигурация
- `ua_type` label dimension на всех SIP-метриках (requests, responses, RFC 6076 ratios, histograms, sessions)
- Tracker propagation: UA type определяется при запросе, наследуется ответами через tracker, сохраняется в dialog entries
- Composite key `carrier+"\x00"+ua_type` в sync.Map для ratio collectors
- Конфигурация: `SIP_EXPORTER_USER_AGENTS_CONFIG` → YAML с regex-паттернами
- E2E тесты: 8 UA-тестов (Yealink, Grandstream, multiple types, no-UA, no-config, SDC, rated metrics, combined carrier+ua_type)
- Нагрузочный тест: `TestLoad_DualUAType` — 1 carrier + 2 ua_types, CPU/RAM overhead validation
- Grafana dashboard: `ua_type` template variable, обновлённые легенды и запросы

**Результат:**
```
sip_exporter_invite_total{carrier="mobile-operator-a",ua_type="yealink"}      1523
sip_exporter_ser{carrier="mobile-operator-a",ua_type="yealink"}                95.2
sip_exporter_ser{carrier="mobile-operator-a",ua_type="grandstream"}            87.4
```

---

## Приоритет реализации (обновлённый)

### Реализованные фичи

| Приоритет | Фича | Версия |
|-----------|------|--------|
| ~~P0~~ | Histogram для SPD/RRD | ✅ v0.10.0 |
| ~~P0~~ | TTR (Time to First Response) | ✅ v0.10.0 |
| ~~P1~~ | Health + graceful shutdown | ✅ v0.10.0 |
| ~~P1~~ | SDC (Session Duration Counter) | ✅ v0.9.0 |
| ~~P1~~ | ASR (Answer Seizure Ratio) | ✅ v0.9.0 |
| ~~P2~~ | NER (Network Effectiveness Ratio) | ✅ v0.10.0 |
| ~~P2~~ | SIP OPTIONS monitoring (ORD) | ✅ v0.10.0 |
| ~~P3~~ | ISS (Ineffective Session Severity) | ✅ v0.10.0 |
| ~~P3~~ | LRD (Location Registration Delay) | ✅ v0.10.0 |
| ~~P0~~ | Per-Carrier Labels | ✅ v0.11.0 |
| ~~P3~~ | 10 новых status code counters (181, 182, 405, 481, 487, 488, 501, 502, 604, 606) | ✅ v0.12.0 |
| ~~P3~~ | CANCEL request: inviteTracker cleanup | ✅ v0.12.0 |
| ~~—~~ | E2E test optimization (shared exporter + container restart) | ✅ v0.12.0 |
| ~~P2~~ | SIP User-Agent Classification (ua_type label) | ✅ v0.13.0 |
| ~~P1~~ | RFC 6035 Voice Quality Reporting (vq-rtcpxr) | ✅ v0.14.0 |

### Roadmap по версиям

| Версия | Фичи | Обоснование |
|--------|-------|-------------|
| ~~**v0.11**~~ | ~~Per-Carrier Labels + Self-Monitoring~~ | ✅ Выполнено |
| ~~**v0.12**~~ | ~~10 status code counters + CANCEL handler + e2e optimization~~ | ✅ Выполнено |
| ~~**v0.13**~~ | ~~SIP User-Agent Classification~~ | ✅ Выполнено |
| ~~**v0.14**~~ | ~~RFC 6035 Voice Quality Reporting (vq-rtcpxr)~~ | ✅ Выполнено |
| **v0.15** | PDD (Post-Dial Delay) histogram | Индустриальный стандарт — INVITE → первый 180/183, запрошено пользователем |

### Итог

Для перехода от «лучший экспортер» к «must-have инструмент №1» критичны 3 вещи:

1. ~~**Per-carrier labels**~~ ✅ — оператор должен видеть каждый транк отдельно
2. ~~**RTP/RTCP quality metrics via RFC 6035**~~ ✅ — голосовое качество из SIP signaling, без изменений eBPF
3. ~~**PDD (Post-Dial Delay)**~~ ✅ — индустриальный стандарт, запрошено пользователем

---

## Дистрибуция — роадмап распространения

### Текущее состояние

- GitHub: 10 звёзд, 0 фолловеров
- Docker Hub: `frzq/sip-exporter:latest`
- Документация: README + docs/ (хорошее состояние)
- Нет: блога, social media, conference talks, community presence
- Целевая аудитория: VoIP-операторы, NOC-инженеры, DevOps/SRE в телекоме, Kamailio/OpenSIPS/Asterisk администраторы

### Целевые метрики

| Метрика | Текущее | +3 мес | +6 мес | +12 мес |
|---------|---------|--------|--------|---------|
| GitHub Stars | 10 | 100 | 300 | 1000 |
| Docker Hub pulls | — | 500+ | 2K+ | 10K+ |
| Known production users | 0 | 3-5 | 10+ | 30+ |
| Внешние contributions | 0 | 1-2 | 5+ | 15+ |
| Articles/talks mentioning | 0 | 3-5 | 10+ | 20+ |

---

### Фаза 1: Фундамент (Недели 1-2)

Цель: подготовить все точки касания к привлечению пользователей.

#### 1.1 GitHub Profile Package

- [ ] **README.md** — добавить badge: Docker Pulls, GitHub Releases, Go Reference
- [ ] **Screenshots/GIF** — добавить анимацию в README: Grafana dashboard в действии (SER gauge, traffic graph)
- [ ] **CONTRIBUTING.md** — правила контрибуции (как запускать тесты, стиль кода, PR process)
- [ ] **GitHub Topics** — добавить: `sip`, `voip`, `ebpf`, `prometheus-exporter`, `telecom`, `monitoring`, `sip-monitoring`, `voip-monitoring`, `kamailio`, `opensips`
- [ ] **GitHub Releases** — опубликовать v0.10.0 release с бинарниками (linux/amd64, linux/arm64) через GoReleaser + GitHub Actions
- [ ] **GitHub Discussions** — включить, создать категории: Q&A, Ideas, Show & Tell
- [ ] **Repo description** — убедиться что SEO-friendly: «High-performance eBPF-based SIP/VoIP monitoring exporter for Prometheus»

#### 1.2 Docker Hub

- [ ] **Automated builds** — GitHub Actions → Docker Hub push на каждый tag
- [ ] **Multi-arch** — amd64 + arm64 (телеком железо часто ARM)
- [ ] **Tags strategy** — `latest`, `0.10.0`, `0.10`, `0` (semver ranges)
- [ ] **Description** — заполнить overview на Docker Hub с примером docker-compose

#### 1.3 Документация

- [ ] **Quick Start Guide** — 5-минутный туториал: `docker compose up` → Grafana → видеть метрики (можно как отдельный docs/QUICKSTART.md)
- [ ] **Comparison table** — docs/COMPARISON.md: sip-exporter vs HOMER vs kamailio_exporter vs KRAM-PRO/SIP_Exporter
- [ ] **Architecture diagram** — визуальная схема (Mermaid или ASCII) в README/документации
- [ ] **Use Cases page** — docs/USE_CASES.md: «SIP trunk monitoring», «carrier SLA», «NOC dashboard», «debugging call quality»

---

### Фаза 2: Подготовка аккаунтов (Недели 3-4)

Цель: создать и «прогреть» аккаунты на целевых площадках. Нельзя прийти с нуля и начать постить свои проекты — удалят или проигнорируют.

#### 2.1 Реалии площадок — барьеры входа

| Площадка | Барьер | Что нужно ДО любого поста | Время подготовки |
|----------|--------|---------------------------|------------------|
| **Hacker News** | Низкий. Show HN специально для своих проектов. Нужен аккаунт >1 дня. | Зарегистрироваться. 1-2 дня просто почитать/проголосовать. Понять стиль «Show HN». | 2-3 дня |
| **Reddit** | Высокий. Karma-требования, self-promotion rules (10:1 ratio), AutoModerator. | Зарегистрировать аккаунт. Нарастить ~100-200 karma: отвечать на вопросы в r/voip, r/selfhosted, r/devops, r/kubernetes. Помогать людям. | 2-4 недели |
| **Twitter/X** | Средний. 0 followers = 0 охват. Алгоритм не показывает новые аккаунты. | Создать профиль. 2-4 недели писать полезные треды про VoIP/eBPF/Prometheus, комментировать чужие, нарастить 200-500 followers. | 3-6 недель |
| **LinkedIn** | Средний. Зависит от числа connections. | Расширить сеть: подключиться к VoIP/SRE/DevOps людям. Писать «insights» и «posts» (не просто ссылки). | 2-4 недели |
| **Dev.to** | Низкий. Любой может публиковать. Доброжелательны к self-promotion. | Зарегистрироваться. Понять формат (практические туториалы, не PR-посты). | 1-2 дня |
| **Medium** | Низкий. Любой может публиковать. | Зарегистрироваться. Писать в publications (Better Programming, Towards DevOps) для большего охвата. | 1-2 дня |
| **Habr** | Средний. Для публикации в хабах нужна карма ≥5 или заявка в песочницу. | Зарегистрироваться. Написать 1-2 статьи в песочницу (хабы: Linux, Сетевые технологии). Комментировать чужие статьи. | 2-4 недели |
| **Telegram группы** | Низкий. Любой может писать. | Вступить в группы (@voip_dev, @sip_ru, @telecom_ru, @ebpf_ru, @prometheus_ru). Неделю пообщаться, помочь людям, затем аккуратно упомянуть проект. | 1 неделя |
| **Mailing lists** | Низкий. Подписался — пишешь. | Подписаться на sr-users@lists.kamailio.org и аналогичные. Понять tone и format (технический, без маркетинга). | Несколько дней |

#### 2.2 План подготовки аккаунтов

**Неделя 3:**

| День | Действие |
|------|----------|
| Пн | Зарегистрировать аккаунты: Hacker News, Reddit, Dev.to, Medium, Twitter/X |
| Вт | Reddit: подписаться на r/voip, r/selfhosted, r/devops, r/kubernetes, r/PrometheusMonitoring. Ответить на 5-10 вопросов. |
| Ср | Twitter/X: follow 50-100 людей (VoIP, eBPF, Prometheus, Go). Ответить на 5-10 тредов содержательно. |
| Чт | LinkedIn: подключиться к 50 людям (Kamailio, OpenSIPS, Asterisk, Prometheus, eBPF — поиск по ключевым словам). |
| Пт | Dev.to: написать первую статью (не про sip-exporter, а образовательную — «Understanding RFC 6076 SIP Metrics»). Это прогрев + karma. |
| Сб | Вступить в Telegram группы (@voip_dev, @sip_ru, @prometheus_ru, @ebpf_ru, @k8s_ru). Начать общаться. |
| Вс | Подписаться на mailing lists: Kamailio (sr-users), OpenSIPS, FreeSWITCH. Прочитать последние 20 писем для понимания тона. |

**Неделя 4:**

| День | Действие |
|------|----------|
| Пн-Ср | Reddit: продолжать наращивать karma. Писать полезные комментарии (10-20 в день). Цель: ~100 karma. |
| Чт | Habr: написать статью в песочницу («Мониторинг SIP в 2026: что доступно open-source»). |
| Пт | Twitter/X: опубликовать образовательный тред (не про свой проект, а про RFC 6076 или eBPF). |
| Сб | LinkedIn: опубликовать insight post (не ссылку, а мнение/наблюдение про VoIP мониторинг). |
| Вс | Проверить все аккаунты: karma/followers/connections готовы? Если нет — ещё неделя прогрева. |

#### 2.3 Критерии готовности к launch

Не переходить к Фазе 3 пока:

- [ ] Reddit аккаунт: karma ≥ 50, возраст ≥ 2 недели
- [ ] Twitter/X: ≥ 100 followers, 5-10 опубликованных тредов
- [ ] LinkedIn: ≥ 50 connections в VoIP/DevOps/SRE
- [ ] Dev.to: ≥ 1 опубликованная статья (не про sip-exporter)
- [ ] Habr: карма ≥ 5 (или статья прошла модерацию из песочницы)
- [ ] Telegram: активно общаетесь в 3+ группах минимум неделю

---

### Фаза 3: Launch (Недели 5-6)

Цель: максимальный охват за 48 часов. Постить только когда аккаунты прогреты.

#### 3.1 Порядок публикации (важно!)

Запуск не одновременный, а каскадный — каждая площадка имеет свой «пиковый час»:

| День | Время UTC | Площадка | Контент | Почему этот порядок |
|------|-----------|----------|---------|---------------------|
| День 1, утро | 08:00-10:00 | **Dev.to** | Полная статья (2000+ слов): «sip-exporter: eBPF-based SIP monitoring for Prometheus» | Dev.to толерантен к self-promo, статья индексируется Google, будет ссылкой для остальных постов |
| День 1, утро | 10:00-12:00 | **Hacker News** | «Show HN: sip-exporter — eBPF-based SIP monitoring with Prometheus (github.com/aibudaevv/sip-exporter)» | Show HN — формат для своих проектов. Ссылка на Dev.to или GitHub. Пиковый трафик HN — US утро. |
| День 1, день | 12:00-14:00 | **Reddit r/selfhosted** | Пост с описанием + docker-compose пример | r/selfhosted самый толерантный к self-promo. Обязательно с практическим примером. |
| День 1, день | 14:00-16:00 | **Reddit r/voip** | Пост: «Open-source passive SIP monitoring with RFC 6076 metrics» | Прямая аудитория. Акцент на техническую глубину, не маркетинг. |
| День 1, вечер | 16:00-18:00 | **Twitter/X** | Thread (10-15 твитов): «I built a SIP monitoring tool using eBPF...» | Ссылки на GitHub + Dev.to. Хештеги: #eBPF #Prometheus #VoIP #SIP #Golang |
| День 2, утро | 08:00-10:00 | **LinkedIn** | Статья/Post: профессиональный рассказ про eBPF + VoIP | LinkedIn algorithm любит «insight» формат — не «look at my project», а «here's what I learned building...» |
| День 2, день | 10:00-12:00 | **Reddit r/devops** + **r/PrometheusMonitoring** | Cross-post с адаптацией под каждую аудиторию | r/devops: акцент на Prometheus + eBPF. r/PrometheusMonitoring: акцент на RFC 6076 metrics |
| День 2, вечер | 16:00-18:00 | **Medium** | Cross-post Dev.to статьи (canonical URL на Dev.to) | SEO: Medium хорошо индексируется |
| День 3 | В течение дня | **Telegram группы** | Написать в @voip_dev, @sip_ru, @telecom_ru — но от первого лица, как «смотрите что сделал, буду рад фидбеку» | Не как реклама, а как «коллеги, сделал инструмент для нашей сферы» |
| День 3-4 | В течение дня | **Mailing lists** | Kamailio sr-users, OpenSIPS users — email с описанием проблемы и решения | Технический tone, без маркетинга. «I needed X, couldn't find it, built Y.» |

#### 3.2 Шаблоны контента

**Hacker News (Show HN) — формат:**

```
Show HN: sip-exporter – Passive SIP monitoring with eBPF for Prometheus

I built this because I couldn't find a lightweight SIP traffic monitor 
that works with Prometheus without running a full HOMER stack or needing 
direct access to Kamailio internals.

It uses eBPF socket filters to capture SIP packets at kernel level — 
zero packet loss at 2,000 CPS (~24K PPS), <15% CPU, ~15MB RAM.

Implements full RFC 6076 metrics: SER, SEER, ISA, SCR, RRD, SPD + 
extended metrics (TTR, NER, ORD, LRD, ASR). Single Docker container, 
no dependencies.

55 E2E tests with real SIP traffic (SIPp + testcontainers-go).

Would love feedback from anyone running SIP infrastructure.
```

**Reddit — важно: не копировать HN текст. Reddit ненавидит копипасту.**

r/selfhosted (практический акцент):
```
Заголовок: I made a SIP monitoring tool that runs in Docker and 
outputs Prometheus metrics

Текст: [docker-compose пример] [скриншот Grafana] [2-3 предложения 
о проблеме/решении] [ссылка на GitHub]
```

r/voip (технический акцент):
```
Заголовок: Open-source passive SIP traffic monitor with RFC 6076 
metrics (SER, SEER, ISA, SCR)

Текст: [описание RFC 6076 реализации] [какие метрики] 
[архитектура: eBPF → AF_PACKET → Go] [ссылка]
```

**Twitter/X thread:**

```
1/ I built a SIP monitoring tool using eBPF 🧵

Problem: VoIP operators need SER, SEER, ISA metrics per RFC 6076. 
Existing options = full HOMER stack (heavy) or kamailio_exporter 
(Kamailio-only).

2/ Solution: passive packet capture at kernel level. 
No agent on the SIP server. No module changes.
Just point at a network interface.

3/ Architecture:
SIP Traffic → NIC → eBPF socket filter → ringbuf → Go → Prometheus

Zero-copy kernel→userspace. No tcpdump overhead.

4/ Numbers from load testing:
✅ 2,000 CPS (24K PPS) — zero packet loss
✅ <15% CPU, ~15MB RAM  
✅ GC pauses <1ms (400x smaller than socket buffer)

5/ Full RFC 6076 implementation:
SER (Session Establishment Ratio)
SEER (Session Establishment Effectiveness Ratio)
ISA (Ineffective Session Attempts)
SCR (Session Completion Ratio)
RRD, SPD, TTR — histograms with percentiles

6/ 55 end-to-end tests with real SIP traffic via SIPp.
8 load tests benchmarking PPS, memory, GC pauses.

Open source, AGPL-3.0:
github.com/aibudaevv/sip-exporter
```

#### 3.3 Что делать ПОСЛЕ launch

Launch — это 20% работы. 80% — это то, что происходит после:

- [ ] **Отвечать на каждый комментарий** на HN/Reddit в первые 24-48 часов. Это определяет ранг поста.
- [ ] **HN:** быть готовым к вопросам про eBPF verifer, AF_PACKET vs XDP, why Go not Rust. Техническая глубина = уважение.
- [ ] **Reddit:** следить за upvote/downvote ratio. Если пост уходит в минус — проанализировать почему (обычно: слишком промо, мало практической ценности).
- [ ] **Twitter/X:** отвечать на каждый reply и retweet. Это повышает видимость в алгоритме.
- [ ] **Collect feedback:** записывать все вопросы и замечания — это input для будущих статей и фич.

---

### Фаза 4: Контент-маркетинг (Месяцы 2-3)

Цель: SEO-позиционирование + репутация эксперта. Каждый материал = постоянный источник трафика.

#### 4.1 Технические статьи (на английском)

| Приоритет | Статья | Площадка | Хук | Барьер |
|-----------|--------|----------|-----|--------|
| P0 | «eBPF for SIP Monitoring: Zero-Overhead Packet Capture» | Dev.to → cross-post Medium | Уникальный eBPF use case | Нет — Dev.to открыт |
| P0 | «RFC 6076 SIP Performance Metrics in Prometheus» | Dev.to → cross-post Medium | Образовательный + SEO | Нет |
| P1 | «SIP Monitoring: HOMER vs sip-exporter vs kamailio_exporter» | Dev.to | Comparison SEO-трафик | Нет |
| P1 | «Monitoring Kamailio with Prometheus — No Module Changes Needed» | Kamailio wiki / blog | Прямая аудитория | Нужен контакт с Kamailio community |
| P2 | «Building a Grafana Dashboard for VoIP Quality» | Grafana community blog | Grafana ecosystem | Guest post — нужен pitch |
| P2 | «Benchmarking: 2000 CPS SIP Processing in Go» | Dev.to | Performance angle | Нет |
| P3 | «eBPF Socket Filters: Practical Guide for Network Monitoring» | Dev.to + Lobste.rs | Техническая глубина | Lobste.rs нужен инвайт |

#### 4.2 Контент на русском

| Приоритет | Статья | Площадка | Барьер |
|-----------|--------|----------|--------|
| P0 | «Мониторинг SIP-трафика на eBPF: SER, SEER, SCR для Grafana» | Habr (Сетевые технологии) | Карма ≥ 5 или песочница |
| P1 | «VoIP-мониторинг в Kubernetes: Prometheus + eBPF» | Habr (DevOps) | Карма ≥ 5 |
| P2 | «RFC 6076: метрики SIP-качества — от формул до Grafana» | Habr (Телефония) | Карма ≥ 5 |

#### 4.3 Визуальный контент

- [ ] **YouTube видео** (10-15 мин): «Deploy sip-exporter + Grafana in 5 minutes» — screen recording полного цикла
- [ ] **YouTube shorts / TikTok** (30-60 сек): «SIP monitoring with eBPF — zero packet loss at 2000 CPS» — Grafana dashboard в действии
- [ ] **Архитектурная схема** (SVG/Mermaid) для README и статей

#### 4.4 Stack Overflow стратегия

Не спамить ссылкой на проект. Вместо этого:
- Искать вопросы: «sip monitoring prometheus», «sip traffic grafana», «kamailio prometheus metrics», «voip monitoring tools»
- Давать развёрнутый ответ (5-10 предложений)
- В конце: «For a comprehensive solution, you might also look at sip-exporter which captures SIP traffic via eBPF...»
- Цель: ~10 качественных ответов за 2 месяца. Каждый = постоянный трафик.

---

### Фаза 5: Экосистемная интеграция (Месяцы 3-4)

Цель: попасть в «официальные» списки и каталоги. Это PR, не постинг — правила другие.

#### 5.1 Prometheus Ecosystem

- [ ] **awesome-prometheus-exporters** — PR в github.com/prometheus-community/awesome-prometheus-exporters. Формат: одна строка в markdown. Нужен meaningful description. Барьер: нужен аппрув мейнтейнера.
- [ ] **Prometheus.io exporters page** — PR в github.com/prometheus/docs. Добавить в таблицу exporters. Барьер: нужно соответствовать критериям (open-source, работает, документация).
- [ ] **Grafana dashboards marketplace** — опубликовать на grafana.com/grafana/dashboards/. Нужен аккаунт Grafana Cloud (бесплатный). Загрузить JSON через форму.
- [ ] **Artifact Hub** — опубликовать Helm chart (когда будет готов). Формат: стандартный Helm chart + annotation.

#### 5.2 VoIP Ecosystem

- [ ] **voip-info.org** — wiki-style сайт. Любой может создать страницу. Написать статью с примерами docker-compose и PromQL.
- [ ] **Kamailio wiki cookbook** — предложить рецепт через mailing list. Барьер: нужно быть частью community.
- [ ] **Asterisk wiki** — аналогично
- [ ] **FreeSWITCH wiki** — аналогично

#### 5.3 eBPF Ecosystem

- [ ] **ebpf.io/applications** — PR в github.com/ebpf-io/ebpf.io. Добавить в список applications.
- [ ] **Cilium / eBPF Slack** — зарегистрироваться на slack.cilium.io, написать в #general или #showcase
- [ ] **ebpf-forum** — опубликовать use case

#### 5.4 Package Managers

- [ ] **GoReleaser** — автоматические GitHub Releases с бинарниками для linux/amd64, linux/arm64
- [ ] **Homebrew Tap** — `brew tap aibudaevv/sip-exporter && brew install sip-exporter`
- [ ] **Nixpkgs** — предложить пакет через PR (открытый процесс)
- [ ] **Alpine packages** — предложить через aports repository

---

### Фаза 6: Conference & Community (Месяцы 5+)

Цель: репутация thought leader в пересечении VoIP + Observability.

#### 6.1 Конференции (CFP — Call for Papers)

| Конференция | Формат | Дедлайн CFP | Тема | Барьер входа |
|-------------|--------|-------------|------|-------------|
| **Kamailio World** (Berlin) | Talk (30 мин) | Дек-Янв | «SIP Monitoring with eBPF and Prometheus» | Низкий — маленькая конференция, приветствуют community talks |
| **OpenSIPS Summit** | Talk | — | Аналогично | Низкий |
| **ClueCon** (Chicago) | Talk | — | «eBPF for VoIP Monitoring» | Средний |
| **KubeCon / CloudNativeCon** | Lightning talk | За ~4 мес | «eBPF-based SIP Monitoring in Kubernetes» | Высокий — огромный конкурс, нужен убедительный abstract |
| **PromCon** | Talk | — | «RFC 6076 SIP Metrics in Prometheus» | Средний |
| **FOSDEM** (Brussels) | Talk (Networking devroom) | Сент-Окт | «eBPF Socket Filters for SIP» | Средний — нужно попасть в конкретный devroom |
| **HighLoad** (Москва) | Talk | — | «Мониторинг VoIP на eBPF: 2000 CPS без потерь» | Средний |
| **RootConf** (Москва) | Talk | — | «eBPF + Prometheus для телекома» | Низкий |

**Стратегия:** начинать с Kamailio World / OpenSIPS Summit (прямая аудитория, низкий барьер) → FOSDEM / PromCon → KubeCon (самый престижный, но и самый конкурентный).

#### 6.2 Meetups

- Kubernetes meetups (локальные) — «Monitoring legacy protocols (SIP) in cloud-native world»
- Prometheus meetups — «Building a custom Prometheus exporter with eBPF»
- VoIP/Telecom meetups — «Modern monitoring for SIP infrastructure»
- Go meetups — «eBPF + Go: High-performance network monitoring»

#### 6.3 Подкасты и интервью

- **Kubernetes Podcast** — предложить интервью (email hosts)
- **Prometheus Podcast / CNCF Podcast** — аналогично
- **Русскоязычные подкасты**: «Подкаст Слёрма», «DevOops», «Радио-Т» (попросить упомянуть)
- **Telecom-specific podcasts**: VoIP Users Conference, VUC (SIPtastic)

---

### Фаза 7: Рост и удержание (Месяцы 6+)

Цель: органический рост через community и content flywheel.

#### 7.1 SEO-стратегия

**Целевые запросы** (Google):
- «SIP monitoring prometheus» — P0
- «SIP exporter prometheus» — P0
- «RFC 6076 metrics grafana» — P1
- «kamailio monitoring prometheus» — P1
- «voip monitoring grafana» — P1
- «eBPF SIP monitoring» — P1
- «SER SEER ISA SCR prometheus» — P2
- «sip quality metrics prometheus» — P2

**Действия:**
- Каждая статья из Фазы 4 = landing page для SEO
- README на GitHub индексируется Google — убедиться что содержит ключевые слова
- docs/ страницы должны иметь правильные `<title>` и `<meta description>` (если будет website)
- Cross-link: статьи → GitHub, GitHub → статьи

#### 7.2 Community Building

- [ ] **GitHub Discussions** — активно отвечать на вопросы, создавать polls для приоритизации фич
- [ ] **Discord/Telegram группа** — живой чат для пользователей (быстрее GitHub Issues)
- [ ] **Monthly updates** — GitHub Discussions post с прогрессом (аналог changelog но человечнее)
- [ ] **Contributing guide** — упростить первый contribution (good first issues, help wanted labels)
- [ ] **User stories** — просить пользователей делиться их deploy-кейсами

#### 7.3 Partnership & Integration

- [ ] **Kamailio integration** — предложить включение в документацию Kamailio (monitoring chapter)
- [ ] **OpenSIPS integration** — аналогично
- [ ] **Grafana integration** — официальный data source plugin или dashboard bundle
- [ ] **VictoriaMetrics integration** — убедиться в совместимости, попросить mention
- [ ] **HOMER ecosystem** — позиционировать как lightweight HEP capture agent (когда HOMER/HEP фича будет готова)

#### 7.4 Аналитика

- [ ] **GitHub traffic analytics** — отслеживать clones, visitors, referrers
- [ ] **Docker Hub analytics** — отслеживать pulls по географии/OS
- [ ] **Demo instance** — поднять sip-exporter с публичным Grafana dashboard для live demo

---

### Краткий чеклист по неделям (обновлённый)

| Неделя | Задачи |
|--------|--------|
| **1** | GoReleaser + multi-arch Docker + GitHub Topics + Discussions + badges в README |
| **2** | Screenshots/GIF в README + CONTRIBUTING.md + Comparison table + Quick Start guide |
| **3** | Регистрация аккаунтов (Reddit, HN, Twitter/X, LinkedIn, Dev.to, Habr). Начать прогрев. |
| **4** | Прогрев аккаунтов: Reddit karma, Twitter followers, LinkedIn connections, Dev.to статья #0 (образовательная). |
| **5** | **LAUNCH**: Dev.to → HN → Reddit → Twitter/X. Каскадный запуск за 48 часов. |
| **6** | LinkedIn post + Medium cross-post + Telegram группы + mailing lists. Отвечать на все комментарии. |
| **7-8** | Habr статья (рус) + Stack Overflow ответы + awesome-prometheus-exporters PR |
| **9-10** | Dev.to article #2 (RFC 6076) + Grafana dashboard marketplace + voip-info.org |
| **11-12** | YouTube видео + Prometheus exporters page PR + ebpf.io + Homebrew tap |
| **Месяц 4** | Подать CFP: Kamailio World, FOSDEM, PromCon. Продолжать контент. |
| **Месяц 5-6** | Конференции + meetup talks + подкасты + community building |

---

### Ключевые принципы дистрибуции

1. **Прогрев > спам.** Аккаунт с 0 karma/0 followers, постящий свой проект, выглядит как спамер и будет проигнорирован или забанен. 2-4 недели подготовки = разница между 10 и 500 звёздами.
2. **Контент = compound interest.** Каждая статья работает 24/7/365. Приоритет: SEO-driven evergreen content на Dev.to/Medium.
3. **Вертикальные сообщества > горизонтальные.** 1 пост в Kamailio mailing list ценнее 10 постов в r/programming. VoIP-инженеры — прямая аудитория.
4. **eBPF — уникальный хук.** В мире VoIP никто не использует eBPF. Это пересечение двух комьюнити (VoIP + eBPF).
5. **«Show, don't tell».** GIF Grafana dashboard + benchmark числа > любое текстовое описание.
6. **Каскадный запуск, не одновременный.** Сначала Dev.to (нет барьеров, индексируется) → HN (утро US) → Reddit (день US) → Twitter/X → LinkedIn (следующий день). Каждый пост ссылается на предыдущий.
7. **Русскоязычный рынок недообслужен.** Habr статьи + Telegram группы = быстрый охват в СНГ телекоме.
8. **Каждый release — повод для коммуникации.** Release notes → Twitter → LinkedIn → Discussions → Reddit (с соблюдением self-promotion ratio).
9. **Отвечать на всё.** Первые 48 часов после launch поста — каждый ответ в комментариях повышает ранг и видимость. Это определяет вирусность.

---

## Commercial Funnel: sip-exporter → sip-shield

### Модель: Open-Source Funnel (как Grafana OSS → Grafana Cloud)

sip-exporter — не просто open-source проект. Это **верхняя часть воронки** для коммерческого SaaS продукта sip-shield (SIP security monitor). Модель повторяет путь Grafana, Elastic, GitLab:

```
                    sip-exporter (FREE)                    sip-shield (SaaS $29-99/мес)
                    ─────────────────                      ──────────────────────────────
                    Open-source monitoring                  Commercial security product
                    AGPL-3.0                               Freemium + Stripe billing
                    ↓                                      ↓
              1000 пользователей                         10-30 платящих клиентов
              «вижу метрики SIP»                     «вижу атаки → хочу защиту»
```

### Почему это работает

1. **60-70% code reuse.** sip-shield строится на базе sip-exporter: eBPF capture, SIP parser, dialog tracker, Prometheus metrics. Каждый баг-фикс и фича в sip-exporter автоматически улучшает sip-shield.

2. **Тёплая аудитория.** Каждый sip-exporter пользователь — потенциальный sip-shield клиент. Он уже:
   - Установил eBPF-агента на свой SIP-сервер
   - Настроил мониторинг, видит Grafana dashboard
   - Заметил странный трафик (scanner attempts, failed REGISTERs)
   - Доверяет качеству кода (видел 55 E2E тестов, load tests)
   - Привык к `frzq/` Docker images

3. **Natural upgrade path.** Пользователь не ищет «SIP security tool» — он уже видит проблему в sip-exporter метриках и хочет решение. Это не холодная продажа, а ответ на уже возникшую потребность.

### Конверсионный путь (Customer Journey)

```
Этап 1: DISCOVERY
Пользователь находит sip-exporter (Habr, HN, Reddit, Google «sip monitoring prometheus»)
↓
Этап 2: ADOPTION
docker run frzq/sip-exporter:latest → Grafana dashboard → видит свои SIP метрики
↓
Этап 3: PROBLEM AWARENESS
Grafana показывает: «47 scanner attempts за 24 часа», «high 401 rate», «unknown User-Agent»
Пользователь понимает: «на меня атакуют, а я не вижу это в реальном времени»
↓
Этап 4: SOLUTION DISCOVERY
README / docs / Grafana panel: «Need threat detection + auto-block? Try sip-shield →»
↓
Этап 5: CONVERSION
Переходит на sip-shield.cloud → Free tier → ставит frzq/sip-shield:latest (тот же Docker, новые фичи)
↓
Этап 6: MONETIZATION
Free: видит threats, понимает ценность → Pro ($29/мес): auto-block + Slack alerts + 90 дней истории
```

### Touchpoints — точки конверсии

Каждая точка касания sip-exporter должна мягко направлять к sip-shield:

#### README.md (GitHub)

```markdown
## Security Monitoring

Need real-time threat detection for your SIP infrastructure?
sip-shield adds brute-force detection, flood alerts, scanner identification,
and auto-blocking — built on the same eBPF foundation.

→ [sip-shield.cloud](https://sip-shield.cloud)
```

#### Grafana Dashboard (panel)

В Grafana dashboard добавить text panel внизу:
```
⚠ Security: Seeing unusual traffic patterns?
sip-shield adds real-time threat detection, auto-blocking, and alerting.
Learn more → sip-shield.cloud
```

#### Docker Hub Description

```
frzq/sip-exporter — SIP traffic monitoring for Prometheus
frzq/sip-shield  — SIP security monitoring with threat detection + auto-block
```

#### Все статьи (Dev.to, Medium, Habr)

В конце каждой статьи:
```
---
If you're also seeing SIP attacks on your infrastructure, check out sip-shield —
real-time threat detection built on the same eBPF foundation.
```

#### docs/QUICKSTART.md

После успешного deployment:
```
## Next Steps
- ✅ You're now monitoring SIP traffic
- 🔒 Want to detect attacks? [Install sip-shield →](https://sip-shield.cloud)
```

#### /metrics endpoint

Добавить comment в Prometheus output:
```
# HELP sip_exporter_build_info Build information
# TYPE sip_exporter_build_info gauge
sip_exporter_build_info{version="0.10.0",security="sip-shield available at https://sip-shield.cloud"} 1
```

### Метрики воронки

| Метрика | Определение | Цель (12 мес) |
|---------|-------------|---------------|
| **sip-exporter installs** | Docker pulls frzq/sip-exporter | 10K+ |
| **sip-shield landing visits** | Уникальные визиты sip-shield.cloud из sip-exporter touchpoints | 500+ |
| **Free tier signups** | Регистрации на sip-shield.cloud | 100+ |
| **Agent installs (free)** | Docker pulls frzq/sip-shield с API key | 50+ |
| **Paid conversions** | Pro/Business подписки | 10-30 |
| **Conversion rate** | paid / sip-exporter installs | 0.1-0.3% |

Ожидаемая конверсия: 10K exporter installs → 500 landing visits (5%) → 100 signups (1%) → 10-30 paid (0.1-0.3%). Это реалистично для open-source funnel (Grafana OSS → Cloud конверсия ~0.5%).

### Синхронизация release cycles

| Событие | sip-exporter | sip-shield | Комментарий |
|---------|-------------|------------|-------------|
| eBPF bug fix | v0.10.1 | cherry-pick | Один код — один фикс |
| New SIP header parsing | ~~v0.11 (UA classification)~~ v0.13 ✅ | reuse для scanner detection | sip-shield Rule 4 (Known Scanner UA) зависит от UA parsing |
| Per-carrier labels | v0.11 | reuse для per-trunk threat visibility | «какой транк атакуют?» |
| RTCP quality metrics | v0.14 | не нужен для sip-shield | Расходящиеся дороги — это нормально |

### GitHub Organization стратегия

Перенести оба проекта в GitHub organization (например, `github.com/sip-shield`):

```
github.com/sip-shield/
├── sip-exporter     (open-source, AGPL-3.0)  ← monitoring
├── sip-shield       (source-available)        ← security SaaS
├── sip-shield-agent (open-source, AGPL-3.0)  ← agent (fork of sip-exporter)
├── sip-shield-cloud (proprietary)             ← cloud backend
└── .github          (shared profiles, SECURITY.md)
```

Преимущества:
- Cross-promotion: каждый посетитель sip-exporter видит sip-shield в том же org
- Trust: «from the makers of sip-exporter (1K stars)» на landing page
- Единый brand: Docker images `sipshield/exporter`, `sipshield/agent`
- Единые GitHub Discussions для обоих продуктов

### Финансовая модель (projection)

| Метрика | Месяц 6 | Месяц 12 | Месяц 18 |
|---------|---------|----------|----------|
| sip-exporter stars | 300 | 1000 | 2000 |
| sip-exporter Docker pulls | 2K | 10K | 25K |
| sip-shield free users | 20 | 50 | 100 |
| sip-shield paid (Pro) | 3-5 ($87-145/мес) | 15 ($435/мес) | 30 ($870/мес) |
| sip-shield paid (Business) | 0 | 2 ($198/мес) | 5 ($495/мес) |
| **MRR** | **$87-145** | **$633** | **$1,365** |

При полной занятости (10-15ч/нед на sip-shield) break-even: ~$200/мес (VPS + домен + Stripe fees + Resend). Это покрывается к месяцу 6-8.

### Приоритизация с учётом commercial funnel

sip-exporter фичи приоритизируются не только по технической ценности, но и по вкладу в воронку:

| Фича | Ценность для monitoring | Ценность для sip-shield | Приоритет с учётом funnel |
|------|------------------------|------------------------|--------------------------|
| Per-carrier labels | Высокая | Высокая (per-trunk threats) | P0 — не снижать |
| ~~UA Classification~~ | ~~Средняя~~ | ~~Критическая (scanner detection Rule 4)~~ | ✅ v0.13.0 |
| Self-Monitoring | Средняя | Высокая (agent health для SaaS SLA) | P2 → P1 |
| RTCP Quality | Высокая | Низкая | P1 → P2 (сдвинуть ради UA) |
| RFC 6035 vq-rtcpxr | Высокая | Низкая | P1 — не требует eBPF-изменений |
| CDR Export | Средняя | Низкая | P2 — оставить |

### Что НЕ делать в sip-exporter (оставить для sip-shield)

- **Threat detection rules** — это core sip-shield, не sip-exporter
- **Auto-blocking (iptables/nftables)** — security feature, не monitoring
- **Cloud reporter / SaaS integration** — коммерческая фича
- **Rate limiting per IP** — security, не monitoring

sip-exporter остаётся чистым monitoring tool. sip-shield добавляет security layer поверх. Чёткое разделение = нет конфликта интересов с open-source community.
