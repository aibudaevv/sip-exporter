#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>
#include <linux/ip.h>

#define ETH_P_IP     0x0800
#define IPPROTO_UDP  17
#define UDP_PORT_SIP 5060
#define UDP_PORT_SIPS 5061
#define MAX_SIP_SIZE 512

// Структура события: длина + данные
struct sip_event {
    __u32 len;
    __u8 data[MAX_SIP_SIZE];
};

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 16);
} rb SEC(".maps");

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

    __u32 sip_payload_offset = ip_offset + ip_header_len + 8;

    if (skb->len <= sip_payload_offset) {
        return 0;
    }

    __u32 sip_payload_len = skb->len - sip_payload_offset;

    bpf_printk("SIP matched: %u->%u, len=%u", src_port, dest_port, sip_payload_len);

    struct sip_event *e;
    e = bpf_ringbuf_reserve(&rb, sizeof(*e), 0);
    if (!e) {
        bpf_printk("failed reserve memory");
        return 0;
    }

    // Сохраняем фактическую длину (ограниченную MAX_SIP_SIZE)
    e->len = sip_payload_len;
    if (e->len > MAX_SIP_SIZE) {
        e->len = MAX_SIP_SIZE;
    }

    // Копируем блоками по 64 байта
    // Копируем блок, если в нём есть хотя бы 1 байт данных
    if (sip_payload_offset + 1 <= skb->len) {
        __u32 len = 64;
        if (sip_payload_offset + len > skb->len) {
            len = skb->len - sip_payload_offset;
        }
        ret = bpf_skb_load_bytes(skb, sip_payload_offset, e->data, len);
        if (ret < 0) goto discard;
    }
    if (sip_payload_offset + 65 <= skb->len) {
        __u32 len = 64;
        if (sip_payload_offset + 64 + len > skb->len) {
            len = skb->len - sip_payload_offset - 64;
        }
        ret = bpf_skb_load_bytes(skb, sip_payload_offset + 64, (void *)e->data + 64, len);
        if (ret < 0) goto discard;
    }
    if (sip_payload_offset + 129 <= skb->len) {
        __u32 len = 64;
        if (sip_payload_offset + 128 + len > skb->len) {
            len = skb->len - sip_payload_offset - 128;
        }
        ret = bpf_skb_load_bytes(skb, sip_payload_offset + 128, (void *)e->data + 128, len);
        if (ret < 0) goto discard;
    }
    if (sip_payload_offset + 193 <= skb->len) {
        __u32 len = 64;
        if (sip_payload_offset + 192 + len > skb->len) {
            len = skb->len - sip_payload_offset - 192;
        }
        ret = bpf_skb_load_bytes(skb, sip_payload_offset + 192, (void *)e->data + 192, len);
        if (ret < 0) goto discard;
    }
    if (sip_payload_offset + 257 <= skb->len) {
        __u32 len = 64;
        if (sip_payload_offset + 256 + len > skb->len) {
            len = skb->len - sip_payload_offset - 256;
        }
        ret = bpf_skb_load_bytes(skb, sip_payload_offset + 256, (void *)e->data + 256, len);
        if (ret < 0) goto discard;
    }
    if (sip_payload_offset + 321 <= skb->len) {
        __u32 len = 64;
        if (sip_payload_offset + 320 + len > skb->len) {
            len = skb->len - sip_payload_offset - 320;
        }
        ret = bpf_skb_load_bytes(skb, sip_payload_offset + 320, (void *)e->data + 320, len);
        if (ret < 0) goto discard;
    }
    if (sip_payload_offset + 385 <= skb->len) {
        __u32 len = 64;
        if (sip_payload_offset + 384 + len > skb->len) {
            len = skb->len - sip_payload_offset - 384;
        }
        ret = bpf_skb_load_bytes(skb, sip_payload_offset + 384, (void *)e->data + 384, len);
        if (ret < 0) goto discard;
    }
    if (sip_payload_offset + 449 <= skb->len) {
        __u32 len = 64;
        if (sip_payload_offset + 448 + len > skb->len) {
            len = skb->len - sip_payload_offset - 448;
        }
        ret = bpf_skb_load_bytes(skb, sip_payload_offset + 448, (void *)e->data + 448, len);
        if (ret < 0) goto discard;
    }

submit:
    bpf_ringbuf_submit(e, 0);
    return 0;

discard:
    bpf_ringbuf_discard(e, 0);
    return 0;
}

char _license[] SEC("license") = "GPL";
