# GeoIP Setup Guide — `source_country` label

`source_country` is **optional**. Without a database the label is `"unknown"`
(unless overridden by `carrier.country` in
[`carriers.yaml`](../examples/carriers.yaml)) — the exporter runs normally with
zero extra cardinality.

For label semantics, resolution precedence, and PromQL examples, see
[docs/METRICS.md > Geo-Enrichment Labels](METRICS.md#geo-enrichment-labels).

---

## 1. What you need

| Item | Value |
|------|-------|
| Database file | **`GeoLite2-Country.mmdb`** (~6 MB) |
| Edition | Country **only**. Do **not** use City or ASN — only `iso_code` is read. |

## 2. Connect & verify

Point the exporter at the file with `SIP_EXPORTER_GEOIP_COUNTRY_DB`.

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

## 3. No database? Use `carrier.country`

If you don't want to manage a GeoIP database — or your traffic originates from
**private (RFC 1918) IPs**, where MaxMind has no data — set the `country` field on
your carriers in [`carriers.yaml`](../examples/carriers.yaml). It takes **priority
over GeoIP** and is the recommended approach for enterprise / contact-center
deployments:

```yaml
carriers:
  - name: "telecom-alpha"
    country: "RU"          # → source_country="RU" for all IPs in these CIDRs
    cidrs:
      - "10.1.0.0/16"
```

---

## `destination_country`

The companion `destination_country` label needs **no database** — it is resolved
from the E.164 phone-number prefix embedded in the binary. Set
`SIP_EXPORTER_LOCAL_COUNTRY_CODE` (e.g. `RU`, `US`) only for domestic numbers
lacking an international prefix.
