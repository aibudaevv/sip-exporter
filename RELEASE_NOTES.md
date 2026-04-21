🚀 SIP Exporter v0.9.0: TTR, SPD, ASR, SDC — four new metrics for SIP monitoring

Released a new version of SIP Exporter — an eBPF-based SIP traffic monitor for Prometheus. What's new:

📊 New Metrics

⏱ TTR (Time to First Response) — delay from INVITE to first provisional response (100 Trying, 180 Ringing). Histogram with buckets from 1ms to 5s. Track SBC/softswitch response latency.
📞 SPD (Session Process Duration, RFC 6076 §4.5) — full SIP session duration from 200 OK INVITE to BYE or Session-Expires timeout. Histogram with buckets from 1s to 1h.
📈 ASR (Answer Seizure Ratio, ITU-T E.411) — ratio of answered calls to total call attempts.
🔢 SDC (Session Duration Counter) — counter of completed SIP sessions.

🛠 Other Improvements

💚 /health endpoint — 200 when exporter is running, 503 when not
🏋️ Load test baseline system — SLO-based load tests with regression detection

🐳 Docker: docker pull frzq/sip-exporter:0.9.0
💻 GitHub: github.com/aibudaevv/sip-exporter

#SIP #eBPF #Monitoring #Prometheus #VoIP #OpenSource