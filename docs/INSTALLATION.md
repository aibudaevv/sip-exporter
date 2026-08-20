# Installation Verification and Empty-Dashboard Runbook

Use this runbook after deploying the [production Compose example](../examples/docker-compose.production.yml). It verifies the path from exporter to Grafana without exposing SIP payloads, Call-ID values, or endpoint labels.

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

Run from the directory containing `docker-compose.yml`:

```bash
docker compose ps
curl -fsS http://127.0.0.1:10047/health
curl -fsS http://127.0.0.1:10047/metrics | grep '^sip_exporter_build_info'
```

Expected result: the service is running, `/health` returns successfully, and the last command prints the build-info metric. If a check fails, inspect operational logs only:

```bash
docker compose logs --tail=100 sip-exporter
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
up{job="sip-exporter"}
```

If your scrape configuration uses another job name, replace `sip-exporter`. A successful local `curl` with a target still down means the scraper cannot reach the host: check address, port `10047`, firewall and network namespace. Do not diagnose SIP capture until the target is `UP`.

<a id="verify-sip"></a>
## 3. Verify SIP Capture

Place one test call through the monitored production path, then query:

```promql
sum(increase(sip_exporter_socket_packets_received_total[5m]))
sum(increase(sip_exporter_invite_total[5m]))
```

The socket counter proves packets reached the AF_PACKET socket. A positive INVITE increase proves that SIP INVITEs were parsed. If the socket counter is zero, choose the correct NIC and verify the host forwards or terminates the traffic. If it rises but INVITEs do not, confirm UDP transport and `SIP_EXPORTER_SIP_PORTS`; SIP over TCP/TLS is not captured.

<a id="verify-dialog-sdp"></a>
## 4. Verify Dialog and SDP Visibility

During an active answered call, query:

```promql
sum(sip_exporter_active_dialogs)
sum(sip_exporter_active_trackers{type="rtp"})
```

After a completed call, query:

```promql
sum(increase(sip_exporter_sessions_missing_rtp_total[15m]))
```

An active dialog confirms that the INVITE/200 OK dialog was observed. `sessions_missing_rtp_total` rises only after a dialog with SDP media endpoints ends without observed RTP. If SIP is present but media correlation is absent, ensure both SIP directions, final IPv4 UDP SDP endpoints and media traverse the same supported path. RTP-only visibility cannot be correlated because media endpoints are learned from SDP.

<a id="verify-rtp"></a>
## 5. Verify RTP Capture

While media flows, query:

```promql
sum(increase(sip_exporter_rtp_packets_total[5m]))
sum(sip_exporter_rtp_active_streams)
```

Both values should be positive for an active call with visible media. If SIP and dialogs work but RTP stays zero, media may bypass the host, NAT may change the source port (symmetric RTP), SDP may differ from the observed endpoint, or only a mirrored/SPAN copy may be available. Move the sensor to a forwarding host that sees SIP and both RTP directions before interpreting empty RTP panels as voice quality.

<a id="verify-drops"></a>
## 6. Verify Data Quality and Drops

Query these before acting on quality, fraud, or one-way-media panels:

```promql
sum(rate(sip_exporter_socket_packets_dropped_total[5m]))
sum(rate(sip_exporter_rtp_dropped_total[5m]))
100 * sum(rate(sip_exporter_socket_packets_dropped_total[5m])) / sum(rate(sip_exporter_socket_packets_received_total[5m]))
sip_exporter_channel_length / clamp_min(sip_exporter_channel_capacity, 1)
```

Socket drops mean the kernel receive buffer overflowed; RTP drops mean the internal userspace channel was full. A channel ratio near `1` means sustained saturation. Reduce traffic per sensor, correct duplicate-interface capture, or provision adequate CPU before trusting derived RTP loss, MOS, FAS, missing-RTP or one-way-RTP signals.

See [Metrics](METRICS.md), [Alerting](ALERTING.md), and the [Grafana dashboard](../examples/grafana-dashboard.json) for metric definitions and alerts.
