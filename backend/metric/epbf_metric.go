//go:build linux

package metric

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"openwrt-diskio-api/backend/model"
	bpf "openwrt-diskio-api/backend/pkg/ebpf"
	"openwrt-diskio-api/backend/utils"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type IPMetrics struct {
	IP            string
	UploadRate    float64
	DownloadRate  float64
	TotalUpload   uint64
	TotalDownload uint64
}

type EbpfNetTrafficService struct {
	captureInterface    string
	interfaceIpv4       netip.Addr
	interfaceIpv4Prefix netip.Prefix
	interfaceIpv6       netip.Addr
	interfaceIpv6Prefix netip.Prefix
	keyExpiredTime      time.Duration
	activeChan          chan struct{}
	objs                *bpf.BpfObjects
	link                netlink.Link
	metricsMap          map[netip.Addr]*IPMetrics
	mutex               sync.RWMutex
	lastRequestTimeUnix int64
	captureStartAt      int64
}

func NewEbpfNetTrafficService(keyExpiredTime time.Duration) *EbpfNetTrafficService {
	return &EbpfNetTrafficService{
		keyExpiredTime: keyExpiredTime,
		activeChan:     make(chan struct{}, 1),
		metricsMap:     make(map[netip.Addr]*IPMetrics),
		captureStartAt: time.Now().UnixNano(),
	}
}

func (svc *EbpfNetTrafficService) InitEbpfInterfaceDevice(targetInterface string) error {
	svc.captureInterface = targetInterface
	ipv4, ipv4Prefix, err := utils.GetInterfaceIpv4Info(targetInterface)
	if err != nil {
		return err
	}
	log.Printf("Get %q interface ipv4: %q \n", targetInterface, ipv4.String())
	log.Printf("Get %q interface ipv4Prefix: %q \n", targetInterface, ipv4Prefix.String())
	ipv6, ipv6Prefix, err := utils.GetInterfaceGuaIpv6Info(targetInterface)
	if err != nil {
		return err
	}
	log.Printf("Get %q interface ipv6: %q \n", targetInterface, ipv6.String())
	log.Printf("Get %q interface ipv6Prefix: %q \n", targetInterface, ipv6Prefix.String())
	svc.interfaceIpv4 = ipv4
	svc.interfaceIpv4Prefix = ipv4Prefix
	svc.interfaceIpv6 = ipv6
	svc.interfaceIpv6Prefix = ipv6Prefix

	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("Try to remove ebpf memory lock failed: %w", err)
	}

	objs := bpf.BpfObjects{}
	if err := bpf.LoadBpfObjects(&objs, nil); err != nil {
		return fmt.Errorf("Load BPF object failed: %w", err)
	}

	link, err := netlink.LinkByName(targetInterface)
	if err != nil {
		return fmt.Errorf("Network interface %q not found: %w", targetInterface, err)
	}

	if err := attachTCObjects(link, objs.CountFlow.FD()); err != nil {
		log.Fatalf("Attach network interface %q failed: %s", targetInterface, err)
	}
	log.Printf("Capture traffic from interface %q now\n", targetInterface)

	startCapture(&objs)
	svc.link = link
	svc.objs = &objs

	return nil
}

func (svc *EbpfNetTrafficService) frame(
	objs *bpf.BpfObjects,
	keyExpiredTime time.Duration,
	lastSnapshots map[bpf.BpfFlowKey]uint64,
) {
	if !isCapturing(objs) {
		return
	}

	metricsMap := svc.metricsMap
	lastUnix := atomic.LoadInt64(&svc.lastRequestTimeUnix)
	lastTime := time.Unix(0, lastUnix)
	svc.mutex.Lock()
	defer svc.mutex.Unlock()

	// 如果超过 n 秒没收到新请求，则关停以节省资源
	if time.Since(lastTime) > keyExpiredTime {
		stopCapture(objs)
		clearFlowMap(objs.FlowMap)
		clear(metricsMap)
		clear(lastSnapshots)
		return
	}

	nowKtime := getKtimeNS()
	timeout := uint64(keyExpiredTime) // n秒无流量老化

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

		svc.trafficAggregate(key, delta, metricsMap)
	}
}

func (svc *EbpfNetTrafficService) Run(ctx context.Context) {
	updateChan, done, err := subscribeNetworkChanges()
	if err != nil {
		log.Fatalln(err)
	}
	defer close(done)
	go svc.WatchNetworkChanges(ctx, updateChan)

	objs := svc.objs
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	keyExpiredTime := svc.keyExpiredTime
	lastSnapshots := make(map[bpf.BpfFlowKey]uint64)
	atomic.StoreInt64(&svc.lastRequestTimeUnix, time.Now().UnixNano())
	atomic.StoreInt64(&svc.captureStartAt, time.Now().UnixNano())

	for {
		select {
		case <-ctx.Done():
			return

		case <-svc.activeChan:
			// 1. 收到接口请求信号，刷新最后活跃时间
			if !isCapturing(objs) {
				atomic.StoreInt64(&svc.captureStartAt, time.Now().UnixNano())
				startCapture(objs)
			}

		case <-ticker.C:
			svc.frame(
				objs,
				keyExpiredTime,
				lastSnapshots,
			)
		}
	}
}

func (svc *EbpfNetTrafficService) ActiveSignal() {
	// log.Println("Receive ebpf active signal")
	atomic.StoreInt64(&svc.lastRequestTimeUnix, time.Now().UnixNano())

	// 如果通道满了，说明 Run 循环还没来得及处理之前的信号
	// 没关系，因为时间戳已经原子更新了，Run 下一秒会读到最新的
	select {
	case svc.activeChan <- struct{}{}:
	default:
	}
}

func (svc *EbpfNetTrafficService) Close() {
	log.Println("Cleaning ebpf resources...")
	stopCapture(svc.objs)
	cleanUpTC(svc.link)
	svc.objs.Close()
}

func (svc *EbpfNetTrafficService) GetAggregationTrafficMetric() *model.AggregationTrafficMetric {
	metricsMap := svc.metricsMap
	captureStartAtUnix := atomic.LoadInt64(&svc.captureStartAt)
	captureStartAt := time.Unix(0, captureStartAtUnix)
	result := &model.AggregationTrafficMetric{
		CaptureStartAt:   captureStartAt,
		CaptureInterface: svc.captureInterface,
		Details:          make([]model.AggregationTrafficDetails, 0, len(metricsMap)),
	}
	svc.mutex.RLock()
	defer svc.mutex.RUnlock()
	for ip, value := range metricsMap {
		ipStr := formatIP(ip)
		rate, unit := utils.ConvertBytes(value.DownloadRate, model.BSecond)
		incoming := model.MetricUnit{
			Value: rate,
			Unit:  unit,
		}
		rate, unit = utils.ConvertBytes(value.UploadRate, model.BSecond)
		outgoing := model.MetricUnit{
			Value: rate,
			Unit:  unit,
		}
		rate, unit = utils.ConvertBytes(value.DownloadRate+value.UploadRate, model.BSecond)
		totalThroughput := model.MetricUnit{
			Value: rate,
			Unit:  unit,
		}

		total, unit := utils.ConvertBytes(float64(value.TotalDownload), model.Byte)
		totalIncoming := model.MetricUnit{
			Value: total,
			Unit:  unit,
		}
		total, unit = utils.ConvertBytes(float64(value.TotalUpload), model.Byte)
		totalOutgoing := model.MetricUnit{
			Value: total,
			Unit:  unit,
		}
		total, unit = utils.ConvertBytes(float64(value.TotalDownload+value.TotalUpload), model.Byte)
		totalTraffic := model.MetricUnit{
			Value: total,
			Unit:  unit,
		}

		ipFamily := model.IpFamilyTypeIpv4
		if ip.Is6() {
			ipFamily = model.IpFamilyTypeIpv6
		}

		result.Details = append(result.Details, model.AggregationTrafficDetails{
			Ip:              ipStr,
			IpType:          model.IpAddressTypeLan, // TODO 先写死,因为其他类型的ip流量抓取还没做
			IpFamily:        ipFamily,
			Incoming:        incoming,
			Outgoing:        outgoing,
			TotalThroughput: totalThroughput,
			TotalIncoming:   totalIncoming,
			TotalOutgoing:   totalOutgoing,
			TotalTraffic:    totalTraffic,
			Tcp:             -1, // TODO 先这样,后面会找NetworkConnection里的值统计好之后再填进去
			Udp:             -1, // TODO 先这样,后面会找NetworkConnection里的值统计好之后再填进去
			Other:           -1, // TODO 先这样,后面会找NetworkConnection里的值统计好之后再填进去
		})
	}
	return result
}

func (svc *EbpfNetTrafficService) trafficAggregate(key bpf.BpfFlowKey, delta uint64, res map[netip.Addr]*IPMetrics) {
	rateByte := float64(delta)
	srcAddr := svc.parseToAddr(key.SrcAddr, key.Family)
	dstAddr := svc.parseToAddr(key.DstAddr, key.Family)

	// 不过滤openwrt自身ipv6地址,因为不考虑nat66
	if srcAddr != svc.interfaceIpv4 && svc.IsInLocalSubnet(srcAddr) {
		metric := getOrCreateMetrics(srcAddr, res)
		metric.UploadRate += rateByte
		metric.TotalUpload += delta
	}

	if dstAddr != svc.interfaceIpv4 && svc.IsInLocalSubnet(dstAddr) {
		metric := getOrCreateMetrics(dstAddr, res)
		metric.DownloadRate += rateByte
		metric.TotalDownload += delta
	}

}

func (svc *EbpfNetTrafficService) parseToAddr(addr [4]uint32, family uint8) netip.Addr {
	if family == 2 { // AF_INET
		// 将小端序 uint32 转为 4 字节数组
		b := [4]byte{byte(addr[0]), byte(addr[0] >> 8), byte(addr[0] >> 16), byte(addr[0] >> 24)}
		return netip.AddrFrom4(b)
	}
	// IPv6: 直接从 16 字节切片读取
	b := [16]byte{}
	binary.NativeEndian.PutUint32(b[0:4], addr[0])
	binary.NativeEndian.PutUint32(b[4:8], addr[1])
	binary.NativeEndian.PutUint32(b[8:12], addr[2])
	binary.NativeEndian.PutUint32(b[12:16], addr[3])
	return netip.AddrFrom16(b)
}

func (svc *EbpfNetTrafficService) refreshInterfaceInfo() {
	// 防止 refresh 时 frame 函数正在读取
	ipv4, ipv4Prefix, err4 := utils.GetInterfaceIpv4Info(svc.captureInterface)
	if err4 != nil {
		log.Println(err4)
	}
	ipv6, ipv6Prefix, err6 := utils.GetInterfaceGuaIpv6Info(svc.captureInterface)
	if err6 != nil {
		log.Println(err6)
	}
	if err4 != nil && err6 != nil {
		return
	}

	v4Change := ipv4Prefix != svc.interfaceIpv4Prefix
	v6Change := ipv6Prefix != svc.interfaceIpv6Prefix
	if !v4Change && !v6Change {
		return
	}

	svc.mutex.Lock()
	defer svc.mutex.Unlock()
	if err4 == nil && v4Change {
		svc.interfaceIpv4 = ipv4
		svc.interfaceIpv4Prefix = ipv4Prefix
		log.Printf("[Network] IPv4 Updated: %s (Prefix: %s)", ipv4, ipv4Prefix)
	}

	if err6 == nil && v6Change {
		svc.interfaceIpv6 = ipv6
		svc.interfaceIpv6Prefix = ipv6Prefix
		log.Printf("[Network] IPv6 Updated: %s (Prefix: %s)", ipv6, ipv6Prefix)
	}
}

func (svc *EbpfNetTrafficService) WatchNetworkChanges(ctx context.Context, ch <-chan netlink.AddrUpdate) {
	log.Println("Watching for network interface changes...")
	for {
		select {
		case <-ctx.Done():
			return
		case update, ok := <-ch:
			if !ok {
				log.Println("Netlink address update channel closed")
				return
			}
			link, _ := netlink.LinkByIndex(update.LinkIndex)
			if link != nil && link.Attrs().Name == svc.captureInterface {
				// 内核很多网卡事件都会进来,所以不打印
				// log.Printf("Network change (NewAddr: %v) detected on %s", update.NewAddr, svc.captureInterface)
				svc.refreshInterfaceInfo()
			}
		}
	}
}

// 一定要记得close(done)通道
func subscribeNetworkChanges() (updateChan chan netlink.AddrUpdate, done chan struct{}, err error) {
	updateChan = make(chan netlink.AddrUpdate)
	done = make(chan struct{})
	if err := netlink.AddrSubscribe(updateChan, done); err != nil {
		return nil, nil, fmt.Errorf("failed to subscribe netlink addr changes: %w", err)
	}
	return updateChan, done, nil
}

func getOrCreateMetrics(ip netip.Addr, res map[netip.Addr]*IPMetrics) *IPMetrics {
	if m, ok := res[ip]; ok {
		return m
	}
	m := &IPMetrics{IP: formatIP(ip)}
	res[ip] = m
	return m
}

func getKtimeNS() uint64 {
	var ts unix.Timespec
	unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts)
	return uint64(ts.Sec)*1e9 + uint64(ts.Nsec)
}

func (svc *EbpfNetTrafficService) IsInLocalSubnet(ip netip.Addr) bool {
	if ip.Is4() {
		return svc.interfaceIpv4Prefix.Contains(ip)
	}

	if ip.Is6() {
		// 过滤链路本地地址 (fe80::/10)，这种流量通常不计入互联网统计
		if ip.IsLinkLocalUnicast() {
			return false
		}
		// 只有在 Prefix 有效时才进行判断
		if !svc.interfaceIpv6Prefix.IsValid() {
			return false
		}
		return svc.interfaceIpv6Prefix.Contains(ip)
	}
	return false
}

func formatIP(n netip.Addr) string {
	return n.String()
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

func startCapture(objs *bpf.BpfObjects) {
	log.Println("Enable ebpf network traffic capture")
	key := uint32(0)
	val := uint32(1)
	objs.ConfigMap.Update(&key, &val, ebpf.UpdateAny)
}

func stopCapture(objs *bpf.BpfObjects) {
	log.Println("Disable ebpf network traffic capture")
	key := uint32(0)
	val := uint32(0)
	objs.ConfigMap.Update(&key, &val, ebpf.UpdateAny)
}

func isCapturing(objs *bpf.BpfObjects) bool {
	key := uint32(0)
	val := uint32(0)

	err := objs.ConfigMap.Lookup(&key, &val)
	if err != nil {
		return false
	}
	return val == 1
}

func clearFlowMap(m *ebpf.Map) {
	if m == nil {
		return
	}

	var keys []bpf.BpfFlowKey
	var key bpf.BpfFlowKey
	// 不能传 nil 进去 ebpf map
	var val bpf.BpfFlowStats

	iter := m.Iterate()
	for iter.Next(&key, &val) {
		keys = append(keys, key)
	}

	if err := iter.Err(); err != nil {
		return
	}

	for _, k := range keys {
		_ = m.Delete(k)
	}
}
