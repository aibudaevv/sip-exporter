# Carrier Feature — Тест-кейсы для ручного тестирования

## Описание фичи

SIP Exporter поддерживает привязку SIP-трафика к операторам связи (carrier) по IP-адресам.
Конфигурация задаётся через YAML-файл, в котором каждому carrier сопоставлен список CIDR-подсетей.
Все Prometheus-метрики получают метку `carrier="имя"`, что позволяет строить дашборды и алерты
по отдельным операторам.

**Ключевые свойства:**
- Carrier определяется по **source IP** запроса (INVITE, REGISTER, OPTIONS)
- Для INVITE-ответов carrier «замораживается» на момент INVITE (direction fix) — даже если 200 OK приходит от IP другого carrier
- Для REGISTER-ответов carrier тоже берётся из трекера (сохранён при REGISTER-запросе)
- IP, не попадающий ни в один CIDR, получает `carrier="other"`
- При пересекающихся CIDR побеждает **первое совпадение** (порядок в YAML)
- Без конфига carriers.yaml все метрики получают `carrier="other"`

## Окружение

**Требования:**
- Linux (root-доступ для eBPF)
- Docker + Docker Compose
- Доступ к интернету (pull образов)

**Образы:**
- `sip-exporter:latest` — собрать из репозитория: `make docker_build`
- `pbertera/sipp:latest` — публичный образ SIPp

**Сетап:** Все тесты выполняются на loopback-интерфейсе `lo`. Для тестов с разными carrier
на `lo` добавляются secondary IP-адреса через привилегированный Docker-контейнер.

---

## Подготовка окружения

Выполняется **один раз** перед всеми тестами:

```bash
cd /home/aleksey/go/src/github.com/sip-exporter

# 1. Собрать образ экспортера
make docker_build

# 2. Добавить secondary IP на loopback
for addr in 10.1.0.1/32 10.2.0.1/32 172.16.0.1/32 172.16.0.2/32 10.1.1.5/32 10.1.0.1/32; do
  docker run --rm --privileged --network host --entrypoint "" alpine \
    ip addr add $addr dev lo
done

# 3. Проверить что IP добавились
docker run --rm --privileged --network host --entrypoint "" alpine ip addr show lo | grep inet
```

**Ожидание:** в выводе должны быть адреса 10.1.0.1, 10.2.0.1, 172.16.0.1, 172.16.0.2, 10.1.1.5, 10.1.0.1

**Очистка после всех тестов:**
```bash
for addr in 10.1.0.1/32 10.2.0.1/32 172.16.0.1/32 172.16.0.2/32 10.1.1.5/32 10.1.0.1/32; do
  docker run --rm --privileged --network host --entrypoint "" alpine \
    ip addr del $addr dev lo
done
```

**Общее правило:** между тест-кейсами **перезапускать экспортер** (docker stop/rm → docker run).
Каждый тест-кейс начинается с «чистого» экспортера.

---

## Вспомогательные команды

**Проверить конкретную метрику:**
```bash
# Получить значение метрики по имени и carrier
curl -s http://localhost:2112/metrics | grep 'sip_exporter_invite_total{carrier="carrier-A"}'
```

**Проверить все carrier-метрики:**
```bash
curl -s http://localhost:2112/metrics | grep 'carrier=' | grep -v '^#'
```

**Посмотреть логи экспортера:**
```bash
docker logs sip-exp -f
```

---

## TC-01: Базовое разрешение carrier по IP

### Цель
Проверить что при наличии carriers.yaml трафик с 127.0.0.1 получает `carrier="loopback-carrier"`.

### Предусловия
- Экспортер **не запущен**
- `carriers.yaml` определяет `127.0.0.0/8 → loopback-carrier`

### Конфигурация carriers.yaml
```yaml
carriers:
  - name: "loopback-carrier"
    cidrs:
      - "127.0.0.0/8"
```
Файл: `test/e2e/carriers.yaml`

### Шаги

1. Запустить экспортер:
```bash
docker run -d --name sip-exp --privileged --network host \
  -e SIP_EXPORTER_INTERFACE=lo \
  -e SIP_EXPORTER_HTTP_PORT=2112 \
  -e SIP_EXPORTER_SIP_PORT=5060 \
  -e SIP_EXPORTER_LOGGER_LEVEL=info \
  -v $(pwd)/test/e2e/carriers.yaml:/etc/sip-exporter/carriers.yaml:ro \
  -e SIP_EXPORTER_CARRIERS_CONFIG=/etc/sip-exporter/carriers.yaml \
  sip-exporter:latest
```

2. Дождаться запуска (появится `/metrics`):
```bash
sleep 3 && curl -s http://localhost:2112/metrics | head -5
```

3. Запустить SIPp-сервер (UAS) — ожидает 10 входящих звонков, отвечает 100 Trying → 180 Ringing → 200 OK, затем получает BYE и отвечает 200 OK:
```bash
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/uas_100.xml -i 127.0.0.1 -p 5060 -m 10 -nostdin
```

4. В **другом терминале** запустить SIPp-клиент (UAC) — совершает 10 звонков:
```bash
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/uac_100.xml -i 127.0.0.1 -p 5061 -m 10 127.0.0.1:5060
```

5. Дождаться завершения SIPp (~5 секунд)

6. Проверить метрики:
```bash
# Все INVITE-счётчики должны иметь carrier="loopback-carrier"
curl -s http://localhost:2112/metrics | grep 'invite_total{carrier='
curl -s http://localhost:2112/metrics | grep 'ser{carrier='
curl -s http://localhost:2112/metrics | grep 'sdc_total{carrier='
```

### Ожидаемый результат

| Метрика | Ожидаемое значение | Пояснение |
|---------|-------------------|-----------|
| `sip_exporter_invite_total{carrier="loopback-carrier"}` | 20 | 10 звонков × 2 (loopback дублирует пакеты: отправка + приём) |
| `sip_exporter_invite_total{carrier="other"}` | отсутствует | IP 127.0.0.1 входит в CIDR 127.0.0.0/8 |
| `sip_exporter_ser{carrier="loopback-carrier"}` | 100 | SER = (200 OK / (INVITE - 3xx)) × 100; все 10 звонков успешные |
| `sip_exporter_sdc_total{carrier="loopback-carrier"}` | 10 | 10 завершённых сессий (BYE→200 OK) |
| `sip_exporter_sessions{carrier="loopback-carrier"}` | 0 | Все диалоги завершены |

**Важно:** не должно быть метрик с другими carrier-значениями (только `loopback-carrier`).

### Очистка
```bash
docker stop sip-exp && docker rm sip-exp
```

---

## TC-02: Direction Fix — carrier определяется по INVITE, а не по ответу

### Цель
Проверить что при отправке INVITE от IP carrier-A и получении 200 OK от IP carrier-B,
все метрики (включая SessionCompleted) привязываются к carrier-A.

### Предусловия
- Экспортер **не запущен**
- На `lo` добавлены secondary IP: 10.1.0.1 (carrier-A), 10.2.0.1 (carrier-B)

### Конфигурация
```yaml
carriers:
  - name: "carrier-A"
    cidrs:
      - "10.1.0.0/16"
  - name: "carrier-B"
    cidrs:
      - "10.2.0.0/16"
```
Файл: `test/e2e/carriers_direction.yaml`

### Шаги

1. Запустить экспортер:
```bash
docker run -d --name sip-exp --privileged --network host \
  -e SIP_EXPORTER_INTERFACE=lo \
  -e SIP_EXPORTER_HTTP_PORT=2112 \
  -e SIP_EXPORTER_SIP_PORT=5060 \
  -e SIP_EXPORTER_LOGGER_LEVEL=info \
  -v $(pwd)/test/e2e/carriers_direction.yaml:/etc/sip-exporter/carriers.yaml:ro \
  -e SIP_EXPORTER_CARRIERS_CONFIG=/etc/sip-exporter/carriers.yaml \
  sip-exporter:latest
```

2. Запустить SIPp UAS на IP carrier-B (10.2.0.1, порт 5060):
```bash
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/uas_100.xml -i 10.2.0.1 -p 5060 -m 10 -nostdin
```

3. В **другом терминале** запустить SIPp UAC на IP carrier-A (10.1.0.1, порт 5061), целевой адрес 10.2.0.1:5060:
```bash
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/uac_100.xml -i 10.1.0.1 -p 5061 -m 10 10.2.0.1:5060
```

4. Дождаться завершения SIPp

5. Проверить INVITE-счётчики:
```bash
curl -s http://localhost:2112/metrics | grep 'invite_total{carrier='
```

6. Проверить SER:
```bash
curl -s http://localhost:2112/metrics | grep 'ser{carrier='
```

7. Проверить завершённые сессии (SDC):
```bash
curl -s http://localhost:2112/metrics | grep 'sdc_total{carrier='
```

### Ожидаемый результат

| Метрика | Значение | Пояснение |
|---------|----------|-----------|
| `invite_total{carrier="carrier-A"}` | 20 | INVITE отправлен от 10.1.0.1 (carrier-A); ×2 на loopback |
| `invite_total{carrier="carrier-B"}` | 0 | INVITE не отправлялся от IP carrier-B |
| `ser{carrier="carrier-A"}` | 50 | inviteTotal=20, invite200OKTotal=10 (tracker захватил carrier-A); SER=10/20×100=50% |
| `ser{carrier="carrier-B"}` | 0 | Нет INVITE от carrier-B → SER не определена → 0 |
| `sdc_total{carrier="carrier-A"}` | 10 | Диалог создан с carrier-A (из INVITE tracker); BYE→200 OK завершил сессию |
| `sdc_total{carrier="carrier-B"}` | 0 | Диалоги не создавались с carrier-B |

**Почему SER=50%, а не 100%:** на loopback каждый INVITE виден дважды (отправка+приём).
`invite_total` удваивается (20 вместо 10), а `invite200OKTotal` частично попадает в carrier-B
(эхо-ответ 200 OK с «мёртвым» трекером резолвится по srcIP=10.2.0.1 → carrier-B).
Итого invite200OKTotal{carrier-A}=10, inviteTotal{carrier-A}=20 → SER=50%.

### Очистка
```bash
docker stop sip-exp && docker rm sip-exp
```

---

## TC-03: Мульти-carrier — разделение трафика

### Цель
Проверить что два оператора, отправляющих трафик через один экспортер, получают
раздельные, не смешиваемые метрики.

### Предусловия
- Экспортер **не запущен**

### Шаги

1. Запустить экспортер с `carriers_direction.yaml`:
```bash
docker run -d --name sip-exp --privileged --network host \
  -e SIP_EXPORTER_INTERFACE=lo \
  -e SIP_EXPORTER_HTTP_PORT=2112 \
  -e SIP_EXPORTER_SIP_PORT=5060 \
  -v $(pwd)/test/e2e/carriers_direction.yaml:/etc/sip-exporter/carriers.yaml:ro \
  -e SIP_EXPORTER_CARRIERS_CONFIG=/etc/sip-exporter/carriers.yaml \
  sip-exporter:latest
```

2. **Сессия 1 — carrier-A звонит (успех):** UAS на 10.2.0.1, UAC на 10.1.0.1, 10 звонков → 200 OK
```bash
# Терминал 1: UAS
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/uas_100.xml -i 10.2.0.1 -p 5060 -m 10 -nostdin &

sleep 1

# Терминал 2: UAC
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/uac_100.xml -i 10.1.0.1 -p 5061 -m 10 10.2.0.1:5060

wait
sleep 2
```

3. **Сессия 2 — carrier-B звонит (отклонение):** UAS на 10.1.0.1, UAC на 10.2.0.1, 10 звонков → 480 Busy
```bash
# Терминал 1: UAS
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/uas_busy.xml -i 10.1.0.1 -p 5060 -m 10 -nostdin &

sleep 1

# Терминал 2: UAC
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/uac_busy.xml -i 10.2.0.1 -p 5061 -m 10 10.1.0.1:5060

wait
```

4. Проверить:
```bash
curl -s http://localhost:2112/metrics | grep 'invite_total{carrier='
curl -s http://localhost:2112/metrics | grep 'sdc_total{carrier='
curl -s http://localhost:2112/metrics | grep 'ser{carrier='
```

### Ожидаемый результат

| Метрика | carrier-A | carrier-B | Пояснение |
|---------|-----------|-----------|-----------|
| `invite_total` | 20 | 20 | Каждый отправил 10 INVITE × 2 (loopback) |
| `sdc_total` | 10 | 0 | Только carrier-A имел успешные звонки с полным диалогом (INVITE→200 OK→BYE→200 OK) |
| `ser` | > 0 | > 0 | У обоих есть INVITE; у carrier-A были 200 OK, у carrier-B — 480 (SER=0 по формуле, но из-за эхо-эффектов loopback invite200OKTotal{carrier-B} > 0) |

**Ключевая проверка:** `sdc_total{carrier="carrier-B"} = 0`. Звонки carrier-B были отклонены (480 Busy),
диалоги не создавались, сессии не завершались.

### Очистка
```bash
docker stop sip-exp && docker rm sip-exp
```

---

## TC-04: Неизвестный IP → carrier="other"

### Цель
Проверить что SIP-трафик от IP-адреса, не попадающего ни в один CIDR в конфиге,
получает `carrier="other"`.

### Предусловия
- Экспортер **не запущен**
- На `lo` добавлены 172.16.0.1, 172.16.0.2

### Шаги

1. Запустить экспортер с `carriers_direction.yaml` (определены только 10.1.0.0/16 и 10.2.0.0/16):
```bash
docker run -d --name sip-exp --privileged --network host \
  -e SIP_EXPORTER_INTERFACE=lo \
  -e SIP_EXPORTER_HTTP_PORT=2112 \
  -e SIP_EXPORTER_SIP_PORT=5060 \
  -v $(pwd)/test/e2e/carriers_direction.yaml:/etc/sip-exporter/carriers.yaml:ro \
  -e SIP_EXPORTER_CARRIERS_CONFIG=/etc/sip-exporter/carriers.yaml \
  sip-exporter:latest
```

2. Запустить UAS на 172.16.0.2 (вне всех CIDR):
```bash
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/uas_100.xml -i 172.16.0.2 -p 5060 -m 10 -nostdin &

sleep 1
```

3. Запустить UAC на 172.16.0.1 (вне всех CIDR):
```bash
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/uac_100.xml -i 172.16.0.1 -p 5061 -m 10 172.16.0.2:5060
```

4. Проверить:
```bash
# Должны быть метрики с carrier="other"
curl -s http://localhost:2112/metrics | grep 'carrier="other"' | grep -v '^#'

# НЕ должно быть метрик с carrier-A или carrier-B
curl -s http://localhost:2112/metrics | grep -E 'carrier="(carrier-A|carrier-B)"' | grep -v '^#'
```

### Ожидаемый результат

| Метрика | Ожидание | Пояснение |
|---------|----------|-----------|
| `invite_total{carrier="other"}` | 20 | 172.16.0.1 и 172.16.0.2 не входят в 10.1.0.0/16 или 10.2.0.0/16 |
| `invite_total{carrier="carrier-A"}` | отсутствует | 172.16.0.1 не входит в 10.1.0.0/16 |
| `invite_total{carrier="carrier-B"}` | отсутствует | Аналогично |
| `sdc_total{carrier="other"}` | 10 | Диалоги созданы с carrier="other" |
| `ser{carrier="other"}` | 50 | Аналогично TC-02 (loopback doubling) |

### Очистка
```bash
docker stop sip-exp && docker rm sip-exp
```

---

## TC-05: Пересекающиеся CIDR — первое совпадение выигрывает

### Цель
Проверить что при пересечении подсетей в конфиге (10.1.1.0/24 и 10.1.0.0/16),
IP-адрес 10.1.1.5 привязывается к **первому** совпадению в YAML.

### Предусловия
- Экспортер **не запущен**

### Конфигурация
```yaml
carriers:
  - name: "carrier-specific"    # listed FIRST — должен победить
    cidrs:
      - "10.1.1.0/24"
  - name: "carrier-broad"       # listed SECOND — более широкая подсеть
    cidrs:
      - "10.1.0.0/16"
```
Файл: `test/e2e/carriers_direction_overlap.yaml`

IP 10.1.1.5 входит **в обе** подсети. Ожидается `carrier="carrier-specific"` (первый в списке).

### Шаги

1. Запустить экспортер:
```bash
docker run -d --name sip-exp --privileged --network host \
  -e SIP_EXPORTER_INTERFACE=lo \
  -e SIP_EXPORTER_HTTP_PORT=2112 \
  -e SIP_EXPORTER_SIP_PORT=5060 \
  -v $(pwd)/test/e2e/carriers_direction_overlap.yaml:/etc/sip-exporter/carriers.yaml:ro \
  -e SIP_EXPORTER_CARRIERS_CONFIG=/etc/sip-exporter/carriers.yaml \
  sip-exporter:latest
```

2. Запустить UAS на 10.1.0.1 (входит только в carrier-broad):
```bash
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/uas_100.xml -i 10.1.0.1 -p 5060 -m 10 -nostdin &

sleep 1
```

3. Запустить UAC на 10.1.1.5 (входит в оба — должно выбрать carrier-specific):
```bash
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/uac_100.xml -i 10.1.1.5 -p 5061 -m 10 10.1.0.1:5060
```

4. Проверить:
```bash
curl -s http://localhost:2112/metrics | grep 'carrier="carrier-specific"'
curl -s http://localhost:2112/metrics | grep 'carrier="carrier-broad"'
```

### Ожидаемый результат

| Метрика | Значение | Пояснение |
|---------|----------|-----------|
| `invite_total{carrier="carrier-specific"}` | 20 | UAC на 10.1.1.5 → первое совпадение 10.1.1.0/24 → carrier-specific |
| `invite_total{carrier="carrier-broad"}` | 0 | INVITE-запросы отправлены от 10.1.1.5, который резолвится в carrier-specific |
| `sdc_total{carrier="carrier-specific"}` | 10 | Диалоги созданы с carrier-specific |

**Примечание:** UAS на 10.1.0.1 резолвится в carrier-broad, но INVITE-запросы отправляются
от UAC (10.1.1.5), поэтому `invite_total` привязывается к carrier-specific.

### Очистка
```bash
docker stop sip-exp && docker rm sip-exp
```

---

## TC-06: Без конфига carriers.yaml — всё в "other"

### Цель
Проверить что без переменной `SIP_EXPORTER_CARRIERS_CONFIG` все метрики получают `carrier="other"`.

### Шаги

1. Запустить экспортер **без** монтирования YAML:
```bash
docker run -d --name sip-exp --privileged --network host \
  -e SIP_EXPORTER_INTERFACE=lo \
  -e SIP_EXPORTER_HTTP_PORT=2112 \
  -e SIP_EXPORTER_SIP_PORT=5060 \
  sip-exporter:latest
```

2. Запустить UAS и UAC на 127.0.0.1 (как в TC-01)

3. Проверить:
```bash
curl -s http://localhost:2112/metrics | grep 'carrier=' | grep -v '^#' | grep -v other
```

### Ожидаемый результат
- Команда из п.3 возвращает **пустой вывод** (нет carrier-значений кроме "other")
- Все SIP-метрики содержат `carrier="other"`

### Очистка
```bash
docker stop sip-exp && docker rm sip-exp
```

---

## TC-07: Per-carrier REGISTER (RRD)

### Цель
Проверить что REGISTER-запросы получают carrier по source IP, а задержка RRD
измеряется и записывается в гистограмму с правильным carrier-лейблом.

### Шаги

1. Запустить экспортер с `carriers_direction.yaml`

2. Запустить REGISTER UAS на IP carrier-A (10.1.0.1):
```bash
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/reg_uas.xml -i 10.1.0.1 -p 5060 -m 10 -nostdin &

sleep 1
```

3. Запустить REGISTER UAC на IP carrier-A (10.1.0.1):
```bash
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/reg_uac.xml -i 10.1.0.1 -p 5061 -m 10 10.1.0.1:5060
```

4. Проверить:
```bash
curl -s http://localhost:2112/metrics | grep -E '(register_total|rrd_count).*carrier='
```

### Ожидаемый результат

| Метрика | Значение | Пояснение |
|---------|----------|-----------|
| `register_total{carrier="carrier-A"}` | > 0 | REGISTER от 10.1.0.1 → carrier-A |
| `rrd_count{carrier="carrier-A"}` | > 0 | RRD измерен для REGISTER→200 OK |
| `rrd_count{carrier="carrier-B"}` | отсутствует | Нет REGISTER от carrier-B |

### Очистка
```bash
docker stop sip-exp && docker rm sip-exp
```

---

## TC-08: Per-carrier Session-Expires (истечение диалога)

### Цель
Проверить что при истечении диалога по таймауту Session-Expires (без BYE),
SessionCompleted и SDC привязываются к carrier, с которым диалог был создан.

### Шаги

1. Запустить экспортер с `carriers_direction.yaml`

2. Запустить UAS с коротким Session-Expires (3 секунды, без BYE) на IP carrier-B:
```bash
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/uas_short_expires.xml -i 10.2.0.1 -p 5060 -m 5 -nostdin &

sleep 1
```

3. Запустить UAC с коротким Session-Expires на IP carrier-A:
```bash
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/uac_short_expires.xml -i 10.1.0.1 -p 5061 -m 5 10.2.0.1:5060
```

4. Подождать истечения диалогов (Session-Expires: 3 сек, экспортер проверяет каждую секунду):
```bash
echo "Ждём 10 секунд (Session-Expires=3с, cleanup ticker=1с)..."
sleep 10
```

5. Проверить:
```bash
# Активные сессии должны быть 0
curl -s http://localhost:2112/metrics | grep 'sessions{carrier='

# Завершённые сессии (SDC) — carrier-A, не carrier-B
curl -s http://localhost:2112/metrics | grep 'sdc_total{carrier='
```

### Ожидаемый результат

| Метрика | Значение | Пояснение |
|---------|----------|-----------|
| `sessions{carrier="carrier-A"}` | 0 | Диалоги истекли (Session-Expires: 3с) |
| `sdc_total{carrier="carrier-A"}` | 5 | Диалог создан с carrier-A (INVITE от 10.1.0.1); истечение → SessionCompleted(carrier-A) |
| `sdc_total{carrier="carrier-B"}` | 0 | Диалоги создавались с carrier-A, не с carrier-B |
| `spd_count{carrier="carrier-A"}` | 5 | SPD измерен при истечении (duration = время жизни диалога) |

### Очистка
```bash
docker stop sip-exp && docker rm sip-exp
```

---

## TC-09: Per-carrier OPTIONS (ORD)

### Цель
Проверить что OPTIONS-запросы получают carrier по source IP, а задержка ORD
измеряется с правильным carrier-лейблом.

### Шаги

1. Запустить экспортер с `carriers_direction.yaml`

2. Запустить UAS (отвечает на OPTIONS, без INVITE) на IP carrier-B:
```bash
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/uas_no_invite.xml -i 10.2.0.1 -p 5060 -m 10 -nostdin &

sleep 1
```

3. Запустить UAC (отправляет OPTIONS) на IP carrier-A:
```bash
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/uac_no_invite.xml -i 10.1.0.1 -p 5061 -m 10 10.2.0.1:5060
```

4. Проверить:
```bash
curl -s http://localhost:2112/metrics | grep 'ord_count{carrier='
curl -s http://localhost:2112/metrics | grep 'invite_total{carrier='
```

### Ожидаемый результат

| Метрика | Значение | Пояснение |
|---------|----------|-----------|
| `ord_count{carrier="carrier-A"}` | 10 | OPTIONS отправлен от 10.1.0.1 → carrier-A; tracker сохранил carrier-A; на ответ ORD измерен |
| `ord_count{carrier="carrier-B"}` | отсутствует | OPTIONS-запросы отправлялись только от carrier-A |
| `invite_total{carrier="carrier-A"}` | отсутствует | Сценарий «no_invite» не отправляет INVITE |

### Очистка
```bash
docker stop sip-exp && docker rm sip-exp
```

---

## TC-10: Per-carrier REGISTER Redirect (LRD)

### Цель
Проверить что REGISTER 302 redirect измеряется как LRD (не RRD) с правильным carrier.

### Шаги

1. Запустить экспортер с `carriers_direction.yaml`

2. Запустить REGISTER UAS (отвечает 302 Redirect) на IP carrier-B:
```bash
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/reg_uas_redirect.xml -i 10.2.0.1 -p 5060 -m 10 -nostdin &

sleep 1
```

3. Запустить REGISTER UAC на IP carrier-A:
```bash
docker run --rm --network host \
  -v $(pwd)/test/e2e/sipp:/scenarios:ro \
  pbertera/sipp:latest \
  -sf /scenarios/reg_uac_redirect.xml -i 10.1.0.1 -p 5061 -m 10 10.2.0.1:5060
```

4. Проверить:
```bash
curl -s http://localhost:2112/metrics | grep 'lrd_count{carrier='
curl -s http://localhost:2112/metrics | grep 'rrd_count{carrier='
```

### Ожидаемый результат

| Метрика | Значение | Пояснение |
|---------|----------|-----------|
| `lrd_count{carrier="carrier-A"}` | 10 | REGISTER от carrier-A → 302 redirect → LRD измерен (tracker сохранил carrier-A из запроса) |
| `lrd_count{carrier="carrier-B"}` | отсутствует | REGISTER-запросы отправлялись от carrier-A |
| `rrd_count{carrier="carrier-A"}` | отсутствует | 302 redirect — это LRD, не RRD (RRD измеряется только на 200 OK) |

### Очистка
```bash
docker stop sip-exp && docker rm sip-exp
```

---

## Инварианты (проверять после каждого теста)

После любого тест-кейса должны выполняться:

```bash
# 1. Все SIP-метрики имеют carrier-лейбл
curl -s http://localhost:2112/metrics | grep -v '^#' | grep -v '^$' | \
  grep -E 'sip_exporter_(invite|register|200|ser|seer|isa|scr|asr|ner|sdc|iss|sessions|rrd|spd|ttr|ord|lrd)' | \
  grep -v 'carrier='
# → пустой вывод (все метрики имеют carrier)

# 2. SER >= ASR (SER исключает 3xx из знаменателя, ASR нет)
# Для каждого carrier: sip_exporter_ser >= sip_exporter_asr

# 3. SCR <= SER (SCR считает только завершённые сессии)
# Для каждого carrier: sip_exporter_scr <= sip_exporter_ser

# 4. SEER >= SER (SEER numerator — надмножество SER numerator)
# Для каждого carrier: sip_exporter_seer >= sip_exporter_ser

# 5. NER + ISA = 100 (по определению)
# Для каждого carrier: sip_exporter_ner + sip_exporter_isa ≈ 100

# 6. sessions = 0 после завершения всех звонков
curl -s http://localhost:2112/metrics | grep 'sip_exporter_sessions'
# → все значения 0
```
