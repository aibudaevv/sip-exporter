# Справочник метрик

Все метрики экспортируются на эндпоинте `/metrics` в формате exposition Prometheus.

## Содержание

- [Лейблы](#лейблы)
  - [Лейбл carrier](#лейбл-carrier)
  - [Лейбл типа User-Agent](#лейбл-типа-user-agent)
  - [Лейблы геообогащения](#лейблы-геообогащения)
- [Системные метрики](#системные-метрики) — `packets_total`, `system_error_total`
- [Активные сессии](#активные-сессии) — `sessions`
- [Метрики SIP-запросов](#метрики-sip-запросов) — `invite_total`, `reinvite_total`, `register_total`, `bye_total` и др.
- [Метрики SIP-ответов (по кодам статуса)](#метрики-sip-ответов-по-кодам-статуса) — `100_total`…`606_total`
- [Здоровье регистраций](#здоровье-регистраций) — `register_success_total`, `register_failure_total`, `register_success_ratio`, `active_registrations`
- [Детекция фрода](#детекция-фрода) — `register_scan_total`, `invite_burst_total`, `register_country_change_total`
- [Мониторинг ёмкости](#мониторинг-ёмкости) — `sessions_limit`, `sessions_utilization`
- [Метрики RTP-медиа](#метрики-rtp-медиа) — `rtp_packets_total`, `rtp_mos_score`, `rtp_jitter_milliseconds` и др.
- [Метрики самомониторинга](#метрики-самомониторинга) — `socket_packets_*`, `channel_*`, `parse_errors_total`, `active_trackers`, `active_dialogs`, `build_info`
- [Метрики производительности RFC 6076](#метрики-производительности-rfc-6076)
  - [SER](#session-establishment-ratio-ser) — Session Establishment Ratio
  - [SEER](#session-establishment-effectiveness-ratio-seer) — Session Establishment Effectiveness Ratio
  - [ISA](#ineffective-session-attempts-isa) — Ineffective Session Attempts
  - [SCR](#session-completion-ratio-scr) — Session Completion Ratio
  - [ASR](#answer-seizure-ratio-asr) — Answer Seizure Ratio
  - [NER](#network-effectiveness-ratio-ner) — Network Effectiveness Ratio
  - [SDC](#session-duration-counter-sdc) — Session Duration Counter
  - [ISS](#ineffective-session-severity-iss) — Ineffective Session Severity
  - [RRD](#registration-request-delay-rrd) — Registration Request Delay
  - [SPD](#session-process-duration-spd) — Session Process Duration
  - [TTR](#time-to-first-response-ttr) — Time to First Response
  - [PDD](#post-dial-delay-pdd) — Post Dial Delay
  - [ORD](#options-response-delay-ord) — OPTIONS Response Delay
  - [LRD](#location-registration-delay-lrd) — Location Registration Delay
- [Метрики качества голоса (RFC 6035)](#метрики-качества-голоса-rfc-6035) — NLR, JDR, BLD, GLD, RTD, ESD, IAJ, MAJ, MOSLQ, MOSCQ, RLQ, RCQ, RERL
- [Алерты](#алерты) — преднастроенные правила и рекомендации по порогам

## Лейблы

SIP-метрики используют многоуровневую модель лейблов. Большинство метрик включают **три базовых лейбла**; счётчики INVITE добавляют ещё **три**:

| Лейбл | Область | Значение | Описание |
|-------|---------|----------|----------|
| `carrier` | Базовый (все SIP) | Имя оператора из конфига | Source IP → CIDR-маппинг, разрешается при запросе |
| `ua_type` | Базовый (все SIP) | Тип UA из конфига | `User-Agent` → regex-маппинг, разрешается при запросе |
| `source_country` | Базовый (все SIP) | ISO 3166-1 alpha-2 | Страна вызывающего устройства. См. [Лейблы геообогащения](#лейблы-геообогащения) |
| `destination_country` | Только INVITE | ISO alpha-2 или `"unknown"` | Страна назначения по префиксу номера E.164. См. [Лейблы геообогащения](#лейблы-геообогащения) |
| `caller_host` | Только INVITE (**opt-in**) | IP или домен | Хост-часть SIP URI из заголовка `From` |
| `called_host` | Только INVITE (**opt-in**) | IP или домен | Хост-часть SIP URI из заголовка `To` |

> **Примечание:** Сигнатуры отдельных метрик ниже показывают `{carrier="...",ua_type="..."}` для краткости. Используйте эту таблицу, чтобы определить полный набор лейблов для любой метрики:
>
> | Уровень | Метрики | Полный набор лейблов |
> |---------|---------|----------------------|
> | **Системные** | `packets_total`, `system_error_total`, самомониторинг | *(нет)* |
> | **Базовый** | Все SIP-запросы, SER/SEER/ISA/SCR/ASR/NER, RRD/SPD/TTR/PDD/ORD/LRD, VQ-отчёты, sessions, `reinvite_total`, здоровье регистраций (`register_success_total`, `register_success_ratio`, `active_registrations`) | `carrier, ua_type, source_country` |
> | **Ошибки рег.** | `register_failure_total` | `carrier, ua_type, source_country, code` |
> | **RTP** | `rtp_packets_total`, `rtp_packets_lost_total`, `rtp_duplicate_packets_total`, `rtp_jitter_milliseconds`, `rtp_mos_score`, `rtp_mos_f1`, `rtp_mos_f2`, `rtp_mos_adaptive`, `rtp_r_factor`, `rtp_burst_loss_density`, `rtp_gap_loss_density`, `rtp_active_streams` | `carrier, ua_type, codec, source_country` |
> | **RTP-диалог** | `rtp_oneway_calls_total`, `sessions_missing_rtp_total` | `carrier, ua_type, source_country` |
> | **INVITE raw** | `invite_total`, `invite_200_total` | `carrier, ua_type, source_country, destination_country, caller_host, called_host` |
> | **Фрод** | `register_country_change_total`, `register_scan_total`, `invite_burst_total` | `carrier, source_country` |
> | **Ёмкость** | `sessions_limit`, `sessions_utilization` | `carrier` |

`carrier` и `ua_type` по умолчанию `"other"`, если не настроены или нет совпадения. `source_country` по умолчанию `"unknown"`, если ни `carrier.country`, ни GeoIP-БД недоступны.

**Пример:**
```
sip_exporter_invite_total{carrier="carrier-a",ua_type="yealink",source_country="RU",destination_country="US",caller_host="10.1.5.20",called_host="sip.example.com"} 1523
sip_exporter_200_total{carrier="carrier-a",ua_type="yealink",source_country="RU"} 847
sip_exporter_ser{carrier="carrier-a",ua_type="yealink",source_country="RU"} 95.2
```

### Метрики БЕЗ лейблов `carrier` и `ua_type`

Следующие метрики являются системными и не включают ни один из этих лейблов:

- `sip_exporter_system_error_total` — внутренние ошибки экспортёра (не SIP-трафик)
- `sip_exporter_packets_total` — считает все разобранные SIP-пакеты независимо от источника

### Поведение по умолчанию

- Если конфиг операторов не предоставлен (`SIP_EXPORTER_CARRIERS_CONFIG` не задан), все SIP-метрики используют `carrier="other"`.
- Если конфиг user-agents не предоставлен (`SIP_EXPORTER_USER_AGENTS_CONFIG` не задан), все SIP-метрики используют `ua_type="other"`.

### Лейбл carrier

Лейбл `carrier` идентифицирует сетевого оператора, который **инициировал** SIP-транзакцию. Он разрешается из IP-адреса источника **запроса** (INVITE, REGISTER, OPTIONS) и распространяется на все связанные ответы и события жизненного цикла диалога через трекер.

### Конфигурация

| Переменная | По умолчанию | Обязательна | Описание |
|------------|--------------|-------------|----------|
| `SIP_EXPORTER_CARRIERS_CONFIG` | — | нет | Путь к YAML-файлу с CIDR-маппингом операторов |
| `SIP_EXPORTER_USER_AGENTS_CONFIG` | — | нет | Путь к YAML-файлу с маппингом User-Agent → тип |

**Формат конфигурационного файла:**
```yaml
carriers:
  - name: "mobile-operator-a"
    cidrs:
      - "10.1.0.0/16"
      - "10.2.0.0/16"

  - name: "sip-trunk-provider"
    cidrs:
      - "192.168.10.0/24"
      - "192.168.11.0/24"

  - name: "enterprise-pbx"
    cidrs:
      - "172.16.5.0/24"
```

См. [`examples/carriers.yaml`](../examples/carriers.yaml) — полный пример.

#### Как заполнять конфигурацию

- `name` — произвольная строка, используемая как значение лейбла `carrier` в Prometheus. Избегайте пробелов и спецсимволов.
- `cidrs` — список IPv4-подсетей в CIDR-нотации. Указывайте подсети **устройств, отправляющих SIP-запросы** (телефоны, SBC, АТС, SIP-прокси).
- **Порядок важен**: первое совпавшее CIDR-правило выигрывает (first match wins).
- IP-адреса, не попавшие ни в одно CIDR → `carrier="other"`.
- Если `SIP_EXPORTER_CARRIERS_CONFIG` не задан → все метрики используют `carrier="other"`.

**Рекомендации:**
- Группируйте CIDR по **логическому владельцу** (оператор, клиент, VLAN).
- Для мониторинга качества по клиентам: каждый клиент = отдельный carrier со своими подсетями.
- Для мониторинга по upstream-провайдерам: каждый провайдер = отдельный carrier.
- Избегайте пересекающихся CIDR между операторами — порядок может вызвать неожиданные результаты.

### Алгоритм разрешения

Оператор определяется в момент **запроса** и наследуется всеми ответами в той же транзакции:

```
1. Поступает SIP-запрос (INVITE/REGISTER/OPTIONS):
   - Source IP извлекается из IPv4-заголовка
   - IP сопоставляется с настроенными CIDR (first match wins)
   - Если нет совпадения — проверяется destination IP как fallback
   - Если нет совпадения → carrier="other"
   - Оператор сохраняется в трекере по Call-ID

2. Поступает SIP-ответ:
   - Оператор извлекается из трекера (по Call-ID), НЕ из IP ответа
   - Ответы на INVITE → carrier из inviteTracker
   - Ответы на REGISTER → carrier из registerTracker
   - Ответы на OPTIONS → carrier из optionsTracker
   - Если запись в трекере истекла (TTL 60с) → fallback на IP пакета ответа

3. Жизненный цикл диалога:
   - Диалог создаётся с carrier из INVITE-трекера
   - BYE 200 OK → carrier из записи диалога
   - Истечение Session-Expires → carrier из записи диалога
```

### Семантика carrier по метрикам

| Метрика | Источник carrier | Значение |
|---------|-----------------|----------|
| `invite_total{carrier}` | IP отправителя INVITE | Сколько вызовов инициировал этот оператор |
| `200_total{carrier}` | Трекер запроса | Сколько 200 OK для транзакций этого оператора |
| `sessions{carrier}` | Трекер INVITE → диалог | Активные диалоги, инициированные этим оператором |
| SER, SEER, ISA, SCR, ASR, NER | Трекер INVITE | Качество вызовов, инициированных этим оператором |
| RRD | Трекер REGISTER | Задержка регистрации для этого оператора |
| TTR | Трекер INVITE | Время до первого ответа для INVITE этого оператора |
| PDD | Трекер INVITE | Post Dial Delay (INVITE → 180 Ringing) для INVITE этого оператора |
| SPD | Диалог (carrier из INVITE) | Длительность сессий, инициированных этим оператором |
| ORD | Трекер OPTIONS | Задержка ответа OPTIONS для этого оператора |
| LRD | Трекер REGISTER | Задержка редиректа регистрации для этого оператора |
| `system_error_total` | Без carrier | Системные ошибки |
| `packets_total` | Без carrier | Все SIP-пакеты |

### Пример сценария

```
Конфигурация:
   carrier-A: 10.0.1.0/24  (мобильный оператор, отправляет INVITE)
   carrier-B: 10.0.2.0/24  (SIP-платформа, отвечает 200 OK)

Сценарий: абонент carrier-A звонит через платформу carrier-B

Пакет                   | Source IP  | Carrier           | Источник
INVITE                  | 10.0.1.5   | carrier-A         | IP → трекер
100 Trying              | 10.0.2.5   | carrier-A         | inviteTracker
200 OK                  | 10.0.2.5   | carrier-A         | inviteTracker
ACK                     | 10.0.1.5   | carrier-A         | IP (запрос)
BYE                     | 10.0.1.5   | carrier-A         | IP (запрос)
200 OK to BYE           | 10.0.2.5   | carrier-A         | запись диалога

Результат:
  invite_total{carrier="carrier-A",ua_type="yealink"} += 1
  invite200OK_total{carrier="carrier-A",ua_type="yealink"} += 1
  sessions{carrier="carrier-A",ua_type="yealink"} = N
  sessionCompleted_total{carrier="carrier-A",ua_type="yealink"} += 1
  SER{carrier="carrier-A",ua_type="yealink"} корректен

Метрики carrier-B: только счётчики ответов для нетрекаемых пакетов (если есть)
```

**Аналитический смысл:** `carrier` представляет **инициатора вызова**. Это соответствует:
- Биллинговой модели (платит звонящий)
- Метрикам RFC 6076 (все привязаны к инициатору INVITE)
- Планированию ёмкости («сколько сессий генерирует этот оператор?»

---

## Лейбл типа User-Agent

Лейбл `ua_type` идентифицирует **тип SIP-устройства**, отправившего запрос, на основе заголовка `User-Agent`. Он разрешается по regex-шаблонам из YAML-конфига и распространяется на все связанные ответы и события жизненного цикла диалога через тот же механизм трекера, что и `carrier`.

**Почему это важно:**
- Разные SIP-устройства имеют разные паттерны сбоев — IP-телефоны ломаются не так, как софтфоны или SBC
- Траблшутинг: «Телефоны Yealink получают 408 timeout, а Grandstream — нет» — невозможно увидеть без `ua_type`
- Планирование ёмкости: «сколько вызовов от мобильных клиентов vs. настольных телефонов?»

### Поведение по умолчанию

Если конфиг user-agents не предоставлен (`SIP_EXPORTER_USER_AGENTS_CONFIG` не задан), все SIP-метрики используют `ua_type="other"`.

### Конфигурация

| Переменная | По умолчанию | Обязательна | Описание |
|------------|--------------|-------------|----------|
| `SIP_EXPORTER_USER_AGENTS_CONFIG` | — | нет | Путь к YAML-файлу с regex-шаблонами User-Agent |

**Формат конфигурационного файла:**
```yaml
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

  - regex: '(?i)^FreeSWITCH'
    label: freeswitch
```

См. [`examples/user_agents.yaml`](../examples/user_agents.yaml) — полный пример с 11 шаблонами.

#### Как заполнять конфигурацию

- `regex` — регулярное выражение (Go-совместимое), применяемое к полному значению заголовка `User-Agent`. Используйте префикс `(?i)` для регистронезависимого сопоставления или привязку `^Yealink` для совпадения с начала.
- `label` — произвольная строка, используемая как значение лейбла `ua_type` в Prometheus. Избегайте пробелов и спецсимволов.
- **Порядок важен**: первое совпавшее regex-правило выигрывает (first match wins).
- Значения `User-Agent`, не попавшие ни под один regex → `ua_type="other"`.
- Если `SIP_EXPORTER_USER_AGENTS_CONFIG` не задан → все метрики используют `ua_type="other"`.

**Рекомендации:**
- Группируйте шаблоны по **семейству устройств** (Yealink SIP-T46S, SIP-T42S → оба совпадают с `^Yealink`).
- Используйте широкие шаблоны для семейств, узкие — для проблемных моделей.
- Типичные категории: IP-телефоны (Yealink, Grandstream, Cisco), софтфоны (MicroSIP, Zoiper, Linphone), серверы (Asterisk, Kamailio, FreeSWITCH), SBC.
- Заголовок `User-Agent` извлекается из всех SIP-пакетов (запросы и ответы). Однако SIP-ответы обычно используют заголовок `Server` вместо `User-Agent`, поэтому на практике только запросы дают осмысленную классификацию.

### Алгоритм разрешения

Тип UA определяется в момент **запроса** и наследуется всеми ответами в той же транзакции через тот же механизм трекера, что и `carrier`:

```
1. Поступает SIP-запрос (INVITE/REGISTER/OPTIONS):
   - Заголовок User-Agent извлекается из SIP-пакета
   - Значение сопоставляется с настроенными regex-шаблонами (first match wins)
   - Если нет совпадения → ua_type="other"
   - ua_type сохраняется в трекере по Call-ID (вместе с carrier)

2. Поступает SIP-ответ:
   - ua_type извлекается из трекера (по Call-ID), НЕ из пакета ответа
   - Ответы на INVITE → ua_type из inviteTracker
   - Ответы на REGISTER → ua_type из registerTracker
   - Ответы на OPTIONS → ua_type из optionsTracker
   - Если запись в трекере истекла (TTL 60с) → fallback на User-Agent пакета ответа
   - Если в ответе нет User-Agent → ua_type="other"

3. Жизненный цикл диалога:
   - Диалог создаётся с ua_type из INVITE-трекера
   - BYE 200 OK → ua_type из записи диалога
   - Истечение Session-Expires → ua_type из записи диалога
```

### Семантика ua_type по метрикам

| Метрика | Источник UA type | Значение |
|---------|-----------------|----------|
| `invite_total{ua_type}` | User-Agent из INVITE | Сколько вызовов инициировал этот тип устройств |
| `200_total{ua_type}` | Трекер запроса | Сколько 200 OK для транзакций этого типа устройств |
| `sessions{ua_type}` | Трекер INVITE → диалог | Активные диалоги от этого типа устройств |
| SER, SEER, ISA, SCR, ASR, NER | Трекер INVITE | Качество вызовов от этого типа устройств |
| RRD | Трекер REGISTER | Задержка регистрации для этого типа устройств |
| TTR | Трекер INVITE | Время до первого ответа для этого типа устройств |
| PDD | Трекер INVITE | Post Dial Delay для этого типа устройств |
| SPD | Диалог (ua_type из INVITE) | Длительность сессий от этого типа устройств |
| ORD | Трекер OPTIONS | Задержка ответа OPTIONS для этого типа устройств |
| LRD | Трекер REGISTER | Задержка редиректа регистрации для этого типа устройств |
| `system_error_total` | Без ua_type | Системные ошибки |
| `packets_total` | Без ua_type | Все SIP-пакеты |

### Пример сценария

```
Конфигурация:
  ua_type patterns:
    - regex: '(?i)^Yealink'     → label: yealink
    - regex: '(?i)^Grandstream' → label: grandstream

Сценарий: телефон Yealink звонит через SIP-платформу

Пакет                        | User-Agent              | ua_type           | Источник
INVITE                       | Yealink SIP-T46S 66.15  | yealink           | заголовок → трекер
100 Trying                   | (нет / Server header)   | yealink           | inviteTracker
200 OK                       | (нет / Server header)   | yealink           | inviteTracker
ACK                          | Yealink SIP-T46S 66.15  | yealink           | заголовок (запрос)
BYE                          | Yealink SIP-T46S 66.15  | yealink           | заголовок (запрос)
200 OK to BYE                | (нет / Server header)   | yealink           | запись диалога

Результат:
  invite_total{carrier="...",ua_type="yealink"} += 1
  sessions{carrier="...",ua_type="yealink"} = N
  SER{carrier="...",ua_type="yealink"} корректен
  SPD{carrier="...",ua_type="yealink"} зафиксирован

INVITE от Grandstream с "Grandstream GXP2160" → ua_type="grandstream"
Неизвестное устройство "SomeUnknownClient/1.0" → ua_type="other"
Нет заголовка User-Agent вообще               → ua_type="other"
```

**Аналитический смысл:** `ua_type` представляет **тип устройства инициатора вызова**. Это соответствует:
- Траблшутингу по семейству устройств («У Yealink низкий SER»)
- Детекции проблем прошивки («конкретная модель получает 503»)
- Планированию ёмкости по типу устройств («сколько вызовов от мобильных софтфонов?»)

### Комбинированный анализ carrier + ua_type

Оба лейбла работают вместе для двумерного анализа:

```promql
# SER по оператору И типу устройства
sip_exporter_ser

# SER для телефонов Yealink на carrier-a
sip_exporter_ser{carrier="carrier-a",ua_type="yealink"}

# Сравнение Yealink vs Grandstream у одного оператора
sip_exporter_ser{carrier="carrier-a",ua_type="yealink"}
  - sip_exporter_ser{carrier="carrier-a",ua_type="grandstream"}

# Активные сессии по типам устройств (по всем операторам)
sum by (ua_type) (sip_exporter_sessions)

# Rate INVITE по оператору и типу устройства
sum by (carrier, ua_type) (rate(sip_exporter_invite_total[5m]))
```

---

## Лейблы геообогащения

SIP Exporter обогащает метрики географическим контекстом, используя **двухметодную модель**:

| Измерение | Метод | Основа | Лейблы |
|-----------|-------|--------|--------|
| **Страна источника** (откуда идёт вызов) | GeoIP-поиск по IP источника | MaxMind GeoLite2-Country DB | `source_country` |
| **Страна назначения** (куда идёт вызов) | Префикс номера E.164 | Встроенная таблица (Google libphonenumber, Apache 2.0) | `destination_country` |

GeoIP для IP источника, префикс номера для назначения — два независимых метода, каждый оптимальный для своей задачи.

### source_country

**Приоритет разрешения:**

```
1. carrier.country  →  если у оператора есть поле "country" в carriers.yaml, оно в приоритете
2. GeoIP(srcIP)     →  поиск по MaxMind GeoLite2-Country для IP источника
3. "unknown"        →  fallback, если ни то, ни другое недоступно
```

- **carrier.country** (кураторский, авторитетный): укажите `country: "RU"` у оператора в `carriers.yaml` — переопределяет GeoIP для всех IP из CIDR этого оператора
- **GeoIP**: требуется `GeoLite2-Country.mmdb` (скачать с [maxmind.com](https://www.maxmind.com)). Приватные IP (RFC 1918: `10.x`, `172.16-31.x`, `192.168.x`) не дают результата в MaxMind — используйте `carrier.country` для enterprise/contact-center
- **Без GeoIP-БД**: все лейблы `source_country` — `"unknown"`, если не задан `carrier.country` — нулевая дополнительная кардинальность

**Конфиг:**

| Переменная | По умолчанию | Описание |
|------------|--------------|----------|
| `SIP_EXPORTER_GEOIP_COUNTRY_DB` | (пусто = выключено) | Путь к `GeoLite2-Country.mmdb` |

**Влияние на кардинальность:** ~250 ISO alpha-2 кодов. С выключенным GeoIP и без `carrier.country` каждая метрика имеет `source_country="unknown"` — та же кардинальность, что и без лейбла.

### destination_country

**Логика разрешения:**

```
1. Номер начинается с "+" или "00"  →  E.164 longest-prefix match (напр. "+7495..." → RU)
2. Иначе, если задан LOCAL_COUNTRY_CODE  →  используется этот код (fallback для внутренних номеров)
3. Иначе                             →  "unknown"
```

- **Таблица E.164** встроена в бинарник (сгенерирована из Google libphonenumber `PhoneNumberMetadata.xml`, Apache 2.0). **Не требует скачивания БД** — в отличие от GeoIP
- Корректно обрабатывает мультинациональные коды: `+1212...`→US, `+1416...`→CA (Торонто), `+7727...`→KZ (Алматы), `+7495...`→RU
- **Только для INVITE**: `destination_country` появляется только на `invite_total` и `invite_200_total` (не на счётчиках ответов, SER/SCR, RTP и т.д.)

**Конфиг:**

| Переменная | По умолчанию | Описание |
|------------|--------------|----------|
| `SIP_EXPORTER_LOCAL_COUNTRY_CODE` | (пусто = выкл.) | ISO alpha-2 код для внутренних номеров без международного префикса (напр. `"RU"`, `"US"`) |

**Пример:**
```
# INVITE на +74951234567 (Москва) с включённым GeoIP
sip_exporter_invite_total{carrier="carrier-a",ua_type="yealink",source_country="RU",destination_country="RU",caller_host="10.1.5.20",called_host="sip.operator.com"} 100

# INVITE на +442071838750 (Лондон)
sip_exporter_invite_total{...,destination_country="GB"} 50
```

### caller_host / called_host

Хост-часть SIP URI из заголовков `From` и `To` соответственно. Извлекается при разборе пакета из `<sip:user@host:port>`.

- Может быть IP-адресом (`10.1.5.20`) или доменным именем (`sip.example.com`)
- **Только для INVITE**: появляются только на `invite_total` и `invite_200_total`
- **Opt-in** (`SIP_EXPORTER_HOST_LABELS`, по умолчанию `false`): выключены по умолчанию, т.к. количество уникальных эндпоинтов не ограничено. В выключенном состоянии оба лейбла пусты (нулевая кардинальность). Включайте только в доверенных развёртываниях с ограниченным числом эндпоинтов — см. [Security](SECURITY.md#data-exposed-in-prometheus-labels).

**Конфиг:**

| Переменная | По умолчанию | Описание |
|------------|--------------|----------|
| `SIP_EXPORTER_HOST_LABELS` | `false` | Включить `caller_host`/`called_host` на `invite_total` / `invite_200_total`. По умолчанию выключено (неограниченная кардинальность). |

### SER по направлению (PromQL)

Ratio-метрики (SER, SEER, ISA, SCR) несут `source_country`, но **не** `destination_country` (контроль кардинальности). Чтобы вычислить SER для конкретного направления, используйте PromQL на сырых счётчиках INVITE:

```promql
# SER для вызовов в Россию = успешные / общие INVITE
# (аппроксимация: 3xx не исключаются — см. примечание ниже)
sum(rate(sip_exporter_invite_200_total{destination_country="RU"}[5m]))
  / sum(rate(sip_exporter_invite_total{destination_country="RU"}[5m])) * 100

# Rate INVITE по странам назначения
sum by (destination_country) (rate(sip_exporter_invite_total[5m]))

# Топ-10 стран назначения по объёму вызовов
topk(10, sum by (destination_country) (rate(sip_exporter_invite_total[5m])))
```

> **Почему без исключения 3xx?** Строгий SER (по формуле в [Session Establishment Ratio (SER)](#session-establishment-ratio-ser)) вычитает 3xx-ответы из знаменателя. Но `300_total` — это счётчик ответов, и он **не** несёт `destination_country`, поэтому 3xx нельзя разделить по направлению в PromQL. Используется аппроксимация (200 OK / общие INVITE); для большинства развёртываний доля 3xx невелика и разница пренебрежимо мала.

---

`sip_exporter_packets_total`: общее количество разобранных SIP-пакетов (запросы + ответы). **Без лейблов `carrier` и `ua_type`.**

## Активные сессии

`sip_exporter_sessions{carrier="...",ua_type="..."}`: количество активных SIP-диалогов (RFC 3261).

**Как считаются диалоги:**
- Диалог создаётся при получении ответа `200 OK` на запрос `INVITE`
- Диалог идентифицируется кортежем: `{Call-ID, From tag, To tag}`
- Диалог завершается при получении ответа `200 OK` на запрос `BYE`
- Формат ID диалога: `{call-id}:{min-tag}:{max-tag}` (теги отсортированы лексикографически)
- Re-INVITE (INVITE внутри существующего диалога) **обновляет** таймер Session-Expires диалога без создания нового диалога или инкремента `invite_total`
- Диалоги очищаются, когда:
  - Получен `200 OK` на `BYE` (нормальное завершение)
  - Достигнут таймаут Session-Expires (RFC 4028)
- Таймаут по умолчанию: 1800 секунд (30 мин), если заголовок `Session-Expires` отсутствует
- Очистка выполняется каждую 1 секунду

## Метрики SIP-запросов

`sip_exporter_invite_total{carrier="...",ua_type="..."}`: общее количество полученных SIP INVITE-запросов. Re-INVITE (INVITE внутри существующего диалога) **исключаются** — см. `reinvite_total` ниже.  
`sip_exporter_reinvite_total{carrier="...",ua_type="..."}`: общее количество re-INVITE-запросов (INVITE внутри уже установленного диалога, RFC 3261 §14). Re-INVITE обновляют таймер Session-Expires без создания нового диалога и не искажают SER/SCR/ASR.  
`sip_exporter_register_total{carrier="...",ua_type="..."}`: общее количество полученных SIP REGISTER-запросов.  
`sip_exporter_options_total{carrier="...",ua_type="..."}`: общее количество полученных SIP OPTIONS-запросов.  
`sip_exporter_cancel_total{carrier="...",ua_type="..."}`: общее количество полученных SIP CANCEL-запросов.  
`sip_exporter_bye_total{carrier="...",ua_type="..."}`: общее количество полученных SIP BYE-запросов.  
`sip_exporter_ack_total{carrier="...",ua_type="..."}`: общее количество полученных SIP ACK-запросов.  
`sip_exporter_publish_total{carrier="...",ua_type="..."}`: общее количество полученных SIP PUBLISH-запросов.  
`sip_exporter_prack_total{carrier="...",ua_type="..."}`: общее количество полученных SIP PRACK-запросов.  
`sip_exporter_notify_total{carrier="...",ua_type="..."}`: общее количество полученных SIP NOTIFY-запросов.  
`sip_exporter_subscribe_total{carrier="...",ua_type="..."}`: общее количество полученных SIP SUBSCRIBE-запросов.  
`sip_exporter_refer_total{carrier="...",ua_type="..."}`: общее количество полученных SIP REFER-запросов.  
`sip_exporter_info_total{carrier="...",ua_type="..."}`: общее количество полученных SIP INFO-запросов.  
`sip_exporter_update_total{carrier="...",ua_type="..."}`: общее количество полученных SIP UPDATE-запросов.  
`sip_exporter_message_total{carrier="...",ua_type="..."}`: общее количество полученных SIP MESSAGE-запросов.

`sip_exporter_invite_200_total{carrier,ua_type,source_country,destination_country,caller_host,called_host}`: общее количество ответов `200 OK` на INVITE-запросы (успешные установления вызовов). Это числитель для [SER по направлению](#ser-по-направлению-promql) в PromQL. Несёт полный набор из 6 лейблов — как `invite_total`.

## Метрики SIP-ответов (по кодам статуса)

`sip_exporter_100_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 100 Trying.  
`sip_exporter_180_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 180 Ringing.  
`sip_exporter_181_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 181 Call Is Being Forwarded.  
`sip_exporter_182_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 182 Queued.  
`sip_exporter_183_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 183 Session Progress.  
`sip_exporter_200_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 200 OK.  
`sip_exporter_202_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 202 Accepted.  
`sip_exporter_300_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 300 Multiple Choices.  
`sip_exporter_302_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 302 Moved Temporarily.  
`sip_exporter_400_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 400 Bad Request.  
`sip_exporter_401_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 401 Unauthorized.  
`sip_exporter_403_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 403 Forbidden.  
`sip_exporter_404_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 404 Not Found.  
`sip_exporter_405_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 405 Method Not Allowed.  
`sip_exporter_proxy_authentication_required_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 407 Proxy Authentication Required.  
`sip_exporter_408_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 408 Request Timeout.  
`sip_exporter_480_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 480 Temporarily Unavailable.  
`sip_exporter_481_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 481 Dialog/Transaction Does Not Exist.  
`sip_exporter_486_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 486 Busy Here.  
`sip_exporter_487_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 487 Request Terminated.  
`sip_exporter_488_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 488 Not Acceptable Here.  
`sip_exporter_500_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 500 Server Internal Error.  
`sip_exporter_501_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 501 Not Implemented.  
`sip_exporter_502_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 502 Bad Gateway.  
`sip_exporter_503_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 503 Service Unavailable.  
`sip_exporter_504_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 504 Server Time-out.  
`sip_exporter_600_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 600 Busy Everywhere.  
`sip_exporter_603_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 603 Decline.  
`sip_exporter_604_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 604 Does Not Exist Anywhere.  
`sip_exporter_606_total{carrier="...",ua_type="..."}`: общее количество ответов SIP 606 Not Acceptable.  

## Здоровье регистраций

Метрики регистраций отслеживают полный жизненный цикл SIP-регистраций (RFC 3261 §10): исходы успеха/неудачи, вычисляемый success ratio и количество активных регистраций. Все разбиты по `carrier,ua_type,source_country`.

| Метрика | Тип | Лейблы | Описание |
|---------|-----|--------|----------|
| `sip_exporter_register_success_total` | counter | `carrier,ua_type,source_country` | Ответы REGISTER со статусом `200 OK` |
| `sip_exporter_register_failure_total` | counter | `carrier,ua_type,source_country,code` | Ответы REGISTER со статусом `3xx/4xx/5xx/6xx`, по коду |
| `sip_exporter_register_success_ratio` | gauge | `carrier,ua_type,source_country` | `200 OK / (200 OK + терминальные ошибки) × 100` |
| `sip_exporter_active_registrations` | gauge | `carrier,ua_type,source_country` | Активные регистрации (отслеживание по Expires-TTL) |

### register_success_ratio

```
register_success_ratio = (REGISTER → 200 OK) / (REGISTER → 200 OK + терминальные ошибки) × 100
```

- **Терминальные ошибки** = не-200 ответы **за исключением** `401 Unauthorized` и `407 Proxy Authentication Required` (это challenge digest-auth — нормальная часть хендшейка регистрации, а не реальные ошибки) и `3xx` редиректов.
- Исключение challenge-ответов делает ratio осмысленным на системах с SIP digest-auth: здоровый auth-флоу (`REGISTER → 401 → REGISTER+creds → 200 OK`) даёт ratio около 100%, а не ~50%.
- Не определено (эмитит `0`), если не было ни успешных, ни терминально-неудачных регистраций.

> **Примечание:** `register_failure_total{code}` считает **все** не-1xx/не-2xx ответы, включая `401`/`407`. Поэтому сумма по кодам превышает количество ошибок, используемое в знаменателе ratio. Используйте `register_failure_total{code="401"}` для детекции brute-force (см. [ALERTING.ru.md](ALERTING.ru.md)), и `register_success_ratio` для общего здоровья регистраций.

### active_registrations

- Инкрементируется при каждом `REGISTER → 200 OK`, с ключом по Address-of-Record (`user@host` из URI заголовка `From`).
- Каждая запись имеет TTL из заголовка `Expires` (RFC 3261 §20.19); по умолчанию **3600 с**, если отсутствует.
- **Обновление** (тот же AOR, новый `200 OK`) обновляет TTL — **не** создаёт дубликат.
- Записи удаляются фоновой очисткой (каждую 1 с) по истечении TTL; gauge обновляется соответственно.

**Примеры PromQL:**
```promql
# Success ratio регистраций по операторам
sip_exporter_register_success_ratio

# Активные регистрации во времени (rate churn)
rate(sip_exporter_register_success_total[5m])

# Топ кодов ошибок
topk(5, sum by (code) (rate(sip_exporter_register_failure_total[5m])))
```

## Детекция фрода

Сигналы фрода детектируют подозрительные паттерны: сканирование регистраций (один IP регистрирует множество аккаунтов), географическую невозможность (тот же аккаунт из разных стран) и флуд INVITE (один IP отправляет всплеск вызовов). Все разбиты по `carrier,source_country` — `ua_type` намеренно опущен, т.к. атакующие варьируют User-Agent.

| Метрика | Тип | Лейблы | Описание |
|---------|-----|--------|----------|
| `sip_exporter_register_country_change_total` | counter | `carrier,source_country` | Количество смен страны регистрации (сигнал перехвата аккаунта) |
| `sip_exporter_register_scan_total` | counter | `carrier,source_country` | Сигналы сканирования регистраций (один IP регистрирует N+ уникальных AOR за окно) |
| `sip_exporter_invite_burst_total` | counter | `carrier,source_country` | Сигналы всплеска INVITE (один IP отправляет N+ INVITE за окно) |

### register_country_change_total

Инкрементируется, когда успешная REGISTER 200 OK приходит из **другой страны источника**, чем предыдущая регистрация для того же AOR (Address-of-Record). AOR извлекается из URI заголовка `From`.

- Первая регистрация AOR **не** триггерит сигнал (нет baseline)
- Обновление из той же страны **не** триггерит сигнал
- Пустая предыдущая страна (GeoIP выключен при первой регистрации) **не** триггерит сигнал

```promql
# Всплески перехвата аккаунтов
sip_exporter_register_country_change_total > 0 unless on (carrier, source_country) (sip_exporter_register_country_change_total offset 5m > 0)
```

### register_scan_total

Инкрементируется для каждого нового **уникального AOR**, зарегистрированного с одного IP, когда количество уникальных AOR в течение настроенного `window` достигает или превышает `threshold`. Счётчик растёт непрерывно во время атаки, что делает `rate()` эффективным для алертинга.

| Параметр | Переменная окружения | По умолчанию |
|----------|---------------------|--------------|
| Порог | `SIP_EXPORTER_FRAUD_REGISTER_SCAN_THRESHOLD` | `10` |
| Окно | `SIP_EXPORTER_FRAUD_REGISTER_SCAN_WINDOW` | `60s` |

```promql
# Атаки перечисления регистраций
rate(sip_exporter_register_scan_total[5m])
```

### invite_burst_total

Инкрементируется для каждого **INVITE-запроса** (без re-INVITE) с одного IP, когда количество INVITE в течение настроенного `window` достигает или превышает `threshold`. Счётчик растёт непрерывно во время всплеска, что делает `rate()` эффективным для алертинга.

| Параметр | Переменная окружения | По умолчанию |
|----------|---------------------|--------------|
| Порог | `SIP_EXPORTER_FRAUD_INVITE_BURST_THRESHOLD` | `100` |
| Окно | `SIP_EXPORTER_FRAUD_INVITE_BURST_WINDOW` | `60s` |

```promql
# Toll fraud или DDoS через флуд INVITE
rate(sip_exporter_invite_burst_total[5m])
```

## Мониторинг ёмкости

| Метрика | Тип | Лейблы | Описание |
|---------|-----|--------|----------|
| `sip_exporter_sessions_limit` | gauge | `carrier` | Настроенный лимит одновременных сессий на оператора (из YAML-конфига) |
| `sip_exporter_sessions_utilization` | gauge | `carrier` | Утилизация сессий как процент от лимита (0–100, с ограничением) |

### Конфигурация

Создайте YAML-файл и укажите путь через `SIP_EXPORTER_SESSIONS_LIMITS`:

```yaml
sessions_limits:
  - carrier: "beeline"
    limit: 500
  - carrier: "mts"
    limit: 1000
```

- Утилизация вычисляется при каждом скрейпе: `active_sessions(carrier) / limit × 100`
- Ограничено 100 (переполнение показывается как 100, а не >100) — это скрывает тяжесть переподписки; используйте raw gauge `sip_exporter_sessions` для детекции крайнего превышения
- Операторы без настроенного лимита пропускаются (gauge не эмитится)
- Операторы с `limit: 0` также пропускаются (рассматривается как «без лимита», а не «0% / блокирован»)
- Операторы с 0 активных сессий показывают 0% утилизации

```promql
# Запас ёмкости по операторам
100 - sip_exporter_sessions_utilization

# Операторы на грани исчерпания ёмкости
sip_exporter_sessions_utilization > 90
```

## Метрики RTP-медиа

Метрики RTP-медиа извлекаются из RTP-пакетов, захваченных eBPF-фильтром, и
коррелируются с SIP-диалогами через SDP (медиа IP:port → carrier/ua_type/call-id).
Все RTP-метрики несут лейблы `carrier`, `ua_type`, `codec` и `source_country`. Учитывается только RTP,
принадлежащий установленному SIP-диалогу (после 200 OK на INVITE с SDP);
RTP без коррелированного диалога отбрасывается.

`{carrier="...",ua_type="...",codec="...",source_country="..."}` — `codec` — это имя payload type из SDP `a=rtpmap` (напр. `PCMU`, `PCMA`, `opus`) со статичной fallback-таблицей (RFC 3551). `source_country` наследуется от SIP-диалога (разрешается в момент INVITE).

`sip_exporter_rtp_packets_total{carrier,ua_type,codec,source_country}` *(counter)*: общее количество наблюдённых RTP-пакетов.

`sip_exporter_rtp_packets_lost_total{carrier,ua_type,codec,source_country}` *(counter)*: пакеты, обнаруженные как потерянные по разрывам sequence number в RTP.

`sip_exporter_rtp_duplicate_packets_total{carrier,ua_type,codec,source_country}` *(counter)*: дубликаты RTP-пакетов (тот же sequence number, что у предыдущего — ретрансляция или медиа-петля).

`sip_exporter_rtp_jitter_milliseconds{carrier,ua_type,codec,source_country}` *(histogram, бакеты 0.1..500 мс)*: сглаженный interarrival jitter (RFC 3550 A.8).

`sip_exporter_rtp_mos_score{carrier,ua_type,codec,source_country}` *(histogram, бакеты 1.0..5.0)*: MOS-LQ, оценённый по ITU-T G.107 E-model с предположением jitter-буфера 60 мс.

`sip_exporter_rtp_mos_f1{carrier,ua_type,codec,source_country}` *(histogram, бакеты 1.0..5.0)*: MOS-LQ со строгим jitter-буфером (50 мс) — моделирует низколатентные эндпоинты с меньшей толерантностью к jitter.

`sip_exporter_rtp_mos_f2{carrier,ua_type,codec,source_country}` *(histogram, бакеты 1.0..5.0)*: MOS-LQ с щедрым jitter-буфером (200 мс) — моделирует управляемые эндпоинты с глубокими буферами.

`sip_exporter_rtp_mos_adaptive{carrier,ua_type,codec,source_country}` *(histogram, бакеты 1.0..5.0)*: MOS-LQ с адаптивным jitter-буфером (500 мс) — моделирует адаптивные эндпоинты, поглощающие значительный jitter.

`sip_exporter_rtp_r_factor{carrier,ua_type,codec,source_country}` *(histogram, бакеты 10..100)*: E-model R-factor (ITU-T G.107), диапазон 0–100. Базовая оценка качества до преобразования R→MOS.

`sip_exporter_rtp_burst_loss_density{carrier,ua_type,codec,source_country}` *(histogram, бакеты 10..100)*: доля потерянных пакетов, пришедшихся на burst-серии (≥ 3 последовательных потерь), диапазон 0–100.

`sip_exporter_rtp_gap_loss_density{carrier,ua_type,codec,source_country}` *(histogram, бакеты 10..100)*: доля потерянных пакетов, пришедшихся на изолированные gap (< 3 последовательных потерь), диапазон 0–100.

`sip_exporter_rtp_active_streams{carrier,ua_type,codec,source_country}` *(gauge)*: количество активных RTP-потоков. Сэмплируется раз в секунду; простаивающие потоки истекают через 30 с.

> MOS и R-factor сэмплируются по каждому потоку раз в секунду; E-model использует
> кодековые Ie/Bpl факторы из G.113. Неизвестные кодеки получают консервативное
> значение по умолчанию (Ie=10). Burst/gap density использует упрощённую
> эвристику по мотивам RFC 3611: серии из ≥ 3 последовательных потерь
> классифицируются как burst, более короткие — как gap.

> **Ограничение корреляции:** RTP-потоки коррелируются с SIP-диалогами по
> совпадению source IP:port пакета с медиа-эндпоинтами из SDP
> (`c=` IP + `m=` порт). Это требует симметричного RTP (source port равен
> объявленному). При NAT/port remapping (асимметричный RTP) поток не
> сопоставляется и исключается из RTP-метрик; метрики SIP-сигнализации не затрагиваются.
> Будущая работа: port-learning по RFC 4961.

### Метрики качества RTP-диалога

Эти счётчики вычисляются при завершении диалога (BYE 200 OK или истечение Session-Expires) и несут только `carrier, ua_type, source_country` (без лейбла `codec` — они описывают диалог, а не отдельный поток).

`sip_exporter_rtp_oneway_calls_total{carrier,ua_type,source_country}` *(counter)*: диалоги, где 2+ медиа-эндпоинта были зарегистрированы (SDP от обеих сторон), но RTP наблюдался только в одном направлении.

`sip_exporter_sessions_missing_rtp_total{carrier,ua_type,source_country}` *(counter)*: диалоги с SDP-медиаэндпоинтами, но без RTP вообще.

> Обе метрики опираются на персистентную запись RTP по диалогу, переживающую
> истечение TTL потока, обеспечивая точную детекцию даже когда RTP-потоки
> были очищены до завершения диалога.

## Системные метрики

`sip_exporter_system_error_total`: общее количество внутренних ошибок SIP Exporter. **Без лейблов `carrier` и `ua_type`.**

## Метрики самомониторинга

Метрики самомониторинга дают видимость внутреннего здоровья экспортёра. Все они **не имеют лейблов `carrier` и `ua_type`** — они измеряют сам экспортёр, а не SIP-трафик.

| Метрика | Тип | Описание |
|---------|-----|----------|
| `sip_exporter_socket_packets_received_total` | Counter | Всего пакетов, полученных от kernel AF_PACKET-сокета |
| `sip_exporter_socket_packets_dropped_total` | Counter | Всего пакетов, отброшенных ядром из-за переполнения receive-буфера сокета |
| `sip_exporter_channel_length` | Gauge | Текущее количество пакетов во внутреннем буфере канала сообщений |
| `sip_exporter_channel_capacity` | Gauge | Ёмкость внутреннего буфера канала сообщений (константа: 10000) |
| `sip_exporter_parse_errors_total{type="..."}` | CounterVec | Всего ошибок разбора пакетов по типам |
| `sip_exporter_active_trackers{type="..."}` | GaugeVec | Текущее количество записей в картах трекеров |
| `sip_exporter_active_dialogs` | Gauge | Текущее количество активных SIP-диалогов |
| `sip_exporter_build_info{version="..."}` | GaugeFunc | Информация о сборке; константный лейбл `version`, значение всегда `1`. Полезен для инвентарных запросов `count by (version) (sip_exporter_build_info)` |

### Статистика AF_PACKET-сокета

`sip_exporter_socket_packets_received_total` и `sip_exporter_socket_packets_dropped_total` читаются из kernel `PACKET_STATISTICS` через `getsockopt()` каждую секунду. Ядро сбрасывает счётчики после каждого чтения, поэтому значения накапливаются в экспортёре.

**Примеры PromQL:**
```promql
# Rate отброса пакетов (пакетов/с)
rate(sip_exporter_socket_packets_dropped_total[5m])

# Доля отброса (процент от полученных)
rate(sip_exporter_socket_packets_dropped_total[5m])
  / rate(sip_exporter_socket_packets_received_total[5m]) * 100
```

### Ошибки разбора

`sip_exporter_parse_errors_total{type="..."}` считает ошибки разбора по уровням протокола:

| Тип | Уровень | Описание |
|-----|---------|----------|
| `l2` | Ethernet | Пакет слишком короткий для Ethernet/VLAN-заголовка |
| `l3` | IPv4 | Не IPv4-пакет или IP-заголовок слишком короткий |
| `l4` | UDP | Не UDP-пакет или UDP-заголовок слишком короткий |
| `sip` | SIP | Нет SIP-payload, пакет слишком мал или нераспознанный метод |
| `vq` | Voice Quality | Не удалось разобрать тело VQ-отчёта RFC 6035 |

**Примеры PromQL:**
```promql
# Rate ошибок разбора по типам
sum by (type) (rate(sip_exporter_parse_errors_total[5m]))

# Всего ошибок разбора
sum(rate(sip_exporter_parse_errors_total[5m]))
```

### Буфер канала

`sip_exporter_channel_length` показывает, сколько пакетов буферизировано во внутреннем канале между читателем сокета и SIP-парсером. Если это значение приближается к `channel_capacity` (10000), экспортёр не успевает за потоком пакетов и может терять пакеты на уровне ядра.

**Примеры PromQL:**
```promql
# Утилизация канала (процент)
sip_exporter_channel_length / sip_exporter_channel_capacity * 100

# Алерт: канал переполняется
sip_exporter_channel_length / sip_exporter_channel_capacity > 0.8
```

### Активные трекеры

`sip_exporter_active_trackers{type="register|invite|options|rtp"}` показывает количество записей в каждой карте трекеров. Трекеры `register`/`invite`/`options` хранят временные метки для измерения round-trip задержек (RRD, TTR, ORD, LRD) и очищаются через 60 секунд. Трекер `rtp` содержит активные RTP-медиапотоки (коррелированные с SIP-диалогами) и истекает простаивающие потоки через 30 секунд.

**Примеры PromQL:**
```promql
# Активные трекеры по типам
sip_exporter_active_trackers

# Высокий INVITE-трекер указывает на множество ожидающих вызовов
sip_exporter_active_trackers{type="invite"}
```

### Активные диалоги

`sip_exporter_active_dialogs` показывает общее количество активных SIP-диалогов (то же значение, что `sum(sip_exporter_sessions)`, но без кардинальности лейблов). Полезно для быстрых проверок здоровья.

**Примеры PromQL:**
```promql
# Текущие активные диалоги
sip_exporter_active_dialogs

# Алерт: слишком много активных диалогов
sip_exporter_active_dialogs > 10000
```

## Метрики производительности RFC 6076

Все метрики RFC 6076 **разбиты по carrier, ua_type и source_country** — каждый ratio/histogram вычисляется независимо для каждой комбинации лейблов. Это позволяет сравнивать SER, SEER, ISA, SCR, ASR, NER между операторами, типами устройств и странами источника в одном Prometheus-запросе.

**Пример:**
```promql
# SER по операторам
sip_exporter_ser

# Сравнение SER между операторами для конкретного типа устройств
sip_exporter_ser{ua_type="yealink"}

# Сравнение SER между операторами
sip_exporter_ser{carrier="carrier-a"} - sip_exporter_ser{carrier="carrier-b"}
```

Метрики определены в [RFC 6076](https://datatracker.ietf.org/doc/html/rfc6076):

### Жизненный цикл диалога

```
INVITE → 200 OK → Диалог создан
                      │
                      ├──→ BYE → 200 OK → Диалог удален → SCR +1, SPD обновлён
                      │
                      └──→ [таймаут Session-Expires] → Диалог истёк → SCR +1, SPD обновлён
```

Диалоги отслеживаются с Session-Expires (RFC 4028). Если BYE не получен до таймаута, диалог очищается и засчитывается как «завершённый» в SCR.

| Метрика | Раздел RFC 6076 | Описание |
|---------|-----------------|----------|
| SER | §4.6 | Session Establishment Ratio |
| SEER | §4.7 | Session Establishment Effectiveness Ratio |
| ISA | §4.8 | Ineffective Session Attempts |
| SCR | §4.9 | Session Completion Ratio |
| RRD | §4.1 | Registration Request Delay |
| SPD | §4.5 | Session Process Duration |
| TTR | — | Time to First Response |
| PDD | — | Post Dial Delay |
| ASR | — | Answer Seizure Ratio (ITU-T E.411) |
| SDC | — | Session Duration Counter |
| NER | — | Network Effectiveness Ratio (GSMA IR.42) |
| ISS | — | Ineffective Session Severity |
| ORD | — | OPTIONS Response Delay |
| LRD | — | Location Registration Delay |

---

### Session Establishment Ratio (SER)

`sip_exporter_ser{carrier="...",ua_type="..."}`: процент успешно установленных сессий относительно общего числа попыток INVITE.

**Формула (RFC 6076 §4.6):**
```
SER = (INVITE → 200 OK) / (Всего INVITE - INVITE → 3xx) × 100
```

- **Re-INVITE исключены** из числителя и знаменателя — это не новые попытки сессии, они учитываются отдельно в `reinvite_total`
- Ответы 3xx (редиректы) **исключаются из знаменателя** — они не являются ни успехом, ни неудачей, а инструкцией маршрутизации
- Сессия считается установленной только когда инициирующий UA получает `200 OK` на свой INVITE
- Не определено, если не было получено INVITE-запросов
- Не определено, если все INVITE получили 3xx-ответы (знаменатель = 0)

**Важно:** SER — кумулятивная метрика, вычисляемая за всё время работы.

---

### Session Establishment Effectiveness Ratio (SEER)

`sip_exporter_seer{carrier="...",ua_type="..."}`: процент «эффективных» ответов на INVITE относительно общего числа нередиректных попыток INVITE.

**Формула (RFC 6076 §4.7):**
```
SEER = (INVITE → 200, 480, 486, 600, 603) / (Всего INVITE - INVITE → 3xx) × 100
```

- Ответы 3xx (редиректы) **исключаются из знаменателя** — как в SER
- Числитель включает ответы, представляющие ясный исход для конечного пользователя:
  - `200 OK` — сессия установлена
  - `480 Temporarily Unavailable` — пользователь временно недоступен
  - `486 Busy Here` — пользователь занят
  - `600 Busy Everywhere` — пользователь занят везде
  - `603 Decline` — пользователь отклонил вызов
- Ответы типа `400`, `404`, `500`, `503` **не** считаются эффективными — они указывают на инфраструктурные проблемы или проблемы маршрутизации
- Не определено, если не было получено INVITE-запросов
- Не определено, если все INVITE получили 3xx-ответы (знаменатель = 0)

**Важно:** Как и SER, SEER кумулятивна.

**Связь между SER и SEER:** SEER всегда >= SER, поскольку числитель SEER включает все ответы, засчитываемые в SER, плюс дополнительные «эффективные» коды ошибок (480, 486, 600, 603). Разрыв между ними показывает долю вызовов, получивших окончательный пользовательский исход, а не просто установление сессии.

**Примеры значений:**
- `100` — все нередиректные INVITE получили ясный исход (успех или явный отказ)
- `0` — все нередиректные INVITE получили инфраструктурные ошибки
- `undefined` — нет INVITE или все были 3xx-редиректами

---

### Ineffective Session Attempts (ISA)

`sip_exporter_isa{carrier="...",ua_type="..."}`: процент INVITE-запросов, приведших к серверным ошибкам или таймаутам.

**Формула (RFC 6076 §4.8):**
```
ISA % = (INVITE → 408, 500, 503, 504) / Всего INVITE × 100
```

- В отличие от SER/SEER, ответы 3xx **НЕ исключаются из знаменателя** — ISA измеряет надёжность инфраструктуры
- Числитель включает серверные ошибки, указывающие на перегрузку или неисправность системы:
  - `408 Request Timeout` — downstream-сервер не ответил
  - `500 Server Internal Error` — внутренний сбой сервера
  - `503 Service Unavailable` — сервис временно недоступен (перегрузка)
  - `504 Server Time-out` — шлюзный таймаут сервера
- Ответы типа `400`, `401`, `403`, `404` **не** учитываются — они указывают на клиентские проблемы, а не на серверные сбои
- Не определено, если не было получено INVITE-запросов

**Важно:** ISA кумулятивна за всё время работы.

**Связь с SER/SEER:** ISA измеряет здоровье инфраструктуры, тогда как SER/SEER — успех установления сессий. Растущий ISA обычно указывает на перегруженные или выходящие из строя downstream-серверы. В отличие от SER (который исключает 3xx), ISA включает все INVITE в знаменателе.

**Примеры значений:**
- `0` — серверных ошибок или таймаутов не обнаружено
- `5` — 5% INVITE привели к серверным сбоям (порог мониторинга)
- `>15` — серьёзные инфраструктурные проблемы, требующие немедленного внимания

#### Понимание ISA

ISA измеряет здоровье инфраструктуры, а не пользовательский опыт. В отличие от SER/SEER, которые измеряют успех установления сессий, ISA отслеживает серверные сбои, указывающие на системные проблемы.

| Тренд ISA | Что означает | Вероятные причины |
|-----------|--------------|-------------------|
| **ISA растёт** | Инфраструктура деградирует | Перегрузка серверов, потери пакетов в сети, отказ зависимостей (БД, кэш), некорректные балансировщики |
| **ISA падает** | Инфраструктура стабилизируется | Серверы восстанавливаются, ошибки уменьшаются, система возвращается к здоровому состоянию |
| **ISA 0–5%** | Здоровая система | Нормальная работа, действий не требуется |
| **ISA 5–15%** | Зона предупреждения | Разберитесь с возникающими проблемами до их эскалации |
| **ISA >15%** | Критично | Требуется немедленная диагностика — серверы или сеть отказывают |

---

### Session Completion Ratio (SCR)

`sip_exporter_scr{carrier="...",ua_type="..."}`: процент INVITE-сессий, полностью завершённых (установлены и терминированы), относительно общего числа попыток INVITE.

**Формула (RFC 6076 §4.9):**
```
SCR = (Завершённые сессии) / Всего INVITE × 100
```

- В отличие от SER/SEER, ответы 3xx **НЕ исключаются из знаменателя** — SCR измеряет сквозное завершение сессий
- Сессия считается «завершённой», когда:
  1. Получен `200 OK` на `BYE` (нормальное завершение), **ИЛИ**
  2. Диалог истёк по Session-Expires таймауту (RFC 4028)
- Истёкшие диалоги засчитываются как завершённые для предотвращения инфляции SCR от «зависших» сессий
- Session-Expires по умолчанию: 1800 секунд (30 минут), настраивается через SIP-заголовок `Session-Expires`
- Не определено, если не было получено INVITE-запросов

**Важно:** SCR кумулятивен за всё время работы.

**Связь с SER:** SCR всегда <= SER, поскольку только часть установленных сессий полностью завершается. Разрыв указывает на сессии, ещё активные или брошенные без BYE.

**Примеры значений:**
- `100` — все INVITE привели к полностью завершённым сессиям (INVITE→200 OK + BYE→200 OK)
- `50` — половина всех INVITE привела к полному циклу вызова
- `0` — ни одна сессия не была полностью завершена (либо нет ответов, либо нет BYE)

---

### Registration Request Delay (RRD)

`sip_exporter_rrd{carrier="...",ua_type="..."}`: гистограмма задержек в миллисекундах между отправкой REGISTER-запроса и получением ответа 200 OK.

**Формула (RFC 6076 §4.1):**
```
RRD = Время ответа 200 OK - Время REGISTER-запроса
```

- Измеряет round-trip время для SIP-транзакций регистрации
- Измеряются только успешные регистрации (ответы 200 OK)
- Экспонируется как Prometheus Histogram с бакетами: `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` мс
- Используйте `histogram_quantile()` для алертов на основе перцентилей

**Примеры PromQL:**
```promql
# 95-й перцентиль задержки регистрации (все операторы)
histogram_quantile(0.95, sum(rate(sip_exporter_rrd_bucket[5m])) by (le))

# 95-й перцентиль задержки регистрации (конкретный оператор и тип устройств)
histogram_quantile(0.95, sum(rate(sip_exporter_rrd_bucket{carrier="carrier-a",ua_type="yealink"}[5m])) by (le))

# Средняя задержка регистрации
rate(sip_exporter_rrd_sum[5m]) / rate(sip_exporter_rrd_count[5m])
```

**Важно:** RRD измеряет задержку регистрации, а не задержку установления вызова. Используйте SER/SEER для метрик установления вызовов.

**Примеры значений:**
- `< 100 мс` — отличная производительность регистрации (локальная сеть)
- `100–500 мс` — приемлемая производительность (типичная WAN)
- `> 1000 мс` — потенциальные проблемы (перегрузка сети, перегрузка сервера)

**Примечание:** Используйте `histogram_quantile()` или `rate(sum)/rate(count)` для средних — гистограмма даёт полное распределение, а не только среднее.

---

### Session Process Duration (SPD)

`sip_exporter_spd{carrier="...",ua_type="..."}`: гистограмма длительностей завершённых SIP-сессий в секундах.

**Формула (RFC 6076 §4.5):**
```
SPD = Время окончания сессии - Время начала сессии (200 OK на INVITE)
```

- Измеряет время от установления сессии (`200 OK` на `INVITE`) до завершения сессии (`200 OK` на `BYE` или Session-Expires таймаут)
- Сессия начинается при создании диалога после получения `200 OK` на `INVITE`
- Сессия заканчивается, когда:
  1. Получен `200 OK` на `BYE` (нормальное завершение), **ИЛИ**
  2. Диалог истёк по Session-Expires (RFC 4028)
- Экспонируется как Prometheus Histogram с бакетами: `[1, 5, 10, 30, 60, 300, 600, 1800, 3600]` секунд
- Используйте `histogram_quantile()` для алертов на основе перцентилей

**Примеры PromQL:**
```promql
# 99-й перцентиль длительности сессии (все операторы)
histogram_quantile(0.99, sum(rate(sip_exporter_spd_bucket[5m])) by (le))

# 99-й перцентиль длительности сессии (конкретный оператор и тип устройств)
histogram_quantile(0.99, sum(rate(sip_exporter_spd_bucket{carrier="carrier-a",ua_type="yealink"}[5m])) by (le))

# Средняя длительность сессии
rate(sip_exporter_spd_sum[5m]) / rate(sip_exporter_spd_count[5m])
```

**Важно:** SPD измеряет фактическую длительность сессии, а не задержку установления. Используйте SER/SEER для метрик установления.

**Примеры значений:**
- `< 30 с` — короткие вызовы (IVR, голосовая почта)
- `180 с` — типичный голосовой вызов (~3 минуты)
- `> 3600 с` — длительные сессии (конференции, удержание вызова)

**Примечание:** Используйте `histogram_quantile()` или `rate(sum)/rate(count)` для средних — гистограмма даёт полное распределение, а не только среднее.

---

### Time to First Response (TTR)

`sip_exporter_ttr{carrier="...",ua_type="..."}`: гистограмма задержек в миллисекундах между INVITE-запросом и первым предварительным (1xx) ответом.

**Формула:**
```
TTR = Время первого 1xx-ответа - Время INVITE-запроса
```

- Не определён в RFC 6076, но является полезной операционной метрикой для детекции медленных SIP-серверов
- Измеряет время от INVITE до **первого** предварительного ответа (100 Trying, 180 Ringing, 183 Session Progress)
- Измеряется только первый 1xx-ответ — последующие предварительные ответы игнорируются
- Если предварительный ответ не получен (напр., INVITE → 200 OK напрямую), TTR не измеряется
- Экспонируется как Prometheus Histogram с бакетами: `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` мс
- Используйте `histogram_quantile()` для алертов на основе перцентилей

**Примеры PromQL:**
```promql
# 95-й перцентиль времени до первого ответа (все операторы)
histogram_quantile(0.95, sum(rate(sip_exporter_ttr_bucket[5m])) by (le))

# 95-й перцентиль времени до первого ответа (конкретный оператор и тип устройств)
histogram_quantile(0.95, sum(rate(sip_exporter_ttr_bucket{carrier="carrier-a",ua_type="yealink"}[5m])) by (le))

# Среднее время до первого ответа
rate(sip_exporter_ttr_sum[5m]) / rate(sip_exporter_ttr_count[5m])
```

**Примеры значений:**
- `< 50 мс` — отличная отзывчивость сервера
- `100–500 мс` — приемлемо (типично для нагруженных серверов)
- `> 1000 мс` — потенциальные проблемы (перегрузка сервера, сетевая задержка)

---

### Post Dial Delay (PDD)

`sip_exporter_pdd{carrier="...",ua_type="..."}`: гистограмма задержек в миллисекундах между INVITE-запросом и ответом 180 Ringing.

**Формула:**
```
PDD = Время ответа 180 Ringing - Время INVITE-запроса
```

- Измеряет время от INVITE до ответа **180 Ringing** конкретно (не других 1xx-ответов)
- 100 Trying и 183 Session Progress **не** триггерят измерение PDD
- Если 180 Ringing не получен (напр., INVITE → 200 OK напрямую, или только 100 Trying), PDD не измеряется
- Экспонируется как Prometheus Histogram с бакетами: `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` мс
- Используйте `histogram_quantile()` для алертов на основе перцентилей

**Примеры PromQL:**
```promql
# 95-й перцентиль Post Dial Delay (все операторы)
histogram_quantile(0.95, sum(rate(sip_exporter_pdd_bucket[5m])) by (le))

# 95-й перцентиль Post Dial Delay (конкретный оператор и тип устройств)
histogram_quantile(0.95, sum(rate(sip_exporter_pdd_bucket{carrier="carrier-a",ua_type="yealink"}[5m])) by (le))

# Средний Post Dial Delay
rate(sip_exporter_pdd_sum[5m]) / rate(sip_exporter_pdd_count[5m])
```

**Примеры значений:**
- `< 100 мс` — отлично (быстрое установление вызова)
- `100–500 мс` — приемлемо (типично для межоператорских вызовов)
- `> 3000 мс` — потенциальные проблемы (задержки маршрутизации, перегрузка сервера)

---

### Answer Seizure Ratio (ASR)

`sip_exporter_asr{carrier="...",ua_type="..."}`: процент INVITE-запросов, получивших ответ 200 OK.

**Формула (ITU-T E.411):**
```
ASR = (INVITE → 200 OK) / Всего INVITE × 100
```

- Классическая телеком-метрика, определённая в ITU-T E.411; связана с (но отличается от) SER из RFC 6076 §4.6
- В отличие от SER, ответы 3xx **НЕ исключаются из знаменателя**
- Не определено, если не было получено INVITE-запросов

**Связь с SER:** ASR всегда <= SER. При наличии 3xx-ответов SER исключает их из знаменателя, делая SER выше. ASR держит все INVITE в знаменателе.

**Примеры PromQL:**
```promql
# Текущий ASR (по операторам)
sip_exporter_asr

# Сравнить с SER для детекции объёма редиректов
sip_exporter_ser - sip_exporter_asr
```

**Запросы по операторам:**
```promql
# ASR для конкретного оператора и типа устройств
sip_exporter_asr{carrier="carrier-a",ua_type="yealink"}

# Сравнение ASR по всем операторам
sip_exporter_asr
```

**Примеры значений:**
- `100` — все INVITE получили 200 OK
- `50` — половина INVITE получила 200 OK
- `0` — ни один INVITE не получил 200 OK

---

### Session Duration Counter (SDC)

`sip_exporter_sdc_total{carrier="...",ua_type="..."}`: общее количество завершённых SIP-сессий (Prometheus Counter).

- Считает сессии, завершённые через:
  1. `200 OK` на `BYE` (нормальное завершение), **ИЛИ**
  2. Диалог истёк по Session-Expires (RFC 4028)
- Те же события, что в числителе SCR, но экспонируется как Counter для rate-запросов

**Примеры PromQL:**
```promql
# Rate завершения сессий (сессий в секунду, по операторам)
rate(sip_exporter_sdc_total[5m])

# Rate завершения сессий для конкретного оператора и типа устройств
rate(sip_exporter_sdc_total{carrier="carrier-a",ua_type="yealink"}[5m])

# Rate завершения сессий в минуту
rate(sip_exporter_sdc_total[1m]) * 60
```

---

### Network Effectiveness Ratio (NER)

`sip_exporter_ner{carrier="...",ua_type="..."}`: процент INVITE-запросов, **не** приведших к неэффективным (инфраструктурным) ответам.

**Формула (GSMA IR.42):**
```
NER = (Всего INVITE - INVITE → 408, 500, 503, 504) / Всего INVITE × 100
NER = 100 - ISA
```

- Определён в GSMA IR.42 (не RFC 6076), широко используется в сетях мобильных операторов
- Ответы 3xx **НЕ исключаются из знаменателя**
- Измеряет качество сети включая завершение вызовов — более высокий NER означает меньше инфраструктурных сбоев
- Не определено, если не было получено INVITE-запросов

**Связь с ISA:** NER = 100 − ISA. Всегда используйте вместе: ISA для процента сбоев, NER для процента успеха.

**Примеры PromQL:**
```promql
# Текущий NER (по операторам)
sip_exporter_ner

# Проверка NER = 100 - ISA
sip_exporter_ner + sip_exporter_isa

# NER для конкретного оператора и типа устройств
sip_exporter_ner{carrier="carrier-a",ua_type="yealink"}
```

**Примеры значений:**
- `100` — нет инфраструктурных сбоев (все INVITE разрешились без 408/500/503/504)
- `95` — 5% INVITE попали в серверные ошибки или таймауты
- `0` — все INVITE привели к инфраструктурным сбоям

---

### Ineffective Session Severity (ISS)

`sip_exporter_iss_total{carrier="...",ua_type="..."}`: общее количество INVITE-запросов, приведших к неэффективным ответам (Prometheus Counter).

- Считает INVITE-ответы с кодами статуса: `408`, `500`, `503`, `504`
- Те же коды, что в числителе ISA, но экспонируется как абсолютный Counter
- Полезен для алертов на абсолютный объём инфраструктурных сбоев (в отличие от ISA, который — процент)

**Примеры PromQL:**
```promql
# Неэффективные сессии в секунду (по операторам)
rate(sip_exporter_iss_total[5m])

# Всего неэффективных сессий с момента старта
sip_exporter_iss_total

# Алерт: более 20 неэффективных сессий в секунду для любого оператора
rate(sip_exporter_iss_total[5m]) > 20
```

**Примеры значений:**
- `0` — инфраструктурных сбоев не обнаружено
- `rate > 5/с` — повышенный rate ошибок, требуется разбор
- `rate > 20/с` — критично, требуется немедленное внимание

---

### OPTIONS Response Delay (ORD)

`sip_exporter_ord{carrier="...",ua_type="..."}`: гистограмма задержек в миллисекундах между отправкой OPTIONS-запроса и получением любого ответа.

**Формула:**
```
ORD = Время ответа OPTIONS - Время OPTIONS-запроса
```

- Измеряет round-trip время для SIP OPTIONS-pong транзакций
- Измеряется любой ответ (не только 200 OK) — OPTIONS используется для keepalive/health-check
- OPTIONS-запросы отслеживаются по Call-ID с TTL-очисткой (60с)
- Экспонируется как Prometheus Histogram с бакетами: `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` мс

**Примеры PromQL:**
```promql
# 95-й перцентиль задержки ответа OPTIONS (все операторы)
histogram_quantile(0.95, sum(rate(sip_exporter_ord_bucket[5m])) by (le))

# 95-й перцентиль задержки ответа OPTIONS (конкретный оператор и тип устройств)
histogram_quantile(0.95, sum(rate(sip_exporter_ord_bucket{carrier="carrier-a",ua_type="yealink"}[5m])) by (le))

# Средняя задержка ответа OPTIONS
rate(sip_exporter_ord_sum[5m]) / rate(sip_exporter_ord_count[5m])
```

**Примеры значений:**
- `< 50 мс` — отличная отзывчивость SIP-сервера
- `100–500 мс` — приемлемо (типично для WAN или нагруженных серверов)
- `> 1000 мс` — потенциальные проблемы (перегрузка сервера, сетевая задержка)

---

### Location Registration Delay (LRD)

`sip_exporter_lrd{carrier="...",ua_type="..."}`: гистограмма задержек в миллисекундах между отправкой REGISTER-запроса и получением 3xx-редиректа.

**Формула:**
```
LRD = Время ответа REGISTER 3xx - Время REGISTER-запроса
```

- Измеряет задержку для сценариев регистрационного редиректа (напр., SIP-балансировщик перенаправляет на другой регистратор)
- Измеряются только 3xx-ответы на REGISTER (200 OK измеряется в RRD)
- Использует тот же `registerTracker`, что и RRD — REGISTER→3xx триггерит LRD, REGISTER→200 OK триггерит RRD
- Экспонируется как Prometheus Histogram с бакетами: `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` мс

**Примеры PromQL:**
```promql
# 95-й перцентиль задержки LRD (все операторы)
histogram_quantile(0.95, sum(rate(sip_exporter_lrd_bucket[5m])) by (le))

# 95-й перцентиль задержки LRD (конкретный оператор и тип устройств)
histogram_quantile(0.95, sum(rate(sip_exporter_lrd_bucket{carrier="carrier-a",ua_type="yealink"}[5m])) by (le))

# Средняя задержка LRD
rate(sip_exporter_lrd_sum[5m]) / rate(sip_exporter_lrd_count[5m])
```

**Примеры значений:**
- `< 50 мс` — быстрая обработка редиректа
- `100–500 мс` — приемлемо (редирект включает DNS или запрос к БД)
- `> 1000 мс` — потенциальные проблемы (медленный redirect-сервер, задержки DNS)

---

## Метрики качества голоса (RFC 6035)

Метрики качества голоса извлекаются из SIP PUBLISH- и NOTIFY-запросов, несущих тела `Content-Type: application/vq-rtcpxr` per RFC 6035 (RTCP XR SIP Package). Экспортёр разбирает блоки `VQSessionReport: CallTerm` и экспонирует каждое поле метрики как Prometheus Histogram с лейблами.

### Как это работает

**Источник данных:** SIP-эндпоинты (IP-телефоны, АТС, SBC, медиа-шлюзы) генерируют отчёты о качестве голоса после завершения каждого вызова. Эти отчёты содержат RTP-статистику, измеренную во время вызова — потери пакетов, jitter, задержка, MOS-оценки, R-factor. Отчёты отправляются как SIP PUBLISH или NOTIFY с `Content-Type: application/vq-rtcpxr` на эндпоинт-коллектор.

**Путь захвата:**
```
SIP-эндпоинт → PUBLISH/NOTIFY → Сеть → eBPF-захват → SIP-парсер → VQ-парсер → Prometheus histogram
```

Экспортёр **не** генерирует и не вычисляет метрики качества голоса самостоятельно. Он только разбирает и экспортирует значения, сообщённые SIP-эндпоинтами. Точность метрик полностью зависит от реализации RTP-статистики на отчётном эндпоинте.

**PUBLISH vs NOTIFY:**
- **PUBLISH** — эндпоинт отправляет VQ-отчёт на выделенный URI-коллектор (напр., `sip:collector@example.com`). Наиболее распространённый метод в enterprise- и carrier-средах.
- **NOTIFY** — эндпоинт отправляет VQ-отчёт через SIP event package subscription. Используется, когда коллектор подписан на `vq-rtcpxr`-события от эндпоинта.

Оба метода производят идентичные тела VQ-отчётов. Экспортёр обрабатывает их эквивалентно — единственное отличие — счётчик SIP-метода (`sip_exporter_publish_total` vs `sip_exporter_notify_total`).

### Детекция

Экспортёр детектирует VQ-отчёты, когда:
1. Получен SIP-запрос (обычно PUBLISH или NOTIFY, хотя проверка Content-Type не зависит от метода)
2. Заголовок `Content-Type` содержит `application/vq-rtcpxr`
3. Тело начинается с `VQSessionReport`

Если тело не удалось разобрать, экспортёр инкрементирует `sip_exporter_system_error_total` и `sip_exporter_parse_errors_total{type="vq"}`, и пропускает отчёт.

### Разрешение лейблов

Все VQ-метрики включают лейблы `carrier`, `ua_type` и `source_country`, разрешённые из SIP PUBLISH/NOTIFY-пакета:

| Лейбл | Разрешение |
|-------|------------|
| `carrier` | Source IP PUBLISH/NOTIFY-запроса → CIDR-маппинг по `carriers.yaml` |
| `ua_type` | Заголовок `User-Agent` PUBLISH/NOTIFY-запроса → regex-маппинг по `user_agents.yaml` |
| `source_country` | Source IP → GeoIP/carrier.country (тот же приоритет, что у всех метрик) |

В отличие от метрик установления вызова (INVITE/BYE), где carrier разрешается из INVITE и распространяется через трекеры, VQ-метрики используют carrier/ua_type из PUBLISH/NOTIFY-запроса напрямую. Это связано с тем, что VQ-отчёты отправляются **после** завершения вызова и могут приходить от устройства, отличного от инициатора вызова (напр., SBC, генерирующий отчёты для всех проксируемых вызовов).

### Что измеряет каждая метрика

13 VQ-метрик соответствуют полям блока `VQSessionReport` RFC 6035. Каждое поле сообщается SIP-эндпоинтом на основе его RTP-статистики:

| Категория | Метрики | Источник |
|-----------|---------|----------|
| **Потери пакетов** | NLR, JDR, BLD, GLD | Статистика потерь RTP-пакетов (RTCP XR blocks) |
| **Задержка** | RTD, ESD | Измерение round trip time + задержка обработки эндпоинта |
| **Jitter** | IAJ, MAJ | Вариация interarrival времени RTP-таймстампов |
| **MOS-оценки** | MOSLQ, MOSCQ | Оценивается эндпоинтом по ITU-T G.107 (E-Model) |
| **R-factor** | RLQ, RCQ | Выход ITU-T G.107 E-Model (шкала 0–120) |
| **Эхо** | RERL | Echo return loss после эхоподавления |

**Важно:** Не все эндпоинты сообщают все 13 метрик. Экспортёр наблюдает гистограммы только для полей, присутствующих в теле отчёта. Отсутствующие поля молча пропускаются (см. [Частичные отчёты](#частичные-отчёты) ниже).

### Voice Quality Reports Total

`sip_exporter_vq_reports_total{carrier="...",ua_type="..."}`: общее количество VQ-отчётов сессий, успешно обработанных (Prometheus Counter).

**Примеры PromQL:**
```promql
# Rate обработки VQ-отчётов
rate(sip_exporter_vq_reports_total[5m])

# VQ-отчёты по операторам
sip_exporter_vq_reports_total{carrier="carrier-a"}
```

### Network Loss Rate (NLR)

`sip_exporter_vq_nlr_percent{carrier="...",ua_type="..."}`: гистограмма процента потерь сетевых пакетов.

- Измеряет процент RTP-пакетов, потерянных в сети (не восстановленных jitter-буфером)
- Диапазон: 0–100%
- Бакеты: `[0, 0.1, 0.5, 1, 2, 5, 10, 20, 50, 100]`

**Примеры PromQL:**
```promql
# Средний NLR по операторам
rate(sip_exporter_vq_nlr_percent_sum[5m]) / rate(sip_exporter_vq_nlr_percent_count[5m])

# 95-й перцентиль потерь
histogram_quantile(0.95, sum(rate(sip_exporter_vq_nlr_percent_bucket[5m])) by (le))
```

### Jitter Buffer Discard Rate (JDR)

`sip_exporter_vq_jdr_percent{carrier="...",ua_type="..."}`: гистограмма процента отброса jitter-буфером.

- Измеряет процент RTP-пакетов, отброшенных jitter-буфером (слишком поздно или рано)
- Диапазон: 0–100%
- Бакеты: `[0, 0.1, 0.5, 1, 2, 5, 10, 20, 50, 100]`

### Burst Loss Density (BLD)

`sip_exporter_vq_bld_percent{carrier="...",ua_type="..."}`: гистограмма плотности burst-потерь в процентах.

- Измеряет плотность потерь пакетов в burst-периодах
- Диапазон: 0–100%
- Бакеты: `[0, 0.1, 0.5, 1, 2, 5, 10, 20, 50, 100]`

### Gap Loss Density (GLD)

`sip_exporter_vq_gld_percent{carrier="...",ua_type="..."}`: гистограмма плотности gap-потерь в процентах.

- Измеряет плотность потерь пакетов в gap-периодах (не burst)
- Диапазон: 0–100%
- Бакеты: `[0, 0.1, 0.5, 1, 2, 5, 10, 20, 50, 100]`

### Round Trip Delay (RTD)

`sip_exporter_vq_rtd_ms{carrier="...",ua_type="..."}`: гистограмма round trip delay в миллисекундах.

- Измеряет сетевое round trip время для RTP-потока
- Бакеты: `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` мс

**Примеры PromQL:**
```promql
# 95-й перцентиль round trip delay
histogram_quantile(0.95, sum(rate(sip_exporter_vq_rtd_ms_bucket[5m])) by (le))
```

### End System Delay (ESD)

`sip_exporter_vq_esd_ms{carrier="...",ua_type="..."}`: гистограмма задержки эндпоинта в миллисекундах.

- Измеряет полную задержку, добавляемую эндпоинтом (jitter-буфер, кодек и т.д.)
- Бакеты: `[1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000]` мс

### Interarrival Jitter (IAJ)

`sip_exporter_vq_iaj_ms{carrier="...",ua_type="..."}`: гистограмма interarrival jitter в миллисекундах.

- Измеряет статистическую вариацию interarrival времени RTP-пакетов
- Бакеты: `[0.1, 0.5, 1, 5, 10, 20, 50, 100, 200, 500]` мс

### Mean Absolute Jitter (MAJ)

`sip_exporter_vq_maj_ms{carrier="...",ua_type="..."}`: гистограмма среднего абсолютного jitter в миллисекундах.

- Измеряет среднее абсолютное значение jitter
- Бакеты: `[0.1, 0.5, 1, 5, 10, 20, 50, 100, 200, 500]` мс

### MOS Listening Quality (MOSLQ)

`sip_exporter_vq_mos_lq{carrier="...",ua_type="..."}`: гистограмма оценки MOS Listening Quality.

- Оценка MOS для одностороннего качества прослушивания (без учёта эха)
- Диапазон: 1.0–4.9
- Бакеты: `[1.0, 1.5, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5.0]`

**Примеры PromQL:**
```promql
# Средний MOS Listening Quality
rate(sip_exporter_vq_mos_lq_sum[5m]) / rate(sip_exporter_vq_mos_lq_count[5m])

# Процент вызовов с MOS ниже 3.0
sum(rate(sip_exporter_vq_mos_lq_bucket{le="2.5"}[5m]))
  / sum(rate(sip_exporter_vq_mos_lq_count[5m])) * 100
```

**Примеры значений:**
- `4.0–4.9` — отличное качество
- `3.5–4.0` — хорошее качество
- `3.0–3.5` — приемлемое качество
- `1.0–3.0` — плохое качество, требуется разбор

### MOS Conversational Quality (MOSCQ)

`sip_exporter_vq_mos_cq{carrier="...",ua_type="..."}`: гистограмма оценки MOS Conversational Quality.

- Оценка MOS, включающая оба направления и эффекты эха
- Диапазон: 1.0–4.9
- Бакеты: `[1.0, 1.5, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5.0]`

### R-factor Listening Quality (RLQ)

`sip_exporter_vq_rlq{carrier="...",ua_type="..."}`: гистограмма R-factor Listening Quality.

- R-factor для одностороннего качества прослушивания (ITU-T G.107)
- Диапазон: 0–120 (обычно 0–100)
- Бакеты: `[0, 10, 20, 30, 50, 60, 70, 80, 90, 100, 120]`

**Примеры значений:**
- `90–100` — отлично
- `80–90` — хорошо
- `70–80` — приемлемо
- `< 70` — плохо

### R-factor Conversational Quality (RCQ)

`sip_exporter_vq_rcq{carrier="...",ua_type="..."}`: гистограмма R-factor Conversational Quality.

- R-factor, включающий оба направления и эффекты эха
- Диапазон: 0–120
- Бакеты: `[0, 10, 20, 30, 50, 60, 70, 80, 90, 100, 120]`

### Residual Echo Return Loss (RERL)

`sip_exporter_vq_rerl_db{carrier="...",ua_type="..."}`: гистограмма residual echo return loss в дБ.

- Измеряет echo return loss после эхоподавления
- Более высокие значения означают меньше эха
- Бакеты: `[0, 5, 10, 15, 20, 30, 40, 50, 60, 80, 100]` дБ

### Частичные отчёты

Не все VQ-отчёты сессий содержат все 13 метрик. Экспортёр наблюдает гистограммы только для полей, присутствующих в теле отчёта. Отсутствующие поля молча пропускаются. Это означает, что значения `*_count` могут различаться между VQ-метриками.

### Формат VQ-отчёта

Ожидаемый формат тела (RFC 6035):
```
VQSessionReport: CallTerm
CallID: <call-id>
LocalID: <sip-uri>
RemoteID: <sip-uri>
NLR=0.50 JDR=1.20 BLD=0.30 GLD=0.10
RTD=45.5 ESD=20.3 IAJ=5.2 MAJ=3.1
MOSLQ=4.5 MOSCQ=4.2 RLQ=92.0 RCQ=88.0
RERL=55.0
```

Несколько метрик могут быть на одной строке (через пробел) или на отдельных строках. Строки заголовков (содержащие `:`, но не `=`) игнорируются.

---

## Алерты

Репозиторий включает преднастроенные правила алертов и дашборды для работы мониторинга из коробки.

### Что включено

| Компонент | Файл | Описание |
|-----------|------|----------|
| Дашборд Grafana | [`examples/grafana-dashboard.json`](../examples/grafana-dashboard.json) | Полный production-дашборд (переменные, все типы метрик) |
| Руководство по алертингу | [`docs/ALERTING.ru.md`](ALERTING.ru.md) | Полный справочник: правила, конфиги Alertmanager (Slack/PagerDuty/Email), настройка порогов |

### Сводка алертов по категориям

#### Детекция фрода

| Алерт | Метрика | Триггер | Severity |
|-------|---------|---------|----------|
| `SIPRegistrationScan` | `register_scan_total` | rate > 0 (один IP регистрирует много аккаунтов) | critical |
| `SIPInviteBurst` | `invite_burst_total` | rate > 0 (один IP флудит INVITE) | critical |
| `SIPRegistrationCountryChange` | `register_country_change_total` | счётчик > 0 и был 0/отсутствовал 5м назад | warning |
| `SIPSessionCapacityExhaustion` | `sessions_utilization` | > 90% в течение 5м | warning |

#### Здоровье SIP

| Алерт | Метрика | Триггер | Severity |
|-------|---------|---------|----------|
| `SIPExporterDown` | `up` | == 0 в течение 1м | critical |
| `SIPHighServerErrorRate` | `isa` | avg > 50% в течение 1м | critical |
| `SIPSessionEstablishmentCritical` | `ser` | < 20% в течение 2м | critical |
| `SIPSessionEstablishmentLow` | `ser` | < 50% в течение 5м | warning |
| `SIPRegistrationSlow` | `rrd` | p95 > 500мс в течение 5м | warning |
| `SIPPacketDropHigh` | `socket_packets_dropped_total` | > 10/с в течение 2м | warning |

#### Качество голоса (RTP)

| Алерт | Метрика | Триггер | Severity |
|-------|---------|---------|----------|
| `RTPMOSLow` | `rtp_mos_score` | avg < 3.5 в течение 5м | warning |
| `RTPPacketLossHigh` | `rtp_packets_lost_total` / `rtp_packets_total` | > 5% в течение 5м | warning |
| `RTPJitterHigh` | `rtp_jitter_milliseconds` | p95 > 50мс в течение 5м | warning |

### Рекомендации по порогам

| Метрика | Зелёная | Жёлтая | Красная |
|---------|---------|--------|---------|
| SER | ≥ 95% | 80–95% | < 80% |
| SEER | ≥ 95% | 80–95% | < 80% |
| ISA | < 5% | 5–15% | > 15% |
| SCR | ≥ 80% | 60–80% | < 60% |
| RRD p95 | < 100мс | 100–500мс | > 500мс |
| RTP MOS | ≥ 4.0 | 3.5–4.0 | < 3.5 |
| RTP Jitter p95 | < 30мс | 30–50мс | > 50мс |
| RTP Packet Loss | < 2% | 2–5% | > 5% |

Полное руководство по алертингу с правилами Prometheus, конфигами Alertmanager и лучшими практиками: [`docs/ALERTING.ru.md`](ALERTING.ru.md).