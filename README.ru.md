# SIP-exporter

**[EN](README.md)** | **[RU](README.ru.md)**

Высокопроизводительный мониторинг SIP-телефонии на базе eBPF. Захватывает пакеты напрямую в ядре Linux и экспортирует метрики в Prometheus-совместимые системы (Prometheus, VictoriaMetrics и др.).

[![Go Test](https://github.com/aibudaevv/sip-exporter/actions/workflows/go.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/go.yml)
[![Go Vulncheck](https://github.com/aibudaevv/sip-exporter/actions/workflows/vulncheck.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/vulncheck.yml)
[![Container Scan](https://github.com/aibudaevv/sip-exporter/actions/workflows/trivy.yml/badge.svg)](https://github.com/aibudaevv/sip-exporter/actions/workflows/trivy.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/aibudaevv/sip-exporter)](https://goreportcard.com/report/github.com/aibudaevv/sip-exporter)
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
- [Интеграция](#интеграция)
- [Лицензия](#лицензия)
- [Changelog](#changelog)

## Возможности

- ⚡ **Минимальная нагрузка** — фильтрация пакетов через eBPF в пространстве ядра
- 🐳 **Один контейнер** — никаких внешних зависимостей
- 🔧 **Настраиваемые SIP-порты** — мониторинг нестандартных портов через переменные окружения
- 📈 **Нативный Prometheus** — стандартный эндпоинт `/metrics`
- 🏷️ **Метрики по операторам** — разрешение carrier на основе CIDR для всех SIP-метрик

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
      # Опционально: метки carrier для мониторинга по операторам
      # - SIP_EXPORTER_CARRIERS_CONFIG=/etc/sip-exporter/carriers.yaml
    # volumes:
    #   - ./examples/carriers.yaml:/etc/sip-exporter/carriers.yaml:ro
```

```bash
docker compose up -d
curl http://localhost:2112/metrics
```

Метрики доступны на `http://localhost:2112/metrics`.

## Технология

Сервис использует eBPF (extended Berkeley Packet Filter), подключённый к сокетам `AF_PACKET` для перехвата SIP-пакетов (UDP/5060-5061) на L4 без накладных расходов iptables/nftables или userspace-демонов вроде tcpdump. Отфильтрованные пакеты передаются в userspace через сокет для эффективной обработки на Go.

## Архитектура
```
SIP-трафик → NIC → eBPF socket filter → AF_PACKET socket → Go poller → SIP-парсер → Prometheus
```

## Производительность

Нулевая потеря пакетов до **2 000 CPS** (~24 000 PPS) при полном жизненном цикле SIP-диалога, **<15% CPU** и **~15 МБ RAM**. GC stop-the-world паузы менее **1 мс** — в 400 раз меньше ёмкости буфера сокета, что гарантирует отсутствие потерь пакетов из-за GC. Память стабильна при длительной нагрузке, утечек не обнаружено.

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
* `SIP_EXPORTER_INTERFACE` — сетевой интерфейс (обязательно)
* `SIP_EXPORTER_HTTP_PORT` — HTTP-порт для Prometheus (по умолчанию 2112)
* `SIP_EXPORTER_LOGGER_LEVEL` — уровень логирования (по умолчанию info)
* `SIP_EXPORTER_SIP_PORT` — SIP-порт (по умолчанию 5060)
* `SIP_EXPORTER_SIPS_PORT` — SIPS-порт (по умолчанию 5061)
* `SIP_EXPORTER_OBJECT_FILE_PATH` — путь к eBPF-объектному файлу (по умолчанию /usr/local/bin/sip.o)
* `SIP_EXPORTER_CARRIERS_CONFIG` — путь к YAML-конфигурации carriers (опционально, см. [`examples/carriers.yaml`](examples/carriers.yaml))

Контейнер должен запускаться с `--privileged` и `--network host` (eBPF требует `CAP_BPF` и доступ к сетевому интерфейсу). Подробнее о безопасности — в [Безопасность](docs/SECURITY.ru.md).

## Метрики

Все метрики доступны на `/metrics` в формате Prometheus. Все SIP-метрики содержат лейбл `carrier` для разбивки по операторам (настраивается через CIDR-маппинг). Экспортер предоставляет:

- **Счётчики трафика** — типы SIP-запросов (INVITE, BYE, REGISTER и т.д.) и коды ответов (100–603)
- **Активные сессии** — количество активных SIP-диалогов в реальном времени
- **Метрики RFC 6076** — SER, SEER, ISA, SCR, ASR, NER, RRD, SPD, TTR
- **Расширенные метрики** — ISS, SDC, ORD, LRD

Полный справочник с формулами, примерами и привязкой к RFC: [docs/METRICS.md](docs/METRICS.md)

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
      - ./carriers.yaml:/etc/sip-exporter/carriers.yaml:ro
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
sip_exporter_invite_total{carrier="telecom-alpha"}  1523
sip_exporter_ser{carrier="telecom-alpha"}            95.2
sip_exporter_ser{carrier="telecom-beta"}             87.4
sip_exporter_ser{carrier="other"}                     0.0
```

**Важно знать:**

- Carrier определяется в момент **запроса** (INVITE/REGISTER/OPTIONS), а не ответа. Если carrier-A отправил INVITE, а carrier-B ответил 200 OK — все метрики относятся к carrier-A, инициатору звонка
- Если source IP не входит ни в одну CIDR-подсеть, проверяется destination IP. Если и он не найден → `carrier="other"`
- При пересекающихся CIDR **побеждает первое совпадение** — указывайте более специфичные подсети перед широкими
- Без файла конфигурации все метрики получают `carrier="other"` — ничего не ломается
- Для каждого carrier можно указать несколько CIDR, количество carrier не ограничено
- CIDR-нотация обязательна — обычные IP-адреса без `/` не принимаются. Используйте `/32` для одного хоста, например `"10.226.97.5/32"` вместо `"10.226.97.5"`

Полный пример конфигурации: [`examples/carriers.yaml`](examples/carriers.yaml)

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
- **55 E2E-тестов** — реальный SIP-трафик через SIPp + testcontainers-go, валидация всех метрик RFC 6076
- **8 нагрузочных тестов** — пропускная способность PPS, параллельные сессии, стабильность памяти, GC-паузы, latency скрейпа

## Нагрузочное тестирование

Результаты: **0% потерь пакетов при 2 000 CPS (28 000 PPS)**.

Подробности в [BENCHMARK.md](./docs/BENCHMARK.md) — результаты, методология и заметки по оптимизации.

## Интеграция

### Алертинг

Готовые примеры алертов в [ALERTING.md](./docs/ALERTING.md):

- **Правила Prometheus** — critical, warning и info-алерты для SER, ISA, RRD и других метрик
- **Grafana дашборд** — готовый к импорту JSON с фильтрацией по carrier
- **Примеры Alertmanager** — интеграция со Slack, PagerDuty и Email
- **Best practices** — интервалы скрейпинга, хранение данных, настройка порогов

### Grafana дашборд

Импортируйте готовый дашборд в Grafana:

1. Grafana → Dashboards → Import
2. Загрузите `examples/grafana-dashboard.json` или вставьте JSON
3. Выберите datasource Prometheus или VictoriaMetrics

Дашборд содержит: счётчики трафика, разбивку SIP-запросов/ответов, активные сессии, метрики RFC 6076 (SER, SEER, ISA, SCR, NER), гистограммы задержек (RRD, TTR, SPD, ORD, LRD), метрики качества (ISS, ASR, SDC) и системные ошибки.

Файл дашборда: [`examples/grafana-dashboard.json`](examples/grafana-dashboard.json)

### Совместимость с хранилищами метрик

SIP-Exporter экспортирует метрики в формате Prometheus exposition, совместимом с:

- **Prometheus** — pull-based мониторинг
- **VictoriaMetrics** — высокопроизводительная TSDB
- **Grafana Cloud** — облачная наблюдаемость
- **Любой Prometheus-совместимый скрейпер** — эндпоинт `/metrics` следует стандартному формату

## Лицензия

Проект лицензирован под **GNU Affero General Public License v3.0 (AGPL-3.0)**.

Полный текст: [LICENSE](LICENSE).

### Коммерческое использование
- ✅ Бесплатно для личного и образовательного использования
- ✅ Бесплатно для коммерческого использования с условиями
- ⚠️ При модификации и запуске как публичный сервис — необходимо открыть исходный код модификаций
- 📧 Для коммерческого лицензирования без требований AGPL — свяжитесь с автором

## Changelog
См. [CHANGELOG.md](CHANGELOG.md) для истории версий.
