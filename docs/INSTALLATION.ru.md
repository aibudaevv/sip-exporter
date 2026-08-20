# Проверка установки и runbook пустого дашборда

Используйте этот runbook после развёртывания [production Compose-примера](../examples/docker-compose.production.yml). Он проверяет путь от экспортёра до Grafana и не выводит SIP payload, Call-ID или endpoint-лейблы.

## Первый полезный дашборд

1. Запустите экспортёр на хосте, который видит и SIP-сигнализацию, и RTP-медиа.
2. Проверьте контейнер и его endpoint `/health`.
3. Настройте любой Prometheus-совместимый scraper на `http://<host>:2112/metrics`.
4. Убедитесь, что target имеет статус `UP`, затем совершите один тестовый вызов по наблюдаемому пути.
5. Импортируйте [`examples/grafana-dashboard.json`](../examples/grafana-dashboard.json) и выберите datasource scraper'а.

Экспортёр поддерживает SIP и RTP по IPv4/UDP. SIP по TCP/TLS, IPv6, фрагментированный UDP, QoE через SPAN/TAP и RTP без видимого SDP не входят в контракт захвата; см. [топологию развёртывания](../README.ru.md#топология-развёртывания).

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

Выполните из каталога с `docker-compose.yml`:

```bash
docker compose ps
curl -fsS http://127.0.0.1:2112/health
curl -fsS http://127.0.0.1:2112/metrics | grep '^sip_exporter_build_info'
```

Ожидаемый результат: сервис запущен, `/health` отвечает успешно, последняя команда выводит build-info метрику. Если проверка не прошла, посмотрите только эксплуатационные логи:

```bash
docker compose logs --tail=100 sip-exporter
```

Убедитесь, что `SIP_EXPORTER_INTERFACE` — NIC с production-трафиком. Контейнеру нужны `network_mode: host` и `privileged: true`; не включайте `SIP_EXPORTER_IGNORE_OUTGOING` вне loopback-тестов.

<a id="verify-scrape"></a>
## 2. Проверка scrape target

Настройте scraper на:

```text
http://<exporter-host>:2112/metrics
```

В представлении статуса targets scraper'а он должен быть `UP`. В Grafana Explore выберите тот же datasource и выполните запрос:

```promql
up{job="sip-exporter"}
```

Если в scrape-конфигурации другое имя job, замените `sip-exporter`. Успешный локальный `curl` при target down означает, что scraper не достигает хоста: проверьте адрес, порт `2112`, firewall и network namespace. Не диагностируйте SIP-захват, пока target не станет `UP`.

<a id="verify-sip"></a>
## 3. Проверка захвата SIP

Совершите один тестовый вызов по наблюдаемому production-пути, затем выполните запросы:

```promql
sum(increase(sip_exporter_socket_packets_received_total[5m]))
sum(increase(sip_exporter_invite_total[5m]))
```

Socket counter доказывает, что пакеты достигли AF_PACKET socket. Положительный рост INVITE доказывает, что SIP INVITE был распарсен. Если socket counter равен нулю, выберите верный NIC и проверьте, что хост пересылает или терминирует этот трафик. Если он растёт, но INVITE нет, проверьте UDP-транспорт и `SIP_EXPORTER_SIP_PORTS`; SIP по TCP/TLS не захватывается.

<a id="verify-dialog-sdp"></a>
## 4. Проверка видимости диалога и SDP

Во время активного отвеченного вызова выполните запросы:

```promql
sum(sip_exporter_active_dialogs)
sum(sip_exporter_active_trackers{type="rtp"})
```

После завершившегося вызова выполните запрос:

```promql
sum(increase(sip_exporter_sessions_missing_rtp_total[15m]))
```

Активный dialog подтверждает наблюдение INVITE/200 OK. `sessions_missing_rtp_total` растёт только после завершения dialog с SDP media endpoints, когда RTP не был замечен. Если SIP есть, но media correlation нет, убедитесь, что оба направления SIP, финальные IPv4/UDP endpoints из SDP и медиа проходят одним поддерживаемым путём. RTP без видимого SIP не коррелируется: media endpoints экспортёр узнаёт из SDP.

<a id="verify-rtp"></a>
## 5. Проверка RTP-захвата

Во время передачи медиа выполните запросы:

```promql
sum(increase(sip_exporter_rtp_packets_total[5m]))
sum(sip_exporter_rtp_active_streams)
```

Оба значения должны быть положительными для активного вызова с видимым медиа. Если SIP и dialogs работают, но RTP остаётся нулевым, медиа может обходить хост, NAT может менять source port (symmetric RTP), SDP может отличаться от наблюдаемого endpoint, либо доступна только зеркальная/SPAN-копия. Переместите сенсор на forwarding host, где видны SIP и оба направления RTP, прежде чем трактовать пустые RTP-панели как качество голоса.

<a id="verify-drops"></a>
## 6. Проверка качества данных и drops

До реакции на панели качества, фрода или one-way media выполните запросы:

```promql
sum(rate(sip_exporter_socket_packets_dropped_total[5m]))
sum(rate(sip_exporter_rtp_dropped_total[5m]))
100 * sum(rate(sip_exporter_socket_packets_dropped_total[5m])) / sum(rate(sip_exporter_socket_packets_received_total[5m]))
sip_exporter_channel_length / clamp_min(sip_exporter_channel_capacity, 1)
```

Socket drops означают переполнение kernel receive buffer; RTP drops — заполнение внутреннего userspace channel. Channel ratio около `1` означает устойчивую saturation. Уменьшите трафик на сенсор, устраните захват через дублирующие интерфейсы или обеспечьте хост достаточным CPU, прежде чем доверять derived RTP loss, MOS, FAS, missing-RTP или one-way-RTP сигналам.

См. [Метрики](METRICS.ru.md), [Алертинг](ALERTING.ru.md) и [Grafana-дашборд](../examples/grafana-dashboard.json) для определений метрик и алертов.
