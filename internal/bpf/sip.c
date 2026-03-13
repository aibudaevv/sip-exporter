#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>
#include <linux/ip.h>

#define ETH_P_IP     0x0800
#define IPPROTO_UDP  17
#define UDP_PORT_SIP 5060
#define UDP_PORT_SIPS 5061

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

    if (src_port != UDP_PORT_SIP && src_port != UDP_PORT_SIPS &&
        dest_port != UDP_PORT_SIP && dest_port != UDP_PORT_SIPS) {
        return 0;
    }

    // Возвращаем полную длину пакета
    bpf_printk("SIP packet: %u->%u, skb->len=%u", src_port, dest_port, skb->len);
    return skb->len;
}

char _license[] SEC("license") = "GPL";
