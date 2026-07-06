# Руководство по настройке GeoIP — лейбл `source_country`

`source_country` **необязателен**. Без базы лейбл равен `"unknown"` (если не
переопределён через `carrier.country` в
[`carriers.yaml`](../examples/carriers.yaml)) — экспортер работает штатно без
дополнительной кардинальности. 

Семантика лейблов, приоритет разрешения и примеры PromQL — в
[docs/METRICS.md > Geo-Enrichment Labels](METRICS.md#geo-enrichment-labels).

---

## 1. Что понадобится

| Пункт | Значение |
|------|-------|
| Файл базы | **`GeoLite2-Country.mmdb`** (~6 МБ) |
| Редакция | Только **Country**. **Не** используйте City или ASN — считывается только `iso_code`. |

## 2. Подключение и проверка

Укажите путь к файлу через `SIP_EXPORTER_GEOIP_COUNTRY_DB`.

### Docker (docker-compose)

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
    volumes:
      - ./GeoLite2-Country.mmdb:/data/GeoLite2-Country.mmdb:ro
```

## 3. Нет базы? Используйте `carrier.country`

Если вы не хотите управлять базой GeoIP — или ваш трафик идёт с **приватных
(RFC 1918) IP**, для которых у MaxMind нет данных, — задайте поле `country` у
операторов в [`carriers.yaml`](../examples/carriers.yaml). Оно имеет **приоритет
над GeoIP** и рекомендуется для enterprise / контакт-центров:

```yaml
carriers:
  - name: "telecom-alpha"
    country: "RU"          # → source_country="RU" для всех IP из этих CIDR
    cidrs:
      - "10.1.0.0/16"
```

---

## `destination_country`

Сопутствующий лейбл `destination_country` **не требует базы** — он определяется
по префиксу телефонного номера E.164, встроенному в бинарник. Укажите
`SIP_EXPORTER_LOCAL_COUNTRY_CODE` (например, `RU`, `US`) только для локальных
номеров без международного префикса.
