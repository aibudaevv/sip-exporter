# SIP-exporter

**[EN](README.md)** | **[RU](README.ru.md)**

sip-exporter — это eBPF-сенсор с открытым исходным кодом, который преобразует SIP-сигнализацию и
связанный RTP, наблюдаемые на Linux-хосте, в метрики Prometheus и панели Grafana, не сохраняя
содержимое пакетов.

> **Область действия и ограничения:** Требуется привилегированное развёртывание на Linux-хосте,
> который видит SIP-сигнализацию по IPv4/UDP и связанный RTP/RTCP-трафик. Сервис не сохраняет пакеты
> или аудио и не предоставляет интерфейс поиска пакетов. Метрики QoE описывают только трафик,
> видимый сенсору, и не гарантируют сквозное качество для абонента.

[![Go Test](https://github.com/aibudaevv/sip-exporter/actions/workflows/go.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/go.yml)
[![Go Vulncheck](https://github.com/aibudaevv/sip-exporter/actions/workflows/vulncheck.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/vulncheck.yml)
[![Container Scan](https://github.com/aibudaevv/sip-exporter/actions/workflows/trivy.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/trivy.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/aibudaevv/sip-exporter)](https://goreportcard.com/report/github.com/aibudaevv/sip-exporter)
[![Docker Pulls](https://img.shields.io/docker/pulls/frzq/sip-exporter)](https://hub.docker.com/r/frzq/sip-exporter)
[![GitHub Release](https://img.shields.io/github/v/release/aibudaevv/sip-exporter)](https://github.com/aibudaevv/sip-exporter/releases)
[![License](https://img.shields.io/badge/license-AGPL--3.0-blue)](https://github.com/aibudaevv/sip-exporter/blob/master/LICENSE)
[![Issues](https://img.shields.io/github/issues/aibudaevv/sip-exporter)](https://github.com/aibudaevv/sip-exporter/issues)

## Содержание

- [Возможности](#возможности)
- [Быстрый старт](#быстрый-старт)
- [Проверка установки](docs/INSTALLATION.ru.md)
- [Технология](#технология)
- [Архитектура](#архитектура)
- [Производительность](#производительность)
- [Установка](#установка)
- [Топология развёртывания](#топология-развёртывания)
- [Метрики](#метрики)
- [Детекция фрода](#детекция-фрода)
- [Безопасность](docs/SECURITY.ru.md)
- [Разработка](#разработка)
- [Нагрузочное тестирование](#нагрузочное-тестирование)
- [Алертинг](#алертинг)
- [Совместимость с хранилищами метрик](#совместимость-с-хранилищами-метрик)
- [Поддержка](#поддержка)
- [Лицензия](#лицензия)
- [Changelog](#changelog)

## Возможности

- 🌐 **Мониторинг нескольких интерфейсов** — захват SIP/RTP с нескольких NIC одновременно, каждый помечается лейблом `iface`
- ⚡ **Фильтрация в ядре** — eBPF socket filter отбирает нужный трафик до разбора в userspace
- 🐳 **Один контейнер** — никаких внешних зависимостей
- 🔧 **Настраиваемые SIP-порты** — мониторинг нестандартных портов через переменные окружения
- 📈 **Нативный Prometheus** — стандартный эндпоинт `/metrics`
- 🏷️ **Метрики по операторам** — CIDR-разрешение carrier для SIP-семейств с carrier-контекстом
- 🏷️ **Метрики по типам устройств** — классификация User-Agent для SIP-семейств с контекстом устройства
- 🌍 **Гео-обогащение** — лейблы `source_country` (GeoIP) и `destination_country` (E.164 prefix) в SIP-метриках
- 🔀 **Направление трафика** — лейбл `inbound`/`outbound` в SIP- и RTP-метриках трафика определяется через kernel `pkttype`
- 📞 **Качество голоса (RFC 6035)** — MOS, джиттер, потери пакетов из SIP PUBLISH/NOTIFY
- 🎧 **Анализ RTP-медиа** — джиттер, потери, MOS (E-model G.107) и Packet Delay Variation (PDV, per-packet) из RTP-потоков, скоррелированных с SIP-диалогами
- 📊 **RTCP-качество от эндпоинтов** — потери, джиттер и round-trip time (RTT) из RTCP SR/RR (RFC 3550), корреляция по SSRC; поддерживает rtcp-mux (RFC 5761), явный `a=rtcp` (RFC 3605) и legacy port+1
- 🛡️ **Детекция фрода** — сигналы сканирования регистраций, всплесков INVITE, перехвата аккаунтов (смена страны) и False Answer Supervision (FAS)

## Быстрый старт

Скопируйте [production Compose-пример с закреплённой версией](examples/docker-compose.production.yml)
на хост. Задайте `SIP_EXPORTER_INTERFACE` — интерфейс, через который проходят SIP-сигнализация и RTP-медиа.

```bash
cp examples/docker-compose.production.yml docker-compose.yml
SIP_EXPORTER_INTERFACE=eth0 docker compose up -d
curl http://localhost:10047/metrics
```

Пример содержит pinned image, политику перезапуска, healthcheck, read-only filesystem и все
перечисленные ниже runtime-параметры с их значениями по умолчанию.

Метрики доступны на `http://localhost:10047/metrics`.

**Миграция порта:** новые установки используют `10047`. Существующие установки могут сохранить
предыдущий порт через `SIP_EXPORTER_HTTP_PORT=2112`, согласовав с ним scrape URL и healthcheck.

**Первый полезный дашборд:** пройдите [runbook проверки установки](docs/INSTALLATION.ru.md):
он последовательно проверяет health, scrape status, SIP, видимость SDP/RTP и drops до импорта Grafana.

## Технология

Сервис использует eBPF (extended Berkeley Packet Filter), подключённый к сокетам `AF_PACKET` для отбора IPv4 SIP-пакетов по UDP (по умолчанию порт 5060) на L4. SIP по TCP или TLS не захватывается. Отобранные пакеты передаются в userspace через сокет для обработки на Go.

## Архитектура
```
SIP + RTP-трафик → NIC → eBPF socket filter → AF_PACKET socket → Go poller → SIP-парсер + RTP-трекер → Prometheus
```

## Производительность

Проверенный release-профиль включает full-call трафик на 1 000 CPS под лимитами 1 CPU / 128 MiB,
full-call трафик с параллельными Prometheus scrape на 1 800 CPS под лимитами 2 CPU / 256 MiB и
десятиминутный soak на 500 CPS под лимитами 1 CPU / 128 MiB. Это результаты конкретных профилей
приёмки, а не универсальная гарантия production sizing.

Измеренные сценарии, проверки целостности, окружение и команды воспроизведения описаны в
[docs/BENCHMARK.md](./docs/BENCHMARK.md).

## Установка

```bash
docker pull frzq/sip-exporter:1.11.0
```

### Конфигурация

Переменные окружения:
* `SIP_EXPORTER_INTERFACE` — один или несколько сетевых интерфейсов через запятую (обязательно). Примеры: `eth0`, `eth0,eth1,eth2`.
* `SIP_EXPORTER_HTTP_PORT` — HTTP-порт для Prometheus (по умолчанию 10047)
* `SIP_EXPORTER_LOGGER_LEVEL` — уровень логирования: `error`, `info` или `debug` (по умолчанию `info`). Debug-логи содержат raw SIP payload; используйте их только в контролируемой среде. Любое другое значение сейчас включает debug-логирование.
* `SIP_EXPORTER_SIP_PORTS` — один или несколько SIP-портов через запятую (по умолчанию 5060; до 3 на интерфейс). Через `;` — наборы для каждого интерфейса: `5060,5062;5060,5061`.
* `SIP_EXPORTER_OBJECT_FILE_PATH` — путь к eBPF-объектному файлу (по умолчанию /usr/local/bin/sip.o)
* `SIP_EXPORTER_CARRIERS_CONFIG` — путь к YAML-конфигурации carriers (опционально, см. [`examples/carriers.yaml`](examples/carriers.yaml))
* `SIP_EXPORTER_USER_AGENTS_CONFIG` — путь к YAML-конфигурации user-agents (опционально, см. [`examples/user_agents.yaml`](examples/user_agents.yaml))
* `SIP_EXPORTER_RTP_STREAM_TTL` — время жизни простаивающего RTP-потока до удаления, таймаут RFC 3550 §6.3.5 (по умолчанию 30s)
* `SIP_EXPORTER_IGNORE_OUTGOING` — только для loopback/тестов: подавляет дубликаты TX-пакетов на `lo` (по умолчанию false, НЕ включать в production)
* `SIP_EXPORTER_GEOIP_COUNTRY_DB` — путь к MaxMind GeoLite2-Country.mmdb для лейбла `source_country` (опционально)
* `SIP_EXPORTER_LOCAL_COUNTRY_CODE` — код страны ISO alpha-2 для локальных номеров без международного префикса в `destination_country` (опционально, напр. `RU`)
* `SIP_EXPORTER_HOST_LABELS` — включить лейблы `caller_host`/`called_host` в INVITE-метриках (по умолчанию `false`; opt-in — неограниченная кардинальность, включайте только в доверенных деплоях с ограниченным числом узлов)
* `SIP_EXPORTER_SESSIONS_LIMITS` — путь к YAML-конфигурации лимитов сессий (опционально, per-carrier лимиты параллельных сессий и метрики утилизации)
* `SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD` — сканирование регистраций: количество уникальных аккаунтов (AoR), успешно зарегистрированных (200 OK) с одного source IP, для срабатывания сигнала (по умолчанию 10)
* `SIP_EXPORTER_FRAUD_REGISTER_SCAN_WINDOW` — скользящее окно сканирования регистраций (по умолчанию 60s)
* `SIP_EXPORTER_FRAUD_INVITE_BURST_THRESHOLD` — INVITE burst: количество INVITE с одного источника для срабатывания сигнала (по умолчанию 100)
* `SIP_EXPORTER_FRAUD_INVITE_BURST_WINDOW` — скользящее окно INVITE burst (по умолчанию 60s)
* `SIP_EXPORTER_FRAUD_FAS_THRESHOLD` — False Answer Supervision: базовое ожидание sweep-path после 200 OK без answer-side RTP (по умолчанию 10s; BYE-path использует независимый floor 3s)
* `SIP_EXPORTER_TELEMETRY` — анонимная телеметрия использования, отключается значением `false` (по умолчанию true)

Контейнер должен запускаться с `--privileged` и `--network host` (eBPF требует `CAP_BPF` и доступ к сетевому интерфейсу). Подробнее о безопасности — в [Безопасность](docs/SECURITY.ru.md).

> ⚠️ **Особенность мульти-интерфейса:** не указывайте интерфейсы, которые видят один и тот же трафик (bond parent + child, bridge + member, VLAN parent + subinterface, дублирующие SPAN-порты). Это приведёт к задвоению метрик. Если сомневаетесь — указывайте только физические NIC.

## Топология развёртывания

Устанавливайте sip-exporter на хост, через который проходят **и SIP-сигналинг, и RTP-медиа**. Он захватывает пакеты с того сетевого интерфейса, к которому подключён, а лейбл `direction` опирается на то, что ядро видит пакеты как адресованные этому хосту — поэтому хост должен владеть этими IP, а не получать их через зеркало.

Покрытие зависит от того, что именно видит хост:

- **Только SIP** (сигналинг проходит, медиа — нет) → только SIP-метрики; RTP-метрики остаются пустыми.
- **Только RTP** (медиа проходит, сигналинг — нет) → экспортёр не сможет скоррелировать потоки с диалогами, так как RTP-эндпоинты он узнаёт из SDP внутри SIP-сообщений. Размещайте там, где виден и сигналинг.
- **SIP + RTP** → полные метрики.

### Матрица поддержки захвата

| Сценарий | Статус | Эксплуатационное требование или ограничение |
|----------|--------|---------------------------------------------|
| SIP и RTP/RTCP по IPv4/UDP | Поддерживается | Сенсор должен видеть сигналинг и оба направления медиа на одном пути вызова. |
| `rtcp-mux`, SDP `a=rtcp` или legacy RTP/RTCP port+1 | Поддерживается | Финальные IPv4 endpoint и port должны присутствовать в видимом экспортёру SDP. |
| NAT/SBC со стабильными media-endpoint'ами из SDP | Поддерживается | Symmetric RTP с remapping source-port обучается после RTP, скоррелированного по destination, если source IP совпадает с SDP peer. Смена source IP и неоднозначные shared endpoint'ы не обучаются. |
| SIP по TCP/TLS, IPv6 SIP/SDP/media или фрагментированный UDP | Не поддерживается | Путь захвата и SDP работает только с IPv4/UDP и не собирает IP-фрагменты. |
| RTP без видимого SDP или смена endpoint'ов ICE/TURN после SDP | Не поддерживается | У kernel filter нет endpoint'а для регистрации, поэтому медиа отбрасывается. |
| SPAN/TAP или иной зеркалированный трафик | Не поддерживается для QoE/direction | Пакеты могут быть захвачены, но `direction` недостоверен, поскольку сенсор не владеет IP трафика. Разворачивайте на forwarding host. |

## Метрики

Все метрики доступны на `/metrics` в формате Prometheus. Большинство SIP-метрик содержит `carrier`, `ua_type`, `source_country` и `direction`; специализированные семейства fraud, capacity, traffic, RTP/RTCP и самомониторинга используют точные схемы из [матрицы лейблов](docs/METRICS.ru.md#лейблы). Raw INVITE-метрики дополнительно содержат `destination_country`, opt-in `caller_host`/`called_host` и capture-interface `iface`. Экспортер предоставляет:

- **Счётчики трафика** — типы SIP-запросов (INVITE, re-INVITE, BYE, REGISTER и т.д.) и коды ответов (100–606)
- **Активные сессии** — количество активных SIP-диалогов в реальном времени
- **Метрики RFC 6076** — SER, SEER, ISA, SCR, ASR, NER, RRD, SPD, TTR, PDD, PBD
- **Метрики качества голоса RFC 6035** — NLR, JDR, BLD, GLD, RTD, ESD, IAJ, MAJ, MOSLQ, MOSCQ, RLQ, RCQ, RERL
- **Метрики RTP-медиа** — `rtp_packets_total`, `rtp_packets_lost_total`, `rtp_jitter_milliseconds`, `rtp_pdv_milliseconds` (per-packet Packet Delay Variation), `rtp_mos_score`, `rtp_active_streams` (лейблы: `carrier,ua_type,codec,source_country,direction`)
- **Метрики RTCP-качества** — гистограммы качества и cumulative loss используют `carrier,ua_type,codec,source_country,direction`; в `rtcp_reports_total` вместо `codec` используется `type`, а `rtcp_orphan_reports_total` не имеет лейблов
- **Фрод-сигналы** — `fas_calls_total` (False Answer Supervision: 200 OK без RTP в течение threshold), `register_scan_total`, `invite_burst_total`, `register_country_change_total`
- **Диагностика** — `sip_retransmission_total` (ретрансмиссии по SIP Timer A), `rtp_out_of_order_total` (нарушение порядка RTP-пакетов), `short_calls_total` (звонки короче 20/60/180 секунд)

Полный справочник с формулами, примерами и привязкой к RFC: [docs/METRICS.ru.md](docs/METRICS.ru.md)

### Метрики по операторам (Carrier)

Если ваша SIP-инфраструктура обрабатывает трафик от нескольких операторов (телефонные провайдеры, SIP-транки, PBX-кластеры), вам нужно видеть метрики **по каждому оператору**, а не в сумме.

Функция carrier решает эту задачу, связывая IP-подсети с именами операторов через YAML-конфигурацию. Call-метрики — количество INVITE, SER, активные сессии, задержка RRD — получают лейбл `carrier`, что позволяет строить отдельные дашборды Grafana и алерты для каждого оператора.

**Как это работает:**

Экспортер анализирует **source IP** каждого SIP-запроса и сопоставляет его с CIDR-подсетями из конфигурации. Когда UAC с адресом `10.1.5.20` отправляет INVITE, экспортер определяет, что `10.1.5.20` входит в подсеть `10.1.0.0/16`, заданную для carrier "telecom-alpha", и помечает все метрики этого звонка — сам INVITE, ответ 200 OK, BYE и даже истечение диалога — лейблом `carrier="telecom-alpha"`.

Это означает:
- INVITE от `10.1.5.20` → метрики с `carrier="telecom-alpha"`
- INVITE от `192.168.11.3` → метрики с `carrier="telecom-beta"`
- INVITE от `8.8.8.8` (не входит ни в одну подсеть) → метрики с `carrier="other"`

**Настройка:**

Добавьте в production Compose read-only mount для своей [carrier-конфигурации](examples/carriers.yaml)
и переменную `SIP_EXPORTER_CARRIERS_CONFIG=/etc/sip-exporter/carriers.yaml`.

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
sip_exporter_invite_total{carrier="telecom-alpha",ua_type="other",source_country="unknown",direction="inbound",destination_country="unknown",caller_host="",called_host="",iface="ens3"} 1523
sip_exporter_ser{carrier="telecom-alpha",ua_type="other",source_country="unknown",direction="inbound"} 95.2
sip_exporter_ser{carrier="telecom-beta",ua_type="other",source_country="unknown",direction="inbound"} 87.4
sip_exporter_ser{carrier="other",ua_type="other",source_country="unknown",direction="inbound"} 0.0
```

**Важно знать:**

- Carrier определяется в момент **запроса** (INVITE/REGISTER/OPTIONS), а не ответа. Если carrier-A отправил INVITE, а carrier-B ответил 200 OK — все метрики относятся к carrier-A, инициатору звонка
- Если source IP не входит ни в одну CIDR-подсеть, проверяется destination IP. Если и он не найден → `carrier="other"`
- При пересекающихся CIDR **побеждает первое совпадение** — указывайте более специфичные подсети перед широкими
- Без файла конфигурации метрики с `carrier` используют `carrier="other"` — ничего не ломается
- Для каждого carrier можно указать несколько CIDR, количество carrier не ограничено
- CIDR-нотация обязательна — обычные IP-адреса без `/` не принимаются. Используйте `/32` для одного хоста, например `"10.226.97.5/32"` вместо `"10.226.97.5"`

Полный пример конфигурации: [`examples/carriers.yaml`](examples/carriers.yaml)

### Метрики по типам устройств (User-Agent)

Если вам нужно видеть метрики **по типам SIP-устройств** — IP-телефоны, софтфоны, SBC — классификация User-Agent добавляет `ua_type` к семействам с контекстом устройства.

Экспортер читает заголовок `User-Agent` из каждого SIP-запроса и сопоставляет его с regex-паттернами из YAML-конфигурации. Call-метрики — количество INVITE, SER, активные сессии, длительность SPD — получают `ua_type`, что позволяет строить отдельные дашборды Grafana и алерты для каждого семейства устройств.

**Как это работает:**

Экспортер парсит заголовок `User-Agent` каждого SIP-запроса и сопоставляет его с regex-паттернами из конфигурации. Когда телефон с `User-Agent: Yealink SIP-T46S 66.15.0.10` отправляет INVITE, экспортер находит совпадение с паттерном `^Yealink` и помечает все метрики этого звонка лейблом `ua_type="yealink"`.

Это означает:
- INVITE от телефона Yealink → метрики с `ua_type="yealink"`
- INVITE от телефона Grandstream → метрики с `ua_type="grandstream"`
- INVITE с неизвестным User-Agent → метрики с `ua_type="other"`

**Настройка:**

Добавьте в production Compose read-only mount для своей [User-Agent-конфигурации](examples/user_agents.yaml)
и переменную `SIP_EXPORTER_USER_AGENTS_CONFIG=/etc/sip-exporter/user_agents.yaml`.

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
sip_exporter_invite_total{carrier="telecom-alpha",ua_type="yealink",source_country="unknown",direction="inbound",destination_country="unknown",caller_host="",called_host="",iface="ens3"} 1523
sip_exporter_ser{carrier="telecom-alpha",ua_type="yealink",source_country="unknown",direction="inbound"} 95.2
sip_exporter_ser{carrier="telecom-alpha",ua_type="grandstream",source_country="unknown",direction="inbound"} 87.4
sip_exporter_ser{carrier="telecom-alpha",ua_type="other",source_country="unknown",direction="inbound"} 0.0
```

**Важно знать:**

- Тип устройства определяется в момент **запроса** (INVITE/REGISTER/OPTIONS), используя тот же механизм трекера, что и carrier. Ответы наследуют `ua_type` из трекера запроса, а не из собственных заголовков ответа
- Заголовок `User-Agent` извлекается из всех SIP-пакетов, но SIP-ответы обычно используют заголовок `Server`, поэтому на практике только запросы дают осмысленную классификацию
- Если ни один паттерн не совпал → `ua_type="other"`
- При пересечении паттернов **побеждает первое совпадение** — указывайте специфичные паттерны перед широкими
- Без файла конфигурации метрики с `ua_type` используют `ua_type="other"` — ничего не ломается
- Паттерны нечувствительны к регистру при использовании префикса `(?i)`
- Работает **совместно с carrier** — базовые и call-level SIP-метрики имеют оба лейба для двумерного анализа

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
| `source_country` | GeoIP-лукап source IP (MaxMind GeoLite2-Country) | Базовые/call-level SIP-, RTP- и скоррелированные RTCP-метрики |
| `destination_country` | Префикс E.164 номера (embedded, без БД) | Только INVITE-метрики |

**Разрешение source_country:**
1. `carrier.country` — опциональное поле в `carriers.yaml`, приоритет над GeoIP (оператор знает лучше)
2. `GeoIP(srcIP)` — лукап по базе MaxMind GeoLite2-Country
3. `"unknown"` — fallback при отсутствии обоих

**destination_country** не требует **никакой базы** — таблица префиксов встроена в бинарник (Google libphonenumber, Apache 2.0). Для локальных номеров без международного префикса укажите `SIP_EXPORTER_LOCAL_COUNTRY_CODE`.

**caller_host / called_host** **отключены по умолчанию** (`SIP_EXPORTER_HOST_LABELS=false`). Они раскрывают host-часть SIP-URI `From`/`To` в `invite_total` / `invite_200_total`. Поскольку число уникальных узлов не ограничено, они opt-in: включайте (`SIP_EXPORTER_HOST_LABELS=true`) только в доверенных деплоях с ограниченным числом узлов, иначе они могут раздуть память Prometheus. См. [Безопасность > Данные в лейблах Prometheus](docs/SECURITY.ru.md#данные-в-лейблах-prometheus).

**Настройка:**

Следуйте [руководству GeoIP](docs/geoip.ru.md), чтобы добавить read-only mount базы и
`SIP_EXPORTER_GEOIP_COUNTRY_DB` в production Compose.

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

Помимо сигналинга SIP, экспортер захватывает RTP-потоки для оценки transport quality в **точке захвата** (джиттер, разрывы sequence number и E-model MOS). RTP-потоки **скоррелированы с SIP-диалогами**: когда `200 OK` на INVITE несёт SDP, экспортер регистрирует согласованные media-endpoint'ы и отслеживает соответствующие RTP-потоки до BYE (или истечения Session-Expires). Поэтому RTP-метрики наследуют лейблы диалога `carrier`, `ua_type`, `source_country` и `direction`, а также согласованный `codec`.

Производимые метрики:

| Метрика | Тип | Описание |
|--------|------|-------------|
| `sip_exporter_rtp_packets_total` | counter | количество RTP-пакетов |
| `sip_exporter_rtp_packets_lost_total` | counter | потерянные пакеты (по seq-gap RFC 3550) |
| `sip_exporter_rtp_jitter_milliseconds` | histogram | межпакетный джиттер (RFC 3550 A.8) |
| `sip_exporter_rtp_mos_score` | histogram | MOS-LQ по E-model ITU-T G.107 (1.0–4.5) |
| `sip_exporter_rtp_active_streams` | gauge | активные RTP-потоки, скоррелированные с диалогами |

**Приватность:** RTP-пакеты копируются в userspace снимком до 64 байт, поэтому вместе с заголовками может попасть небольшой префикс payload. Приложение разбирает только фиксированный 12-байтовый RTP-заголовок и не инспектирует и не сохраняет аудио. Совпавшие RTCP compound-пакеты копируются до Ethernet MTU, чтобы разобрать report blocks.

Захват RTP всегда включён. RTP без скоррелированного SIP-диалога (без замеченного SDP-обмена) отбрасывается, поэтому учитывается только медиа для отслеживаемых звонков.

eBPF-фильтр использует **SDP-driven RTP-детекцию**: media-endpoint'ы (IP:порт), извлечённые из SDP в INVITE 200 OK, помещаются в BPF LRU hash map. Через ядро проходят только UDP-пакеты, совпадающие с зарегистрированным endpoint'ом — весь остальной UDP отбрасывается. Это исключает ложные срабатывания от постороннего UDP-трафика на публичных IP-адресах.

**Как интерпретировать QoE:** RTP loss, jitter, PDV и MOS — это наблюдение пакетов, дошедших до данного сенсора; они не являются субъективной оценкой абонента и не доказывают end-to-end деградацию. RTCP SR/RR добавляет собственную RTP-статистику приёмника для коррелированного SSRC, но охватывает только репорты и медиа, видимые сенсору. Перед реакцией на QoE-алерты проверьте `sip_exporter_socket_packets_dropped_total`, `sip_exporter_rtp_dropped_total` и топологию захвата выше.

```PromQL
# Средний MOS за окно 5m (по кодекам)
sum by (codec) (rate(sip_exporter_rtp_mos_score_sum[5m]))
  / sum by (codec) (rate(sip_exporter_rtp_mos_score_count[5m]))

# Доля потерь по операторам
sum by (carrier) (rate(sip_exporter_rtp_packets_lost_total[5m]))
  / (
      sum by (carrier) (rate(sip_exporter_rtp_packets_total[5m]))
      + sum by (carrier) (rate(sip_exporter_rtp_packets_lost_total[5m]))
    )
```

Полный справочник по RTP-метрикам, формулы и разрешение лейблов — в [docs/METRICS.ru.md](docs/METRICS.ru.md).

## Детекция фрода

Экспортёр выдаёт Prometheus-сигналы для распространённых паттернов toll fraud:

- **Сканирование регистраций** — много уникальных аккаунтов (AoR), зарегистрированных с одного IP за короткое окно
- **Всплеск INVITE** — аномальная частота INVITE с одного источника
- **Перехват аккаунта** — тот же AOR успешно перерегистрируется из страны, отличной от его активной регистрации

Полная настройка, метрики и алерты: [docs/fraud-detection.ru.md](docs/fraud-detection.ru.md)

## Разработка

### Требования
- Go 1.26.6+
- Clang/LLVM (для компиляции eBPF)
- Ядро Linux с поддержкой eBPF
- Права root (требуются для eBPF и packet socket)

### Покрытие тестами

Покрытие меняется вместе с набором тестов и не фиксируется в этом документе. Актуальное покрытие
пакетов можно получить командой `go test -cover ./internal/... ./pkg/...`.

Набор тестов:
- **Unit-тесты** — MC/DC-ориентированное покрытие бизнес-логики
- **Табличные E2E-тесты** — реальный SIP-трафик через SIPp + testcontainers-go, покрывающий RFC 6076, RFC 6035, RTP, RTCP, fraud и multi-interface сценарии
- **Нагрузочные тесты** — пропускная способность PPS, VQ-отчёты, параллельные сессии, стабильность памяти, GC-паузы и latency скрейпа

## Нагрузочное тестирование

В [BENCHMARK.md](./docs/BENCHMARK.md) приведены проверенный release-профиль нагрузки, методика,
пороги приёмки и ограничения применимости результатов.

## Алертинг

В репозиторий включён Grafana-дашборд и документированные примеры правил алертов Prometheus.

**Grafana-дашборд** — импорт вручную:

1. Grafana → Dashboards → Import
2. Загрузите [`examples/grafana-dashboard.json`](examples/grafana-dashboard.json)
3. Выберите datasource Prometheus или VictoriaMetrics

Дашборд содержит: счётчики трафика, разбивку SIP-запросов/ответов, активные сессии, метрики RFC 6076 (SER, SEER, ISA, SCR, NER), регистрации (активные, success ratio, ошибки по кодам, фрод-сигналы), анализ RTP-медиа (активные потоки, rate пакетов, loss rate, MOS, jitter по кодекам), метрики качества голоса RFC 6035 (MOS, jitter, потери пакетов), гистограммы задержек (RRD, TTR, PDD, SPD, ORD, LRD, PBD), метрики качества сессий (ISS, ASR, SDC), диагностику (ретрансмиссии SIP, короткие звонки) и системные ошибки.

Полный гайд по алертингу: правила Prometheus, конфиги Alertmanager (Slack/PagerDuty/Email), настройка порогов — [`docs/ALERTING.ru.md`](docs/ALERTING.ru.md)

## Совместимость с хранилищами метрик

SIP-Exporter экспортирует метрики в формате Prometheus exposition, совместимом с:

- **Prometheus** — pull-based мониторинг
- **VictoriaMetrics** — Prometheus-совместимая TSDB
- **Grafana Cloud** — облачная наблюдаемость
- **Любой Prometheus-совместимый скрейпер** — эндпоинт `/metrics` следует стандартному формату

## Поддержка

Для поддержки, сообщений об ошибках и запросов функций используйте [GitHub Issues](https://github.com/aibudaevv/sip-exporter/issues).

## Лицензия

Проект лицензирован под **GNU Affero General Public License v3.0 (AGPL-3.0)**.

Полный текст: [LICENSE](LICENSE).

### Лицензии сторонних данных

- **MaxMind GeoLite2** (`source_country`) — пользователь скачивает БД отдельно. Использование, атрибуция, распространение и обязанность обновления регулируются [GeoLite EULA](https://www.maxmind.com/en/geolite/eula) и включёнными в неё условиями CC BY-SA 4.0.
- **Google libphonenumber** (`destination_country`) — [Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0). Данные префиксов E.164 встроены в бинарник при компиляции.

### Коммерческое использование
- ✅ Бесплатно для личного и образовательного использования
- ✅ Бесплатно для коммерческого использования с условиями
- ⚠️ Если модифицированная версия доступна пользователям по сети, AGPL-3.0 §13 требует предложить этим пользователям Corresponding Source
- 📧 Для коммерческого лицензирования без требований AGPL — свяжитесь с автором

## Changelog
История версий — на странице [GitHub Releases](https://github.com/aibudaevv/sip-exporter/releases).
