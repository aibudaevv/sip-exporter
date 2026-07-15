# SIP Exporter — Руководство по алертингу

Это руководство содержит примеры преднастроенных алертов для мониторинга SIP-инфраструктуры с помощью SIP Exporter.

## Быстрый старт

### Ручная настройка

1. Скопируйте правила алертов Prometheus в конфигурацию Prometheus
2. Импортируйте JSON дашборда Grafana
3. Настройте получателя Alertmanager (Slack/PagerDuty/Email)
4. Скорректируйте пороги под ваши паттерны трафика

## Правила алертов Prometheus

### Critical-алерты

```yaml
groups:
  - name: sip_exporter_critical
    interval: 30s
    rules:
      - alert: SIPExporterDown
        expr: up{job="sip-exporter"} == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "SIP Exporter недоступен"
          description: "Инстанс SIP Exporter {{ $labels.instance }} недоступен более 1 минуты."

      - alert: SIPHighServerErrorRate
        expr: sip_exporter_isa > 50
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Высокий rate серверных ошибок SIP"
          description: "ISA составляет {{ $value | printf \"%.2f\" }}%. Более половины INVITE завершаются серверными ошибками (408/500/503/504). Возможные причины: перегрузка сервера, недоступность downstream, некорректная настройка или DDoS."
          runbook_url: "https://wiki.example.com/runbooks/sip-high-server-error-rate"

      - alert: SIPSessionEstablishmentCritical
        expr: sip_exporter_ser < 20
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "SER критически низкий"
          description: "Session Establishment Ratio составляет {{ $value | printf \"%.1f\" }}%. Большинство вызовов не устанавливается."
          runbook_url: "https://wiki.example.com/runbooks/sip-ser-low"
```

### Warning-алерты

```yaml
      - alert: SIPAuthFailuresHigh
        expr: rate(sip_exporter_401_total[5m]) > 10
        for: 3m
        labels:
          severity: warning
        annotations:
          summary: "Высокий rate ошибок аутентификации"
          description: "Rate 401 Unauthorized — {{ $value | printf \"%.2f\" }}/с. Возможна brute-force атака или неверная настройка клиентов."

      - alert: SIPRegistrationSlow
        expr: histogram_quantile(0.95, sum(rate(sip_exporter_rrd_bucket[5m])) by (le)) > 500
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Медленная регистрация SIP"
          description: "95-й перцентиль задержки регистрации — {{ $value | printf \"%.0f\" }}мс. Проблемы производительности сети или регистратора."

      - alert: SIPSessionEstablishmentLow
        expr: sip_exporter_ser < 50 and sip_exporter_ser >= 20
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "SER ниже порога"
          description: "Session Establishment Ratio составляет {{ $value | printf \"%.1f\" }}%. Качество вызовов может быть снижено."

      - alert: SIPForbiddenHigh
        expr: rate(sip_exporter_403_total[5m]) > 5
        for: 3m
        labels:
          severity: warning
        annotations:
          summary: "Высокий rate ответов 403 Forbidden"
          description: "Rate 403 Forbidden — {{ $value | printf \"%.2f\" }}/с. Проверьте права пользователей и конфигурацию ACL."

      - alert: SIPServerErrorHigh
        expr: rate(sip_exporter_500_total[5m]) > 5
        for: 3m
        labels:
          severity: warning
        annotations:
          summary: "Высокий rate серверных ошибок"
          description: "Rate 500 Server Error — {{ $value | printf \"%.2f\" }}/с. SIP-сервер может быть перегружен или некорректно настроен."

      - alert: SIPStaleDialogsHigh
        expr: |
          (
            sum by (carrier) (sip_exporter_sessions)
            / on (carrier)
              clamp_min(sum by (carrier) (increase(sip_exporter_invite_200_total[30m])), 1)
          ) * 100 > 50
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Высокая доля зависших SIP-диалогов"
          description: "Более половины сессий, установленных за последние 30 минут, всё ещё активны на операторе {{ $labels.carrier }}. Возможны проблемы с таймаутом Session-Expires или отсутствуют сообщения BYE."
      ```

### Алерты здоровья регистраций

```yaml
      - alert: SIPRegistrationSuccessLow
        expr: sip_exporter_register_success_ratio < 80
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Низкий success ratio регистраций"
          description: "REGISTER success ratio составляет {{ $value | printf \"%.1f\" }}% (ниже 80%). Клиенты не могут зарегистрироваться — проверьте здоровье регистратора, учётные данные или ACL. Примечание: 401/407 digest-auth исключаются из знаменателя."

      - alert: SIPRegistrationBruteForce
        expr: sum by (carrier, ua_type, source_country) (rate(sip_exporter_register_failure_total{code="401"}[5m])) > 10
        for: 3m
        labels:
          severity: warning
        annotations:
          summary: "Возможный brute-force регистраций SIP"
          description: "Rate REGISTER 401 Unauthorized — {{ $value | printf \"%.2f\" }}/с. Поток неудачных аутентификаций — рассмотрите fail2ban или rate-limiting. (Только для REGISTER; отличается от generic-алерта 401.)"

      - alert: SIPRegistrationDrop
        expr: sum(sip_exporter_active_registrations) < 5
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Количество активных регистраций критически мало"
          description: "Всего {{ $value | printf \"%.0f\" }} активных регистраций. Массовая дерегистрация может указывать на сбой регистратора или сетевую изоляцию."
```

> **401 vs `register_failure_total{code="401"}`:** Метрика `sip_exporter_401_total` считает 401 по **всем** методам (auth-челленджи INVITE + REGISTER). Метрика `sip_exporter_register_failure_total{code="401"}` — **только REGISTER**, что даёт более чистый сигнал brute-force. Алерт success-ratio использует `register_success_ratio`, который исключает 401/407 из знаменателя.

### Алерты детекции фрода

Эти алерты детектируют подозрительные SIP-паттерны: перехват аккаунта, перечисление регистраций и флуд INVITE. Полная конфигурация описана в `docs/fraud-detection.ru.md`.

```yaml
      - alert: SIPRegistrationCountryChange
        expr: sip_exporter_register_country_change_total > 0 unless on (carrier, source_country) (sip_exporter_register_country_change_total offset 5m > 0)
        for: 0m
        labels:
          severity: warning
        annotations:
          summary: "Обнаружена смена страны регистрации"
          description: "Пользователь перерегистрировался из другой страны на {{ $labels.carrier }}. Возможный перехват аккаунта."

      - alert: SIPRegistrationScan
        expr: rate(sip_exporter_register_scan_total[5m]) > 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Обнаружена атака сканирования регистраций"
          description: "Один IP регистрирует множество разных аккаунтов на {{ $labels.carrier }} из {{ $labels.source_country }}. Возможный credential stuffing или перечисление."

      - alert: SIPInviteBurst
        expr: rate(sip_exporter_invite_burst_total[5m]) > 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Обнаружен всплеск INVITE"
          description: "Один IP отправляет аномально высокий rate INVITE на {{ $labels.carrier }} из {{ $labels.source_country }}. Возможный toll fraud, DDoS или traffic pump."

      - alert: SIPSessionCapacityExhaustion
        expr: sip_exporter_sessions_utilization > 90
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Исчерпание ёмкости сессий"
          description: "Оператор {{ $labels.carrier }} на {{ $value | printf \"%.0f\" }}% от настроенного лимита сессий. Запланируйте расширение ёмкости или разберите всплеск трафика."
```

### Алерты качества голоса (RFC 6035)

Эти алерты мониторят метрики качества голоса, извлечённые из SIP PUBLISH/NOTIFY с `Content-Type: application/vq-rtcpxr` (RFC 6035).

```yaml
      - alert: SIPVoiceQualityMOSLow
        expr: |
          rate(sip_exporter_vq_mos_lq_sum[5m]) / rate(sip_exporter_vq_mos_lq_count[5m]) < 3.5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Низкое качество голоса (MOS)"
          description: "Средний MOS Listening Quality — {{ $value | printf \"%.1f\" }}. Качество голоса снижено — проверьте сеть, кодек или проблемы эндпоинтов."

      - alert: SIPVoiceQualityMOSCritical
        expr: |
          rate(sip_exporter_vq_mos_lq_sum[5m]) / rate(sip_exporter_vq_mos_lq_count[5m]) < 3.0
        for: 3m
        labels:
          severity: critical
        annotations:
          summary: "Критическое качество голоса (MOS ниже 3.0)"
          description: "Средний MOS Listening Quality — {{ $value | printf \"%.1f\" }}. Пользователи испытывают плохое качество звонков. Немедленно проверьте потери пакетов, jitter или несоответствие кодеков."

      - alert: SIPVoiceQualityPacketLossHigh
        expr: |
          rate(sip_exporter_vq_nlr_percent_sum[5m]) / rate(sip_exporter_vq_nlr_percent_count[5m]) > 2
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Высокий уровень потерь сетевых пакетов"
          description: "Средний rate потерь сетевых пакетов — {{ $value | printf \"%.1f\" }}%. Потери выше 2% снижают качество голоса. Проверьте перегрузку сети и конфигурацию QoS."

      - alert: SIPVoiceQualityPacketLossCritical
        expr: |
          rate(sip_exporter_vq_nlr_percent_sum[5m]) / rate(sip_exporter_vq_nlr_percent_count[5m]) > 5
        for: 3m
        labels:
          severity: critical
        annotations:
          summary: "Критический уровень потерь сетевых пакетов"
          description: "Средний rate потерь сетевых пакетов — {{ $value | printf \"%.1f\" }}%. Критические потери пакетов приводят к неприемлемому качеству голоса. Требуется немедленное расследование сети."

      - alert: SIPVoiceQualityJitterHigh
        expr: |
          histogram_quantile(0.95, sum(rate(sip_exporter_vq_iaj_ms_bucket[5m])) by (le)) > 50
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Высокий interarrival jitter"
          description: "95-й перцентиль interarrival jitter — {{ $value | printf \"%.1f\" }}мс. Высокий jitter вызывает артефакты аудио. Проверьте очереди сети и конфигурацию jitter buffer."
```

### Алерты RTP-медиа

Эти алерты мониторят качество RTP-потоков в реальном времени (jitter, потери пакетов, MOS), измеряемое пассивно из заголовков RTP (RFC 3550). Метрики размечены лейблами `carrier`, `ua_type`, `codec` и `source_country`.

```yaml
      - alert: RTPPacketLossHigh
        expr: |
          sum by (carrier) (rate(sip_exporter_rtp_packets_lost_total[5m]))
          / sum by (carrier) (rate(sip_exporter_rtp_packets_total[5m])) * 100 > 5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Потери RTP-пакетов выше 5%"
          description: "Потери RTP-пакетов для оператора {{ $labels.carrier }} — {{ $value | printf \"%.1f\" }}%. Перегрузка сети или неверная настройка QoS могут снижать качество голоса."

      - alert: RTPPacketLossCritical
        expr: |
          sum by (carrier) (rate(sip_exporter_rtp_packets_lost_total[5m]))
          / sum by (carrier) (rate(sip_exporter_rtp_packets_total[5m])) * 100 > 10
        for: 3m
        labels:
          severity: critical
        annotations:
          summary: "Критические потери RTP-пакетов выше 10%"
          description: "Потери RTP-пакетов для оператора {{ $labels.carrier }} — {{ $value | printf \"%.1f\" }}%. Серьёзная деградация медиа — пользователи слышат прерывистое аудио или испытывают обрывы звонков."

      - alert: RTPMOSLow
        expr: |
          sum by (carrier) (rate(sip_exporter_rtp_mos_score_sum[5m]))
          / sum by (carrier) (rate(sip_exporter_rtp_mos_score_count[5m])) < 3.5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Низкий RTP MOS"
          description: "Средний RTP MOS для оператора {{ $labels.carrier }} — {{ $value | printf \"%.1f\" }}. Оценка качества E-model ниже 3.5 указывает на заметную деградацию."

      - alert: RTPJitterHigh
        expr: |
          histogram_quantile(0.95, sum by (le, carrier) (rate(sip_exporter_rtp_jitter_milliseconds_bucket[5m]))) > 50
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Высокий RTP jitter"
          description: "95-й перцентиль RTP jitter для оператора {{ $labels.carrier }} — {{ $value | printf \"%.1f\" }}мс. Jitter выше 50мс вызывает артефакты аудио и переполнение jitter buffer."
```

### Info-алерты

```yaml
      - alert: SIPRedirectIncrease
        expr: rate(sip_exporter_302_total[10m]) > 2 and increase(sip_exporter_302_total[1h]) > 100
        for: 10m
        labels:
          severity: info
        annotations:
          summary: "Увеличение redirect-ответов"
          description: "Rate 302 Moved Temporarily вырос. Возможна миграция пользователей на новые эндпоинты."

      - alert: SIPSessionCompletionLow
        expr: sip_exporter_scr < 30 and sip_exporter_scr > 0
        for: 10m
        labels:
          severity: info
        annotations:
          summary: "Низкий session completion ratio"
          description: "SCR составляет {{ $value | printf \"%.1f\" }}%. Множество сессий не завершаются нормально (INVITE без BYE)."

      - alert: SIPSessionDurationTooShort
        expr: histogram_quantile(0.95, sum(rate(sip_exporter_spd_bucket[5m])) by (le)) > 0 and histogram_quantile(0.95, sum(rate(sip_exporter_spd_bucket[5m])) by (le)) < 5
        for: 10m
        labels:
          severity: info
        annotations:
          summary: "Очень короткая длительность звонков"
          description: "95-й перцентиль длительности сессии — {{ $value | printf \"%.1f\" }}с. Звонки завершаются очень быстро — возможны проблемы с медиа или неверная настройка диалпланов."
```

## Дашборд Grafana

### Инструкция по импорту

1. Откройте Grafana → Dashboards → Import
2. Загрузите [`examples/grafana-dashboard.json`](../examples/grafana-dashboard.json) или вставьте его содержимое
3. Выберите ваш datasource Prometheus или VictoriaMetrics
4. Нажмите "Import"

Дашборд включает переменные `$carrier`, `$source_country` и `$destination_country` для фильтрации по оператору и географии, с панелями:
- **Overview** — активные сессии, rate пакетов, rate INVITE, завершённые сессии
- **RFC 6076 Ratios** — гейги SER, SEER, ISA, SCR, ASR, NER + график трендов
- **Latency Histograms** — RRD, TTR, SPD, ORD, LRD (p50/p95/p99 по операторам)
- **Voice Quality (RFC 6035)** — MOS Listening/Conversational Quality, потери пакетов (NLR), jitter (IAJ), round trip delay (RTD), R-factor (RLQ/RCQ)
- **RTP Media Analysis** — активные RTP-потоки, rate пакетов, потери, MOS (E-model), jitter p95 — всё по кодекам
- **SIP Traffic** — разбивка rate запросов/ответов по методам и кодам статусов
- **Registrations** — активные регистрации, success ratio, ошибки по кодам, фрод-сигналы
- **System Health** — rate ошибок, ISS rate
- **Geographic Distribution** — топ исходных/целевых стран по rate INVITE (GeoIP)

Файл дашборда: [`examples/grafana-dashboard.json`](../examples/grafana-dashboard.json)

## Примеры Alertmanager

### Интеграция со Slack

```yaml
global:
  resolve_timeout: 5m

route:
  group_by: ['alertname', 'severity']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 4h
  receiver: 'sip-alerts'
  routes:
    - match:
        severity: critical
      receiver: 'sip-critical'

receivers:
  - name: 'sip-alerts'
    slack_configs:
      - channel: '#sip-monitoring'
        send_resolved: true
        title: '{{ .GroupLabels.alertname }}'
        text: |-
          {{ range .Alerts }}
          *Alert:* {{ .Annotations.summary }}
          *Description:* {{ .Annotations.description }}
          *Severity:* {{ .Labels.severity }}
          {{ end }}
        color: '{{ if eq .Status "firing" }}{{ if eq .CommonLabels.severity "critical" }}danger{{ else }}warning{{ end }}{{ else }}good{{ end }}'

  - name: 'sip-critical'
    slack_configs:
      - channel: '#sip-critical'
        send_resolved: true
        title: '🚨 {{ .GroupLabels.alertname }}'
        text: '{{ range .Alerts }}{{ .Annotations.description }}{{ end }}'
        color: 'danger'
```

### Интеграция с PagerDuty

```yaml
receivers:
  - name: 'sip-pagerduty'
    pagerduty_configs:
      - service_key: '<your-pagerduty-integration-key>'
        severity: '{{ .CommonLabels.severity }}'
        description: '{{ .GroupLabels.alertname }}: {{ range .Alerts }}{{ .Annotations.summary }}{{ end }}'
        details:
          firing: '{{ template "pagerduty.default.instances" .Alerts.Firing }}'
          resolved: '{{ template "pagerduty.default.instances" .Alerts.Resolved }}'
          num_firing: '{{ .Alerts.Firing | len }}'
          num_resolved: '{{ .Alerts.Resolved | len }}'
```

### Интеграция с Email

```yaml
global:
  smtp_smarthost: 'smtp.example.com:587'
  smtp_from: 'alertmanager@example.com'
  smtp_auth_username: 'alertmanager@example.com'
  smtp_auth_password: '<password>'

receivers:
  - name: 'sip-email'
    email_configs:
      - to: 'sip-team@example.com'
        send_resolved: true
        headers:
          Subject: '[SIP Alert] {{ .GroupLabels.alertname }}'
        html: |
          <h2>{{ .GroupLabels.alertname }}</h2>
          {{ range .Alerts }}
          <p><strong>Summary:</strong> {{ .Annotations.summary }}</p>
          <p><strong>Description:</strong> {{ .Annotations.description }}</p>
          <p><strong>Severity:</strong> {{ .Labels.severity }}</p>
          <hr>
          {{ end }}
```

## Лучшие практики

### Интервал скрейпинга

- **Рекомендуется**: 10-15 секунд для production
- **Минимум**: 5 секунд (может увеличить нагрузку)
- **Конфигурация**:
  ```yaml
  scrape_configs:
    - job_name: 'sip-exporter'
      scrape_interval: 10s
      static_configs:
        - targets: ['localhost:2112']
  ```

### Хранение данных

- **Локальный Prometheus**: обычно 15-30 дней
- **Долгосрочное хранение**: Thanos, Cortex или VictoriaMetrics
- **Конфигурация**:
  ```bash
  prometheus --storage.tsdb.retention.time=15d
  ```

### Заглушение алертов

Используйте silence во время сервисных окон:

```bash
amtool silence add alertname=SIPHighServerErrorRate duration=2h comment="Плановое обслуживание"
```

### Настройка порогов

1. **Сначала baseline**: мониторьте метрики 1-2 недели перед настройкой порогов
2. **Паттерны трафика**: учитывайте часы пик vs. часы простоя
3. **Постепенная настройка**: начинайте с широких порогов, сужайте со временем
4. **Документация**: документируйте, почему выбран каждый порог

### Интеграция с Runbook

Привязывайте runbook к алертам через аннотацию `runbook_url`:

```yaml
annotations:
  runbook_url: "https://wiki.example.com/runbooks/{{ .GroupLabels.alertname }}"
```

### Несколько инстансов

Для высокой доступности запускайте несколько инстансов экспортёра:

```yaml
scrape_configs:
  - job_name: 'sip-exporter'
    static_configs:
      - targets:
          - 'sip-exporter-1:2112'
          - 'sip-exporter-2:2112'
```

### Кардинальность метрик

SIP Exporter экспортирует ~65 метрик с лейблами `carrier`, `ua_type` и `source_country`. RTP-метрики также имеют лейбл `codec` (обычно 3-8 кодеков). Кардинальность равна числу настроенных операторов × типов UA × исходных стран × (для RTP) активных кодеков. Без конфигурации операторов и без GeoIP `source_country="unknown"` (кардинальность = 1); включение GeoIP или настройка `carrier.country` увеличивает кардинальность на число наблюдаемых исходных стран.

### Организация дашбордов

Организуйте дашборды по разделам:
- **Overview**: высокоуровневое здоровье (SER, SEER, ISA, SCR)
- **Traffic**: rate запросов/ответов
- **Errors**: разбивка по кодам ошибок
- **Performance**: RRD, метрики задержки
- **Voice Quality**: MOS, потери пакетов, jitter, R-factor (RFC 6035)
- **RTP Media**: активные потоки, rate пакетов, потери, MOS, jitter по кодекам
