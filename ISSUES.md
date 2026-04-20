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
| NER | — | Network Effectiveness Ratio (GSMA IR.42) | 0.10.0 |
| ISS | — | Ineffective Session Severity (counter) | 0.10.0 |
| ORD | — | OPTIONS Response Delay (histogram) | 0.10.0 |
| LRD | — | Location Registration Delay (histogram) | 0.10.0 |

---

## Предлагаемые метрики RFC 6076

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

### P0: Per-IP / Per-Carrier метрики (дифференциатор)

**Проблема:** все метрики — глобальные счётчики без labels. Оператор не может ответить на вопрос «какой carrier/транк деградирует?».

**Решение:**
- Добавить `carrier` label через конфигурируемые CIDR-группы
- Парсить source/destination IP из пакета (уже есть в L2→L3 парсере)
- Новые метрики: `sip_exporter_ser{carrier="provider-a"}`, `sip_exporter_isa{carrier="provider-b"}`

**Конфигурация:**
```yaml
carriers:
  - name: "provider-a"
    cidr: "10.0.1.0/24"
  - name: "provider-b"
    cidr: "10.0.2.0/24"
  - name: "internal"
    cidr: "192.168.0.0/16"
```

**Почему P0:** ни один open-source SIP exporter такого не делает. Это главный дифференциатор для enterprise-заказчиков.

**Сложность:** средняя. Требует:
- Изменить конфигурацию (YAML вместо env-only)
- Изменить eBPF packet parsing — извлечь source/destination IP
- Переработать `Metricser` — добавить label dimension
- Обновить Grafana dashboard

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

### P1: TCP/TLS поддержка

**Проблема:** eBPF-фильтр захватывает только UDP (`exporter.go:249`: `if ipHeader[9] != 17 // UDP`). В продакшене SIP-инфраструктура почти всегда использует TCP и TLS (SIPS port 5061).

**Решение:**
- Расширить eBPF-парсер для TCP-потоковой реассемблизации
- Использовать Content-Length header для определения границ SIP-сообщения в TCP-потоке
- Для TLS — варианты:
  1. SSL/TLS offload (расшифровка на termination point, мониторинг plaintext после него)
  2. SPAN-port подход (зеркалирование трафика на decryption point)
  3. eBPF uprobe на OpenSSL/BoringSSL для перехвата decrypted данных

**Сложность:** высокая. TCP реассемблизация — нетривиальная задача.

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

### P2: OpenTelemetry Export

**Проблема:** мир движется к OTel. Ограничение только Prometheus exposition format сужает рынок интеграции.

**Решение:**
- Добавить `SIP_EXPORTER_OTEL_ENDPOINT` env var
- Экспортировать те же метрики через OTLP protocol
- Поддержка: Datadog, New Relic, Splunk, Honeycomb, Grafana Tempo
- Библиотека `go.opentelemetry.io/otel` уже в зависимостях (indirect)

**Конфигурация:**
```yaml
SIP_EXPORTER_OTEL_ENDPOINT=otel-collector:4317
SIP_EXPORTER_OTEL_PROTOCOL=grpc  # grpc | http
```

**Сложность:** средняя.

---

### P3: Multi-interface мониторинг

**Проблема:** `SIP_EXPORTER_INTERFACE` принимает один интерфейс. На border-серверах обычно 2+ NIC (internal + external).

**Решение:**
```yaml
SIP_EXPORTER_INTERFACES=eth0,eth1
```

Отдельный eBPF socket на каждый интерфейс.

**Workaround:** запустить несколько инстансов экспортера.

**Сложность:** средняя.

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

#### P0: Per-IP / Per-Carrier метрики (дифференциатор)

**Проблема:** все метрики — глобальные счётчики без labels. Оператор не может ответить на вопрос «какой carrier/транк деградирует?».

**Решение:**
- Добавить `carrier` label через конфигурируемые CIDR-группы
- Парсить source/destination IP из пакета (уже есть в L2→L3 парсере)
- Новые метрики: `sip_exporter_ser{carrier="provider-a"}`, `sip_exporter_isa{carrier="provider-b"}`

**Конфигурация:**
```yaml
carriers:
  - name: "provider-a"
    cidr: "10.0.1.0/24"
  - name: "provider-b"
    cidr: "10.0.2.0/24"
  - name: "internal"
    cidr: "192.168.0.0/16"
```

**Почему P0:** ни один open-source SIP exporter такого не делает. Это главный дифференциатор для enterprise-заказчиков.

**Сложность:** средняя. Требует:
- Изменить конфигурацию (YAML вместо env-only)
- Изменить eBPF packet parsing — извлечь source/destination IP
- Переработать `Metricser` — добавить label dimension
- Обновить Grafana dashboard

---

#### P1: TCP/TLS поддержка

**Проблема:** eBPF-фильтр захватывает только UDP. В продакшене ~40-60% SIP трафика — TCP/TLS (SIPS port 5061).

**Решение:**
- Расширить eBPF-парсер для TCP-потоковой реассемблизации
- Использовать Content-Length header для определения границ SIP-сообщения в TCP-потоке
- Для TLS — варианты:
  1. SSL/TLS offload (расшифровка на termination point, мониторинг plaintext после него) — самый практичный
  2. SPAN-port подход (зеркалирование трафика на decryption point)
  3. eBPF uprobe на OpenSSL/BoringSSL для перехвата decrypted данных

**Сложность:** высокая. TCP реассемблизация — нетривиальная задача.

**Почему P1:** без этого продукт отсекает 40-60% production-сценариев.

---

#### P1: RTP/RTCP Quality Metrics (главный новый дифференциатор)

**Проблема:** мониторинг SIP signaling без quality-метрик — только половина картины. VoIP-оператору нужен MOS, jitter, packet loss.

**Решение:** парсить RTCP Receiver Reports (RFC 3550) параллельно с SIP:

**Метрики:**
```promql
sip_exporter_rtp_packet_loss_percent{call_id="..."}   # % потерянных RTP-пакетов
sip_exporter_rtp_jitter_ms{call_id="..."}              # джиттер в ms (histogram)
sip_exporter_rtp_mos_estimate{call_id="..."}           # синтетический MOS score (1-5)
sip_exporter_rtp_r_factor{call_id="..."}               # R-factor по ITU-T G.107
sip_exporter_rtcp_reports_total                         # количество обработанных RTCP отчетов
```

**MOS calculation:** ITU-T G.107 E-Model → R-factor → MOS:
```
R = 94.2 - Ie - Id - Is
MOS = 1 + (0.035 * R) + (7 * 10^-6 * R * (R - 60) * (100 - R))
```

Где `Ie` (equipment impairment) из таблицыcodec-specific (G.113), `Id` — delay impairment, `Is` — simultaneous impairment.

**Архитектура:**
- eBPF-фильтр: добавить RTCP (UDP, не только SIP-порты) — конфигурируемый RTP port range
- Новый интерфейс `Qualityer` с методами `UpdateJitter()`, `UpdatePacketLoss()`, `UpdateMOS()`
- Correlate RTCP с SIP dialog через SDP negotiation (порт → Call-ID mapping)
- Histogram бакеты jitter: `[0.1, 0.5, 1, 5, 10, 20, 50, 100, 200, 500]` ms
- Histogram бакеты MOS: `[1.0, 1.5, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5.0]`

**Почему P1:** HOMER делает это, но как тяжёлая платформа. Лёгкий eBPF-based RTP мониторинг — уникальное предложение на рынке. Превращает product из «SIP метрик» в «полный VoIP quality мониторинг».

**Сложность:** высокая. Требует:
- Новый eBPF-фильтр для RTCP (отдельный от SIP)
- SDP-парсинг для port→Call-ID correlation
- E-Model реализация для MOS/R-factor
- Значительное расширение тестов

---

### Tier 2: Growth Features

#### P2: OpenTelemetry Export

**Проблема:** мир движется к OTel. Ограничение только Prometheus exposition format сужает рынок интеграции.

**Решение:**
- Добавить `SIP_EXPORTER_OTEL_ENDPOINT` env var
- Экспортировать те же метрики через OTLP protocol
- Поддержка: Datadog, New Relic, Splunk, Honeycomb, Grafana Tempo
- Библиотека `go.opentelemetry.io/otel` уже в зависимостях (indirect)

**Конфигурация:**
```yaml
SIP_EXPORTER_OTEL_ENDPOINT=otel-collector:4317
SIP_EXPORTER_OTEL_PROTOCOL=grpc  # grpc | http
```

**Сложность:** средняя.

**Почему P2:** OTel = доступ к рынку enterprise observability (Datadog, New Relic, Splunk).

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

#### P2: CDR Export (Call Detail Records)

**Проблема:** каждый VoIP-оператор нуждается в CDR для биллинга и отчётности. Сейчас sip-exporter видит все данные для генерации CDR, но не экспортирует их.

**Решение:**

Формат CDR записи:
```json
{
  "call_id": "abc123@10.0.1.5",
  "from_uri": "sip:user1@example.com",
  "to_uri": "sip:user2@example.com",
  "from_tag": "abc",
  "to_tag": "xyz",
  "invite_time": "2026-04-20T10:30:00Z",
  "answer_time": "2026-04-20T10:30:02Z",
  "end_time": "2026-04-20T10:30:47Z",
  "duration_ms": 45000,
  "setup_duration_ms": 2000,
  "end_reason": "bye",
  "status_code": 200,
  "source_ip": "10.0.1.5",
  "destination_ip": "10.0.2.10",
  "carrier": "provider-a"
}
```

Каналы экспорта:
- **Файл:** JSON Lines `/var/log/sip-exporter/cdr.jsonl` с rotation
- **HTTP webhook:** POST на конфигурируемый URL
- **Kafka/NATS:** push в message queue (опционально)
- **Stdout:** для container-native (docker logs → Fluentd/Loki)

**Конфигурация:**
```yaml
cdr:
  enabled: true
  output: "file"  # file | webhook | stdout
  path: "/var/log/sip-exporter/cdr.jsonl"
  webhook_url: "http://billing-service/api/cdr"
```

**Архитектура:**
- Новый интерфейс `CdrWriter` с методами `Write(record CdrRecord)`
- CDR генерируется в `handleBye200OK()` и `sipDialogMetricsUpdate()` (expired dialogs)
- Асинхронная запись через buffered channel
- Requires: парсинг From/To URI (уже есть в dto.Packet)

**Почему P2:** CDR = биллинг + отчётность. Это то, за что платят деньги.

**Сложность:** средняя.

---

#### P2: SIP User-Agent Classification

**Проблема:** оператор не видит, какие устройства/серверы генерируют ошибки. «Проблема с Yealink T46S» vs «проблема с Asterisk» — разная реакция.

**Решение:**
- Парсить `User-Agent` и `Server` заголовки из SIP-пакетов
- Классифицировать по встроенной базе паттернов
- Добавить label `ua_type` к существующим метрикам:
  ```
  sip_exporter_invite_total{ua_type="yealink"}
  sip_exporter_response_total{status="503",ua_type="asterisk"}
  ```

**База паттернов (расширяемая через конфигурацию):**
```yaml
user_agents:
  - pattern: "Yealink.*"
    label: "yealink"
  - pattern: "Grandstream.*"
    label: "grandstream"
  - pattern: "Cisco/SPA.*"
    label: "cisco_spa"
  - pattern: "Kamailio.*"
    label: "kamailio"
  - pattern: "OpenSIPS.*"
    label: "opensips"
  - pattern: "Asterisk.*"
    label: "asterisk"
  - pattern: "FreeSWITCH.*"
    label: "freeswitch"
  - pattern: "Linphone.*"
    label: "linphone"
  - pattern: "MicroSIP.*"
    label: "microsip"
  - pattern: "Zoiper.*"
    label: "zoiper"
  - default: "other"
```

**Архитектура:**
- Парсинг `User-Agent`/`Server` заголовка в `sipPacketParse()` — добавить поле `UserAgent []byte` в `dto.Packet`
- Классификация через regex match (pre-compiled при загрузке конфигурации)
- Добавить `ua_type` label dimension к `Metricser`

**Сложность:** средняя. Требует переработки `Metricser` (добавление label).

**Почему P2:** бесценно для troubleshooting — «какие телефоны генерируют больше ошибок».

---

#### P3: Hot-Reload Configuration

**Проблема:** рестарт для смены конфигурации = риск потери пакетов в 24/7 NOC.

**Решение:**
- SIGHUP → перечитывание конфигурации без остановки
- Применимо к: порты, carrier CIDR-группы, UA-паттерны
- Не применимо к: интерфейс (требует пересоздания AF_PACKET socket)

**Сложность:** низкая.

---

### Tier 3: Completeness Features

#### P3: Multi-interface мониторинг

**Проблема:** `SIP_EXPORTER_INTERFACE` принимает один интерфейс. На border-серверах обычно 2+ NIC (internal + external).

**Решение:**
```yaml
SIP_EXPORTER_INTERFACES=eth0,eth1
```

Отдельный eBPF socket на каждый интерфейс.

**Workaround:** запустить несколько инстансов экспортера.

**Сложность:** средняя.

---

#### P3: IPv6 Support

**Проблема:** eBPF-фильтр обрабатывает только IPv4 (ethertype 0x0800). В современных сетях растёт доля IPv6.

**Решение:**
- Добавить обработку ethertype 0x86DD (IPv6) в eBPF-фильтр
- IPv6 header: 40 байт fixed, Next Header вместо Protocol
- Обновить L3→L4 парсинг в `parseRawPacket()`

**Сложность:** средняя. Требует изменения eBPF C-кода + Go парсера.

---

#### P3: SIP Retransmission Detection

**Проблема:** SIP retransmissions (duplicate INVITE/BYE с тем же Call-ID + CSeq) — индикатор потери пакетов или перегрузки сервера. RFC 3261 §17.

**Решение:**
- Детекция: пакет с тем же Call-ID + CSeq (ID + Method) что уже был обработан
- Метрика: `sip_exporter_retransmissions_total{method="INVITE"}` (counter)
- Ограничение: tracker с TTL (как registerTracker), хранить хэш последних N Call-ID+CSeq

**Сложность:** низкая. Переиспользует архитектуру tracker maps.

---

#### P3: GeoIP Enrichment

**Проблема:** IP-адреса без гео-контекста менее информативны для дашбордов.

**Решение:**
- Интеграция MaxMind GeoLite2 (бесплатная база)
- Embed в бинарник или загружать по пути
- Добавить labels: `sip_exporter_invite_total{country="RU", city="Moscow"}`
- Требует: Per-Carrier Labels (P0) как prerequisite

**Конфигурация:**
```yaml
geoip:
  enabled: true
  db_path: "/usr/local/share/GeoLite2-City.mmdb"
```

**Сложность:** средняя.

---

#### P3: HOMER/HEP Protocol Export

**Проблема:** многие операторы уже используют HOMER для packet capture. sip-exporter не интегрирован с этой экосистемой.

**Решение:**
- Генерация HEPv3 (Homer Encapsulation Protocol) пакетов
- Экспорт захваченных SIP-пакетов в HOMER/heplify-server
- sip-exporter как lightweight capture agent → HOMER как full troubleshooting UI
- Конфигурация: `SIP_EXPORTER_HEP_ENDPOINT=heplify-server:9060`

**Почему P3:** мост между лёгким мониторингом (sip-exporter) и полной платформой (HOMER). Не all-or-nothing.

**Сложность:** средняя. HEPv3 — простой binary protocol.

---

#### P3: Kubernetes Helm Chart

**Проблема:** нет стандартного способа деплоя в Kubernetes.

**Решение:**
- Helm chart с `values.yaml`
- `ServiceMonitor` для Prometheus Operator
- `PodMonitor` для VictoriaMetrics Operator
- ConfigMap для carrier-конфигурации
- `PriorityClass: system-node-critical` (monitoring не должен вытесняться)

**Сложность:** низкая. Стандартная задача.

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

### Roadmap по версиям

| Версия | Фичи | Обоснование |
|--------|-------|-------------|
| **v0.11** | Per-Carrier Labels + Self-Monitoring | Enterprise не купится без multi-tenant видимости; self-monitoring для отличия «трафик упал» от «экспортер сломался» |
| **v0.12** | SIP User-Agent Classification | Troubleshooting по типам устройств — топ-2 запрос NOC после per-carrier |
| **v0.13** | TCP Support (TLS offload) | Открытие 40-60% рынка production-сценариев |
| **v0.14** | RTCP Quality Metrics + MOS | Уникальный дифференциатор — «полный VoIP quality мониторинг», никто в OSS этого не делает на eBPF |
| **v0.15** | CDR Export | Монетизация, биллинг, отчётность |
| **v0.16** | OpenTelemetry + Helm Chart | Расширение рынка интеграции + упрощение деплоя |
| **v0.17** | Hot-reload + IPv6 + Multi-interface | Production completeness |
| **v1.0** | Retransmission Detection + GeoIP + HOMER/HEP | Полнота продукта для GA |

### Итог

Для перехода от «лучший экспортер» к «must-have инструмент №1» критичны 3 вещи:

1. **Per-carrier labels** — оператор должен видеть каждый транк отдельно
2. **RTP/RTCP quality metrics** — мониторинг без quality = полдела
3. **TCP/TLS** — без этого продукт отсекает 40-60% production-сценариев

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
| New SIP header parsing | v0.11 (UA classification) | reuse для scanner detection | sip-shield Rule 4 (Known Scanner UA) зависит от UA parsing |
| TCP/TLS support | v0.13 | critical для sip-shield | Без TCP sip-shield не видит TLS-атаки |
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
| UA Classification | Средняя | Критическая (scanner detection Rule 4) | Повысить до P1 → P0 |
| Self-Monitoring | Средняя | Высокая (agent health для SaaS SLA) | P2 → P1 |
| TCP/TLS | Высокая | Критическая (TLS-атаки) | P1 — не снижать |
| RTCP Quality | Высокая | Низкая | P1 → P2 (сдвинуть ради UA) |
| CDR Export | Средняя | Низкая | P2 — оставить |
| OpenTelemetry | Средняя | Низкая | P2 → P3 (сдвинуть) |

### Что НЕ делать в sip-exporter (оставить для sip-shield)

- **Threat detection rules** — это core sip-shield, не sip-exporter
- **Auto-blocking (iptables/nftables)** — security feature, не monitoring
- **Cloud reporter / SaaS integration** — коммерческая фича
- **Rate limiting per IP** — security, не monitoring

sip-exporter остаётся чистым monitoring tool. sip-shield добавляет security layer поверх. Чёткое разделение = нет конфликта интересов с open-source community.
