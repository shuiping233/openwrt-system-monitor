//go:build linux

package metric

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	bpf "openwrt-diskio-api/backend/pkg/ebpf"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

var (
	targetInterface = "eth1"
)

type IPMetrics struct {
	IP            string
	UploadRate    float64
	DownloadRate  float64
	TotalUpload   uint64
	TotalDownload uint64
}

func main() {
	if err := rlimit.RemoveMemlock(); err != nil {
		log.Fatalf("无法解除内存限制: %v", err)
	}

	objs := bpf.BpfObjects{}
	if err := bpf.LoadBpfObjects(&objs, nil); err != nil {
		log.Fatalf("加载 BPF 对象失败: %v", err)
	}
	defer objs.Close()

	link, err := netlink.LinkByName(targetInterface)
	if err != nil {
		log.Fatalf("🔴 错误: 未找到网卡 %s: %v", targetInterface, err)
	}

	if err := attachTCObjects(link, objs.CountFlow.FD()); err != nil {
		log.Fatalf("❌ 挂载网卡 %s 失败: %v", targetInterface, err)
	}
	fmt.Printf("✅ 正在监控局域网流量: %s\n", targetInterface)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	StartCapture(&objs)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastSnapshots := make(map[bpf.BpfFlowKey]uint64)
	metricsMap := make(map[uint32]*IPMetrics)

	for {
		select {
		case <-sig:
			fmt.Println("\n正在清理并退出...")
			StopCapture(&objs)
			cleanUpTC(link)
			return

		case <-ticker.C:
			nowKtime := getKtimeNS()
			timeout := uint64(10 * time.Second) // 10秒无流量老化

			// 重置每秒速率
			for _, m := range metricsMap {
				m.UploadRate = 0
				m.DownloadRate = 0
			}

			var key bpf.BpfFlowKey
			var val bpf.BpfFlowStats
			iter := objs.FlowMap.Iterate()

			for iter.Next(&key, &val) {
				// 老化处理
				if nowKtime-val.LastSeen > timeout {
					objs.FlowMap.Delete(key)
					delete(lastSnapshots, key)
					continue
				}

				currentKey := key
				currentBytes := val.Bytes
				delta := uint64(0)
				if lastBytes, ok := lastSnapshots[currentKey]; ok {
					if currentBytes >= lastBytes {
						delta = currentBytes - lastBytes
					}
				} else {
					delta = currentBytes
				}
				lastSnapshots[currentKey] = currentBytes

				aggregate(key, delta, metricsMap)
			}

			drawUI(metricsMap)
		}
	}
}

func aggregate(key bpf.BpfFlowKey, delta uint64, res map[uint32]*IPMetrics) {
	rateKB := float64(delta) / 1024.0

	// 只统计局域网段 IP 的流量
	if isPrivateIP(key.SrcIp) {
		m := getOrCreateMetrics(key.SrcIp, res)
		m.UploadRate += rateKB
		m.TotalUpload += delta
	}

	if isPrivateIP(key.DstIp) {
		m := getOrCreateMetrics(key.DstIp, res)
		m.DownloadRate += rateKB
		m.TotalDownload += delta
	}
}

func getOrCreateMetrics(ip uint32, res map[uint32]*IPMetrics) *IPMetrics {
	if m, ok := res[ip]; ok {
		return m
	}
	m := &IPMetrics{IP: formatIP(ip)}
	res[ip] = m
	return m
}

func drawUI(metrics map[uint32]*IPMetrics) {
	var keys []uint32
	for k := range metrics {
		keys = append(keys, k)
	}

	// 排序：按累计总流量降序
	sort.Slice(keys, func(i, j int) bool {
		mi, mj := metrics[keys[i]], metrics[keys[j]]
		return (mi.TotalUpload + mi.TotalDownload) > (mj.TotalUpload + mj.TotalDownload)
	})

	fmt.Printf("\033[H\033[2J") // 清屏
	fmt.Printf("【 局域网流量统计 (eBPF) 】- %s\n", time.Now().Format("15:04:05"))
	fmt.Printf("%-18s | %-12s | %-12s | %-12s\n", "内网 IP 地址", "上传(KB/s)", "下载(KB/s)", "累计总流量")
	fmt.Println(strings.Repeat("-", 65))

	if len(keys) == 0 {
		fmt.Println("  等待流量...")
		return
	}

	for _, key := range keys {
		m := metrics[key]
		totalMB := float64(m.TotalUpload+m.TotalDownload) / 1024 / 1024

		// 过滤掉没跑过流量且没累计数据的 IP (保持界面干净)
		if m.UploadRate == 0 && m.DownloadRate == 0 && totalMB < 0.01 {
			continue
		}

		fmt.Printf("%-18s | %10.2f | %10.2f | %10.2f MB\n",
			m.IP, m.UploadRate, m.DownloadRate, totalMB)
	}
}

// --- 基础辅助函数 ---

func getKtimeNS() uint64 {
	var ts unix.Timespec
	unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts)
	return uint64(ts.Sec)*1e9 + uint64(ts.Nsec)
}

func isPrivateIP(ip uint32) bool {
	b1 := byte(ip & 0xFF)
	b2 := byte((ip >> 8) & 0xFF)
	return b1 == 10 || (b1 == 172 && b2 >= 16 && b2 <= 31) || (b1 == 192 && b2 == 168)
}

func formatIP(n uint32) string {
	return net.IPv4(byte(n), byte(n>>8), byte(n>>16), byte(n>>24)).String()
}

// --- TC 控制 ---

func attachTCObjects(link netlink.Link, fd int) error {
	cleanUpTC(link)
	qdisc := &netlink.GenericQdisc{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: link.Attrs().Index,
			Handle:    netlink.MakeHandle(0xffff, 0),
			Parent:    netlink.HANDLE_CLSACT,
		},
		QdiscType: "clsact",
	}
	if err := netlink.QdiscAdd(qdisc); err != nil {
		return err
	}

	parents := []uint32{netlink.HANDLE_MIN_INGRESS, netlink.HANDLE_MIN_EGRESS}
	for _, parent := range parents {
		filter := &netlink.BpfFilter{
			FilterAttrs: netlink.FilterAttrs{
				LinkIndex: link.Attrs().Index,
				Parent:    parent,
				Priority:  1,
				Protocol:  unix.ETH_P_ALL,
			},
			Fd:           fd,
			DirectAction: true,
		}
		if err := netlink.FilterAdd(filter); err != nil {
			return err
		}
	}
	return nil
}

func cleanUpTC(link netlink.Link) {
	qdiscs, _ := netlink.QdiscList(link)
	for _, q := range qdiscs {
		if q.Attrs().Parent == netlink.HANDLE_CLSACT {
			netlink.QdiscDel(q)
		}
	}
}

func StartCapture(objs *bpf.BpfObjects) {
	var key uint32 = 0
	var val uint32 = 1
	objs.ConfigMap.Update(&key, &val, ebpf.UpdateAny)
}

func StopCapture(objs *bpf.BpfObjects) {
	var key uint32 = 0
	var val uint32 = 0
	objs.ConfigMap.Update(&key, &val, ebpf.UpdateAny)
}
