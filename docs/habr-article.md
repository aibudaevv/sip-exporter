# Мониторинг SIP-телефонии через eBPF: полная Observability для VoIP-инфраструктуры

SIP-телефония — критичная инфраструктура. Падение качества = потеря звонков = потеря денег. При этом мониторинг SIP значительно сложнее, чем мониторинг HTTP: протокол stateful, каждый звонок — это диалог (INVITE → 200 OK → BYE), метрики качества стандартизированы в RFC 6076, но реализаций в opensource мало, а агрегированные метрики скрывают проблемы конкретных источников трафика.

В этой статье — как я решил задачу мониторинга SIP-телефонии с помощью eBPF: от захвата пакетов в ядре Linux до метрик RFC 6076 с разбивкой по источникам трафика в Prometheus/VictoriaMetrics.

## eBPF socket filter: как это работает

eBPF (extended Berkeley Packet Filter) позволяет выполнять небольшие программы непосредственно в ядре Linux. Верификатор eBPF гарантирует безопасность: программа не может выйти за рамки выделенной памяти, не может зависнуть в бесконечном цикле, не может модифицировать ядро.

Мой подход — eBPF socket filter на AF_PACKET. Это пассивное наблюдение за сетевым трафиком:

```
SIP-трафик → Сетевая карта → eBPF-фильтр → AF_PACKET сокет → Go → Парсер SIP → Метрики
```

### Ключевой момент

eBPF-фильтр — это socket filter, а не tc/XDP filter. Он только решает, **копировать ли** пакет в приложение. Пакет в любом случае продолжает путь по сетевому стеку к адресату. Фильтр не может модифицировать, заблокировать или перенаправить трафик. Нулевое влияние на прохождение звонков.

### eBPF-программа

Весь фильтр — [100 строк на C](https://github.com/aibudaevv/sip-exporter/blob/main/internal/bpf/sip.c). Порты настраиваются из Go-кода через BPF map, по умолчанию 5060/5061. eBPF отсекает 99% трафика в ядре — в userspace попадают только SIP-пакеты на нужных портах.

## Полный стек метрик

Экспортер предоставляет не только метрики RFC 6076, а полный стек observability для SIP-инфраструктуры.

### Трафик в реальном времени

14 типов SIP-запросов с счётчиками:

| Метрика | Запрос |
|---|---|
| `invite_total` | INVITE — установление сессии |
| `bye_total` | BYE — завершение сессии |
| `register_total` | REGISTER — регистрация |
| `options_total` | OPTIONS — проверка доступности |
| `cancel_total` | CANCEL — отмена запроса |
| `ack_total` | ACK — подтверждение |
| + ещё 8 типов | SUBSCRIBE, NOTIFY, PUBLISH, INFO, PRACK, UPDATE, MESSAGE, REFER |

30 кодов ответов: 100, 180, 181, 182, 183, 200, 202, 300, 302, 400, 401, 403, 404, 405, 407, 408, 480, 481, 486, 487, 488, 500, 501, 502, 503, 504, 600, 603, 604, 606.

Active sessions — gauge текущего количества активных SIP-диалогов. Диалог создаётся при получении 200 OK на INVITE, удаляется при получении 200 OK на BYE или по истечении Session-Expires (по умолчанию 30 минут).

### Качество связи — RFC 6076

[RFC 6076](https://datatracker.ietf.org/doc/html/rfc6076) определяет стандартные метрики производительности SIP. Все метрики — кумулятивные, вычисляются на основе атомарных счётчиков, обновляются при каждом скрейпе.

**SER (Session Establishment Ratio)** — процент успешно установленных сессий:

```
SER = (INVITE → 200 OK) / (Total INVITE - INVITE → 3xx) × 100
```

3xx (redirect) исключаются из знаменателя — это не успех и не неудача, а инструкция маршрутизации. SER = 100 означает, что все не-redirect INVITE получили 200 OK.

**SEER (Session Establishment Effectiveness Ratio)** — процент "эффективных" ответов:

```
SEER = (INVITE → 200, 480, 486, 600, 603) / (Total INVITE - INVITE → 3xx) × 100
```

В числитель входят ответы, означающие однозначный исход: 200 OK (сессия установлена), 480 (абонент недоступен), 486 (занято), 600 (занято везде), 603 (отклонено). SEER всегда ≥ SER.

**ISA (Ineffective Session Attempts)** — процент инфраструктурных ошибок:

```
ISA = (INVITE → 408, 500, 503, 504) / Total INVITE × 100
```

408 (timeout), 500 (internal error), 503 (unavailable), 504 (gateway timeout) — серверные ошибки. ISA растёт — инфраструктура проседает. В отличие от SER/SEER, 3xx НЕ исключаются из знаменателя.

**SCR (Session Completion Ratio)** — процент полностью завершённых сессий:

```
SCR = (Completed Sessions) / Total INVITE × 100
```

Завершённая сессия = INVITE → 200 OK → BYE → 200 OK (или истечение Session-Expires). SCR ≤ SER всегда: не все установленные сессии завершаются корректно.

**ASR (Answer Seizure Ratio)** — классическая метрика телефонии (ITU-T E.411):

```
ASR = (INVITE → 200 OK) / Total INVITE × 100
```

В отличие от SER, 3xx НЕ исключаются. ASR ≤ SER при наличии redirect-ответов.

**NER (Network Effectiveness Ratio)** — качество сети (GSMA IR.42):

```
NER = 100 − ISA
```

NER = 100 — нет инфраструктурных ошибок. NER < 95 — пора бить тревогу.

### Задержки на каждом этапе

Пять гистограмм покрывают все этапы SIP-транзакции:

| Метрика | Что измеряет | От → До |
|---|---|---|
| **RRD** | Задержка регистрации | REGISTER → 200 OK |
| **TTR** | Задержка первого ответа | INVITE → первый 1xx |
| **SPD** | Длительность сессии | INVITE 200 OK → BYE 200 OK |
| **ORD** | Задержка ответа на OPTIONS | OPTIONS → любой ответ |
| **LRD** | Задержка redirect-регистрации | REGISTER → 3xx |

Все гистограммы поддерживают `histogram_quantile()` для перцентильного анализа: p50, p95, p99.

Пример для VictoriaMetrics / Prometheus:

```promql
# 95-й перцентиль задержки регистрации
histogram_quantile(0.95, sum(rate(sip_exporter_rrd_bucket[5m])) by (le))

# 99-й перцентиль длительности сессии
histogram_quantile(0.99, sum(rate(sip_exporter_spd_bucket[5m])) by (le))
```

### Дополнительные метрики

**ISS (Ineffective Session Severity)** — абсолютное количество INVITE→408/500/503/504. В отличие от ISA (процент), ISS позволяет строить алерты на абсолютный объём ошибок:

```promql
# Больше 20 инфраструктурных ошибок в секунду — critical
rate(sip_exporter_iss_total[5m]) > 20
```

**SDC (Session Duration Counter)** — Prometheus Counter завершённых сессий. Удобен для rate-запросов:

```promql
# Завершённых сессий в секунду
rate(sip_exporter_sdc_total[5m])
```

## Per-carrier: метрики по источникам трафика

Агрегированные метрики скрывают проблемы конкретных источников трафика. Если SER = 85%, непонятно — это все источники работают на 85%, или один на 50% а остальные на 95%.

Экспортер решает это через CIDR-маппинг: IP-подсети → имя источника → лейбл `carrier` на каждой метрике.

### Конфигурация

```yaml
# carriers.yaml
carriers:
  - name: "mobile-operator-a"
    cidrs:
      - "10.1.0.0/16"
  - name: "sip-trunk-provider"
    cidrs:
      - "192.168.10.0/24"
      - "192.168.11.0/24"
  - name: "enterprise-pbx"
    cidrs:
      - "172.16.5.0/24"
```

### Как работает

Carrier определяется в момент запроса (INVITE/REGISTER/OPTIONS) по source IP. Если INVITE пришёл от 10.1.5.20 — экспортер находит, что этот IP входит в 10.1.0.0/16, и помечает все метрики этого звонка (включая ответы и завершение диалога) лейблом `carrier="mobile-operator-a"`.

Ответы приходят от другого IP (от SIP-сервера), но carrier наследуется из трекера по Call-ID, а не определяется по IP ответа. Это корректно: метрики относятся к инициатору звонка, а не к серверу.

Результат:

```
sip_exporter_invite_total{carrier="mobile-operator-a"}  1523
sip_exporter_ser{carrier="mobile-operator-a"}            95.2
sip_exporter_ser{carrier="sip-trunk-provider"}           87.4
sip_exporter_ser{carrier="other"}                         0.0
```

Теперь видно: у trunk-провайдера SER = 87.4%, а у мобильного оператора — 95.2%. Можно строить отдельные дашборды и алерты для каждого источника трафика.

IP, не попавшие ни в одну CIDR-подсеть, получают `carrier="other"`.

## Производительность

Нагрузочное тестирование проводилось с помощью [SIPp](https://sipp.sourceforge.net/) через [testcontainers-go](https://golang.testcontainers.org/) — реальный SIP-трафик, не моки.

**Стенд:** Debian 12, ядро Linux 6.x, Docker 29.3.1, Intel i7-8665U (4 ядра / 8 потоков), Go 1.25.9.

### Полный жизненный цикл звонка

Каждый звонок — это полный SIP-диалог: INVITE → 100 Trying → 180 Ringing → 200 OK → ACK → BYE → 200 OK. На loopback-интерфейсе каждый пакет дублируется (отправка + приём), поэтому 7 сообщений → 14 пакетов на звонок.

**GOMAXPROCS=1 (одно ядро):**

| CPS | PPS | CPU avg | CPU peak | RAM | Потери |
|-----|-----|---------|----------|-----|--------|
| 100 | ~1,200 | 0.9% | 1.6% | 12 MB | 0.00% |
| 500 | ~5,900 | 2.5% | 4.0% | 11 MB | 0.00% |
| 1,000 | ~11,800 | 4.5% | 6.6% | 11 MB | 0.00% |
| 1,600 | ~18,900 | 5.8% | 8.9% | 10 MB | 0.00% |
| 2,000 | ~23,600 | 5.0% | 9.2% | 12 MB | 0.00% |

**GOMAXPROCS=8 (все ядра):**

| CPS | PPS | CPU avg | CPU peak | RAM | Потери |
|-----|-----|---------|----------|-----|--------|
| 100 | ~1,200 | 1.0% | 1.9% | 14 MB | 0.00% |
| 500 | ~5,900 | 3.3% | 5.7% | 14 MB | 0.00% |
| 1,000 | ~11,800 | 5.9% | 8.7% | 13 MB | 0.00% |
| 2,000 | ~23,600 | 6.7% | 12.2% | 15 MB | 0.00% |

На одном ядре — ниже CPU и RAM, но стабильность падает на 1800+ CPS (2 из 3 прогонов). На всех ядрах — стабильно 0% потерь на всех нагрузках.

**2000 CPS, 0% потерь, <12% CPU, ~15 MB RAM.**

### Почему так быстро

1. **eBPF отсекает 99% трафика в ядре** — в userspace попадают только SIP-пакеты на портах 5060/5061
2. **Буфер сокета 4 MB** — вмещает ~420мс трафика при 28000 PPS
3. **Go GC pause < 1мс** — в 400 раз меньше ёмкости буфера, пакеты никогда не теряются из-за GC
4. **Парсинг ~1мкс** — микробенчмарки: INVITE 1.1μs, BYE 860ns, 200 OK 2.0μs

### Потребление ресурсов при скрейпе

HTTP GET `/metrics` под нагрузкой 2000 CPS (14000 PPS):

| Метрика | Значение |
|---|---|
| Min | 1.7 ms |
| Avg | 4.2 ms |
| P95 | 6.4 ms |
| Max | 8.4 ms |

Скрейп не мешает обработке пакетов. Можно скрейпить каждые 5-10 секунд даже при максимальной нагрузке.

### Требования к системе

| Нагрузка | CPU | RAM |
|---|---|---|
| ≤ 500 CPS | 1 ядро | 128 MB |
| ≤ 1,000 CPS | 1 ядро | 128 MB |
| ≤ 2,000 CPS | 2 ядра | 256 MB |
| > 2,000 CPS | 4 ядра | 512 MB |

## Безопасность: почему `--privileged`

Контейнер требует `--privileged` и `network_mode: host`. Вот почему это безопасно.

### Какие capabilities нужны

| Capability | Зачем |
|---|---|
| `CAP_BPF` | Загрузка eBPF-программы в ядро через syscall `bpf()` |
| `CAP_NET_RAW` | Создание raw-сокета `AF_PACKET` для чтения пакетов |
| `CAP_NET_ADMIN` | Привязка eBPF-фильтра к сокету, настройка буфера |

Это три конкретные capabilities для конкретных операций. Все eBPF-инструменты (Cilium, Falco, Pixie) требуют то же самое — это ограничение на уровне ядра Linux, а не контейнера.

### Что контейнер делает с привилегиями

Только **читает** пакеты:

1. Загружает eBPF socket filter в ядро (один раз, при запуске)
2. Создаёт `AF_PACKET` raw-сокет, привязанный к сетевому интерфейсу
3. Читает пакеты из сокета в Go-канал (буфер 10,000)
4. Парсит SIP-заголовки
5. Экспортирует метрики через `/metrics`

### Что контейнер НЕ делает

- **Не** модифицирует пакеты — eBPF-фильтр пассивный (read-only)
- **Не** отправляет SIP-трафик — исключительно слушатель
- **Не** пишет в файловую систему хоста — все volumes `:ro`
- **Не** обращается к другим контейнерам, процессам или системным ресурсам
- **Не** открывает порты, кроме `/metrics` (по умолчанию 2112)
- **Не** устанавливает исходящие соединения

### Аудит eBPF-кода

Весь eBPF-фильтр — 100 строк на C, можно прочитать за 2 минуты: [`internal/bpf/sip.c`](https://github.com/aibudaevv/sip-exporter/blob/main/internal/bpf/sip.c). Программа делает одну вещь: фильтрует пакеты по UDP-портам.

### Автоматическое сканирование уязвимостей

Код и образ контейнера автоматически проверяются:

- **govulncheck** — Go-зависимости по Go Vulnerability Database (каждый push + ежедневно)
- **Trivy** — образ контейнера (пакеты ОС + бинарники) по базам CVE (каждый push + ежедневно)

Результаты публикуются в GitHub Security tab. Текущий статус: **0 уязвимостей** в коде и образе.

## Быстрый старт

```yaml
# docker-compose.yml
services:
  sip-exporter:
    image: frzq/sip-exporter:latest
    privileged: true
    network_mode: host
    environment:
      - SIP_EXPORTER_INTERFACE=eth0
```

```bash
docker compose up -d
curl http://localhost:2112/metrics
```

Пример вывода:

```
# HELP sip_exporter_ser Session Establishment Ratio (RFC 6076)
# TYPE sip_exporter_ser gauge
sip_exporter_ser{carrier="other"} 95.2

# HELP sip_exporter_invite_total Total SIP INVITE requests
# TYPE sip_exporter_invite_total counter
sip_exporter_invite_total{carrier="other"} 1523

# HELP sip_exporter_sessions Number of active SIP dialogs
# TYPE sip_exporter_sessions gauge
sip_exporter_sessions{carrier="other"} 12

# HELP sip_exporter_rrd Registration Request Delay (RFC 6076)
# TYPE sip_exporter_rrd histogram
sip_exporter_rrd_bucket{carrier="other",le="1"} 10
sip_exporter_rrd_bucket{carrier="other",le="5"} 45
sip_exporter_rrd_bucket{carrier="other",le="10"} 78
sip_exporter_rrd_bucket{carrier="other",le="25"} 95
sip_exporter_rrd_bucket{carrier="other",le="50"} 98
sip_exporter_rrd_bucket{carrier="other",le="100"} 100
sip_exporter_rrd_sum{carrier="other"} 423.5
sip_exporter_rrd_count{carrier="other"} 100
```

Совместимость: Prometheus, VictoriaMetrics, Grafana Cloud — любой scraper, поддерживающий Prometheus exposition format.

Проект: [github.com/aibudaevv/sip-exporter](https://github.com/aibudaevv/sip-exporter) (AGPL-3.0)
