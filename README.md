# SIP-exporter
High-performance eBPF-based SIP monitoring service that captures and exports telephony metrics to Prometheus.  
Zero-copy packet processing directly in the Linux kernel for <1μs latency on 10Gbps+ SIP traffic.  
### Core Technology: eBPF
This service uses eBPF (extended Berkeley Packet Filter) attached to network sockets (XDP-like filtering) to  
intercept SIP packets (UDP/5060-5061) at L4 without overhead of iptables/nftables or userspace daemons like tcpdump.  
### Architecture
SIP Traffic → NIC → eBPF socket filter → ringbuf → Go poller → SIP parser → Prometheus

## Install  
`docker pull frzq/sip-exporter:0.2.0`
### Configure
Environment variables:
* `SIP_EXPORTER_INTERFACE` - net interface (required)
* `SIP_EXPORTER_HTTP_PORT` - http port for prometheus (default 2112)
* `SIP_EXPORTER_LOGGER_LEVEL` - log level (default info)
* `SIP_EXPORTER_SIP_PORT` - SIP port (default 5060)
* `SIP_EXPORTER_SIPS_PORT` - SIPS port (default 5061)
* `SIP_EXPORTER_OBJECT_FILE_PATH` - path to eBPF object file (default /usr/local/bin/sip.o)

Start docker container in privileged mode is true and host mode.
## Metrics
### Generic SIP traffic metric
`sip_exporter_packets_total`: total number of parsed SIP packets (requests + responses).  
`sip_exporter_sessions`: active sip dialogs. (unique session it key call-id:from.tag:to.tag) 

### SIP request metrics
`sip_exporter_publish_total`: total number of received SIP PUBLISH requests.  
`sip_exporter_prack_total`: total number of received SIP PRACK requests.  
`sip_exporter_notify_total`: total number of received SIP NOTIFY requests.  
`sip_exporter_subscribe_total`: total number of received SIP SUBSCRIBE requests.  
`sip_exporter_refer_total`: total number of received SIP REFER requests.  
`sip_exporter_info_total`: total number of received SIP INFO requests.  
`sip_exporter_update_total`: total number of received SIP UPDATE requests.  
`sip_exporter_register_total`: total number of received SIP REGISTER requests.  
`sip_exporter_options_total`: total number of received SIP OPTIONS requests.  
`sip_exporter_cancel_total`: total number of received SIP CANCEL requests.  
`sip_exporter_bye_total`: total number of received SIP BYE requests.  
`sip_exporter_ack_total`: total number of received SIP ACK requests.  
`sip_exporter_invite_total`: total number of received SIP INVITE requests.  
### SIP response metrics (by status code)
`sip_exporter_100_total`: total number of SIP 100 Trying responses.  
`sip_exporter_180_total`: total number of SIP 180 Ringing responses.  
`sip_exporter_183_total`: total number of SIP 183 Session Progress responses.  
`sip_exporter_200_total`: total number of SIP 200 OK responses.  
`sip_exporter_202_total`: total number of SIP 202 Accepted responses.  
`sip_exporter_300_total`: total number of SIP 300 Multiple Choices responses.  
`sip_exporter_302_total`: total number of SIP 302 Moved Temporarily responses.  
`sip_exporter_400_total`: total number of SIP 400 Bad Request responses.  
`sip_exporter_401_total`: total number of SIP 401 Unauthorized responses.  
`sip_exporter_403_total`: total number of SIP 403 Forbidden responses.  
`sip_exporter_404_total`: total number of SIP 404 Not Found responses.  
`sip_exporter_408_total`: total number of SIP 408 Request Timeout responses.  
`sip_exporter_480_total`: total number of SIP 480 Temporarily Unavailable responses.  
`sip_exporter_486_total`: total number of SIP 486 Busy Here responses.  
`sip_exporter_500_total`: total number of SIP 500 Server Internal Error responses.  
`sip_exporter_503_total`: total number of SIP 503 Service Unavailable responses.  
`sip_exporter_600_total`: total number of SIP 600 Busy Everywhere responses.  
`sip_exporter_603_total`: total number of SIP 603 Decline responses.  
### System metrics  
`sip_exporter_system_error_total`: total number internal sip exporter error.