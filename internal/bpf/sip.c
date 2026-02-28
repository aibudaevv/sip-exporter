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
#define MAX_SIP_SIZE 512
#define SIP_BUFFER_SIZE 512


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
    int ip_offset = 0;                    // Смещение до IP-заголовка
    __u16 eth_type;

    // Читаем ethertype с позиции 12 (после MAC-адресов)
    ret = bpf_skb_load_bytes(skb, 12, &eth_type, 2);
    if (ret < 0) return 0;

    // Проверяем, является ли это VLAN-тегом (0x8100)
    if (eth_type == bpf_htons(0x8100)) {
        // Это VLAN-кадр
        // Проверяем, достаточно ли данных для чтения VLAN + новый ethertype
        if (skb->len < 18) return 0;

        // Читаем ethertype после VLAN-тега (смещение 16)
        ret = bpf_skb_load_bytes(skb, 16, &eth_type, 2);
        if (ret < 0) return 0;

        // IP-заголовок начинается с 18-го байта
        ip_offset = 18;
    } else {
        // Это обычный Ethernet-кадр (без VLAN)
        // IP-заголовок начинается с 14-го байта
        ip_offset = 14;
    }

//    bpf_printk("packet: offset=%u (14 without VLAN, 18 with VLAN)", ip_offset);

    // Проверяем, что дальше идёт IPv4
    if (eth_type != bpf_htons(ETH_P_IP)) {
        return 0;
    }

    // Проверяем, хватает ли данных для IP-заголовка (минимум 20 байт)
    if (skb->len < ip_offset + 20) {
        return 0;
    }

    // Читаем первые 20 байт IP-заголовка
    __u8 ip_header[20];
    ret = bpf_skb_load_bytes(skb, ip_offset, ip_header, 20);
    if (ret < 0) return 0;

    // Проверяем версию IP: должно быть IPv4 (биты 0-3: версия)
    if ((ip_header[0] >> 4) != 4) {
        return 0;
    }

    // Извлекаем длину IP-заголовка (IHL) — нижние 4 бита
    __u8 ihl = ip_header[0] & 0x0F;
    // IHL измеряется в 32-битных словах → умножаем на 4
    __u8 ip_header_len = ihl * 4;

    // Проверяем корректность IHL
    if (ihl < 5 || ihl > 15) {  // IHL: 5..15 (20..60 байт)
        return 0;
    }

    // Проверяем, что пакет не обрезан
    if (skb->len < ip_offset + ip_header_len) {
        return 0;
    }


    // Проверяем, что протокол — UDP
    if (ip_header[9] != IPPROTO_UDP) {
        return 0;
    }

    // Проверяем, хватает ли данных для UDP-заголовка (8 байт)
    if (skb->len < ip_offset + ip_header_len + 8) {
         return 0;
    }


    // Читаем первые 4 байта UDP-заголовка: src_port и dest_port
    __u8 udp_raw[4];
    ret = bpf_skb_load_bytes(skb, ip_offset + ip_header_len, udp_raw, 4);
    if (ret < 0) return 0;

    // Извлекаем порты
    __u16 src_port = (__u16)((udp_raw[0] << 8) | udp_raw[1]);
    __u16 dest_port = (__u16)((udp_raw[2] << 8) | udp_raw[3]);

    // Проверяем, является ли трафик SIP (порт 5060 или 5061)
    if (src_port != UDP_PORT_SIP && src_port != UDP_PORT_SIPS &&
        dest_port != UDP_PORT_SIP && dest_port != UDP_PORT_SIPS) {
            return 0;  // Не SIP — отклоняем
    }

   // Вычисляем смещение до начала UDP-пейлоада (то есть до SIP)
    __u32 sip_payload_offset = ip_offset + ip_header_len + 8;

    // Проверяем, что пакет не короче этого смещения
    if (skb->len <= sip_payload_offset) {
        return 0;
    }

    // Вычисляем длину SIP-пейлоада
    __u32 sip_payload_len = skb->len - sip_payload_offset;

    // Ограничиваем размер (чтобы не переполнить буфер)
    if (sip_payload_len > MAX_SIP_SIZE) {
        sip_payload_len = MAX_SIP_SIZE;
    }

   // Резервируем буфер в ringbuf ТОЛЬКО под SIP-пейлоад
   // Вычислить область памяти для резерва динамически нельзя, верификатор не пропускает
   // Аллоцируем 512 байт
    void *data = bpf_ringbuf_reserve(&rb, SIP_BUFFER_SIZE, 0);
    if (!data) {
        bpf_printk("failed reserve memory for sip_payload_len");
        return 0;
    }

    bpf_printk("SIP matched:  %u->%u, sip_payload_offset=%u, sip_payload_len=%u", src_port,
        dest_port, sip_payload_offset, sip_payload_len);

    // 🔥 Пересчитываем длину ПОСЛЕ reserve — чтобы верификатор "увидел" её в контексте
    __u32 copy_len = skb->len - sip_payload_offset;

    // 🔥 Проверяем, что copy_len > 0 — КРИТИЧЕСКИ ВАЖНО
    if (copy_len == 0) {
        bpf_printk("copy_len failed");
        bpf_ringbuf_discard(data, 0);
        return 0;
    }

    copy_len &= 0xFFFF;  // ← ЯВНАЯ ГАРАНТИЯ: неотрицательное, <= 65535
    if (copy_len > MAX_SIP_SIZE) {
        copy_len = MAX_SIP_SIZE;
    }


   // Копируем SIP-пейлоад из пакета в буфер
//    ret = bpf_skb_load_bytes(skb, sip_payload_offset, data, copy_len);
    ret = bpf_skb_load_bytes(skb, sip_payload_offset, data, MAX_SIP_SIZE);
    if (ret < 0) {
        bpf_printk("failed bpf_skb_load_bytes");

        // Ошибка копирования — освобождаем буфер
        bpf_ringbuf_discard(data, 0);
        return 0;
    }

    bpf_ringbuf_submit(data, 0);

    return 0;
}

char _license[] SEC("license") = "GPL";
