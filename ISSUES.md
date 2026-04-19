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

## Приоритет реализации

| Приоритет | Фича | Почему |
|-----------|-------|--------|
| **P0** | Per-carrier labels | Главный дифференциатор, enterprise не купится без этого |
| ~~**P0**~~ | ~~Histogram для SPD/RRD~~ | ✅ Реализовано — histogram с бакетами, deprecated *_average сохранены |
| ~~**P0**~~ | ~~TTR (Time to First Response)~~ | ✅ Реализовано — inviteTracker + histogram `sip_exporter_ttr` |
| ~~**P1**~~ | ~~Health + graceful shutdown~~ | ✅ Реализовано — `/health` endpoint, `IsAlive()`, SIGTERM handler |
| ~~**P1**~~ | ~~SDC (Session Duration Counter)~~ | ✅ Реализовано — `sip_exporter_sdc_total` Counter |
| ~~**P1**~~ | ~~ASR (Answer Seizure Ratio)~~ | ✅ Реализовано — `sip_exporter_asr` GaugeFunc (ITU-T E.411) |
| ~~**P2**~~ | ~~NER (Network Effectiveness Ratio)~~ | ✅ Реализовано — `sip_exporter_ner` GaugeFunc (GSMA IR.42), NER = 100 - ISA |
| ~~**P2**~~ | ~~SIP OPTIONS monitoring~~ | ✅ Реализовано — `sip_exporter_ord` histogram, optionsTracker |
| ~~**P3**~~ | ~~ISS (Ineffective Session Severity)~~ | ✅ Реализовано — `sip_exporter_iss_total` Counter |
| ~~**P3**~~ | ~~LRD (Location Registration Delay)~~ | ✅ Реализовано — `sip_exporter_lrd` histogram, REGISTER→3xx |
| **P1** | TCP/TLS поддержка | Без этого продукт неполноценен для production |
| **P2** | OpenTelemetry export | Расширяет рынок интеграции |
| **P3** | Multi-interface | Нишевый кейс, workaround через несколько инстансов |
