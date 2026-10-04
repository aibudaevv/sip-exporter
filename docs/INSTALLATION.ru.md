# Проверка установки и runbook пустого дашборда

Используйте эту инструкцию после развёртывания [production Compose-примера](../examples/docker-compose.production.yml). Она проверяет путь от экспортёра до Grafana. Диагностические запросы ниже не выводят SIP-содержимое; логи могут содержать Call-ID и другие чувствительные данные — не публикуйте их без удаления таких данных.

## Первый полезный дашборд

1. Запустите экспортёр на хосте, который видит и SIP-сигнализацию, и RTP-медиа.
2. Проверьте контейнер и его endpoint `/health`.
3. Настройте любой Prometheus-совместимый scraper на `http://<host>:10047/metrics`.
4. Убедитесь, что target имеет статус `UP`, затем совершите один тестовый вызов по наблюдаемому пути.
5. Импортируйте [`examples/grafana-dashboard.json`](../examples/grafana-dashboard.json) и выберите datasource scraper'а.

Экспортёр поддерживает SIP и RTP по IPv4/UDP. SIP по TCP/TLS, IPv6, фрагментированный UDP, QoE через SPAN/TAP и RTP без видимого SDP не входят в контракт захвата; см. [топологию развёртывания](../README.ru.md#топология-развёртывания).

> **Миграция порта:** новые установки используют `10047`. Существующая установка может сохранить
> предыдущий порт через `SIP_EXPORTER_HTTP_PORT=2112`, согласовав с ним scrape URL и healthcheck.

## Матрица поддержки

| Симптом | Проверка | Значение | Действие |
|---|---|---|---|
| Нет target с метриками | статус target | Scraper не может достичь экспортёра | Проверьте URL, порт, firewall и сетевой путь scraper'а. |
| Target down | `/health` и контейнер | Экспортёр не готов или остановлен | Проверьте статус Compose и логи. |
| Target up, SIP-панели пусты | `invite_total` и socket receive counter | На заданный интерфейс/UDP-порт не приходит SIP | Проверьте NIC, `SIP_EXPORTER_SIP_PORTS` и топологию. |
| SIP-панели работают, RTP-панели пусты | dialog и RTP counters | SDP или RTP не видны/не коррелируются | Проверьте SIP с SDP и медиа на поддерживаемом пути. |
| Значения неполные | socket/userspace drop counters | Пакеты теряются при захвате или в userspace | Устраните drops до доверия QoE и fraud-сигналам. |

<a id="verify-container"></a>
## 1. Проверка контейнера и health

Выполните из каталога с `docker-compose.production.yml` и `.env`, подготовленными по Quick Start. Настройка `SIP_EXPORTER_INTERFACE` должна оставаться в `.env` и для последующих команд:

```bash
docker compose --env-file .env -f docker-compose.production.yml ps
curl -fsS http://127.0.0.1:10047/health
curl -fsS http://127.0.0.1:10047/metrics | grep '^sip_exporter_build_info'
```

Ожидаемый результат: сервис запущен, `/health` отвечает успешно, последняя команда выводит build-info. Успешный `/health` подтверждает инициализацию, но не полноту захвата. Если проверка не прошла, изучите логи локально; даже при `info` они могут содержать Call-ID:

```bash
docker compose --env-file .env -f docker-compose.production.yml logs --tail=100 sip-exporter
```

Убедитесь, что `SIP_EXPORTER_INTERFACE` — NIC с production-трафиком. Контейнеру нужны `network_mode: host` и `privileged: true`; не включайте `SIP_EXPORTER_IGNORE_OUTGOING` вне loopback-тестов.

<a id="verify-scrape"></a>
## 2. Проверка scrape target

Настройте scraper на:

```text
http://<exporter-host>:10047/metrics
```

В представлении статуса targets scraper'а он должен быть `UP`. В Grafana Explore выберите тот же datasource и выполните запрос:

```promql
up{job="sip-exporter",instance="sensor:10047"}
```

Во всех запросах замените `sensor:10047` на точное значение лейбла `instance` нужного сенсора, а при необходимости — имя `job`. Локальный `curl` и удалённый scrape используют разные сетевые пути: при target down проверьте адрес, порт, firewall и сообщение ошибки на странице targets. К диагностике SIP переходите после появления `UP`.

<a id="verify-sip"></a>
## 3. Проверка захвата SIP

На новом сенсоре совершите первый тестовый вызов, чтобы появились серии с нужными лейблами. Дождитесь минимум двух успешных scrape, затем совершите второй вызов и дождитесь следующего scrape. Проверьте рост счётчиков:

```promql
sum(increase(sip_exporter_socket_packets_received_total{job="sip-exporter",instance="sensor:10047"}[5m]))
sum(increase(sip_exporter_invite_total{job="sip-exporter",instance="sensor:10047"}[5m]))
```

Socket counter доказывает, что пакеты достигли AF_PACKET socket. Положительный рост INVITE доказывает, что SIP INVITE был распарсен. Если socket counter равен нулю, выберите верный NIC и проверьте, что хост пересылает или терминирует этот трафик. Если он растёт, но INVITE нет, проверьте UDP-транспорт и `SIP_EXPORTER_SIP_PORTS`; SIP по TCP/TLS не захватывается.

`increase()` не восстанавливает события, случившиеся до первого измерения новой серии. Пустой результат или нулевой рост после единственного звонка ещё не доказывает отсутствие захвата. При необходимости посмотрите текущее значение `sip_exporter_invite_total` с теми же фильтрами и повторите контрольный вызов.

<a id="verify-dialog-sdp"></a>
## 4. Проверка видимости диалога и SDP

Во время активного отвеченного вызова выполните запросы:

```promql
sum(sip_exporter_active_dialogs{job="sip-exporter",instance="sensor:10047"})
sum(sip_exporter_active_trackers{job="sip-exporter",instance="sensor:10047",type="rtp"})
```

После завершившегося вызова выполните запрос:

```promql
sum(increase(sip_exporter_sessions_missing_rtp_total{job="sip-exporter",instance="sensor:10047"}[15m]))
```

Активный dialog подтверждает наблюдение INVITE/200 OK. `sessions_missing_rtp_total` растёт только после завершения dialog с SDP media endpoints, когда RTP не был замечен. Если SIP есть, но media correlation нет, убедитесь, что оба направления SIP, финальные IPv4/UDP endpoints из SDP и медиа проходят одним поддерживаемым путём. RTP без видимого SIP не коррелируется: media endpoints экспортёр узнаёт из SDP.

<a id="verify-rtp"></a>
## 5. Проверка RTP-захвата

Во время передачи медиа выполните запросы:

```promql
sum(increase(sip_exporter_rtp_packets_total{job="sip-exporter",instance="sensor:10047"}[5m]))
sum(sip_exporter_rtp_active_streams{job="sip-exporter",instance="sensor:10047"})
```

Во время достаточно долгого контрольного вызова число активных RTP-потоков должно стать положительным; рост `rtp_packets_total` требует нескольких scrape. Если SIP виден, а RTP — нет, проверьте маршрут медиа и соответствие SDP наблюдаемым адресам. Поддерживается обучение изменённого source-порта при неизменном IP и однозначной корреляции; смена IP и неоднозначные общие endpoints не поддерживаются. Разместите сенсор там, где видны SIP и оба направления RTP, согласно матрице поддерживаемых топологий.

<a id="verify-drops"></a>
## 6. Проверка качества данных и drops

До реакции на панели качества, фрода или one-way media выполните запросы:

```promql
sum(rate(sip_exporter_socket_packets_dropped_total{job="sip-exporter",instance="sensor:10047"}[5m]))
sum(rate(sip_exporter_rtp_dropped_total{job="sip-exporter",instance="sensor:10047"}[5m]))
100 * sum(rate(sip_exporter_socket_packets_dropped_total{job="sip-exporter",instance="sensor:10047"}[5m])) / sum(rate(sip_exporter_socket_packets_received_total{job="sip-exporter",instance="sensor:10047"}[5m]))
sip_exporter_channel_length{job="sip-exporter",instance="sensor:10047"} / clamp_min(sip_exporter_channel_capacity{job="sip-exporter",instance="sensor:10047"}, 1)
```

`socket_packets_dropped_total` показывает потери в приёмном буфере ядра. `rtp_dropped_total` показывает RTP-пакеты, которые приложение не приняло в очередь из-за недостатка места или приоритета ожидающего SIP. Длина очереди обновляется периодически и может не показать короткий всплеск; положительный счётчик потерь надёжнее этого снимка. Уменьшите нагрузку на сенсор, устраните двойной захват через интерфейсы или увеличьте доступную производительность. До устранения потерь выводы о качестве RTP, MOS, FAS и отсутствии медиа ненадёжны.

См. [Метрики](METRICS.ru.md), [Алертинг](ALERTING.ru.md) и [Grafana-дашборд](../examples/grafana-dashboard.json) для определений метрик и алертов.
