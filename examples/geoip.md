# GeoIP Setup Guide — `source_country` label

This guide explains how to obtain and connect the MaxMind GeoLite2-Country database
that powers the **`source_country`** label on SIP and RTP metrics.

`source_country` is **optional**. Without a database, every `source_country` label is
`"unknown"` (unless overridden by `carrier.country` in
[`carriers.yaml`](carriers.yaml)) — the exporter runs normally with zero extra
cardinality.

For the label semantics, resolution precedence, and PromQL examples, see
[docs/METRICS.md > Geo-Enrichment Labels](../docs/METRICS.md#geo-enrichment-labels).

---

## 1. What you need

| Item | Value |
|------|-------|
| Database file | **`GeoLite2-Country.mmdb`** (~6 MB) |
| Edition | Country **only**. Do **not** use City or ASN — only `iso_code` is read. |

## 2. Get the database

Since December 2019, MaxMind requires a **free account and a license key** — the
database can no longer be downloaded anonymously.

1. Sign up at <https://www.maxmind.com/en/geolite2/signup> (free).
2. Sign in and generate a **license key** under *Account → GeoIP2 / GeoLite2 →
   Download Files* (or *Manage License Keys*).
3. Download the database, using one of the methods below.

### Option A — `geoipupdate` (automated, recommended for production)

Install the official `geoipupdate` utility and configure it with your account ID
and license key. It downloads and rotates the database on a schedule.

```ini
# /etc/GeoIP.conf
AccountID  YOUR_ACCOUNT_ID
LicenseKey YOUR_LICENSE_KEY
EditionIDs GeoLite2-Country
```

```sh
geoipupdate                     # one-shot download → GeoLite2-Country.mmdb
```

### Option B — manual download (one-time / testing)

Download the `.tar.gz` archive labeled *GeoLite2 Country* from your MaxMind
account's download page and extract `GeoLite2-Country.mmdb` from it.

```sh
tar xzf GeoLite2-Country_*.tar.gz
mv GeoLite2-Country_*/GeoLite2-Country.mmdb ./GeoLite2-Country.mmdb
```

## 3. Connect it to the exporter

Point the exporter at the file with the `SIP_EXPORTER_GEOIP_COUNTRY_DB` environment
variable.

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

### Docker (run)

```sh
docker run --privileged --network host \
  -e SIP_EXPORTER_INTERFACE=eth0 \
  -e SIP_EXPORTER_GEOIP_COUNTRY_DB=/data/GeoLite2-Country.mmdb \
  -v "$PWD/GeoLite2-Country.mmdb:/data/GeoLite2-Country.mmdb:ro" \
  frzq/sip-exporter:latest
```

### Bare metal / systemd

```sh
SIP_EXPORTER_INTERFACE=eth0 \
SIP_EXPORTER_GEOIP_COUNTRY_DB=/etc/sip-exporter/GeoLite2-Country.mmdb \
  ./sip-exporter
```

## 4. Verify

After startup the exporter logs `GeoIP country DB loaded` with the resolved path.
A successful lookup appears as the label on any SIP/RTP metric:

```
sip_exporter_invite_total{carrier="other",ua_type="other",source_country="DE",destination_country="RU",caller_host="10.0.0.5",called_host="sip.example.com"} 142
sip_exporter_rtp_mos_score_sum{carrier="other",ua_type="other",codec="PCMU",source_country="DE"} 689.0
```

Public test IP `81.2.69.142` resolves to `GB` (MaxMind's documented test record).

## 5. Without a database (carrier fallback)

If you do not want to manage a GeoIP database at all, set the `country` field on
your carriers in [`carriers.yaml`](carriers.yaml). It takes **priority over
GeoIP** and is the recommended approach for private-IP deployments (see below):

```yaml
carriers:
  - name: "telecom-alpha"
    country: "RU"          # → source_country="RU" for all IPs in these CIDRs
    cidrs:
      - "10.1.0.0/16"
```

## 6. Updating the database

MaxMind publishes updates **weekly, on Tuesdays**. To apply a new database file
you can either **hot-reload without downtime** or restart:

```sh
# Hot-reload (no restart, no dropped packets) — recommended
docker compose exec sip-exporter kill -HUP 1
# or, for a bare-metal systemd unit:
kill -HUP "$(pidof sip-exporter)"

# Full restart (also works)
docker compose restart sip-exporter
systemctl restart sip-exporter
```

On `SIGHUP` the exporter reopens the database file and swaps it in under an
internal lock, so concurrent lookups stay consistent. If the new file is missing
or invalid, the reload is logged and skipped — the previous database stays in
use.

A minimal weekly cron example:

```cron
# /etc/cron.d/geoipupdate — Tuesdays 02:30 download, 02:31 hot-reload
30 2 * * 2  root  geoipupdate
31 2 * * 2  root  kill -HUP "$(pidof sip-exporter)"
```

## 7. Private-IP caveat (enterprise / contact centers)

MaxMind has **no data for RFC 1918 private ranges** (`10.0.0.0/8`,
`172.16.0.0/12`, `192.168.0.0/16`). If your traffic originates from private IPs,
the GeoIP database is useless for those flows — `source_country` will be
`"unknown"` unless you set `carrier.country` in [`carriers.yaml`](carriers.yaml).
For enterprise/contact-center deployments, `carrier.country` is typically the
only source of geographic context.

## 8. `destination_country` needs no database

The companion `destination_country` label is resolved from the **E.164 phone-number
prefix** (embedded in the binary from Google libphonenumber, Apache 2.0). **Nothing
to download.** Set `SIP_EXPORTER_LOCAL_COUNTRY_CODE` only for domestic numbers that
lack an international prefix (e.g. `RU`, `US`).

---

## Licensing

GeoLite2 is free for internal use with attribution. Redistribution or bundling of
the database requires a [MaxMind Commercial License](https://www.maxmind.com/en/geolite2/eula).
That is why the database is **never bundled** with the exporter — each user
downloads it separately under their own account.
