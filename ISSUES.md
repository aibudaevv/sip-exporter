# SIP Exporter — Roadmap & Feature Proposals

## Реализованные метрики (v0.5–0.9)

| Метрика | RFC 6076 | Описание | Версия |
|---------|----------|----------|--------|
| SER | §4.2 | Session Establishment Ratio | 0.5.0 |
| SEER | §4.3 | Session Establishment Effectiveness Ratio | 0.6.0 |
| ISA | §4.6 | Ineffective Session Attempts | 0.7.0 |
| SCR | §4.9 | Session Completion Ratio | 0.8.0 |
| RRD | §4.1 | Registration Request Delay | 0.8.0 |
| SPD | §4.7 | Session Process Duration | 0.9.0 |

---

## Предлагаемые метрики RFC 6076

### P0: TTR — Time to First Response (RFC 6076 §4.8)

**Описание:** время от INVITE до первого provisional response (100/180/183).

**Зачем:** позволяет отслеживать задержки на стороне сервера (медленные SIP-прокси). Непосредственно влияет на пользовательский опыт — «сколько времени сервер думает перед ответом».

**Реализация:** расширить `dialogEntry` полем `inviteAt time.Time`, замерять при получении первого 1xx ответа на INVITE.

**Формула:**
```
TTR = Average(Time of first 1xx response - Time of INVITE request)
```

**Почему P0:** естественно расширяет текущую архитектуру с `dialogEntry`, даёт операционную видимость.

---

### P1: SDC — Session Duration Counter (RFC 6076 §4.10)

**Описание:** общее количество завершённых сессий (BYE 200 OK).

**Зачем:** простой счётчик, дополняет SPD для понимания «сколько сессий в среднем за период». Позволяет строить rate-графики (`rate(sip_exporter_sdc_total[5m])`).

**Реализация:** добавить `prometheus.Counter` с инкрементом в `handleBye200OK` и `sipDialogMetricsUpdate`.

---

### P1: ASR — Answer Seizure Ratio

**Описание:** классика телефонии — ratio ответов (200 OK на INVITE) к общему числу INVITE.

**Формула:**
```
ASR = (INVITE → 200 OK) / Total INVITE × 100
```

**Отличие от SER:** не исключает 3xx из знаменателя. Проще интерпретировать, стандартная метрика в телекоме.

---

### P2: NER — Network Effectiveness Ratio (RFC 6076 §4.4)

**Описание:** похож на SEER, но с фокусом на сетевые проблемы.

**Формула:**
```
NER = (Total INVITE - INVITE → 408, 500, 503, 504) / Total INVITE × 100
```

**Зачем:** дополняет ISA. Показывает долю вызовов, не пострадавших от сетевых проблем.

---

### P3: LRD — Location Registration Delay

**Описание:** задержка между REGISTER и 3xx redirect.

**Зачем:** нишевый кейс, если есть redirect-сервер. Аналог RRD, но для redirect-сценариев.

---

### P3: ISS — Ineffective Session Severity

**Описание:** по сути просто счётчик ошибок (408+500+503+504).

**Зачем:** уже можно вычислить из существующих каунтеров через PromQL:
```promql
sip_exporter_408_total + sip_exporter_500_total + sip_exporter_503_total + sip_exporter_504_total
```

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

### P0: Histogram вместо среднего для SPD/RRD

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

### P1: Health endpoint + Graceful Shutdown

**Проблема:** нет health-check endpoint'а и graceful shutdown. Kubernetes убивает контейнер без drained connections.

**Решение:**

```
GET /health → 200 OK
```

SIGTERM handler:
```go
signal.Notify(sigCh, syscall.SIGTERM)
<-sigCh
// 1. Stop accepting packets
// 2. Flush metrics
// 3. Close eBPF socket
// 4. Shutdown HTTP server gracefully
```

Kubernetes probes:
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

**Сложность:** низкая.

---

### P2: SIP OPTIONS Ping Monitoring

**Проблема:** SIP trunk-провайдеры используют OPTIONS ping для проверки alive-статуса. Мониторинг ответов на OPTIONS — критичная фича для NOC-команд.

**Решение:**
- Метрика: `sip_exporter_options_response_seconds` (histogram) — задержка ответа на OPTIONS
- Парсить OPTIONS → 200 OK pair по Call-ID (аналог RRD, но для OPTIONS)
- Alert: OPTIONS без ответа > N секунд → trunk down

**Сложность:** низкая. Аналогична реализации RRD.

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
| **P0** | Histogram для SPD/RRD | Текущие средние — бесполезны для alerting |
| **P0** | TTR (Time to First Response) | Операционная видимость, естественно расширяет архитектуру |
| **P1** | TCP/TLS поддержка | Без этого продукт неполноценен для production |
| **P1** | Health + graceful shutdown | Минимум для Kubernetes deployment |
| **P1** | SDC, ASR метрики | Дополняют картину, простая реализация |
| **P2** | SIP OPTIONS monitoring | Востребовано NOC-командами |
| **P2** | OpenTelemetry export | Расширяет рынок интеграции |
| **P2** | NER метрика | Дополняет ISA |
| **P3** | Multi-interface | Нишевый кейс, workaround через несколько инстансов |
| **P3** | LRD, ISS метрики | Вычисляются из существующих метрик |
