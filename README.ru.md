# SIP-exporter

**[EN](README.md)** | **[RU](README.ru.md)**

Высокопроизводительный мониторинг SIP-телефонии на базе eBPF. Захватывает пакеты напрямую в ядре Linux и экспортирует метрики в Prometheus-совместимые системы (Prometheus, VictoriaMetrics и др.).

[![Go Test](https://github.com/aibudaevv/sip-exporter/actions/workflows/go.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/go.yml)
[![Go Vulncheck](https://github.com/aibudaevv/sip-exporter/actions/workflows/vulncheck.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/vulncheck.yml)
[![Container Scan](https://github.com/aibudaevv/sip-exporter/actions/workflows/trivy.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/trivy.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/aibudaevv/sip-exporter)](https://goreportcard.com/report/github.com/aibudaevv/sip-exporter)
[![Docker Pulls](https://img.shields.io/docker/pulls/frzq/sip-exporter)](https://hub.docker.com/r/frzq/sip-exporter)
[![GitHub Release](https://img.shields.io/github/v/release/aibudaevv/sip-exporter)](https://github.com/aibudaevv/sip-exporter/releases)
[![License](https://img.shields.io/badge/license-AGPL--3.0-blue)](https://github.com/aibudaevv/sip-exporter/blob/main/LICENSE)
[![Issues](https://img.shields.io/github/issues/aibudaevv/sip-exporter)](https://github.com/aibudaevv/sip-exporter/issues)

## Содержание

- [Возможности](#возможности)
- [Быстрый старт](#быстрый-старт)
- [Технология](#технология)
- [Архитектура](#архитектура)
- [Производительность](#производительность)
- [Установка](#установка)
- [Метрики](#метрики)
- [Безопасность](docs/SECURITY.ru.md)
- [Разработка](#разработка)
- [Нагрузочное тестирование](#нагрузочное-тестирование)
- [Алертинг](#алертинг)
- [Совместимость с хранилищами метрик](#совместимость-с-хранилищами-метрик)
- [Лицензия](#лицензия)
- [Changelog](#changelog)

## Возможности

- 🌐 **Мониторинг нескольких интерфейсов** — захват SIP/RTP с нескольких NIC одновременно, каждый помечается лейблом `iface`
- ⚡ **Минимальная нагрузка** — фильтрация пакетов через eBPF в пространстве ядра
- 🐳 **Один контейнер** — никаких внешних зависимостей
- 🔧 **Настраиваемые SIP-порты** — мониторинг нестандартных портов через переменные окружения
- 📈 **Нативный Prometheus** — стандартный эндпоинт `/metrics`
- 🏷️ **Метрики по операторам** — разрешение carrier на основе CIDR для всех SIP-метрик
- 🏷️ **Метрики по типам устройств** — классификация User-Agent для всех SIP-метрик
- 🌍 **Гео-обогащение** — лейблы `source_country` (GeoIP) и `destination_country` (E.164 prefix) в SIP-метриках
- 📞 **Качество голоса (RFC 6035)** — MOS, джиттер, потери пакетов из SIP PUBLISH/NOTIFY
- 🎧 **Анализ RTP-медиа** — джиттер, потери и MOS (E-model G.107) из RTP-потоков, скоррелированных с SIP-диалогами, без захвата голосового payload (только заголовок)
- 🛡️ **Детекция фрода** — сигналы сканирования регистраций, всплесков INVITE и перехвата аккаунтов (смена страны)

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
      - SIP_EXPORTER_HTTP_PORT=2112
      - SIP_EXPORTER_SIP_PORTS=5060
      # Опционально: метки carrier для мониторинга по операторам
      # - SIP_EXPORTER_CARRIERS_CONFIG=/etc/sip-exporter/carriers.yaml
      # Опционально: метки ua_type для мониторинга по типам устройств
      # - SIP_EXPORTER_USER_AGENTS_CONFIG=/etc/sip-exporter/user_agents.yaml
    # volumes:
    #   - ./examples/carriers.yaml:/etc/sip-exporter/carriers.yaml:ro
    #   - ./examples/user_agents.yaml:/etc/sip-exporter/user_agents.yaml:ro
```

```bash
docker compose up -d
curl http://localhost:2112/metrics
```

Метрики доступны на `http://localhost:2112/metrics`.

## Технология

Сервис использует eBPF (extended Berkeley Packet Filter), подключённый к сокетам `AF_PACKET` для перехвата SIP-пакетов (UDP/5060) на L4 без накладных расходов iptables/nftables или userspace-демонов вроде tcpdump. Отфильтрованные пакеты передаются в userspace через сокет для эффективной обработки на Go.

## Архитектура
```
SIP + RTP-трафик → NIC → eBPF socket filter → AF_PACKET socket → Go poller → SIP-парсер + RTP-трекер → Prometheus
```

## Производительность

Нулевая потеря пакетов до **2 000 CPS** (~28 000 PPS) при полном жизненном цикле SIP-диалога, **<15% CPU** и **~15 МБ RAM**. GC stop-the-world паузы менее **1 мс** — в 400 раз меньше ёмкости буфера сокета, что гарантирует отсутствие потерь пакетов из-за GC. Память стабильна при длительной нагрузке, утечек не обнаружено.

Микробенчмарки Go:

| Операция | Задержка | Память |
|-----------|---------|--------|
| Парсинг BYE (L2→SIP) | ~860 ns | 712 B/op |
| Парсинг INVITE (L2→SIP) | ~1.1 μs | 808 B/op |
| Парсинг 200 OK (L2→SIP) | ~2.0 μs | 1176 B/op |

Полные результаты: [docs/BENCHMARK.md](./docs/BENCHMARK.md).

## Установка

```bash
docker pull frzq/sip-exporter:latest
```

### Конфигурация

Переменные окружения:
* `SIP_EXPORTER_INTERFACE` — один или несколько сетевых интерфейсов через запятую (обязательно). Примеры: `eth0`, `eth0,eth1,eth2`.
* `SIP_EXPORTER_HTTP_PORT` — HTTP-порт для Prometheus (по умолчанию 2112)
* `SIP_EXPORTER_LOGGER_LEVEL` — уровень логирования (по умолчанию info)
* `SIP_EXPORTER_SIP_PORTS` — один или несколько SIP-портов через запятую (по умолчанию 5060; до 3 на интерфейс). Через `;` — наборы для каждого интерфейса: `5060,5062;5060,5061`.
* `SIP_EXPORTER_OBJECT_FILE_PATH` — путь к eBPF-объектному файлу (по умолчанию /usr/local/bin/sip.o)
* `SIP_EXPORTER_CARRIERS_CONFIG` — путь к YAML-конфигурации carriers (опционально, см. [`examples/carriers.yaml`](examples/carriers.yaml))
* `SIP_EXPORTER_USER_AGENTS_CONFIG` — путь к YAML-конфигурации user-agents (опционально, см. [`examples/user_agents.yaml`](examples/user_agents.yaml))
* `SIP_EXPORTER_RTP_STREAM_TTL` — время жизни простаивающего RTP-потока до удаления, таймаут RFC 3550 §6.3.5 (по умолчанию 30s)
* `SIP_EXPORTER_IGNORE_OUTGOING` — только для loopback/тестов: подавляет дубликаты TX-пакетов на `lo` (по умолчанию false, НЕ включать в production)
* `SIP_EXPORTER_GEOIP_COUNTRY_DB` — путь к MaxMind GeoLite2-Country.mmdb для лейбла `source_country` (опционально)
* `SIP_EXPORTER_LOCAL_COUNTRY_CODE` — код страны ISO alpha-2 для локальных номеров без международного префикса в `destination_country` (опционально, напр. `RU`)
* `SIP_EXPORTER_HOST_LABELS` — включить лейблы `caller_host`/`called_host` в INVITE-метриках (по умолчанию `false`; opt-in — неограниченная кардинальность, включайте только в доверенных деплоях с ограниченным числом узлов)
* `SIP_EXPORTER_TELEMETRY` — анонимная телеметрия использования, отключается значением `false` (по умолчанию true)

Контейнер должен запускаться с `--privileged` и `--network host` (eBPF требует `CAP_BPF` и доступ к сетевому интерфейсу). Подробнее о безопасности — в [Безопасность](docs/SECURITY.ru.md).

> ⚠️ **Особенность мульти-интерфейса:** не указывайте интерфейсы, которые видят один и тот же трафик (bond parent + child, bridge + member, VLAN parent + subinterface, дублирующие SPAN-порты). Это приведёт к задвоению метрик. Если сомневаетесь — указывайте только физические NIC.

## Метрики

Все метрики доступны на `/metrics` в формате Prometheus. Все SIP-метрики содержат лейблы `carrier`, `ua_type` и `source_country` для многомерного анализа. INVITE-метрики дополнительно содержат лейбл `iface`, идентифицирующий сетевой интерфейс, захвативший трафик. Экспортер предоставляет:

- **Счётчики трафика** — типы SIP-запросов (INVITE, re-INVITE, BYE, REGISTER и т.д.) и коды ответов (100–606)
- **Активные сессии** — количество активных SIP-диалогов в реальном времени
- **Метрики RFC 6076** — SER, SEER, ISA, SCR, ASR, NER, RRD, SPD, TTR, PDD, PBD
- **Метрики качества голоса RFC 6035** — NLR, JDR, BLD, GLD, RTD, ESD, IAJ, MAJ, MOSLQ, MOSCQ, RLQ, RCQ, RERL
- **Метрики RTP-медиа** — `rtp_packets_total`, `rtp_packets_lost_total`, `rtp_jitter_milliseconds`, `rtp_mos_score`, `rtp_active_streams` (лейблы: `carrier,ua_type,codec,source_country`)
- **Диагностика** — `sip_retransmission_total` (ретрансмиссии по SIP Timer A), `rtp_out_of_order_total` (нарушение порядка RTP-пакетов), `short_calls_total` (звонки короче 20/60/180 секунд)

Полный справочник с формулами, примерами и привязкой к RFC: [docs/METRICS.ru.md](docs/METRICS.ru.md)

### Метрики по операторам (Carrier)

Если ваша SIP-инфраструктура обрабатывает трафик от нескольких операторов (телефонные провайдеры, SIP-транки, PBX-кластеры), вам нужно видеть метрики **по каждому оператору**, а не в сумме.

Функция carrier решает эту задачу, связывая IP-подсети с именами операторов через YAML-конфигурацию. Каждая метрика — количество INVITE, SER, активные сессии, задержка RRD — получает лейбл `carrier`, что позволяет строить отдельные дашборды Grafana и алерты для каждого оператора.

**Как это работает:**

Экспортер анализирует **source IP** каждого SIP-запроса и сопоставляет его с CIDR-подсетями из конфигурации. Когда UAC с адресом `10.1.5.20` отправляет INVITE, экспортер определяет, что `10.1.5.20` входит в подсеть `10.1.0.0/16`, заданную для carrier "telecom-alpha", и помечает все метрики этого звонка — сам INVITE, ответ 200 OK, BYE и даже истечение диалога — лейблом `carrier="telecom-alpha"`.

Это означает:
- INVITE от `10.1.5.20` → метрики с `carrier="telecom-alpha"`
- INVITE от `192.168.11.3` → метрики с `carrier="telecom-beta"`
- INVITE от `8.8.8.8` (не входит ни в одну подсеть) → метрики с `carrier="other"`

**Настройка:**

```yaml
# docker-compose.yml
services:
  sip-exporter:
    image: frzq/sip-exporter:latest
    privileged: true
    network_mode: host
    environment:
      - SIP_EXPORTER_INTERFACE=eth0
      - SIP_EXPORTER_CARRIERS_CONFIG=/etc/sip-exporter/carriers.yaml
    volumes:
      - ./examples/carriers.yaml:/etc/sip-exporter/carriers.yaml:ro
```

```yaml
# carriers.yaml — привязка IP-подсетей к операторам
carriers:
  - name: "telecom-alpha"
    cidrs:
      - "10.1.0.0/16"
  - name: "telecom-beta"
    cidrs:
      - "192.168.10.0/24"
      - "192.168.11.0/24"
```

После настройки метрики выглядят так:

```
sip_exporter_invite_total{carrier="telecom-alpha",ua_type="other"}  1523
sip_exporter_ser{carrier="telecom-alpha",ua_type="other"}            95.2
sip_exporter_ser{carrier="telecom-beta",ua_type="other"}             87.4
sip_exporter_ser{carrier="other",ua_type="other"}                     0.0
```

**Важно знать:**

- Carrier определяется в момент **запроса** (INVITE/REGISTER/OPTIONS), а не ответа. Если carrier-A отправил INVITE, а carrier-B ответил 200 OK — все метрики относятся к carrier-A, инициатору звонка
- Если source IP не входит ни в одну CIDR-подсеть, проверяется destination IP. Если и он не найден → `carrier="other"`
- При пересекающихся CIDR **побеждает первое совпадение** — указывайте более специфичные подсети перед широкими
- Без файла конфигурации все метрики получают `carrier="other"` — ничего не ломается
- Для каждого carrier можно указать несколько CIDR, количество carrier не ограничено
- CIDR-нотация обязательна — обычные IP-адреса без `/` не принимаются. Используйте `/32` для одного хоста, например `"10.226.97.5/32"` вместо `"10.226.97.5"`

Полный пример конфигурации: [`examples/carriers.yaml`](examples/carriers.yaml)

### Метрики по типам устройств (User-Agent)

Если вам нужно видеть метрики **по типам SIP-устройств** — IP-телефоны, софтфоны, SBC — функция классификации User-Agent добавляет лейбл `ua_type` к каждой метрике.

Экспортер читает заголовок `User-Agent` из каждого SIP-запроса и сопоставляет его с regex-паттернами из YAML-конфигурации. Каждая метрика — количество INVITE, SER, активные сессии, длительность SPD — получает лейбл `ua_type`, что позволяет строить отдельные дашборды Grafana и алерты для каждого семейства устройств.

**Как это работает:**

Экспортер парсит заголовок `User-Agent` каждого SIP-запроса и сопоставляет его с regex-паттернами из конфигурации. Когда телефон с `User-Agent: Yealink SIP-T46S 66.15.0.10` отправляет INVITE, экспортер находит совпадение с паттерном `^Yealink` и помечает все метрики этого звонка лейблом `ua_type="yealink"`.

Это означает:
- INVITE от телефона Yealink → метрики с `ua_type="yealink"`
- INVITE от телефона Grandstream → метрики с `ua_type="grandstream"`
- INVITE с неизвестным User-Agent → метрики с `ua_type="other"`

**Настройка:**

```yaml
# docker-compose.yml
services:
  sip-exporter:
    image: frzq/sip-exporter:latest
    privileged: true
    network_mode: host
    environment:
      - SIP_EXPORTER_INTERFACE=eth0
      - SIP_EXPORTER_USER_AGENTS_CONFIG=/etc/sip-exporter/user_agents.yaml
    volumes:
      - ./examples/user_agents.yaml:/etc/sip-exporter/user_agents.yaml:ro
```

```yaml
# user_agents.yaml — привязка User-Agent паттернов к типам устройств
user_agents:
  - regex: '(?i)^Yealink'
    label: yealink
  - regex: '(?i)^Grandstream'
    label: grandstream
  - regex: '(?i)^Cisco/SPA'
    label: cisco_spa
  - regex: '(?i)^Kamailio'
    label: kamailio
  - regex: '(?i)^Asterisk'
    label: asterisk
```

После настройки метрики выглядят так:

```
sip_exporter_invite_total{carrier="telecom-alpha",ua_type="yealink"}     1523
sip_exporter_ser{carrier="telecom-alpha",ua_type="yealink"}               95.2
sip_exporter_ser{carrier="telecom-alpha",ua_type="grandstream"}           87.4
sip_exporter_ser{carrier="telecom-alpha",ua_type="other"}                  0.0
```

**Важно знать:**

- Тип устройства определяется в момент **запроса** (INVITE/REGISTER/OPTIONS), используя тот же механизм трекера, что и carrier. Ответы наследуют `ua_type` из трекера запроса, а не из собственных заголовков ответа
- Заголовок `User-Agent` извлекается из всех SIP-пакетов, но SIP-ответы обычно используют заголовок `Server`, поэтому на практике только запросы дают осмысленную классификацию
- Если ни один паттерн не совпал → `ua_type="other"`
- При пересечении паттернов **побеждает первое совпадение** — указывайте специфичные паттерны перед широкими
- Без файла конфигурации все метрики получают `ua_type="other"` — ничего не ломается
- Паттерны нечувствительны к регистру при использовании префикса `(?i)`
- Работает **совместно с carrier** — каждая метрика имеет оба лейбла `carrier` и `ua_type` для двумерного анализа

**Совместные запросы carrier + ua_type:**

```promql
# SER для телефонов Yealink у конкретного оператора
sip_exporter_ser{carrier="telecom-alpha",ua_type="yealink"}

# Активные сессии по типам устройств (по всем операторам)
sum by (ua_type) (sip_exporter_sessions)

# Частота INVITE по операторам и типам устройств
sum by (carrier, ua_type) (rate(sip_exporter_invite_total[5m]))
```

Полный пример конфигурации: [`examples/user_agents.yaml`](examples/user_agents.yaml)

### Гео-обогащение метрик (Geo-Enrichment)

Экспортер добавляет географический контекст к SIP-метрикам через два лейбла:

| Лейбл | Метод | Область |
|-------|-------|---------|
| `source_country` | GeoIP-лукап source IP (MaxMind GeoLite2-Country) | Все SIP + RTP метрики |
| `destination_country` | Префикс E.164 номера (embedded, без БД) | Только INVITE-метрики |

**Разрешение source_country:**
1. `carrier.country` — опциональное поле в `carriers.yaml`, приоритет над GeoIP (оператор знает лучше)
2. `GeoIP(srcIP)` — лукап по базе MaxMind GeoLite2-Country
3. `"unknown"` — fallback при отсутствии обоих

**destination_country** не требует **никакой базы** — таблица префиксов встроена в бинарник (Google libphonenumber, Apache 2.0). Для локальных номеров без международного префикса укажите `SIP_EXPORTER_LOCAL_COUNTRY_CODE`.

**caller_host / called_host** **отключены по умолчанию** (`SIP_EXPORTER_HOST_LABELS=false`). Они раскрывают host-часть SIP-URI `From`/`To` в `invite_total` / `invite_200_total`. Поскольку число уникальных узлов не ограничено, они opt-in: включайте (`SIP_EXPORTER_HOST_LABELS=true`) только в доверенных деплоях с ограниченным числом узлов, иначе они могут раздуть память Prometheus. См. [Безопасность > Данные в лейблах Prometheus](docs/SECURITY.ru.md#данные-в-лейблах-prometheus).

**Настройка:**

```yaml
# docker-compose.yml
services:
  sip-exporter:
    image: frzq/sip-exporter:latest
    privileged: true
    network_mode: host
    environment:
      - SIP_EXPORTER_INTERFACE=eth0
      - SIP_EXPORTER_GEOIP_COUNTRY_DB=/data/GeoLite2-Country.mmdb
      - SIP_EXPORTER_LOCAL_COUNTRY_CODE=RU    # опционально: fallback для локальных номеров
    volumes:
      - ./GeoLite2-Country.mmdb:/data/GeoLite2-Country.mmdb:ro
```

Полный справочник с формулами и примерами PromQL: [docs/METRICS.ru.md > Лейблы геообогащения](docs/METRICS.ru.md#лейблы-геообогащения)

Пошаговая настройка (как получить и подключить базу MaxMind): [`docs/geoip.ru.md`](docs/geoip.ru.md)

```promql
# SER для звонков в Россию
sum(rate(sip_exporter_invite_200_total{destination_country="RU"}[5m]))
  / sum(rate(sip_exporter_invite_total{destination_country="RU"}[5m])) * 100

# Частота INVITE по странам назначения
sum by (destination_country) (rate(sip_exporter_invite_total[5m]))
```

### Анализ RTP-медиа

Помимо сигналинга SIP, экспортер умеет захватывать и анализировать RTP-потоки, чтобы измерять реальное качество звонка (джиттер, потери, MOS). RTP-потоки **скоррелированы с SIP-диалогами**: когда `200 OK` на INVITE несёт SDP, экспортер регистрирует согласованные media-endpoint'ы и отслеживает соответствующие RTP-потоки до BYE (или истечения Session-Expires). Поэтому RTP-метрики наследуют лейблы диалога `carrier`, `ua_type` и согласованный `codec`.

Производимые метрики:

| Метрика | Тип | Описание |
|--------|------|-------------|
| `sip_exporter_rtp_packets_total` | counter | количество RTP-пакетов |
| `sip_exporter_rtp_packets_lost_total` | counter | потерянные пакеты (по seq-gap RFC 3550) |
| `sip_exporter_rtp_jitter_milliseconds` | histogram | межпакетный джиттер (RFC 3550 A.8) |
| `sip_exporter_rtp_mos_score` | histogram | MOS-LQ по E-model ITU-T G.107 (1.0–4.5) |
| `sip_exporter_rtp_active_streams` | gauge | активные RTP-потоки, скоррелированные с диалогами |

**Приватность:** захватывается только 12-байтовый заголовок RTP — голосовой payload обрезается в ядре (eBPF) до попадания в userspace, поэтому никакой аудиозаписи не происходит.

Захват RTP всегда включён. RTP без скоррелированного SIP-диалога (без замеченного SDP-обмена) отбрасывается, поэтому учитывается только медиа для отслеживаемых звонков.

eBPF-фильтр использует **SDP-driven RTP-детекцию**: media-endpoint'ы (IP:порт), извлечённые из SDP в INVITE 200 OK, помещаются в BPF LRU hash map. Через ядро проходят только UDP-пакеты, совпадающие с зарегистрированным endpoint'ом — весь остальной UDP отбрасывается. Это исключает ложные срабатывания от постороннего UDP-трафика на публичных IP-адресах.

```PromQL
# Средний MOS за 5 минут (по кодекам)
sum by (codec) (rate(sip_exporter_rtp_mos_score_sum[5m]))
  / sum by (codec) (rate(sip_exporter_rtp_mos_score_count[5m]))

# Доля потерь по операторам
sum by (carrier) (rate(sip_exporter_rtp_packets_lost_total[5m]))
  / sum by (carrier) (rate(sip_exporter_rtp_packets_total[5m]))
```

Полный справочник по RTP-метрикам, формулы и разрешение лейблов — в [docs/METRICS.ru.md](docs/METRICS.ru.md).

## Разработка

### Требования
- Go 1.25+
- Clang/LLVM (для компиляции eBPF)
- Ядро Linux с поддержкой eBPF
- Права root (требуются для eBPF и packet socket)

### Покрытие тестами

| Пакет | Покрытие |
|---------|----------|
| `internal/config` | 100.0% |
| `pkg/log` | 95.5% |
| `internal/server` | 90.5% |
| `internal/service` | 75.4% |
| `internal/exporter` | 64.0% |

Набор тестов:
- **Unit-тесты** — стандарт MC/DC, покрыта вся бизнес-логика
- **158 E2E-тестов** — реальный SIP-трафик через SIPp + testcontainers-go, валидация всех метрик RFC 6076, RFC 6035 и RTP
- **13 нагрузочных тестов** — пропускная способность PPS, VQ-отчёты, параллельные сессии, стабильность памяти, GC-паузы, latency скрейпа

## Нагрузочное тестирование

Результаты: **0% потерь пакетов при 2 000 CPS (28 000 PPS)**.

Подробности в [BENCHMARK.md](./docs/BENCHMARK.md) — результаты, методология и заметки по оптимизации.

## Алертинг

Grafana-дашборд и правила алертов Prometheus включены в репозиторий.

**Grafana-дашборд** — импорт вручную:

1. Grafana → Dashboards → Import
2. Загрузите [`examples/grafana-dashboard.json`](examples/grafana-dashboard.json)
3. Выберите datasource Prometheus или VictoriaMetrics

Дашборд содержит: счётчики трафика, разбивку SIP-запросов/ответов, активные сессии, метрики RFC 6076 (SER, SEER, ISA, SCR, NER), регистрации (активные, success ratio, ошибки по кодам, фрод-сигналы), анализ RTP-медиа (активные потоки, rate пакетов, loss rate, MOS, jitter по кодекам), метрики качества голоса RFC 6035 (MOS, jitter, потери пакетов), гистограммы задержек (RRD, TTR, PDD, SPD, ORD, LRD, PBD), метрики качества сессий (ISS, ASR, SDC), диагностику (ретрансмиссии SIP, короткие звонки) и системные ошибки.

Полный гайд по алертингу: правила Prometheus, конфиги Alertmanager (Slack/PagerDuty/Email), настройка порогов — [`docs/ALERTING.ru.md`](docs/ALERTING.ru.md)

## Совместимость с хранилищами метрик

SIP-Exporter экспортирует метрики в формате Prometheus exposition, совместимом с:

- **Prometheus** — pull-based мониторинг
- **VictoriaMetrics** — высокопроизводительная TSDB
- **Grafana Cloud** — облачная наблюдаемость
- **Любой Prometheus-совместимый скрейпер** — эндпоинт `/metrics` следует стандартному формату

## Лицензия

Проект лицензирован под **GNU Affero General Public License v3.0 (AGPL-3.0)**.

Полный текст: [LICENSE](LICENSE).

### Лицензии сторонних данных

- **MaxMind GeoLite2** (`source_country`) — бесплатно для внутреннего использования с указанием авторства; распространение/встраивание требует [Commercial License](https://www.maxmind.com/en/geolite2/eula). Пользователь скачивает БД отдельно.
- **Google libphonenumber** (`destination_country`) — [Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0). Данные префиксов E.164 встроены в бинарник при компиляции.

### Коммерческое использование
- ✅ Бесплатно для личного и образовательного использования
- ✅ Бесплатно для коммерческого использования с условиями
- ⚠️ При модификации и запуске как публичный сервис — необходимо открыть исходный код модификаций
- 📧 Для коммерческого лицензирования без требований AGPL — свяжитесь с автором

## Changelog
История версий — на странице [GitHub Releases](https://github.com/aibudaevv/sip-exporter/releases).
