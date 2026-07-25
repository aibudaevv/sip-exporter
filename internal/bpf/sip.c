#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>
#include <linux/ip.h>

#define ETH_P_IP     0x0800
#define IPPROTO_UDP  17
#define SIP_MAX_PORTS 3  // must match maxPortsPerInterface in internal/config/config.go

// RTCP packet types (RFC 3550 §6): SR=200, RR=201, SDES=202, BYE=203, APP=204.
// RTP and RTCP share V=2 on the same port (rtcp-mux, RFC 5761). They are told
// apart by the UDP payload's packet-type byte (byte[1]): RTCP uses 200-204. The
// ranges are disjoint ONLY because RFC 5761 §4 forbids RTP payload types 64-95
// under rtcp-mux — otherwise an RTP packet with the marker bit set and PT in
// {72..76} would yield byte[1] in {200..204}. A non-matching byte[1] here is
// treated as RTP and keeps the small header-only snapshot.
#define RTCP_PT_MIN 200
#define RTCP_PT_MAX 204

// Snapshot size returned to userspace for a matched RTP endpoint. RTP needs only
// its 12-byte header (64 bytes is ample incl. L2/L3/L4 headers); RTCP compounds
// must arrive whole, so they bypass this cap (see the PT peek below) but are
// themselves capped at the Ethernet MTU — every real RTCP compound fits.
#define RTP_SNAPSHOT_CAP 64
#define RTCP_SNAPSHOT_CAP 1500

// Map for SIP ports (configured from userspace)
struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, SIP_MAX_PORTS);
	__type(key, __u32);
	__type(value, __u16);
} sip_ports SEC(".maps");

// SDP-driven RTP endpoint map (populated from userspace via SDP parsing)
struct rtp_endpoint_key {
	__u32 ip;     // IPv4 in host byte order (byte[0]<<24 | byte[1]<<16 | ...)
	__u16 port;   // Port in host byte order
	__u16 _pad;   // Alignment to 8 bytes
};

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 65536);
	__type(key, struct rtp_endpoint_key);
	__type(value, __u8);
} rtp_endpoints SEC(".maps");

SEC("socket")
int bpf_socket_filter(struct __sk_buff *skb) {
    if (skb->len < 14) {
        return 0;
    }

    int ret;
    int ip_offset = 0;
    __u16 eth_type;

    ret = bpf_skb_load_bytes(skb, 12, &eth_type, 2);
    if (ret < 0) return 0;

    if (eth_type == bpf_htons(0x8100)) {
        if (skb->len < 18) return 0;
        ret = bpf_skb_load_bytes(skb, 16, &eth_type, 2);
        if (ret < 0) return 0;
        ip_offset = 18;
    } else {
        ip_offset = 14;
    }

    if (eth_type != bpf_htons(ETH_P_IP)) {
        return 0;
    }

    if (skb->len < ip_offset + 20) {
        return 0;
    }

    __u8 ip_header[20];
    ret = bpf_skb_load_bytes(skb, ip_offset, ip_header, 20);
    if (ret < 0) return 0;

    if ((ip_header[0] >> 4) != 4) {
        return 0;
    }

    __u8 ihl = ip_header[0] & 0x0F;
    __u8 ip_header_len = ihl * 4;

    if (ihl < 5 || ihl > 15) {
        return 0;
    }

    if (skb->len < ip_offset + ip_header_len) {
        return 0;
    }

    if (ip_header[9] != IPPROTO_UDP) {
        return 0;
    }

    if (skb->len < ip_offset + ip_header_len + 8) {
        return 0;
    }

    __u8 udp_raw[4];
    ret = bpf_skb_load_bytes(skb, ip_offset + ip_header_len, udp_raw, 4);
    if (ret < 0) return 0;

    __u16 src_port = (__u16)((udp_raw[0] << 8) | udp_raw[1]);
    __u16 dest_port = (__u16)((udp_raw[2] << 8) | udp_raw[3]);

    // Check SIP ports (up to SIP_MAX_PORTS slots; zero entries are skipped).
    // Backward compat: userspace writes keys 0,1 (sip,sips); key 2 stays 0.
    #pragma unroll
    for (int i = 0; i < SIP_MAX_PORTS; i++) {
        __u32 key = i;
        __u16 *port = bpf_map_lookup_elem(&sip_ports, &key);
        if (port && *port != 0 &&
            (src_port == *port || dest_port == *port)) {
            return skb->len;
        }
    }

    // Not a SIP port — check SDP-driven RTP endpoint lookup.
    __u32 src_ip = (__u32)ip_header[12]<<24 | (__u32)ip_header[13]<<16
                 | (__u32)ip_header[14]<<8  | (__u32)ip_header[15];
    __u32 dst_ip = (__u32)ip_header[16]<<24 | (__u32)ip_header[17]<<16
                 | (__u32)ip_header[18]<<8  | (__u32)ip_header[19];

    struct rtp_endpoint_key dst_key = { .ip = dst_ip, .port = dest_port, ._pad = 0 };
    struct rtp_endpoint_key src_key = { .ip = src_ip, .port = src_port, ._pad = 0 };

    // dst first (local receive endpoint, NAT-robust), then src as fallback.
    int matched = 0;
    if (bpf_map_lookup_elem(&rtp_endpoints, &dst_key)) {
        matched = 1;
    } else if (bpf_map_lookup_elem(&rtp_endpoints, &src_key)) {
        matched = 1;
    }
    if (matched) {
        // PT peek: RTCP compounds must reach userspace whole (a 64-byte snapshot
        // leaves only ~22 bytes of UDP payload, truncating any report block). RTP
        // needs only its 12-byte header, so it keeps the small cap. The UDP payload
        // starts at ip_offset + ip_header_len + 8 (UDP header); PT is byte[1].
        __u32 payload_off = ip_offset + ip_header_len + 8;
        __u8 pt = 0;
        if (bpf_skb_load_bytes(skb, payload_off + 1, &pt, 1) == 0 &&
            pt >= RTCP_PT_MIN && pt <= RTCP_PT_MAX) {
            // RTCP — full compound, capped at the MTU as defense-in-depth against
            // oversized packets (legitimate RTCP always fits a single Ethernet frame).
            __u32 rtcp_snap = skb->len;
            if (rtcp_snap > RTCP_SNAPSHOT_CAP) rtcp_snap = RTCP_SNAPSHOT_CAP;
            return rtcp_snap;
        }
        __u32 snap = skb->len;
        if (snap > RTP_SNAPSHOT_CAP) snap = RTP_SNAPSHOT_CAP;
        return snap;  // RTP (or PT unreadable) — header-only snapshot
    }

    // No SDP-driven match and no pattern fallback — drop.
    // Only RTP/RTCP from endpoints learned via SDP signaling is passed.
    return 0;
}

char _license[] SEC("license") = "GPL";
