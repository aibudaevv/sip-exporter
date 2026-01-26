#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>
#include <linux/ip.h>

#define ETH_P_IP     0x0800
#define IPPROTO_UDP  17
#define UDP_PORT_SIP 5060
#define UDP_PORT_SIPS 5061
//#define FIXED_SIZE   420
#define FIXED_SIZE   512
#define SIP_MIN_LEN  40

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 16);
} rb SEC(".maps");

SEC("socket")
int bpf_socket_filter(struct __sk_buff *skb) {
    __u8 ip_header[20];
    int ret, ip_offset = 0;

    if (skb->len >= 14) {
        __u16 eth_type;
        ret = bpf_skb_load_bytes(skb, 12, &eth_type, 2);
        if (ret == 0 && eth_type == bpf_htons(ETH_P_IP)) ip_offset = 14;
    }

    ret = bpf_skb_load_bytes(skb, ip_offset, ip_header, 20);
    if (ret < 0) return 0;

    if ((ip_header[0] >> 4) != 4) return 0;
    __u8 ihl = ip_header[0] & 0x0F;
    if (ihl < 5 || ihl * 4 > 60 || ip_header[9] != IPPROTO_UDP) return 0;

    __u8 udp_raw[4];
    ret = bpf_skb_load_bytes(skb, ip_offset + ihl * 4, udp_raw, 4);
    if (ret < 0) return 0;

    __u16 src_port = ((__u16)udp_raw[0] << 8 | udp_raw[1]);
    __u16 dest_port = ((__u16)udp_raw[2] << 8 | udp_raw[3]);

    if (src_port != UDP_PORT_SIP && src_port != UDP_PORT_SIPS &&
        dest_port != UDP_PORT_SIP && dest_port != UDP_PORT_SIPS) return 0;

    if (skb->len < SIP_MIN_LEN) return 0;

     bpf_printk("SIP matched: %u->%u len=%d", src_port, dest_port, skb->len);

    void *buf = bpf_ringbuf_reserve(&rb, FIXED_SIZE, 0);
    if (!buf) {
        bpf_printk("reserve failed");
        return 0;
    }

    // metadata (8 bytes)
    __u32 *pkt_len_ptr = (__u32*)buf;
    __u16 *ports_ptr = (__u16*)(buf + 4);
    *pkt_len_ptr = skb->len;
    ports_ptr[0] = src_port;
    ports_ptr[1] = dest_port;

    __u8 *data_start = buf + 8;
    ret = bpf_skb_load_bytes(skb, 0, data_start, 334);
    if (ret < 0) {
        bpf_printk("load failed");
        bpf_ringbuf_discard(buf, 0);
        return 0;
    }

    bpf_ringbuf_submit(buf, 0);
    return 0;
}

char _license[] SEC("license") = "GPL";
