# Manual Test Cases — Carrier Support

## Prerequisites

```bash
# Build and run exporter with carrier config
make docker_build
docker run --rm --privileged --network host \
  -v $(pwd)/examples/carriers.yaml:/etc/sip-exporter/carriers.yaml:ro \
  sip-exporter:latest
```

Environment:

```
SIP_EXPORTER_INTERFACE=eth0
SIP_EXPORTER_SIP_PORT=5060
SIP_EXPORTER_HTTP_PORT=2112
SIP_EXPORTER_CARRIERS_CONFIG=/etc/sip-exporter/carriers.yaml
```

Test tools: `sipp`, `curl`, `tcpdump` (or `ngrep`).

---

## TC-01: All metrics use carrier="other" without config

**Purpose:** verify backward compatibility when `SIP_EXPORTER_CARRIERS_CONFIG` is not set.

**Steps:**
1. Start exporter WITHOUT `SIP_EXPORTER_CARRIERS_CONFIG`
2. Send INVITE → 200 OK → BYE flow from any IP
3. `curl -s localhost:2112/metrics | grep invite_total`

**Expected:**
```
sip_exporter_invite_total{carrier="other"} 1
```
All SIP metrics have `carrier="other"`.

---

## TC-02: Single carrier — INVITE flow

**Purpose:** verify carrier resolution from INVITE source IP.

**Config:**
```yaml
carriers:
  - name: "test-carrier"
    cidrs:
      - "192.168.1.0/24"
```

**Steps:**
1. Start exporter with config above
2. Send INVITE from 192.168.1.100, receive 200 OK from 10.0.0.1
3. `curl -s localhost:2112/metrics | grep 'carrier="test-carrier"'`

**Expected:**
```
sip_exporter_invite_total{carrier="test-carrier"} 1
sip_exporter_200_total{carrier="test-carrier"} 1
sip_exporter_invite200_ok_total{carrier="test-carrier"} 1
```

---

## TC-03: Direction mismatch — carrier from request, not response

**Purpose:** verify carrier is resolved from INVITE sender IP, not from 200 OK responder IP.

**Config:**
```yaml
carriers:
  - name: "carrier-A"
    cidrs:
      - "10.1.0.0/16"
  - name: "carrier-B"
    cidrs:
      - "10.2.0.0/16"
```

**Steps:**
1. INVITE from 10.1.0.5 (carrier-A) → 200 OK from 10.2.0.5 (carrier-B)
2. `curl -s localhost:2112/metrics | grep invite200_ok_total`

**Expected:**
```
sip_exporter_invite200_ok_total{carrier="carrier-A"} 1
sip_exporter_invite200_ok_total{carrier="carrier-B"} 0
```
Counter increments for carrier-A (INVITE sender), NOT carrier-B (200 OK sender).

---

## TC-04: SER per carrier

**Purpose:** verify SER is computed per-carrier.

**Config:**
```yaml
carriers:
  - name: "carrier-A"
    cidrs:
      - "10.1.0.0/16"
  - name: "carrier-B"
    cidrs:
      - "10.2.0.0/16"
```

**Steps:**
1. From 10.1.0.5: send 10 INVITE → all get 200 OK
2. From 10.2.0.5: send 10 INVITE → all get 486 Busy
3. `curl -s localhost:2112/metrics | grep sip_exporter_ser`

**Expected:**
```
sip_exporter_ser{carrier="carrier-A"} 100
sip_exporter_ser{carrier="carrier-B"} 0
```

---

## TC-05: SCR per carrier with session completion

**Purpose:** verify SCR accounts for BYE→200 OK per carrier.

**Config:** same as TC-04.

**Steps:**
1. From 10.1.0.5: send 10 INVITE → 200 OK → BYE → 200 OK (all completed)
2. From 10.2.0.5: send 10 INVITE → 200 OK (no BYE — sessions expire)
3. Wait for Session-Expires timeout
4. `curl -s localhost:2112/metrics | grep -E 'scr|sessions'`

**Expected:**
- `sip_exporter_scr{carrier="carrier-A"}` → high (completed via BYE)
- `sip_exporter_scr{carrier="carrier-B"}` → increases after expiry
- `sip_exporter_sessions{carrier="carrier-A"}` → 0 (completed)
- `sip_exporter_sessions{carrier="carrier-B"}` → 0 after expiry

---

## TC-06: Unknown IP → carrier="other"

**Purpose:** verify fallback to "other" for unmatched IPs.

**Config:**
```yaml
carriers:
  - name: "known-carrier"
    cidrs:
      - "10.1.0.0/16"
```

**Steps:**
1. Send INVITE from 172.16.0.5 (no matching CIDR)
2. `curl -s localhost:2112/metrics | grep invite_total`

**Expected:**
```
sip_exporter_invite_total{carrier="other"} 1
sip_exporter_invite_total{carrier="known-carrier"} 0
```

---

## TC-07: Overlapping CIDRs — first match wins

**Purpose:** verify carrier resolution order.

**Config:**
```yaml
carriers:
  - name: "specific"
    cidrs:
      - "10.1.1.0/24"
  - name: "broad"
    cidrs:
      - "10.1.0.0/16"
```

**Steps:**
1. Send INVITE from 10.1.1.50 (matches both)
2. `curl -s localhost:2112/metrics | grep invite_total`

**Expected:**
```
sip_exporter_invite_total{carrier="specific"} 1
sip_exporter_invite_total{carrier="broad"} 0
```
First matching CIDR wins.

---

## TC-08: RRD per carrier

**Purpose:** verify REGISTER delay attributed to correct carrier.

**Config:**
```yaml
carriers:
  - name: "reg-carrier"
    cidrs:
      - "192.168.1.0/24"
```

**Steps:**
1. Send REGISTER from 192.168.1.10
2. Server responds 200 OK after ~50ms delay
3. `curl -s localhost:2112/metrics | grep sip_exporter_rrd`

**Expected:**
```
sip_exporter_rrd_count{carrier="reg-carrier"} 1
sip_exporter_rrd_sum{carrier="reg-carrier"} ~0.05
sip_exporter_rrd_count{carrier="other"} 0
```

---

## TC-09: TTR per carrier

**Purpose:** verify TTR uses carrier from inviteTracker.

**Config:**
```yaml
carriers:
  - name: "carrier-A"
    cidrs:
      - "10.1.0.0/16"
```

**Steps:**
1. INVITE from 10.1.0.5 → 100 Trying (from different IP) → 200 OK
2. `curl -s localhost:2112/metrics | grep sip_exporter_ttr`

**Expected:**
```
sip_exporter_ttr_count{carrier="carrier-A"} 1
```
TTR attributed to carrier-A (INVITE sender), not to 100 Trying sender.

---

## TC-10: SPD per carrier after BYE

**Purpose:** verify session duration tracked per carrier through dialog.

**Steps:**
1. INVITE from carrier-A IP → 200 OK → wait 5s → BYE → 200 OK
2. `curl -s localhost:2112/metrics | grep sip_exporter_spd`

**Expected:**
```
sip_exporter_spd_count{carrier="carrier-A"} 1
sip_exporter_spd_sum{carrier="carrier-A"} ~5.0
```

---

## TC-11: SPD per carrier after Session-Expires

**Purpose:** verify expired dialogs emit SPD with correct carrier.

**Steps:**
1. INVITE from carrier-A IP → 200 OK (with Session-Expires: 5)
2. Wait 6 seconds for expiry
3. `curl -s localhost:2112/metrics | grep -E 'spd|sdc'`

**Expected:**
```
sip_exporter_spd_count{carrier="carrier-A"} 1
sip_exporter_sdc_total{carrier="carrier-A"} 1
```

---

## TC-12: ORD per carrier

**Purpose:** verify OPTIONS delay per carrier.

**Steps:**
1. Send OPTIONS from carrier-A IP
2. Server responds 200 OK
3. `curl -s localhost:2112/metrics | grep sip_exporter_ord`

**Expected:**
```
sip_exporter_ord_count{carrier="carrier-A"} 1
```

---

## TC-13: LRD per carrier

**Purpose:** verify REGISTER redirect delay per carrier.

**Steps:**
1. Send REGISTER from carrier-A IP
2. Server responds 302 Redirect
3. `curl -s localhost:2112/metrics | grep sip_exporter_lrd`

**Expected:**
```
sip_exporter_lrd_count{carrier="carrier-A"} 1
```

---

## TC-14: Mixed traffic — multiple carriers simultaneously

**Purpose:** verify isolation between carriers under concurrent traffic.

**Config:**
```yaml
carriers:
  - name: "carrier-A"
    cidrs:
      - "10.1.0.0/16"
  - name: "carrier-B"
    cidrs:
      - "10.2.0.0/16"
```

**Steps:**
1. From 10.1.0.5: 50 INVITE → all 200 OK (SER=100%)
2. From 10.2.0.5: 50 INVITE → all 500 Error (SER=0%)
3. `curl -s localhost:2112/metrics | grep sip_exporter_ser`

**Expected:**
```
sip_exporter_ser{carrier="carrier-A"} 100
sip_exporter_ser{carrier="carrier-B"} 0
sip_exporter_500_total{carrier="carrier-A"} 0
sip_exporter_500_total{carrier="carrier-B"} 50
```

---

## TC-15: Grafana dashboard — carrier filter

**Purpose:** verify Grafana dashboard variable works.

**Prerequisites:** Grafana with Prometheus datasource scraping exporter.

**Steps:**
1. Open dashboard → verify `$carrier` dropdown lists all carriers + "other"
2. Select single carrier → verify all panels filter by that carrier
3. Select "All" → verify all carriers shown
4. Verify SER/SEER/ISA/SCR panels show per-carrier values

**Expected:**
- Dropdown shows: `carrier-A`, `carrier-B`, `other`
- Each panel legend shows selected carriers
- SER panel: one line per carrier

---

## TC-16: Invalid carriers.yaml — startup failure

**Purpose:** verify exporter fails with clear error on bad config.

**Steps:**
1. Create invalid config:
```yaml
carriers:
  - name: ""
    cidrs:
      - "not-a-cidr"
```
2. Start exporter with `SIP_EXPORTER_CARRIERS_CONFIG` pointing to it

**Expected:** exporter exits with error containing "carrier name is empty" or "invalid CIDR".

---

## TC-17: Config with overlapping CIDRs across carriers

**Purpose:** verify no crash and deterministic resolution with overlapping ranges.

**Config:**
```yaml
carriers:
  - name: "first"
    cidrs:
      - "10.0.0.0/8"
  - name: "second"
    cidrs:
      - "10.1.0.0/16"
```

**Steps:**
1. INVITE from 10.1.5.5 (matches both "first" and "second")
2. `curl -s localhost:2112/metrics | grep invite_total`

**Expected:** `carrier="first"` (first match wins), no crash.

---

## TC-18: Tracker TTL expiry — fallback to response IP

**Purpose:** verify carrier fallback when tracker entry expires (TTL=60s).

**Steps:**
1. Send INVITE from carrier-A IP → save in inviteTracker
2. Wait 61 seconds (tracker entry expires)
3. Send 200 OK response for that Call-ID
4. `curl -s localhost:2112/metrics | grep invite200_ok_total`

**Expected:** carrier resolved from response packet IP (fallback), not "other" if response IP matches a carrier.

---

## TC-19: ISS per carrier

**Purpose:** verify ineffective session severity counter per carrier.

**Steps:**
1. From carrier-A: 10 INVITE → all get 500 Server Error
2. From carrier-B: 10 INVITE → all get 200 OK
3. `curl -s localhost:2112/metrics | grep sip_exporter_iss_total`

**Expected:**
```
sip_exporter_iss_total{carrier="carrier-A"} 10
sip_exporter_iss_total{carrier="carrier-B"} 0
```

---

## TC-20: NER = 100 - ISA per carrier

**Purpose:** verify mathematical relationship per carrier.

**Steps:**
1. From carrier-A: 20 INVITE → 200 OK, 15 INVITE → 500, 15 INVITE → 486 Busy
2. ISA = 15/(20+15+15) × 100 = 30%
3. `curl -s localhost:2112/metrics | grep -E 'isa|ner'`

**Expected:**
```
sip_exporter_isa{carrier="carrier-A"} 30.0
sip_exporter_ner{carrier="carrier-A"} 70.0
```
NER + ISA = 100.

---

## TC-21: Sessions gauge per carrier

**Purpose:** verify `sip_exporter_sessions` shows active dialogs per carrier.

**Steps:**
1. INVITE from carrier-A → 200 OK (1 active session)
2. INVITE from carrier-B → 200 OK (1 active session)
3. Do NOT send BYE
4. `curl -s localhost:2112/metrics | grep sip_exporter_sessions`

**Expected:**
```
sip_exporter_sessions{carrier="carrier-A"} 1
sip_exporter_sessions{carrier="carrier-B"} 1
```

---

## TC-22: Reload after config change (restart required)

**Purpose:** verify config is re-read on restart.

**Steps:**
1. Start with carriers.yaml containing carrier-A only
2. Send INVITE → verify `carrier="carrier-A"`
3. Stop exporter
4. Add carrier-B to carriers.yaml
5. Start exporter again
6. Send INVITE from carrier-B IP
7. `curl -s localhost:2112/metrics | grep invite_total`

**Expected:** `carrier="carrier-B"` appears after restart.

---

## TC-23: Large number of carriers

**Purpose:** verify no performance degradation with many carriers.

**Config:** 50 carriers, each with 2 CIDRs.

**Steps:**
1. Start exporter
2. Send INVITE from IP matching carrier #50
3. Verify startup time < 5s
4. Verify `/metrics` scrape time < 100ms

**Expected:** no degradation, correct carrier resolution.
