# Детекция фрода — Как это работает

> **Версия:** sip-exporter v1.9.0
>
> sip-exporter предоставляет **сигнальную** (signal-only) детекцию фрода.
> Экспортёр не блокирует и не перехватывает трафик. Вместо этого он экспортирует
> Prometheus counter/gauge-метрики, которые инкрементируются при обнаружении
> подозрительных паттернов. Алерты настраиваются в AlertManager, а блокировка
> выполняется внешними средствами (fail2ban, правила SBC, firewall).

## Что детектируется

sip-exporter покрывает топовые категории VoIP-фрода — компрометацию PBX и кражу
личности — пятью сигналами детекции:

| Сигнал | Метрика | Тип | Что детектирует |
|--------|---------|-----|-----------------|
| Сканирование регистраций | `register_scan_total` | счётчик | Перечисление аккаунтов / компрометация PBX |
| Смена страны регистрации | `register_country_change_total` | счётчик | Перехват аккаунта из новой географии |
| Всплеск INVITE | `invite_burst_total` | счётчик | Начало toll-fraud / SIP-флуд DDoS |
| False Answer Supervision | `fas_calls_total` | счётчик | Ответивший вызов с media без answer-side RTP |
| Утилизация сессий | `sessions_utilization` | gauge | Исчерпание ёмкости / нарушение контракта |

Три signaling-счётчика несут лейблы `{carrier,source_country,direction}`. FAS —
call-level сигнал и дополнительно несёт `ua_type`. IP-адрес источника используется
внутри для threshold-детектинга, но **никогда не экспонируется** как Prometheus-лейбл.

---

## Метрики и конфигурация

### Сканирование регистраций

`sip_exporter_register_scan_total{carrier,source_country,direction}` — счётчик

Детектирует регистрацию множества уникальных SIP-аккаунтов (AOR) с одного
IP-адреса в скользящем окне. Ловит компрометацию PBX (массовая регистрация
экстеншенов), фермы аккаунтов или credential stuffing с успешными регистрациями.

| Переменная окружения | По умолчанию | Описание |
|----------------------|-------------|----------|
| `SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD` | `10` | Уникальных AOR с одного IP для срабатывания |
| `SIP_EXPORTER_FRAUD_REGISTER_SCAN_WINDOW` | `60s` | Длительность скользящего окна |

**Пример:** PBX на 203.0.113.5 регистрирует 15 аккаунтов за 30с, порог=10:
регистрации 1–9 → сигнала нет; 10-й уникальный AOR → счётчик +1; 11–15 → +1 за каждый.

### Смена страны регистрации

`sip_exporter_register_country_change_total{carrier,source_country,direction}` — счётчик

Детектирует перерегистрацию того же AOR из другой страны — сигнал перехвата
аккаунта. Конфигурация не требуется (использует существующую настройку
GeoIP/страны оператора).

**Пример:** `alice@example.com` регистрируется из RU, затем из GE → счётчик
инкрементируется. Тот же AOR снова из GE → сигнала нет.

### Всплеск INVITE

`sip_exporter_invite_burst_total{carrier,source_country,direction}` — счётчик

Детектирует аномально высокую частоту первоначальных INVITE с одного IP —
toll-fraud или SIP-флуд. Re-INVITE внутри существующего диалога исключаются
(считаются отдельно, не триггерят детектор).

| Переменная окружения | По умолчанию | Описание |
|----------------------|-------------|----------|
| `SIP_EXPORTER_FRAUD_INVITE_BURST_THRESHOLD` | `100` | Первоначальных INVITE с одного IP для срабатывания |
| `SIP_EXPORTER_FRAUD_INVITE_BURST_WINDOW` | `60s` | Длительность скользящего окна |

**Пример:** PBX на 198.51.100.10 совершает 150 звонков/мин, порог=100: INVITE
1–99 → сигнала нет; 100-й → счётчик +1; 101–150 → +1 за каждый.

### False Answer Supervision

`sip_exporter_fas_calls_total{carrier,ua_type,source_country,direction}` — счётчик

Детектирует ответивший вызов с media без answer-side RTP. Сигнал имеет
два пути: периодический sweep после настроенного threshold (плюс 15s grace
для DTLS-SRTP) или BYE teardown после независимого floor 3s. Он отличается от
`sessions_missing_rtp_total`, который вычисляется при завершении диалога.

| Переменная окружения | По умолчанию | Описание |
|----------------------|-------------|----------|
| `SIP_EXPORTER_FRAUD_FAS_THRESHOLD` | `10s` | Базовое ожидание periodic sweep; не изменяет BYE floor 3s |

FAS зависит от полноты RTP-захвата. Перед реакцией проверяйте
`sip_exporter_rtp_dropped_total`. Side detection, короткие вызовы, NAT и ожидаемые
false positive для one-way media описаны в [ограничениях FAS](METRICS.ru.md#ограничения-fas).

### Утилизация сессий

- `sip_exporter_sessions_utilization{carrier}` — gauge (% от лимита)
- `sip_exporter_sessions_limit{carrier}` — gauge (настроенный лимит)

Показывает, насколько каждый оператор близок к лимиту параллельных сессий.
Полезно для планирования ёмкости — внезапный всплеск может указывать на фрод
или неправильную настройку dialer'а. Утилизация ограничена 100%.

| Переменная окружения | Описание |
|----------------------|----------|
| `SIP_EXPORTER_SESSIONS_LIMITS` | Путь к YAML-файлу с лимитами сессий |

```yaml
sessions_limits:
  - carrier: "beeline"
    limit: 500
  - carrier: "mts"
    limit: 200
  - carrier: "other"
    limit: 1000
```

---

## Алерты

> **Примечание об окне `rate()`:** `rate(counter[5m]) > 0` остаётся истинным
> ~5 минут после сигнала. Параметр `for: 1m` снижает шум от кратковременных
> всплесков.
> Выражение country-change объединяет `increase()` для повторных событий с веткой
> новой series по точному набору лейблов, поэтому детектирует первое и последующие события каждого направления.

```yaml
# Сканирование регистраций → расследование credential stuffing
- alert: SIPRegistrationScan
  expr: rate(sip_exporter_register_scan_total[5m]) > 0
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "Обнаружена атака сканирования регистраций"
    description: "Один IP регистрирует множество разных аккаунтов на {{ $labels.carrier }} из {{ $labels.source_country }}."

# Всплеск INVITE → расследование toll-fraud
- alert: SIPInviteBurst
  expr: rate(sip_exporter_invite_burst_total[5m]) > 0
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "Обнаружён всплеск INVITE"
    description: "Один IP отправляет аномально высокий поток INVITE на {{ $labels.carrier }} из {{ $labels.source_country }}."

# Смена страны регистрации → перехват аккаунта
- alert: SIPRegistrationCountryChange
  expr: |
    increase(sip_exporter_register_country_change_total[5m]) > 0
    or
    (sip_exporter_register_country_change_total > 0
      unless sip_exporter_register_country_change_total offset 5m)
  for: 0m
  labels:
    severity: warning
  annotations:
    summary: "Обнаружена смена страны регистрации"
    description: "Пользователь перерегистрировался из другой страны на {{ $labels.carrier }}."

# False Answer Supervision → расследование биллинг-фрода
- alert: SIPFalseAnswerSupervision
  expr: |
    (rate(sip_exporter_fas_calls_total[5m]) > 0)
    unless on()
    (rate(sip_exporter_rtp_dropped_total[5m]) > 100)
  for: 2m
  labels:
    severity: warning
  annotations:
    summary: "Подозрение на False Answer Supervision"
    description: "Ответившие вызовы на {{ $labels.carrier }} не понесли answer-side RTP. Перед реакцией проверьте RTP-дропы и ожидаемые one-way-media эндпоинты."

# Исчерпание ёмкости сессий
- alert: SIPSessionCapacityExhaustion
  expr: sip_exporter_sessions_utilization > 90
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Ёмкость сессий близка к исчерпанию"
    description: "Оператор {{ $labels.carrier }} на {{ $value | printf \"%.0f\" }}% от настроенного лимита сессий."
```

---

## Ограничения

**Сканирование регистраций:**
- Отслеживает только *успешные* (200 OK) регистрации. Для brute-force (401/403) используйте `register_failure_total{code="401"}` с алертом `SIPRegistrationBruteForce`.
- SBC/прокси, распределяющий регистрации по экстеншенам, может вызывать ложные срабатывания. Повысьте порог.
- Ботнеты с ротацией IP могут не достичь порога на один IP. Агрегируйте по всем IP в PromQL.

**Смена страны регистрации:**
- Легитимный роуминг вызывает сигнал — это намеренно, оператор разбирается.
- Если GeoIP отключён и страна оператора не задана → `source_country="unknown"` для всех → детекция не срабатывает.
- Если предыдущая регистрация истекла по TTL до перерегистрации из другой страны → сигнала нет (нет базовой страны).

**Всплеск INVITE:**
- SBC/шлюз, мультиплексирующий абонентов через один IP, может превысить порог=100. Повысьте порог для этого источника.

**False Answer Supervision:**
- Неполный RTP-захват может дать false positive; коррелируйте сигнал с socket- и userspace-drop метриками.
- Voicemail, IVR, paging и announcement-эндпоинты без answer-side RTP триггерят эвристику по дизайну.
- Перед использованием сигнала для enforcement изучите [полные ограничения FAS](METRICS.ru.md#ограничения-fas).

**Утилизация сессий:**
- Ограничено 100% — `активные=300, лимит=100` покажет 100%. Отслеживайте `sip_exporter_sessions` (raw gauge) для экстремальной перегрузки.
- `limit: 0` означает «без лимита» — оператор исключается из этих метрик полностью.
