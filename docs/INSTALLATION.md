# Installation Verification and Empty-Dashboard Runbook

Use this runbook after deploying the [production Compose example](../examples/docker-compose.production.yml). It verifies the exporter-to-Grafana path. The diagnostic queries below do not display SIP payloads; logs can contain Call-IDs and other sensitive data, so redact them before sharing.

## First Useful Dashboard

1. Start the exporter on a host that sees both SIP signalling and RTP media.
2. Confirm the container and its `/health` endpoint.
3. Configure any Prometheus-compatible scraper to collect `http://<host>:10047/metrics`.
4. Confirm the target is `UP`, then make one test call through the monitored path.
5. Import [`examples/grafana-dashboard.json`](../examples/grafana-dashboard.json) and select the scraper datasource.

The exporter supports IPv4 UDP SIP and RTP. SIP over TCP/TLS, IPv6, fragmented UDP, SPAN/TAP QoE and RTP without visible SDP are outside this capture contract; see the [deployment topology](../README.md#deployment-topology).

> **Port migration:** new installations use `10047`. An existing deployment may retain the
> previous port by setting `SIP_EXPORTER_HTTP_PORT=2112` and updating its scrape and healthcheck
> URLs consistently.

## Support Matrix

| Symptom | Check | Meaning | Next action |
|---|---|---|---|
| No metrics target | target status | The scraper cannot reach the exporter | Check URL, port, firewall and scraper network path. |
| Target is down | `/health` and container | The exporter is not ready or has stopped | Check Compose status and logs. |
| Target is up, no SIP panels | `invite_total` and socket receive counter | No SIP reaches the configured interface and UDP port | Verify NIC, `SIP_EXPORTER_SIP_PORTS`, and topology. |
| SIP panels work, RTP panels are empty | dialog and RTP counters | SDP or RTP is not visible/correlated | Verify that SIP with SDP and media use a supported path. |
| Values are incomplete | socket/userspace drop counters | Capture or userspace processing loses packets | Resolve drops before trusting QoE or fraud signals. |

<a id="verify-container"></a>
## 1. Verify Container and Health

Run from the directory containing `docker-compose.production.yml` and `.env` prepared in Quick Start. Keep `SIP_EXPORTER_INTERFACE` in `.env` for subsequent commands:

```bash
docker compose --env-file .env -f docker-compose.production.yml ps
curl -fsS http://127.0.0.1:10047/health
curl -fsS http://127.0.0.1:10047/metrics | grep '^sip_exporter_build_info'
```

Expected result: the service is running, `/health` succeeds and the last command prints build info. Health confirms initialization, not complete capture. If a check fails, inspect logs locally; even `info` logs can contain Call-IDs:

```bash
docker compose --env-file .env -f docker-compose.production.yml logs --tail=100 sip-exporter
```

Confirm that `SIP_EXPORTER_INTERFACE` names the NIC carrying production traffic. The container needs `network_mode: host` and `privileged: true`; do not enable `SIP_EXPORTER_IGNORE_OUTGOING` outside loopback tests.

<a id="verify-scrape"></a>
## 2. Verify the Scrape Target

Configure the scraper to collect:

```text
http://<exporter-host>:10047/metrics
```

In its target-status view, the target must be `UP`. In Grafana Explore, select the same datasource and query:

```promql
up{job="sip-exporter",instance="sensor:10047"}
```

In every query, replace `sensor:10047` with the target's exact `instance` label and adjust `job` if needed. Local curl and remote scraping use different network paths: for a down target, inspect its error, address, port and firewall. Diagnose SIP only after the target is UP.

<a id="verify-sip"></a>
## 3. Verify SIP Capture

On a new sensor, make an initial test call to create the labeled series. Wait for at least two successful scrapes, make a second call, then wait for another scrape before checking counter growth:

```promql
sum(increase(sip_exporter_socket_packets_received_total{job="sip-exporter",instance="sensor:10047"}[5m]))
sum(increase(sip_exporter_invite_total{job="sip-exporter",instance="sensor:10047"}[5m]))
```

The socket counter proves packets reached the AF_PACKET socket. A positive INVITE increase proves that SIP INVITEs were parsed. If the socket counter is zero, choose the correct NIC and verify the host forwards or terminates the traffic. If it rises but INVITEs do not, confirm UDP transport and `SIP_EXPORTER_SIP_PORTS`; SIP over TCP/TLS is not captured.

`increase()` cannot recover events before the first sample of a new series. An empty result or zero increase after a single call does not prove capture failure. Inspect the current `sip_exporter_invite_total` with the same filters and repeat the controlled call if necessary.

<a id="verify-dialog-sdp"></a>
## 4. Verify Dialog and SDP Visibility

During an active answered call, query:

```promql
sum(sip_exporter_active_dialogs{job="sip-exporter",instance="sensor:10047"})
sum(sip_exporter_active_trackers{job="sip-exporter",instance="sensor:10047",type="rtp"})
```

After a completed call, query:

```promql
sum(increase(sip_exporter_sessions_missing_rtp_total{job="sip-exporter",instance="sensor:10047"}[15m]))
```

An active dialog confirms that the INVITE/200 OK dialog was observed. `sessions_missing_rtp_total` rises only after a dialog with SDP media endpoints ends without observed RTP. If SIP is present but media correlation is absent, ensure both SIP directions, final IPv4 UDP SDP endpoints and media traverse the same supported path. RTP-only visibility cannot be correlated because media endpoints are learned from SDP.

<a id="verify-rtp"></a>
## 5. Verify RTP Capture

While media flows, query:

```promql
sum(increase(sip_exporter_rtp_packets_total{job="sip-exporter",instance="sensor:10047"}[5m]))
sum(sip_exporter_rtp_active_streams{job="sip-exporter",instance="sensor:10047"})
```

During a sufficiently long controlled call, active RTP streams should become positive; packet increases require multiple scrapes. If SIP is visible but RTP is not, check the media route and SDP addresses. Source-port remapping is supported when the source IP remains unchanged and correlation is unambiguous; source-IP changes and ambiguous shared endpoints are unsupported. Use a capture point that sees SIP and both RTP directions, following the topology support matrix.

<a id="verify-drops"></a>
## 6. Verify Data Quality and Drops

Query these before acting on quality, fraud, or one-way-media panels:

```promql
sum(rate(sip_exporter_socket_packets_dropped_total{job="sip-exporter",instance="sensor:10047"}[5m]))
sum(rate(sip_exporter_rtp_dropped_total{job="sip-exporter",instance="sensor:10047"}[5m]))
100 * sum(rate(sip_exporter_socket_packets_dropped_total{job="sip-exporter",instance="sensor:10047"}[5m])) / sum(rate(sip_exporter_socket_packets_received_total{job="sip-exporter",instance="sensor:10047"}[5m]))
sip_exporter_channel_length{job="sip-exporter",instance="sensor:10047"} / clamp_min(sip_exporter_channel_capacity{job="sip-exporter",instance="sensor:10047"}, 1)
```

`socket_packets_dropped_total` measures kernel receive-buffer drops. `rtp_dropped_total` measures RTP rejected by the application queue because capacity is insufficient or waiting SIP has priority. Queue length is sampled and can miss short bursts; recorded drops take precedence over that snapshot. Reduce sensor load, eliminate duplicate interface capture or increase processing capacity. Until drops are resolved, treat RTP quality, MOS, FAS and media-presence conclusions as incomplete.

See [Metrics](METRICS.md), [Alerting](ALERTING.md), and the [Grafana dashboard](../examples/grafana-dashboard.json) for metric definitions and alerts.
