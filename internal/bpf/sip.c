#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>
#include <linux/ip.h>

#define ETH_P_IP     0x0800
#define IPPROTO_UDP  17

// Map for SIP ports (configured from userspace)
struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 2);
	__type(key, __u32);
	__type(value, __u16);
} sip_ports SEC(".maps");

// RTP capture config (configured from userspace)
// value: 1 = RTP capture enabled, 0 = disabled
struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, __u8);
} rtp_config SEC(".maps");

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

    // Read ports from map
    __u32 key_sip = 0;
    __u32 key_sips = 1;
    __u16 *sip_port = bpf_map_lookup_elem(&sip_ports, &key_sip);
    __u16 *sips_port = bpf_map_lookup_elem(&sip_ports, &key_sips);
    
    __u16 port1 = sip_port ? *sip_port : 5060;
    __u16 port2 = sips_port ? *sips_port : 5061;

    // SIP port → pass the entire packet (for SIP header parsing)
    if (src_port == port1 || src_port == port2 ||
        dest_port == port1 || dest_port == port2) {
        return skb->len;
    }

    // Not a SIP port — check RTP capture config
    __u32 cfg_key = 0;
    __u8 *rtp_on = bpf_map_lookup_elem(&rtp_config, &cfg_key);
    if (!rtp_on || *rtp_on != 1) {
        return 0;
    }

    // SDP-driven lookup: check if endpoint is a known RTP media endpoint.
    // dst first (local receive endpoint, NAT-robust), then src as fallback.
    __u32 src_ip = (__u32)ip_header[12]<<24 | (__u32)ip_header[13]<<16
                 | (__u32)ip_header[14]<<8  | (__u32)ip_header[15];
    __u32 dst_ip = (__u32)ip_header[16]<<24 | (__u32)ip_header[17]<<16
                 | (__u32)ip_header[18]<<8  | (__u32)ip_header[19];

    struct rtp_endpoint_key dst_key = { .ip = dst_ip, .port = dest_port, ._pad = 0 };
    if (bpf_map_lookup_elem(&rtp_endpoints, &dst_key)) {
        __u32 snap = skb->len;
        if (snap > 64) snap = 64;
        return snap;
    }

    struct rtp_endpoint_key src_key = { .ip = src_ip, .port = src_port, ._pad = 0 };
    if (bpf_map_lookup_elem(&rtp_endpoints, &src_key)) {
        __u32 snap = skb->len;
        if (snap > 64) snap = 64;
        return snap;
    }

    // No SDP-driven match and no pattern fallback — drop.
    // Only RTP from endpoints learned via SDP signaling is passed.
    return 0;
}

char _license[] SEC("license") = "GPL";
