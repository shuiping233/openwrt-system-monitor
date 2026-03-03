#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/tcp.h>
#include <linux/udp.h>
#include <linux/in.h>
#include <linux/pkt_cls.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

struct flow_key {
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8  proto;
    __u8  _pad[3]; 
};

struct flow_stats {
    __u64 packets;
    __u64 bytes;
    __u64 last_seen;
};

// 配置 Map：用于惰性求值开关状态
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, __u32);
} config_map SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 65535);
    __type(key, struct flow_key);
    __type(value, struct flow_stats);
} flow_map SEC(".maps");

SEC("classifier")
int count_flow(struct __sk_buff *skb) {
    // --- 惰性求值控制 ---
    __u32 config_key = 0;
    __u32 *enabled = bpf_map_lookup_elem(&config_map, &config_key);
    if (!enabled || *enabled == 0) {
        return TC_ACT_OK;
    }

    void *data = (void *)(long)skb->data;
    void *data_end = (void *)(long)skb->data_end;

    struct ethhdr *eth = data;
    if (data + sizeof(*eth) > data_end) return TC_ACT_OK;
    if (eth->h_proto != bpf_htons(ETH_P_IP)) return TC_ACT_OK;

    struct iphdr *ip = data + sizeof(*eth);
    if ((void *)ip + sizeof(*ip) > data_end) return TC_ACT_OK;

    struct flow_key key = {0};
    key.src_ip = ip->saddr;
    key.dst_ip = ip->daddr;
    key.proto  = ip->protocol;

    void *l4_header = (void *)ip + (ip->ihl * 4);

    if (key.proto == IPPROTO_TCP) {
        struct tcphdr *tcp = l4_header;
        if ((void *)tcp + sizeof(*tcp) <= data_end) {
            key.src_port = bpf_ntohs(tcp->source);
            key.dst_port = bpf_ntohs(tcp->dest);
        }
    } else if (key.proto == IPPROTO_UDP) {
        struct udphdr *udp = l4_header;
        if ((void *)udp + sizeof(*udp) <= data_end) {
            key.src_port = bpf_ntohs(udp->source);
            key.dst_port = bpf_ntohs(udp->dest);
        }
    }

    struct flow_stats *val = bpf_map_lookup_elem(&flow_map, &key);
    __u64 now = bpf_ktime_get_ns();

    if (val) {
        __sync_fetch_and_add(&val->packets, 1);
        __sync_fetch_and_add(&val->bytes, skb->len);
        val->last_seen = now;
    } else {
        struct flow_stats new_stats = {
            .packets = 1, 
            .bytes = skb->len, 
            .last_seen = now
        };
        bpf_map_update_elem(&flow_map, &key, &new_stats, BPF_ANY);
    }

    return TC_ACT_OK;
}

char _license[] SEC("license") = "GPL";